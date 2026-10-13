package pdf

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// pageEntryWidth is the width of the page entry in the toolbar.
const pageEntryWidth = 64

// PageLabel returns the label of page (0-based): the number the document
// prints on it, as "iv", "12" or "A-3". It is empty for a document without
// page labels, for a page before the first that has one, and after Close.
func (s *Source) PageLabel(page int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return ""
	}
	return s.doc.PageLabel(page)
}

// PageByLabel returns the first page (0-based) with label, and false if no
// page has it, as in a document without page labels.
func (s *Source) PageByLabel(label string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return 0, false
	}
	return s.doc.PageByLabel(label)
}

// PageLabel returns the label of page (0-based) of the loaded PDF, see
// [Source.PageLabel].
func (d *Document) PageLabel(page int) string {
	if page < 0 || page >= len(d.pageLabels) {
		return ""
	}
	return d.pageLabels[page]
}

// GoToLabel goes to the first page with label, the number the document
// prints on it, keeping the zoom. It reports false if no page has the
// label.
func (d *Document) GoToLabel(label string) bool {
	src := d.loadedSource()
	if src == nil {
		return false
	}
	page, ok := src.PageByLabel(label)
	return ok && d.goToPage(page)
}

// goToPage goes to page, keeping the zoom, and reports whether there is
// such a page.
func (d *Document) goToPage(page int) bool {
	if page < 0 || page >= d.PageCount() {
		return false
	}
	d.turnPage(page, false)
	return true
}

// goToPageText goes to the page text names: the page with that label, or,
// if no page has it, the page with that number, counted from 1.
func (d *Document) goToPageText(text string) bool {
	text = strings.TrimSpace(text)
	if d.GoToLabel(text) {
		return true
	}
	number, err := strconv.Atoi(text)
	return err == nil && d.goToPage(number-1)
}

// pageCaption is what names page to the user: its label, or its number
// if it has none.
func (d *Document) pageCaption(page int) string {
	if label := d.PageLabel(page); label != "" {
		return label
	}
	return strconv.Itoa(page + 1)
}

// newPageEntry makes the page entry of the toolbar, with the text beside
// it that says where the page is in the document.
func (d *Document) newPageEntry() fyne.CanvasObject {
	d.pageEntry = widget.NewEntry()
	d.pageEntry.OnSubmitted = func(text string) {
		if d.goToPageText(text) {
			// The keys navigate again.
			d.area.focus()
		}
		// Also when the page stays: the entry names it as the document does.
		d.updatePageEntry()
	}
	d.pagePosition = widget.NewLabel("")
	width := canvas.NewRectangle(nil)
	width.SetMinSize(fyne.NewSize(pageEntryWidth, 0))
	d.updatePageEntry()
	return container.NewHBox(container.NewStack(width, d.pageEntry), d.pagePosition)
}

// updatePageEntry shows the current page in the page entry: its label with
// the position beside it, as "iv" and "(4 of 212)", or its number and "of
// 212" in a document without labels.
func (d *Document) updatePageEntry() {
	count := d.PageCount()
	if count == 0 {
		d.pageEntry.SetText("")
		d.pageEntry.Disable()
		d.pagePosition.SetText("")
		return
	}
	d.pageEntry.Enable()
	page := d.CurrentPage()
	d.pageEntry.SetText(d.pageCaption(page))
	if d.labelled {
		d.pagePosition.SetText(fmt.Sprintf("(%d of %d)", page+1, count))
	} else {
		d.pagePosition.SetText(fmt.Sprintf("of %d", count))
	}
}
