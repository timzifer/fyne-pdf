package pdf

import (
	"cmp"
	"context"
	"fmt"
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	// commentsWidth is the minimum width of the comments panel,
	// notePopupWidth the width of the popup that shows a note.
	commentsWidth  = 280
	notePopupWidth = 300
	// replyIndent indents a reply per level.
	replyIndent = 16
)

// selectionColor marks the selected annotation on its page.
var selectionColor = color.NRGBA{R: 0x33, G: 0x99, B: 0xff, A: 0x50}

type (
	// annotKey names an annotation: its page and its index in /Annots.
	annotKey struct{ page, index int }

	// commentsPanel lists the markup annotations of the document beside
	// the pages: one row per annotation that is not a reply, its replies
	// indented below it.
	commentsPanel struct {
		doc *Document
		// box holds the list with its minimum width; it is shown while
		// wanted and there is something to list.
		box    *fyne.Container
		list   *fyne.Container
		scroll *container.Scroll
		wanted bool

		// annots are the annotations by page, as far as read.
		annots map[int][]Annotation
		rows   map[annotKey]*commentRow
		// selected is the selected annotation, if any.
		selected    annotKey
		hasSelected bool
		popup       *widget.PopUp
	}

	// commentRow is an annotation in the comments panel.
	commentRow struct {
		widget.BaseWidget

		panel *commentsPanel
		annot Annotation
		depth int
		// state is the review state the replies set, shown as a badge.
		state      string
		background *canvas.Rectangle
	}
)

func newCommentsPanel(d *Document) *commentsPanel {
	p := &commentsPanel{doc: d, annots: map[int][]Annotation{}, rows: map[annotKey]*commentRow{}}
	p.list = container.NewVBox()
	p.scroll = container.NewVScroll(p.list)
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(commentsWidth, 0))
	p.box = container.NewStack(width, p.scroll)
	p.box.Hide()
	return p
}

// reset empties the panel for another document.
func (p *commentsPanel) reset() {
	p.hidePopup()
	p.annots, p.rows = map[int][]Annotation{}, map[annotKey]*commentRow{}
	p.hasSelected = false
	p.list.Objects = nil
	p.list.Refresh()
	p.scroll.ScrollToTop()
	p.doc.updateComments()
}

// addPage lists the annotations of page, read in the background: pages
// are added in their order, the annotations of one sorted by position.
func (p *commentsPanel) addPage(page int, annots []Annotation) {
	p.annots[page] = annots
	byIndex := map[int]bool{}
	for _, a := range annots {
		byIndex[a.Index] = true
	}
	isReply := func(a *Annotation) bool { return a.ReplyTo >= 0 && byIndex[a.ReplyTo] }

	var tops []*Annotation
	for i := range annots {
		if !isReply(&annots[i]) {
			tops = append(tops, &annots[i])
		}
	}
	slices.SortStableFunc(tops, func(a, b *Annotation) int {
		return cmp.Or(cmp.Compare(a.Bounds.Y0, b.Bounds.Y0), cmp.Compare(a.Bounds.X0, b.Bounds.X0))
	})

	seen := map[int]bool{}
	var add func(a *Annotation, depth int)
	add = func(a *Annotation, depth int) {
		if seen[a.Index] {
			return
		}
		seen[a.Index] = true
		row := newCommentRow(p, *a, depth)
		for i := range annots {
			if r := &annots[i]; r.ReplyTo == a.Index && !r.Group && r.State != "" {
				row.state = r.State
			}
		}
		p.rows[annotKey{page, a.Index}] = row
		p.list.Add(row)
		for i := range annots {
			if r := &annots[i]; r.ReplyTo == a.Index && !r.Group {
				add(r, depth+1)
			}
		}
	}
	for _, a := range tops {
		add(a, 0)
	}
	p.doc.updateComments()
}

// annotation returns the annotation k names, if it is read.
func (p *commentsPanel) annotation(k annotKey) (Annotation, bool) {
	for _, a := range p.annots[k.page] {
		if a.Index == k.index {
			return a, true
		}
	}
	return Annotation{}, false
}

// replies returns the replies to a, and theirs, in the order of /Annots.
func (p *commentsPanel) replies(a Annotation) []Annotation {
	var out []Annotation
	seen := map[int]bool{a.Index: true}
	var add func(index int)
	add = func(index int) {
		for _, r := range p.annots[a.Page] {
			if r.ReplyTo == index && !r.Group && !seen[r.Index] {
				seen[r.Index] = true
				out = append(out, r)
				add(r.Index)
			}
		}
	}
	add(a.Index)
	return out
}

// selectAnnotation marks the annotation k on its page and in the list.
func (p *commentsPanel) selectAnnotation(k annotKey) {
	old, had := p.selected, p.hasSelected
	p.selected, p.hasSelected = k, true
	if had {
		if row := p.rows[old]; row != nil {
			row.Refresh()
		}
	}
	if row := p.rows[k]; row != nil {
		row.Refresh()
		p.revealRow(row)
	}
	p.doc.updateHighlights()
}

