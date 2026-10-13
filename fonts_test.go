package pdf

import (
	"slices"
	"sync"
	"testing"

	"github.com/timzifer/cera"
)

// askedFonts is a font provider that has no fonts and notes which ones it
// was asked for.
type askedFonts struct {
	mu    sync.Mutex
	names []string
}

func (f *askedFonts) Font(req cera.FontRequest) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = append(f.names, req.Name)
	return nil, false
}

func (f *askedFonts) asked(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.names, name)
}

func (f *askedFonts) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.names = nil
}

func TestSourceFonts(t *testing.T) {
	fonts := &askedFonts{}
	src, err := OpenSourceWith(textPDF([]string{"alpha"}), SourceOptions{Fonts: fonts})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	if _, err := src.RenderPage(0, 72); err != nil {
		t.Fatal(err)
	}
	if !fonts.asked("Helvetica") {
		t.Errorf("provider asked for %v, want Helvetica, which the PDF does not embed", fonts.names)
	}

	if _, err := OpenSourceWith(nil, SourceOptions{Fonts: fonts}); err == nil {
		t.Error("no error for empty contents")
	}
	if _, err := OpenSourceWith(encryptedPDF("user", "owner", restricted), SourceOptions{Password: "wrong"}); err == nil {
		t.Error("no error for a wrong password")
	}
	enc, err := OpenSourceWith(encryptedPDF("user", "owner", restricted), SourceOptions{Password: "user"})
	if err != nil {
		t.Fatal(err)
	}
	_ = enc.Close()
}

// TestDocumentFonts checks that the widget opens a PDF with its fonts, and
// every version of it after a change.
func TestDocumentFonts(t *testing.T) {
	fonts := &askedFonts{}
	d := newTestDocument(t)
	d.Fonts = fonts
	loadAndWait(t, d, textPDF([]string{"alpha beta"}))
	if !fonts.asked("Helvetica") {
		t.Fatalf("provider asked for %v on load, want Helvetica", fonts.names)
	}

	fonts.reset()
	d.OnError = func(_ int, err error) { t.Error(err) }
	if !d.addAnnotation(0, cera.NewAnnotation{Subtype: "Text", Contents: "note", Markup: cera.Markup{Bounds: cera.Rect{X0: 100, Y0: 100, X1: 120, Y1: 120}}}) {
		t.Fatal("could not add an annotation")
	}
	settle(d)
	if !fonts.asked("Helvetica") {
		t.Errorf("provider asked for %v after a change, want Helvetica", fonts.names)
	}

	fonts.reset()
	d.Undo()
	settle(d)
	if !fonts.asked("Helvetica") {
		t.Errorf("provider asked for %v after undo, want Helvetica", fonts.names)
	}
}
