package pdf

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/timzifer/cera"
)

// The annotation tools: see [Document.SetTool].
const (
	// ToolHighlight, ToolUnderline and ToolStrikeOut mark the text a drag
	// selects, or the word a double click does.
	ToolHighlight Tool = iota + ToolSelect + 1
	ToolUnderline
	ToolStrikeOut
	// ToolNote places a note where the page is tapped and asks for its
	// text.
	ToolNote
	// ToolInk draws what a drag draws.
	ToolInk
	// ToolRect, ToolEllipse and ToolLine draw a rectangle, an ellipse or
	// a line from where a drag starts to where it ends.
	ToolRect
	ToolEllipse
	ToolLine
	// ToolFreeText places a text box where the page is tapped, or over
	// the area a drag spans, and asks for its text.
	ToolFreeText
)

const (
	// noteSize is the size of a note's icon, freeTextSize that of a text
	// box placed with a tap, in points.
	noteSize = 20
	// minShapeSize is the least size of a drawn shape in points; smaller
	// drags draw nothing.
	minShapeSize = 2
	// strokeWidth is the width of the lines drawn, in points.
	strokeWidth = 2
)

var freeTextSize = fyne.NewSize(200, 40)

// sketch is what a drag draws with a drawing tool, until it ends.
type sketch struct {
	page int
	// points are the points of the drag in points of the page.
	points [][2]float64
	// objects draw it in the page area.
	objects []fyne.CanvasObject
}

// selectsText reports whether a drag with t selects text.
func (t Tool) selectsText() bool {
	return t == ToolSelect || t == ToolHighlight || t == ToolUnderline || t == ToolStrikeOut
}

// markup is the annotation subtype of a text markup tool.
func (t Tool) markup() string {
	switch t {
	case ToolHighlight:
		return "Highlight"
	case ToolUnderline:
		return "Underline"
	case ToolStrikeOut:
		return "StrikeOut"
	}
	return ""
}

// draws reports whether a drag with t draws.
func (t Tool) draws() bool {
	return t == ToolInk || t == ToolRect || t == ToolEllipse || t == ToolLine || t == ToolFreeText
}

// annotationColor returns the colour of annotations made with t as
// components from 0 to 1; nil for cera's default.
func (d *Document) annotationColor(t Tool) []float64 {
	if c := d.AnnotationColor; c != nil {
		r, g, b, _ := c.RGBA()
		return []float64{float64(r) / 0xffff, float64(g) / 0xffff, float64(b) / 0xffff}
	}
	switch t {
	case ToolHighlight, ToolNote:
		return []float64{1, 1, 0}
	case ToolInk, ToolRect, ToolEllipse, ToolLine:
		return []float64{1, 0, 0}
	}
	return nil
}

// markSelection marks the selected text with the markup of the tool.
func (d *Document) markSelection() {
	s := d.selection
	kind := d.tool.markup()
	if s == nil || s.rng.Empty() || kind == "" {
		return
	}
	quads := s.text.Quads(s.rng)
	d.ClearSelection()
	d.editPage(s.page, func(e *cera.Editor) error {
		return e.AddHighlight(s.page, cera.Highlight{
			Kind:   kind,
			Quads:  quads,
			Color:  d.annotationColor(d.tool),
			Author: d.Author,
			Date:   time.Now(),
		})
	})
}

// addAnnotation adds a to page, with the author and the date.
func (d *Document) addAnnotation(page int, a cera.NewAnnotation) bool {
	now := time.Now()
	a.Author, a.Created, a.Modified = d.Author, now, now
	return d.editPage(page, func(e *cera.Editor) error {
		_, err := e.AddAnnotation(page, a)
		return err
	})
}

// startSketch starts a drawing at pos in the page area.
func (d *Document) startSketch(pos fyne.Position) {
	page := d.layout.pageAt(pos.Y, d.areaSize())
	if page < 0 || !d.layout.contains(page) {
		return
	}
	d.sketch = &sketch{page: page}
	d.extendSketch(pos)
}

// extendSketch draws on to pos in the page area.
func (d *Document) extendSketch(pos fyne.Position) {
	s := d.sketch
	if s == nil {
		return
	}
	x, y := d.pagePoint(s.page, pos)
	if d.tool == ToolInk || len(s.points) < 2 {
		s.points = append(s.points, [2]float64{x, y})
	} else {
		s.points[1] = [2]float64{x, y}
	}

	stroke := strokeColor(d.annotationColor(d.tool))
	at := func(p [2]float64) fyne.Position {
		return d.areaPoint(s.page, p[0], p[1])
	}
	first, last := at(s.points[0]), at(s.points[len(s.points)-1])
	lo := fyne.NewPos(min(first.X, last.X), min(first.Y, last.Y))
	size := fyne.NewSize(abs32(last.X-first.X), abs32(last.Y-first.Y))
	width := float32(strokeWidth * d.scale * unitsPerPoint)

	switch d.tool {
	case ToolInk:
		if n := len(s.points); n >= 2 {
			l := canvas.NewLine(stroke)
			l.StrokeWidth = width
			l.Position1, l.Position2 = at(s.points[n-2]), at(s.points[n-1])
			s.objects = append(s.objects, l)
		}
	case ToolLine:
		l := canvas.NewLine(stroke)
		l.StrokeWidth = width
		l.Position1, l.Position2 = first, last
		s.objects = []fyne.CanvasObject{l}
	case ToolEllipse:
		c := canvas.NewCircle(color.Transparent)
		c.StrokeColor, c.StrokeWidth = stroke, width
		c.Move(lo)
		c.Resize(size)
		s.objects = []fyne.CanvasObject{c}
	default: // ToolRect, ToolFreeText
		r := canvas.NewRectangle(color.Transparent)
		r.StrokeColor, r.StrokeWidth = stroke, width
		r.Move(lo)
		r.Resize(size)
		s.objects = []fyne.CanvasObject{r}
	}
	d.area.Refresh()
}

