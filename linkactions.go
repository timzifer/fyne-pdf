package pdf

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"github.com/timzifer/cera"
)

// maxViewHistory is how many views GoBack can return to.
const maxViewHistory = 100

type (
	// viewState is a view of the document to come back to.
	viewState struct {
		page int
		// anchor is the page point at the top left of the view.
		anchor viewAnchor
		zoom   float64
		// fitHorizontal and fitVertical are the fit that was kept.
		fitHorizontal, fitVertical bool
	}

	// viewHistory are the views left by a jump: by a link, an outline
	// entry or GoTo. back are those to go back to, the last one first;
	// forward those gone back from.
	viewHistory struct {
		back, forward []viewState
	}
)

// Description says in words what following the link does, for a tooltip or
// a status line: "Go to page 12", "Open https://example.com/". For a link
// the widget does not follow — one that runs a script, starts a program or
// submits a form — it says what the link would do.
func (l *Link) Description() string {
	switch {
	case l.Dest != nil:
		return fmt.Sprintf("Go to page %d", l.Dest.Page+1)
	case l.URI != "":
		return "Open " + l.URI
	}
	switch l.NamedAction {
	case "NextPage":
		return "Go to the next page"
	case "PrevPage":
		return "Go to the previous page"
	case "FirstPage":
		return "Go to the first page"
	case "LastPage":
		return "Go to the last page"
	case "GoBack":
		return "Go back to the previous view"
	case "GoForward":
		return "Go forward to the next view"
	case "Print":
		return "Print the document"
	case "":
	default:
		return fmt.Sprintf("Would run the viewer command %q (not supported)", l.NamedAction)
	}
	switch l.Action {
	case "GoToR":
		return fmt.Sprintf("Open the file %q", l.File)
	case "GoToE":
		return fmt.Sprintf("Would open the embedded file %q (not supported)", strings.Join(l.Embedded, " > "))
	case "Launch":
		return fmt.Sprintf("Would start %q (never done)", l.File)
	case "JavaScript":
		return "Would run a script (never done)"
	case "SubmitForm":
		return "Would send the form (not supported)"
	case "":
		return "Leads nowhere"
	}
	return fmt.Sprintf("Would do the action %q (not supported)", l.Action)
}

// follows reports whether a tap on the link does something: the pointer
// is a hand over it then.
func (d *Document) follows(l *Link) bool {
	if l.Dest != nil || l.URI != "" {
		return true
	}
	switch l.NamedAction {
	case "NextPage", "PrevPage", "FirstPage", "LastPage":
		_, ok := d.namedPage(l.NamedAction)
		return ok
	case "GoBack":
		return d.CanGoBack()
	case "GoForward":
		return d.CanGoForward()
	case "Print":
		return d.OnPrint != nil
	}
	return l.Action == "GoToR" && l.File != ""
}

// namedPage returns the page the named action name leads to from the
// current one.
func (d *Document) namedPage(name string) (int, bool) {
	t := cera.LinkTarget{NamedAction: name}
	return t.NamedPage(d.CurrentPage(), d.PageCount())
}

// followLink does what l asks for: it asks OnLinkTapped first, if set;
// then it goes to the destination, opens the URI in the browser, or
// follows a named action. A link to another PDF is opened after the user
// agreed; nothing is run, started or sent.
func (d *Document) followLink(l Link) {
	if d.OnLinkTapped != nil && d.OnLinkTapped(l) {
		return
	}
	switch {
	case l.Dest != nil:
		d.GoTo(*l.Dest)
		return
	case l.URI != "":
		if u, err := url.Parse(l.URI); err == nil {
			_ = d.openURL(u)
		}
		return
	}
	switch l.NamedAction {
	case "NextPage", "PrevPage", "FirstPage", "LastPage":
		if page, ok := d.namedPage(l.NamedAction); ok {
			d.rememberView()
			d.turnPage(page, false)
		}
	case "GoBack":
		d.GoBack()
	case "GoForward":
		d.GoForward()
	case "Print":
		if d.OnPrint != nil {
			d.OnPrint()
		}
	case "":
		if l.Action == "GoToR" && l.File != "" {
			d.askOpenFile(l)
		}
	}
}

