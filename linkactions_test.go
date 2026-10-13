package pdf

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
)

// The links of linkActionsPDF, from the top of its first page; link i is
// 50 pt high at y 50+60*i of the displayed page, from x 100 to 300.
const (
	linkNext = iota
	linkLast
	linkBack
	linkPrint
	linkScript
	linkLaunch
	linkOtherPDF
	linkEmbedded
	linkFind
	linkSubmit
	linkCount
)

// linkActionsPDF builds a PDF of three pages with a link of each kind on
// the first.
func linkActionsPDF() []byte {
	actions := [linkCount]string{
		linkNext:     "<< /S /Named /N /NextPage >>",
		linkLast:     "<< /S /Named /N /LastPage >>",
		linkBack:     "<< /S /Named /N /GoBack >>",
		linkPrint:    "<< /S /Named /N /Print >>",
		linkScript:   "<< /S /JavaScript /JS (app.alert(1)) >>",
		linkLaunch:   "<< /S /Launch /F (setup.exe) >>",
		linkOtherPDF: "<< /S /GoToR /F (more/other.pdf) /D [2 /Fit] >>",
		linkEmbedded: "<< /S /GoToE /T << /R /C /N (inner.pdf) >> /D [0 /Fit] >>",
		linkFind:     "<< /S /Named /N /Find >>",
		linkSubmit:   "<< /S /SubmitForm /F (https://example.com/form) >>",
	}
	var annots []string
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		"", // the first page, below
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	for i, action := range actions {
		top := 792 - (50 + 60*i)
		annots = append(annots, fmt.Sprintf("%d 0 R", len(objects)+1))
		objects = append(objects, fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [100 %d 300 %d] /Border [0 0 0] /A %s >>", top-50, top, action))
	}
	objects[2] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [%s] >>", strings.Join(annots, " "))
	return buildPDF(objects, "")
}

// linkY is a y inside link i of linkActionsPDF.
func linkY(i int) float64 {
	return float64(50 + 60*i + 25)
}

func TestLinkDescriptions(t *testing.T) {
	src, err := OpenSource(linkActionsPDF())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	links, err := src.Links(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != linkCount {
		t.Fatalf("%d links, want %d", len(links), linkCount)
	}
	want := [linkCount]string{
		linkNext:     "Go to the next page",
		linkLast:     "Go to the last page",
		linkBack:     "Go back to the previous view",
		linkPrint:    "Print the document",
		linkScript:   "Would run a script (never done)",
		linkLaunch:   `Would start "setup.exe" (never done)`,
		linkOtherPDF: `Open the file "more/other.pdf"`,
		linkEmbedded: `Would open the embedded file "inner.pdf" (not supported)`,
		linkFind:     `Would run the viewer command "Find" (not supported)`,
		linkSubmit:   "Would send the form (not supported)",
	}
	for i, l := range links {
		if got := l.Description(); got != want[i] {
			t.Errorf("link %d (%+v): %q, want %q", i, l, got, want[i])
		}
	}
	if l := links[linkOtherPDF]; !l.External || l.File != "more/other.pdf" || l.Action != "GoToR" || l.Dest != nil {
		t.Errorf("link to another PDF: %+v", l)
	}
	if l := links[linkScript]; l.Action != "JavaScript" || l.Dest != nil || l.URI != "" {
		t.Errorf("script link: %+v", l)
	}

	for _, c := range []struct {
		link Link
		want string
	}{
		{Link{Dest: &Destination{Page: 11}}, "Go to page 12"},
		{Link{URI: "https://example.com/"}, "Open https://example.com/"},
		{Link{NamedAction: "GoForward"}, "Go forward to the next view"},
		{Link{NamedAction: "PrevPage"}, "Go to the previous page"},
		{Link{NamedAction: "FirstPage"}, "Go to the first page"},
		{Link{Action: "Hide"}, `Would do the action "Hide" (not supported)`},
		{Link{}, "Leads nowhere"},
	} {
		if got := c.link.Description(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.link, got, c.want)
		}
	}
}

func TestFollowNamedActions(t *testing.T) {
	d := newTestDocument(t)
	var opened []string
	d.openURL = func(u *url.URL) error { opened = append(opened, u.String()); return nil }
	loadAndWait(t, d, linkActionsPDF())
	d.Zoom(0.5)
	settle(d)
	tap := func(link int) {
		t.Helper()
		d.turnPage(0, false)
		d.area.Tapped(&fyne.PointEvent{Position: linkPoint(d, 0, 200, linkY(link))})
		settle(d)
	}
	hand := func(link int) bool {
		t.Helper()
		d.turnPage(0, false)
		d.area.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: linkPoint(d, 0, 200, linkY(link))}})
		return d.area.Cursor() == desktop.PointerCursor
	}

	// What the widget does not do gets no hand, and a tap does nothing.
	for _, link := range []int{linkScript, linkLaunch, linkEmbedded, linkFind, linkSubmit, linkPrint, linkBack} {
		if hand(link) {
			t.Errorf("hand over link %d", link)
		}
		tap(link)
		if d.CurrentPage() != 0 || len(opened) != 0 || d.fileDialog != nil {
			t.Fatalf("link %d: page %d, opened %v, dialog %v", link, d.CurrentPage(), opened, d.fileDialog != nil)
		}
	}

	if !hand(linkNext) || !hand(linkLast) {
		t.Error("no hand over the links to the next and the last page")
	}
	tap(linkNext)
	if d.CurrentPage() != 1 {
		t.Errorf("next page: page %d, want 1", d.CurrentPage())
	}
	tap(linkLast)
	if d.CurrentPage() != 2 {
		t.Errorf("last page: page %d, want 2", d.CurrentPage())
	}

	// There is a view to go back to now, which the link does.
	if !hand(linkBack) {
		t.Error("no hand over the link back though there is a view to go back to")
	}
	d.turnPage(2, false)
	d.area.Tapped(&fyne.PointEvent{Position: linkPoint(d, 2, 300, 300)})
	if !d.CanGoBack() {
		t.Fatal("nothing to go back to after following links")
	}
	tap(linkBack)
	if d.CurrentPage() != 0 || !d.CanGoForward() {
		t.Errorf("after the link back: page %d, can go forward %v", d.CurrentPage(), d.CanGoForward())
	}

	printed := 0
	d.OnPrint = func() { printed++ }
	if !hand(linkPrint) {
		t.Error("no hand over the print link with OnPrint set")
	}
	tap(linkPrint)
	if printed != 1 {
		t.Errorf("printed %d times, want 1", printed)
	}

	// OnLinkTapped is asked first, also for what the widget does not do.
	var tapped []string
	d.OnLinkTapped = func(l Link) bool { tapped = append(tapped, l.Action+l.NamedAction); return true }
	tap(linkScript)
	tap(linkNext)
	if strings.Join(tapped, " ") != "JavaScript NamedNextPage" || d.CurrentPage() != 0 {
		t.Errorf("OnLinkTapped got %v, page %d", tapped, d.CurrentPage())
	}
}

