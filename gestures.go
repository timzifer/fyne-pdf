package pdf

import (
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	// wheelNotch is the scroll distance of one mouse wheel notch on Windows
	// and Linux (Fyne's scroll speed; 10 on macOS).
	wheelNotch = 25

	// zoomWheelFactor is the zoom change per wheel notch with Ctrl held.
	zoomWheelFactor = 1.1
	// zoomKeyFactor is the zoom change per Ctrl +/- key press.
	zoomKeyFactor = 1.25

	// pageTurnDistance is how far the user has to scroll past the top or
	// bottom of a page to turn to the previous or next one.
	pageTurnDistance = 2 * wheelNotch
	// scrollGestureGap separates scroll gestures: events closer together
	// belong to one gesture (a wheel spin, a touchpad swipe with inertia),
	// which turns at most one page.
	scrollGestureGap = 250 * time.Millisecond

	// arrowScrollStep is how far the arrow keys scroll.
	arrowScrollStep = 40
	// pageScrollOverlap is the part of the view that stays visible when
	// scrolling by a screen with Page Up/Down or Space.
	pageScrollOverlap = 0.1
)

// pageArea is the content of the main scroll view. It shows the page and
// handles the viewer gestures: Ctrl+wheel zoom, page turns at the edges,
// dragging to pan, double tap, and keyboard navigation once focused.
type pageArea struct {
	widget.BaseWidget

	doc *Document

	// overscroll accumulates scrolling past the top (positive) or bottom
	// (negative) edge of the page within one gesture.
	overscroll float32
	lastScroll time.Time
	// turned is set when the current gesture has turned a page.
	turned bool
}

var (
	_ fyne.Scrollable     = (*pageArea)(nil)
	_ fyne.Draggable      = (*pageArea)(nil)
	_ fyne.Tappable       = (*pageArea)(nil)
	_ fyne.DoubleTappable = (*pageArea)(nil)
	_ fyne.Focusable      = (*pageArea)(nil)
	_ fyne.Shortcutable   = (*pageArea)(nil)
	_ desktop.Mouseable   = (*pageArea)(nil)
)

func newPageArea(d *Document) *pageArea {
	a := &pageArea{doc: d}
	a.ExtendBaseWidget(a)
	return a
}

func (a *pageArea) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(a.doc.bigPage)
}

// Scrolled zooms with Ctrl (Cmd on macOS) held, otherwise scrolls the view
// and turns the page when scrolling on past its top or bottom.
func (a *pageArea) Scrolled(ev *fyne.ScrollEvent) {
	d := a.doc
	if d.zoomModifier() {
		factor := math.Pow(zoomWheelFactor, float64(ev.Scrolled.DY)/wheelNotch)
		d.zoomAt(d.bigPage.Scale*factor, ev.Position.Subtract(d.scrollContainer.Offset))
		return
	}

	before := d.scrollContainer.Offset
	d.scrollContainer.Scrolled(ev)

	now := time.Now()
	if now.Sub(a.lastScroll) > scrollGestureGap {
		a.overscroll, a.turned = 0, false
	}
	a.lastScroll = now

	if d.scrollContainer.Offset != before || ev.Scrolled.DY == 0 {
		a.overscroll = 0
		return
	}
	if a.turned {
		return
	}
	if (a.overscroll < 0) != (ev.Scrolled.DY < 0) {
		a.overscroll = 0
	}
	a.overscroll += ev.Scrolled.DY
	switch {
	case a.overscroll <= -pageTurnDistance:
		a.turned = d.turnPage(d.CurrentPage()+1, false)
	case a.overscroll >= pageTurnDistance:
		a.turned = d.turnPage(d.CurrentPage()-1, true)
	}
	if a.turned {
		a.overscroll = 0
	}
}

// Dragged pans the page.
func (a *pageArea) Dragged(ev *fyne.DragEvent) {
	d := a.doc
	d.scrollTo(d.scrollContainer.Offset.Subtract(ev.Dragged))
}

func (a *pageArea) DragEnd() {}

// Tapped focuses the view on touch devices; desktop focuses on MouseDown.
func (a *pageArea) Tapped(*fyne.PointEvent) {
	a.focus()
}

// DoubleTapped switches between the fitted page and 100 %, zooming in at
// the tapped point.
func (a *pageArea) DoubleTapped(ev *fyne.PointEvent) {
	d := a.doc
	if d.fitHorizontal && d.fitVertical {
		d.zoomAt(1, ev.Position.Subtract(d.scrollContainer.Offset))
		return
	}
	d.ZoomToFit()
}

func (a *pageArea) MouseDown(*desktop.MouseEvent) {
	a.focus()
}

func (a *pageArea) MouseUp(*desktop.MouseEvent) {}

func (a *pageArea) focus() {
	if c := fyne.CurrentApp().Driver().CanvasForObject(a); c != nil {
		c.Focus(a)
	}
}

func (a *pageArea) FocusGained() {}

func (a *pageArea) FocusLost() {}

func (a *pageArea) TypedRune(rune) {}

