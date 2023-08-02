package pdf

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"strconv"
)

type Thumbnail struct {
	widget.BaseWidget

	page  *canvas.Image
	label *widget.Label

	base *fyne.Container

	PageNumber         int
	previousPageNumber int
	OnTapped           func()

	renderer PageRenderer
}

var _ fyne.Widget = (*Thumbnail)(nil)
var _ fyne.Tappable = (*Thumbnail)(nil)

func (t *Thumbnail) Tapped(_ *fyne.PointEvent) {
	if t.OnTapped != nil {
		t.OnTapped()
	}
}

func (t *Thumbnail) CreateRenderer() fyne.WidgetRenderer {
	t.ExtendBaseWidget(t)
	return widget.NewSimpleRenderer(t.base)
}

func NewThumbnail(renderer PageRenderer) *Thumbnail {
	t := &Thumbnail{
		page: &canvas.Image{
			FillMode:  canvas.ImageFillContain,
			ScaleMode: canvas.ImageScaleFastest,
		},
		label:              widget.NewLabelWithStyle("0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		renderer:           renderer,
		previousPageNumber: -1,
	}
	t.page.SetMinSize(fyne.Size{
		Width:  200,
		Height: 200,
	})
	t.base = container.NewVBox(container.NewMax(t.page), t.label)
	t.ExtendBaseWidget(t)

	return t
}

func (t *Thumbnail) Refresh() {

	if t.previousPageNumber != t.PageNumber || t.page.Image == nil {
		t.label.Text = strconv.Itoa(t.PageNumber + 1)

		if nil == t.page.Image {
			t.page.Image, _ = t.renderer(t.PageNumber, t.Size().Max(t.page.MinSize()))
		}

		t.previousPageNumber = t.PageNumber
	}

	t.base.Refresh()
}
