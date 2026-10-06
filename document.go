package pdf

import (
	"context"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/cockroachdb/errors"
	"github.com/timzifer/fyne_lucide"
	"github.com/timzifer/fyne_tabler"
)

const (
	// unitsPerPoint maps PDF points (1/72 inch) to Fyne units, so that zoom 1
	// shows the page at roughly its physical size.
	unitsPerPoint = 96.0 / 72

	minZoom = 0.2
	maxZoom = 4

	// maxRenderPixels caps the size of the page image rendered for the main
	// view (~100 MB RGBA). Beyond that the image is scaled up for display.
	maxRenderPixels = 25_000_000

	// zoomRenderDelay debounces re-rendering while the zoom changes.
	zoomRenderDelay = 150 * time.Millisecond

	// zoomSliderWidth is the minimum width of the zoom slider; in the
	// toolbar's HBox it would otherwise shrink to the size of its thumb.
	zoomSliderWidth = 150
)

type (
	// Document is a PDF viewer widget: a page view with a toolbar for zooming
	// and a thumbnail strip for navigation. Create it with [NewDocument].
	//
	// The page view supports the usual viewer gestures: Ctrl+wheel (Cmd on
	// macOS) zooms around the pointer, scrolling on past the top or bottom
	// of a page turns it, dragging pans, a double tap switches between the
	// fitted page and 100 %. Once clicked, it takes the keyboard focus:
	// Page Up/Down and Space scroll by a screen, Home/End go to the first
	// and last page, the arrow keys scroll (Left/Right turn pages while the
	// page fits horizontally), Ctrl +/-/0 zoom.
	//
	// Pages are rendered in the background; the resolution of the main view
	// follows the zoom level. Methods must be called from the Fyne main
	// goroutine (or before the app runs).
	Document struct {
		widget.BaseWidget

		// BackgroundColor fills the area behind the page.
		BackgroundColor color.Color
		// SaveCallback, if set, shows a save button in the toolbar that calls
		// it. Call Refresh after changing it.
		SaveCallback func()
		// OnError is called on the main goroutine when a page cannot be
		// rendered. If nil, the error is logged.
		OnError func(page int, err error)

		thumbnails []*Thumbnail
		bigPage    *Page
		area       *pageArea

		base               *fyne.Container
		thumbnailContainer *fyne.Container
		thumbnailScroller  *container.Scroll
		toolbar            *fyne.Container
		zoom               binding.Float
		zoomSlider         *widget.Slider
		scrollContainer    *container.Scroll

		toggleThumbnailsButton *widget.Button
		zoomInButton           *widget.Button
		zoomOutButton          *widget.Button
		saveButton             *widget.Button

		// fitHorizontal and fitVertical keep the page fitted to the view
		// while the view is resized, until the zoom is changed otherwise.
		// fitScale is the zoom the last fit set.
		fitHorizontal, fitVertical bool
		fitScale                   float64
		// modifiers returns the key modifiers held; replaced in tests.
		modifiers func() fyne.KeyModifier
		// runOnMain applies results of background renders; fyne.Do, replaced
		// in tests.
		runOnMain func(func())

		// mutex guards the fields below; they are read by render goroutines.
		mutex       sync.Mutex
		source      *Source
		bounds      []image.Rectangle
		ctx         context.Context
		cancel      context.CancelFunc
		current     int
		renderedDPI float64
		renderTimer *time.Timer
		// pending counts scheduled renders that have not been applied yet.
		pending sync.WaitGroup
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
		runOnMain:          fyne.Do,
		modifiers:          currentKeyModifiers,
	}
	_ = d.zoom.Set(1)

	d.thumbnailScroller = container.NewHScroll(d.thumbnailContainer)

	d.bigPage = NewPage()
	d.area = newPageArea(d)
	d.scrollContainer = container.NewScroll(d.area)

	d.zoomSlider = widget.NewSliderWithData(minZoom, maxZoom, d.zoom)
	d.zoomSlider.Step = 0

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
	d.toggleThumbnailsButton.Hidden = true
	d.thumbnailScroller.Hide()

	d.zoomInButton = widget.NewButton("", func() {
		if current, err := d.zoom.Get(); err == nil {
			_ = d.zoom.Set(math.Min(maxZoom, current+0.25))
		}
	})
	d.zoomInButton.Icon = fyne_lucide.Icon(fyne_lucide.IconZoomIn)
	d.zoomInButton.Importance = widget.LowImportance

	d.zoomOutButton = widget.NewButton("", func() {
		if current, err := d.zoom.Get(); err == nil {
			_ = d.zoom.Set(math.Max(minZoom, current-0.25))
		}
	})
	d.zoomOutButton.Icon = fyne_lucide.Icon(fyne_lucide.IconZoomOut)
	d.zoomOutButton.Importance = widget.LowImportance

	d.saveButton = widget.NewButton("", nil)
	d.saveButton.Icon = fyne_lucide.Icon(fyne_lucide.IconSave)
	d.saveButton.Importance = widget.LowImportance
	d.saveButton.Hidden = true

	d.toolbar.Add(d.toggleThumbnailsButton)
	d.toolbar.Add(layout.NewSpacer())
	d.toolbar.Add(d.zoomOutButton)
	sliderWidth := canvas.NewRectangle(color.Transparent)
	sliderWidth.SetMinSize(fyne.NewSize(zoomSliderWidth, 0))
	d.toolbar.Add(container.NewStack(sliderWidth, d.zoomSlider))
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

	d.zoom.AddListener(binding.NewDataListener(d.zoomChanged))

	return d
}

