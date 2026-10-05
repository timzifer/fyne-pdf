package pdf

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"image"
	"image/color"
)

type (
	// Page displays a single rendered page image, scaled by Scale.
	Page struct {
		widget.BaseWidget

		image *canvas.Image

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
	p.background.Move(fyne.Position{
		X: 0,
		Y: 0,
	})
	p.background.Resize(size)
	p.background.Refresh()

	p.pageWidget.image.Move(fyne.Position{
		X: 0,
		Y: 0,
	})
	p.pageWidget.image.Resize(size)
	p.pageWidget.image.Refresh()

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

	p.Layout(p.pageWidget.Size())
	canvas.Refresh(p.pageWidget)
}

func (p *Page) CreateRenderer() fyne.WidgetRenderer {
	p.ExtendBaseWidget(p)
	return &pageRenderer{
		background: canvas.NewRectangle(p.BackgroundColor),
		pageWidget: p,
	}
}

// PageSize returns the size of the page image in pixels.
func (p *Page) PageSize() fyne.Size {
	imageSize := p.image.Image.Bounds().Max
	return fyne.Size{
		Width:  float32(imageSize.X),
		Height: float32(imageSize.Y),
	}
}

// SetImage replaces the displayed page image.
func (p *Page) SetImage(pageImage image.Image) {
	p.image.Image = pageImage
	p.image.Refresh()
	p.Refresh()
}

/*
	func (p *Page) ReplaceWithPageNumber(pageNumber int) error {
		if img, err := p.renderer(pageNumber, p.Size()); err != nil {
			return err
		} else {
			p.image.Image = img
			p.image.Refresh()
			p.Refresh()
			return nil
		}
	}
*/

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
			ScaleMode: canvas.ImageScaleFastest,
			Image: image.NewGray(image.Rectangle{
				Min: image.Point{
					X: 0,
					Y: 0,
				},
				Max: image.Point{
					X: 0,
					Y: 0,
				},
			}),
		},
	}
	p.ExtendBaseWidget(p)
	return p
}
