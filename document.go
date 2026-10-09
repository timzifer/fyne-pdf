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

	// zoomRenderDelay debounces re-rendering while the zoom changes.
	zoomRenderDelay = 150 * time.Millisecond

	// zoomSliderWidth is the minimum width of the zoom slider; in the
	// toolbar's HBox it would otherwise shrink to the size of its thumb.
	zoomSliderWidth = 150
)

// ViewMode selects how the main view of a [Document] shows the pages.
type ViewMode int

const (
	// ViewSinglePage shows one page at a time; scrolling past its top or
	// bottom turns to the previous or next one.
	ViewSinglePage ViewMode = iota
	// ViewContinuous shows all pages one below the other, scrolled through
	// without a break.
	ViewContinuous
)

type (
	// Document is a PDF viewer widget: a page view with a toolbar for zooming
	// and a thumbnail strip for navigation. Create it with [NewDocument].
	//
	// The page view shows one page at a time or, see [Document.SetViewMode],
	// all pages one below the other. It supports the usual viewer gestures:
	// Ctrl+wheel (Cmd on macOS) zooms around the pointer, dragging pans, a
	// double tap switches between the fitted page and 100 %; showing one
	// page, scrolling on past its top or bottom turns it. Once clicked, it
	// takes the keyboard focus: Page Up/Down and Space scroll by a screen,
	// Home/End go to the first and last page, the arrow keys scroll
	// (Left/Right go to the previous and next page while the pages fit
	// horizontally), Ctrl +/-/0 zoom.
	//
	// Pages are rendered in the background, those in view first; the
	// resolution of the main view follows the zoom level. Methods must be
	// called from the Fyne main goroutine (or before the app runs).
	Document struct {
		widget.BaseWidget

		// BackgroundColor fills the area around the pages. If nil, the
		// theme's separator color is used.
		BackgroundColor color.Color
		// SaveCallback, if set, shows a save button in the toolbar that calls
		// it. Call Refresh after changing it.
		SaveCallback func()
		// OnError is called on the main goroutine when a page cannot be
		// rendered. If nil, the error is logged.
		OnError func(page int, err error)

		thumbnails []*Thumbnail
		area       *pageArea

		base               *fyne.Container
		thumbnailContainer *fyne.Container
		thumbnailScroller  *container.Scroll
		toolbar            *fyne.Container
		zoom               binding.Float
		zoomSlider         *widget.Slider
		scrollContainer    *container.Scroll

		toggleThumbnailsButton *widget.Button
		viewModeButton         *widget.Button
		zoomInButton           *widget.Button
		zoomOutButton          *widget.Button
		saveButton             *widget.Button

		// scale is the zoom the page view shows.
		scale    float64
		viewMode ViewMode
		// pageSizes are the sizes of all pages at zoom 1.
		pageSizes []fyne.Size
		layout    pageLayout
		// views shows the pages in or next to the view, by page; spareViews
		// are views to reuse.
		views      map[int]*Page
		spareViews []*Page
		// anchor is the page point at the top left of the view; it stays
		// there when the view is resized. view is the size it was taken at.
		anchor viewAnchor
		view   fyne.Size
		// adjusting is set while the layout and the scroll offset change
		// together; navigating while the view scrolls to a page that is
		// made current.
		adjusting, navigating bool

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
		renderTimer *time.Timer
		// pending counts scheduled renders that have not finished yet.
		pending sync.WaitGroup
		// jobs are the renders wanted; rendering is set while a render
		// loop works through them, inflight is the render in progress.
		jobs           []renderJob
		rendering      bool
		inflight       renderJob
		inflightCancel context.CancelFunc
		// rendered holds the page images for the main view, failed the
		// resolution a page could not be rendered at.
		rendered       map[int]*renderedPage
		renderedPixels int
		useClock       uint64
		failed         map[int]float64
	}
)

