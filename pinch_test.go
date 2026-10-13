package pdf

import (
	"math"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/mobile"
)

// finger is the event for finger id at pos in the view. The mobile driver
// gives the position in the page area.
func finger(d *Document, id int, pos fyne.Position) *mobile.TouchEvent {
	ev := &mobile.TouchEvent{ID: id}
	ev.Position = pos.Add(d.scrollContainer.Offset)
	return ev
}

// startPinch puts finger 0 and 1 down at p and q in the view.
func startPinch(t *testing.T, d *Document, p, q fyne.Position) {
	t.Helper()
	d.area.TouchDown(finger(d, 0, p))
	if d.pinch != nil {
		t.Fatal("one finger pinches")
	}
	d.area.TouchDown(finger(d, 1, q))
	if d.pinch == nil {
		t.Fatal("two fingers do not pinch")
	}
}

func sameAnchor(a, b viewAnchor) bool {
	return a.page == b.page &&
		math.Abs(float64(a.point.X-b.point.X)) <= 1 && math.Abs(float64(a.point.Y-b.point.Y)) <= 1
}

func TestPinchZoomsAroundCentre(t *testing.T) {
	for _, mode := range []ViewMode{ViewSinglePage, ViewContinuous} {
		d := newGestureDocument(t)
		d.SetViewMode(mode)
		d.ZoomToFit()
		settle(d)
		zoomTop(d, 1)
		d.scrollTo(fyne.NewPos(0, 200))

		centre := fyne.NewPos(350, 250)
		before := d.anchorAt(centre)
		startPinch(t, d, fyne.NewPos(300, 250), fyne.NewPos(400, 250))

		// The fingers move apart to three times their distance.
		d.area.TouchMoved(finger(d, 1, fyne.NewPos(500, 250)))
		d.area.TouchMoved(finger(d, 0, fyne.NewPos(200, 250)))
		settle(d)
		if z := zoomOf(t, d); !approx(z, 3) {
			t.Fatalf("mode %v: zoom = %v, want 3", mode, z)
		}
		if after := d.anchorAt(centre); !sameAnchor(before, after) {
			t.Errorf("mode %v: page point under the centre moved from %+v to %+v", mode, before, after)
		}
		if d.fitHorizontal || d.fitVertical {
			t.Errorf("mode %v: fit kept while pinching", mode)
		}

		// Together again, and on to half the distance.
		d.area.TouchMoved(finger(d, 0, fyne.NewPos(325, 250)))
		d.area.TouchMoved(finger(d, 1, fyne.NewPos(375, 250)))
		settle(d)
		if z := zoomOf(t, d); !approx(z, 0.5) {
			t.Errorf("mode %v: zoom = %v, want 0.5", mode, z)
		}
	}
}

func TestPinchPans(t *testing.T) {
	d := newGestureDocument(t)
	zoomTop(d, 2)
	d.scrollTo(fyne.NewPos(100, 200))

	startPinch(t, d, fyne.NewPos(300, 250), fyne.NewPos(400, 250))
	d.area.TouchMoved(finger(d, 0, fyne.NewPos(260, 220)))
	d.area.TouchMoved(finger(d, 1, fyne.NewPos(360, 220)))
	if z := d.scale; z != 2 {
		t.Errorf("zoom = %v, want 2", z)
	}
	if o := d.scrollContainer.Offset; o != fyne.NewPos(140, 230) {
		t.Errorf("offset = %v, want 140,230", o)
	}

	// The drag the driver reports for the first finger does not pan as
	// well.
	d.area.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(-40, -30)})
	if o := d.scrollContainer.Offset; o != fyne.NewPos(140, 230) {
		t.Errorf("offset after the drag of a pinching finger = %v, want 140,230", o)
	}
}

