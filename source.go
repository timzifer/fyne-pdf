package pdf

import (
	"image"
	"log"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/go-pdfkit/reader"
	"github.com/go-pdfkit/render"
)

// renderTimeout begrenzt die Renderzeit pro Seite. Einzelne Seiten können mit
// go-pdfkit sehr lange brauchen - nach Ablauf wird das bis dahin gezeichnete
// Bild verwendet.
const renderTimeout = 15 * time.Second

// Source ist ein geöffnetes PDF. Alle Methoden sind goroutine-safe
// (reader.Document ist es nicht, daher der eigene Mutex).
type Source struct {
	mu  sync.Mutex
	doc *reader.Document
}

func OpenSource(contents []byte) (*Source, error) {
	if len(contents) == 0 {
		return nil, errors.New("could not open pdf: empty contents")
	}
	doc, err := reader.Open(contents)
	if err != nil {
		return nil, errors.Wrap(err, "could not open pdf")
	}
	return &Source{doc: doc}, nil
}

func (s *Source) PageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doc.PageCount()
}

// RenderPage rendert die Seite page (0-basiert) mit der angegebenen Auflösung.
func (s *Source) RenderPage(page int, dpi float64) (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if page < 0 || page >= s.doc.PageCount() {
		return nil, errors.Newf("page %d out of range (0..%d)", page, s.doc.PageCount()-1)
	}

	pic, err := render.Page(s.doc, page+1, render.Options{
		DPI:         dpi,
		MaxDuration: renderTimeout,
	})
	if errors.Is(err, render.ErrTimedOut) && pic != nil {
		log.Printf("pdf: rendering page %d timed out after %s, using partial image", page, renderTimeout)
	} else if err != nil {
		return nil, errors.Wrapf(err, "could not render pdf-page %d", page)
	}
	return pic.ToNRGBA(), nil
}