func (d *Document) zoomChanged() {
	factor, _ := d.zoom.Get()
	// Listeners run asynchronously; Get returns the latest zoom, so a fit
	// followed by another fit does not look like a change by the user.
	if factor != d.fitScale {
		d.fitHorizontal, d.fitVertical = false, false
	}
	d.applyScale(factor)

	if factor <= minZoom {
		d.zoomOutButton.Disable()
	} else {
		d.zoomOutButton.Enable()
	}
	if factor >= maxZoom {
		d.zoomInButton.Disable()
	} else {
		d.zoomInButton.Enable()
	}
}

// Close releases the loaded PDF and stops pending renders. Pages already
// shown stay visible.
func (d *Document) Close() error {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.closeLocked()
}

func (d *Document) closeLocked() error {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	if d.renderTimer != nil && d.renderTimer.Stop() {
		d.pending.Done()
	}
	d.renderTimer = nil
	if d.source == nil {
		return nil
	}
	err := d.source.Close()
	d.source = nil
	d.bounds = nil
	return err
}

// LoadFromMemory opens the PDF in contents and shows its first page. A
// previously loaded PDF is closed. Pages are rendered in the background.
func (d *Document) LoadFromMemory(contents []byte) error {
	src, err := OpenSource(contents)
	if err != nil {
		return errors.Wrap(err, "could not open pdf from memory")
	}
	d.load(src)
	return nil
}

// PageCount returns the number of pages of the loaded PDF.
func (d *Document) PageCount() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return len(d.bounds)
}

// CurrentPage returns the index (0-based) of the page shown in the main view.
func (d *Document) CurrentPage() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.current
}

