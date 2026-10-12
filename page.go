package pdf

import (
	"image"
	"image/color"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

type (
	// Page displays a single rendered page image at [Page.PageSize]
	// multiplied by Scale, which is also its minimum size. A larger widget
	// centres the page on BackgroundColor. The image is scaled to the page
	// size, so it can be rendered at any resolution; the aspect ratio is
	// kept. Highlights, see [Page.SetHighlights], are drawn over the image.
	Page struct {
		widget.BaseWidget

		image      *canvas.Image
		pageSize   fyne.Size
		highlights []Highlight

		Scale           float64
		BackgroundColor color.Color
	}

	// Highlight marks an area of a [Page], such as a search result: a
	// rectangle in the units of [Page.PageSize] (at Scale 1, origin at the
	// top left of the page), filled with Color over the page image. Use a
	// translucent color to keep the text readable.
	Highlight struct {
		Position fyne.Position
		Size     fyne.Size
		Color    color.Color
	}

	pageRenderer struct {
		pageWidget *Page
		background *canvas.Rectangle
		// highlights draw the highlights of the page, in order.
		highlights []*canvas.Rectangle
		objects    []fyne.CanvasObject
	}
)

var _ fyne.Widget = (*Page)(nil)
var _ fyne.WidgetRenderer = (*pageRenderer)(nil)

func (p *pageRenderer) Destroy() {
}

func (p *pageRenderer) Layout(size fyne.Size) {
	p.background.Move(fyne.Position{})
	p.background.Resize(size)

	// The page keeps its zoomed size: stretched to a larger widget (e.g. a
	// scroll container wider than the page), it would be shown larger than
	// the resolution it was rendered for.
	page := p.MinSize()
	p.pageWidget.image.Move(fyne.NewPos(
		max(0, (size.Width-page.Width)/2),
		max(0, (size.Height-page.Height)/2),
	))
	p.pageWidget.image.Resize(page)

	origin := p.pageWidget.image.Position()
	scale := float32(p.pageWidget.Scale)
	// Until Refresh catches up with new highlights, the old rectangles
	// keep their places.
	for i, rect := range p.highlights[:min(len(p.highlights), len(p.pageWidget.highlights))] {
		h := p.pageWidget.highlights[i]
		rect.Move(origin.AddXY(h.Position.X*scale, h.Position.Y*scale))
		rect.Resize(fyne.NewSize(h.Size.Width*scale, h.Size.Height*scale))
	}
}

func (p *pageRenderer) MinSize() fyne.Size {
	size := p.pageWidget.PageSize()
	return fyne.Size{
		Width:  size.Width * float32(p.pageWidget.Scale),
		Height: size.Height * float32(p.pageWidget.Scale),
	}
}

func (p *pageRenderer) Objects() []fyne.CanvasObject {
	return p.objects
}

func (p *pageRenderer) Refresh() {
	p.background.FillColor = p.pageWidget.BackgroundColor
	p.background.Refresh()

	highlights := p.pageWidget.highlights
	for len(p.highlights) < len(highlights) {
		p.highlights = append(p.highlights, canvas.NewRectangle(color.Transparent))
	}
	p.highlights = p.highlights[:len(highlights)]
	p.objects = append(p.objects[:0], p.background, p.pageWidget.image)
	for i, h := range highlights {
		p.highlights[i].FillColor = h.Color
		p.highlights[i].Refresh()
		p.objects = append(p.objects, p.highlights[i])
	}

	p.Layout(p.pageWidget.Size())
	p.pageWidget.image.Refresh()
}

func (p *Page) CreateRenderer() fyne.WidgetRenderer {
	p.ExtendBaseWidget(p)
	r := &pageRenderer{
		background: canvas.NewRectangle(p.BackgroundColor),
		pageWidget: p,
	}
	r.Refresh()
	return r
}

// PageSize returns the size of the page at Scale 1. Unless set with
// [Page.SetPageSize], this is the size of the page image in pixels.
func (p *Page) PageSize() fyne.Size {
	if !p.pageSize.IsZero() {
		return p.pageSize
	}
	imageSize := p.image.Image.Bounds().Size()
	return fyne.Size{
		Width:  float32(imageSize.X),
		Height: float32(imageSize.Y),
	}
}

// SetPageSize sets the size of the page at Scale 1, independent of the
// resolution of the image.
func (p *Page) SetPageSize(size fyne.Size) {
	p.pageSize = size
	p.Refresh()
}

// SetImage replaces the displayed page image.
func (p *Page) SetImage(pageImage image.Image) {
	p.image.Image = pageImage
	p.Refresh()
}

// Highlights returns the highlights drawn over the page.
func (p *Page) Highlights() []Highlight {
	return slices.Clone(p.highlights)
}

// SetHighlights replaces the highlights drawn over the page; nil removes
// them.
func (p *Page) SetHighlights(highlights []Highlight) {
	if slices.Equal(p.highlights, highlights) {
		return
	}
	p.highlights = slices.Clone(highlights)
	p.Refresh()
}

// NewPageWithImage creates a Page showing img.
func NewPageWithImage(img image.Image) *Page {
	p := NewPage()
	p.SetImage(img)
	return p
}

// NewPage creates an empty Page.
func NewPage() *Page {
	p := &Page{
		image: &canvas.Image{
			FillMode:  canvas.ImageFillContain,
			ScaleMode: canvas.ImageScaleSmooth,
			Image:     image.NewGray(image.Rectangle{}),
		},
		Scale: 1,
	}
	p.ExtendBaseWidget(p)
	return p
}
