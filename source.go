package pdf

import (
	"context"
	"image"
	"image/color"
	"log"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/cockroachdb/errors"
	"github.com/go-pdfkit/reader"
	"github.com/timzifer/cera"
)

// renderTimeout begrenzt die Renderzeit pro Seite. Nach Ablauf wird das bis
// dahin gezeichnete Bild verwendet.
const renderTimeout = 15 * time.Second

var paper = color.RGBA{R: 255, G: 255, B: 255, A: 255}

// Source is an opened PDF. All methods are safe for concurrent use: cera only
// allows interpreting pages of a document from one goroutine at a time, hence
// the mutex. A single page is still rasterized by cera on all cores.
type Source struct {
	mu  sync.Mutex
	doc *cera.Document
}

// OpenSource parses the PDF in contents.
func OpenSource(contents []byte) (*Source, error) {
	if len(contents) == 0 {
		return nil, errors.New("could not open pdf: empty contents")
	}
	doc, err := cera.Open(contents)
	if err != nil {
		return nil, errors.Wrap(err, "could not open pdf")
	}
	return &Source{doc: doc}, nil
}

// PageCount returns the number of pages, or 0 after Close.
func (s *Source) PageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return 0
	}
	return s.doc.NumPages()
}

// RenderPage renders page (0-based) at the given resolution onto a white
// background. If rendering takes longer than 15 seconds, the partially drawn
// image is returned without an error.
func (s *Source) RenderPage(page int, dpi float64) (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doc == nil {
		return nil, errors.New("pdf source is closed")
	}
	if page < 0 || page >= s.doc.NumPages() {
		return nil, errors.Newf("page %d out of range (0..%d)", page, s.doc.NumPages()-1)
	}
	p, err := s.doc.Page(page)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read pdf-page %d", page)
	}
	// Die Display-List wird nur für dieses eine Rendern gebraucht.
	defer p.Release()

	scale := dpi / 72
	dst := image.NewRGBA(p.Bounds(scale))
	err = p.Render(context.Background(), dst, cera.RenderOptions{
		Scale:      scale,
		Background: paper,
		Deadline:   time.Now().Add(renderTimeout),
	})
	var panicErr *cera.PanicError
	switch {
	case err == nil:
	case errors.As(err, &panicErr):
		return nil, errors.Wrapf(err, "could not render pdf-page %d", page)
	default:
		// ErrDeadline oder ein Budget des Rasterizers: dst enthält, was bis
		// dahin gezeichnet wurde.
		log.Printf("pdf: rendering page %d incomplete (%v), using partial image", page, err)
	}
	return dst, nil
}

// Close releases the document. Afterwards all methods return an error or zero
// pages. Closing more than once is allowed.
func (s *Source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.doc = nil
	return nil
}

// Bound returns the visible area of page (0-based) in points (72 dpi), with
// /Rotate and /UserUnit already applied.
func (s *Source) Bound(page int) (image.Rectangle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doc == nil {
		return image.Rectangle{}, errors.New("pdf source is closed")
	}
	p, err := s.doc.Page(page)
	if err != nil {
		return image.Rectangle{}, errors.Wrapf(err, "could not read pdf-page %d", page)
	}
	w, h := p.Size()
	return image.Rect(0, 0, int(math.Ceil(w)), int(math.Ceil(h))), nil
}

// Metadata returns the entries of the document info dictionary with lowercase
// keys ("title", "author", ...).
func (s *Source) Metadata() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := map[string]string{}
	if s.doc == nil {
		return out
	}
	info, ok := reader.ToDict(s.resolve(s.doc.Reader().Trailer().Get("Info")))
	if !ok {
		return out
	}
	for key, value := range info {
		if str, ok := reader.ToString(s.resolve(value)); ok {
			out[strings.ToLower(string(key))] = decodeTextString(str)
		}
	}
	return out
}

func (s *Source) resolve(o reader.Object) reader.Object {
	out, _ := s.doc.Reader().Resolve(o)
	return out
}

// decodeTextString dekodiert einen PDF-Textstring: UTF-16BE bzw. UTF-8 mit
// BOM, sonst PDFDocEncoding (hier vereinfacht als Latin-1).
func decodeTextString(b []byte) string {
	switch {
	case len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF:
		b = b[2:]
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		}
		return string(utf16.Decode(u))
	case len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF:
		return string(b[3:])
	default:
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return string(r)
	}
}
