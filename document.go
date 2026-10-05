package pdf

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/cockroachdb/errors"
	"github.com/davecgh/go-spew/spew"
	"github.com/timzifer/fyne_lucide"
	"github.com/timzifer/fyne_tabler"
	"golang.org/x/sync/errgroup"
	"image"
	"image/color"
	"io"
	"math"
	"strconv"
	"sync"
)

type (
	// Document is a PDF viewer widget: a page view with a toolbar for zooming
	// and a thumbnail strip for navigation. Create it with [NewDocument].
	Document struct {
		widget.BaseWidget

		source *Source

		thumbnails []*Thumbnail

		bigPage *Page

		base               *fyne.Container
		thumbnailContainer *fyne.Container
		thumbnailScroller  *container.Scroll
		toolbar            *fyne.Container
		zoom               binding.Float
		scrollContainer    *container.Scroll

		// BackgroundColor fills the area behind the page.
		BackgroundColor color.Color
		// SaveCallback, if set, shows a save button in the toolbar that calls
		// it. Call Refresh after changing it.
		SaveCallback func()

		mutex                  sync.Mutex
		toggleThumbnailsButton *widget.Button
		zoomInButton           *widget.Button
		zoomOutButton          *widget.Button
		saveButton             *widget.Button
	}
)

func (d *Document) CreateRenderer() fyne.WidgetRenderer {
	d.ExtendBaseWidget(d)
	return widget.NewSimpleRenderer(d.base)
}

var (
	_ fyne.Widget = (*Document)(nil)
	_ io.Closer   = (*Document)(nil)
)

// NewDocument creates an empty viewer. Load a PDF with [Document.LoadFromMemory].
func NewDocument() *Document {
	d := &Document{
		thumbnailContainer: container.NewHBox(),
		toolbar:            container.NewHBox(),
		zoom:               binding.NewFloat(),
	}

	d.thumbnailScroller = container.NewHScroll(d.thumbnailContainer)

	d.bigPage = NewPage()
	d.scrollContainer = container.NewScroll(d.bigPage)

	zoomSlider := widget.NewSliderWithData(0.2, 4, d.zoom)
	zoomSlider.Step = 0.

	resetZoomButton := widget.NewButton("100%", func() {
		d.Zoom(1)
	})
	resetZoomButton.Importance = widget.LowImportance

	fitButton := widget.NewButton("", func() {
		d.ZoomToFit()
	})
	fitButton.Icon = fyne_lucide.Icon(fyne_lucide.IconMaximize2)
	fitButton.Importance = widget.LowImportance

	fitVerticalButton := widget.NewButton("", func() {
		d.ZoomToFitVertical()
	})
	fitVerticalButton.Icon = fyne_tabler.Icon(fyne_tabler.IconArrowAutofitHeight)
	fitVerticalButton.Importance = widget.LowImportance

	fitHorizontalButton := widget.NewButton("", func() {
		d.ZoomToFitHorizontal()
	})
	fitHorizontalButton.Icon = fyne_tabler.Icon(fyne_tabler.IconArrowAutofitWidth)
	fitHorizontalButton.Importance = widget.LowImportance

	d.toggleThumbnailsButton = widget.NewButton("", func() {
		if d.thumbnailScroller.Visible() {
			d.toggleThumbnailsButton.Icon = fyne_lucide.Icon(fyne_lucide.IconToggleLeft)
			d.toggleThumbnailsButton.Refresh()
			d.thumbnailScroller.Hide()
		} else {
			d.toggleThumbnailsButton.Icon = fyne_lucide.Icon(fyne_lucide.IconToggleRight)
			d.toggleThumbnailsButton.Refresh()
			d.thumbnailScroller.Show()
		}
	})
	d.toggleThumbnailsButton.Icon = fyne_lucide.Icon(fyne_lucide.IconToggleRight)
	d.toggleThumbnailsButton.Importance = widget.LowImportance

	d.zoomInButton = widget.NewButton("", func() {
		if current, err := d.zoom.Get(); err == nil {
			_ = d.zoom.Set(math.Min(zoomSlider.Max, current+0.25))
		}
	})
	d.zoomInButton.Icon = fyne_lucide.Icon(fyne_lucide.IconZoomIn)
	d.zoomInButton.Importance = widget.LowImportance

	d.zoomOutButton = widget.NewButton("", func() {
		if current, err := d.zoom.Get(); err == nil {
			_ = d.zoom.Set(math.Max(zoomSlider.Min, current-0.25))
		}
	})
	d.zoomOutButton.Icon = fyne_lucide.Icon(fyne_lucide.IconZoomOut)
	d.zoomOutButton.Importance = widget.LowImportance

	d.saveButton = widget.NewButton("", func() {
	})
	d.saveButton.Icon = fyne_lucide.Icon(fyne_lucide.IconSave)
	d.saveButton.Importance = widget.LowImportance
	d.saveButton.Hidden = true

	d.toolbar.Add(d.toggleThumbnailsButton)
	d.toolbar.Add(layout.NewSpacer())
	d.toolbar.Add(d.zoomOutButton)
	d.toolbar.Add(zoomSlider)
	d.toolbar.Add(d.zoomInButton)
	d.toolbar.Add(resetZoomButton)
	d.toolbar.Add(fitButton)
	d.toolbar.Add(fitVerticalButton)
	d.toolbar.Add(fitHorizontalButton)
	d.toolbar.Add(d.saveButton)

	d.base = container.NewBorder(
		nil,
		container.NewVBox(
			d.toolbar,
			d.thumbnailScroller,
		),
		nil, nil,
		d.scrollContainer,
	)

	d.ExtendBaseWidget(d)

	d.zoom.AddListener(binding.NewDataListener(func() {
		factor, _ := d.zoom.Get()
		//d.bigPage.SetScaleFactor(factor)
		if factor != d.bigPage.Scale {
			d.bigPage.Scale = factor
			d.scrollContainer.Refresh()
			d.Refresh()
		}

		if factor <= zoomSlider.Min {
			d.zoomOutButton.Disable()
		} else {
			d.zoomOutButton.Enable()
		}

		if factor >= zoomSlider.Max {
			d.zoomInButton.Disable()
		} else {
			d.zoomInButton.Enable()
		}

	}))

	return d
}

