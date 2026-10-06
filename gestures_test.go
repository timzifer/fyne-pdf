package pdf

import (
	"math"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

func holding(m fyne.KeyModifier) func() fyne.KeyModifier {
	return func() fyne.KeyModifier { return m }
}

func wheel(dy float32, pos fyne.Position) *fyne.ScrollEvent {
	ev := &fyne.ScrollEvent{Scrolled: fyne.NewDelta(0, dy)}
	ev.Position = pos
	return ev
}

// pagePointAt returns the page point (at zoom 1) shown at pos in the view.
func pagePointAt(d *Document, pos fyne.Position) fyne.Position {
	zoom := float32(d.bigPage.Scale)
	origin := pageOrigin(d.scrollContainer.Size(), d.bigPage.PageSize(), d.bigPage.Scale)
	p := d.scrollContainer.Offset.Add(pos).Subtract(origin)
	return fyne.NewPos(p.X/zoom, p.Y/zoom)
}

// newGestureDocument shows three pages at zoom 1, where a page is taller
// than the 800x600 window.
func newGestureDocument(t *testing.T) *Document {
	t.Helper()
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(3))
	d.Zoom(1)
	settle(d)
	return d
}

// newGesture starts a new scroll gesture, as after a pause.
func newGesture(d *Document) {
	d.area.lastScroll = time.Time{}
}

func bottomOffset(d *Document) float32 {
	return d.area.MinSize().Height - d.scrollContainer.Size().Height
}

func TestGestureCtrlWheelZoomsAroundCursor(t *testing.T) {
	d := newGestureDocument(t)
	d.modifiers = holding(fyne.KeyModifierShortcutDefault)
	d.scrollTo(fyne.NewPos(0, 200))

	anchor := fyne.NewPos(300, 250)
	before := pagePointAt(d, anchor)
	d.area.Scrolled(wheel(wheelNotch, d.scrollContainer.Offset.Add(anchor)))
	settle(d)

	if z := zoomOf(t, d); !approx(z, zoomWheelFactor) {
		t.Fatalf("zoom = %v, want %v", z, zoomWheelFactor)
	}
	after := pagePointAt(d, anchor)
	if math.Abs(float64(after.X-before.X)) > 1 || math.Abs(float64(after.Y-before.Y)) > 1 {
		t.Errorf("page point under cursor moved from %v to %v", before, after)
	}

	d.area.Scrolled(wheel(-2*wheelNotch, d.scrollContainer.Offset.Add(anchor)))
	settle(d)
	if z, want := zoomOf(t, d), zoomWheelFactor/(zoomWheelFactor*zoomWheelFactor); !approx(z, want) {
		t.Errorf("zoom after zooming out = %v, want %v", z, want)
	}
}

func TestGestureWheelScrolls(t *testing.T) {
	d := newGestureDocument(t)
	d.modifiers = holding(0)

	d.area.Scrolled(wheel(-wheelNotch, fyne.NewPos(100, 100)))
	if y := d.scrollContainer.Offset.Y; y != wheelNotch {
		t.Errorf("offset after scrolling = %v, want %v", y, wheelNotch)
	}
	if z := zoomOf(t, d); z != 1 {
		t.Errorf("zoom = %v, want 1", z)
	}
}