func (p *commentsPanel) clearSelection() {
	if !p.hasSelected {
		return
	}
	p.hasSelected = false
	if row := p.rows[p.selected]; row != nil {
		row.Refresh()
	}
	p.hidePopup()
	p.doc.updateHighlights()
}

// revealRow scrolls the list to row if it is out of view.
func (p *commentsPanel) revealRow(row *commentRow) {
	top, bottom := row.Position().Y, row.Position().Y+row.Size().Height
	offset, view := p.scroll.Offset.Y, p.scroll.Size().Height
	if top < offset || bottom > offset+view {
		p.scroll.ScrollToOffset(fyne.NewPos(0, top))
	}
}

// showPopup shows the text of a and its replies beside a on the page.
func (p *commentsPanel) showPopup(a Annotation) {
	p.hidePopup()
	d := p.doc
	c := fyne.CurrentApp().Driver().CanvasForObject(d)
	if c == nil || !d.layout.contains(a.Page) {
		return
	}
	content := container.NewVBox(noteContent(a)...)
	for _, r := range p.replies(a) {
		content.Add(widget.NewSeparator())
		content.Add(container.NewPadded(container.NewVBox(noteContent(r)...)))
	}
	p.popup = widget.NewPopUp(container.NewVScroll(content), c)

	// Beside the annotation, within the window.
	scale := float32(d.scale * unitsPerPoint)
	origin := d.layout.origin(a.Page, d.areaSize()).Subtract(d.scrollContainer.Offset)
	view := fyne.CurrentApp().Driver().AbsolutePositionForObject(d.scrollContainer)
	pos := view.Add(origin).AddXY(float32(a.Bounds.X1)*scale+theme.Padding(), float32(a.Bounds.Y0)*scale)
	p.popup.Resize(fyne.NewSize(notePopupWidth, 1))
	size := fyne.NewSize(notePopupWidth, min(content.MinSize().Height+2*theme.Padding(), c.Size().Height/2))
	if pos.X+size.Width > c.Size().Width {
		pos.X = max(0, view.X+origin.X+float32(a.Bounds.X0)*scale-size.Width-theme.Padding())
	}
	pos.Y = max(0, min(pos.Y, c.Size().Height-size.Height))
	p.popup.Resize(size)
	p.popup.ShowAtPosition(pos)
}

func (p *commentsPanel) hidePopup() {
	if p.popup != nil {
		p.popup.Hide()
		p.popup = nil
	}
}

