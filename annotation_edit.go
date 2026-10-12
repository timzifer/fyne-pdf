package pdf

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/cockroachdb/errors"
	"github.com/timzifer/cera"
	"github.com/timzifer/fyne_lucide"
)

// annotationColors are the colours the popup of an annotation offers.
var annotationColors = []color.RGBA{
	{255, 235, 59, 255}, // yellow
	{255, 152, 0, 255},  // orange
	{244, 67, 54, 255},  // red
	{233, 30, 99, 255},  // pink
	{76, 175, 80, 255},  // green
	{33, 150, 243, 255}, // blue
	{156, 39, 176, 255}, // purple
	{0, 0, 0, 255},      // black
}

// movable reports whether an annotation of subtype can be moved: not text
// markup, which belongs to its text.
func movable(subtype string) bool {
	switch subtype {
	case "Highlight", "Underline", "Squiggly", "StrikeOut", "Redact":
		return false
	}
	return true
}

// editAnnotation returns annotation index of page as the editor takes it.
func (s *Source) editAnnotation(page, index int) (cera.NewAnnotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doc == nil {
		return cera.NewAnnotation{}, errors.New("pdf source is closed")
	}
	p, err := s.doc.Page(page)
	if err != nil {
		return cera.NewAnnotation{}, errors.Wrapf(err, "could not read pdf-page %d", page)
	}
	defer p.Release()
	a, ok := p.EditAnnotation(index)
	if !ok {
		return cera.NewAnnotation{}, errors.Newf("annotation %d of page %d cannot be changed", index, page)
	}
	return a, nil
}

// changeAnnotation changes a as change says and writes it.
func (d *Document) changeAnnotation(a Annotation, change func(n *cera.NewAnnotation)) bool {
	src := d.loadedSource()
	if src == nil {
		return false
	}
	n, err := src.editAnnotation(a.Page, a.Index)
	if err != nil {
		d.reportError(a.Page, err)
		return false
	}
	change(&n)
	n.Modified = time.Now()
	key := annotKey{a.Page, a.Index}
	if !d.editPage(a.Page, func(e *cera.Editor) error { return e.SetAnnotation(a.Page, a.Index, n) }) {
		return false
	}
	// The annotation stays selected once it is read again.
	d.comments.reselect = &key
	return true
}

// SetAnnotationContents sets the text of a, one of [Document.Annotations].
func (d *Document) SetAnnotationContents(a Annotation, text string) bool {
	return d.changeAnnotation(a, func(n *cera.NewAnnotation) { n.Contents = text })
}

// SetAnnotationColor sets the colour of a, one of [Document.Annotations].
func (d *Document) SetAnnotationColor(a Annotation, c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return d.changeAnnotation(a, func(n *cera.NewAnnotation) {
		n.Color = []float64{float64(r) / 0xffff, float64(g) / 0xffff, float64(b) / 0xffff}
	})
}

// MoveAnnotation moves a, one of [Document.Annotations], by dx, dy points
// of the page. Text markup — highlights, underlines, strikethroughs —
// belongs to its text and is not moved. Dragging the selected annotation
// with [ToolNone] moves it, too.
func (d *Document) MoveAnnotation(a Annotation, dx, dy float64) bool {
	if !movable(a.Subtype) || dx == 0 && dy == 0 {
		return false
	}
	return d.changeAnnotation(a, func(n *cera.NewAnnotation) {
		m := &n.Markup
		move := func(p *[2]float64) { p[0] += dx; p[1] += dy }
		m.Bounds = cera.Rect{X0: m.Bounds.X0 + dx, Y0: m.Bounds.Y0 + dy, X1: m.Bounds.X1 + dx, Y1: m.Bounds.Y1 + dy}
		for _, stroke := range m.Ink {
			for i := range stroke {
				move(&stroke[i])
			}
		}
		for _, points := range [][][2]float64{m.Vertices, m.Line, m.Callout} {
			for i := range points {
				move(&points[i])
			}
		}
		for i := range m.Quads {
			for k := range m.Quads[i].P {
				move(&m.Quads[i].P[k])
			}
		}
		if p := m.Popup; p != nil {
			p.Bounds = cera.Rect{X0: p.Bounds.X0 + dx, Y0: p.Bounds.Y0 + dy, X1: p.Bounds.X1 + dx, Y1: p.Bounds.Y1 + dy}
		}
	})
}

