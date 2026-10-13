package pdf

import (
	"image/color"
	"math"
	"net/url"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/timzifer/cera"
)

// outlineWidth is the minimum width of the outline panel.
const outlineWidth = 240

type (
	// OutlineItem is an entry of the document outline, the bookmarks a
	// viewer shows in a side panel.
	OutlineItem struct {
		Title string
		// Dest is where the entry leads in the document; nil for an entry
		// that leads elsewhere (URI, External) or nowhere, such as a
		// heading of its children.
		Dest *Destination
		// URI is the address of an entry that leads to the web.
		URI string
		// External is set for an entry that leads to another file or
		// launches an application; File names it. The widget does not
		// follow it.
		External bool
		File     string
		// Open says that the children are shown at first.
		Open bool
		// Bold and Italic are the style of the title, Color its colour
		// (black by default).
		Bold, Italic bool
		Color        color.RGBA
		Children     []OutlineItem
	}

	// Destination is a view of a page, as outline entries and links lead
	// to, in points (1/72 inch) of the displayed page from its top left.
	Destination struct {
		// Page is the index (0-based) of the page.
		Page int
		// Fit is the kind of view: "XYZ" shows X, Y at the top left at
		// Zoom; "Fit" fits the page into the view, "FitH" its width with
		// Y at the top, "FitV" its height with X at the left, "FitR" Rect;
		// "FitB", "FitBH" and "FitBV" are treated as their plain kinds.
		Fit string
		// X and Y are the point of the page at the top left of the view;
		// NaN keeps the current one, or for Y, the top of the page.
		X, Y float64
		// Zoom is the zoom of XYZ (1 is 100 %); 0 keeps the current one.
		Zoom float64
		// Rect is the area to fit into the view for FitR.
		Rect Rect
	}

	// outlinePanel shows the outline of the document beside the pages.
	outlinePanel struct {
		doc  *Document
		tree *widget.Tree
		// box holds the tree with its minimum width.
		box *fyne.Container
		// items are the entries by tree node id: the path of indices,
		// "0/2/1"; children the ids of the children of a node, "" for the
		// top. order lists the ids in document order.
		items    map[string]*OutlineItem
		children map[string][]string
		order    []string
		// current is the entry of the current page; "" if none.
		current string
	}

	// outlineEntry is the title of an entry in the tree. It takes the taps
	// rather than the tree: a tap on the entry already marked as current
	// goes there, too.
	outlineEntry struct {
		widget.BaseWidget

		panel *outlinePanel
		id    string
		text  *canvas.Text
	}
)

// Outline returns the entries at the top of the document outline, nil for
// a PDF without one, or after Close.
func (s *Source) Outline() []OutlineItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return nil
	}
	return s.outlineItems(s.doc.Outline())
}

func (s *Source) outlineItems(items []cera.OutlineItem) []OutlineItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]OutlineItem, len(items))
	for i, item := range items {
		o := OutlineItem{
			Title:    item.Title,
			Open:     item.Open,
			Bold:     item.Bold,
			Italic:   item.Italic,
			Color:    item.Color,
			Children: s.outlineItems(item.Children),
		}
		if t := item.Target; t != nil {
			o.URI, o.External, o.File = t.URI, t.External, t.File
			o.Dest = s.destination(t)
		}
		out[i] = o
	}
	return out
}

// destination resolves t to a view of a page of the document; nil if it
// leads elsewhere or nowhere.
func (s *Source) destination(t *cera.LinkTarget) *Destination {
	dest, ok := s.doc.Destination(t)
	if !ok {
		return nil
	}
	return &Destination{
		Page: dest.Page,
		Fit:  dest.Fit,
		X:    dest.X,
		Y:    dest.Y,
		Zoom: dest.Zoom,
		Rect: Rect{X0: dest.Rect.X0, Y0: dest.Rect.Y0, X1: dest.Rect.X1, Y1: dest.Rect.Y1},
	}
}

