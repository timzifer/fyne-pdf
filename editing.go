package pdf

import (
	"bytes"
	"context"
	"io"

	"github.com/cockroachdb/errors"
	"github.com/timzifer/cera"
)

type (
	// docVersion is a version of the document's file: the first n bytes
	// of *data. The versions made by incremental updates share data, each
	// a prefix of the next.
	docVersion struct {
		data *[]byte
		n    int
		// page is the page the change to this version was made on; -1
		// for the first version.
		page int
	}

	// editHistory are the versions of the document's file: the one loaded,
	// then one per change. pos is the version shown, saved the one last
	// loaded or saved; those after pos can be redone.
	editHistory struct {
		versions   []docVersion
		pos, saved int
		// password opens the versions of an encrypted document.
		password string
	}
)

func (v docVersion) bytes() []byte {
	return (*v.data)[:v.n]
}

// reset starts the history with contents.
func (h *editHistory) reset(contents []byte, password string) {
	data := contents[:len(contents):len(contents)]
	h.versions = []docVersion{{data: &data, n: len(data), page: -1}}
	h.pos, h.saved, h.password = 0, 0, password
}

// current returns the version shown; ok is false before a load.
func (h *editHistory) current() (docVersion, bool) {
	if len(h.versions) == 0 {
		return docVersion{}, false
	}
	return h.versions[h.pos], true
}

// push makes out, the file after a change on page, the version shown, and
// drops the versions that could be redone.
func (h *editHistory) push(out []byte, page int) {
	cur := h.versions[h.pos]
	h.versions = h.versions[:h.pos+1]
	if bytes.HasPrefix(out, cur.bytes()) {
		// An incremental update: the new bytes follow those of cur.
		*cur.data = append((*cur.data)[:cur.n], out[cur.n:]...)
		h.versions = append(h.versions, docVersion{data: cur.data, n: len(out), page: page})
	} else {
		data := out
		h.versions = append(h.versions, docVersion{data: &data, n: len(out), page: page})
	}
	h.pos++
	if h.saved >= h.pos {
		// The saved version can no longer be reached.
		h.saved = -1
	}
}

// update applies edit to an editor of the document and returns the file
// after it: the file as it is with an incremental update, or, where that
// is not possible for a document that is not encrypted, a new file.
func (s *Source) update(edit func(e *cera.Editor) error) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return nil, errors.New("pdf source is closed")
	}
	e := s.doc.Edit()
	if err := edit(e); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err := e.Update(&buf)
	if errors.Is(err, cera.ErrNoUpdate) && !s.doc.Encrypted() {
		// A repaired file: written anew.
		buf.Reset()
		err = e.Save(&buf)
	}
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// editPage applies edit, a change of the annotations of page, makes the
// result the version shown, and reports an error through OnError.
func (d *Document) editPage(page int, edit func(e *cera.Editor) error) bool {
	src := d.loadedSource()
	if src == nil {
		return false
	}
	// The source is free sooner without the render in progress.
	d.mutex.Lock()
	if d.inflightCancel != nil {
		d.inflightCancel()
	}
	d.mutex.Unlock()

	out, err := src.update(edit)
	if err == nil {
		var next *Source
		next, err = d.open(out, d.history.password)
		if err == nil {
			d.history.push(out, page)
			d.swapSource(next, page)
			d.changed()
			return true
		}
	}
	d.reportError(page, errors.Wrap(err, "could not change the annotations"))
	return false
}

// Modified reports whether the document was changed since it was loaded
// or saved.
func (d *Document) Modified() bool {
	return len(d.history.versions) > 0 && d.history.pos != d.history.saved
}

// CanUndo and CanRedo report whether there is a change to undo or redo.
func (d *Document) CanUndo() bool {
	return d.history.pos > 0
}

func (d *Document) CanRedo() bool {
	return d.history.pos+1 < len(d.history.versions)
}