func TestGestureScrollTurnsPage(t *testing.T) {
	d := newGestureDocument(t)
	d.modifiers = holding(0)
	d.scrollTo(fyne.NewPos(0, bottomOffset(d)))

	// One notch past the bottom is not enough, the second one turns.
	d.area.Scrolled(wheel(-wheelNotch, fyne.Position{}))
	if p := d.CurrentPage(); p != 0 {
		t.Fatalf("page after one notch = %d, want 0", p)
	}
	d.area.Scrolled(wheel(-wheelNotch, fyne.Position{}))
	settle(d)
	if p := d.CurrentPage(); p != 1 {
		t.Fatalf("page after two notches = %d, want 1", p)
	}
	if y := d.scrollContainer.Offset.Y; y != 0 {
		t.Errorf("offset on the next page = %v, want its top", y)
	}
	if z := zoomOf(t, d); z != 1 {
		t.Errorf("zoom after turning = %v, want it kept at 1", z)
	}

	// The same gesture does not turn back at the top of the new page.
	d.area.Scrolled(wheel(3*wheelNotch, fyne.Position{}))
	if p := d.CurrentPage(); p != 1 {
		t.Fatalf("page after scrolling back in the same gesture = %d, want 1", p)
	}

	// A new gesture does, and shows the bottom of the previous page.
	newGesture(d)
	d.area.Scrolled(wheel(wheelNotch, fyne.Position{}))
	d.area.Scrolled(wheel(wheelNotch, fyne.Position{}))
	settle(d)
	if p := d.CurrentPage(); p != 0 {
		t.Fatalf("page after scrolling up = %d, want 0", p)
	}
	if y, want := d.scrollContainer.Offset.Y, bottomOffset(d); !approx(float64(y), float64(want)) {
		t.Errorf("offset on the previous page = %v, want its bottom %v", y, want)
	}

	// No page before the first one.
	newGesture(d)
	d.scrollTo(fyne.Position{})
	d.area.Scrolled(wheel(2*wheelNotch, fyne.Position{}))
	if p := d.CurrentPage(); p != 0 {
		t.Errorf("page = %d, want 0", p)
	}
}

func TestGestureScrollTurnsFittedPage(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(2))
	d.modifiers = holding(0)
	fit := zoomOf(t, d)

	d.area.Scrolled(wheel(-wheelNotch, fyne.Position{}))
	d.area.Scrolled(wheel(-wheelNotch, fyne.Position{}))
	settle(d)
	if p := d.CurrentPage(); p != 1 {
		t.Fatalf("page = %d, want 1", p)
	}
	if !d.fitHorizontal || !d.fitVertical || !approx(zoomOf(t, d), fit) {
		t.Errorf("fit not kept: zoom %v, want %v", zoomOf(t, d), fit)
	}
}

func TestGestureDragPans(t *testing.T) {
	d := newGestureDocument(t)
	d.Zoom(2)
	settle(d)

	d.area.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(-30, -40)})
	if o := d.scrollContainer.Offset; o != fyne.NewPos(30, 40) {
		t.Fatalf("offset after drag = %v, want 30,40", o)
	}
	d.area.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(100, 100)})
	if o := d.scrollContainer.Offset; o != (fyne.Position{}) {
		t.Errorf("offset after dragging past the edge = %v, want 0,0", o)
	}
	d.area.DragEnd()
}

func TestGestureKeys(t *testing.T) {
	d := newGestureDocument(t)
	d.modifiers = holding(0)
	key := func(name fyne.KeyName) {
		t.Helper()
		d.area.TypedKey(&fyne.KeyEvent{Name: name})
		settle(d)
	}
	screen := d.scrollContainer.Size().Height * (1 - pageScrollOverlap)

	key(fyne.KeyPageDown)
	if y := d.scrollContainer.Offset.Y; !approx(float64(y), float64(min(screen, bottomOffset(d)))) {
		t.Fatalf("offset after Page Down = %v, want %v", y, screen)
	}
	// Paging on reaches the bottom, then turns to the next page.
	for i := 0; d.CurrentPage() == 0; i++ {
		if i == 10 {
			t.Fatal("Page Down does not turn the page")
		}
		if d.scrollContainer.Offset.Y < bottomOffset(d) {
			key(fyne.KeyPageDown)
			continue
		}
		key(fyne.KeyPageDown)
		if p := d.CurrentPage(); p != 1 {
			t.Fatalf("page after Page Down at the bottom = %d, want 1", p)
		}
	}
	if y := d.scrollContainer.Offset.Y; y != 0 {
		t.Fatalf("offset on the next page = %v, want its top", y)
	}

	key(fyne.KeyDown)
	if y := d.scrollContainer.Offset.Y; y != arrowScrollStep {
		t.Errorf("offset after Down = %v, want %v", y, arrowScrollStep)
	}
	key(fyne.KeyUp)
	key(fyne.KeyPageUp)
	if p := d.CurrentPage(); p != 0 {
		t.Fatalf("page after Page Up at the top = %d, want 0", p)
	}

	key(fyne.KeyEnd)
	if p := d.CurrentPage(); p != 2 {
		t.Errorf("page after End = %d, want 2", p)
	}
	d.modifiers = holding(fyne.KeyModifierShift)
	key(fyne.KeySpace)
	if p := d.CurrentPage(); p != 1 {
		t.Errorf("page after Shift+Space at the top = %d, want 1", p)
	}
	d.modifiers = holding(0)
	key(fyne.KeyHome)
	if p := d.CurrentPage(); p != 0 {
		t.Errorf("page after Home = %d, want 0", p)
	}

	// Left/Right turn pages while the page fits horizontally ...
	d.ZoomToFit()
	settle(d)
	key(fyne.KeyRight)
	if p := d.CurrentPage(); p != 1 {
		t.Errorf("page after Right = %d, want 1", p)
	}
	key(fyne.KeyLeft)
	if p := d.CurrentPage(); p != 0 {
		t.Errorf("page after Left = %d, want 0", p)
	}
	// ... and scroll otherwise.
	d.Zoom(2)
	settle(d)
	key(fyne.KeyRight)
	if p, x := d.CurrentPage(), d.scrollContainer.Offset.X; p != 0 || x != arrowScrollStep {
		t.Errorf("after Right at zoom 2: page %d, offset %v, want page 0, offset %v", p, x, arrowScrollStep)
	}
}