// noteContent is the author, kind, date and text of a.
func noteContent(a Annotation) []fyne.CanvasObject {
	author := a.Author
	if author == "" {
		author = "Unknown author"
	}
	objects := []fyne.CanvasObject{
		widget.NewLabelWithStyle(author, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		metaText(a),
	}
	if a.Contents != "" {
		body := widget.NewLabel(a.Contents)
		body.Wrapping = fyne.TextWrapWord
		objects = append(objects, body)
	}
	return objects
}

// metaText is the kind of a, its page and its date in small type.
func metaText(a Annotation) fyne.CanvasObject {
	s := fmt.Sprintf("%s · Page %d", annotationKind(a.Subtype), a.Page+1)
	date := a.Modified
	if date.IsZero() {
		date = a.Created
	}
	if !date.IsZero() {
		s += " · " + date.Local().Format("2006-01-02 15:04")
	}
	t := canvas.NewText(s, theme.Color(theme.ColorNamePlaceHolder))
	t.TextSize = theme.CaptionTextSize()
	return container.NewPadded(t)
}

// annotationKind names a subtype for people.
func annotationKind(subtype string) string {
	switch subtype {
	case "Text":
		return "Note"
	case "FreeText":
		return "Text box"
	case "Squiggly":
		return "Squiggly underline"
	case "StrikeOut":
		return "Strikethrough"
	case "Ink":
		return "Drawing"
	case "Square":
		return "Rectangle"
	case "Circle":
		return "Ellipse"
	case "PolyLine":
		return "Polyline"
	case "FileAttachment":
		return "Attachment"
	case "Caret":
		return "Insertion"
	}
	return subtype
}

func newCommentRow(p *commentsPanel, a Annotation, depth int) *commentRow {
	r := &commentRow{panel: p, annot: a, depth: depth, background: canvas.NewRectangle(color.Transparent)}
	r.ExtendBaseWidget(r)
	return r
}

func (r *commentRow) selected() bool {
	p := r.panel
	return p.hasSelected && p.selected == annotKey{r.annot.Page, r.annot.Index}
}

func (r *commentRow) Tapped(*fyne.PointEvent) {
	r.panel.doc.selectAnnotation(r.annot, true, false)
}

func (r *commentRow) CreateRenderer() fyne.WidgetRenderer {
	a := r.annot
	swatch := canvas.NewRectangle(a.Color)
	if a.Color == nil {
		swatch.FillColor = theme.Color(theme.ColorNameDisabled)
	}
	swatch.SetMinSize(fyne.NewSize(4, 0))

	content := noteContent(a)
	if r.state != "" {
		badge := widget.NewLabelWithStyle(r.state, fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
		badge.Importance = widget.SuccessImportance
		if r.state == "Rejected" || r.state == "Cancelled" {
			badge.Importance = widget.DangerImportance
		}
		content[0] = container.NewBorder(nil, nil, nil, badge, content[0])
	}
	indent := canvas.NewRectangle(color.Transparent)
	indent.SetMinSize(fyne.NewSize(float32(r.depth*replyIndent), 0))
	row := container.NewBorder(nil, widget.NewSeparator(), container.NewHBox(indent, swatch), nil,
		container.NewVBox(content...))
	r.updateBackground()
	return widget.NewSimpleRenderer(container.NewStack(r.background, row))
}

func (r *commentRow) Refresh() {
	r.updateBackground()
	r.BaseWidget.Refresh()
}

func (r *commentRow) updateBackground() {
	if r.selected() {
		r.background.FillColor = theme.Color(theme.ColorNameSelection)
	} else {
		r.background.FillColor = color.Transparent
	}
	r.background.Refresh()
}

var _ fyne.Tappable = (*commentRow)(nil)

// loadAnnotations reads the annotations of all pages of src in the
// background and lists them in the comments panel.
func (d *Document) loadAnnotations(src *Source, ctx context.Context, pages int) {
	d.pending.Add(1)
	go func() {
		defer d.pending.Done()
		for page := range pages {
			annots, err := src.Annotations(page)
			if ctx.Err() != nil {
				return
			}
			if err != nil || len(annots) == 0 {
				continue
			}
			d.runOnMain(func() {
				if ctx.Err() == nil {
					d.comments.addPage(page, annots)
				}
			})
		}
	}()
}

// ShowComments shows or hides the comments panel beside the pages: a list
// of the annotations of the document with their authors, dates and
// replies. It is hidden at first; the toolbar has a button for it once
// the document is found to have annotations.
func (d *Document) ShowComments(show bool) {
	d.comments.wanted = show
	d.updateComments()
}

// CommentsShown reports whether the comments panel is shown.
func (d *Document) CommentsShown() bool {
	return d.comments.box.Visible()
}

// Annotations returns the markup annotations of page read so far; they
// are read in the background after loading.
func (d *Document) Annotations(page int) []Annotation {
	return slices.Clone(d.comments.annots[page])
}

// updateComments shows the comments panel and its button as wanted and as
// there are annotations.
func (d *Document) updateComments() {
	p := d.comments
	has := len(p.rows) > 0
	if show := p.wanted && has; show != p.box.Visible() {
		if show {
			p.box.Show()
		} else {
			p.box.Hide()
		}
		d.base.Refresh()
	}
	if !has {
		d.commentsButton.Hide()
		return
	}
	if p.box.Visible() {
		d.commentsButton.Importance = widget.MediumImportance
	} else {
		d.commentsButton.Importance = widget.LowImportance
	}
	d.commentsButton.Show()
	d.commentsButton.Refresh()
}

// selectAnnotation selects a in the comments panel and marks it on its
// page; reveal scrolls the page to it, popup shows its text beside it.
func (d *Document) selectAnnotation(a Annotation, reveal, popup bool) {
	if reveal {
		d.revealRect(a.Page, a.Bounds)
	}
	d.comments.selectAnnotation(annotKey{a.Page, a.Index})
	if popup {
		d.comments.showPopup(a)
	}
}

// annotationAt returns the annotation at pos in the page area that is not
// a reply, if there is one; the one drawn last wins.
func (d *Document) annotationAt(pos fyne.Position) (Annotation, bool) {
	area := d.areaSize()
	page := d.layout.pageAt(pos.Y, area)
	if page < 0 || !d.layout.contains(page) {
		return Annotation{}, false
	}
	scale := float32(d.scale * unitsPerPoint)
	p := pos.Subtract(d.layout.origin(page, area))
	x, y := float64(p.X/scale), float64(p.Y/scale)
	annots := d.comments.annots[page]
	for i := len(annots) - 1; i >= 0; i-- {
		if a := &annots[i]; a.ReplyTo < 0 && a.contains(x, y) {
			return *a, true
		}
	}
	return Annotation{}, false
}

// selectionHighlights marks the selected annotation if it is on page.
func (d *Document) selectionHighlights(page int) []Highlight {
	p := d.comments
	if !p.hasSelected || p.selected.page != page {
		return nil
	}
	a, ok := p.annotation(p.selected)
	if !ok {
		return nil
	}
	rects := a.Rects
	if len(rects) == 0 {
		rects = []Rect{a.Bounds}
	}
	out := make([]Highlight, len(rects))
	for i, r := range rects {
		out[i] = Highlight{
			Position: fyne.NewPos(float32(r.X0*unitsPerPoint), float32(r.Y0*unitsPerPoint)),
			Size:     fyne.NewSize(float32((r.X1-r.X0)*unitsPerPoint), float32((r.Y1-r.Y0)*unitsPerPoint)),
			Color:    selectionColor,
		}
	}
	return out
}

// ShowAnnotation goes to a, one of [Document.Annotations], marks it on its
// page and in the comments panel, and shows its text and replies beside
// it.
func (d *Document) ShowAnnotation(a Annotation) {
	d.selectAnnotation(a, true, true)
}
