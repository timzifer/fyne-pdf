package pdf

import (
	"context"
	"image"
	"image/color"
	"log"
	"math"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
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
	return s.RenderPageContext(context.Background(), page, dpi)
}

// RenderPageContext is like [Source.RenderPage], but stops early and returns
// ctx.Err() when ctx is cancelled.
func (s *Source) RenderPageContext(ctx context.Context, page int, dpi float64) (image.Image, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Renders cancelled while waiting for the lock return right away.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
	err = p.Render(ctx, dst, cera.RenderOptions{
		Scale:      scale,
		Background: paper,
		Deadline:   time.Now().Add(renderTimeout),
	})
	var panicErr *cera.PanicError
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return nil, ctx.Err()
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

// Metadata is what a document says about itself: its document information
// dictionary (/Info), decoded, and its XMP metadata stream.
type Metadata struct {
	Title, Author, Subject, Keywords, Creator, Producer string

	// Created and Modified are zero when absent or not a date.
	Created, Modified time.Time

	// Trapped is "True", "False", "Unknown" or "".
	Trapped string

	// Custom holds the other string entries of /Info, by key as written
	// in the document ("Department", ...).
	Custom map[string]string

	// XMP is the catalog's /Metadata stream, decoded but not parsed; nil if
	// there is none.
	XMP []byte
}

// Metadata returns the document's metadata, or the zero Metadata after
// Close. A missing or damaged /Info gives empty fields.
func (s *Source) Metadata() Metadata {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.doc == nil {
		return Metadata{}
	}
	m := s.doc.Metadata()
	return Metadata{
		Title:    m.Title,
		Author:   m.Author,
		Subject:  m.Subject,
		Keywords: m.Keywords,
		Creator:  m.Creator,
		Producer: m.Producer,
		Created:  m.Created,
		Modified: m.Modified,
		Trapped:  m.Trapped,
		Custom:   m.Custom,
		XMP:      m.XMP,
	}
}
