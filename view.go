package pdf

import (
	"image/color"
	"slices"
	"sort"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
)

// afterLayout calls after once layout has laid out the objects.
type afterLayout struct {
	layout fyne.Layout
	after  func()
}

func (l *afterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	l.layout.Layout(objects, size)
	l.after()
}

func (l *afterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return l.layout.MinSize(objects)
}

// pageAreaRenderer draws the background of the page view and the pages in
// or next to the view; the other pages get no objects.
type pageAreaRenderer struct {
	area       *pageArea
	background *canvas.Rectangle
	objects    []fyne.CanvasObject
}

var _ fyne.WidgetRenderer = (*pageAreaRenderer)(nil)

func (r *pageAreaRenderer) Destroy() {}

func (r *pageAreaRenderer) Layout(size fyne.Size) {
	r.background.Resize(size)
	d := r.area.doc
	for page, view := range d.views {
		if !d.layout.contains(page) {
			continue
		}
		view.Move(d.layout.origin(page, size))
		view.Resize(d.layout.pageSize(page))
	}
}

func (r *pageAreaRenderer) MinSize() fyne.Size {
	return r.area.doc.layout.size
}

func (r *pageAreaRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *pageAreaRenderer) Refresh() {
	d := r.area.doc
	r.background.FillColor = d.BackgroundColor
	if r.background.FillColor == nil {
		r.background.FillColor = theme.Color(theme.ColorNameSeparator)
	}
	r.background.Refresh()

	pages := make([]int, 0, len(d.views))
	for page := range d.views {
		pages = append(pages, page)
	}
	slices.Sort(pages)
	r.objects = append(r.objects[:0], r.background)
	for _, page := range pages {
		r.objects = append(r.objects, d.views[page])
	}
	r.Layout(r.area.Size())
}

// relayout lays out the pages shown for the view mode, the current page
// and the zoom, and sizes the scroll content for it. The scroll offset is
// left to the caller, followed by viewChanged.
func (d *Document) relayout() {
	first, last := 0, len(d.pageSizes)
	if d.viewMode == ViewSinglePage {
		first = d.CurrentPage()
		last = min(first+1, last)
	}
	d.layout = newPageLayout(d.pageSizes, first, last, d.scale)

	d.adjusting = true
	d.area.Resize(d.areaSize())
	d.scrollContainer.Refresh()
	d.adjusting = false
}

// setOffset scrolls the view to offset, clamped to the content, without
// updating anything else.
func (d *Document) setOffset(offset fyne.Position) {
	view := d.scrollContainer.Size()
	content := d.areaSize()
	d.adjusting = true
	d.scrollContainer.ScrollToOffset(fyne.NewPos(
		max(0, min(offset.X, content.Width-view.Width)),
		max(0, min(offset.Y, content.Height-view.Height)),
	))
	d.adjusting = false
}

// scrollTo moves the view to offset, clamped to the content.
func (d *Document) scrollTo(offset fyne.Position) {
	d.setOffset(offset)
	d.viewChanged(0)
}

// scrollToPage scrolls to the top or bottom of page, which has to be
// shown, without changing the current page.
func (d *Document) scrollToPage(page int, atBottom bool) {
	offset := fyne.NewPos(d.scrollContainer.Offset.X, d.layout.origin(page, d.areaSize()).Y)
	if atBottom {
		offset.Y += d.layout.pageSize(page).Height - d.scrollContainer.Size().Height
	}
	d.navigating = true
	d.scrollTo(offset)
	d.navigating = false
}

// scrolled follows a change of the scroll offset by the scroll container:
// scrolling with the wheel or the scroll bars.
func (d *Document) scrolled() {
	// While the view is resized, the scroll container clamps the offset
	// before viewResized puts the anchor back in place.
	if d.adjusting || d.scrollContainer.Size() != d.view {
		return
	}
	d.viewChanged(0)
}

// viewResized keeps the anchor in place when the view changes size.
func (d *Document) viewResized() {
	size := d.scrollContainer.Size()
	if size == d.view {
		return
	}
	anchor, wasShown := d.anchor, !d.view.IsZero()
	d.relayout()
	if offset, ok := d.anchorOffset(anchor); ok && wasShown {
		d.setOffset(offset)
	}
	d.viewChanged(0)
}

// viewChanged updates everything that depends on the part of the pages
// in view: the page objects, the current page in continuous mode, the
// anchor, and the renders, which start after delay.
func (d *Document) viewChanged(delay time.Duration) {
	d.updateViews()
	if d.viewMode == ViewContinuous && !d.navigating {
		view := d.scrollContainer.Size()
		centre := d.scrollContainer.Offset.Y + view.Height/2
		if page := d.layout.pageAt(centre, d.areaSize()); page >= 0 {
			d.setCurrent(page)
		}
	}
	d.anchor = d.anchorAt(fyne.Position{})
	d.view = d.scrollContainer.Size()
	d.scheduleRender(delay)
}

// visiblePages returns the pages in view, nearest to its centre first, and
// the pages to keep objects for: those and one more above and below.
func (d *Document) visiblePages() (visible, live []int) {
	l := &d.layout
	if len(l.tops) == 0 {
		return nil, nil
	}
	view := d.scrollContainer.Size()
	area := d.areaSize()
	top := d.scrollContainer.Offset.Y
	from, to := l.pagesIn(top, top+view.Height, area)
	if to < from {
		// The view has no size yet.
		from = l.pageAt(top, area)
		to = from
	}

	centre := top + view.Height/2
	distance := func(page int) float32 {
		origin := l.origin(page, area)
		return abs(origin.Y + l.pageSize(page).Height/2 - centre)
	}
	for page := from; page <= to; page++ {
		visible = append(visible, page)
	}
	sort.SliceStable(visible, func(i, j int) bool {
		return distance(visible[i]) < distance(visible[j])
	})

	for page := max(from-1, l.first); page <= min(to+1, l.first+len(l.tops)-1); page++ {
		live = append(live, page)
	}
	return visible, live
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// updateViews gives the pages in or next to the view an object and drops
// the objects of the others, for reuse.
func (d *Document) updateViews() {
	_, live := d.visiblePages()
	for page, view := range d.views {
		if !slices.Contains(live, page) {
			delete(d.views, page)
			view.image.Image = emptyImage
			d.spareViews = append(d.spareViews, view)
		}
	}
	for _, page := range live {
		view, ok := d.views[page]
		if !ok {
			view = d.newView()
			d.views[page] = view
		}
		view.Scale = d.scale
		view.pageSize = d.pageSizes[page]
	}
	d.showImages()
	d.area.Refresh()
}

// newView returns a spare page object or a new one.
func (d *Document) newView() *Page {
	if n := len(d.spareViews); n > 0 {
		view := d.spareViews[n-1]
		d.spareViews = d.spareViews[:n-1]
		return view
	}
	view := NewPage()
	// Shown until the page or its thumbnail is rendered.
	view.BackgroundColor = color.White
	return view
}

// clearViews drops the page objects of a previous document.
func (d *Document) clearViews() {
	for page, view := range d.views {
		view.SetImage(emptyImage)
		d.spareViews = append(d.spareViews, view)
		delete(d.views, page)
	}
}
