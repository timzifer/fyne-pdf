package pdf

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/cockroachdb/errors"
	"github.com/timzifer/cera"
	"github.com/timzifer/fyne_lucide"
)

// attachmentsWidth is the minimum width of the attachments panel.
const attachmentsWidth = 260

type (
	// Attachment is a file a PDF embeds: the XML of an e-invoice, source
	// data, another PDF.
	Attachment struct {
		// Name is the file name, Description what the document says
		// about the file.
		Name        string
		Description string
		// MIME is the media type, such as "application/xml"; empty if the
		// document gives none.
		MIME string
		// Size is the size in bytes as the document states it, -1 if it
		// does not.
		Size int64
		// Created and Modified are zero when the document does not say.
		Created, Modified time.Time
		// Relationship is what the file is to the document
		// (/AFRelationship): "Source", "Data", "Alternative",
		// "Supplement", ...; empty if it does not say. The XML of an
		// e-invoice is "Alternative" or "Data".
		Relationship string
		// Page and Index name the file attachment annotation that holds
		// the file, as in [Annotation]; Page is -1 for a file of the
		// document itself.
		Page, Index int

		src  *Source
		file *cera.Attachment
	}

	// attachmentsPanel lists the files the document embeds beside the
	// pages, each with a button to save it.
	attachmentsPanel struct {
		doc *Document
		// box holds the list with its minimum width; it is shown while
		// wanted and there is something to list.
		box    *fyne.Container
		list   *fyne.Container
		scroll *container.Scroll
		wanted bool

		// files are the files listed: those of the document, then those
		// of the file annotations as far as read.
		files []Attachment
		rows  []*attachmentRow
		// selected is the row selected, -1 for none.
		selected int
	}

	// attachmentRow is a file in the attachments panel.
	attachmentRow struct {
		widget.BaseWidget

		panel      *attachmentsPanel
		index      int
		background *canvas.Rectangle
	}
)

// Open returns the content of the file, decoded and, in an encrypted PDF,
// decrypted. It fails once the PDF it comes from is closed; the widget
// closes a PDF also when it shows a changed version of it, so take the
// files from [Document.Attachments] when they are needed.
func (a Attachment) Open() (io.ReadCloser, error) {
	if a.src == nil || a.file == nil {
		return nil, errors.New("the attachment has no content")
	}
	a.src.mu.Lock()
	defer a.src.mu.Unlock()
	if a.src.doc == nil {
		return nil, errors.New("pdf source is closed")
	}
	r, err := a.file.Open()
	if err != nil {
		return nil, errors.Wrapf(err, "could not read the attachment %q", a.Name)
	}
	defer func() { _ = r.Close() }()
	// Read here: the source is not to be read while it renders.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, errors.Wrapf(err, "could not read the attachment %q", a.Name)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// fileName returns the name of the file without directories, for a save
// dialog: a PDF may name its files as it likes.
func (a Attachment) fileName() string {
	name := path.Base(strings.ReplaceAll(a.Name, "\\", "/"))
	if name == "." || name == "/" || name == ".." {
		return "attachment"
	}
	return name
}

// attachment wraps f, the file of the annotation index of page, or of the
// document for page -1.
func (s *Source) attachment(f *cera.Attachment, page, index int) Attachment {
	return Attachment{
		Name:         f.Name,
		Description:  f.Description,
		MIME:         f.MIME,
		Size:         f.Size,
		Created:      f.Created,
		Modified:     f.Modified,
		Relationship: f.Relationship,
		Page:         page,
		Index:        index,
		src:          s,
		file:         f,
	}
}

// Attachments returns the files the PDF embeds, nil for a PDF without, or
// after Close. The files of file attachment annotations are on their
// pages, see [Annotation.File].
func (s *Source) Attachments() []Attachment {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return nil
	}
	var out []Attachment
	for _, f := range s.doc.Attachments() {
		out = append(out, s.attachment(f, -1, -1))
	}
	return out
}

// Attachments returns the files of the loaded PDF the attachments panel
// lists: those of the document, then those of its file attachment
// annotations as far as they are read; they are read in the background
// after loading.
func (d *Document) Attachments() []Attachment {
	return slices.Clone(d.attachments.files)
}

// ShowAttachments shows or hides the attachments panel beside the pages:
// a list of the files the document embeds, each with a button to save it.
// It is hidden at first; the toolbar has a button for it while the
// document has such files. The widget never opens a file.
func (d *Document) ShowAttachments(show bool) {
	d.attachments.wanted = show
	d.updateAttachments()
}

// AttachmentsShown reports whether the attachments panel is shown.
func (d *Document) AttachmentsShown() bool {
	return d.attachments.box.Visible()
}

// updateAttachments shows the attachments panel and its button as wanted
// and as there are files.
func (d *Document) updateAttachments() {
	p := d.attachments
	has := len(p.files) > 0
	if show := p.wanted && has; show != p.box.Visible() {
		if show {
			p.box.Show()
		} else {
			p.box.Hide()
		}
		d.left.Refresh()
		d.base.Refresh()
	}
	if !has {
		d.attachmentsButton.Hide()
		return
	}
	if p.box.Visible() {
		d.attachmentsButton.Importance = widget.MediumImportance
	} else {
		d.attachmentsButton.Importance = widget.LowImportance
	}
	d.attachmentsButton.Show()
	d.attachmentsButton.Refresh()
}

// saveAttachment asks where to save a and writes it there.
func (d *Document) saveAttachment(a Attachment) {
	w := d.window()
	if w == nil {
		return
	}
	save := dialog.NewFileSave(func(out fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if out == nil {
			return
		}
		if err := a.writeTo(out); err != nil {
			dialog.ShowError(err, w)
		}
	}, w)
	save.SetFileName(a.fileName())
	save.Show()
}

