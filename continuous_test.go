package pdf

import (
	"context"
	"image"
	"maps"
	"slices"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestPageLayout(t *testing.T) {
	sizes := []fyne.Size{{Width: 100, Height: 200}, {Width: 300, Height: 100}, {Width: 100, Height: 200}}
	l := newPageLayout(sizes, 0, 3, 2)

	// 200x400, 600x200 and 200x400, with a gap between them.
	if want := fyne.NewSize(600, 400+pageGap+200+pageGap+400); l.size != want {
		t.Fatalf("size = %v, want %v", l.size, want)
	}
	if o := l.origin(1, l.size); o != fyne.NewPos(0, 400+pageGap) {
		t.Errorf("origin of the widest page = %v", o)
	}
	if o := l.origin(2, l.size); o != fyne.NewPos(200, 600+2*pageGap) {
		t.Errorf("origin of the last page = %v, want it centred", o)
	}
	// A larger area centres the pages.
	area := fyne.NewSize(800, l.size.Height+100)
	if o := l.origin(0, area); o != fyne.NewPos(300, 50) {
		t.Errorf("origin in a larger area = %v, want 300,50", o)
	}

	for _, c := range []struct {
		y    float32
		want int
	}{
		{-10, 0}, {0, 0}, {399, 0},
		{400 + pageGap/2 - 1, 0}, {400 + pageGap/2 + 1, 1}, // in the gap
		{500, 1}, {700, 2}, {5000, 2},
	} {
		if got := l.pageAt(c.y, l.size); got != c.want {
			t.Errorf("pageAt(%v) = %d, want %d", c.y, got, c.want)
		}
	}

	for _, c := range []struct {
		y0, y1   float32
		from, to int
	}{
		{0, 100, 0, 0},
		{300, 650, 0, 2},
		{410, 500, 1, 1},
		{401, 407, 1, 0}, // only the gap
	} {
		if from, to := l.pagesIn(c.y0, c.y1, l.size); from != c.from || to != c.to {
			t.Errorf("pagesIn(%v, %v) = %d..%d, want %d..%d", c.y0, c.y1, from, to, c.from, c.to)
		}
	}

	// Single page mode lays out one page.
	single := newPageLayout(sizes, 1, 2, 1)
	if single.contains(0) || !single.contains(1) || single.contains(2) {
		t.Error("single page layout should contain page 1 only")
	}
	if single.size != sizes[1] {
		t.Errorf("single page size = %v, want %v", single.size, sizes[1])
	}
	if p := single.pageAt(0, single.size); p != 1 {
		t.Errorf("single page pageAt = %d, want 1", p)
	}

	empty := newPageLayout(nil, 0, 0, 1)
	if p := empty.pageAt(0, fyne.NewSize(100, 100)); p != -1 {
		t.Errorf("pageAt without pages = %d, want -1", p)
	}
}

// newContinuousDocument shows contents in continuous mode in an 800x600
// test window.
func newContinuousDocument(t *testing.T, contents []byte) *Document {
	t.Helper()
	d := newTestDocument(t)
	d.SetViewMode(ViewContinuous)
	loadAndWait(t, d, contents)
	d.modifiers = holding(0)
	return d
}

// pageTop returns the scroll offset that shows the top of page.
func pageTop(d *Document, page int) float32 {
	return d.layout.origin(page, d.areaSize()).Y
}

func viewedPages(d *Document) []int {
	return slices.Sorted(maps.Keys(d.views))
}

func TestContinuousLayout(t *testing.T) {
	d := newContinuousDocument(t, sizedPagesPDF([2]int{612, 792}, [2]int{1224, 792}, [2]int{300, 400}))
	zoomTop(d, 1)

	want := fyne.NewSize(1224*unitsPerPoint, (792+792+400)*unitsPerPoint+2*pageGap)
	if s := d.area.MinSize(); !approx(float64(s.Width), float64(want.Width)) || !approx(float64(s.Height), float64(want.Height)) {
		t.Fatalf("content size = %v, want %v: pages one below the other, widest page", s, want)
	}
	if !d.fitsHorizontally() {
		// The content is wider than the view, the narrow pages are
		// centred.
		if x := d.layout.origin(0, d.areaSize()).X; !approx(float64(x), 612*unitsPerPoint/2) {
			t.Errorf("first page x = %v, want centred at %v", x, 612*unitsPerPoint/2)
		}
	}

	// Fit width applies to the current page.
	d.ShowPage(1)
	d.ZoomToFitHorizontal()
	settle(d)
	if z, want := zoomOf(t, d), float64(d.scrollContainer.Size().Width/(1224*unitsPerPoint)); !approx(z, want) {
		t.Errorf("zoom after fit width on the wide page = %v, want %v", z, want)
	}
}

func TestContinuousVirtualised(t *testing.T) {
	d := newContinuousDocument(t, multiPagePDF(60))
	zoomTop(d, 1)

	pageHeight := float32(792 * unitsPerPoint)
	if h, want := d.area.MinSize().Height, 60*pageHeight+59*pageGap; !approx(float64(h), float64(want)) {
		t.Fatalf("content height = %v, want %v", h, want)
	}

	// Only the pages in view and their neighbours get objects.
	if got := viewedPages(d); !slices.Equal(got, []int{0, 1}) {
		t.Errorf("pages with objects at the top = %v, want [0 1]", got)
	}
	d.scrollTo(fyne.NewPos(0, pageTop(d, 30)+10))
	settle(d)
	if got := viewedPages(d); !slices.Equal(got, []int{29, 30, 31}) {
		t.Errorf("pages with objects in the middle = %v, want [29 30 31]", got)
	}
	if n := len(test.WidgetRenderer(d.area).Objects()); n != 4 {
		t.Errorf("canvas objects = %d, want background and 3 pages", n)
	}
	view := d.views[30]
	if pos := view.Position(); !approx(float64(pos.Y), float64(pageTop(d, 30))) {
		t.Errorf("page 30 at y %v, want %v", pos.Y, pageTop(d, 30))
	}
	if imageWidth(view) != 816 {
		t.Errorf("page 30 rendered %d px wide, want 816", imageWidth(view))
	}
	// Objects are reused.
	if len(d.views)+len(d.spareViews) > 5 {
		t.Errorf("%d page objects created", len(d.views)+len(d.spareViews))
	}
}

func TestContinuousCurrentPageFollowsScroll(t *testing.T) {
	d := newContinuousDocument(t, multiPagePDF(10))
	zoomTop(d, 1)

	view := d.scrollContainer.Size()
	pageHeight := d.layout.pageSize(7).Height
	d.scrollTo(fyne.NewPos(0, pageTop(d, 7)+pageHeight/2-view.Height/2))
	settle(d)
	if p := d.CurrentPage(); p != 7 {
		t.Fatalf("CurrentPage = %d, want 7 at the centre of the view", p)
	}
	if !d.thumbnails[7].Selected() || d.thumbnails[0].Selected() {
		t.Error("thumbnail selection does not follow the view")
	}

	// Wheel scrolling crosses page boundaries without turning pages.
	d.scrollTo(fyne.NewPos(0, pageTop(d, 8)-view.Height))
	for range 4 {
		d.area.Scrolled(wheel(-wheelNotch, fyne.Position{}))
	}
	settle(d)
	if y, want := d.scrollContainer.Offset.Y, pageTop(d, 8)-view.Height+4*wheelNotch; !approx(float64(y), float64(want)) {
		t.Errorf("offset after scrolling = %v, want %v", y, want)
	}
}

func TestContinuousShowPage(t *testing.T) {
	d := newContinuousDocument(t, multiPagePDF(20))
	zoomTop(d, 1)

	d.ShowPage(12)
	settle(d)
	if p := d.CurrentPage(); p != 12 {
		t.Fatalf("CurrentPage = %d, want 12", p)
	}
	if y := d.scrollContainer.Offset.Y; !approx(float64(y), float64(pageTop(d, 12))) {
		t.Errorf("offset = %v, want the top of page 12 at %v", y, pageTop(d, 12))
	}
	if z := zoomOf(t, d); z != 1 {
		t.Errorf("zoom = %v, want it kept at 1", z)
	}

	test.Tap(d.thumbnails[3])
	settle(d)
	if p, y := d.CurrentPage(), d.scrollContainer.Offset.Y; p != 3 || !approx(float64(y), float64(pageTop(d, 3))) {
		t.Errorf("after tapping thumbnail 3: page %d at %v, want 3 at %v", p, y, pageTop(d, 3))
	}

	// The last page cannot scroll to the top of the view, but is current.
	d.ShowPage(19)
	settle(d)
	if p := d.CurrentPage(); p != 19 {
		t.Errorf("CurrentPage = %d, want 19", p)
	}
}

func TestContinuousKeys(t *testing.T) {
	d := newContinuousDocument(t, multiPagePDF(5))
	zoomTop(d, 1)
	key := func(name fyne.KeyName) {
		t.Helper()
		d.area.TypedKey(&fyne.KeyEvent{Name: name})
		settle(d)
	}
	screen := d.scrollContainer.Size().Height * (1 - pageScrollOverlap)
	bottom := d.area.MinSize().Height - d.scrollContainer.Size().Height

	// Page Down scrolls by a screen across page boundaries.
	var want float32
	for range 6 {
		key(fyne.KeyPageDown)
		want = min(want+screen, bottom)
		if y := d.scrollContainer.Offset.Y; !approx(float64(y), float64(want)) {
			t.Fatalf("offset after Page Down = %v, want %v", y, want)
		}
	}
	if p := d.CurrentPage(); p == 0 {
		t.Error("current page did not follow Page Down")
	}

	key(fyne.KeyEnd)
	if p, y := d.CurrentPage(), d.scrollContainer.Offset.Y; p != 4 || !approx(float64(y), float64(pageTop(d, 4))) {
		t.Errorf("after End: page %d at %v, want 4 at %v", p, y, pageTop(d, 4))
	}
	key(fyne.KeyHome)
	if p, y := d.CurrentPage(), d.scrollContainer.Offset.Y; p != 0 || y != 0 {
		t.Errorf("after Home: page %d at %v, want 0 at 0", p, y)
	}

	// Left/Right go to the previous/next page top while the pages fit.
	d.ZoomToFit()
	settle(d)
	key(fyne.KeyRight)
	key(fyne.KeyRight)
	if p, y := d.CurrentPage(), d.scrollContainer.Offset.Y; p != 2 || !approx(float64(y), float64(pageTop(d, 2))) {
		t.Errorf("after Right Right: page %d at %v, want 2 at %v", p, y, pageTop(d, 2))
	}
	key(fyne.KeyLeft)
	if p := d.CurrentPage(); p != 1 {
		t.Errorf("after Left: page %d, want 1", p)
	}
	// Shift+Space at the top of the document stays there.
	key(fyne.KeyHome)
	d.modifiers = holding(fyne.KeyModifierShift)
	key(fyne.KeySpace)
	if p, y := d.CurrentPage(), d.scrollContainer.Offset.Y; p != 0 || y != 0 {
		t.Errorf("after Shift+Space at the top: page %d at %v", p, y)
	}
}

// pointAt returns the page and page point at pos in the view.
func pointAt(d *Document, pos fyne.Position) (int, fyne.Position) {
	a := d.anchorAt(pos)
	return a.page, a.point
}

func samePoint(a, b fyne.Position) bool {
	return approx(float64(a.X), float64(b.X)) && approx(float64(a.Y), float64(b.Y))
}

func TestContinuousAnchor(t *testing.T) {
	d := newContinuousDocument(t, multiPagePDF(10))
	w := fyne.CurrentApp().Driver().AllWindows()[0]
	zoomTop(d, 1)
	d.scrollTo(fyne.NewPos(0, pageTop(d, 4)+300))
	settle(d)

	// Zooming keeps the point at the centre of the view.
	view := d.scrollContainer.Size()
	centre := fyne.NewPos(view.Width/2, view.Height/2)
	page, point := pointAt(d, centre)
	d.Zoom(2)
	settle(d)
	if p, pt := pointAt(d, centre); p != page || !samePoint(pt, point) {
		t.Errorf("point at the centre moved from %d %v to %d %v when zooming", page, point, p, pt)
	}

	// Ctrl+wheel keeps the point under the pointer.
	d.modifiers = holding(fyne.KeyModifierShortcutDefault)
	at := fyne.NewPos(200, 100)
	page, point = pointAt(d, at)
	d.area.Scrolled(wheel(-wheelNotch, d.scrollContainer.Offset.Add(at)))
	settle(d)
	if p, pt := pointAt(d, at); p != page || !samePoint(pt, point) {
		t.Errorf("point under the pointer moved from %d %v to %d %v", page, point, p, pt)
	}
	d.modifiers = holding(0)

	// Resizing keeps the point at the top left.
	page, point = pointAt(d, fyne.Position{})
	w.Resize(fyne.NewSize(700, 500))
	settle(d)
	if p, pt := pointAt(d, fyne.Position{}); p != page || !samePoint(pt, point) {
		t.Errorf("point at the top left moved from %d %v to %d %v when resizing", page, point, p, pt)
	}

	// So does showing and hiding the thumbnails.
	test.Tap(d.toggleThumbnailsButton)
	settle(d)
	if p, pt := pointAt(d, fyne.Position{}); p != page || !samePoint(pt, point) {
		t.Errorf("point at the top left moved from %d %v to %d %v when hiding the thumbnails", page, point, p, pt)
	}
	test.Tap(d.toggleThumbnailsButton)
	settle(d)
	if p, pt := pointAt(d, fyne.Position{}); p != page || !samePoint(pt, point) {
		t.Errorf("point at the top left moved from %d %v to %d %v when showing the thumbnails", page, point, p, pt)
	}
}

func TestContinuousFitFollowsResize(t *testing.T) {
	d := newContinuousDocument(t, multiPagePDF(5))
	w := fyne.CurrentApp().Driver().AllWindows()[0]

	d.ShowPage(2)
	d.ZoomToFit()
	settle(d)
	if y := d.scrollContainer.Offset.Y; !approx(float64(y), float64(pageTop(d, 2))) {
		t.Fatalf("offset after fit = %v, want the top of page 2", y)
	}
	w.Resize(fyne.NewSize(900, 800))
	settle(d)
	if z := zoomOf(t, d); !approx(z, fittedZoom(d)) {
		t.Errorf("zoom after resize = %v, want fit %v", z, fittedZoom(d))
	}
	if p := d.CurrentPage(); p != 2 {
		t.Errorf("CurrentPage after resize = %d, want 2", p)
	}
}

func TestViewModeSwitch(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(4))
	if d.ViewMode() != ViewSinglePage {
		t.Fatal("default view mode should be single page")
	}
	d.ShowPage(2)
	zoomTop(d, 1)
	d.scrollTo(fyne.NewPos(0, 100))
	settle(d)

	test.Tap(d.viewModeButton)
	settle(d)
	if d.ViewMode() != ViewContinuous {
		t.Fatal("view mode button did not switch to continuous")
	}
	if p, z := d.CurrentPage(), zoomOf(t, d); p != 2 || z != 1 {
		t.Errorf("after switching: page %d, zoom %v, want 2 and 1", p, z)
	}
	if y := d.scrollContainer.Offset.Y; !approx(float64(y), float64(pageTop(d, 2)+100)) {
		t.Errorf("offset after switching = %v, want %v", y, pageTop(d, 2)+100)
	}
	if got := viewedPages(d); !slices.Equal(got, []int{1, 2, 3}) {
		t.Errorf("pages with objects = %v, want [1 2 3]", got)
	}

	d.SetViewMode(ViewSinglePage)
	settle(d)
	if p, y := d.CurrentPage(), d.scrollContainer.Offset.Y; p != 2 || y != 100 {
		t.Errorf("back in single page mode: page %d at %v, want 2 at 100", p, y)
	}
	if got := viewedPages(d); !slices.Equal(got, []int{2}) {
		t.Errorf("pages with objects = %v, want [2]", got)
	}
}

