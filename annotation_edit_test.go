package pdf

import (
	"image/color"
	"math"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// noteOf returns the note of commentsPDF (index 0 of page 1).
func noteOf(t *testing.T, d *Document) Annotation {
	t.Helper()
	for _, a := range d.Annotations(0) {
		if a.Index == 0 {
			return a
		}
	}
	t.Fatal("note not read")
	return Annotation{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestEditAnnotation(t *testing.T) {
	d := newToolDocument(t, commentsPDF(), ToolNone)
	note := noteOf(t, d)
	d.selectAnnotation(note, false, false)

	if !d.SetAnnotationContents(note, "Changed") {
		t.Fatal("contents not set")
	}
	settle(d)
	if n := noteOf(t, d); n.Contents != "Changed" || n.Author != "Alice" {
		t.Errorf("note after editing: %+v", n)
	}
	if p := d.comments; !p.hasSelected || p.selected != (annotKey{0, 0}) {
		t.Error("note no longer selected after the change")
	}

	blue := color.RGBA{33, 150, 243, 255}
	d.SetAnnotationColor(noteOf(t, d), blue)
	settle(d)
	if c := noteOf(t, d).Color; c != blue {
		t.Errorf("colour %v, want %v", c, blue)
	}

	d.MoveAnnotation(noteOf(t, d), 50, 30)
	settle(d)
	n := noteOf(t, d)
	if b := n.Bounds; !near(b.X0, 450) || !near(b.Y0, 102) {
		t.Errorf("bounds after moving %v, want from 450, 102", b)
	}
	if p := n.Popup; p == nil || !near(p.Bounds.X0, 470) {
		t.Errorf("popup not moved along: %+v", p)
	}

	// Text markup stays on its text.
	for _, a := range d.Annotations(0) {
		if a.Subtype == "Highlight" && d.MoveAnnotation(a, 10, 10) {
			t.Error("highlight moved")
		}
	}

	for range 3 {
		d.Undo()
	}
	settle(d)
	if n := noteOf(t, d); n.Contents != "Check this" || n.Bounds.X0 != 400 {
		t.Errorf("after undoing all: %+v", n)
	}
}

func TestMoveAnnotationByDrag(t *testing.T) {
	d := newToolDocument(t, commentsPDF(), ToolNone)
	offset := d.scrollContainer.Offset

	// Not selected: the drag pans.
	dragArea(d, 410, 82, 450, 102)
	if n := noteOf(t, d); n.Bounds.X0 != 400 {
		t.Fatal("an annotation not selected was moved")
	}

	d.scrollTo(offset)
	d.selectAnnotation(noteOf(t, d), false, true)
	from := linkPoint(d, 0, 410, 82)
	to := linkPoint(d, 0, 450, 102)
	d.area.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: to}, Dragged: fyne.NewDelta(to.X-from.X, to.Y-from.Y)})
	if d.moving == nil || d.moving.outline == nil || d.comments.popup != nil {
		t.Fatal("no outline while moving, or the popup still shown")
	}
	if o := d.scrollContainer.Offset; o != offset {
		t.Errorf("moving panned to %v", o)
	}
	d.area.DragEnd()
	settle(d)
	if n := noteOf(t, d); !near(n.Bounds.X0, 440) || !near(n.Bounds.Y0, 92) {
		t.Errorf("bounds after the drag %v, want from 440, 92", n.Bounds)
	}
	if d.moving != nil {
		t.Error("move not finished")
	}
}

// buttonsOf returns the buttons in o, by text.
func buttonsOf(o fyne.CanvasObject) map[string]*widget.Button {
	out := map[string]*widget.Button{}
	var walk func(o fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		switch o := o.(type) {
		case *widget.Button:
			out[o.Text] = o
		case *fyne.Container:
			for _, c := range o.Objects {
				walk(c)
			}
		}
	}
	walk(o)
	return out
}

func TestAnnotationPopupActions(t *testing.T) {
	d := newToolDocument(t, commentsPDF(), ToolNone)
	note := noteOf(t, d)
	actions := d.annotationActions(note)
	buttons := buttonsOf(actions)
	if buttons["Edit"] == nil || buttons["Delete"] == nil {
		t.Fatalf("buttons %v", buttons)
	}

	test.Tap(buttons["Edit"])
	if d.textEntry == nil || d.textEntry.Text != "Check this" {
		t.Fatal("no dialog with the text of the note")
	}
	d.textEntry.SetText("Edited")
	d.textDialog.Submit()
	settle(d)
	if n := noteOf(t, d); n.Contents != "Edited" {
		t.Errorf("contents %q", n.Contents)
	}

	sw := actions.(*fyne.Container).Objects[0].(*fyne.Container).Objects[2].(*swatch)
	test.Tap(sw)
	settle(d)
	if c := noteOf(t, d).Color; c != sw.color {
		t.Errorf("colour %v after tapping %v", c, sw.color)
	}

	test.Tap(buttonsOf(d.annotationActions(noteOf(t, d)))["Delete"])
	settle(d)
	for _, a := range d.Annotations(0) {
		if a.Contents == "Edited" {
			t.Error("note not deleted")
		}
	}
	if n := len(d.Annotations(0)); n != 3 {
		t.Errorf("%d annotations after deleting the note, want 3", n)
	}
}
