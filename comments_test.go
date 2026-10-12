package pdf

import (
	"fmt"
	"image/color"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// commentsPDF has two pages of 612x792 pt. Page 1 has, in /Annots: a note
// by Alice at the top right (index 0) with its popup (1), a highlight by
// Bob (2) with a reply by Alice (3) and a review state set by Bob (4), and
// a link (5). Page 2 has a strikethrough by Carol (0).
func commentsPDF() []byte {
	content := "1 0 0 rg 100 100 200 200 re f"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R /Annots [6 0 R 7 0 R 8 0 R 9 0 R 10 0 R 11 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R /Annots [12 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Annot /Subtype /Text /Rect [400 700 420 720] /Contents (Check this) /T (Alice) /Name /Comment" +
			" /CreationDate (D:20260101100000Z) /M (D:20260101120000Z) /C [1 1 0] /Popup 7 0 R >>",
		"<< /Type /Annot /Subtype /Popup /Parent 6 0 R /Rect [420 600 600 720] /Open false >>",
		"<< /Type /Annot /Subtype /Highlight /Rect [100 690 300 710] /QuadPoints [100 710 300 710 100 690 300 690]" +
			" /C [1 1 0] /T (Bob) /Contents (Important) >>",
		"<< /Type /Annot /Subtype /Text /Rect [300 690 320 710] /IRT 8 0 R /T (Alice) /Contents (Agreed) /F 2 >>",
		"<< /Type /Annot /Subtype /Text /Rect [300 690 320 710] /IRT 8 0 R /T (Bob) /Contents (Accepted set by Bob)" +
			" /State (Accepted) /StateModel (Review) /F 2 >>",
		"<< /Type /Annot /Subtype /Link /Rect [100 100 200 120] /A << /S /URI /URI (https://example.com/) >> >>",
		"<< /Type /Annot /Subtype /StrikeOut /Rect [100 400 200 420] /QuadPoints [100 420 200 420 100 400 200 400]" +
			" /C [1 0 0] /T (Carol) /Contents (Remove) >>",
	}, "")
}

func TestSourceAnnotations(t *testing.T) {
	src, err := OpenSource(commentsPDF())
	if err != nil {
		t.Fatal(err)
	}
	annots, err := src.Annotations(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(annots) != 4 {
		t.Fatalf("%d annotations, want 4 (no popup, no link): %+v", len(annots), annots)
	}
	note, hl, reply, state := annots[0], annots[1], annots[2], annots[3]
	if note.Subtype != "Text" || note.Index != 0 || note.Author != "Alice" || note.Contents != "Check this" || note.Icon != "Comment" {
		t.Errorf("note: %+v", note)
	}
	if !note.Created.Equal(time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)) || !note.Modified.Equal(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("note dates: %v, %v", note.Created, note.Modified)
	}
	if note.Color != (color.RGBA{255, 255, 0, 255}) || note.Bounds != (Rect{400, 72, 420, 92}) || note.ReplyTo != -1 {
		t.Errorf("note: colour %v, bounds %v, reply to %d", note.Color, note.Bounds, note.ReplyTo)
	}
	if p := note.Popup; p == nil || p.Open || p.Bounds != (Rect{420, 72, 600, 192}) {
		t.Errorf("note popup: %+v", p)
	}
	if hl.Subtype != "Highlight" || hl.Index != 2 || len(hl.Rects) != 1 || hl.Rects[0] != (Rect{100, 82, 300, 102}) {
		t.Errorf("highlight: %+v", hl)
	}
	if reply.ReplyTo != 2 || reply.Group || state.ReplyTo != 2 || state.State != "Accepted" || state.StateModel != "Review" {
		t.Errorf("replies: %+v, %+v", reply, state)
	}
}

// rowsOf returns author and depth of the rows of the comments panel.
func rowsOf(d *Document) []string {
	var out []string
	for _, o := range d.comments.list.Objects {
		r := o.(*commentRow)
		out = append(out, fmt.Sprintf("%d:%s:%d", r.annot.Page, r.annot.Author, r.depth))
	}
	return out
}

func TestDocumentCommentsPanel(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(2))
	if d.commentsButton.Visible() || len(d.comments.rows) != 0 {
		t.Fatal("comments button shown for a PDF without annotations")
	}

	loadAndWait(t, d, commentsPDF())
	want := []string{"0:Alice:0", "0:Bob:0", "0:Alice:1", "0:Bob:1", "1:Carol:0"}
	if got := rowsOf(d); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("rows %v, want %v", got, want)
	}
	if r := d.comments.rows[annotKey{0, 2}]; r == nil || r.state != "Accepted" {
		t.Error("highlight row without its review state")
	}
	if !d.commentsButton.Visible() || d.CommentsShown() {
		t.Fatal("want the comments button, and the panel hidden at first")
	}
	viewWidth := d.scrollContainer.Size().Width
	test.Tap(d.commentsButton)
	if !d.CommentsShown() {
		t.Fatal("panel not shown after tapping the button")
	}
	if w := d.scrollContainer.Size().Width; w > viewWidth-commentsWidth+1 {
		t.Errorf("view %v wide beside the comments, was %v", w, viewWidth)
	}
	if n := len(d.Annotations(0)); n != 4 {
		t.Errorf("Annotations(0): %d, want 4", n)
	}

	// The panel stays wanted, but there is nothing to show.
	loadAndWait(t, d, minimalPDF())
	if d.CommentsShown() || d.commentsButton.Visible() || len(d.comments.list.Objects) != 0 {
		t.Error("comments still shown for a PDF without annotations")
	}
	loadAndWait(t, d, commentsPDF())
	if !d.CommentsShown() {
		t.Error("panel not shown again for the next PDF with annotations")
	}
}