// askOpenFile asks whether to open the PDF l leads to, and has the system
// open it then.
func (d *Document) askOpenFile(l Link) {
	w := d.window()
	if w == nil {
		return
	}
	file := d.linkedFile(l.File)
	dlg := dialog.NewConfirm("Open another file",
		fmt.Sprintf("This link leads to another file:\n\n%s\n\nOpen it?", file), func(ok bool) {
			d.fileDialog = nil
			if !ok {
				return
			}
			if !filepath.IsAbs(file) {
				dialog.ShowInformation("Open another file",
					"The file is named relative to this document, whose place is not known.", w)
				return
			}
			if u, err := url.Parse(storage.NewFileURI(file).String()); err == nil {
				if err := d.openURL(u); err != nil {
					dialog.ShowError(err, w)
				}
			}
		}, w)
	d.fileDialog = dlg
	dlg.Show()
}

// linkedFile returns the name of a file a link leads to as a path: one
// relative to the document is resolved against Dir, if that is set.
func (d *Document) linkedFile(name string) string {
	file := filepath.FromSlash(name)
	if !filepath.IsAbs(file) && d.Dir != "" {
		file = filepath.Join(d.Dir, file)
	}
	return file
}

// currentView returns the view shown, to come back to it.
func (d *Document) currentView() viewState {
	return viewState{
		page:          d.CurrentPage(),
		anchor:        d.anchorAt(fyne.Position{}),
		zoom:          d.scale,
		fitHorizontal: d.fitHorizontal,
		fitVertical:   d.fitVertical,
	}
}

// showView shows a view left earlier.
func (d *Document) showView(v viewState) {
	if v.page < 0 || v.page >= d.PageCount() {
		return
	}
	d.turnPage(v.page, false)
	d.setCurrent(v.page)
	d.fitHorizontal, d.fitVertical = v.fitHorizontal, v.fitVertical
	if v.fitHorizontal || v.fitVertical {
		d.applyFit()
	} else {
		d.setZoom(v.zoom)
	}
	if offset, ok := d.anchorOffset(v.anchor); ok {
		d.navigating = true
		d.scrollTo(offset)
		d.navigating = false
	}
}

// rememberView notes the view shown before a jump, for GoBack.
func (d *Document) rememberView() {
	if d.PageCount() == 0 {
		return
	}
	h := &d.jumps
	h.back = append(h.back, d.currentView())
	if len(h.back) > maxViewHistory {
		h.back = h.back[1:]
	}
	h.forward = nil
}

// CanGoBack reports whether there is a view to go back to: one left by a
// link, an outline entry or [Document.GoTo].
func (d *Document) CanGoBack() bool {
	return len(d.jumps.back) > 0
}

// CanGoForward reports whether there is a view gone back from.
func (d *Document) CanGoForward() bool {
	return len(d.jumps.forward) > 0
}

// GoBack returns to the view that was shown before the last jump — its
// page, zoom and place; Alt+Left in the page view. It reports whether
// there was one.
func (d *Document) GoBack() bool {
	h := &d.jumps
	if len(h.back) == 0 {
		return false
	}
	v := h.back[len(h.back)-1]
	h.back = h.back[:len(h.back)-1]
	h.forward = append(h.forward, d.currentView())
	d.showView(v)
	return true
}

// GoForward returns to the view [Document.GoBack] left; Alt+Right in the
// page view. It reports whether there was one.
func (d *Document) GoForward() bool {
	h := &d.jumps
	if len(h.forward) == 0 {
		return false
	}
	v := h.forward[len(h.forward)-1]
	h.forward = h.forward[:len(h.forward)-1]
	h.back = append(h.back, d.currentView())
	d.showView(v)
	return true
}

// hoverLink tells OnLinkHovered when the pointer comes over another link
// or leaves one.
func (d *Document) hoverLink(l *Link) {
	same := l == nil && d.hovered == nil ||
		l != nil && d.hovered != nil && l.page == d.hovered.page && l.index == d.hovered.index
	if same {
		return
	}
	d.hovered = l
	if d.OnLinkHovered != nil {
		d.OnLinkHovered(l)
	}
}
