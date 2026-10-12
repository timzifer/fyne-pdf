package main

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	pdf "github.com/timzifer/fyne-pdf"
)

const (
	// searchDelay debounces searching while the query is typed.
	searchDelay = 300 * time.Millisecond
	// listPrefixRunes is how much of the text before a result the list
	// shows, so the result itself fits.
	listPrefixRunes = 16
)

// lastRunes returns the last n runes of s.
func lastRunes(s string, n int) string {
	r := []rune(s)
	return string(r[max(0, len(r)-n):])
}

// searchPanel is the search UI of the viewer: a bar above the document
// with the query, the options, the result count and buttons for the
// previous and next result, and a list of the results beside the document.
// It drives the search of the pdf.Document.
type searchPanel struct {
	doc *pdf.Document

	bar     *fyne.Container
	entry   *searchEntry
	status  *widget.Label
	list    *widget.List
	content *container.Split

	matchCase, wholeWord, ignoreDiacritics *widget.Check

	// results are the results the list shows.
	results []pdf.SearchResult
	timer   *time.Timer
	// selecting is set while the list follows the document's current
	// result, so its selection does not go to the result again.
	selecting bool
}

// newSearchPanel returns the search UI for doc; content is the document
// with the result list, to be put below bar.
func newSearchPanel(doc *pdf.Document) *searchPanel {
	s := &searchPanel{doc: doc}

	s.entry = newSearchEntry()
	s.entry.SetPlaceHolder("Search…")
	s.entry.OnChanged = func(string) { s.searchLater() }
	// Enter goes to the next result, or searches a query not searched yet.
	s.entry.OnSubmitted = func(query string) {
		if query != doc.SearchQuery() {
			s.search()
			return
		}
		doc.NextSearchResult()
	}
	s.entry.onEscape = s.hide
	s.entry.onF3 = func(shift bool) {
		if shift {
			doc.PreviousSearchResult()
		} else {
			doc.NextSearchResult()
		}
	}

	option := func(label string) *widget.Check {
		return widget.NewCheck(label, func(bool) { s.search() })
	}
	s.matchCase = option("Match case")
	s.wholeWord = option("Whole words")
	s.ignoreDiacritics = option("Ignore accents")

	s.status = widget.NewLabel("")
	previous := widget.NewButtonWithIcon("", theme.MoveUpIcon(), doc.PreviousSearchResult)
	previous.Importance = widget.LowImportance
	next := widget.NewButtonWithIcon("", theme.MoveDownIcon(), doc.NextSearchResult)
	next.Importance = widget.LowImportance
	closeButton := widget.NewButtonWithIcon("", theme.CancelIcon(), s.hide)
	closeButton.Importance = widget.LowImportance

	s.bar = container.NewBorder(nil, nil, nil,
		container.NewHBox(s.matchCase, s.wholeWord, s.ignoreDiacritics, s.status, previous, next, closeButton),
		s.entry,
	)
	s.bar.Hide()

	s.list = widget.NewList(
		func() int { return len(s.results) },
		func() fyne.CanvasObject {
			text := widget.NewRichText()
			text.Truncation = fyne.TextTruncateEllipsis
			return container.NewVBox(widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Italic: true}), text)
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			r := s.results[id]
			box := o.(*fyne.Container)
			box.Objects[0].(*widget.Label).SetText(fmt.Sprintf("Page %d", r.Page+1))
			text := box.Objects[1].(*widget.RichText)
			// The text found in bold, between the text before and after it.
			text.Segments = []widget.RichTextSegment{
				&widget.TextSegment{Text: "…" + lastRunes(r.Prefix, listPrefixRunes), Style: widget.RichTextStyleInline},
				&widget.TextSegment{Text: r.Text, Style: widget.RichTextStyleStrong},
				&widget.TextSegment{Text: r.Suffix + "…", Style: widget.RichTextStyleInline},
			}
			text.Refresh()
		},
	)
	s.list.OnSelected = func(id widget.ListItemID) {
		if !s.selecting {
			doc.ShowSearchResult(id)
		}
	}

	s.content = container.NewHSplit(doc, s.list)
	s.content.Offset = 0.72
	s.list.Hide()

	doc.OnSearchChanged = s.update
	return s
}

// show shows the search bar and focuses the query.
func (s *searchPanel) show(c fyne.Canvas) {
	s.bar.Show()
	c.Focus(s.entry)
}

// hide hides the search bar and the result list and ends the search.
func (s *searchPanel) hide() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.bar.Hide()
	s.doc.ClearSearch()
}

// searchLater searches once the query has not changed for a moment.
func (s *searchPanel) searchLater() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(searchDelay, func() { fyne.Do(s.search) })
}

// search searches the query with the options set.
func (s *searchPanel) search() {
	if s.timer != nil {
		s.timer.Stop()
	}
	s.doc.Search(s.entry.Text, pdf.SearchOptions{
		MatchCase:        s.matchCase.Checked,
		WholeWord:        s.wholeWord.Checked,
		IgnoreDiacritics: s.ignoreDiacritics.Checked,
	})
}

// update follows the document's search: the result count, the list and
// its selection.
func (s *searchPanel) update() {
	s.results = s.doc.SearchResults()
	current := s.doc.CurrentSearchResult()

	searched, total := s.doc.SearchProgress()
	switch {
	case s.doc.SearchQuery() == "":
		s.status.SetText("")
	case s.doc.Searching():
		s.status.SetText(fmt.Sprintf("%d found, page %d of %d…", len(s.results), searched, total))
	case len(s.results) == 0:
		s.status.SetText("Not found")
	default:
		s.status.SetText(fmt.Sprintf("%d of %d", current+1, len(s.results)))
	}

	// The list is shown while there are results.
	if len(s.results) == 0 {
		s.list.Hide()
	} else {
		s.list.Show()
	}
	s.list.Refresh()

	s.selecting = true
	if current >= 0 {
		s.list.Select(current)
		s.list.ScrollTo(current)
	} else {
		s.list.UnselectAll()
	}
	s.selecting = false
	s.content.Refresh()
}

// searchEntry is the query entry: Escape closes the search, F3 and
// Shift+F3 go to the next and previous result.
type searchEntry struct {
	widget.Entry
	onEscape func()
	onF3     func(shift bool)
	shift    bool
}

func newSearchEntry() *searchEntry {
	e := &searchEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *searchEntry) TypedKey(ev *fyne.KeyEvent) {
	switch ev.Name {
	case fyne.KeyEscape:
		e.onEscape()
	case fyne.KeyF3:
		e.onF3(e.shift)
	default:
		e.Entry.TypedKey(ev)
	}
}

// KeyDown and KeyUp track Shift for Shift+F3.
func (e *searchEntry) KeyDown(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		e.shift = true
	}
	e.Entry.KeyDown(ev)
}

func (e *searchEntry) KeyUp(ev *fyne.KeyEvent) {
	if ev.Name == desktop.KeyShiftLeft || ev.Name == desktop.KeyShiftRight {
		e.shift = false
	}
	e.Entry.KeyUp(ev)
}
