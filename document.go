package pdf

import (
	"context"
	"image"
	"image/color"
	"io"
	"log"
	"math"
	"net/url"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/cockroachdb/errors"
	"github.com/timzifer/cera"
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
	// horizontally), Ctrl +/-/0 zoom. On a touch screen (Android, iOS) two
	// fingers pinch to zoom around the point between them; on Windows a
	// pinch on a precision touchpad zooms as Ctrl+wheel does.
	//
	// [Document.Search] finds text in the background and highlights the
	// results on the pages; F3 and Shift+F3 go to the next and previous one.
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
		// OnSearchChanged is called on the main goroutine when the search
		// changes: results found, the current result, the search done or
		// cleared. See [Document.Search].
		OnSearchChanged func()
		// PasswordCallback, if set, is asked for the password of an
		// encrypted PDF that needs one: retry is false the first time and
		// true after a wrong password. It is called again until the
		// password opens the PDF or it returns ok false. If nil,
		// LoadFromMemory asks with a dialog on the widget's window.
		PasswordCallback func(retry bool) (password string, ok bool)
		// OnChanged is called when the annotations were changed, a change
		// was undone or redone, or the document was saved: when
		// Modified, CanUndo or CanRedo may say something else.
		OnChanged func()
		// OnToolChanged is called when the tool changes, also by the
		// toolbar's button for selecting text.
		OnToolChanged func()
		// Author is the author of the annotations made with the tools.
		Author string
		// Fonts supplies the fonts a PDF names but does not embed, see
		// [SourceOptions]; PDFs loaded after it is set use it. If nil,
		// such fonts are drawn with the stand-ins cera has built in.
		Fonts cera.FontProvider
		// AnnotationColor is the colour of the annotations made with the
		// tools; if nil, yellow for highlights and notes, red for
		// drawings and shapes, black for underlines and strikethroughs.
		AnnotationColor color.Color
		// OnLoaded is called on the main goroutine when a PDF has been
		// loaded: by LoadFromMemory, or once the password asked for by
		// its dialog has been entered.
		OnLoaded func()
		// FollowViewSettings opens a PDF the way it asks, see
		// [Document.ViewSettings]: at the page and view it names, with
		// all pages one below the other for "OneColumn" and with the
		// outline panel for "UseOutlines". NewDocument sets it; an
		// application that restores a view of its own switches it off.
		// The settings are applied before OnLoaded is called, so that
		// what OnLoaded does with the view stays.
		FollowViewSettings bool
		// OnLinkTapped, if set, is called when a link on a page is
		// tapped; returning true means it handled the link. Otherwise the
		// widget goes to the link's destination, opens its URI in the
		// browser, or follows its named action; a link to another PDF it
		// has the system open after the user agreed in a dialog. It never
		// starts a program, runs a script or sends a form.
		OnLinkTapped func(Link) bool
		// OnLinkHovered, if set, is called when the pointer comes over a
		// link on a page, and with nil when it leaves one, to say what
		// the link does: see [Link.Description].
		OnLinkHovered func(*Link)
		// OnPrint, if set, is called by a link that asks to print the
		// document; without it such links do nothing.
		OnPrint func()
		// Dir is the directory the loaded PDF comes from, for its links to
		// other PDFs that are named relative to it. The widget loads PDFs
		// from memory and does not know it; set it with the PDF.
		Dir string
		// HighlightColor fills the search results on the pages,
		// CurrentHighlightColor the current one. If nil, translucent
		// yellow and orange are used.
		HighlightColor, CurrentHighlightColor color.Color

		thumbnails []*Thumbnail
		// thumbnailDPIs is the resolution of each thumbnail.
		thumbnailDPIs []float64
		area          *pageArea

		base               *fyne.Container
		thumbnailContainer *fyne.Container
		thumbnailScroller  *container.Scroll
		toolbar            *fyne.Container
		zoom               binding.Float
		zoomSlider         *widget.Slider
		// pageEntry names the current page and takes the page to go to;
		// pagePosition says where the page is in the document.
		pageEntry       *widget.Entry
		pagePosition    *widget.Label
		scrollContainer *pageScroll

		toggleThumbnailsButton *widget.Button
		outlineButton          *widget.Button
		commentsButton         *widget.Button
		viewModeButton         *widget.Button
		toolButton             *widget.Button
		zoomInButton           *widget.Button
		zoomOutButton          *widget.Button
		saveButton             *widget.Button

		// scale is the zoom the page view shows.
		scale    float64
		viewMode ViewMode
		// pageSizes are the sizes of all pages at zoom 1.
		pageSizes []fyne.Size
		// pageLabels are the labels of all pages, "" for a page without;
		// labelled says that one has a label.
		pageLabels []string
		labelled   bool
		layout     pageLayout
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
		// pinch is the pinch gesture in progress, nil if there is none;
		// watched is the canvas whose window reports pinches.
		pinch   *pinch
		watched fyne.Canvas
		// loads counts the calls of LoadFromMemory and Close; a password
		// dialog answered after another one is ignored.
		loads int
		// passwordDialog asks for the password of the PDF being loaded,
		// passwordEntry is its entry; nil if no dialog is shown.
		passwordDialog *dialog.FormDialog
		passwordEntry  *widget.Entry
		// tool is what dragging does; selection is the selected text, nil
		// for none.
		tool      Tool
		selection *textSelection
		// sketch is what a drag draws with a drawing tool, nil while none
		// does; textDialog asks for the text of a note or a text box,
		// textEntry is its entry.
		sketch     *sketch
		moving     *annotationMove
		textDialog *dialog.FormDialog
		textEntry  *widget.Entry
		// outline is the outline panel, comments the comments panel.
		outline  *outlinePanel
		comments *commentsPanel
		// openURL opens the URIs of outline entries; replaced in tests.
		openURL func(*url.URL) error
		// history are the versions of the file, one per change.
		history editHistory
		// jumps are the views to go back and forward to; hovered is the
		// link under the pointer; fileDialog asks whether to open the
		// file a link leads to.
		jumps      viewHistory
		hovered    *Link
		fileDialog *dialog.ConfirmDialog
		// search is the state of the search, see Document.Search.
		search documentSearch
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
		// links are the links by page, nil while being read; a page
		// missing has not been asked for. texts are the texts of the
		// pages alike, read while a tool needs them.
		links map[int][]Link
		texts map[int]*cera.PageText
		// renderOptions are those of the main view; images rendered with
		// others are shown until they are rendered again.
		renderOptions RenderOptions
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
		FollowViewSettings: true,
		modifiers:          currentKeyModifiers,
		openURL:            func(u *url.URL) error { return fyne.CurrentApp().OpenURL(u) },
		scale:              1,
		views:              map[int]*Page{},
		rendered:           map[int]*renderedPage{},
		failed:             map[int]float64{},
		links:              map[int][]Link{},
		texts:              map[int]*cera.PageText{},
	}
	_ = d.zoom.Set(1)

	d.thumbnailScroller = container.NewHScroll(d.thumbnailContainer)

	d.area = newPageArea(d)
	d.scrollContainer = newPageScroll(d, d.area)
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

	d.toolButton = widget.NewButton("", func() {
		if d.tool == ToolSelect {
			d.SetTool(ToolNone)
		} else {
			d.SetTool(ToolSelect)
		}
	})
	d.toolButton.Icon = fyne_lucide.Icon(fyne_lucide.IconTextCursor)
	d.toolButton.Importance = widget.LowImportance

	d.saveButton = widget.NewButton("", nil)
	d.saveButton.Icon = fyne_lucide.Icon(fyne_lucide.IconSave)
	d.saveButton.Importance = widget.LowImportance
	d.saveButton.Hidden = true

	d.outline = newOutlinePanel(d)
	d.outlineButton = widget.NewButton("", func() {
		d.ShowOutline(!d.OutlineShown())
	})
	d.outlineButton.Icon = fyne_lucide.Icon(fyne_lucide.IconBookmark)
	d.outlineButton.Importance = widget.LowImportance
	d.outlineButton.Hidden = true

	d.comments = newCommentsPanel(d)
	d.commentsButton = widget.NewButton("", func() {
		d.ShowComments(!d.CommentsShown())
	})
	d.commentsButton.Icon = fyne_lucide.Icon(fyne_lucide.IconMessageSquare)
	d.commentsButton.Importance = widget.LowImportance
	d.commentsButton.Hidden = true

	d.toolbar.Add(d.outlineButton)
	d.toolbar.Add(d.toggleThumbnailsButton)
	d.toolbar.Add(d.newPageEntry())
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
	d.toolbar.Add(d.toolButton)
	d.toolbar.Add(d.commentsButton)
	d.toolbar.Add(d.saveButton)

	bottom := container.NewVBox(
		d.toolbar,
		d.thumbnailScroller,
	)
	// The view changes size with the window, but also when the thumbnail
	// strip, the outline or the comments are shown or hidden.
	d.base = container.New(
		&afterLayout{layout: layout.NewBorderLayout(nil, bottom, d.outline.box, d.comments.box), after: d.viewResized},
		bottom, d.outline.box, d.comments.box, d.scrollContainer,
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
	d.cancelPassword()
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
//
// An encrypted PDF that needs a password is opened with the one
// [Document.PasswordCallback] returns; if it gives up, the error wraps
// [ErrPassword]. Without a callback, LoadFromMemory shows a password dialog
// on the widget's window and returns nil at once: the PDF is loaded, and
// [Document.OnLoaded] called, when the right password is entered; the
// dialog asks again after a wrong one, and cancelling it leaves the
// previous PDF. A widget not on a window returns an error wrapping
// [ErrPassword].
func (d *Document) LoadFromMemory(contents []byte) error {
	d.cancelPassword()
	src, err := d.open(contents, "")
	if errors.Is(err, ErrPassword) {
		if d.PasswordCallback != nil {
			src, err = d.askPassword(contents)
		} else if w := d.window(); w != nil {
			d.showPasswordDialog(w, contents, d.loads, false)
			return nil
		}
	}
	if err != nil {
		return errors.Wrap(err, "could not open pdf from memory")
	}
	d.loaded(src)
	return nil
}

// open opens contents, a PDF or a version of the one loaded, with the
// options of the widget.
func (d *Document) open(contents []byte, password string) (*Source, error) {
	return OpenSourceWith(contents, SourceOptions{Password: password, Fonts: d.Fonts})
}

// loaded shows src and tells OnLoaded.
func (d *Document) loaded(src *Source) {
	d.history.reset(src.contents, src.password)
	d.jumps = viewHistory{}
	d.hoverLink(nil)
	d.load(src)
	d.followViewSettings(src)
	if d.OnLoaded != nil {
		d.OnLoaded()
	}
}

// askPassword opens contents with the passwords PasswordCallback returns.
func (d *Document) askPassword(contents []byte) (*Source, error) {
	for retry := false; ; retry = true {
		password, ok := d.PasswordCallback(retry)
		if !ok {
			return nil, errors.Wrap(ErrPassword, "no password given")
		}
		src, err := d.open(contents, password)
		if !errors.Is(err, ErrPassword) {
			return src, err
		}
	}
}

// showPasswordDialog asks on w for the password of contents, the PDF of
// load number load; retry says that the last one was wrong.
func (d *Document) showPasswordDialog(w fyne.Window, contents []byte, load int, retry bool) {
	message := "The document is protected by a password."
	if retry {
		message = "The password is wrong. Try again."
	}
	entry := widget.NewPasswordEntry()
	item := widget.NewFormItem("Password", entry)
	item.HintText = message
	items := []*widget.FormItem{item}
	var dlg *dialog.FormDialog
	dlg = dialog.NewForm("Password", "Open", "Cancel", items, func(ok bool) {
		if load != d.loads || d.passwordDialog != dlg {
			return
		}
		d.passwordDialog, d.passwordEntry = nil, nil
		if !ok {
			return
		}
		src, err := d.open(contents, entry.Text)
		switch {
		case errors.Is(err, ErrPassword):
			d.showPasswordDialog(w, contents, load, true)
		case err != nil:
			dialog.ShowError(err, w)
		default:
			d.loaded(src)
		}
	}, w)
	entry.OnSubmitted = func(string) { dlg.Submit() }
	d.passwordDialog, d.passwordEntry = dlg, entry
	dlg.Show()
	w.Canvas().Focus(entry)
}

// cancelPassword closes a password dialog shown for an earlier load.
func (d *Document) cancelPassword() {
	d.loads++
	if dlg := d.passwordDialog; dlg != nil {
		d.passwordDialog, d.passwordEntry = nil, nil
		dlg.Hide()
	}
}

// window returns the window showing the widget, or nil.
func (d *Document) window() fyne.Window {
	drv := fyne.CurrentApp().Driver()
	c := drv.CanvasForObject(d)
	if c == nil {
		return nil
	}
	for _, w := range drv.AllWindows() {
		if w.Canvas() == c {
			return w
		}
	}
	return nil
}

// loadedSource returns the loaded PDF, or nil.
func (d *Document) loadedSource() *Source {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.source
}

// Metadata returns the metadata of the loaded PDF, see [Source.Metadata].
func (d *Document) Metadata() Metadata {
	if src := d.loadedSource(); src != nil {
		return src.Metadata()
	}
	return Metadata{}
}

// Encrypted reports whether the loaded PDF is encrypted.
func (d *Document) Encrypted() bool {
	src := d.loadedSource()
	return src != nil && src.Encrypted()
}

// Permissions returns what the loaded PDF allows, see
// [Source.Permissions]; 0 if none is loaded.
func (d *Document) Permissions() Permissions {
	if src := d.loadedSource(); src != nil {
		return src.Permissions()
	}
	return 0
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

// ShowLinkBorders reports whether the main view draws the borders of links,
// see [Document.SetShowLinkBorders].
func (d *Document) ShowLinkBorders() bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.renderOptions.LinkBorders
}

// SetShowLinkBorders sets whether the main view draws the borders link
// annotations ask for, such as the coloured boxes around the links of LaTeX
// documents made with hyperref (see [RenderOptions.LinkBorders]). Off by
// default; thumbnails never show them. The pages in view are rendered
// again, showing the old images until then.
func (d *Document) SetShowLinkBorders(show bool) {
	d.mutex.Lock()
	if d.renderOptions.LinkBorders == show {
		d.mutex.Unlock()
		return
	}
	d.renderOptions.LinkBorders = show
	// A page that failed may render with the other options.
	clear(d.failed)
	d.mutex.Unlock()
	d.scheduleRender(0)
}

func (d *Document) updateViewModeButton() {
	if d.viewMode == ViewContinuous {
		d.viewModeButton.Icon = fyne_lucide.Icon(fyne_lucide.IconFiles)
	} else {
		d.viewModeButton.Icon = fyne_lucide.Icon(fyne_lucide.IconFile)
	}
	d.viewModeButton.Refresh()
}

// renderThumbnails renders the thumbnail of page, or of all pages for
// allPages, in the background.
func (d *Document) renderThumbnails(src *Source, ctx context.Context, page int) {
	thumbnails, dpis := d.thumbnails, d.thumbnailDPIs
	d.pending.Add(1)
	go func() {
		defer d.pending.Done()
		for i, thumbnail := range thumbnails {
			if page >= 0 && i != page {
				continue
			}
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
}

func (d *Document) load(src *Source) {
	d.resetSearch()
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
	d.links = map[int][]Link{}
	d.texts = map[int]*cera.PageText{}
	d.mutex.Unlock()
	d.selection = nil
	d.pinch = nil

	scale := d.canvasScale()

	d.pageSizes = make([]fyne.Size, numPages)
	d.pageLabels = make([]string, numPages)
	d.labelled = false
	d.thumbnails = make([]*Thumbnail, numPages)
	objects := make([]fyne.CanvasObject, numPages)
	dpis := make([]float64, numPages)
	for i := range numPages {
		pageNumber := i
		d.pageSizes[i] = fyne.Size{
			Width:  float32(float64(bounds[i].Dx()) * unitsPerPoint),
			Height: float32(float64(bounds[i].Dy()) * unitsPerPoint),
		}

		d.pageLabels[i] = src.PageLabel(i)
		d.labelled = d.labelled || d.pageLabels[i] != ""

		thumbnail := NewThumbnail()
		thumbnail.SetTitle(d.pageCaption(i))
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

	d.outline.setItems(src.Outline())
	d.ShowOutline(d.OutlineShown())
	d.comments.reset()
	d.loadAnnotations(src, ctx, numPages)

	d.thumbnailDPIs = dpis
	d.renderThumbnails(src, ctx, allPages)

	d.updatePageEntry()

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
	d.outline.markCurrent()
	d.updatePageEntry()
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
