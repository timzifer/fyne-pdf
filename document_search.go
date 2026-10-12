package pdf

import (
	"context"
	"image/color"
	"slices"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
)

var (
	// defaultHighlightColor marks the search results.
	defaultHighlightColor color.Color = color.NRGBA{R: 255, G: 213, B: 0, A: 96}
	// defaultCurrentHighlightColor marks the current search result.
	defaultCurrentHighlightColor color.Color = color.NRGBA{R: 255, G: 120, B: 0, A: 144}
)

// documentSearch is the state of the search in a Document. It belongs to
// the main goroutine; the search goroutine hands its results over with
// runOnMain.
type documentSearch struct {
	query string
	opt   SearchOptions
	// results are sorted by page, in reading order on a page.
	results []SearchResult
	// current is the index of the current result, -1 if none.
	current int
	// searched counts the pages searched so far.
	searched int
	running  bool
	// startPage is the current page when the search started: the first
	// result at or after it becomes current.
	startPage int
	cancel    context.CancelFunc
	// generation tells the results of an old search from the current one.
	generation int
}

// Search finds query in the document, in the background: pages from the
// current one to the end first, then those before it. The results are
// highlighted on the pages as they are found; the first one at or after
// the current page becomes the current result, and the view scrolls to
// it. OnSearchChanged reports the progress. An empty query, like
// [Document.ClearSearch], ends the search and removes the highlights.
// Loading another document ends it, too.
func (d *Document) Search(query string, opt SearchOptions) {
	d.stopSearch()
	s := &d.search
	s.generation++
	s.query, s.opt = query, opt
	s.results, s.current, s.searched = nil, -1, 0
	s.startPage = d.CurrentPage()

	d.mutex.Lock()
	src, parent := d.source, d.ctx
	d.mutex.Unlock()
	d.updateHighlights()
	if strings.TrimSpace(query) == "" || src == nil {
		s.query = ""
		d.searchChanged()
		return
	}

	ctx, cancel := context.WithCancel(parent)
	s.cancel, s.running = cancel, true
	d.searchChanged()

	generation, start, n := s.generation, s.startPage, d.PageCount()
	d.pending.Add(1)
	go func() {
		defer d.pending.Done()
		// The pages from start to the end, then those before it.
		for k := range n {
			page := (start + k) % n
			found, err := src.SearchPage(ctx, page, query, opt)
			if ctx.Err() != nil {
				break
			}
			if err != nil {
				// A page whose text cannot be read has no results; its
				// render reports what is wrong with it.
				found = nil
			}
			d.runOnMain(func() { d.addSearchResults(generation, found) })
		}
		d.runOnMain(func() { d.searchDone(generation) })
	}()
}

// addSearchResults adds the results of one page.
func (d *Document) addSearchResults(generation int, found []SearchResult) {
	s := &d.search
	if generation != s.generation {
		return
	}
	s.searched++
	if len(found) == 0 {
		d.searchChanged()
		return
	}
	page := found[0].Page
	at := sort.Search(len(s.results), func(i int) bool { return s.results[i].Page > page })
	s.results = slices.Insert(s.results, at, found...)
	if s.current >= at {
		s.current += len(found)
	}
	if s.current < 0 && page >= s.startPage {
		// The pages from the start page on are searched first: this is
		// the first result at or after it.
		d.ShowSearchResult(at)
		return
	}
	d.updateHighlights()
	d.searchChanged()
}

// searchDone ends the search of generation.
func (d *Document) searchDone(generation int) {
	s := &d.search
	if generation != s.generation || !s.running {
		return
	}
	s.running, s.cancel = false, nil
	if s.current < 0 && len(s.results) > 0 {
		// None after the start page: wrap around to the first.
		d.ShowSearchResult(0)
		return
	}
	d.searchChanged()
}

// stopSearch cancels a running search.
func (d *Document) stopSearch() {
	s := &d.search
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.running = false
}

// ClearSearch ends the search and removes its highlights.
func (d *Document) ClearSearch() {
	d.Search("", d.search.opt)
}

// resetSearch drops the search of a previous document.
func (d *Document) resetSearch() {
	if d.search.query == "" && !d.search.running {
		return
	}
	d.ClearSearch()
}

// SearchQuery returns the query of the last search, "" after ClearSearch.
func (d *Document) SearchQuery() string {
	return d.search.query
}

// Searching reports whether the search is still going through the pages.
func (d *Document) Searching() bool {
	return d.search.running
}

// SearchProgress returns the number of pages searched so far and the
// number of pages to search.
func (d *Document) SearchProgress() (searched, total int) {
	if d.search.query == "" {
		return 0, 0
	}
	return d.search.searched, d.PageCount()
}