func TestDocumentSelectComment(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, commentsPDF())
	d.ShowComments(true)
	p := d.comments

	// A tap on the row of the strikethrough goes to page 2 and marks it.
	test.Tap(p.rows[annotKey{1, 0}])
	settle(d)
	if d.CurrentPage() != 1 || !p.hasSelected || p.selected != (annotKey{1, 0}) {
		t.Fatalf("after tapping the row: page %d, selected %v %v", d.CurrentPage(), p.hasSelected, p.selected)
	}
	hl := currentView(d).highlights
	if len(hl) != 1 || hl[0].Color != selectionColor || !approx(float64(hl[0].Position.Y), 372*unitsPerPoint) {
		t.Errorf("page 2 highlights %v, want the strikethrough marked", hl)
	}
	if !p.rows[annotKey{1, 0}].selected() || p.popup != nil {
		t.Error("row not selected, or a popup shown for a tap in the list")
	}

	// A tap on the highlight on page 1 selects its row and shows the
	// thread beside it.
	d.ShowPage(0)
	settle(d)
	c := fyne.CurrentApp().Driver().CanvasForObject(d)
	tapPage := func(x, y float64) {
		t.Helper()
		pos := linkPoint(d, 0, x, y).Subtract(d.scrollContainer.Offset)
		test.TapCanvas(c, fyne.CurrentApp().Driver().AbsolutePositionForObject(d.scrollContainer).Add(pos))
	}
	tapPage(200, 92)
	if p.selected != (annotKey{0, 2}) || p.rows[annotKey{1, 0}].selected() {
		t.Fatalf("after tapping the highlight: selected %v", p.selected)
	}
	if p.popup == nil || !p.popup.Visible() {
		t.Fatal("no popup for the tapped highlight")
	}
	if replies := p.replies(p.rows[annotKey{0, 2}].annot); len(replies) != 2 || replies[0].Contents != "Agreed" {
		t.Errorf("thread of the highlight: %+v", replies)
	}

	// A tap beside the annotations clears the selection.
	p.hidePopup()
	tapPage(500, 500)
	if p.hasSelected || len(currentView(d).highlights) != 0 {
		t.Error("selection not cleared by a tap beside the annotations")
	}

	// Links take precedence over annotations.
	var opened bool
	d.OnLinkTapped = func(Link) bool { opened = true; return true }
	tapPage(150, 682)
	if !opened {
		t.Error("link not followed")
	}
}
