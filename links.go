package pdf

import (
	"net/url"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"github.com/cockroachdb/errors"
)

// Link is a link on a page: an area that leads to a place in the
// document, to a URI, or to another file.
type Link struct {
	// Rects are the areas that react, in points (1/72 inch) of the
	// displayed page from its top left.
	Rects []Rect
	// Dest is where the link leads in the document; nil for one that
	// leads elsewhere.
	Dest *Destination
	// URI is the address of a link to the web (or mailto:).
	URI string
	// External is set for a link to another file or an application; File
	// names it. The widget does not follow it.
	External bool
	File     string
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

// Links returns the links of page (0-based) that lead somewhere, in the
// order of the page's annotations, leaving out hidden ones.
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
			URI:      l.Target.URI,
			External: l.Target.External,
			File:     l.Target.File,
			Dest:     s.destination(&l.Target),
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

// followLink goes where l leads: it asks OnLinkTapped first, if set; then
// it goes to the destination, or opens the URI in the browser. Links to
// other files are not followed.
func (d *Document) followLink(l Link) {
	if d.OnLinkTapped != nil && d.OnLinkTapped(l) {
		return
	}
	switch {
	case l.Dest != nil:
		d.GoTo(*l.Dest)
	case l.URI != "":
		if u, err := url.Parse(l.URI); err == nil {
			_ = d.openURL(u)
		}
	}
}

var (
	_ desktop.Hoverable  = (*pageArea)(nil)
	_ desktop.Cursorable = (*pageArea)(nil)
)

// MouseIn and MouseMoved choose the pointer for what is under it. With the
// pointer over the pages, their window is watched for pinches.
func (a *pageArea) MouseIn(ev *desktop.MouseEvent) {
	a.doc.watchWindow()
	a.MouseMoved(ev)
}

func (a *pageArea) MouseMoved(ev *desktop.MouseEvent) {
	a.cursor = a.doc.cursor(ev.Position)
}

func (a *pageArea) MouseOut() {
	a.cursor = desktop.DefaultCursor
}

// Cursor is a hand over a link, an I-beam over text while selecting it.
func (a *pageArea) Cursor() desktop.Cursor {
	if a.cursor == nil {
		return desktop.DefaultCursor
	}
	return a.cursor
}
