package pdf

import (
	"context"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/timzifer/cera"
)

// SearchOptions control how a query is matched. The zero value matches
// case-insensitively anywhere in a word.
type SearchOptions struct {
	// MatchCase tells upper and lower case apart.
	MatchCase bool
	// WholeWord matches the query only where it neither continues a word
	// before nor after it.
	WholeWord bool
	// IgnoreDiacritics finds "resume" in "résumé" and reads compatibility
	// characters as what they stand for, such as "2" for "²".
	IgnoreDiacritics bool
}

// Rect is a rectangle on a page in points (1/72 inch) of the page as it is
// displayed: origin at the top left corner, y down, /Rotate and /UserUnit
// applied, like [Source.Bound].
type Rect struct {
	X0, Y0, X1, Y1 float64
}

// SearchResult is a place on a page where the query was found.
type SearchResult struct {
	// Page is the index (0-based) of the page.
	Page int
	// Text is the text found, Prefix and Suffix up to 32 characters of the
	// text before and after it, for a result list. White space in them is
	// one space.
	Prefix, Text, Suffix string
	// Rects cover the text found, one per line it spans.
	Rects []Rect
}

// Bounds returns the smallest rectangle around the rects of r.
func (r SearchResult) Bounds() Rect {
	if len(r.Rects) == 0 {
		return Rect{}
	}
	b := r.Rects[0]
	for _, q := range r.Rects[1:] {
		b.X0, b.Y0 = min(b.X0, q.X0), min(b.Y0, q.Y0)
		b.X1, b.Y1 = max(b.X1, q.X1), max(b.Y1, q.Y1)
	}
	return b
}

// textIndex returns the text index of the document and its number of
// pages. The index is made on first use; it keeps the laid-out text of
// the most recently searched pages.
func (s *Source) textIndex() (*cera.TextIndex, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return nil, 0, errors.New("pdf source is closed")
	}
	if s.text == nil {
		s.text = cera.NewTextIndex(s.doc, cera.TextOptions{}, 0)
	}
	return s.text, s.doc.NumPages(), nil
}

// pageText returns the laid-out text of page. It does not hold the lock
// while it lays out the text: a cera.Document is safe for concurrent use,
// so searching does not wait for renders, nor renders for a search.
func (s *Source) pageText(ctx context.Context, page int) (*cera.PageText, error) {
	x, n, err := s.textIndex()
	if err != nil {
		return nil, err
	}
	if page < 0 || page >= n {
		return nil, errors.Newf("page %d out of range (0..%d)", page, n-1)
	}
	t, err := x.Page(ctx, page)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.Wrapf(err, "could not read the text of pdf-page %d", page)
	}
	return t, nil
}

// PageText returns the text of page (0-based) in reading order: lines
// separated by a line break, blocks of text by an empty line.
func (s *Source) PageText(ctx context.Context, page int) (string, error) {
	t, err := s.pageText(ctx, page)
	if err != nil {
		return "", err
	}
	return t.String(), nil
}

// SearchPage returns the places of query on page (0-based), in reading
// order and not overlapping. Runs of white space in the text and the query
// match one space, and words broken across lines by a hyphen are found
// joined. An empty query finds nothing.
func (s *Source) SearchPage(ctx context.Context, page int, query string, opt SearchOptions) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	t, err := s.pageText(ctx, page)
	if err != nil {
		return nil, err
	}
	matches := t.Find(query, cera.SearchOptions{
		MatchCase:        opt.MatchCase,
		WholeWord:        opt.WholeWord,
		IgnoreDiacritics: opt.IgnoreDiacritics,
	})
	results := make([]SearchResult, 0, len(matches))
	for _, m := range matches {
		quads := t.Quads(m.Range)
		rects := make([]Rect, len(quads))
		for i, q := range quads {
			b := q.Bounds()
			rects[i] = Rect{X0: b.X0, Y0: b.Y0, X1: b.X1, Y1: b.Y1}
		}
		results = append(results, SearchResult{
			Page:   page,
			Prefix: m.Quote.Prefix,
			Text:   m.Quote.Exact,
			Suffix: m.Quote.Suffix,
			Rects:  rects,
		})
	}
	return results, nil
}

// Search returns the places of query in the whole document, page by page,
// as [Source.SearchPage] finds them. Pages whose text cannot be read are
// skipped; their errors are returned joined, with the results of the
// other pages. When ctx ends, Search returns the results so far and
// ctx.Err().
func (s *Source) Search(ctx context.Context, query string, opt SearchOptions) ([]SearchResult, error) {
	var (
		results []SearchResult
		errs    []error
	)
	for page := range s.PageCount() {
		found, err := s.SearchPage(ctx, page, query, opt)
		if ctx.Err() != nil {
			return results, ctx.Err()
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		results = append(results, found...)
	}
	return results, errors.Join(errs...)
}