// TypedKey navigates: Page Up/Down and Space (Shift+Space) scroll by a
// screen and then turn the page, Home/End go to the first/last page, the
// arrow keys scroll; Left/Right turn the page when it fits horizontally.
func (a *pageArea) TypedKey(ev *fyne.KeyEvent) {
	d := a.doc
	switch ev.Name {
	case fyne.KeyPageDown:
		d.scrollScreen(1)
	case fyne.KeyPageUp:
		d.scrollScreen(-1)
	case fyne.KeySpace:
		if d.modifiers()&fyne.KeyModifierShift != 0 {
			d.scrollScreen(-1)
		} else {
			d.scrollScreen(1)
		}
	case fyne.KeyHome:
		d.turnPage(0, false)
	case fyne.KeyEnd:
		d.turnPage(d.PageCount()-1, false)
	case fyne.KeyDown:
		d.scrollTo(d.scrollContainer.Offset.AddXY(0, arrowScrollStep))
	case fyne.KeyUp:
		d.scrollTo(d.scrollContainer.Offset.AddXY(0, -arrowScrollStep))
	case fyne.KeyRight:
		if d.fitsHorizontally() {
			d.turnPage(d.CurrentPage()+1, false)
		} else {
			d.scrollTo(d.scrollContainer.Offset.AddXY(arrowScrollStep, 0))
		}
	case fyne.KeyLeft:
		if d.fitsHorizontally() {
			d.turnPage(d.CurrentPage()-1, false)
		} else {
			d.scrollTo(d.scrollContainer.Offset.AddXY(-arrowScrollStep, 0))
		}
	}
}

// TypedShortcut zooms with Ctrl (Cmd on macOS) and +, - or 0.
func (a *pageArea) TypedShortcut(s fyne.Shortcut) {
	cs, ok := s.(*desktop.CustomShortcut)
	if !ok || cs.Modifier&^fyne.KeyModifierShift != fyne.KeyModifierShortcutDefault {
		return
	}
	d := a.doc
	center := d.scrollContainer.Size()
	anchor := fyne.NewPos(center.Width/2, center.Height/2)
	switch cs.KeyName {
	case fyne.KeyPlus, fyne.KeyEqual:
		d.zoomAt(d.bigPage.Scale*zoomKeyFactor, anchor)
	case fyne.KeyMinus:
		d.zoomAt(d.bigPage.Scale/zoomKeyFactor, anchor)
	case fyne.Key0:
		d.zoomAt(1, anchor)
	}
}

// zoomModifier reports whether the modifier for zooming with the wheel is
// held: Ctrl, or Cmd on macOS.
func (d *Document) zoomModifier() bool {
	m := d.modifiers()
	return m&(fyne.KeyModifierShortcutDefault|fyne.KeyModifierControl) != 0
}

// driverKeyModifiers returns the modifiers the Fyne driver tracks. Its GLFW
// driver treats every key action but a press as a release, so a modifier
// that is held long enough to auto-repeat drops out (fyne v2.8.1); see
// currentKeyModifiers.
func driverKeyModifiers() fyne.KeyModifier {
	if drv, ok := fyne.CurrentApp().Driver().(desktop.Driver); ok {
		return drv.CurrentKeyModifiers()
	}
	return 0
}

// zoomAt sets the zoom, keeping the page point at anchor (in view
// coordinates) in place.
func (d *Document) zoomAt(zoom float64, anchor fyne.Position) {
	zoom = clampZoom(zoom)
	old := d.bigPage.Scale
	if zoom == old {
		return
	}
	view := d.scrollContainer.Size()
	page := d.bigPage.PageSize()

	// Page point under the anchor, at zoom 1.
	oldOrigin := pageOrigin(view, page, old)
	point := d.scrollContainer.Offset.Add(anchor).Subtract(oldOrigin)
	point = fyne.NewPos(point.X/float32(old), point.Y/float32(old))

	d.Zoom(zoom)

	newOrigin := pageOrigin(view, page, zoom)
	target := newOrigin.Add(fyne.NewPos(point.X*float32(zoom), point.Y*float32(zoom)))
	d.scrollTo(target.Subtract(anchor))
}

// pageOrigin is the position of the page within the scroll content: the
// page is centred while it is smaller than the view.
func pageOrigin(view, page fyne.Size, zoom float64) fyne.Position {
	w, h := page.Width*float32(zoom), page.Height*float32(zoom)
	return fyne.NewPos(max(0, (view.Width-w)/2), max(0, (view.Height-h)/2))
}

// scrollTo moves the view to offset, clamped to the content.
func (d *Document) scrollTo(offset fyne.Position) {
	view := d.scrollContainer.Size()
	content := d.area.MinSize().Max(view)
	d.scrollContainer.ScrollToOffset(fyne.NewPos(
		min(max(0, offset.X), content.Width-view.Width),
		min(max(0, offset.Y), content.Height-view.Height),
	))
}

// scrollScreen scrolls by one screen down (dir 1) or up (dir -1), turning
// the page when its bottom or top is already visible.
func (d *Document) scrollScreen(dir float32) {
	before := d.scrollContainer.Offset
	step := d.scrollContainer.Size().Height * (1 - pageScrollOverlap)
	d.scrollTo(before.AddXY(0, dir*step))
	if d.scrollContainer.Offset != before {
		return
	}
	if dir > 0 {
		d.turnPage(d.CurrentPage()+1, false)
	} else {
		d.turnPage(d.CurrentPage()-1, true)
	}
}

func (d *Document) fitsHorizontally() bool {
	return d.area.MinSize().Width <= d.scrollContainer.Size().Width
}

// turnPage shows page keeping the zoom (or the fit, if one is active) and
// scrolls to its top or bottom. It reports whether the page changed.
func (d *Document) turnPage(page int, atBottom bool) bool {
	if !d.setPage(page) {
		return false
	}
	if d.fitHorizontal || d.fitVertical {
		d.fitZoom(d.fitHorizontal, d.fitVertical)
	}
	d.layoutArea()
	d.scheduleRender(0)

	offset := fyne.NewPos(d.scrollContainer.Offset.X, 0)
	if atBottom {
		offset.Y = d.area.MinSize().Height
	}
	d.scrollTo(offset)
	return true
}
