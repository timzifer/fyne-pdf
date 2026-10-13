package pdf

import "math"

// ViewSettings is how a PDF asks to be opened.
type ViewSettings struct {
	// Open is where to start; nil if the PDF does not say. It is nil as
	// well for anything that would run or lead out of the document: a
	// script, an address, another file.
	Open *Destination
	// PageMode is what is shown with the pages: "UseNone" (nothing, the
	// default), "UseOutlines" (the outline), "UseThumbs" (thumbnails),
	// "FullScreen", "UseOC" (the layers) or "UseAttachments".
	PageMode string
	// PageLayout is how the pages are laid out: "SinglePage" (one at a
	// time, the default), "OneColumn" (all pages one below the other),
	// "TwoColumnLeft" and "TwoColumnRight" (two columns), "TwoPageLeft"
	// and "TwoPageRight" (two pages at a time).
	PageLayout string
	// RightToLeft says that the document reads from right to left: the
	// pages of a spread run from the right.
	RightToLeft bool
	// DisplayDocTitle says that the title bar is to show the title of
	// the document ([Metadata.Title]) rather than the name of its file.
	DisplayDocTitle bool
}

// ViewSettings returns how the PDF asks to be opened; the defaults after
// Close.
func (s *Source) ViewSettings() ViewSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return ViewSettings{PageMode: "UseNone", PageLayout: "SinglePage"}
	}
	v := s.doc.ViewSettings()
	settings := ViewSettings{
		PageMode:        v.PageMode,
		PageLayout:      v.PageLayout,
		RightToLeft:     v.RightToLeft,
		DisplayDocTitle: v.DisplayDocTitle,
	}
	if v.Open == nil {
		return settings
	}
	settings.Open = s.destination(v.Open)
	if settings.Open == nil {
		// A named action: the first or the last page.
		if page, ok := v.Open.NamedPage(0, s.doc.NumPages()); ok {
			settings.Open = &Destination{Page: page, Fit: "XYZ", X: math.NaN(), Y: math.NaN()}
		}
	}
	return settings
}

// ViewSettings returns how the loaded PDF asks to be opened, see
// [Source.ViewSettings].
func (d *Document) ViewSettings() ViewSettings {
	if src := d.loadedSource(); src != nil {
		return src.ViewSettings()
	}
	return ViewSettings{PageMode: "UseNone", PageLayout: "SinglePage"}
}

// followViewSettings opens the document the way it asks, see
// Document.FollowViewSettings.
func (d *Document) followViewSettings(src *Source) {
	if !d.FollowViewSettings {
		return
	}
	v := src.ViewSettings()
	// "SinglePage" is also what a PDF that does not say comes with, so
	// it does not end a continuous view. The layouts in two columns have
	// no view mode of their own yet.
	if v.PageLayout == "OneColumn" {
		d.SetViewMode(ViewContinuous)
	}
	// The thumbnails of "UseThumbs" are shown anyway; never full screen
	// without the user: that is the application's to offer.
	switch v.PageMode {
	case "UseOutlines":
		d.ShowOutline(true)
	case "UseAttachments":
		d.ShowAttachments(true)
	}
	if v.Open != nil {
		d.GoTo(*v.Open)
	}
}
