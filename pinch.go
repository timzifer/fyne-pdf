package pdf

import (
	"math"
	"slices"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/mobile"
)

const (
	// minPinchSpread is the least distance of two fingers a pinch is
	// measured with: fingers that start close together would zoom in leaps.
	minPinchSpread = 10
	// pinchTapGap is how long after a pinch taps are ignored: a finger
	// lifted at its end is no tap.
	pinchTapGap = 500 * time.Millisecond
)

// pinch is a pinch gesture in progress.
type pinch struct {
	// zoom is the zoom at its start, anchor the page point under its
	// centre then.
	zoom   float64
	anchor viewAnchor
}

var (
	_ mobile.Touchable = (*pageArea)(nil)
	_ mobile.Movable   = (*pageArea)(nil)
)

// pinchBegin starts a pinch around centre, in view coordinates. The source
// of the pinch (the touches on the page view, a hook of the platform)
// follows it with pinchUpdate and pinchEnd.
func (d *Document) pinchBegin(centre fyne.Position) {
	if d.PageCount() == 0 {
		return
	}
	d.pinch = &pinch{zoom: d.scale, anchor: d.anchorAt(centre)}
}

// pinchUpdate zooms to scale times the zoom the pinch began at and keeps
// the page point that was under its centre then at centre: a pinch that
// moves pans the view.
func (d *Document) pinchUpdate(scale float64, centre fyne.Position) {
	if d.pinch == nil {
		return
	}
	zoom := clampZoom(d.pinch.zoom * scale)
	anchor := d.pinch.anchor
	anchor.at = centre
	if zoom != d.scale {
		d.fitHorizontal, d.fitVertical = false, false
		d.scale = zoom
		d.relayout()
	}
	if offset, ok := d.anchorOffset(anchor); ok {
		d.setOffset(offset)
	}
	d.viewChanged(zoomRenderDelay)
	// The binding's listener finds the zoom applied already.
	_ = d.zoom.Set(zoom)
}

func (d *Document) pinchEnd() {
	d.pinch = nil
}

// TouchDown follows the fingers on a touch screen, as TouchMoved, TouchUp
// and TouchCancel do: two of them pinch to zoom. Only the mobile driver
// reports them.
func (a *pageArea) TouchDown(ev *mobile.TouchEvent) {
	a.touched(ev)
}

func (a *pageArea) TouchMoved(ev *mobile.TouchEvent) {
	a.touched(ev)
}

func (a *pageArea) TouchUp(ev *mobile.TouchEvent) {
	a.released(ev.ID)
}

func (a *pageArea) TouchCancel(ev *mobile.TouchEvent) {
	a.released(ev.ID)
}

// touched notes where a finger is, and starts or follows the pinch. A
// finger not seen going down counts as well: the driver does not report
// every finger lifted (see liftAll), so one may still be down.
func (a *pageArea) touched(ev *mobile.TouchEvent) {
	d := a.doc
	if a.touches == nil {
		a.touches = map[int]fyne.Position{}
	}
	a.touches[ev.ID] = ev.Position.Subtract(d.scrollContainer.Offset)
	if d.pinch == nil {
		a.startPinch()
		return
	}
	if slices.Contains(a.fingers[:], ev.ID) {
		centre, spread := a.pinchGeometry()
		d.pinchUpdate(float64(spread/a.spread), centre)
	}
}

func (a *pageArea) released(id int) {
	delete(a.touches, id)
	if a.doc.pinch != nil && slices.Contains(a.fingers[:], id) {
		a.endPinch()
	}
}

// liftAll forgets the fingers. The mobile driver reports a finger lifted
// while one drags by ending the drag, without TouchUp (fyne v2.8.1).
func (a *pageArea) liftAll() {
	clear(a.touches)
	if a.doc.pinch != nil {
		a.endPinch()
	}
}

// startPinch starts a pinch with the first two fingers down, if there are
// two.
func (a *pageArea) startPinch() {
	if len(a.touches) < 2 {
		return
	}
	ids := make([]int, 0, len(a.touches))
	for id := range a.touches {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	a.fingers = [2]int{ids[0], ids[1]}
	var centre fyne.Position
	centre, a.spread = a.pinchGeometry()
	a.doc.pinchBegin(centre)
}

func (a *pageArea) endPinch() {
	a.doc.pinchEnd()
	a.pinched = time.Now()
}

// pinchGeometry returns the point between the two fingers that pinch, in
// view coordinates, and their distance.
func (a *pageArea) pinchGeometry() (centre fyne.Position, spread float32) {
	p, q := a.touches[a.fingers[0]], a.touches[a.fingers[1]]
	centre = fyne.NewPos((p.X+q.X)/2, (p.Y+q.Y)/2)
	spread = float32(math.Hypot(float64(q.X-p.X), float64(q.Y-p.Y)))
	return centre, max(spread, minPinchSpread)
}

// afterPinch reports whether a pinch goes on or has just ended.
func (a *pageArea) afterPinch() bool {
	return a.doc.pinch != nil || time.Since(a.pinched) < pinchTapGap
}

// watchWindow lets the window showing the widget report the pinches the
// platform has but Fyne does not pass on, see hookWindow. The widget is on
// its window by the time the pointer is over it, not yet when it is first
// laid out.
func (d *Document) watchWindow() {
	c := fyne.CurrentApp().Driver().CanvasForObject(d)
	if c == nil || c == d.watched {
		return
	}
	if w := d.window(); w != nil && hookWindow(w) {
		d.watched = c
	}
}