// Outline returns the outline of the loaded PDF, see [Source.Outline].
func (d *Document) Outline() []OutlineItem {
	if src := d.loadedSource(); src != nil {
		return src.Outline()
	}
	return nil
}

// ShowOutline shows or hides the outline panel beside the pages. It is
// hidden at first; the toolbar has a button for it while the PDF has an
// outline. A PDF without one hides it.
func (d *Document) ShowOutline(show bool) {
	show = show && len(d.outline.order) > 0
	if show != d.outline.box.Visible() {
		if show {
			d.outline.box.Show()
		} else {
			d.outline.box.Hide()
		}
		d.left.Refresh()
		d.base.Refresh()
	}
	d.updateOutlineButton()
}

// OutlineShown reports whether the outline panel is shown.
func (d *Document) OutlineShown() bool {
	return d.outline.box.Visible()
}

func (d *Document) updateOutlineButton() {
	if len(d.outline.order) == 0 {
		d.outlineButton.Hide()
		return
	}
	if d.outline.box.Visible() {
		d.outlineButton.Importance = widget.MediumImportance
	} else {
		d.outlineButton.Importance = widget.LowImportance
	}
	d.outlineButton.Show()
	d.outlineButton.Refresh()
}

// GoTo shows dest: it makes its page current, and zooms and scrolls as its
// kind of view says. A destination out of range is ignored.
func (d *Document) GoTo(dest Destination) {
	page := dest.Page
	if page < 0 || page >= d.PageCount() {
		return
	}
	x, y := dest.X, dest.Y
	d.turnPage(page, false)
	d.setCurrent(page)
	switch strings.Replace(dest.Fit, "FitB", "Fit", 1) {
	case "XYZ":
		if dest.Zoom > 0 {
			d.fitHorizontal, d.fitVertical = false, false
			d.zoomAt(dest.Zoom, fyne.Position{})
		}
	case "FitH":
		d.fitZoom(true, false)
		x = math.NaN()
	case "FitV":
		d.fitZoom(false, true)
		y = math.NaN()
	case "FitR":
		r := dest.Rect
		w, h := math.Max(r.X1-r.X0, 1), math.Max(r.Y1-r.Y0, 1)
		view := d.scrollContainer.Size()
		zoom := math.Min(float64(view.Width)/(w*unitsPerPoint), float64(view.Height)/(h*unitsPerPoint))
		d.fitHorizontal, d.fitVertical = false, false
		d.zoomAt(zoom, fyne.Position{})
		// The rectangle in the middle of the view.
		zoom = d.scale * unitsPerPoint
		x = r.X0 - (float64(view.Width)/zoom-w)/2
		y = r.Y0 - (float64(view.Height)/zoom-h)/2
	default: // Fit, and what is not known
		d.fitZoom(true, true)
		x, y = math.NaN(), math.NaN()
	}
	d.scrollToPoint(page, x, y)
}

// scrollToPoint scrolls the point x, y of page (in points) to the top left
// of the view: NaN keeps the current offset for x, and the top of the page
// for y.
func (d *Document) scrollToPoint(page int, x, y float64) {
	scale := d.scale * unitsPerPoint
	origin := d.layout.origin(page, d.areaSize())
	offset := fyne.NewPos(d.scrollContainer.Offset.X, origin.Y)
	if !math.IsNaN(x) {
		offset.X = origin.X + float32(x*scale)
	}
	if !math.IsNaN(y) {
		offset.Y = origin.Y + float32(y*scale)
	}
	d.navigating = true
	d.scrollTo(offset)
	d.navigating = false
	d.setCurrent(page)
}