// Close releases the loaded PDF. Pages already shown stay visible.
func (d *Document) Close() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	d.source = nil
	return nil
}

// LoadFromMemory opens the PDF in contents, renders the thumbnails and shows
// the first page.
func (d *Document) LoadFromMemory(contents []byte) error {
	src, err := OpenSource(contents)
	if err != nil {
		return errors.Wrap(err, "could not open pdf from memory")
	}
	return d.load(src)
}

// renderer rendert außerhalb von d.mutex - Source serialisiert selbst.
func (d *Document) renderer(pageNum int, size fyne.Size) (image.Image, error) {
	d.mutex.Lock()
	src := d.source
	d.mutex.Unlock()

	if src == nil {
		return nil, nil
	}

	// TODO choose dpi for size
	return src.RenderPage(pageNum, 144)
}

func (d *Document) load(src *Source) error {

	defer func() {
		if len(d.thumbnails) > 0 {
			d.thumbnails[0].OnTapped()
		}
	}()

	d.mutex.Lock()
	d.source = src
	d.mutex.Unlock()

	numPage := src.PageCount()

	eg := errgroup.Group{}
	for pageNumber := 0; pageNumber < numPage; pageNumber++ {
		localPageNumber := pageNumber
		newPage := NewThumbnail()
		eg.Go(func() error {
			if img, err := d.renderer(localPageNumber, fyne.Size{
				Width:  200,
				Height: 200,
			}); err != nil {
				return err
			} else if img != nil {
				newPage.SetImage(img)
				newPage.SetTitle(strconv.Itoa(localPageNumber))
			}
			return nil
		})
		newPage.OnTapped = func() {
			if img, err := d.renderer(localPageNumber, d.bigPage.Size()); err != nil {
				spew.Dump(err)
			} else if img != nil {
				d.bigPage.SetImage(img)
				d.ZoomToFit()
			}
		}

		d.thumbnailContainer.Objects = append(d.thumbnailContainer.Objects, newPage)
		d.thumbnails = append(d.thumbnails, newPage)
	}
	if numPage <= 1 {
		d.toggleThumbnailsButton.Hide()
		d.thumbnailScroller.Hide()
	} else {
		d.toggleThumbnailsButton.Show()
		d.thumbnailScroller.Show()
	}

	// finally load first page to big page
	return eg.Wait()
}

func (d *Document) Refresh() {
	d.bigPage.BackgroundColor = d.BackgroundColor

	if d.SaveCallback != nil {
		d.saveButton.OnTapped = d.SaveCallback
		d.saveButton.Show()
	} else {
		d.saveButton.Hide()
	}

	d.bigPage.Refresh()
	d.scrollContainer.Refresh()
	d.thumbnailContainer.Refresh()
	d.base.Refresh()
}

// Zoom sets the zoom factor; 1 is 100 %. The slider allows 0.2 to 4.
func (d *Document) Zoom(scale float64) {
	_ = d.zoom.Set(scale)
}

// ZoomToFit scales the page so it fits completely into the view.
func (d *Document) ZoomToFit() {

	imageSize := d.bigPage.PageSize()

	scaleX := d.scrollContainer.Size().Width / imageSize.Width
	scaleY := d.scrollContainer.Size().Height / imageSize.Height

	if scaleX < scaleY {
		_ = d.zoom.Set(float64(scaleX))
	} else {
		_ = d.zoom.Set(float64(scaleY))
	}
}

// ZoomToFitVertical scales the page to the height of the view.
func (d *Document) ZoomToFitVertical() {
	imageSize := d.bigPage.PageSize()
	_ = d.zoom.Set(float64(d.scrollContainer.Size().Height / imageSize.Height))
}

// ZoomToFitHorizontal scales the page to the width of the view.
func (d *Document) ZoomToFitHorizontal() {
	imageSize := d.bigPage.PageSize()
	_ = d.zoom.Set(float64(d.scrollContainer.Size().Width / imageSize.Width))
}