var emptyImage image.Image = image.NewGray(image.Rectangle{})

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
		scale:              1,
		views:              map[int]*Page{},
		rendered:           map[int]*renderedPage{},
		failed:             map[int]float64{},
	}
	_ = d.zoom.Set(1)

	d.thumbnailScroller = container.NewHScroll(d.thumbnailContainer)

	d.area = newPageArea(d)
	d.scrollContainer = container.NewScroll(d.area)
	d.scrollContainer.OnScrolled = func(fyne.Position) { d.scrolled() }

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

	d.viewModeButton = widget.NewButton("", func() {
		if d.viewMode == ViewContinuous {
			d.SetViewMode(ViewSinglePage)
		} else {
			d.SetViewMode(ViewContinuous)
		}
	})
	d.viewModeButton.Importance = widget.LowImportance
	d.updateViewModeButton()

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
	d.toolbar.Add(d.viewModeButton)
	d.toolbar.Add(d.saveButton)

	bottom := container.NewVBox(
		d.toolbar,
		d.thumbnailScroller,
	)
	// The view changes size with the window, but also when the thumbnail
	// strip is shown or hidden.
	d.base = container.New(
		&afterLayout{layout: layout.NewBorderLayout(nil, bottom, nil, nil), after: d.viewResized},
		bottom, d.scrollContainer,
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

// CurrentPage returns the index (0-based) of the page shown in the main
// view. Showing all pages, it is the page at the centre of the view, or
// the one last gone to with [Document.ShowPage] or a key until the view is
// scrolled.
func (d *Document) CurrentPage() int {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.current
}

// ViewMode returns how the main view shows the pages.
func (d *Document) ViewMode() ViewMode {
	return d.viewMode
}

// SetViewMode switches between showing one page at a time
// ([ViewSinglePage], the default) and all pages one below the other
// ([ViewContinuous]). The current page stays in view, as do the zoom and
// the fit. The toolbar has a button for it, too.
func (d *Document) SetViewMode(mode ViewMode) {
	if mode == d.viewMode {
		return
	}
	anchor := d.anchorOnPage(d.CurrentPage(), fyne.Position{})
	d.viewMode = mode
	d.updateViewModeButton()

	d.relayout()
	if offset, ok := d.anchorOffset(anchor); ok {
		d.setOffset(offset)
	}
	d.navigating = true
	d.viewChanged(0)
	d.navigating = false
}

func (d *Document) updateViewModeButton() {
	if d.viewMode == ViewContinuous {
		d.viewModeButton.Icon = fyne_lucide.Icon(fyne_lucide.IconFiles)
	} else {
		d.viewModeButton.Icon = fyne_lucide.Icon(fyne_lucide.IconFile)
	}
	d.viewModeButton.Refresh()
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
	d.current = 0
	d.jobs, d.rendering, d.inflightCancel = nil, false, nil
	d.rendered, d.renderedPixels = map[int]*renderedPage{}, 0
	d.failed = map[int]float64{}
	d.mutex.Unlock()

	scale := d.canvasScale()

	d.pageSizes = make([]fyne.Size, numPages)
	d.thumbnails = make([]*Thumbnail, numPages)
	objects := make([]fyne.CanvasObject, numPages)
	dpis := make([]float64, numPages)
	for i := range numPages {
		pageNumber := i
		d.pageSizes[i] = fyne.Size{
			Width:  float32(float64(bounds[i].Dx()) * unitsPerPoint),
			Height: float32(float64(bounds[i].Dy()) * unitsPerPoint),
		}

		thumbnail := NewThumbnail()
		thumbnail.SetTitle(strconv.Itoa(pageNumber + 1))
		thumbnail.SetSelected(i == 0)
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
	d.thumbnailScroller.ScrollToOffset(fyne.Position{})

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
				// The thumbnail stands in for the page until it is
				// rendered.
				d.showImages()
			})
		}
	}()

	d.clearViews()
	d.fitHorizontal, d.fitVertical = true, true
	d.relayout()
	d.applyFit()
	d.setOffset(fyne.Position{})
	d.viewChanged(0)
}

// applyScale shows the pages at zoom, keeping the page point at the centre
// of the view in place. The zoom binding's listener calls it, but only
// asynchronously; code that needs the new layout right away (a fit, a page
// turn, zooming around a point) calls it directly.
func (d *Document) applyScale(zoom float64) {
	view := d.scrollContainer.Size()
	d.applyScaleAt(zoom, fyne.NewPos(view.Width/2, view.Height/2))
}

// applyScaleAt shows the pages at zoom, keeping the page point at view
// position at in place.
func (d *Document) applyScaleAt(zoom float64, at fyne.Position) {
	if zoom == d.scale {
		return
	}
	anchor := d.anchorAt(at)
	d.scale = zoom
	d.relayout()
	if offset, ok := d.anchorOffset(anchor); ok {
		d.setOffset(offset)
	}
	d.viewChanged(zoomRenderDelay)
}

