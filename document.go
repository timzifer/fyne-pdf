package pdf

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/cockroachdb/errors"
	"github.com/davecgh/go-spew/spew"
	"github.com/gen2brain/go-fitz"
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
	Document struct {
		widget.BaseWidget

		document *fitz.Document

		thumbnails []*Thumbnail

		bigPage *Page

		base               *fyne.Container
		thumbnailContainer *fyne.Container
		thumbnailScroller  *container.Scroll
		toolbar            *fyne.Container
		zoom               binding.Float
		scrollContainer    *container.Scroll

		BackgroundColor color.Color
		SaveCallback    func()

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
			d.zoom.Set(math.Min(zoomSlider.Max, current+0.25))
		}
	})
	d.zoomInButton.Icon = fyne_lucide.Icon(fyne_lucide.IconZoomIn)
	d.zoomInButton.Importance = widget.LowImportance

	d.zoomOutButton = widget.NewButton("", func() {
		if current, err := d.zoom.Get(); err == nil {
			d.zoom.Set(math.Max(zoomSlider.Min, current-0.25))
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

// Close gibt das MuPDF-Dokument frei und nilt den Pointer unter d.mutex.
// Ohne das Nilen rendern noch laufende load()-Goroutinen (oder ein späterer
// Thumbnail-Tap) auf dem freigegebenen fz_context weiter - Use-after-free,
// der im CEF-Prozess als MuPDF-Abort ("aborting process from uncaught
// error!") endet.
func (d *Document) Close() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.document == nil {
		return nil
	}

	MuPDFMutex.Lock()
	defer MuPDFMutex.Unlock()
	err := d.document.Close()
	d.document = nil
	return err
}

func (d *Document) LoadFromMemory(contents []byte) error {

	MuPDFMutex.Lock()
	doc, err := fitz.NewFromMemory(contents)
	MuPDFMutex.Unlock()
	if err != nil {
		return errors.Wrap(err, "could not open pdf from memory")
	}
	return d.load(doc)
}

func (d *Document) renderer(pageNum int, size fyne.Size) (image.Image, error) {

	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.document == nil {
		return nil, nil
	}

	MuPDFMutex.Lock()
	defer MuPDFMutex.Unlock()
	// TODO choose dpi for size
	return d.document.ImageDPI(pageNum, 144)
}

func (d *Document) load(document *fitz.Document) error {

	defer func() {
		if len(d.thumbnails) > 0 {
			d.thumbnails[0].OnTapped()
		}
	}()

	MuPDFMutex.Lock()
	// close previous document
	if d.document != nil && d.document != document {
		// TODO Log
		_ = d.document.Close()
	}
	d.document = document

	numPage := document.NumPage() // cache
	MuPDFMutex.Unlock()

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
			} else {
				newPage.SetImage(img)
				newPage.SetTitle(strconv.Itoa(localPageNumber))
				return nil
			}
		})
		newPage.OnTapped = func() {
			if img, err := d.renderer(localPageNumber, d.bigPage.Size()); err != nil {
				spew.Dump(err)
			} else {
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

func (d *Document) Zoom(scale float64) {
	_ = d.zoom.Set(scale)
}

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

func (d *Document) ZoomToFitVertical() {
	imageSize := d.bigPage.PageSize()
	_ = d.zoom.Set(float64(d.scrollContainer.Size().Height / imageSize.Height))
}

func (d *Document) ZoomToFitHorizontal() {
	imageSize := d.bigPage.PageSize()
	_ = d.zoom.Set(float64(d.scrollContainer.Size().Width / imageSize.Width))
}
