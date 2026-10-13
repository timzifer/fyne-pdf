package pdf

import (
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/cockroachdb/errors"
)

// Link is a link on a page: an area that leads to a place in the
// document, to a URI or to another file, or that asks the viewer to do
// something. [Link.Description] says in words what it does.
type Link struct {
	// Rects are the areas that react, in points (1/72 inch) of the
	// displayed page from its top left.
	Rects []Rect
	// Dest is where the link leads in the document; nil for one that
	// leads elsewhere.
	Dest *Destination
	// URI is the address of a link to the web (or mailto:).
	URI string
	// External is set for a link out of the document: to another PDF
	// (Action "GoToR"), which the widget opens after the user agreed, to a
	// file embedded in this or another one ("GoToE", see Embedded) or to a
	// file or program ("Launch"), which it never opens. File names it.
	External bool
	File     string
	// Embedded is the way to the target of a "GoToE" link, from File, or
	// from this document when File is empty: the names of embedded files,
	// each inside the one before.
	Embedded []string
	// NamedAction is the name of a command for the viewer: "NextPage",
	// "PrevPage", "FirstPage" and "LastPage", which the widget follows,
	// "GoBack" and "GoForward" (see [Document.GoBack]), "Print" (see
	// Document.OnPrint), or one it does not know.
	NamedAction string
	// Action is the type of the action the link was read from: "GoTo",
	// "GoToR", "GoToE", "Launch", "URI", "Named", or one that is only
	// reported and never done: "JavaScript", "SubmitForm", "Hide", ... It
	// is empty for a plain destination.
	Action string

	// page and index name the link annotation.
	page, index int
}

// Contains reports whether the point x, y (in points of the displayed
// page) is on the link.
func (l *Link) Contains(x, y float64) bool {
	for _, r := range l.Rects {
		if x >= r.X0 && x <= r.X1 && y >= r.Y0 && y <= r.Y1 {
			return true
		}
	}
	return false
}

// Links returns the links of page (0-based) in the order of the page's
// annotations, leaving out hidden ones.
func (s *Source) Links(page int) ([]Link, error) {
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
	defer p.Release()

	var out []Link
	for _, l := range p.Links() {
		link := Link{
			URI:         l.Target.URI,
			External:    l.Target.External,
			File:        l.Target.File,
			Embedded:    l.Target.Embedded,
			NamedAction: l.Target.NamedAction,
			Action:      l.Target.Action,
			page:        page,
			index:       l.Index,
		}
		if !link.External {
			// The destination of a link out of the document is a place
			// in the other file.
			link.Dest = s.destination(&l.Target)
		}
		for _, q := range l.Quads {
			b := q.Bounds()
			link.Rects = append(link.Rects, Rect{X0: b.X0, Y0: b.Y0, X1: b.X1, Y1: b.Y1})
		}
		out = append(out, link)
	}
	return out, nil
}

// loadLinks reads the links of those of pages not read yet, in the
// background: the main goroutine must not wait for the source while it
// renders.
func (d *Document) loadLinks(pages []int) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.source == nil {
		return
	}
	var missing []int
	for _, page := range pages {
		if _, ok := d.links[page]; !ok {
			// Asked for once: a page that cannot be read has no links.
			d.links[page] = nil
			missing = append(missing, page)
		}
	}
	if len(missing) == 0 {
		return
	}
	src, ctx := d.source, d.ctx
	d.pending.Add(1)
	go func() {
		defer d.pending.Done()
		for _, page := range missing {
			links, _ := src.Links(page)
			d.mutex.Lock()
			if ctx != d.ctx || ctx.Err() != nil {
				d.mutex.Unlock()
				return
			}
			d.links[page] = links
			d.mutex.Unlock()
		}
	}()
}

// linkAt returns the link at pos in the page area, if there is one.
func (d *Document) linkAt(pos fyne.Position) (Link, bool) {
	area := d.areaSize()
	page := d.layout.pageAt(pos.Y, area)
	if page < 0 || !d.layout.contains(page) {
		return Link{}, false
	}
	d.mutex.Lock()
	links := d.links[page]
	d.mutex.Unlock()

	scale := float32(d.scale * unitsPerPoint)
	p := pos.Subtract(d.layout.origin(page, area))
	x, y := float64(p.X/scale), float64(p.Y/scale)
	i := slices.IndexFunc(links, func(l Link) bool { return l.Contains(x, y) })
	if i < 0 {
		return Link{}, false
	}
	return links[i], true
}

var (
	_ desktop.Hoverable  = (*pageArea)(nil)
	_ desktop.Cursorable = (*pageArea)(nil)
)

// MouseIn and MouseMoved choose the pointer for what is under it.
func (a *pageArea) MouseIn(ev *desktop.MouseEvent) {
	a.MouseMoved(ev)
}

func (a *pageArea) MouseMoved(ev *desktop.MouseEvent) {
	a.cursor = a.doc.cursor(ev.Position)
	if l, ok := a.doc.linkAt(ev.Position); ok {
		a.doc.hoverLink(&l)
	} else {
		a.doc.hoverLink(nil)
	}
}

func (a *pageArea) MouseOut() {
	a.cursor = desktop.DefaultCursor
	a.doc.hoverLink(nil)
}

// Cursor is a hand over a link that is followed, an I-beam over text
// while selecting it.
func (a *pageArea) Cursor() desktop.Cursor {
	if a.cursor == nil {
		return desktop.DefaultCursor
	}
	return a.cursor
}
