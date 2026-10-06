package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"math"
	"strings"
	"sync"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// multiPagePDF builds a PDF with n pages of 612x792 pt, each with a red
// rectangle.
func multiPagePDF(n int) []byte {
	content := "1 0 0 rg 100 100 200 200 re f"
	kids := make([]string, n)
	for i := range n {
		kids[i] = fmt.Sprintf("%d 0 R", 4+i)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	for range n {
		objects = append(objects, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 3 0 R >>")
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// mainQueue collects the UI updates of background renders. The test driver
// runs fyne.Do on the calling goroutine, so the tests apply them on their own
// goroutine instead, like the real driver does on the main goroutine.
type mainQueue struct {
	mu    sync.Mutex
	funcs []func()
}

func (q *mainQueue) add(fn func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.funcs = append(q.funcs, fn)
}

func (q *mainQueue) drain() {
	q.mu.Lock()
	funcs := q.funcs
	q.funcs = nil
	q.mu.Unlock()
	for _, fn := range funcs {
		fn()
	}
}

// newDocument creates a Document whose background results are applied by
// settle.
func newDocument(t *testing.T) *Document {
	t.Helper()
	test.NewTempApp(t)
	q := &mainQueue{}
	d := NewDocument()
	d.runOnMain = q.add
	queues[d] = q
	t.Cleanup(func() {
		_ = d.Close()
		settle(d)
		delete(queues, d)
	})
	return d
}

var queues = map[*Document]*mainQueue{}

// settle waits for all scheduled renders and applies their results.
func settle(d *Document) {
	d.waitRendered()
	queues[d].drain()
}

// newTestDocument shows an empty Document in an 800x600 test window.
func newTestDocument(t *testing.T) *Document {
	t.Helper()
	d := newDocument(t)
	w := test.NewTempWindow(t, d)
	w.Resize(fyne.NewSize(800, 600))
	return d
}

func loadAndWait(t *testing.T, d *Document, contents []byte) {
	t.Helper()
	if err := d.LoadFromMemory(contents); err != nil {
		t.Fatal(err)
	}
	settle(d)
}

func zoomOf(t *testing.T, d *Document) float64 {
	t.Helper()
	z, err := d.zoom.Get()
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func approx(a, b float64) bool {
	return math.Abs(a-b) < 1e-3
}

func imageWidth(p *Page) int {
	return p.image.Image.Bounds().Dx()
}

func TestDocumentLoad(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(3))

	if n := d.PageCount(); n != 3 {
		t.Fatalf("PageCount = %d, want 3", n)
	}
	if p := d.CurrentPage(); p != 0 {
		t.Fatalf("CurrentPage = %d, want 0", p)
	}
	if len(d.thumbnails) != 3 || len(d.thumbnailContainer.Objects) != 3 {
		t.Fatalf("thumbnails = %d/%d, want 3", len(d.thumbnails), len(d.thumbnailContainer.Objects))
	}
	for i, th := range d.thumbnails {
		if want := fmt.Sprint(i + 1); th.label.Text != want {
			t.Errorf("thumbnail %d title = %q, want %q", i, th.label.Text, want)
		}
		if th.Selected() != (i == 0) {
			t.Errorf("thumbnail %d selected = %v", i, th.Selected())
		}
		if imageWidth(th.page) == 0 {
			t.Errorf("thumbnail %d not rendered", i)
		}
	}
	if !d.thumbnailScroller.Visible() || !d.toggleThumbnailsButton.Visible() {
		t.Error("thumbnails should be visible for multi-page documents")
	}

	// The page is fitted into the view and rendered for the resulting zoom.
	view := d.scrollContainer.Size()
	page := d.bigPage.PageSize()
	want := math.Min(float64(view.Width/page.Width), float64(view.Height/page.Height))
	if z := zoomOf(t, d); !approx(z, want) {
		t.Errorf("zoom = %v, want %v (fit)", z, want)
	}
	if w, want := imageWidth(d.bigPage), int(math.Ceil(612*96*want/72)); w < want-1 || w > want+1 {
		t.Errorf("page image width = %d, want %d", w, want)
	}
}

func TestDocumentReload(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(3))
	loadAndWait(t, d, multiPagePDF(1))

	if n := d.PageCount(); n != 1 {
		t.Fatalf("PageCount = %d, want 1", n)
	}
	if len(d.thumbnails) != 1 || len(d.thumbnailContainer.Objects) != 1 {
		t.Fatalf("thumbnails after reload = %d/%d, want 1", len(d.thumbnails), len(d.thumbnailContainer.Objects))
	}
	if d.thumbnailScroller.Visible() || d.toggleThumbnailsButton.Visible() {
		t.Error("thumbnails should be hidden for single-page documents")
	}
}

func TestDocumentLoadInvalid(t *testing.T) {
	d := newTestDocument(t)
	if err := d.LoadFromMemory(nil); err == nil {
		t.Fatal("expected error for empty contents")
	}
	if err := d.LoadFromMemory([]byte("no pdf")); err == nil {
		t.Fatal("expected error for invalid contents")
	}
	if n := d.PageCount(); n != 0 {
		t.Fatalf("PageCount = %d, want 0", n)
	}
}

func TestDocumentShowPage(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(3))

	test.Tap(d.thumbnails[2])
	settle(d)
	if p := d.CurrentPage(); p != 2 {
		t.Fatalf("CurrentPage = %d, want 2", p)
	}
	if !d.thumbnails[2].Selected() || d.thumbnails[0].Selected() {
		t.Error("selection did not follow the shown page")
	}
	if imageWidth(d.bigPage) == 0 {
		t.Error("page 2 not rendered")
	}

	// Out-of-range pages are ignored.
	d.ShowPage(3)
	d.ShowPage(-1)
	if p := d.CurrentPage(); p != 2 {
		t.Fatalf("CurrentPage = %d, want 2", p)
	}
}

func TestDocumentZoom(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(1))

	d.Zoom(2)
	settle(d)
	if d.bigPage.Scale != 2 {
		t.Fatalf("page scale = %v, want 2", d.bigPage.Scale)
	}
	if min := d.bigPage.MinSize(); !approx(float64(min.Width), 612*96.0/72*2) {
		t.Errorf("page min width = %v, want %v", min.Width, 612*96.0/72*2)
	}
	// 192 dpi
	if w := imageWidth(d.bigPage); w != 1632 {
		t.Errorf("page image width = %d, want 1632", w)
	}

	// Zooming out a little keeps the sharper image.
	d.Zoom(1.8)
	settle(d)
	if w := imageWidth(d.bigPage); w != 1632 {
		t.Errorf("page image width = %d, want 1632 (not re-rendered)", w)
	}

	d.Zoom(100)
	if z := zoomOf(t, d); z != maxZoom {
		t.Errorf("zoom = %v, want clamped to %v", z, maxZoom)
	}
	if !d.zoomInButton.Disabled() {
		t.Error("zoom in should be disabled at max zoom")
	}
	d.Zoom(0)
	if z := zoomOf(t, d); z != minZoom {
		t.Errorf("zoom = %v, want clamped to %v", z, minZoom)
	}
	if !d.zoomOutButton.Disabled() {
		t.Error("zoom out should be disabled at min zoom")
	}
	settle(d)
}

func TestDocumentRenderSizeCapped(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(1))

	d.Zoom(maxZoom)
	settle(d)
	b := d.bigPage.image.Image.Bounds()
	if px := b.Dx() * b.Dy(); px > maxRenderPixels*1.01 {
		t.Errorf("rendered %d pixels, want at most %d", px, maxRenderPixels)
	}
}

