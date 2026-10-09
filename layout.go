package pdf

import (
	"sort"

	"fyne.io/fyne/v2"
)

// pageGap is the space between two pages in continuous mode, independent
// of the zoom.
const pageGap = 8

// pageLayout places the pages shown in the scroll content: one below the
// other, each at its own size times the zoom, centred horizontally. In
// single page mode it holds one page. While the content is smaller than
// the view, it is centred in the view.
type pageLayout struct {
	// first is the index of the first page shown.
	first int
	// tops and sizes are the offset from the top of the content and the
	// zoomed size of each page shown, starting with first.
	tops  []float32
	sizes []fyne.Size
	// size is the size the pages need, the MinSize of the scroll content.
	size fyne.Size
}

// newPageLayout lays out pages first to last-1 of pageSizes (at zoom 1)
// at zoom.
func newPageLayout(pageSizes []fyne.Size, first, last int, zoom float64) pageLayout {
	l := pageLayout{first: first}
	if first < 0 || last <= first {
		return l
	}
	n := last - first
	l.tops = make([]float32, n)
	l.sizes = make([]fyne.Size, n)
	var y float32
	for i := range n {
		if i > 0 {
			y += pageGap
		}
		s := pageSizes[first+i]
		size := fyne.NewSize(s.Width*float32(zoom), s.Height*float32(zoom))
		l.tops[i], l.sizes[i] = y, size
		y += size.Height
		l.size.Width = max(l.size.Width, size.Width)
	}
	l.size.Height = y
	return l
}

// contains reports whether page is shown.
func (l *pageLayout) contains(page int) bool {
	return page >= l.first && page < l.first+len(l.tops)
}

// pageSize returns the zoomed size of a page shown.
func (l *pageLayout) pageSize(page int) fyne.Size {
	return l.sizes[page-l.first]
}

// origin returns the position of a page shown within content of size
// area, which is at least l.size.
func (l *pageLayout) origin(page int, area fyne.Size) fyne.Position {
	i := page - l.first
	return fyne.NewPos(
		(area.Width-l.sizes[i].Width)/2,
		l.tops[i]+l.shift(area),
	)
}

// shift is the vertical offset that centres pages smaller than area.
func (l *pageLayout) shift(area fyne.Size) float32 {
	return max(0, (area.Height-l.size.Height)/2)
}

// pageAt returns the page at y in content of size area, or the nearest one
// when y is in a gap or outside the pages; -1 if no page is shown.
func (l *pageLayout) pageAt(y float32, area fyne.Size) int {
	n := len(l.tops)
	if n == 0 {
		return -1
	}
	y -= l.shift(area)
	// First page whose bottom is below y.
	i := sort.Search(n, func(i int) bool { return l.tops[i]+l.sizes[i].Height > y })
	if i == n {
		return l.first + n - 1
	}
	if i > 0 && y < l.tops[i] {
		// In the gap above page i.
		if l.tops[i]-y > y-(l.tops[i-1]+l.sizes[i-1].Height) {
			i--
		}
	}
	return l.first + i
}

// pagesIn returns the range of pages from to to (inclusive) that overlap
// the content from y0 to y1; to < from if there is none.
func (l *pageLayout) pagesIn(y0, y1 float32, area fyne.Size) (from, to int) {
	n := len(l.tops)
	shift := l.shift(area)
	y0, y1 = y0-shift, y1-shift
	first := sort.Search(n, func(i int) bool { return l.tops[i]+l.sizes[i].Height > y0 })
	last := sort.Search(n, func(i int) bool { return l.tops[i] >= y1 }) - 1
	return l.first + first, l.first + last
}

// viewAnchor is a point on a page, kept at a position in the view while
// the layout changes: zooming, resizing, switching the view mode.
type viewAnchor struct {
	page int
	// point is relative to the page's origin, at zoom 1.
	point fyne.Position
	// at is the position in the view.
	at fyne.Position
}

// areaSize is the size of the scroll content: the pages, or the view if
// that is larger.
func (d *Document) areaSize() fyne.Size {
	return d.layout.size.Max(d.scrollContainer.Size())
}

// anchorAt returns the anchor for the page at view position at, or the
// nearest page.
func (d *Document) anchorAt(at fyne.Position) viewAnchor {
	content := d.scrollContainer.Offset.Add(at)
	return d.anchorOnPage(d.layout.pageAt(content.Y, d.areaSize()), at)
}

// anchorOnPage returns the anchor for view position at relative to page,
// which may lie outside of it.
func (d *Document) anchorOnPage(page int, at fyne.Position) viewAnchor {
	a := viewAnchor{page: page, at: at}
	if !d.layout.contains(page) {
		return a
	}
	p := d.scrollContainer.Offset.Add(at).Subtract(d.layout.origin(page, d.areaSize()))
	a.point = fyne.NewPos(p.X/float32(d.scale), p.Y/float32(d.scale))
	return a
}

// anchorOffset returns the scroll offset that shows a at its view
// position, and false if its page is not shown.
func (d *Document) anchorOffset(a viewAnchor) (fyne.Position, bool) {
	if !d.layout.contains(a.page) {
		return fyne.Position{}, false
	}
	origin := d.layout.origin(a.page, d.areaSize())
	return origin.AddXY(a.point.X*float32(d.scale), a.point.Y*float32(d.scale)).Subtract(a.at), true
}