func TestLinkToAnotherPDF(t *testing.T) {
	d := newTestDocument(t)
	var opened []string
	d.openURL = func(u *url.URL) error { opened = append(opened, u.String()); return nil }
	loadAndWait(t, d, linkActionsPDF())
	d.Zoom(0.5)
	settle(d)
	tap := func() {
		t.Helper()
		d.area.Tapped(&fyne.PointEvent{Position: linkPoint(d, 0, 200, linkY(linkOtherPDF))})
		if d.fileDialog == nil {
			t.Fatal("no question before opening another file")
		}
		if len(opened) != 0 {
			t.Fatalf("opened %v before the question was answered", opened)
		}
	}
	d.area.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: linkPoint(d, 0, 200, linkY(linkOtherPDF))}})
	if d.area.Cursor() != desktop.PointerCursor {
		t.Error("no hand over the link to another PDF")
	}

	// The place of the document is not known: nothing to open.
	tap()
	d.fileDialog.Confirm()
	if len(opened) != 0 || d.fileDialog != nil {
		t.Errorf("opened %v without knowing where the document is", opened)
	}
	for _, o := range fyne.CurrentApp().Driver().CanvasForObject(d).Overlays().List() {
		o.Hide()
	}

	d.Dir = t.TempDir()
	tap()
	d.fileDialog.Dismiss()
	if len(opened) != 0 || d.fileDialog != nil {
		t.Errorf("opened %v though the question was answered with no", opened)
	}
	tap()
	d.fileDialog.Confirm()
	want := storage.NewFileURI(filepath.Join(d.Dir, "more", "other.pdf")).String()
	if len(opened) != 1 || opened[0] != want {
		t.Errorf("opened %v, want %v", opened, want)
	}
}