func TestContinuousRenderBudget(t *testing.T) {
	d := newContinuousDocument(t, multiPagePDF(8))
	zoomTop(d, maxZoom)

	for page := range 8 {
		d.ShowPage(page)
		settle(d)
		d.mutex.Lock()
		px := d.renderedPixels
		visible := d.rendered[page]
		d.mutex.Unlock()
		if px > renderBudgetPixels {
			t.Fatalf("page %d: %d pixels kept, budget %d", page, px, renderBudgetPixels)
		}
		if visible == nil {
			t.Fatalf("page %d in view not rendered", page)
		}
	}
	// Pages far from the view have been dropped.
	d.mutex.Lock()
	_, first := d.rendered[0]
	d.mutex.Unlock()
	if first {
		t.Error("image of page 0 kept while at page 7")
	}
}

func TestPlanRenders(t *testing.T) {
	bounds := make([]image.Rectangle, 10)
	for i := range bounds {
		bounds[i] = image.Rect(0, 0, 612, 792)
	}
	pixels := func(jobs []renderJob) float64 {
		var total float64
		for _, job := range jobs {
			total += pagePixels(612*792, job.dpi)
		}
		return total
	}

	// Pages in view first, in their order, then the neighbours.
	jobs := planRenders([]int{4, 3, 5}, []int{2, 3, 4, 5, 6}, bounds, 96)
	if got := []int{jobs[0].page, jobs[1].page, jobs[2].page, jobs[3].page, jobs[4].page}; !slices.Equal(got, []int{4, 3, 5, 2, 6}) {
		t.Errorf("render order = %v", got)
	}
	for _, job := range jobs {
		if job.dpi != 96 {
			t.Errorf("page %d at %v dpi, want 96", job.page, job.dpi)
		}
	}

	// A single page is capped at maxRenderPixels.
	jobs = planRenders([]int{0}, []int{0}, bounds, 2000)
	if px := pixels(jobs); px > maxRenderPixels*1.001 {
		t.Errorf("single page needs %v pixels, cap %d", px, maxRenderPixels)
	}

	// The pages in view share the budget; neighbours that do not fit are
	// left out.
	jobs = planRenders([]int{2, 3, 4}, []int{1, 2, 3, 4, 5}, bounds, 2000)
	if len(jobs) != 3 {
		t.Errorf("%d renders, want the 3 pages in view only", len(jobs))
	}
	if px := pixels(jobs); px > renderBudgetPixels*1.001 {
		t.Errorf("pages in view need %v pixels, budget %d", px, renderBudgetPixels)
	}
}