func (d *Document) load(src *Source) {
	ctx, cancel := context.WithCancel(context.Background())

	// The bounds are read once here: the main goroutine must not wait for
	// the source while it renders in the background.
	numPages := src.PageCount()
	bounds := make([]image.Rectangle, numPages)
	for i := range bounds {
		if b, err := src.Bound(i); err == nil {
			bounds[i] = b
		} else {
			bounds[i] = image.Rect(0, 0, 612, 792)
		}
	}

	d.mutex.Lock()
	_ = d.closeLocked()
	d.source = src
	d.bounds = bounds
	d.ctx, d.cancel = ctx, cancel
	d.mutex.Unlock()

	scale := d.canvasScale()

	d.thumbnails = make([]*Thumbnail, numPages)
	objects := make([]fyne.CanvasObject, numPages)
	dpis := make([]float64, numPages)
	for i := range numPages {
		pageNumber := i
		thumbnail := NewThumbnail()
		thumbnail.SetTitle(strconv.Itoa(pageNumber + 1))
		thumbnail.OnTapped = func() {
			d.ShowPage(pageNumber)
		}
		d.thumbnails[i] = thumbnail
		objects[i] = thumbnail

		longest := float64(max(bounds[i].Dx(), bounds[i].Dy(), 1))
		dpis[i] = 72 * float64(ThumbnailMinimumDimension) * float64(scale) / longest
	}
	d.thumbnailContainer.Objects = objects
	d.thumbnailContainer.Refresh()

	if numPages <= 1 {
		d.toggleThumbnailsButton.Hide()
		d.thumbnailScroller.Hide()
	} else {
		d.toggleThumbnailsButton.Show()
		d.thumbnailScroller.Show()
	}

	thumbnails := d.thumbnails
	d.pending.Add(1)
	go func() {
		defer d.pending.Done()
		for i, thumbnail := range thumbnails {
			img, err := src.RenderPageContext(ctx, i, dpis[i])
			if ctx.Err() != nil {
				return
			}
			d.runOnMain(func() {
				if err != nil {
					d.reportError(i, err)
					return
				}
				thumbnail.SetImage(img)
			})
		}
	}()

	d.mutex.Lock()
	d.current = -1
	d.mutex.Unlock()
	if numPages > 0 {
		d.ShowPage(0)
	}
}

// applyScale shows the page at zoom. The zoom binding's listener calls it,
// but only asynchronously; code that needs the new layout right away (a
// fit, a page turn, zooming around a point) calls it directly.
func (d *Document) applyScale(zoom float64) {
	if zoom == d.bigPage.Scale {
		return
	}
	d.bigPage.Scale = zoom
	d.bigPage.Refresh()
	d.layoutArea()
	d.scrollContainer.Refresh()
	d.scheduleRender(zoomRenderDelay)
}

// layoutArea sizes the scroll content for the current zoom without waiting
// for the next layout pass, so scroll offsets can be set right away.
func (d *Document) layoutArea() {
	d.area.Resize(d.area.MinSize().Max(d.scrollContainer.Size()))
}

// ShowPage shows page (0-based) in the main view and zooms it to fit.
func (d *Document) ShowPage(page int) {
	if d.setPage(page) {
		d.ZoomToFit()
		d.scheduleRender(0)
	}
}

// setPage makes page current and shows its thumbnail as a placeholder until
// it is rendered. It reports false if page is out of range or already
// current.
func (d *Document) setPage(page int) bool {
	d.mutex.Lock()
	if page < 0 || page >= len(d.bounds) || page == d.current {
		d.mutex.Unlock()
		return false
	}
	d.current = page
	d.renderedDPI = 0
	bound := d.bounds[page]
	d.mutex.Unlock()

	for i, thumbnail := range d.thumbnails {
		thumbnail.SetSelected(i == page)
	}

	var placeholder image.Image = image.NewGray(image.Rectangle{})
	if page < len(d.thumbnails) {
		placeholder = d.thumbnails[page].page.image.Image
	}
	d.bigPage.SetImage(placeholder)
	d.bigPage.SetPageSize(fyne.Size{
		Width:  float32(float64(bound.Dx()) * unitsPerPoint),
		Height: float32(float64(bound.Dy()) * unitsPerPoint),
	})
	return true
}