// finishSketch adds the annotation the drawing describes.
func (d *Document) finishSketch() {
	s := d.sketch
	d.sketch = nil
	d.area.Refresh()
	if s == nil || len(s.points) < 2 {
		return
	}
	a, b := s.points[0], s.points[len(s.points)-1]
	bounds := Rect{X0: min(a[0], b[0]), Y0: min(a[1], b[1]), X1: max(a[0], b[0]), Y1: max(a[1], b[1])}
	if d.tool != ToolInk && (bounds.X1-bounds.X0 < minShapeSize && bounds.Y1-bounds.Y0 < minShapeSize) {
		return
	}
	m := cera.Markup{Color: d.annotationColor(d.tool), BorderWidth: strokeWidth}
	switch d.tool {
	case ToolInk:
		m.Ink = [][][2]float64{s.points}
		d.addAnnotation(s.page, cera.NewAnnotation{Subtype: "Ink", Markup: m})
	case ToolLine:
		m.Line = [][2]float64{a, b}
		d.addAnnotation(s.page, cera.NewAnnotation{Subtype: "Line", Markup: m})
	case ToolRect, ToolEllipse:
		subtype := "Square"
		if d.tool == ToolEllipse {
			subtype = "Circle"
		}
		m.Bounds = ceraRect(bounds)
		d.addAnnotation(s.page, cera.NewAnnotation{Subtype: subtype, Markup: m})
	case ToolFreeText:
		d.placeFreeText(s.page, bounds)
	}
}

// placeNote asks for the text of a note and places it at pos in the page
// area.
func (d *Document) placeNote(pos fyne.Position) {
	page := d.layout.pageAt(pos.Y, d.areaSize())
	if page < 0 || !d.layout.contains(page) {
		return
	}
	x, y := d.pagePoint(page, pos)
	d.askText("Note", func(text string) {
		d.addAnnotation(page, cera.NewAnnotation{Subtype: "Text", Contents: text, Markup: cera.Markup{
			Bounds: cera.Rect{X0: x, Y0: y, X1: x + noteSize, Y1: y + noteSize},
			Color:  d.annotationColor(ToolNote),
		}})
	})
}

// placeFreeText asks for the text of a text box and places it over bounds
// of page; a box too small to hold text gets the size of one placed with
// a tap.
func (d *Document) placeFreeText(page int, bounds Rect) {
	if bounds.X1-bounds.X0 < 2*noteSize || bounds.Y1-bounds.Y0 < noteSize {
		bounds.X1 = bounds.X0 + float64(freeTextSize.Width)
		bounds.Y1 = bounds.Y0 + float64(freeTextSize.Height)
	}
	d.askText("Text box", func(text string) {
		d.addAnnotation(page, cera.NewAnnotation{Subtype: "FreeText", Contents: text, Markup: cera.Markup{
			Bounds: ceraRect(bounds),
			Color:  d.annotationColor(ToolFreeText),
		}})
	})
}

// askText asks for a text with a dialog on the widget's window and calls
// done with it, unless the dialog is cancelled or the text is empty.
func (d *Document) askText(title string, done func(text string)) {
	w := d.window()
	if w == nil {
		return
	}
	entry := widget.NewMultiLineEntry()
	entry.SetMinRowsVisible(4)
	entry.Wrapping = fyne.TextWrapWord
	var dlg *dialog.FormDialog
	dlg = dialog.NewForm(title, "OK", "Cancel", []*widget.FormItem{widget.NewFormItem("", entry)}, func(ok bool) {
		if d.textDialog == dlg {
			d.textDialog, d.textEntry = nil, nil
		}
		if ok && entry.Text != "" {
			done(entry.Text)
		}
	}, w)
	dlg.Resize(fyne.NewSize(360, dlg.MinSize().Height))
	d.textDialog, d.textEntry = dlg, entry
	dlg.Show()
	w.Canvas().Focus(entry)
}

// DeleteAnnotation removes a, one of [Document.Annotations], from the
// document, with its popup and its replies; the Delete key removes the
// annotation selected in the comments panel or on the page.
func (d *Document) DeleteAnnotation(a Annotation) bool {
	d.comments.clearSelection()
	return d.editPage(a.Page, func(e *cera.Editor) error {
		return e.RemoveAnnotation(a.Page, a.Index)
	})
}

// deleteSelected removes the annotation selected in the comments panel.
func (d *Document) deleteSelected() {
	p := d.comments
	if !p.hasSelected {
		return
	}
	if a, ok := p.annotation(p.selected); ok {
		d.DeleteAnnotation(a)
	}
}

// areaPoint returns the position in the page area of the point x, y of
// page.
func (d *Document) areaPoint(page int, x, y float64) fyne.Position {
	scale := d.scale * unitsPerPoint
	return d.layout.origin(page, d.areaSize()).AddXY(float32(x*scale), float32(y*scale))
}

func ceraRect(r Rect) cera.Rect {
	return cera.Rect{X0: r.X0, Y0: r.Y0, X1: r.X1, Y1: r.Y1}
}

// strokeColor returns the colour of components, black if there are none.
func strokeColor(c []float64) color.Color {
	if c := componentColor(c); c != nil {
		return c
	}
	return color.Black
}

func abs32(v float32) float32 {
	return float32(math.Abs(float64(v)))
}
