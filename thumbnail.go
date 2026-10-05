package pdf

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"image"
)

type (
	// Thumbnail is a tappable page preview with a title below it.
	Thumbnail struct {
		widget.BaseWidget

		base *fyne.Container

		page  *Page
		label *widget.Label

		OnTapped    func()
		minimumSize fyne.Size
	}

	thumbnailRenderer struct {
		thumbnail *Thumbnail
	}
)

// ThumbnailMinimumDimension is the default width and height of a thumbnail.
const ThumbnailMinimumDimension = float32(100)

func (t *thumbnailRenderer) Destroy() {
}

func (t *thumbnailRenderer) Layout(size fyne.Size) {

	t.thumbnail.page.Move(fyne.Position{
		X: 0,
		Y: 0,
	})
	t.thumbnail.page.Resize(t.thumbnail.minimumSize)

	t.thumbnail.label.Move(fyne.Position{
		X: 0,
		Y: size.Height - t.thumbnail.label.MinSize().Height,
	})
	t.thumbnail.label.Resize(fyne.Size{
		Width:  size.Width,
		Height: t.thumbnail.label.MinSize().Height,
	})
}

func (t *thumbnailRenderer) MinSize() fyne.Size {
	width := t.thumbnail.minimumSize.Width
	if t.thumbnail.label.MinSize().Width > width {
		width = t.thumbnail.label.MinSize().Width
	}
	return fyne.Size{
		Width:  width,
		Height: t.thumbnail.minimumSize.Height + t.thumbnail.label.MinSize().Height,
	}
}

func (t *thumbnailRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{
		t.thumbnail.page,
		t.thumbnail.label,
	}
}

func (t *thumbnailRenderer) Refresh() {

	t.Layout(t.thumbnail.Size())
	canvas.Refresh(t.thumbnail)
}

var _ fyne.WidgetRenderer = (*thumbnailRenderer)(nil)

func (t *Thumbnail) Tapped(_ *fyne.PointEvent) {
	if t.OnTapped != nil {
		t.OnTapped()
	}
}

// SetTitle sets the label shown below the preview.
func (t *Thumbnail) SetTitle(title string) {
	t.label.SetText(title)
	t.Refresh()
}

// SetImage sets the preview image.
func (t *Thumbnail) SetImage(theImage image.Image) {
	t.page.SetImage(theImage)
	t.page.Refresh()
	t.Refresh()
}

func (t *Thumbnail) CreateRenderer() fyne.WidgetRenderer {
	t.ExtendBaseWidget(t)
	return &thumbnailRenderer{thumbnail: t}
}

// SetMinimumSize sets the size of the preview image area.
func (t *Thumbnail) SetMinimumSize(size fyne.Size) {
	t.minimumSize = size
}

var _ fyne.Widget = (*Thumbnail)(nil)
var _ fyne.Tappable = (*Thumbnail)(nil)

// NewThumbnail creates an empty Thumbnail.
func NewThumbnail() *Thumbnail {
	t := &Thumbnail{
		label: widget.NewLabelWithStyle("", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		page:  NewPage(),
		minimumSize: fyne.Size{
			Width:  ThumbnailMinimumDimension,
			Height: ThumbnailMinimumDimension,
		},
	}
	t.base = container.NewBorder(nil, t.label, nil, nil, t.page)
	t.page.image.ScaleMode = canvas.ImageScaleSmooth
	t.ExtendBaseWidget(t)
	return t
}
