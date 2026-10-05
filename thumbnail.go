package pdf

import (
	"image"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type (
	// Thumbnail is a tappable page preview with a title below it.
	Thumbnail struct {
		widget.BaseWidget

		page     *Page
		label    *widget.Label
		selected bool

		OnTapped    func()
		minimumSize fyne.Size
	}

	thumbnailRenderer struct {
		thumbnail  *Thumbnail
		background *canvas.Rectangle
	}
)

// ThumbnailMinimumDimension is the default width and height of a thumbnail.
const ThumbnailMinimumDimension = float32(100)

func (t *thumbnailRenderer) Destroy() {
}

func (t *thumbnailRenderer) Layout(size fyne.Size) {
	t.background.Move(fyne.Position{})
	t.background.Resize(size)

	t.thumbnail.page.Move(fyne.Position{
		X: (size.Width - t.thumbnail.minimumSize.Width) / 2,
	})
	t.thumbnail.page.Resize(t.thumbnail.minimumSize)

	labelHeight := t.thumbnail.label.MinSize().Height
	t.thumbnail.label.Move(fyne.Position{
		Y: size.Height - labelHeight,
	})
	t.thumbnail.label.Resize(fyne.Size{
		Width:  size.Width,
		Height: labelHeight,
	})
}

func (t *thumbnailRenderer) MinSize() fyne.Size {
	labelSize := t.thumbnail.label.MinSize()
	return fyne.Size{
		Width:  max(t.thumbnail.minimumSize.Width, labelSize.Width),
		Height: t.thumbnail.minimumSize.Height + labelSize.Height,
	}
}

func (t *thumbnailRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{
		t.background,
		t.thumbnail.page,
		t.thumbnail.label,
	}
}

func (t *thumbnailRenderer) Refresh() {
	if t.thumbnail.selected {
		t.background.FillColor = theme.Color(theme.ColorNameSelection)
	} else {
		t.background.FillColor = nil
	}
	t.background.Refresh()
	t.Layout(t.thumbnail.Size())
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
	t.Refresh()
}

// SetSelected highlights the thumbnail, e.g. for the page currently shown.
func (t *Thumbnail) SetSelected(selected bool) {
	if t.selected == selected {
		return
	}
	t.selected = selected
	t.Refresh()
}

// Selected reports whether the thumbnail is highlighted.
func (t *Thumbnail) Selected() bool {
	return t.selected
}

func (t *Thumbnail) CreateRenderer() fyne.WidgetRenderer {
	t.ExtendBaseWidget(t)
	return &thumbnailRenderer{
		thumbnail:  t,
		background: canvas.NewRectangle(nil),
	}
}

// SetMinimumSize sets the size of the preview image area.
func (t *Thumbnail) SetMinimumSize(size fyne.Size) {
	t.minimumSize = size
	t.Refresh()
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
	t.ExtendBaseWidget(t)
	return t
}
