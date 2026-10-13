package pdf

import (
	"bytes"
	"testing"
	"time"
)

// metadataPDF builds a PDF of two pages, the second in landscape, that
// says who made it.
func metadataPDF() []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 842 595] >>",
		"<< /Title (Old title) /Author (A. Writer) /Subject (Testing) /Producer (fyne-pdf tests) /CreationDate (D:20260102030405Z) /Department (QA) >>",
	}, "/Info 5 0 R")
}

func TestSetMetadata(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, metadataPDF())
	changes := 0
	d.OnChanged = func() { changes++ }
	d.OnError = func(_ int, err error) { t.Error(err) }

	m := d.Metadata()
	if m.Title != "Old title" || m.Author != "A. Writer" || m.Custom["Department"] != "QA" {
		t.Fatalf("metadata as loaded: %+v", m)
	}
	created := m.Created
	if !created.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("created %v", created)
	}
	image := d.rendered[0]
	if image == nil {
		t.Fatal("first page not rendered")
	}

	m.Title, m.Subject, m.Keywords = "New title", "", "one, two"
	if !d.SetMetadata(m) {
		t.Fatal("SetMetadata = false")
	}
	settle(d)
	if !d.Modified() || !d.CanUndo() || changes != 1 {
		t.Errorf("modified %v, can undo %v, %d changes told", d.Modified(), d.CanUndo(), changes)
	}
	got := d.Metadata()
	if got.Title != "New title" || got.Subject != "" || got.Keywords != "one, two" || got.Author != "A. Writer" ||
		got.Producer != "fyne-pdf tests" || got.Custom["Department"] != "QA" || !got.Created.Equal(created) {
		t.Errorf("metadata after the change: %+v", got)
	}
	if !bytes.Contains(got.XMP, []byte("New title")) {
		t.Errorf("XMP does not say the title: %s", got.XMP)
	}
	// The pages show what they did: nothing is rendered again.
	if d.rendered[0] != image || image.stale {
		t.Error("page image dropped by a change of the metadata")
	}

	d.Undo()
	settle(d)
	if title := d.Metadata().Title; title != "Old title" {
		t.Errorf("title after undo = %q", title)
	}
	if d.rendered[0] != image || image.stale {
		t.Error("page image dropped by undoing a change of the metadata")
	}
	d.Redo()
	settle(d)

	var saved bytes.Buffer
	if err := d.Save(&saved); err != nil {
		t.Fatal(err)
	}
	settle(d)
	src, err := OpenSource(saved.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	if got := src.Metadata(); got.Title != "New title" || got.Subject != "" || got.Keywords != "one, two" || got.Author != "A. Writer" {
		t.Errorf("metadata of the saved file: %+v", got)
	}
}

func TestSetMetadataWithoutDocument(t *testing.T) {
	d := newTestDocument(t)
	if d.SetMetadata(Metadata{Title: "x"}) {
		t.Error("SetMetadata = true without a document")
	}
}

func TestPageSize(t *testing.T) {
	d := newTestDocument(t)
	if w, h := d.PageSize(0); w != 0 || h != 0 {
		t.Errorf("size without a document = %v x %v", w, h)
	}
	loadAndWait(t, d, metadataPDF())
	if w, h := d.PageSize(0); w != 612 || h != 792 {
		t.Errorf("size of page 0 = %v x %v, want 612 x 792", w, h)
	}
	if w, h := d.PageSize(1); w != 842 || h != 595 {
		t.Errorf("size of page 1 = %v x %v, want 842 x 595", w, h)
	}
	if w, h := d.PageSize(2); w != 0 || h != 0 {
		t.Errorf("size of a page past the end = %v x %v", w, h)
	}
}
