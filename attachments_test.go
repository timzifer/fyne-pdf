package pdf

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"github.com/timzifer/cera"
)

const (
	invoiceXML = "<?xml version=\"1.0\"?><Invoice><ID>4711</ID></Invoice>"
	notesText  = "notes of the author"
)

// attachmentsPDF builds a PDF of two pages, as an e-invoice is: it embeds
// factur-x.xml, and a file attachment annotation on the first page holds
// notes.txt. catalog is added to its catalogue.
func attachmentsPDF(catalog string) []byte {
	file := func(subtype, content string) string {
		return fmt.Sprintf("<< /Type /EmbeddedFile /Subtype /%s /Params << /Size %d >> /Length %d >>\nstream\n%s\nendstream",
			subtype, len(content), len(content), content)
	}
	return buildPDF([]string{
		fmt.Sprintf("<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(factur-x.xml) 5 0 R] >> >> %s >>", catalog),
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [7 0 R] >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Filespec /F (factur-x.xml) /UF (factur-x.xml) /Desc (Invoice data) /AFRelationship /Alternative /EF << /F 6 0 R >> >>",
		file("text#2Fxml", invoiceXML),
		"<< /Type /Annot /Subtype /FileAttachment /Rect [100 600 120 620] /FS 8 0 R /Contents (Notes) /T (A. Writer) /Name /Paperclip >>",
		"<< /Type /Filespec /F (drafts/notes.txt) /EF << /F 9 0 R >> >>",
		file("text#2Fplain", notesText),
	}, "")
}

func content(t *testing.T, a Attachment) string {
	t.Helper()
	r, err := a.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSourceAttachments(t *testing.T) {
	src, err := OpenSource(attachmentsPDF(""))
	if err != nil {
		t.Fatal(err)
	}
	files := src.Attachments()
	if len(files) != 1 {
		t.Fatalf("%d files, want 1", len(files))
	}
	f := files[0]
	if f.Name != "factur-x.xml" || f.Description != "Invoice data" || f.MIME != "text/xml" ||
		f.Size != int64(len(invoiceXML)) || f.Relationship != "Alternative" || f.Page != -1 {
		t.Errorf("file of the document: %+v", f)
	}
	if got := content(t, f); got != invoiceXML {
		t.Errorf("content = %q", got)
	}

	annots, err := src.Annotations(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(annots) != 1 || annots[0].File == nil {
		t.Fatalf("annotations of page 0: %+v", annots)
	}
	note := *annots[0].File
	if note.Name != "drafts/notes.txt" || note.Page != 0 || note.Index != annots[0].Index {
		t.Errorf("file of the annotation: %+v", note)
	}
	if got := content(t, note); got != notesText {
		t.Errorf("content of the annotation's file = %q", got)
	}

	_ = src.Close()
	if _, err := f.Open(); err == nil {
		t.Error("file opened after Close")
	}
	if got := src.Attachments(); got != nil {
		t.Errorf("files after Close: %+v", got)
	}
	if _, err := (Attachment{}).Open(); err == nil {
		t.Error("the zero Attachment opened")
	}
}

func TestAttachmentFileName(t *testing.T) {
	for name, want := range map[string]string{
		"factur-x.xml":      "factur-x.xml",
		"drafts/notes.txt":  "notes.txt",
		`..\..\startup.bat`: "startup.bat",
		"/etc/passwd":       "passwd",
		"":                  "attachment",
		"..":                "attachment",
		"dir/":              "dir",
	} {
		if got := (Attachment{Name: name}).fileName(); got != want {
			t.Errorf("file name of %q = %q, want %q", name, got, want)
		}
	}
}

// closeBuffer is a buffer that notes that it was closed.
type closeBuffer struct {
	bytes.Buffer
	closed bool
}

func (b *closeBuffer) Close() error {
	b.closed = true
	return nil
}

func TestAttachmentsPanel(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(2))
	d.ShowAttachments(true)
	if d.AttachmentsShown() || d.attachmentsButton.Visible() || len(d.Attachments()) != 0 {
		t.Error("panel or button shown for a PDF without files")
	}
	d.ShowAttachments(false)

	loadAndWait(t, d, attachmentsPDF(""))
	files := d.Attachments()
	if len(files) != 2 || files[0].Name != "factur-x.xml" || files[1].Name != "drafts/notes.txt" {
		t.Fatalf("files listed: %+v", files)
	}
	if d.AttachmentsShown() || !d.attachmentsButton.Visible() {
		t.Errorf("panel shown %v, button shown %v, want hidden, shown", d.AttachmentsShown(), d.attachmentsButton.Visible())
	}
	test.Tap(d.attachmentsButton)
	if !d.AttachmentsShown() {
		t.Fatal("panel hidden after its button was tapped")
	}
	test.Tap(d.attachmentsButton)
	if d.AttachmentsShown() {
		t.Fatal("panel shown after its button was tapped again")
	}

	// A tap on the paper clip on the page shows the panel with its file.
	d.Zoom(1)
	d.scrollTo(fyne.Position{})
	settle(d)
	d.area.Tapped(&fyne.PointEvent{Position: d.areaPoint(0, 110, 182)})
	if !d.AttachmentsShown() || d.attachments.selected != 1 {
		t.Errorf("after a tap on the annotation: panel shown %v, row %d selected, want shown, 1", d.AttachmentsShown(), d.attachments.selected)
	}
	// A tap on a row selects it; the row of an annotation's file marks
	// the annotation.
	test.Tap(d.attachments.rows[0])
	if d.attachments.selected != 0 {
		t.Errorf("row %d selected, want 0", d.attachments.selected)
	}
	d.comments.clearSelection()
	test.Tap(d.attachments.rows[1])
	if d.attachments.selected != 1 || !d.comments.hasSelected {
		t.Errorf("row %d selected, annotation selected %v, want 1, true", d.attachments.selected, d.comments.hasSelected)
	}

	var out closeBuffer
	if err := files[1].writeTo(&out); err != nil {
		t.Fatal(err)
	}
	if out.String() != notesText || !out.closed {
		t.Errorf("saved %q, closed %v", out.String(), out.closed)
	}

	// After a change the files are those of the new version.
	if !d.addAnnotation(1, cera.NewAnnotation{Subtype: "Text", Contents: "note", Markup: cera.Markup{Bounds: cera.Rect{X0: 100, Y0: 100, X1: 120, Y1: 120}}}) {
		t.Fatal("could not add an annotation")
	}
	settle(d)
	if _, err := files[0].Open(); err == nil {
		t.Error("a file of the version before the change still opens")
	}
	files = d.Attachments()
	if len(files) != 2 {
		t.Fatalf("%d files after a change, want 2", len(files))
	}
	if got := content(t, files[0]); got != invoiceXML {
		t.Errorf("content after a change = %q", got)
	}
	if !d.AttachmentsShown() {
		t.Error("panel hidden by a change")
	}
}

func TestAttachmentsOpenedAsAsked(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, attachmentsPDF("/PageMode /UseAttachments"))
	if !d.AttachmentsShown() {
		t.Error("panel hidden for UseAttachments")
	}
}
