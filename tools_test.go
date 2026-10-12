package pdf

import (
	"bytes"
	"errors"
	"testing"

	"fyne.io/fyne/v2"
	"github.com/timzifer/cera"
)

// newToolDocument shows selectionPDF with tool t; the author is "Tester".
func newToolDocument(t *testing.T, contents []byte, tool Tool) *Document {
	t.Helper()
	d := newTestDocument(t)
	d.Author = "Tester"
	loadAndWait(t, d, contents)
	d.SetTool(tool)
	settle(d)
	return d
}

// annotsOf returns the subtypes of the annotations of page 0.
func annotsOf(d *Document) []string {
	var out []string
	for _, a := range d.Annotations(0) {
		out = append(out, a.Subtype)
	}
	return out
}

func TestToolHighlightUndoRedo(t *testing.T) {
	original := selectionPDF()
	d := newToolDocument(t, original, ToolHighlight)
	changes := 0
	d.OnChanged = func() { changes++ }
	d.Zoom(1.5)
	settle(d)
	zoom, page := d.scale, d.CurrentPage()

	dragArea(d, 70, 85, 128, 85)
	settle(d)
	annots := d.Annotations(0)
	if len(annots) != 1 || annots[0].Subtype != "Highlight" || annots[0].Author != "Tester" || len(annots[0].Rects) != 1 {
		t.Fatalf("annotations after highlighting: %+v", annots)
	}
	if r := annots[0].Rects[0]; r.X0 > 73 || r.X1 < 120 {
		t.Errorf("highlight over %v, want over Hello", r)
	}
	if !d.Modified() || !d.CanUndo() || d.CanRedo() || changes != 1 || d.SelectedText() != "" {
		t.Errorf("modified %v, undo %v, redo %v, %d changes, selection %q", d.Modified(), d.CanUndo(), d.CanRedo(), changes, d.SelectedText())
	}
	if d.scale != zoom || d.CurrentPage() != page {
		t.Errorf("view changed: zoom %v → %v, page %d → %d", zoom, d.scale, page, d.CurrentPage())
	}
	if c := d.Contents(); !bytes.HasPrefix(c, original) || len(c) <= len(original) {
		t.Error("the change is not an incremental update of the file")
	}

	d.area.TypedShortcut(&fyne.ShortcutUndo{})
	settle(d)
	if a := annotsOf(d); len(a) != 0 || d.Modified() || !d.CanRedo() {
		t.Errorf("after undo: %v, modified %v, redo %v", a, d.Modified(), d.CanRedo())
	}
	d.area.TypedShortcut(&fyne.ShortcutRedo{})
	settle(d)
	if a := annotsOf(d); len(a) != 1 || !d.Modified() {
		t.Errorf("after redo: %v", a)
	}

	// A double click marks the word.
	d.SetTool(ToolStrikeOut)
	d.area.DoubleTapped(&fyne.PointEvent{Position: linkPoint(d, 0, 160, 85)})
	settle(d)
	if a := annotsOf(d); len(a) != 2 || a[1] != "StrikeOut" {
		t.Errorf("after a double click with ToolStrikeOut: %v", a)
	}
	if d.CanRedo() {
		t.Error("redo still possible after a new change")
	}
}

func TestToolShapes(t *testing.T) {
	d := newToolDocument(t, selectionPDF(), ToolInk)
	dragArea(d, 300, 300, 400, 380)
	settle(d)
	if d.sketch != nil {
		t.Error("sketch left after the drag")
	}
	for _, tt := range []struct {
		tool    Tool
		subtype string
	}{{ToolRect, "Square"}, {ToolEllipse, "Circle"}, {ToolLine, "Line"}} {
		d.SetTool(tt.tool)
		dragArea(d, 300, 400, 400, 480)
		settle(d)
		a := d.Annotations(0)
		if last := a[len(a)-1]; last.Subtype != tt.subtype || last.Bounds.X0 > 300.01 || last.Bounds.X1 < 399.99 {
			t.Errorf("%s: %+v", tt.subtype, last)
		}
	}
	if a := annotsOf(d); len(a) != 4 || a[0] != "Ink" {
		t.Errorf("annotations %v, want ink and three shapes", a)
	}

	// A drag too short draws nothing.
	d.SetTool(ToolRect)
	dragArea(d, 300, 400, 300.5, 400.5)
	settle(d)
	if n := len(annotsOf(d)); n != 4 {
		t.Errorf("%d annotations after a tiny drag", n)
	}
}

