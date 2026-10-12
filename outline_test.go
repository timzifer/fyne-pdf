package pdf

import (
	"fmt"
	"image/color"
	"math"
	"net/url"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// outlinePDF has three pages of 612x792 pt and the outline
//
//	Chapter 1 (bold, open)  → page 1, XYZ 72 720 at 200 %
//	  Section 1.1           → page 2, FitH 500
//	Chapter 2 (italic, red, closed) → page 3, Fit
//	  Section 2.1           → page 3, FitR 100 100 300 300
//	Web                     → https://example.com/
//	Heading                 → nowhere
func outlinePDF() []byte {
	content := "1 0 0 rg 100 100 200 200 re f"
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R >>"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /Outlines 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		page, page, page,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Outlines /First 8 0 R /Last 12 0 R /Count 4 >>",
		"<< /Title (Chapter 1) /Parent 7 0 R /Next 10 0 R /First 9 0 R /Last 9 0 R /Count 1 /F 2 /Dest [3 0 R /XYZ 72 720 2] >>",
		"<< /Title (Section 1.1) /Parent 8 0 R /Dest [4 0 R /FitH 500] >>",
		"<< /Title (Chapter 2) /Parent 7 0 R /Prev 8 0 R /Next 12 0 R /First 11 0 R /Last 11 0 R /Count -1 /F 1 /C [1 0 0] /Dest [5 0 R /Fit] >>",
		"<< /Title (Section 2.1) /Parent 10 0 R /Dest [5 0 R /FitR 100 100 300 300] >>",
		"<< /Title (Web) /Parent 7 0 R /Prev 10 0 R /Next 13 0 R /A << /S /URI /URI (https://example.com/) >> >>",
		"<< /Title (Heading) /Parent 7 0 R /Prev 12 0 R >>",
	}, "")
}

func TestSourceOutline(t *testing.T) {
	src, err := OpenSource(outlinePDF())
	if err != nil {
		t.Fatal(err)
	}
	items := src.Outline()
	if len(items) != 4 {
		t.Fatalf("%d top entries, want 4", len(items))
	}
	ch1, ch2, web, heading := items[0], items[1], items[2], items[3]
	if ch1.Title != "Chapter 1" || !ch1.Bold || ch1.Italic || !ch1.Open || len(ch1.Children) != 1 {
		t.Errorf("chapter 1: %+v", ch1)
	}
	if d := ch1.Dest; d == nil || d.Page != 0 || d.Fit != "XYZ" || d.X != 72 || d.Y != 72 || d.Zoom != 2 {
		t.Errorf("chapter 1 leads to %+v, want page 0, XYZ 72 72 at 2", d)
	}
	if d := ch1.Children[0].Dest; d == nil || d.Page != 1 || d.Fit != "FitH" || d.Y != 292 || !math.IsNaN(d.X) {
		t.Errorf("section 1.1 leads to %+v, want page 1, FitH 292", d)
	}
	if ch2.Open || !ch2.Italic || ch2.Color != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("chapter 2: open %v, italic %v, colour %v", ch2.Open, ch2.Italic, ch2.Color)
	}
	if d := ch2.Children[0].Dest; d == nil || d.Fit != "FitR" || d.Rect != (Rect{100, 492, 300, 692}) {
		t.Errorf("section 2.1 leads to %+v", d)
	}
	if web.URI != "https://example.com/" || web.Dest != nil {
		t.Errorf("web: %+v", web)
	}
	if heading.Dest != nil || heading.URI != "" {
		t.Errorf("heading: %+v", heading)
	}

	plain, _ := OpenSource(minimalPDF())
	if o := plain.Outline(); o != nil {
		t.Errorf("PDF without outline: %v", o)
	}
}

func TestDocumentOutlinePanel(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(2))
	if d.outlineButton.Visible() || d.OutlineShown() {
		t.Fatal("outline button or panel shown for a PDF without outline")
	}

	loadAndWait(t, d, outlinePDF())
	if !d.outlineButton.Visible() || d.OutlineShown() {
		t.Fatal("want the outline button, and the panel hidden at first")
	}
	viewWidth := d.scrollContainer.Size().Width
	test.Tap(d.outlineButton)
	if !d.OutlineShown() {
		t.Fatal("panel not shown after tapping the button")
	}
	if w := d.scrollContainer.Size().Width; w > viewWidth-outlineWidth+1 {
		t.Errorf("view %v wide beside the outline, was %v", w, viewWidth)
	}
	tree := d.outline.tree
	if !tree.IsBranchOpen("0") || tree.IsBranchOpen("1") {
		t.Error("branches not opened as the outline says")
	}

	// Loading a PDF without outline hides the panel and the button.
	loadAndWait(t, d, minimalPDF())
	if d.OutlineShown() || d.outlineButton.Visible() {
		t.Error("outline still shown for a PDF without one")
	}
}