// writeTo writes the content of the file to w and closes it.
func (a Attachment) writeTo(w io.WriteCloser) (err error) {
	defer func() {
		if cerr := w.Close(); err == nil && cerr != nil {
			err = errors.Wrapf(cerr, "could not save the attachment %q", a.Name)
		}
	}()
	r, err := a.Open()
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	if _, err := io.Copy(w, r); err != nil {
		return errors.Wrapf(err, "could not save the attachment %q", a.Name)
	}
	return nil
}

func newAttachmentsPanel(d *Document) *attachmentsPanel {
	p := &attachmentsPanel{doc: d, selected: -1}
	p.list = container.NewVBox()
	p.scroll = container.NewVScroll(p.list)
	width := canvas.NewRectangle(color.Transparent)
	width.SetMinSize(fyne.NewSize(attachmentsWidth, 0))
	p.box = container.NewStack(width, p.scroll)
	p.box.Hide()
	return p
}

// reset lists files, those of the document; the files of the annotations
// follow with addPage.
func (p *attachmentsPanel) reset(files []Attachment) {
	p.files, p.rows, p.selected = nil, nil, -1
	p.list.Objects = nil
	p.add(files)
	p.scroll.ScrollToTop()
}

// addPage lists the files of the file attachment annotations among
// annots, the annotations of a page read in the background.
func (p *attachmentsPanel) addPage(annots []Annotation) {
	var files []Attachment
	for _, a := range annots {
		if a.File != nil {
			files = append(files, *a.File)
		}
	}
	if len(files) > 0 {
		p.add(files)
	}
}

func (p *attachmentsPanel) add(files []Attachment) {
	for _, f := range files {
		row := &attachmentRow{panel: p, index: len(p.files), background: canvas.NewRectangle(color.Transparent)}
		row.ExtendBaseWidget(row)
		p.files = append(p.files, f)
		p.rows = append(p.rows, row)
		p.list.Add(row)
	}
	p.list.Refresh()
	p.doc.updateAttachments()
}

// selectRow marks row i, -1 for none, and scrolls the list to it.
func (p *attachmentsPanel) selectRow(i int) {
	old := p.selected
	p.selected = i
	for _, j := range []int{old, i} {
		if j >= 0 && j < len(p.rows) {
			p.rows[j].Refresh()
		}
	}
	if i < 0 || i >= len(p.rows) {
		return
	}
	row := p.rows[i]
	top, bottom := row.Position().Y, row.Position().Y+row.Size().Height
	switch view := p.scroll.Size().Height; {
	case top < p.scroll.Offset.Y:
		p.scroll.ScrollToOffset(fyne.NewPos(0, top))
	case bottom > p.scroll.Offset.Y+view:
		p.scroll.ScrollToOffset(fyne.NewPos(0, bottom-view))
	}
}

// selectAnnotation shows the panel with the row of the file of a, a file
// attachment annotation.
func (p *attachmentsPanel) selectAnnotation(a Annotation) {
	for i, f := range p.files {
		if f.Page == a.Page && f.Index == a.Index {
			p.doc.ShowAttachments(true)
			p.selectRow(i)
			return
		}
	}
}

// Tapped selects the row, and shows the annotation that holds its file.
func (r *attachmentRow) Tapped(*fyne.PointEvent) {
	p := r.panel
	p.selectRow(r.index)
	f := p.files[r.index]
	if a, ok := p.doc.comments.annotation(annotKey{f.Page, f.Index}); ok {
		p.doc.selectAnnotation(a, true, false)
	}
}

func (r *attachmentRow) CreateRenderer() fyne.WidgetRenderer {
	f := r.panel.files[r.index]
	name := widget.NewLabelWithStyle(f.Name, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	name.Truncation = fyne.TextTruncateEllipsis
	lines := []fyne.CanvasObject{name}
	if f.Description != "" {
		description := widget.NewLabel(f.Description)
		description.Wrapping = fyne.TextWrapWord
		lines = append(lines, description)
	}
	if info := f.info(); info != "" {
		meta := canvas.NewText(info, theme.Color(theme.ColorNamePlaceHolder))
		meta.TextSize = theme.CaptionTextSize()
		lines = append(lines, container.NewPadded(meta))
	}
	save := widget.NewButtonWithIcon("", fyne_lucide.Icon(fyne_lucide.IconDownload), func() {
		r.panel.doc.saveAttachment(f)
	})
	save.Importance = widget.LowImportance
	row := container.NewBorder(nil, widget.NewSeparator(), nil, container.NewVBox(save), container.NewVBox(lines...))
	r.updateBackground()
	return widget.NewSimpleRenderer(container.NewStack(r.background, row))
}

func (r *attachmentRow) Refresh() {
	r.updateBackground()
	r.BaseWidget.Refresh()
}

func (r *attachmentRow) updateBackground() {
	if r.panel.selected == r.index {
		r.background.FillColor = theme.Color(theme.ColorNameSelection)
	} else {
		r.background.FillColor = color.Transparent
	}
	r.background.Refresh()
}

var _ fyne.Tappable = (*attachmentRow)(nil)

// info says the size of the file and, for the file of an annotation, its
// page.
func (a Attachment) info() string {
	var parts []string
	if a.Size >= 0 {
		parts = append(parts, byteSize(a.Size))
	}
	if a.Page >= 0 {
		parts = append(parts, fmt.Sprintf("page %d", a.Page+1))
	}
	return strings.Join(parts, " · ")
}

// byteSize gives a number of bytes in words.
func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}
