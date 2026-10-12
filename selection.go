package pdf

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/timzifer/cera"
)

// Tool is what dragging on the pages does, see [Document.SetTool].
type Tool int

const (
	// ToolNone pans the pages: the default.
	ToolNone Tool = iota
	// ToolSelect selects text; Ctrl+C copies it.
	ToolSelect
)

// textSelection is selected text: a range of the text of one page.
type textSelection struct {
	page   int
	text   *cera.PageText
	anchor cera.TextPos
	rng    cera.TextRange
}

// Tool returns what dragging on the pages does.
func (d *Document) Tool() Tool {
	return d.tool
}

// SetTool sets what dragging and tapping on the pages do: pan them
// ([ToolNone], the default), select text ([ToolSelect]; the toolbar has a
// button for it), or make annotations ([ToolHighlight], [ToolNote],
// [ToolInk], ...): they are written to the document at once, Undo and Redo
// take them back, Save writes the document with them. Switching clears
// the text selection.
func (d *Document) SetTool(t Tool) {
	if t == d.tool {
		return
	}
	d.tool = t
	d.ClearSelection()
	d.updateToolButton()
	d.loadTexts(d.livePages())
}

// SelectedText returns the selected text; "" if there is none.
func (d *Document) SelectedText() string {
	s := d.selection
	if s == nil || s.rng.Empty() {
		return ""
	}
	return s.text.TextOf(s.rng, cera.TextFormat{JoinHyphenated: true})
}

// CopySelection copies the selected text to the clipboard. It reports
// false, and copies nothing, when there is no selection or the document
// does not allow copying ([PermCopy]).
func (d *Document) CopySelection() bool {
	text := d.SelectedText()
	if text == "" || d.Permissions()&PermCopy == 0 {
		return false
	}
	fyne.CurrentApp().Clipboard().SetContent(text)
	return true
}

// ClearSelection clears the text selection.
func (d *Document) ClearSelection() {
	if d.selection == nil {
		return
	}
	d.selection = nil
	d.updateHighlights()
}

// selectionRects returns the areas of the selected text on page, in
// points of the displayed page.
func (d *Document) selectionRects(page int) []Rect {
	s := d.selection
	if s == nil || s.page != page || s.rng.Empty() {
		return nil
	}
	var out []Rect
	for _, q := range s.text.Quads(s.rng) {
		out = append(out, rectOf(q.Bounds()))
	}
	return out
}

// textAt returns the page at pos in the page area, its text if it is read
// already, and pos in points of that page.
func (d *Document) textAt(pos fyne.Position) (page int, text *cera.PageText, x, y float64) {
	area := d.areaSize()
	page = d.layout.pageAt(pos.Y, area)
	if page < 0 || !d.layout.contains(page) {
		return -1, nil, 0, 0
	}
	x, y = d.pagePoint(page, pos)
	d.mutex.Lock()
	text = d.texts[page]
	d.mutex.Unlock()
	return page, text, x, y
}

// pagePoint returns pos in the page area in points of page.
func (d *Document) pagePoint(page int, pos fyne.Position) (x, y float64) {
	scale := float32(d.scale * unitsPerPoint)
	p := pos.Subtract(d.layout.origin(page, d.areaSize()))
	return float64(p.X / scale), float64(p.Y / scale)
}

// startSelection starts selecting text at pos in the page area.
func (d *Document) startSelection(pos fyne.Position) {
	page, text, x, y := d.textAt(pos)
	if text == nil {
		d.ClearSelection()
		return
	}
	p := text.PosAt(x, y)
	d.selection = &textSelection{page: page, text: text, anchor: p, rng: cera.TextRange{Start: p, End: p}}
	d.updateHighlights()
}

// extendSelection selects from where the selection started to pos in the
// page area; on the page it started on.
func (d *Document) extendSelection(pos fyne.Position) {
	s := d.selection
	if s == nil {
		return
	}
	x, y := d.pagePoint(s.page, pos)
	s.rng = s.text.Select(s.anchor, s.text.PosAt(x, y), cera.SelectChars)
	d.updateHighlights()
}

// selectWord selects the word at pos in the page area.
func (d *Document) selectWord(pos fyne.Position) {
	page, text, x, y := d.textAt(pos)
	if text == nil {
		return
	}
	p := text.PosAt(x, y)
	d.selection = &textSelection{page: page, text: text, anchor: p, rng: text.Select(p, p, cera.SelectWords)}
	d.updateHighlights()
}

// selectPage selects all the text of the current page.
func (d *Document) selectPage() {
	page := d.CurrentPage()
	d.mutex.Lock()
	text := d.texts[page]
	d.mutex.Unlock()
	if text == nil {
		return
	}
	d.selection = &textSelection{page: page, text: text, rng: text.All()}
	d.updateHighlights()
}

// overText reports whether pos in the page area is on a character.
func (d *Document) overText(pos fyne.Position) bool {
	_, text, x, y := d.textAt(pos)
	if text == nil {
		return false
	}
	_, ok := text.CharAt(x, y)
	return ok
}

// livePages returns the pages that have objects in the view.
func (d *Document) livePages() []int {
	pages := make([]int, 0, len(d.views))
	for page := range d.views {
		pages = append(pages, page)
	}
	return pages
}

// loadTexts reads the text of those of pages not read yet in the
// background, while a tool needs it.
func (d *Document) loadTexts(pages []int) {
	if !d.tool.selectsText() {
		return
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.source == nil {
		return
	}
	var missing []int
	for _, page := range pages {
		if _, ok := d.texts[page]; !ok {
			d.texts[page] = nil
			missing = append(missing, page)
		}
	}
	if len(missing) == 0 {
		return
	}
	src, ctx := d.source, d.ctx
	d.pending.Add(1)
	go func() {
		defer d.pending.Done()
		for _, page := range missing {
			text, err := src.pageText(ctx, page)
			d.mutex.Lock()
			if ctx != d.ctx || ctx.Err() != nil {
				d.mutex.Unlock()
				return
			}
			if err == nil {
				d.texts[page] = text
			} else {
				// Asked for again next time.
				delete(d.texts, page)
			}
			d.mutex.Unlock()
		}
	}()
}

func (d *Document) updateToolButton() {
	if d.tool == ToolSelect {
		d.toolButton.Importance = widget.MediumImportance
	} else {
		d.toolButton.Importance = widget.LowImportance
	}
	d.toolButton.Refresh()
}

// cursor is the pointer over pos in the page area: a hand over a link, an
// I-beam over text while selecting it.
func (d *Document) cursor(pos fyne.Position) desktop.Cursor {
	if _, ok := d.linkAt(pos); ok {
		return desktop.PointerCursor
	}
	if d.tool.selectsText() && d.overText(pos) {
		return desktop.TextCursor
	}
	return desktop.DefaultCursor
}
