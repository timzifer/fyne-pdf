package pdf

import (
	"image/color"
	"math"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/timzifer/cera"
)

type (
	// Annotation is a markup annotation of a page: a note, a highlight, a
	// shape, a stamp and the like, with what it says about itself. The
	// geometry is in points (1/72 inch) of the displayed page from its
	// top left.
	Annotation struct {
		// Page is the index (0-based) of the page, Index the position of
		// the annotation in the page's /Annots: together they name it.
		Page, Index int
		// Subtype is the kind: "Text" (a note), "Highlight", "FreeText",
		// "Ink", "Square", "Stamp", ...
		Subtype string
		// Contents is the text of the annotation.
		Contents string
		// Author is who made it, Subject what it is about.
		Author, Subject string
		// Created and Modified are zero when the annotation does not say.
		Created, Modified time.Time
		// Color is the colour of the annotation, nil if it has none.
		Color color.Color
		// Bounds is the rectangle of the annotation; Rects the areas of
		// text a Highlight, Underline, Squiggly or StrikeOut marks.
		Bounds Rect
		Rects  []Rect
		// Icon names the icon of a note or stamp: "Note", "Comment",
		// "Approved", ...
		Icon string
		// State and StateModel are the review state a reply sets on the
		// annotation it replies to: "Accepted" of "Review", ...
		State, StateModel string
		// ReplyTo is the Index of the annotation this one replies to, -1
		// if none. Group says that it is a part of that one rather than a
		// reply: viewers show both as one.
		ReplyTo int
		Group   bool
		// Popup is the window that shows the text, nil for none.
		Popup *AnnotationPopup
		// File is the file a FileAttachment annotation embeds, nil for
		// other annotations.
		File *Attachment
	}

	// AnnotationPopup is the window of an annotation that shows its text.
	AnnotationPopup struct {
		Bounds Rect
		// Open says that the window is shown at first.
		Open bool
	}
)

// Annotations returns the markup annotations of page (0-based) in the order
// of its /Annots, leaving out hidden ones but replies: those a comments
// panel lists.
// Links, form fields and popup windows are not markup annotations.
func (s *Source) Annotations(page int) ([]Annotation, error) {
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

	var out []Annotation
	for _, a := range p.Annotations() {
		m := a.Markup
		// Replies are listed even when hidden, as review states mostly
		// are: they have no place on the page of their own.
		if m == nil || m.Reply == nil && a.Flags&(cera.AnnotHidden|cera.AnnotNoView) != 0 {
			continue
		}
		annot := Annotation{
			Page:       page,
			Index:      a.Index,
			Subtype:    a.Subtype,
			Contents:   a.Contents,
			Author:     m.Author,
			Subject:    m.Subject,
			Created:    m.Created,
			Modified:   m.Modified,
			Color:      componentColor(m.Color),
			Bounds:     rectOf(m.Bounds),
			Icon:       m.Icon,
			State:      m.State,
			StateModel: m.StateModel,
			ReplyTo:    -1,
		}
		switch a.Subtype {
		case "Highlight", "Underline", "Squiggly", "StrikeOut":
			for _, q := range m.Quads {
				annot.Rects = append(annot.Rects, rectOf(q.Bounds()))
			}
		}
		if r := m.Reply; r != nil {
			annot.ReplyTo, annot.Group = r.To, r.Group
		}
		if pp := m.Popup; pp != nil {
			annot.Popup = &AnnotationPopup{Bounds: rectOf(pp.Bounds), Open: pp.Open}
		}
		if a.File != nil {
			file := s.attachment(a.File, page, a.Index)
			annot.File = &file
		}
		out = append(out, annot)
	}
	return out, nil
}

func rectOf(r cera.Rect) Rect {
	return Rect{X0: r.X0, Y0: r.Y0, X1: r.X1, Y1: r.Y1}
}

// componentColor turns gray, RGB or CMYK components from 0 to 1 into a
// colour; nil for none.
func componentColor(c []float64) color.Color {
	b := func(v float64) uint8 { return uint8(math.Round(255 * math.Max(0, math.Min(1, v)))) }
	switch len(c) {
	case 1:
		return color.RGBA{b(c[0]), b(c[0]), b(c[0]), 255}
	case 3:
		return color.RGBA{b(c[0]), b(c[1]), b(c[2]), 255}
	case 4:
		k := 1 - c[3]
		return color.RGBA{b((1 - c[0]) * k), b((1 - c[1]) * k), b((1 - c[2]) * k), 255}
	}
	return nil
}

// contains reports whether the point x, y (in points of the displayed
// page) is on the annotation: on the text it marks, else in its bounds.
func (a *Annotation) contains(x, y float64) bool {
	in := func(r Rect) bool { return x >= r.X0 && x <= r.X1 && y >= r.Y0 && y <= r.Y1 }
	if len(a.Rects) > 0 {
		for _, r := range a.Rects {
			if in(r) {
				return true
			}
		}
		return false
	}
	return in(a.Bounds)
}