func TestGoBackAndForward(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, outlinePDF())
	if d.GoBack() || d.GoForward() || d.CanGoBack() || d.CanGoForward() {
		t.Fatal("somewhere to go without a jump")
	}

	d.Zoom(2)
	d.scrollTo(fyne.NewPos(120, 340))
	settle(d)
	start := d.scrollContainer.Offset

	d.GoTo(Destination{Page: 2, Fit: "Fit"})
	settle(d)
	if d.CurrentPage() != 2 || !d.fitHorizontal || !d.CanGoBack() || d.CanGoForward() {
		t.Fatalf("after the jump: page %d, fitted %v, back %v, forward %v", d.CurrentPage(), d.fitHorizontal, d.CanGoBack(), d.CanGoForward())
	}

	// Alt+Left returns to the page, the zoom and the place.
	d.area.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyLeft, Modifier: fyne.KeyModifierAlt})
	settle(d)
	if z := zoomOf(t, d); d.CurrentPage() != 0 || z != 2 || d.fitHorizontal || d.fitVertical {
		t.Errorf("after going back: page %d, zoom %v, fit %v %v; want 0, 2, none", d.CurrentPage(), z, d.fitHorizontal, d.fitVertical)
	}
	if o := d.scrollContainer.Offset; !approx(float64(o.X), float64(start.X)) || !approx(float64(o.Y), float64(start.Y)) {
		t.Errorf("offset after going back = %v, want %v", o, start)
	}
	if d.CanGoBack() || !d.CanGoForward() {
		t.Errorf("after going back: back %v, forward %v", d.CanGoBack(), d.CanGoForward())
	}

	d.area.TypedShortcut(&desktop.CustomShortcut{KeyName: fyne.KeyRight, Modifier: fyne.KeyModifierAlt})
	settle(d)
	if d.CurrentPage() != 2 || !d.fitHorizontal || !d.fitVertical {
		t.Errorf("after going forward: page %d, fit %v %v; want 2, fitted", d.CurrentPage(), d.fitHorizontal, d.fitVertical)
	}

	// A jump from a view gone back to drops the way forward; the outline
	// jumps as GoTo does.
	d.GoBack()
	d.outline.follow("1")
	settle(d)
	if d.CurrentPage() != 2 || d.CanGoForward() || !d.CanGoBack() {
		t.Errorf("after an outline entry: page %d, back %v, forward %v", d.CurrentPage(), d.CanGoBack(), d.CanGoForward())
	}

	// No more views are kept than maxViewHistory.
	for range maxViewHistory + 10 {
		d.GoTo(Destination{Page: 1, Fit: "Fit"})
	}
	if n := len(d.jumps.back); n != maxViewHistory {
		t.Errorf("%d views kept, want %d", n, maxViewHistory)
	}

	// Another document starts anew.
	loadAndWait(t, d, outlinePDF())
	if d.CanGoBack() || d.CanGoForward() {
		t.Error("views of the document before kept")
	}
}

func TestLinkHovered(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, linkActionsPDF())
	d.Zoom(0.5)
	settle(d)
	var hovered []string
	d.OnLinkHovered = func(l *Link) {
		if l == nil {
			hovered = append(hovered, "none")
		} else {
			hovered = append(hovered, l.Description())
		}
	}
	move := func(x, y float64) {
		d.area.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: linkPoint(d, 0, x, y)}})
	}
	move(400, 400)
	move(150, linkY(linkNext))
	move(250, linkY(linkNext))
	move(250, linkY(linkScript))
	move(400, 400)
	move(250, linkY(linkLast))
	d.area.MouseOut()
	d.area.MouseOut()
	want := "Go to the next page|Would run a script (never done)|none|Go to the last page|none"
	if got := strings.Join(hovered, "|"); got != want {
		t.Errorf("hovered: %s\nwant:    %s", got, want)
	}
}
