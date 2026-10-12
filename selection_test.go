package pdf

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

// helvetica is the /Resources of a page with the font /F1.
const helvetica = "/Resources << /Font << /F1 << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> >> >>"

// textContent writes "Hello world" in 24 pt at 72, 700 and "Second line"
// below it.
const textContent = "BT /F1 24 Tf 72 700 Td (Hello world) Tj 0 -40 Td (Second line) Tj ET"

// selectionPDF has one page of 612x792 pt with textContent.
func selectionPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " + helvetica + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(textContent), textContent),
	}, "")
}

// newSelectDocument shows contents with ToolSelect, its text read.
func newSelectDocument(t *testing.T, contents []byte) *Document {
	t.Helper()
	d := newTestDocument(t)
	loadAndWait(t, d, contents)
	d.SetTool(ToolSelect)
	settle(d)
	if d.texts[0] == nil {
		t.Fatal("text of the page not read")
	}
	return d
}

// pagePos returns the position on the canvas of the point x, y of page 0.
func pagePos(d *Document, x, y float64) fyne.Position {
	pos := linkPoint(d, 0, x, y).Subtract(d.scrollContainer.Offset)
	return fyne.CurrentApp().Driver().AbsolutePositionForObject(d.scrollContainer).Add(pos)
}

// dragArea drags on the page area from the point a to b of page 0 in
// steps, as the GLFW driver reports it: each event at the pointer's new
// position with the distance since the last one.
func dragArea(d *Document, ax, ay, bx, by float64) {
	from, to := linkPoint(d, 0, ax, ay), linkPoint(d, 0, bx, by)
	const steps = 4
	last := from
	for i := 1; i <= steps; i++ {
		f := float32(i) / steps
		pos := fyne.NewPos(from.X+(to.X-from.X)*f, from.Y+(to.Y-from.Y)*f)
		d.area.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: pos}, Dragged: fyne.NewDelta(pos.X-last.X, pos.Y-last.Y)})
		last = pos
	}
	d.area.DragEnd()
}

func TestTextSelectionDrag(t *testing.T) {
	d := newSelectDocument(t, selectionPDF())

	// "Hello world" runs from x 72 at a baseline of y 92 (displayed).
	dragArea(d, 70, 85, 128, 85)
	if s := d.SelectedText(); s != "Hello" {
		t.Errorf("selected %q, want Hello", s)
	}
	if hl := currentView(d).highlights; len(hl) != 1 || hl[0].Color != selectionColor {
		t.Errorf("highlights %v, want the selection", hl)
	}

	// Down to the second line.
	dragArea(d, 70, 85, 156, 125)
	if s := d.SelectedText(); s != "Hello world\nSecond" {
		t.Errorf("selected %q, want two lines", s)
	}
	if n := len(currentView(d).highlights); n != 2 {
		t.Errorf("%d highlights, want one per line", n)
	}

	d.area.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEscape})
	if d.SelectedText() != "" || len(currentView(d).highlights) != 0 {
		t.Error("selection not cleared by Escape")
	}
}

func TestTextSelectionWordAndPage(t *testing.T) {
	d := newSelectDocument(t, selectionPDF())
	d.area.DoubleTapped(&fyne.PointEvent{Position: linkPoint(d, 0, 160, 85)})
	if s := d.SelectedText(); s != "world" {
		t.Errorf("double tap selected %q, want world", s)
	}
	zoom := d.scale
	d.area.TypedShortcut(&fyne.ShortcutSelectAll{})
	if s := d.SelectedText(); s != "Hello world\nSecond line" || d.scale != zoom {
		t.Errorf("Ctrl+A selected %q (zoom %v → %v)", s, zoom, d.scale)
	}
	d.area.Tapped(&fyne.PointEvent{Position: linkPoint(d, 0, 400, 500)})
	if d.SelectedText() != "" {
		t.Error("selection not cleared by a tap")
	}
}

func TestTextSelectionCopy(t *testing.T) {
	d := newSelectDocument(t, selectionPDF())
	clip := fyne.CurrentApp().Clipboard()
	clip.SetContent("before")
	if d.CopySelection() || clip.Content() != "before" {
		t.Error("copied without a selection")
	}
	d.selectPage()
	d.area.TypedShortcut(&fyne.ShortcutCopy{})
	if got := clip.Content(); got != "Hello world\nSecond line" {
		t.Errorf("clipboard %q after Ctrl+C", got)
	}

	// Not allowed by the document.
	d = newSelectDocument(t, encryptedContentPDF(textContent, "", "owner", restricted))
	d.selectPage()
	clip.SetContent("before")
	if d.SelectedText() == "" || d.CopySelection() || clip.Content() != "before" {
		t.Errorf("copied %q without PermCopy", clip.Content())
	}
}

func TestToolPans(t *testing.T) {
	d := newSelectDocument(t, selectionPDF())
	d.Zoom(2)
	d.scrollTo(fyne.Position{})
	settle(d)
	d.area.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(-30, -40)})
	d.area.DragEnd()
	if o := d.scrollContainer.Offset; o != (fyne.Position{}) {
		t.Errorf("ToolSelect panned to %v", o)
	}
	test.Tap(d.toolButton)
	if d.Tool() != ToolNone || d.SelectedText() != "" {
		t.Fatal("tool button did not switch back to panning")
	}
	d.area.Dragged(&fyne.DragEvent{Dragged: fyne.NewDelta(-30, -40)})
	if o := d.scrollContainer.Offset; o != fyne.NewPos(30, 40) {
		t.Errorf("offset after drag = %v, want 30,40", o)
	}
}

func TestTextCursor(t *testing.T) {
	d := newSelectDocument(t, selectionPDF())
	move := func(x, y float64) desktop.Cursor {
		d.area.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: linkPoint(d, 0, x, y)}})
		return d.area.Cursor()
	}
	if move(100, 85) != desktop.TextCursor {
		t.Error("no I-beam over text")
	}
	if move(400, 500) != desktop.DefaultCursor {
		t.Error("I-beam beside the text")
	}
	d.SetTool(ToolNone)
	if move(100, 85) != desktop.DefaultCursor {
		t.Error("I-beam while panning")
	}
}