func TestPinchEnds(t *testing.T) {
	d := newGestureDocument(t)
	p, q := fyne.NewPos(300, 250), fyne.NewPos(400, 250)

	// A finger lifted ends the pinch; the other one does not zoom alone.
	startPinch(t, d, p, q)
	d.area.TouchUp(finger(d, 1, q))
	if d.pinch != nil {
		t.Fatal("pinch goes on after a finger was lifted")
	}
	d.area.TouchMoved(finger(d, 0, fyne.NewPos(100, 250)))
	if z := d.scale; z != 1 {
		t.Errorf("zoom = %v, want 1", z)
	}
	d.area.TouchCancel(finger(d, 0, p))

	// So does a third finger put down, but not when lifted.
	startPinch(t, d, p, q)
	r := fyne.NewPos(500, 400)
	d.area.TouchDown(finger(d, 2, r))
	d.area.TouchMoved(finger(d, 2, fyne.NewPos(700, 500)))
	if z := d.scale; z != 1 {
		t.Errorf("zoom after moving a third finger = %v, want 1", z)
	}
	d.area.TouchUp(finger(d, 2, r))
	if d.pinch == nil {
		t.Fatal("lifting a third finger ended the pinch")
	}

	// The end of the drag stands for a finger lifted while it goes on.
	d.area.TouchMoved(finger(d, 1, fyne.NewPos(500, 250)))
	if z := d.scale; z != 2 {
		t.Fatalf("zoom = %v, want 2", z)
	}
	d.area.DragEnd()
	if d.pinch != nil {
		t.Fatal("pinch goes on after the drag ended")
	}
	d.area.TouchMoved(finger(d, 0, fyne.NewPos(100, 250)))
	if z := d.scale; z != 2 {
		t.Errorf("zoom after moving the finger left = %v, want 2", z)
	}

	// The next pinch starts from the zoom reached.
	d.area.TouchDown(finger(d, 1, fyne.NewPos(300, 250)))
	d.area.TouchMoved(finger(d, 1, fyne.NewPos(200, 250)))
	if z := d.scale; z != 1 {
		t.Errorf("zoom after pinching in = %v, want 1", z)
	}
	d.area.DragEnd()
	settle(d)

	// One finger pans as before.
	before := d.scrollContainer.Offset
	d.area.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(0, -40)})
	d.area.DragEnd()
	if o := d.scrollContainer.Offset; o != before.AddXY(0, 40) {
		t.Errorf("offset after a drag = %v, want %v", o, before.AddXY(0, 40))
	}
}

func TestPinchStaysInZoomLimits(t *testing.T) {
	d := newGestureDocument(t)
	startPinch(t, d, fyne.NewPos(390, 250), fyne.NewPos(410, 250))

	d.area.TouchMoved(finger(d, 1, fyne.NewPos(790, 250)))
	if z := d.scale; z != maxZoom {
		t.Errorf("zoom = %v, want %v", z, maxZoom)
	}
	// Fingers on one spot are as far apart as minPinchSpread.
	d.area.TouchMoved(finger(d, 1, fyne.NewPos(390, 250)))
	if z := d.scale; !approx(z, float64(minPinchSpread)/20) {
		t.Errorf("zoom = %v, want %v", z, float64(minPinchSpread)/20)
	}
	d.area.TouchUp(finger(d, 1, fyne.NewPos(390, 250)))
	d.area.TouchUp(finger(d, 0, fyne.NewPos(390, 250)))

	zoomTop(d, 0.3)
	startPinch(t, d, fyne.NewPos(100, 250), fyne.NewPos(700, 250))
	d.area.TouchMoved(finger(d, 1, fyne.NewPos(150, 250)))
	if z := d.scale; z != minZoom {
		t.Errorf("zoom = %v, want %v", z, minZoom)
	}
	settle(d)
}

func TestPinchIsNoTap(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(1))
	p, q := fyne.NewPos(300, 250), fyne.NewPos(400, 250)

	startPinch(t, d, p, q)
	d.area.TouchMoved(finger(d, 1, fyne.NewPos(450, 250)))
	d.area.TouchUp(finger(d, 1, fyne.NewPos(450, 250)))
	d.area.TouchUp(finger(d, 0, p))
	settle(d)
	pinched := zoomOf(t, d)

	d.area.DoubleTapped(&fyne.PointEvent{Position: p})
	settle(d)
	if z := zoomOf(t, d); z != pinched {
		t.Errorf("zoom after a double tap at the end of a pinch = %v, want %v", z, pinched)
	}
	d.area.pinched = time.Now().Add(-2 * pinchTapGap)
	d.area.DoubleTapped(&fyne.PointEvent{Position: p})
	settle(d)
	if z := zoomOf(t, d); z == pinched {
		t.Error("a later double tap did not zoom")
	}
}

func TestPinchWithoutDocument(t *testing.T) {
	d := newTestDocument(t)
	d.area.TouchDown(finger(d, 0, fyne.NewPos(300, 250)))
	d.area.TouchDown(finger(d, 1, fyne.NewPos(400, 250)))
	d.area.TouchMoved(finger(d, 1, fyne.NewPos(500, 250)))
	d.area.TouchUp(finger(d, 1, fyne.NewPos(500, 250)))
	d.area.DragEnd()
	if z := d.scale; z != 1 {
		t.Errorf("zoom = %v, want 1", z)
	}
}

// TestPinchWatchesWindow checks that the window is hooked once the pointer
// is over the pages or the wheel turns, not again after that.
func TestPinchWatchesWindow(t *testing.T) {
	d := newGestureDocument(t)
	c := fyne.CurrentApp().Driver().CanvasForObject(d)
	if c == nil {
		t.Fatal("document not on a canvas")
	}
	if d.watched != nil {
		t.Fatal("window watched before the pointer came")
	}
	d.area.MouseIn(&desktop.MouseEvent{})
	if d.watched != c {
		t.Error("window not watched with the pointer over the pages")
	}

	d.watched = nil
	d.modifiers = holding(0)
	d.scrollContainer.Scrolled(wheel(-wheelNotch, fyne.NewPos(100, 100)))
	if d.watched != c {
		t.Error("window not watched after the wheel turned")
	}
}
