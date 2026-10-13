package pdf

import (
	"github.com/timzifer/cera"
)

// SetMetadata sets the metadata of the document: its information
// dictionary and its XMP packet, which then say the same. Empty fields and
// zero dates are removed. The XMP packet is m.XMP, or the one the document
// has when m.XMP is nil, with the properties that mirror the fields
// changed and what else it holds kept; start from [Document.Metadata] to
// change single fields.
//
// It is a change like those of the annotation tools: a step of undo,
// written by [Document.Save]. It reports false if the document does not
// allow it, with the error through OnError.
func (d *Document) SetMetadata(m Metadata) bool {
	return d.edit(noPage, "could not change the metadata", func(e *cera.Editor) error {
		return e.SetMetadata(cera.Metadata{
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
		})
	})
}

// PageSize returns the width and height of page (0-based) in points (1/72
// inch), as it is displayed; 0, 0 for a page the loaded PDF does not have.
func (d *Document) PageSize(page int) (width, height float64) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if page < 0 || page >= len(d.bounds) {
		return 0, 0
	}
	return float64(d.bounds[page].Dx()), float64(d.bounds[page].Dy())
}