func TestDocumentToolbar(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(2))

	view := d.scrollContainer.Size()
	page := d.bigPage.PageSize()

	d.Zoom(1)
	test.Tap(d.zoomInButton)
	if z := zoomOf(t, d); !approx(z, 1.25) {
		t.Errorf("zoom after zoom in = %v, want 1.25", z)
	}
	test.Tap(d.zoomOutButton)
	test.Tap(d.zoomOutButton)
	if z := zoomOf(t, d); !approx(z, 0.75) {
		t.Errorf("zoom after zoom out = %v, want 0.75", z)
	}

	buttons := d.toolbar.Objects
	tapButton := func(label string, i int) {
		t.Helper()
		b, ok := buttons[i].(interface{ Tapped(*fyne.PointEvent) })
		if !ok {
			t.Fatalf("%s: toolbar item %d is not tappable", label, i)
		}
		b.Tapped(&fyne.PointEvent{})
	}

	tapButton("100%", 5)
	if z := zoomOf(t, d); z != 1 {
		t.Errorf("zoom after 100%% = %v, want 1", z)
	}
	tapButton("fit width", 8)
	if z := zoomOf(t, d); !approx(z, float64(view.Width/page.Width)) {
		t.Errorf("zoom after fit width = %v, want %v", z, view.Width/page.Width)
	}
	tapButton("fit height", 7)
	if z := zoomOf(t, d); !approx(z, float64(view.Height/page.Height)) {
		t.Errorf("zoom after fit height = %v, want %v", z, view.Height/page.Height)
	}
	tapButton("fit", 6)
	if z := zoomOf(t, d); !approx(z, math.Min(float64(view.Width/page.Width), float64(view.Height/page.Height))) {
		t.Errorf("zoom after fit = %v", z)
	}

	test.Tap(d.toggleThumbnailsButton)
	if d.thumbnailScroller.Visible() {
		t.Error("thumbnails should be hidden after toggle")
	}
	test.Tap(d.toggleThumbnailsButton)
	if !d.thumbnailScroller.Visible() {
		t.Error("thumbnails should be visible after second toggle")
	}
	settle(d)
}