func TestDocumentGoTo(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, outlinePDF())
	items := d.Outline()

	at := func(page int, x, y float64) fyne.Position {
		scale := d.scale * unitsPerPoint
		return d.layout.origin(page, d.areaSize()).AddXY(float32(x*scale), float32(y*scale))
	}
	near := func(a, b fyne.Position) bool {
		return math.Abs(float64(a.X-b.X)) < 1 && math.Abs(float64(a.Y-b.Y)) < 1
	}

	d.GoTo(*items[0].Dest) // XYZ 72 72 at 200 %
	settle(d)
	if d.CurrentPage() != 0 || !approx(d.scale, 2) || d.fitHorizontal || d.fitVertical {
		t.Errorf("XYZ: page %d, zoom %v, fit %v %v", d.CurrentPage(), d.scale, d.fitHorizontal, d.fitVertical)
	}
	if o := d.scrollContainer.Offset; !near(o, at(0, 72, 72)) {
		t.Errorf("XYZ: offset %v, want %v", o, at(0, 72, 72))
	}

	d.GoTo(*items[0].Children[0].Dest) // FitH 292
	settle(d)
	if d.CurrentPage() != 1 || !d.fitHorizontal || d.fitVertical {
		t.Errorf("FitH: page %d, fit %v %v", d.CurrentPage(), d.fitHorizontal, d.fitVertical)
	}
	if o := d.scrollContainer.Offset; !approx(float64(o.Y), float64(at(1, 0, 292).Y)) {
		t.Errorf("FitH: offset %v, want y %v", o, at(1, 0, 292).Y)
	}

	d.GoTo(*items[1].Children[0].Dest) // FitR 100 492 300 692
	settle(d)
	view := d.scrollContainer.Size()
	want := math.Min(float64(view.Width)/(200*unitsPerPoint), float64(view.Height)/(200*unitsPerPoint))
	if d.CurrentPage() != 2 || !approx(d.scale, clampZoom(want)) {
		t.Errorf("FitR: page %d, zoom %v, want %v", d.CurrentPage(), d.scale, want)
	}
	centre := d.scrollContainer.Offset.AddXY(view.Width/2, view.Height/2)
	if c := at(2, 200, 592); !near(centre, c) {
		t.Errorf("FitR: centre of the view %v, want the rectangle's %v", centre, c)
	}

	d.GoTo(*items[1].Dest) // Fit
	settle(d)
	if d.CurrentPage() != 2 || !d.fitHorizontal || !d.fitVertical || !approx(d.scale, fittedZoom(d)) {
		t.Errorf("Fit: page %d, zoom %v", d.CurrentPage(), d.scale)
	}

	d.GoTo(Destination{Page: 7, Fit: "Fit"}) // out of range
	if d.CurrentPage() != 2 {
		t.Errorf("out of range: page %d", d.CurrentPage())
	}
}

func TestDocumentOutlineFollowAndMark(t *testing.T) {
	d := newTestDocument(t)
	var opened []string
	d.openURL = func(u *url.URL) error { opened = append(opened, u.String()); return nil }
	loadAndWait(t, d, outlinePDF())
	d.ShowOutline(true)
	p := d.outline

	if p.current != "0" {
		t.Errorf("marked %q on page 1, want chapter 1", p.current)
	}
	e := newOutlineEntry(p)
	e.set("0/0")
	test.Tap(e)
	settle(d)
	if d.CurrentPage() != 1 || p.current != "0/0" {
		t.Errorf("after tapping section 1.1: page %d, marked %q", d.CurrentPage(), p.current)
	}
	// Tapping the marked entry again goes there again.
	d.scrollTo(fyne.Position{})
	test.Tap(e)
	if o := d.scrollContainer.Offset; o.Y == 0 {
		t.Error("tapping the marked entry again did not go there")
	}

	d.ShowPage(2)
	if p.current != "1" {
		t.Errorf("page 3 marks %q, want chapter 2 (section 2.1 is in a closed branch)", p.current)
	}
	p.tree.OpenBranch("1")
	if p.current != "1/0" {
		t.Errorf("after opening chapter 2: marked %q, want section 2.1", p.current)
	}

	e.set("2")
	test.Tap(e)
	if len(opened) != 1 || opened[0] != "https://example.com/" {
		t.Errorf("web entry opened %v", opened)
	}
	e.set("3") // leads nowhere
	test.Tap(e)
	if d.CurrentPage() != 2 {
		t.Errorf("heading changed the page to %d", d.CurrentPage())
	}
}

func TestDocumentGoToContinuous(t *testing.T) {
	d := newTestDocument(t)
	d.SetViewMode(ViewContinuous)
	loadAndWait(t, d, outlinePDF())
	dest := *d.Outline()[0].Children[0].Dest // page 2, FitH 292
	dest.Page = 2
	d.GoTo(dest)
	settle(d)
	scale := d.scale * unitsPerPoint
	want := d.layout.origin(2, d.areaSize()).Y + float32(292*scale)
	if o := d.scrollContainer.Offset; d.CurrentPage() != 2 || !approx(float64(o.Y), float64(want)) {
		t.Errorf("page %d, offset %v, want page 2 at y %v", d.CurrentPage(), o, want)
	}
}
