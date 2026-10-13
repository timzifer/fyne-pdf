package pdf

import (
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// labelsPDF builds a PDF of ten pages labelled i to iv, 1 to 3 and A-3 to
// A-5.
func labelsPDF() []byte {
	const pages = 10
	kids := make([]string, pages)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R /PageLabels << /Nums [0 << /S /r >> 4 << /S /D >> 7 << /S /D /P (A-) /St 3 >>] >> >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages),
	}
	for range pages {
		objects = append(objects, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>")
	}
	return buildPDF(objects, "")
}

var wantLabels = []string{"i", "ii", "iii", "iv", "1", "2", "3", "A-3", "A-4", "A-5"}

func TestSourcePageLabels(t *testing.T) {
	src, err := OpenSource(labelsPDF())
	if err != nil {
		t.Fatal(err)
	}
	for page, want := range wantLabels {
		if got := src.PageLabel(page); got != want {
			t.Errorf("label of page %d = %q, want %q", page, got, want)
		}
		if got, ok := src.PageByLabel(want); !ok || got != page {
			t.Errorf("page labelled %q = %d, %v, want %d", want, got, ok, page)
		}
	}
	if page, ok := src.PageByLabel("v"); ok {
		t.Errorf("page labelled v = %d, want none", page)
	}
	if got := src.PageLabel(len(wantLabels)); got != "" {
		t.Errorf("label of a page past the end = %q", got)
	}

	_ = src.Close()
	if got := src.PageLabel(0); got != "" {
		t.Errorf("label after Close = %q", got)
	}
	if _, ok := src.PageByLabel("i"); ok {
		t.Error("page found by its label after Close")
	}

	plain, err := OpenSource(multiPagePDF(2))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plain.Close() }()
	if got := plain.PageLabel(0); got != "" {
		t.Errorf("label in a document without labels = %q", got)
	}
	if _, ok := plain.PageByLabel("1"); ok {
		t.Error("page found by a label in a document without labels")
	}
}

func TestDocumentPageLabels(t *testing.T) {
	d := newTestDocument(t)
	if !d.pageEntry.Disabled() || d.pageEntry.Text != "" || d.pagePosition.Text != "" {
		t.Errorf("page entry without a document: %q %q, disabled %v", d.pageEntry.Text, d.pagePosition.Text, d.pageEntry.Disabled())
	}
	loadAndWait(t, d, labelsPDF())
	shows := func(page int, entry, position string) {
		t.Helper()
		settle(d)
		if p := d.CurrentPage(); p != page {
			t.Fatalf("page = %d, want %d", p, page)
		}
		if d.pageEntry.Text != entry || d.pagePosition.Text != position {
			t.Errorf("page entry shows %q %q, want %q %q", d.pageEntry.Text, d.pagePosition.Text, entry, position)
		}
	}
	submit := func(text string) {
		t.Helper()
		d.pageEntry.SetText(text)
		d.pageEntry.OnSubmitted(text)
	}

	shows(0, "i", "(1 of 10)")
	if d.pageEntry.Disabled() {
		t.Error("page entry disabled with a document")
	}
	for page, want := range wantLabels {
		if got := d.PageLabel(page); got != want {
			t.Errorf("label of page %d = %q, want %q", page, got, want)
		}
		if got := d.thumbnails[page].label.Text; got != want {
			t.Errorf("caption of thumbnail %d = %q, want %q", page, got, want)
		}
	}
	if got := d.PageLabel(-1) + d.PageLabel(10); got != "" {
		t.Errorf("label of a page out of range = %q", got)
	}

	if !d.GoToLabel("A-4") {
		t.Fatal("GoToLabel(A-4) = false")
	}
	shows(8, "A-4", "(9 of 10)")
	if d.GoToLabel("B-1") {
		t.Error("GoToLabel(B-1) = true")
	}
	shows(8, "A-4", "(9 of 10)")

	// The entry takes a label first: "2" is the page that says 2, the
	// sixth. A number that is no label counts the pages.
	submit(" iii ")
	shows(2, "iii", "(3 of 10)")
	submit("2")
	shows(5, "2", "(6 of 10)")
	submit("9")
	shows(8, "A-4", "(9 of 10)")
	// Anything else leaves the page and is put right.
	for _, text := range []string{"11", "0", "nowhere", ""} {
		submit(text)
		shows(8, "A-4", "(9 of 10)")
	}
	if c := fyne.CurrentApp().Driver().CanvasForObject(d); c.Focused() != d.area {
		t.Error("page view not focused after going to a page")
	}

	// Other ways to another page are followed.
	d.area.TypedKey(&fyne.KeyEvent{Name: fyne.KeyHome})
	shows(0, "i", "(1 of 10)")
	test.Tap(d.thumbnails[4])
	shows(4, "1", "(5 of 10)")
	zoom := d.scale
	submit("iv")
	shows(3, "iv", "(4 of 10)")
	if d.scale != zoom {
		t.Errorf("zoom after going to a page = %v, want %v kept", d.scale, zoom)
	}

	// Without labels the entry counts the pages.
	loadAndWait(t, d, multiPagePDF(3))
	shows(0, "1", "of 3")
	if got := d.thumbnails[2].label.Text; got != "3" {
		t.Errorf("caption of thumbnail 2 = %q, want 3", got)
	}
	submit("3")
	shows(2, "3", "of 3")
	if d.GoToLabel("1") {
		t.Error("GoToLabel(1) = true in a document without labels")
	}
}
