package pdf

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// viewSettingsPDF builds a PDF of four pages (objects 3 to 6) and an
// outline of one entry, with catalog added to its catalogue.
func viewSettingsPDF(catalog string) []byte {
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"
	return buildPDF([]string{
		fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /Outlines 7 0 R %s >>", catalog),
		"<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R 6 0 R] /Count 4 >>",
		page, page, page, page,
		"<< /Type /Outlines /First 8 0 R /Last 8 0 R /Count 1 >>",
		"<< /Title (Start) /Parent 7 0 R /Dest [3 0 R /Fit] >>",
	}, "")
}

func TestSourceViewSettings(t *testing.T) {
	settings := func(catalog string) ViewSettings {
		t.Helper()
		src, err := OpenSource(viewSettingsPDF(catalog))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = src.Close() }()
		return src.ViewSettings()
	}

	if v := settings(""); v.Open != nil || v.PageMode != "UseNone" || v.PageLayout != "SinglePage" || v.RightToLeft || v.DisplayDocTitle {
		t.Errorf("a PDF that asks for nothing: %+v", v)
	}
	if o := settings("/OpenAction [5 0 R /XYZ 72 720 2]").Open; o == nil || o.Page != 2 || o.Fit != "XYZ" || o.X != 72 || o.Y != 72 || o.Zoom != 2 {
		t.Errorf("destination: %+v, want page 2, XYZ 72 72 at 2", o)
	}
	if o := settings("/OpenAction << /S /GoTo /D [6 0 R /Fit] >>").Open; o == nil || o.Page != 3 || o.Fit != "Fit" {
		t.Errorf("GoTo action: %+v, want page 3, Fit", o)
	}
	if o := settings("/OpenAction << /S /Named /N /LastPage >>").Open; o == nil || o.Page != 3 {
		t.Errorf("named action: %+v, want page 3", o)
	}
	for _, action := range []string{
		"<< /S /URI /URI (https://example.com/) >>",
		"<< /S /JavaScript /JS (app.alert(1)) >>",
		"<< /S /Launch /F (other.pdf) >>",
		"<< /S /GoToR /F (other.pdf) /D [0 /Fit] >>",
		"<< /S /Named /N /Print >>",
	} {
		if o := settings("/OpenAction " + action).Open; o != nil {
			t.Errorf("%s: opens at %+v, want nowhere", action, o)
		}
	}
	v := settings("/PageMode /UseOutlines /PageLayout /OneColumn /ViewerPreferences << /Direction /R2L /DisplayDocTitle true >>")
	if v.PageMode != "UseOutlines" || v.PageLayout != "OneColumn" || !v.RightToLeft || !v.DisplayDocTitle {
		t.Errorf("settings: %+v", v)
	}

	src, err := OpenSource(viewSettingsPDF("/PageMode /FullScreen"))
	if err != nil {
		t.Fatal(err)
	}
	if v := src.ViewSettings(); v.PageMode != "FullScreen" {
		t.Errorf("page mode = %q, want FullScreen", v.PageMode)
	}
	_ = src.Close()
	if v := src.ViewSettings(); v.PageMode != "UseNone" || v.PageLayout != "SinglePage" {
		t.Errorf("settings after Close: %+v", v)
	}
}

func TestDocumentFollowsViewSettings(t *testing.T) {
	d := newTestDocument(t)
	if v := d.ViewSettings(); v.Open != nil || v.PageMode != "UseNone" || v.PageLayout != "SinglePage" {
		t.Errorf("settings without a document: %+v", v)
	}

	// Nothing asked for: the first page, as the widget is set.
	loadAndWait(t, d, viewSettingsPDF(""))
	if d.CurrentPage() != 0 || d.ViewMode() != ViewSinglePage || d.OutlineShown() {
		t.Errorf("page %d, mode %v, outline %v, want 0, single page, hidden", d.CurrentPage(), d.ViewMode(), d.OutlineShown())
	}

	// The settings are applied when OnLoaded is called.
	var page int
	var zoom float64
	d.OnLoaded = func() { page, zoom = d.CurrentPage(), d.scale }
	loadAndWait(t, d, viewSettingsPDF("/OpenAction [5 0 R /XYZ 0 792 2]"))
	if page != 2 || zoom != 2 {
		t.Errorf("OnLoaded saw page %d at zoom %v, want 2 at 2", page, zoom)
	}
	d.OnLoaded = nil
	if z := zoomOf(t, d); d.CurrentPage() != 2 || z != 2 {
		t.Errorf("page %d at zoom %v, want 2 at 2", d.CurrentPage(), z)
	}
	if v := d.ViewSettings(); v.Open == nil || v.Open.Page != 2 {
		t.Errorf("Document.ViewSettings: %+v", v)
	}

	loadAndWait(t, d, viewSettingsPDF("/PageMode /UseOutlines /OpenAction << /S /Named /N /LastPage >>"))
	if !d.OutlineShown() {
		t.Error("outline hidden for UseOutlines")
	}
	if d.CurrentPage() != 3 {
		t.Errorf("page %d, want the last", d.CurrentPage())
	}

	d.ShowOutline(false)
	loadAndWait(t, d, viewSettingsPDF("/PageLayout /OneColumn /OpenAction [4 0 R /Fit]"))
	if d.ViewMode() != ViewContinuous {
		t.Error("not continuous for OneColumn")
	}
	if d.CurrentPage() != 1 || !d.fitHorizontal || !d.fitVertical {
		t.Errorf("page %d, fit %v %v, want 1, fitted", d.CurrentPage(), d.fitHorizontal, d.fitVertical)
	}
	if top := d.layout.origin(1, d.areaSize()).Y; d.scrollContainer.Offset.Y != top {
		t.Errorf("offset %v, want the top of page 1 at %v", d.scrollContainer.Offset.Y, top)
	}
	if d.OutlineShown() {
		t.Error("outline shown though the PDF does not ask for it")
	}

	// An application that has a view of its own keeps it.
	d.FollowViewSettings = false
	d.SetViewMode(ViewSinglePage)
	loadAndWait(t, d, viewSettingsPDF("/PageMode /UseOutlines /PageLayout /OneColumn /OpenAction [5 0 R /Fit]"))
	if d.CurrentPage() != 0 || d.ViewMode() != ViewSinglePage || d.OutlineShown() {
		t.Errorf("not following: page %d, mode %v, outline %v", d.CurrentPage(), d.ViewMode(), d.OutlineShown())
	}
}

// TestViewSettingsBeforeShown loads a PDF before the widget is on a
// window, as the example in the README does.
func TestViewSettingsBeforeShown(t *testing.T) {
	for _, mode := range []ViewMode{ViewSinglePage, ViewContinuous} {
		d := newDocument(t)
		d.SetViewMode(mode)
		loadAndWait(t, d, viewSettingsPDF("/OpenAction [5 0 R /XYZ null 792 null]"))
		w := test.NewTempWindow(t, d)
		w.Resize(fyne.NewSize(800, 600))
		settle(d)
		if d.CurrentPage() != 2 {
			t.Errorf("mode %v: page %d once shown, want 2", mode, d.CurrentPage())
		}
		if top := d.layout.origin(2, d.areaSize()).Y; d.scrollContainer.Offset.Y != top {
			t.Errorf("mode %v: offset %v once shown, want the top of page 2 at %v", mode, d.scrollContainer.Offset.Y, top)
		}
	}
}