func newOutlinePanel(d *Document) *outlinePanel {
	p := &outlinePanel{doc: d, items: map[string]*OutlineItem{}, children: map[string][]string{}}
	p.tree = widget.NewTree(
		func(id string) []string { return p.children[id] },
		func(id string) bool { return len(p.children[id]) > 0 },
		func(bool) fyne.CanvasObject { return newOutlineEntry(p) },
		func(id string, _ bool, o fyne.CanvasObject) { o.(*outlineEntry).set(id) },
	)
	// The entry of the current page is marked in its closed parent.
	p.tree.OnBranchOpened = func(string) { p.markCurrent() }
	p.tree.OnBranchClosed = func(string) { p.markCurrent() }
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(outlineWidth, 0))
	p.box = container.NewStack(width, p.tree)
	p.box.Hide()
	return p
}

// setItems shows items, branches open where they say so.
func (p *outlinePanel) setItems(items []OutlineItem) {
	p.items, p.children, p.order, p.current = map[string]*OutlineItem{}, map[string][]string{}, nil, ""
	var open []string
	var add func(parent string, items []OutlineItem)
	add = func(parent string, items []OutlineItem) {
		for i := range items {
			id := strconv.Itoa(i)
			if parent != "" {
				id = parent + "/" + id
			}
			p.items[id] = &items[i]
			p.children[parent] = append(p.children[parent], id)
			p.order = append(p.order, id)
			if items[i].Open && len(items[i].Children) > 0 {
				open = append(open, id)
			}
			add(id, items[i].Children)
		}
	}
	add("", items)

	p.tree.UnselectAll()
	p.tree.CloseAllBranches()
	for _, id := range open {
		p.tree.OpenBranch(id)
	}
	p.tree.Refresh()
	p.tree.ScrollToTop()
	p.markCurrent()
}

// follow goes where the entry id leads.
func (p *outlinePanel) follow(id string) {
	item := p.items[id]
	if item == nil {
		return
	}
	switch {
	case item.Dest != nil:
		p.doc.GoTo(*item.Dest)
	case item.URI != "":
		if u, err := url.Parse(item.URI); err == nil {
			_ = p.doc.openURL(u)
		}
	}
	p.markCurrent()
}

// markCurrent selects the entry of the current page: the last one in
// document order that leads to it or a page before it; while it is in a
// closed branch, the nearest entry above it that is shown.
func (p *outlinePanel) markCurrent() {
	page := p.doc.CurrentPage()
	current := ""
	for _, id := range p.order {
		if dest := p.items[id].Dest; dest != nil && dest.Page <= page {
			current = id
		}
	}
	for parent := current; parent != ""; {
		i := strings.LastIndexByte(parent, '/')
		if i < 0 {
			break
		}
		parent = parent[:i]
		if !p.tree.IsBranchOpen(parent) {
			current = parent
		}
	}
	if current == p.current {
		return
	}
	p.current = current
	if current == "" {
		p.tree.UnselectAll()
		return
	}
	p.tree.Select(current)
	p.tree.ScrollTo(current)
}

func newOutlineEntry(p *outlinePanel) *outlineEntry {
	e := &outlineEntry{panel: p, text: canvas.NewText("", theme.Color(theme.ColorNameForeground))}
	e.ExtendBaseWidget(e)
	return e
}

func (e *outlineEntry) set(id string) {
	e.id = id
	item := e.panel.items[id]
	if item == nil {
		return
	}
	e.text.Text = item.Title
	e.text.TextStyle = fyne.TextStyle{Bold: item.Bold, Italic: item.Italic}
	// Black is the default colour of the format; on a dark theme it is
	// the theme's.
	if c := item.Color; c.R == 0 && c.G == 0 && c.B == 0 {
		e.text.Color = theme.Color(theme.ColorNameForeground)
	} else {
		e.text.Color = c
	}
	e.text.Refresh()
}

func (e *outlineEntry) Tapped(*fyne.PointEvent) {
	e.panel.follow(e.id)
}

func (e *outlineEntry) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewPadded(e.text))
}

var _ fyne.Tappable = (*outlineEntry)(nil)
