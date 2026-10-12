package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
)

// textPDF builds a PDF with a 612x792 pt page for each entry of pages,
// showing its lines in 12 pt Helvetica from the top left, one below the
// other.
func textPDF(pages ...[]string) []byte {
	kids := make([]string, len(pages))
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 4+2*i)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	for i, lines := range pages {
		var content strings.Builder
		content.WriteString("BT /F1 12 Tf 72 720 Td")
		for _, line := range lines {
			fmt.Fprintf(&content, " (%s) Tj 0 -20 Td", line)
		}
		content.WriteString(" ET")
		objects = append(objects,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", content.Len(), content.String()),
		)
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// searchPDF has three pages; "gamma" is on pages 0 and 2, twice on 2.
func searchPDF() []byte {
	return textPDF(
		[]string{"Alpha beta gamma", "delta"},
		[]string{"nothing to find here"},
		[]string{"Gamma ray", "omega gammas"},
	)
}

func TestSourceSearchPage(t *testing.T) {
	src := mustOpen(t, searchPDF())
	ctx := context.Background()

	results, err := src.SearchPage(ctx, 0, "gamma", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v, want one", results)
	}
	r := results[0]
	if r.Page != 0 || r.Text != "gamma" || !strings.HasSuffix(r.Prefix, "beta ") {
		t.Errorf("result = %+v", r)
	}
	if len(r.Rects) != 1 {
		t.Fatalf("rects = %v, want one line", r.Rects)
	}
	// The first line's baseline is at 792-720 = 72 pt from the top.
	b := r.Bounds()
	if b.X0 <= 72 || b.X1 <= b.X0 || b.Y0 > 72 || b.Y1 < 62 || b.Y1 > 80 {
		t.Errorf("bounds = %+v, want right of x 72 around the first baseline", b)
	}

	if got, _ := src.SearchPage(ctx, 2, "gamma", SearchOptions{}); len(got) != 2 {
		t.Errorf("page 2: %d results, want 2 (case-insensitive, in a word)", len(got))
	}
	if got, _ := src.SearchPage(ctx, 2, "gamma", SearchOptions{MatchCase: true}); len(got) != 1 {
		t.Errorf("page 2 matching case: %d results, want 1", len(got))
	}
	if got, _ := src.SearchPage(ctx, 2, "gamma", SearchOptions{WholeWord: true}); len(got) != 1 {
		t.Errorf("page 2 whole words: %d results, want 1", len(got))
	}
	if got, _ := src.SearchPage(ctx, 0, "  ", SearchOptions{}); len(got) != 0 {
		t.Errorf("blank query found %v", got)
	}
	if _, err := src.SearchPage(ctx, 3, "gamma", SearchOptions{}); err == nil {
		t.Error("page out of range: want an error")
	}

	text, err := src.PageText(ctx, 0)
	if err != nil || !strings.Contains(text, "Alpha beta gamma") || !strings.Contains(text, "delta") {
		t.Errorf("PageText = %q, %v", text, err)
	}
}

func TestSourceSearch(t *testing.T) {
	src := mustOpen(t, searchPDF())

	results, err := src.Search(context.Background(), "gamma", SearchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var pages []int
	for _, r := range results {
		pages = append(pages, r.Page)
	}
	if fmt.Sprint(pages) != "[0 2 2]" {
		t.Errorf("pages of the results = %v, want [0 2 2]", pages)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.Search(ctx, "gamma", SearchOptions{}); err == nil {
		t.Error("cancelled search: want an error")
	}

	_ = src.Close()
	if _, err := src.SearchPage(context.Background(), 0, "gamma", SearchOptions{}); err == nil {
		t.Error("search after Close: want an error")
	}
}

// searchAndWait searches d and applies the results.
func searchAndWait(d *Document, query string, opt SearchOptions) {
	d.Search(query, opt)
	settle(d)
}

func TestDocumentSearch(t *testing.T) {
	d := newContinuousDocument(t, searchPDF())
	var changes int
	d.OnSearchChanged = func() { changes++ }

	searchAndWait(d, "gamma", SearchOptions{})
	if d.Searching() {
		t.Error("still searching after settle")
	}
	if n := len(d.SearchResults()); n != 3 {
		t.Fatalf("%d results, want 3", n)
	}
	if searched, total := d.SearchProgress(); searched != 3 || total != 3 {
		t.Errorf("progress = %d/%d, want 3/3", searched, total)
	}
	if changes == 0 {
		t.Error("OnSearchChanged was not called")
	}
	if d.CurrentSearchResult() != 0 {
		t.Errorf("current = %d, want the first result", d.CurrentSearchResult())
	}

	// The results are highlighted on the pages shown, the current one in
	// its own color.
	h := d.views[0].Highlights()
	if len(h) != 1 || h[0].Color != defaultCurrentHighlightColor {
		t.Fatalf("highlights of page 0 = %+v", h)
	}
	b := d.SearchResults()[0].Bounds()
	if !approx(float64(h[0].Position.X), b.X0*unitsPerPoint) || !approx(float64(h[0].Size.Width), (b.X1-b.X0)*unitsPerPoint) {
		t.Errorf("highlight %+v does not cover %+v", h[0], b)
	}

	// Next goes to page 2, in view and current.
	d.NextSearchResult()
	if d.CurrentSearchResult() != 1 || d.CurrentPage() != 2 {
		t.Fatalf("after next: current = %d on page %d, want 1 on page 2", d.CurrentSearchResult(), d.CurrentPage())
	}
	assertResultInView(t, d, 1)
	h = d.views[2].Highlights()
	if len(h) != 2 || h[0].Color != defaultCurrentHighlightColor || h[1].Color != defaultHighlightColor {
		t.Errorf("highlights of page 2 = %+v", h)
	}

	d.NextSearchResult()
	d.NextSearchResult()
	if d.CurrentSearchResult() != 0 || d.CurrentPage() != 0 {
		t.Errorf("next wraps to the first result: current = %d on page %d", d.CurrentSearchResult(), d.CurrentPage())
	}
	d.PreviousSearchResult()
	if d.CurrentSearchResult() != 2 {
		t.Errorf("previous wraps to the last result: current = %d", d.CurrentSearchResult())
	}
	assertResultInView(t, d, 2)

	d.ClearSearch()
	if len(d.SearchResults()) != 0 || d.CurrentSearchResult() != -1 || d.SearchQuery() != "" {
		t.Error("ClearSearch left results")
	}
	for page, view := range d.views {
		if len(view.Highlights()) != 0 {
			t.Errorf("page %d still highlighted", page)
		}
	}
}

// assertResultInView checks that result i of d is within the view.
func assertResultInView(t *testing.T, d *Document, i int) {
	t.Helper()
	r := d.SearchResults()[i]
	b := r.Bounds()
	origin := d.layout.origin(r.Page, d.areaSize())
	scale := float32(unitsPerPoint * d.scale)
	top := origin.Y + float32(b.Y0)*scale
	bottom := origin.Y + float32(b.Y1)*scale
	offset, view := d.scrollContainer.Offset, d.scrollContainer.Size()
	if top < offset.Y || bottom > offset.Y+view.Height {
		t.Errorf("result %d (%v..%v) not in view (%v..%v)", i, top, bottom, offset.Y, offset.Y+view.Height)
	}
}

func TestDocumentSearchStartsAtCurrentPage(t *testing.T) {
	d := newContinuousDocument(t, searchPDF())
	d.ShowPage(1)
	settle(d)

	searchAndWait(d, "gamma", SearchOptions{})
	// Page 1 has none: the first result after it, on page 2. The results
	// stay in document order.
	results := d.SearchResults()
	if cur := d.CurrentSearchResult(); cur != 1 || results[cur].Page != 2 {
		t.Errorf("current = %d, want 1 (the first on page 2)", cur)
	}
	if results[0].Page != 0 {
		t.Errorf("results not in document order: %+v", results)
	}
}

func TestDocumentSearchSinglePage(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, searchPDF())

	searchAndWait(d, "omega", SearchOptions{})
	if d.CurrentPage() != 2 || !d.layout.contains(2) {
		t.Fatalf("current page = %d, want the result's page 2 shown", d.CurrentPage())
	}
	if h := currentView(d).Highlights(); len(h) != 1 {
		t.Errorf("highlights = %+v, want one", h)
	}
}

func TestDocumentSearchNothing(t *testing.T) {
	d := newContinuousDocument(t, searchPDF())
	searchAndWait(d, "zeta", SearchOptions{})
	if len(d.SearchResults()) != 0 || d.CurrentSearchResult() != -1 || d.SearchQuery() != "zeta" {
		t.Error("search for a missing word")
	}
	// Nothing to go to.
	d.NextSearchResult()
	d.PreviousSearchResult()
	d.ShowSearchResult(0)
}

func TestDocumentSearchReplaced(t *testing.T) {
	d := newContinuousDocument(t, searchPDF())
	d.Search("gamma", SearchOptions{})
	// A new search before the results of the first arrive drops them.
	d.Search("delta", SearchOptions{})
	settle(d)
	results := d.SearchResults()
	if len(results) != 1 || results[0].Text != "delta" {
		t.Errorf("results = %+v, want only delta", results)
	}

	// Loading another document ends the search.
	loadAndWait(t, d, searchPDF())
	if len(d.SearchResults()) != 0 || d.SearchQuery() != "" {
		t.Error("search survived loading a document")
	}
}

func TestDocumentSearchColors(t *testing.T) {
	d := newContinuousDocument(t, searchPDF())
	mark := fyne.CurrentApp().Settings().Theme().Color("primary", 0)
	d.CurrentHighlightColor = mark
	searchAndWait(d, "delta", SearchOptions{})
	if h := d.views[0].Highlights(); len(h) != 1 || h[0].Color != mark {
		t.Errorf("highlights = %+v, want the current one in %v", h, mark)
	}
}

func TestPageHighlights(t *testing.T) {
	p := NewPage()
	p.SetPageSize(fyne.NewSize(100, 200))
	p.Scale = 2
	p.Resize(fyne.NewSize(300, 400))
	p.SetHighlights([]Highlight{{Position: fyne.NewPos(10, 20), Size: fyne.NewSize(30, 5), Color: defaultHighlightColor}})

	r := p.CreateRenderer()
	r.Layout(p.Size())
	objects := r.Objects()
	if len(objects) != 3 {
		t.Fatalf("%d objects, want background, image and one highlight", len(objects))
	}
	// The page (200x400) is centred horizontally at x 50.
	if pos, size := objects[2].Position(), objects[2].Size(); pos != fyne.NewPos(70, 40) || size != fyne.NewSize(60, 10) {
		t.Errorf("highlight at %v, %v; want 70,40 60x10", pos, size)
	}

	p.SetHighlights(nil)
	r.Refresh()
	if n := len(r.Objects()); n != 2 {
		t.Errorf("%d objects after removing the highlights, want 2", n)
	}
}