// SearchResults returns the results found so far, sorted by page and in
// reading order on a page. While the search runs, results of pages before
// the page it started at are inserted in front of those found earlier.
func (d *Document) SearchResults() []SearchResult {
	return slices.Clone(d.search.results)
}

// CurrentSearchResult returns the index of the current result into
// SearchResults, or -1 if there is none.
func (d *Document) CurrentSearchResult() int {
	return d.search.current
}

// ShowSearchResult makes result i (an index into SearchResults) the
// current result, highlighted in CurrentHighlightColor, and scrolls the
// view so it is visible: to its page and, unless it is in view already,
// with the result at the centre of the view.
func (d *Document) ShowSearchResult(i int) {
	s := &d.search
	if i < 0 || i >= len(s.results) {
		return
	}
	s.current = i
	d.revealRect(s.results[i].Page, s.results[i].Bounds())
	d.updateHighlights()
	d.searchChanged()
}

// NextSearchResult makes the result after the current one current,
// wrapping around to the first, and scrolls to it. Without a current
// result, it goes to the first result at or after the current page.
func (d *Document) NextSearchResult() {
	s := &d.search
	if len(s.results) == 0 {
		return
	}
	next := s.current + 1
	if s.current < 0 {
		page := d.CurrentPage()
		next = sort.Search(len(s.results), func(i int) bool { return s.results[i].Page >= page })
	}
	d.ShowSearchResult(next % len(s.results))
}

// PreviousSearchResult makes the result before the current one current,
// wrapping around to the last, and scrolls to it. Without a current
// result, it goes to the last result at or before the current page.
func (d *Document) PreviousSearchResult() {
	s := &d.search
	if len(s.results) == 0 {
		return
	}
	prev := s.current - 1
	if s.current < 0 {
		page := d.CurrentPage()
		prev = sort.Search(len(s.results), func(i int) bool { return s.results[i].Page > page }) - 1
	}
	d.ShowSearchResult((prev + len(s.results)) % len(s.results))
}

// searchChanged reports a change of the search to OnSearchChanged.
func (d *Document) searchChanged() {
	if d.OnSearchChanged != nil {
		d.OnSearchChanged()
	}
}

// revealRect makes page current and scrolls so rect (in points of page)
// is in view, centring it unless it is in view already.
func (d *Document) revealRect(page int, rect Rect) {
	if page < 0 || page >= d.PageCount() {
		return
	}
	if !d.layout.contains(page) {
		// Showing one page at a time: turn to it, keeping the zoom.
		d.turnPage(page, false)
	}
	d.setCurrent(page)

	scale := float32(unitsPerPoint * d.scale)
	origin := d.layout.origin(page, d.areaSize())
	lo := origin.AddXY(float32(rect.X0)*scale, float32(rect.Y0)*scale)
	hi := origin.AddXY(float32(rect.X1)*scale, float32(rect.Y1)*scale)

	offset := d.scrollContainer.Offset
	view := d.scrollContainer.Size()
	if lo.X < offset.X || hi.X > offset.X+view.Width {
		offset.X = (lo.X+hi.X)/2 - view.Width/2
	}
	if lo.Y < offset.Y || hi.Y > offset.Y+view.Height {
		offset.Y = (lo.Y+hi.Y)/2 - view.Height/2
	}
	d.navigating = true
	d.scrollTo(offset)
	d.navigating = false
}

// updateHighlights shows the search results on the pages with objects.
func (d *Document) updateHighlights() {
	for page, view := range d.views {
		view.SetHighlights(d.highlightsOf(page))
	}
}

// highlightsOf returns the highlights of the search results on page.
func (d *Document) highlightsOf(page int) []Highlight {
	s := &d.search
	from := sort.Search(len(s.results), func(i int) bool { return s.results[i].Page >= page })
	var highlights []Highlight
	for i := from; i < len(s.results) && s.results[i].Page == page; i++ {
		c := d.HighlightColor
		if c == nil {
			c = defaultHighlightColor
		}
		if i == s.current {
			c = d.CurrentHighlightColor
			if c == nil {
				c = defaultCurrentHighlightColor
			}
		}
		for _, r := range s.results[i].Rects {
			highlights = append(highlights, Highlight{
				Position: fyne.NewPos(float32(r.X0*unitsPerPoint), float32(r.Y0*unitsPerPoint)),
				Size:     fyne.NewSize(float32((r.X1-r.X0)*unitsPerPoint), float32((r.Y1-r.Y0)*unitsPerPoint)),
				Color:    c,
			})
		}
	}
	return highlights
}