// Undo undoes the last change of the annotations; Ctrl+Z.
func (d *Document) Undo() {
	if d.CanUndo() {
		page := d.history.versions[d.history.pos].page
		d.showVersion(d.history.pos-1, page)
	}
}

// Redo makes the change undone last again; Ctrl+Y or Ctrl+Shift+Z.
func (d *Document) Redo() {
	if d.CanRedo() {
		d.showVersion(d.history.pos+1, d.history.versions[d.history.pos+1].page)
	}
}

// showVersion shows version i of the history; page is the page that
// differs from the version shown.
func (d *Document) showVersion(i, page int) {
	src, err := d.open(d.history.versions[i].bytes(), d.history.password)
	if err != nil {
		d.reportError(max(page, 0), err)
		return
	}
	d.history.pos = i
	d.swapSource(src, page)
	d.changed()
}

// Contents returns the file of the document as it is now, with the
// changes made to it.
func (d *Document) Contents() []byte {
	if v, ok := d.history.current(); ok {
		return bytes.Clone(v.bytes())
	}
	return nil
}

// Save writes the document with its changes to w. A document that is
// signed or encrypted is written as it was loaded followed by incremental
// updates, which keep its signatures valid and it encrypted; others are
// written as a new file. Afterwards the document counts as unchanged.
func (d *Document) Save(w io.Writer) error {
	v, ok := d.history.current()
	if !ok {
		return errors.New("no pdf loaded")
	}
	data := v.bytes()
	out := data
	if src := d.loadedSource(); src != nil && !src.Encrypted() && d.history.pos > 0 {
		var buf bytes.Buffer
		src.mu.Lock()
		err := src.doc.Edit().Save(&buf)
		src.mu.Unlock()
		if err == nil {
			out = buf.Bytes()
		} else if !errors.Is(err, cera.ErrSigned) && !errors.Is(err, cera.ErrNoAssembly) {
			return errors.Wrap(err, "could not save the pdf")
		}
	}
	if _, err := w.Write(out); err != nil {
		return errors.Wrap(err, "could not save the pdf")
	}
	if len(out) != len(data) || !bytes.Equal(out, data) {
		// A new file: shown from now on, so that the indices of the
		// annotations are those of the file. It cannot be undone.
		src, err := d.open(out, d.history.password)
		if err == nil {
			d.history.reset(out, d.history.password)
			d.swapSource(src, -1)
		}
	}
	d.history.saved = d.history.pos
	d.changed()
	return nil
}

// changed tells OnChanged.
func (d *Document) changed() {
	if d.OnChanged != nil {
		d.OnChanged()
	}
}

// swapSource shows src, a version of the document that differs from the
// one shown on page (-1: on any), keeping the view: the current page, the
// zoom, the scroll offset, the tool. The images of other pages are kept;
// that of page is shown until it is rendered again.
func (d *Document) swapSource(src *Source, page int) {
	ctx, cancel := context.WithCancel(context.Background())
	d.mutex.Lock()
	old, oldCancel := d.source, d.cancel
	if d.renderTimer != nil && d.renderTimer.Stop() {
		d.pending.Done()
	}
	d.renderTimer = nil
	d.source, d.ctx, d.cancel = src, ctx, cancel
	d.jobs, d.rendering, d.inflightCancel = nil, false, nil
	d.failed = map[int]float64{}
	for p, r := range d.rendered {
		if page < 0 || p == page {
			r.stale = true
		}
	}
	if page < 0 {
		d.links, d.texts = map[int][]Link{}, map[int]*cera.PageText{}
	} else {
		delete(d.links, page)
		delete(d.texts, page)
	}
	d.mutex.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if old != nil {
		_ = old.Close()
	}

	d.selection = nil
	d.comments.reset()
	d.loadAnnotations(src, ctx, src.PageCount())
	d.renderThumbnails(src, ctx, page)
	if d.search.running {
		d.Search(d.search.query, d.search.opt)
	}
	d.updateViews()
	d.scheduleRender(0)
}