func TestToolSketchShown(t *testing.T) {
	d := newToolDocument(t, selectionPDF(), ToolRect)
	from := linkPoint(d, 0, 100, 100)
	d.area.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: from.AddXY(50, 40)}, Dragged: fyne.NewDelta(50, 40)})
	if d.sketch == nil || len(d.sketch.objects) != 1 || d.sketch.objects[0].Position() != from {
		t.Fatalf("sketch %+v while dragging", d.sketch)
	}
	d.area.DragEnd()
}

func TestToolNoteAndFreeText(t *testing.T) {
	d := newToolDocument(t, selectionPDF(), ToolNote)
	d.area.Tapped(&fyne.PointEvent{Position: linkPoint(d, 0, 300, 200)})
	if d.textDialog == nil {
		t.Fatal("no dialog for the text of the note")
	}
	d.textEntry.SetText("A note")
	d.textDialog.Submit()
	settle(d)
	a := d.Annotations(0)
	if len(a) != 1 || a[0].Subtype != "Text" || a[0].Contents != "A note" || a[0].Bounds.X0 != 300 || a[0].Bounds.Y0 != 200 {
		t.Fatalf("note: %+v", a)
	}

	// Cancelled, or without text: nothing.
	d.area.Tapped(&fyne.PointEvent{Position: linkPoint(d, 0, 300, 300)})
	d.textDialog.Submit()
	settle(d)
	if n := len(annotsOf(d)); n != 1 {
		t.Errorf("%d annotations after a note without text", n)
	}

	d.SetTool(ToolFreeText)
	d.area.Tapped(&fyne.PointEvent{Position: linkPoint(d, 0, 100, 300)})
	d.textEntry.SetText("Boxed")
	d.textDialog.Submit()
	settle(d)
	a = d.Annotations(0)
	if last := a[len(a)-1]; last.Subtype != "FreeText" || last.Contents != "Boxed" || last.Bounds.X1-last.Bounds.X0 < 150 {
		t.Errorf("text box: %+v", last)
	}
}

func TestDeleteAnnotation(t *testing.T) {
	d := newToolDocument(t, commentsPDF(), ToolNone)
	before := len(d.Annotations(0))
	d.selectAnnotation(d.Annotations(0)[1], false, false) // the highlight with its replies
	d.area.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDelete})
	settle(d)
	if n := len(d.Annotations(0)); n != before-3 {
		t.Errorf("%d annotations after deleting the highlight, want %d", n, before-3)
	}
	d.Undo()
	settle(d)
	if n := len(d.Annotations(0)); n != before {
		t.Errorf("%d annotations after undo, want %d", n, before)
	}
}

func TestSave(t *testing.T) {
	d := newToolDocument(t, selectionPDF(), ToolHighlight)
	dragArea(d, 70, 85, 128, 85)
	settle(d)
	var buf bytes.Buffer
	if err := d.Save(&buf); err != nil {
		t.Fatal(err)
	}
	settle(d)
	if d.Modified() || d.CanUndo() {
		t.Errorf("after saving a new file: modified %v, undo %v", d.Modified(), d.CanUndo())
	}
	src, err := OpenSource(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if a, _ := src.Annotations(0); len(a) != 1 || a[0].Subtype != "Highlight" {
		t.Errorf("saved file has %+v", a)
	}
	if !bytes.Equal(d.Contents(), buf.Bytes()) {
		t.Error("the widget does not show the file it saved")
	}
}

func TestSaveEncrypted(t *testing.T) {
	original := encryptedContentPDF(textContent, "", "owner", -4)
	d := newToolDocument(t, original, ToolHighlight)
	dragArea(d, 70, 85, 128, 85)
	settle(d)
	var buf bytes.Buffer
	if err := d.Save(&buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(buf.Bytes(), original) || d.Modified() || !d.CanUndo() {
		t.Errorf("encrypted: prefix %v, modified %v, undo %v", bytes.HasPrefix(buf.Bytes(), original), d.Modified(), d.CanUndo())
	}
	src, err := OpenSource(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !src.Encrypted() {
		t.Error("saved decrypted")
	}

	// Without the right to annotate: refused, reported, nothing changed.
	noAnnot := encryptedContentPDF(textContent, "", "owner", -4&^int32(PermAnnotate|PermModify))
	d = newToolDocument(t, noAnnot, ToolHighlight)
	var reported error
	d.OnError = func(_ int, err error) { reported = err }
	dragArea(d, 70, 85, 128, 85)
	settle(d)
	if !errors.Is(reported, cera.ErrNotPermitted) || d.Modified() || len(annotsOf(d)) != 0 {
		t.Errorf("without PermAnnotate: reported %v, modified %v", reported, d.Modified())
	}
}