// editText asks for the new text of a.
func (d *Document) editText(a Annotation) {
	d.comments.hidePopup()
	d.askTextWith("Edit text", a.Contents, func(text string) {
		d.SetAnnotationContents(a, text)
	})
}

// annotationActions are the buttons of the popup of a: edit its text,
// choose its colour, delete it.
func (d *Document) annotationActions(a Annotation) fyne.CanvasObject {
	edit := widget.NewButtonWithIcon("Edit", fyne_lucide.Icon(fyne_lucide.IconPencil), func() { d.editText(a) })
	remove := widget.NewButtonWithIcon("Delete", fyne_lucide.Icon(fyne_lucide.IconTrash2), func() {
		d.comments.hidePopup()
		d.DeleteAnnotation(a)
	})
	remove.Importance = widget.DangerImportance
	swatches := container.NewHBox()
	for _, c := range annotationColors {
		swatches.Add(newSwatch(c, func() {
			d.comments.hidePopup()
			d.SetAnnotationColor(a, c)
		}))
	}
	return container.NewVBox(swatches, container.NewHBox(edit, remove))
}

// swatch is a tappable square of a colour.
type swatch struct {
	widget.BaseWidget
	color    color.Color
	onTapped func()
}

func newSwatch(c color.Color, tapped func()) *swatch {
	s := &swatch{color: c, onTapped: tapped}
	s.ExtendBaseWidget(s)
	return s
}

func (s *swatch) Tapped(*fyne.PointEvent) { s.onTapped() }

func (s *swatch) CreateRenderer() fyne.WidgetRenderer {
	r := canvas.NewRectangle(s.color)
	r.CornerRadius = 4
	r.SetMinSize(fyne.NewSize(22, 22))
	return widget.NewSimpleRenderer(r)
}

var _ fyne.Tappable = (*swatch)(nil)

// startMove starts moving the selected annotation if pos in the page area
// is on it.
func (d *Document) startMove(pos fyne.Position) bool {
	p := d.comments
	if !p.hasSelected {
		return false
	}
	a, ok := d.annotationAt(pos)
	if !ok || a.Page != p.selected.page || a.Index != p.selected.index || !movable(a.Subtype) {
		return false
	}
	p.hidePopup()
	x, y := d.pagePoint(a.Page, pos)
	d.moving = &annotationMove{annot: a, from: [2]float64{x, y}}
	return true
}

// annotationMove is the move of an annotation by a drag.
type annotationMove struct {
	annot Annotation
	// from is where the drag started, dx, dy how far it went, in points.
	from   [2]float64
	dx, dy float64
	// outline shows where the annotation goes.
	outline *canvas.Rectangle
}

// extendMove moves the outline of the annotation to pos in the page area.
func (d *Document) extendMove(pos fyne.Position) {
	m := d.moving
	x, y := d.pagePoint(m.annot.Page, pos)
	m.dx, m.dy = x-m.from[0], y-m.from[1]
	b := m.annot.Bounds
	if m.outline == nil {
		m.outline = canvas.NewRectangle(color.Transparent)
		m.outline.StrokeColor = selectionColor
		m.outline.StrokeWidth = 2
	}
	lo := d.areaPoint(m.annot.Page, b.X0+m.dx, b.Y0+m.dy)
	hi := d.areaPoint(m.annot.Page, b.X1+m.dx, b.Y1+m.dy)
	m.outline.Move(lo)
	m.outline.Resize(fyne.NewSize(hi.X-lo.X, hi.Y-lo.Y))
	d.area.Refresh()
}

// finishMove moves the annotation where the drag ended.
func (d *Document) finishMove() {
	m := d.moving
	d.moving = nil
	d.area.Refresh()
	d.MoveAnnotation(m.annot, m.dx, m.dy)
}
