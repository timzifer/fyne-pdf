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
	// text is the text index for searching, made on first use.
	text *cera.TextIndex
	// contents and password are what the PDF was opened from.
	contents []byte
	password string
}

var (
	// ErrPassword is returned (wrapped, test with errors.Is) when a PDF is
	// encrypted and the password given does not open it: no password and
	// a wrong one alike. Ask for one and open it again with
	// [OpenSourceWithPassword].
	ErrPassword = cera.ErrPassword
	// ErrUnsupportedEncryption is returned (wrapped) for a PDF encrypted in
	// a way that cannot be read: a security handler other than the
	// standard one, such as certificates. No password opens it.
	ErrUnsupportedEncryption = cera.ErrUnsupportedEncryption
)

// Permissions are what the author of an encrypted PDF allows a reader who
// opened it with the user password. Rendering and text extraction work
// regardless; whether to print or copy is the application's decision.
type Permissions uint32

// The permissions of an encrypted PDF.
const (
	// PermPrint allows printing; without PermPrintFaithful only in a
	// quality that does not serve as a copy of the document.
	PermPrint = Permissions(cera.PermPrint)
	// PermModify allows changing the content of the document.
	PermModify = Permissions(cera.PermModify)
	// PermCopy allows copying text and graphics out of the document.
	PermCopy = Permissions(cera.PermCopy)
	// PermAnnotate allows adding and changing annotations, and filling in
	// form fields.
	PermAnnotate = Permissions(cera.PermAnnotate)
	// PermFillForms allows filling in form fields, also without
	// PermAnnotate.
	PermFillForms = Permissions(cera.PermFillForms)
	// PermExtract allowed extracting text and graphics for accessibility;
	// PDF 2.0 always allows that.
	PermExtract = Permissions(cera.PermExtract)
	// PermAssemble allows inserting, deleting and rotating pages.
	PermAssemble = Permissions(cera.PermAssemble)
	// PermPrintFaithful allows printing in full quality.
	PermPrintFaithful = Permissions(cera.PermPrintFaithful)

	// AllPermissions is every permission: those of a PDF that is not
	// encrypted, or that was opened with the owner password.
	AllPermissions = Permissions(cera.AllPermissions)
)

// OpenSource parses the PDF in contents. An encrypted PDF that needs a
// password returns an error wrapping [ErrPassword].
func OpenSource(contents []byte) (*Source, error) {
	return OpenSourceWithPassword(contents, "")
}

// OpenSourceWithPassword parses the encrypted PDF in contents with
// password: its user password, or its owner password, which lifts the
// restrictions of [Source.Permissions]. A wrong password returns an error
// wrapping [ErrPassword], encryption that cannot be read one wrapping
// [ErrUnsupportedEncryption].
func OpenSourceWithPassword(contents []byte, password string) (*Source, error) {
	return OpenSourceWith(contents, SourceOptions{Password: password})
}

// SourceOptions say how [OpenSourceWith] opens a PDF.
type SourceOptions struct {
	// Password opens an encrypted PDF, see [OpenSourceWithPassword].
	Password string
	// Fonts supplies the fonts the PDF names but does not embed: those
	// of the machine (github.com/timzifer/cera/fonts/system), CJK fonts
	// (github.com/timzifer/cera/fonts/cjk), or several as a
	// [cera.FontProviders]. If nil, such fonts are drawn with the
	// stand-ins cera has built in.
	Fonts cera.FontProvider
}

// OpenSourceWith parses the PDF in contents with options. The errors are
// those of [OpenSourceWithPassword].
func OpenSourceWith(contents []byte, opt SourceOptions) (*Source, error) {
	if len(contents) == 0 {
		return nil, errors.New("could not open pdf: empty contents")
	}
	doc, err := cera.OpenWith(contents, cera.OpenOptions{Password: opt.Password, Fonts: opt.Fonts})
	if err != nil {
		return nil, errors.Wrap(err, "could not open pdf")
	}
	return &Source{doc: doc, contents: contents, password: opt.Password}, nil
}

// Encrypted reports whether the PDF is encrypted; false after Close.
func (s *Source) Encrypted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doc != nil && s.doc.Encrypted()
}

// Permissions returns what the PDF allows: [AllPermissions] if it is not
// encrypted or was opened with the owner password, else what its author
// granted; 0 after Close.
func (s *Source) Permissions() Permissions {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return 0
	}
	return Permissions(s.doc.Permissions())
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
	return s.RenderPageOptions(ctx, page, dpi, RenderOptions{})
}

// RenderOptions changes how [Source.RenderPageOptions] draws a page.
type RenderOptions struct {
	// LinkBorders draws the borders link annotations ask for (/Border,
	// /BS with /C), such as the coloured boxes hyperref puts around the
	// links of LaTeX documents. Acrobat and Firefox draw them, Chrome and
	// Preview do not; off by default.
	LinkBorders bool
}

// RenderPageOptions is like [Source.RenderPageContext], with options.
func (s *Source) RenderPageOptions(ctx context.Context, page int, dpi float64, opt RenderOptions) (image.Image, error) {
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
		Scale:       scale,
		Background:  paper,
		Deadline:    time.Now().Add(renderTimeout),
		LinkBorders: opt.LinkBorders,
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
	s.text = nil
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