func TestDocumentZoomSliderWidth(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(1))

	// In the toolbar's HBox the slider used to shrink to its thumb.
	if w := d.zoomSlider.Size().Width; w < zoomSliderWidth {
		t.Errorf("zoom slider width = %v, want at least %v", w, zoomSliderWidth)
	}
}

func TestDocumentSaveCallback(t *testing.T) {
	d := newTestDocument(t)
	if d.saveButton.Visible() {
		t.Fatal("save button should be hidden without callback")
	}

	saved := false
	d.SaveCallback = func() { saved = true }
	d.Refresh()
	if !d.saveButton.Visible() {
		t.Fatal("save button should be visible with callback")
	}
	test.Tap(d.saveButton)
	if !saved {
		t.Error("save callback not called")
	}

	d.SaveCallback = nil
	d.Refresh()
	if d.saveButton.Visible() {
		t.Error("save button should be hidden after removing callback")
	}
}

func TestDocumentFitBeforeShown(t *testing.T) {
	d := newDocument(t)

	// Not in a window yet: fitting has to wait for a size.
	loadAndWait(t, d, multiPagePDF(1))
	if !d.fitPending {
		t.Fatal("fit should be pending without a size")
	}

	w := test.NewTempWindow(t, d)
	w.Resize(fyne.NewSize(800, 600))
	settle(d)
	if d.fitPending {
		t.Fatal("fit should be applied after resize")
	}
	if z := zoomOf(t, d); z == 1 {
		t.Error("zoom not fitted after resize")
	}
}

func TestDocumentClose(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(2))

	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal("second Close:", err)
	}
	if n := d.PageCount(); n != 0 {
		t.Fatalf("PageCount after Close = %d, want 0", n)
	}
	// Neither zooming nor switching pages renders anything after Close.
	d.Zoom(3)
	d.ShowPage(1)
	settle(d)
	if p := d.CurrentPage(); p != 0 {
		t.Fatalf("CurrentPage after Close = %d, want 0", p)
	}
}

func TestDocumentCloseWhileRendering(t *testing.T) {
	d := newTestDocument(t)
	if err := d.LoadFromMemory(multiPagePDF(20)); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	settle(d)
}

func TestDocumentReportError(t *testing.T) {
	d := newTestDocument(t)

	// Without OnError the error is logged.
	d.reportError(1, errors.New("logged"))

	var gotPage int
	var gotErr error
	d.OnError = func(page int, err error) {
		gotPage, gotErr = page, err
	}
	want := errors.New("boom")
	d.reportError(4, want)
	if gotPage != 4 || !errors.Is(gotErr, want) {
		t.Errorf("OnError got (%d, %v), want (4, %v)", gotPage, gotErr, want)
	}
}

func TestPage(t *testing.T) {
	test.NewTempApp(t)

	p := NewPageWithImage(image.NewRGBA(image.Rect(0, 0, 40, 20)))
	if s := p.PageSize(); s != fyne.NewSize(40, 20) {
		t.Fatalf("PageSize = %v, want image size 40x20", s)
	}

	p.SetPageSize(fyne.NewSize(100, 50))
	p.Scale = 2
	if s := p.MinSize(); s != fyne.NewSize(200, 100) {
		t.Fatalf("MinSize = %v, want 200x100", s)
	}

	r := test.WidgetRenderer(p)
	if len(r.Objects()) != 2 {
		t.Fatalf("renderer objects = %d, want 2", len(r.Objects()))
	}
	p.Resize(fyne.NewSize(300, 150))
	if s := p.image.Size(); s != fyne.NewSize(300, 150) {
		t.Errorf("image size = %v, want 300x150", s)
	}
	r.Destroy()
}

func TestThumbnail(t *testing.T) {
	test.NewTempApp(t)

	th := NewThumbnail()
	th.SetTitle("7")
	th.SetImage(image.NewRGBA(image.Rect(0, 0, 10, 10)))
	th.SetMinimumSize(fyne.NewSize(50, 60))

	min := th.MinSize()
	if min.Width != 50 || min.Height <= 60 {
		t.Fatalf("MinSize = %v, want width 50 and height > 60", min)
	}

	th.Resize(fyne.NewSize(80, min.Height))
	if pos := th.page.Position(); pos.X != 15 {
		t.Errorf("preview x = %v, want centered at 15", pos.X)
	}

	tapped := false
	th.OnTapped = func() { tapped = true }
	test.Tap(th)
	if !tapped {
		t.Error("OnTapped not called")
	}

	th.SetSelected(true)
	th.SetSelected(true)
	if !th.Selected() {
		t.Error("Selected = false after SetSelected(true)")
	}
	r := test.WidgetRenderer(th)
	if len(r.Objects()) != 3 {
		t.Fatalf("renderer objects = %d, want 3", len(r.Objects()))
	}
	r.Destroy()

	// A thumbnail without OnTapped must not panic.
	NewThumbnail().Tapped(nil)
}