// ShowPage makes page (0-based) current. Showing one page at a time, it
// shows page fitted into the view. Showing all pages, it scrolls to the top
// of page and keeps the zoom, or the fit applied to page.
func (d *Document) ShowPage(page int) {
	if d.viewMode == ViewSinglePage && page >= 0 && page < d.PageCount() && page != d.CurrentPage() {
		d.fitHorizontal, d.fitVertical = true, true
	}
	d.turnPage(page, false)
}

// setCurrent makes page the current page and selects its thumbnail.
func (d *Document) setCurrent(page int) {
	d.mutex.Lock()
	changed := d.current != page
	d.current = page
	d.mutex.Unlock()
	if !changed {
		return
	}
	for i, thumbnail := range d.thumbnails {
		thumbnail.SetSelected(i == page)
	}
	d.revealThumbnail(page)
}

// revealThumbnail scrolls the thumbnail strip so the thumbnail of page is
// visible.
func (d *Document) revealThumbnail(page int) {
	if page >= len(d.thumbnails) || !d.thumbnailScroller.Visible() {
		return
	}
	thumbnail := d.thumbnails[page]
	x, width := thumbnail.Position().X, thumbnail.Size().Width
	offset := d.thumbnailScroller.Offset
	view := d.thumbnailScroller.Size().Width
	switch {
	case x < offset.X:
		offset.X = x
	case x+width > offset.X+view:
		offset.X = x + width - view
	}
	d.thumbnailScroller.ScrollToOffset(offset)
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
	if d.SaveCallback != nil {
		d.saveButton.OnTapped = d.SaveCallback
		d.saveButton.Show()
	} else {
		d.saveButton.Hide()
	}

	d.scrollContainer.Refresh()
	d.thumbnailContainer.Refresh()
	d.base.Refresh()
}

// Resize lays out the widget. While the page is fitted to the view (after
// ShowPage or one of the ZoomToFit methods), the fit follows the new size.
func (d *Document) Resize(size fyne.Size) {
	d.BaseWidget.Resize(size)
	d.applyFit()
}

// Zoom sets the zoom factor; 1 shows the page at roughly its physical size.
// The factor is clamped to 0.2 to 4.
func (d *Document) Zoom(scale float64) {
	d.fitHorizontal, d.fitVertical = false, false
	d.setZoom(clampZoom(scale))
}

func (d *Document) setZoom(zoom float64) {
	d.applyScale(zoom)
	_ = d.zoom.Set(zoom)
}

func clampZoom(scale float64) float64 {
	return math.Max(minZoom, math.Min(maxZoom, scale))
}

// ZoomToFit scales the current page so it fits completely into the view;
// showing all pages, it also scrolls to the top of that page. The fit is
// kept when the view is resized, until the zoom is changed otherwise; the
// same holds for ZoomToFitVertical and ZoomToFitHorizontal.
func (d *Document) ZoomToFit() {
	d.fitZoom(true, true)
}

// ZoomToFitVertical scales the current page to the height of the view.
func (d *Document) ZoomToFitVertical() {
	d.fitZoom(false, true)
}

// ZoomToFitHorizontal scales the current page to the width of the view.
func (d *Document) ZoomToFitHorizontal() {
	d.fitZoom(true, false)
}

func (d *Document) fitZoom(horizontal, vertical bool) {
	d.fitHorizontal, d.fitVertical = horizontal, vertical
	d.applyFit()
	if page := d.CurrentPage(); vertical && d.viewMode == ViewContinuous && d.layout.contains(page) {
		d.scrollToPage(page, false)
	}
}

// applyFit sets the zoom of the active fit, if any, for the current page.
func (d *Document) applyFit() {
	if !d.fitHorizontal && !d.fitVertical {
		return
	}
	view := d.scrollContainer.Size()
	var page fyne.Size
	if current := d.CurrentPage(); current < len(d.pageSizes) {
		page = d.pageSizes[current]
	}
	if view.IsZero() || page.IsZero() {
		// Applied by Resize once the view has a size.
		return
	}

	scale := math.Inf(1)
	if d.fitHorizontal {
		scale = math.Min(scale, float64(view.Width/page.Width))
	}
	if d.fitVertical {
		scale = math.Min(scale, float64(view.Height/page.Height))
	}
	d.fitScale = clampZoom(scale)
	// The page point at the top left stays, as when resizing.
	d.applyScaleAt(d.fitScale, fyne.Position{})
	_ = d.zoom.Set(d.fitScale)
}
