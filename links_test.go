package pdf

import (
	"fmt"
	"net/url"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
)

// linksPDF has two pages of 612x792 pt; on the first, three links of
// 200x50 pt from x 100: to https://example.com/ at the top (y 142 of the
// displayed page), to the top of page 2 below it (y 242), to another file
// below that (y 342).
func linksPDF() []byte {
	content := "1 0 0 rg 100 100 200 200 re f"
	link := func(y int, action string) string {
		return fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [100 %d 300 %d] /Border [0 0 0] %s >>", y, y+50, action)
	}
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R /Annots [6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 5 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		link(600, "/A << /S /URI /URI (https://example.com/) >>"),
		link(500, "/Dest [4 0 R /XYZ 0 792 0]"),
		link(400, "/A << /S /GoToR /F (other.pdf) /D [0 /Fit] >>"),
	}, "")
}

func TestSourceLinks(t *testing.T) {
	src, err := OpenSource(linksPDF())
	if err != nil {
		t.Fatal(err)
	}
	links, err := src.Links(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 3 {
		t.Fatalf("%d links, want 3", len(links))
	}
	web, internal, external := links[0], links[1], links[2]
	if web.URI != "https://example.com/" || web.Dest != nil || len(web.Rects) != 1 || web.Rects[0] != (Rect{100, 142, 300, 192}) {
		t.Errorf("web link: %+v", web)
	}
	if !web.Contains(200, 160) || web.Contains(200, 200) {
		t.Error("web link: Contains wrong")
	}
	if d := internal.Dest; d == nil || d.Page != 1 || d.Fit != "XYZ" || d.Y != 0 {
		t.Errorf("internal link leads to %+v, want the top of page 2", d)
	}
	if !external.External || external.File != "other.pdf" || external.Dest != nil {
		t.Errorf("external link: %+v", external)
	}
	if links, err := src.Links(1); err != nil || len(links) != 0 {
		t.Errorf("page 2: %v links, err %v", links, err)
	}
	if _, err := src.Links(2); err == nil {
		t.Error("no error for a page out of range")
	}
}

// linkPoint returns the position in the page area of the point x, y of
// page.
func linkPoint(d *Document, page int, x, y float64) fyne.Position {
	scale := d.scale * unitsPerPoint
	return d.layout.origin(page, d.areaSize()).AddXY(float32(x*scale), float32(y*scale))
}

func TestDocumentFollowLinks(t *testing.T) {
	d := newTestDocument(t)
	var opened []string
	d.openURL = func(u *url.URL) error { opened = append(opened, u.String()); return nil }
	loadAndWait(t, d, linksPDF())
	c := fyne.CurrentApp().Driver().CanvasForObject(d)
	tap := func(x, y float64) {
		t.Helper()
		pos := linkPoint(d, d.CurrentPage(), x, y).Subtract(d.scrollContainer.Offset)
		test.TapCanvas(c, fyne.CurrentApp().Driver().AbsolutePositionForObject(d.scrollContainer).Add(pos))
		settle(d)
	}

	tap(200, 160)
	if len(opened) != 1 || opened[0] != "https://example.com/" {
		t.Errorf("web link opened %v", opened)
	}
	tap(200, 360) // other file: asked first
	if d.fileDialog == nil {
		t.Fatal("no question before opening another file")
	}
	d.fileDialog.Dismiss()
	tap(400, 400) // no link
	if len(opened) != 1 || d.CurrentPage() != 0 {
		t.Errorf("tap beside the links: opened %v, page %d", opened, d.CurrentPage())
	}

	var tapped []Link
	d.OnLinkTapped = func(l Link) bool { tapped = append(tapped, l); return l.External }
	tap(200, 360)
	if len(tapped) != 1 || !tapped[0].External {
		t.Errorf("OnLinkTapped got %v", tapped)
	}
	tap(200, 260)
	if len(tapped) != 2 || d.CurrentPage() != 1 {
		t.Errorf("internal link: OnLinkTapped %d times, page %d; want 2, page 2", len(tapped), d.CurrentPage())
	}
}

func TestDocumentLinkCursor(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, linksPDF())
	move := func(x, y float64) desktop.Cursor {
		d.area.MouseMoved(&desktop.MouseEvent{PointEvent: fyne.PointEvent{Position: linkPoint(d, 0, x, y)}})
		return d.area.Cursor()
	}
	if move(200, 160) != desktop.PointerCursor {
		t.Error("no hand over a link")
	}
	if move(400, 400) != desktop.DefaultCursor {
		t.Error("hand beside the links")
	}
	move(200, 260)
	d.area.MouseOut()
	if d.area.Cursor() != desktop.DefaultCursor {
		t.Error("hand after the pointer left")
	}
}
