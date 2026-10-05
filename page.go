package pdf

import (
	"image"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
)

type (
	// Page displays a single rendered page image. Its minimum size is
	// [Page.PageSize] multiplied by Scale; the image is scaled to fit, so
	// it can be rendered at any resolution. The aspect ratio is kept.
	Page struct {
		widget.BaseWidget

		image    *canvas.Image
		pageSize fyne.Size

		Scale           float64
		BackgroundColor color.Color
	}

	pageRenderer struct {
		pageWidget *Page
		background *canvas.Rectangle
	}
)

var _ fyne.Widget = (*Page)(nil)
var _ fyne.WidgetRenderer = (*pageRenderer)(nil)

func (p *pageRenderer) Destroy() {
}

func (p *pageRenderer) Layout(size fyne.Size) {
	p.background.Move(fyne.Position{})
	p.background.Resize(size)

	p.pageWidget.image.Move(fyne.Position{})
	p.pageWidget.image.Resize(size)
}

func (p *pageRenderer) MinSize() fyne.Size {
	size := p.pageWidget.PageSize()
	return fyne.Size{
		Width:  size.Width * float32(p.pageWidget.Scale),
		Height: size.Height * float32(p.pageWidget.Scale),
	}
}

func (p *pageRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{
		p.background,
		p.pageWidget.image,
	}
}

func (p *pageRenderer) Refresh() {
	p.background.FillColor = p.pageWidget.BackgroundColor
	p.background.Refresh()

	p.Layout(p.pageWidget.Size())
	p.pageWidget.image.Refresh()
}

func (p *Page) CreateRenderer() fyne.WidgetRenderer {
	p.ExtendBaseWidget(p)
	return &pageRenderer{
		background: canvas.NewRectangle(p.BackgroundColor),
		pageWidget: p,
	}
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