// scheduleRender renders the current page for the main view after delay,
// unless it is already rendered at the needed resolution. A later call
// replaces an earlier one that has not started yet.
func (d *Document) scheduleRender(delay time.Duration) {
	scale := d.canvasScale()
	zoom := d.bigPage.Scale

	d.mutex.Lock()
	defer d.mutex.Unlock()

	if d.source == nil || d.current < 0 {
		return
	}
	if d.renderTimer != nil && d.renderTimer.Stop() {
		d.pending.Done()
	}
	d.renderTimer = nil

	src, ctx, page := d.source, d.ctx, d.current
	bound := d.bounds[page]
	points := float64(max(bound.Dx(), 1) * max(bound.Dy(), 1))
	dpi := math.Min(96*zoom*float64(scale), 72*math.Sqrt(maxRenderPixels/points))
	// Rendering at a lower resolution than shown is visible, a slightly
	// higher one is not worth the time.
	if d.renderedDPI >= dpi && d.renderedDPI <= dpi*1.5 {
		return
	}

	d.pending.Add(1)
	d.renderTimer = time.AfterFunc(delay, func() {
		defer d.pending.Done()
		img, err := src.RenderPageContext(ctx, page, dpi)
		if ctx.Err() != nil {
			return
		}
		d.runOnMain(func() {
			d.mutex.Lock()
			stale := ctx.Err() != nil || d.current != page
			if !stale && err == nil {
				d.renderedDPI = dpi
			}
			d.mutex.Unlock()
			switch {
			case stale:
			case err != nil:
				d.reportError(page, err)
			default:
				d.bigPage.SetImage(img)
			}
		})
	})
}

// waitRendered blocks until all scheduled renders are applied.
func (d *Document) waitRendered() {
	d.pending.Wait()
}

func (d *Document) reportError(page int, err error) {
	if d.OnError != nil {
		d.OnError(page, err)
		return
	}
	log.Printf("pdf: page %d: %v", page, err)
}

func (d *Document) canvasScale() float32 {
	if app := fyne.CurrentApp(); app != nil {
		if c := app.Driver().CanvasForObject(d); c != nil {
			return c.Scale()
		}
	}
	return 1
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

// Resize lays out the widget. While the page is fitted to the view (after
// ShowPage or one of the ZoomToFit methods), the fit follows the new size.
func (d *Document) Resize(size fyne.Size) {
	d.BaseWidget.Resize(size)
	if d.fitHorizontal || d.fitVertical {
		d.fitZoom(d.fitHorizontal, d.fitVertical)
	}
}

// Zoom sets the zoom factor; 1 shows the page at roughly its physical size.
// The factor is clamped to 0.2 to 4.
func (d *Document) Zoom(scale float64) {
	d.fitHorizontal, d.fitVertical = false, false
	d.setZoom(clampZoom(scale))
}

func (d *Document) setZoom(zoom float64) {
	_ = d.zoom.Set(zoom)
	d.applyScale(zoom)
}

func clampZoom(scale float64) float64 {
	return math.Max(minZoom, math.Min(maxZoom, scale))
}

// ZoomToFit scales the page so it fits completely into the view. The fit
// is kept when the view is resized, until the zoom is changed otherwise;
// the same holds for ZoomToFitVertical and ZoomToFitHorizontal.
func (d *Document) ZoomToFit() {
	d.fitZoom(true, true)
}

// ZoomToFitVertical scales the page to the height of the view.
func (d *Document) ZoomToFitVertical() {
	d.fitZoom(false, true)
}

// ZoomToFitHorizontal scales the page to the width of the view.
func (d *Document) ZoomToFitHorizontal() {
	d.fitZoom(true, false)
}

func (d *Document) fitZoom(horizontal, vertical bool) {
	d.fitHorizontal, d.fitVertical = horizontal, vertical
	view := d.scrollContainer.Size()
	page := d.bigPage.PageSize()
	if view.IsZero() || page.IsZero() {
		// Applied by Resize once the view has a size.
		return
	}

	scale := math.Inf(1)
	if horizontal {
		scale = math.Min(scale, float64(view.Width/page.Width))
	}
	if vertical {
		scale = math.Min(scale, float64(view.Height/page.Height))
	}
	d.fitScale = clampZoom(scale)
	d.setZoom(d.fitScale)
}