func TestGestureZoomShortcuts(t *testing.T) {
	d := newGestureDocument(t)
	shortcut := func(name fyne.KeyName, mod fyne.KeyModifier) {
		d.area.TypedShortcut(&desktop.CustomShortcut{KeyName: name, Modifier: mod})
		settle(d)
	}

	shortcut(fyne.KeyEqual, fyne.KeyModifierShortcutDefault)
	if z := zoomOf(t, d); !approx(z, zoomKeyFactor) {
		t.Errorf("zoom after Ctrl+= = %v, want %v", z, zoomKeyFactor)
	}
	shortcut(fyne.KeyMinus, fyne.KeyModifierShortcutDefault)
	shortcut(fyne.KeyMinus, fyne.KeyModifierShortcutDefault)
	if z := zoomOf(t, d); !approx(z, 1/zoomKeyFactor) {
		t.Errorf("zoom after Ctrl+- = %v, want %v", z, 1/zoomKeyFactor)
	}
	shortcut(fyne.KeyPlus, fyne.KeyModifierShortcutDefault|fyne.KeyModifierShift)
	shortcut(fyne.Key0, fyne.KeyModifierShortcutDefault)
	if z := zoomOf(t, d); z != 1 {
		t.Errorf("zoom after Ctrl+0 = %v, want 1", z)
	}
	// Other modifiers and shortcuts are ignored.
	shortcut(fyne.KeyEqual, fyne.KeyModifierAlt)
	d.area.TypedShortcut(&fyne.ShortcutCopy{})
	if z := zoomOf(t, d); z != 1 {
		t.Errorf("zoom = %v, want 1", z)
	}
}

func TestGestureDoubleTap(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(1))
	fit := zoomOf(t, d)

	d.area.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(400, 300)})
	settle(d)
	if z := zoomOf(t, d); z != 1 {
		t.Fatalf("zoom after double tap = %v, want 1", z)
	}
	d.area.DoubleTapped(&fyne.PointEvent{Position: fyne.NewPos(400, 300)})
	settle(d)
	if z := zoomOf(t, d); !approx(z, fit) {
		t.Errorf("zoom after second double tap = %v, want fit %v", z, fit)
	}
}

func TestGestureFocus(t *testing.T) {
	d := newGestureDocument(t)
	c := fyne.CurrentApp().Driver().CanvasForObject(d)
	if c == nil {
		t.Fatal("document not on a canvas")
	}

	d.area.MouseDown(&desktop.MouseEvent{})
	d.area.MouseUp(&desktop.MouseEvent{})
	if c.Focused() != d.area {
		t.Fatal("page view not focused after a click")
	}
	c.Unfocus()
	test.Tap(d.area)
	if c.Focused() != d.area {
		t.Error("page view not focused after a tap")
	}
	d.area.TypedRune('x')
}
