package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"sync"
	"testing"
	"time"
)

// minimalPDF baut ein einseitiges PDF (612x792 pt) mit einem roten Rechteck
// von (100,100) bis (300,300) inkl. korrekter xref-Tabelle.
func minimalPDF() []byte {
	return pagePDF("")
}

// pagePDF baut dasselbe PDF mit zusätzlichen Einträgen im Seiten-Dictionary.
func pagePDF(extra string) []byte {
	content := "1 0 0 rg 100 100 200 200 re f"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " + extra + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
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

func TestOpenSourceEmpty(t *testing.T) {
	if _, err := OpenSource(nil); err == nil {
		t.Fatal("expected error for empty contents")
	}
}

func TestRenderPage(t *testing.T) {
	src, err := OpenSource(minimalPDF())
	if err != nil {
		t.Fatal(err)
	}
	if n := src.PageCount(); n != 1 {
		t.Fatalf("PageCount = %d, want 1", n)
	}

	img, err := src.RenderPage(0, 144)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1224 || b.Dy() != 1584 {
		t.Fatalf("bounds = %v, want 1224x1584", b)
	}

	// 144 dpi = Faktor 2, y-Achse gespiegelt: Rechteck bei x 200..600, y 984..1384
	if r, g, b, _ := img.At(400, 1200).RGBA(); r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("pixel inside rect = %v, want red", img.At(400, 1200))
	}
	if c := color.NRGBAModel.Convert(img.At(10, 10)).(color.NRGBA); c != (color.NRGBA{255, 255, 255, 255}) {
		t.Errorf("pixel outside rect = %v, want white", c)
	}

	if _, err := src.RenderPage(5, 144); err == nil {
		t.Error("expected error for page out of range")
	}
	if _, err := src.RenderPage(-1, 144); err == nil {
		t.Error("expected error for negative page")
	}
}

func TestRenderPageConcurrent(t *testing.T) {
	src, err := OpenSource(minimalPDF())
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := src.RenderPage(0, 72); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

func TestNewImageFromMemory(t *testing.T) {
	called := false
	img, err := NewImageFromMemory(minimalPDF(), 0, func(src *Source) {
		called = src.PageCount() == 1
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("callback not called with open source")
	}
	if img.Bounds().Dx() != 1224 {
		t.Errorf("width = %d, want 1224", img.Bounds().Dx())
	}
}

func TestSourceBoundAndClose(t *testing.T) {
	src, err := OpenSource(minimalPDF())
	if err != nil {
		t.Fatal(err)
	}
	b, err := src.Bound(0)
	if err != nil {
		t.Fatal(err)
	}
	if b.Dx() != 612 || b.Dy() != 792 {
		t.Fatalf("bound = %v, want 612x792", b)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if src.PageCount() != 0 {
		t.Fatal("closed source should report no pages")
	}
	if _, err := src.RenderPage(0, 72); err == nil {
		t.Fatal("render on closed source should fail")
	}
}

func TestSourceBoundRotatedCropped(t *testing.T) {
	src, err := OpenSource(pagePDF("/CropBox [0 0 300 400] /Rotate 90"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := src.Bound(0)
	if err != nil {
		t.Fatal(err)
	}
	if b.Dx() != 400 || b.Dy() != 300 {
		t.Fatalf("bound = %v, want 400x300", b)
	}
	img, err := src.RenderPage(0, 144)
	if err != nil {
		t.Fatal(err)
	}
	if s := img.Bounds().Size(); s.X != 800 || s.Y != 600 {
		t.Fatalf("image size = %v, want 800x600", s)
	}
}

func TestSourceMetadata(t *testing.T) {
	pdf := minimalPDF()
	// Append an info dictionary as incremental update.
	var buf bytes.Buffer
	buf.Write(pdf)
	info := buf.Len()
	fmt.Fprintf(&buf, "5 0 obj\n<< /Title (Plan) /Author <FEFF00470072006F00DF> /CreationDate (D:20260102030405+01'00') /Department (Bau) >>\nendobj\n")
	xref := buf.Len()
	prev := bytes.LastIndex(pdf, []byte("startxref\n"))
	var prevXref int
	if _, err := fmt.Sscanf(string(pdf[prev+len("startxref\n"):]), "%d", &prevXref); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&buf, "xref\n5 1\n%010d 00000 n \n", info)
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R /Info 5 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n", prevXref, xref)

	src, err := OpenSource(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	meta := src.Metadata()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("", 3600))
	if meta.Title != "Plan" || meta.Author != "Groß" || !meta.Created.Equal(created) ||
		!meta.Modified.IsZero() || !maps.Equal(meta.Custom, map[string]string{"Department": "Bau"}) {
		t.Fatalf("Metadata = %+v", meta)
	}

	if m := mustOpen(t, minimalPDF()).Metadata(); m.Title != "" || len(m.Custom) != 0 {
		t.Errorf("Metadata without info = %+v, want empty", m)
	}
	_ = src.Close()
	if m := src.Metadata(); m.Title != "" || len(m.Custom) != 0 {
		t.Errorf("Metadata after Close = %+v, want empty", m)
	}
}

func TestRenderPageContextCancelled(t *testing.T) {
	src := mustOpen(t, minimalPDF())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := src.RenderPageContext(ctx, 0, 72); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSourceBoundErrors(t *testing.T) {
	src := mustOpen(t, minimalPDF())
	if _, err := src.Bound(3); err == nil {
		t.Error("expected error for page out of range")
	}
	_ = src.Close()
	if _, err := src.Bound(0); err == nil {
		t.Error("expected error on closed source")
	}
}

func mustOpen(t *testing.T, contents []byte) *Source {
	t.Helper()
	src, err := OpenSource(contents)
	if err != nil {
		t.Fatal(err)
	}
	return src
}