func TestRenderCacheEvictsLeastRecentlyUsed(t *testing.T) {
	d := newDocument(t)
	img := image.NewGray(image.Rect(0, 0, 4000, 4000)) // 16 M pixels

	d.mutex.Lock()
	defer d.mutex.Unlock()
	d.jobs = []renderJob{{page: 3, dpi: 1}}
	d.storeLocked(1, img, 1)
	d.storeLocked(2, img, 1)
	d.storeLocked(3, img, 1)
	// Over budget: page 1 is the oldest not wanted.
	d.storeLocked(4, img, 1)
	if _, ok := d.rendered[1]; ok {
		t.Error("page 1 kept, want it dropped first")
	}
	for _, page := range []int{2, 3, 4} {
		if _, ok := d.rendered[page]; !ok {
			t.Errorf("page %d dropped", page)
		}
	}
	if d.renderedPixels != 3*16_000_000 {
		t.Errorf("renderedPixels = %d", d.renderedPixels)
	}
}

func TestRenderCancelledWhenNoLongerWanted(t *testing.T) {
	d := newDocument(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d.mutex.Lock()
	d.ctx = ctx
	d.rendering = true
	cancelled := false
	d.inflight = renderJob{page: 5, dpi: 100}
	d.inflightCancel = func() { cancelled = true }
	d.mutex.Unlock()

	// Still wanted at that resolution: it goes on.
	if d.startRenders(ctx, []renderJob{{page: 5, dpi: 90}}) || cancelled {
		t.Fatal("render of a wanted page cancelled")
	}
	// Scrolled away.
	if d.startRenders(ctx, []renderJob{{page: 9, dpi: 100}}) || !cancelled {
		t.Error("render of a page out of view not cancelled")
	}
	// Zoomed in: the resolution is too low now.
	cancelled = false
	if d.startRenders(ctx, []renderJob{{page: 5, dpi: 200}}) || !cancelled {
		t.Error("render at a resolution too low not cancelled")
	}
}

func TestContinuousStartsAtTheTop(t *testing.T) {
	d := newDocument(t)
	d.SetViewMode(ViewContinuous)
	w := test.NewTempWindow(t, d)

	// The first layout of a real window can be tiny; growing to the real
	// size must not scroll away from the first page.
	w.Resize(fyne.NewSize(400, 120))
	loadAndWait(t, d, multiPagePDF(10))
	w.Resize(fyne.NewSize(1024, 768))
	settle(d)

	if p, y := d.CurrentPage(), d.scrollContainer.Offset.Y; p != 0 || y != 0 {
		t.Errorf("page %d at %v, want 0 at the top", p, y)
	}
	if z := zoomOf(t, d); !approx(z, fittedZoom(d)) {
		t.Errorf("zoom = %v, want fit %v", z, fittedZoom(d))
	}
}
