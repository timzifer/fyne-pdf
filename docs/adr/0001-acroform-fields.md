# 0001. AcroForm fields in the viewer

- Status: proposed
- Date: 2026-10-05
- Depends on: [cera ADR 0006](https://github.com/timzifer/cera/blob/main/docs/adr/0006-interactive-forms.md)
  (interactive forms and `FormWidgetProvider`), cera ADR 0005 (annotations)

## Context

PDFs with AcroForm fields (text fields, check boxes, radio buttons, combo
and list boxes, push buttons) are common in the documents fyne-pdf is used
for. Today the viewer shows them only as saved: cera draws the widgets'
appearance streams into the page image, nothing is interactive.

cera already provides everything below the viewer (ADR 0006, released in
v0.3.0):

- `Document.Form()`: the field tree with inheritance resolved, widgets per
  page, flags, options, `/DA` appearance.
- `FormState`: the values a user enters, owned by the caller, safe for
  concurrent use, with `Subscribe` for change notifications. The document
  itself is never modified.
- `FormLayer`: joins a page, a state and a `FormWidgetProvider` and turns view
  changes (`View{Scale, Viewport, Origin}`) into `Show`/`Hide` calls with
  placements in device pixels. `FormLayer.Skip` is passed as
  `RenderOptions.SkipAnnotation`, so natively drawn widgets are left out of
  the page image; it does not re-interpret the page.
- Generated appearances from a `FormState` (`RenderOptions.Form`) for widgets
  that are not drawn natively, and for thumbnails.
- `github.com/timzifer/cera/form/fyneform`: a `FormWidgetProvider` for fyne
  (Entry, PasswordEntry, Check, Select/SelectEntry, List, Button) in a
  container without layout, as a separate module so cera stays free of fyne
  and cgo.

What the viewer lacks:

- **Page lifetime**: `Source.RenderPage` opens a `cera.Page` per render and
  releases it. A `FormLayer` needs the page of the main view for as long as
  it is shown.
- **Geometry**: the page image is shown in a `canvas.Image` with
  `ImageFillContain`; when the page is smaller than the view, fyne centres it
  at an offset the widget never learns. Widgets in an overlay need that
  origin exactly.
- **Scale**: the main view is rendered at a resolution that lags behind the
  zoom (debounced) and is capped at 25 MP; the displayed size is
  `PageSize * zoom`, not the image size.
- **Scrolling**: the viewport (visible part of the page) is not tracked.

Saving filled-in values into the PDF is **deliberately deferred** (see Out of
scope).

## Decision

### 1. cera's form layer, not a model of our own

The viewer uses `cera.Form`, `cera.FormState` and `cera.FormLayer`. It does
not parse `/AcroForm` itself and does not use `go-pdfkit/forms` for display.
Placement, skipping widgets in the page image and generated appearances stay
in one place (cera), so overlay and image cannot disagree.

### 2. fyneform as the provider

Widgets are drawn by `cera/form/fyneform`. fyne-pdf does not contain a
provider of its own.

Prerequisite: fyneform is published as a tagged module
(`form/fyneform/vX.Y.Z`) with its `replace github.com/timzifer/cera => ../..`
removed. Until then this ADR cannot be implemented.

### 3. Overlay in `Page`

`Page` becomes a stack of the page image and the provider's `Overlay`. `Page`
lays out the image itself instead of relying on `ImageFillContain`: it
computes the image rectangle (size `PageSize * Scale`, centred when the
widget is larger) and uses the same rectangle's top-left corner as
`View.Origin`. The image and the overlay therefore share one origin by
construction.

### 4. Placements use the displayed scale, not the rendered one

`View.Scale` is the scale at which the page is **shown**, in device pixels per
point:

```
View.Scale = zoom * 96/72 * canvas.Scale()
fyneform.Provider.PixelScale = canvas.Scale()
```

It is independent of the resolution the page image happens to be rendered
at. Widgets therefore land in the right place immediately on zoom, while the
sharper page image follows with the debounced re-render, and stay correct
when the image is capped at 25 MP and scaled up.

`View.Viewport` is the visible part of the page from the scroll offset of the
main view; `FormLayer.Update` runs on zoom, scroll, resize and page change.

### 5. Lifetime and threading

- The `Document` owns one `FormState` per loaded PDF, created from
  `Form().NewState()` when the PDF is loaded and dropped on reload and
  `Close`.
- `Source` keeps the `cera.Page` of the main view open while it is shown and
  releases only its display list between renders (`Page.Release` keeps the
  page usable). On page change the old `FormLayer` is closed and a new one is
  created.
- `FormLayer.Update` and the provider run on the fyne main goroutine. Page
  renders keep running in the background with
  `RenderOptions{Form: state, SkipAnnotation: layer.Skip}`.
- Reading the form (`Document.Form()`) and anything else that interprets the
  document goes through `Source`'s mutex, like rendering.

### 6. Values show everywhere, re-rendering is minimal

- Natively drawn widgets are not in the page image, so typing never
  re-renders the main view.
- Widgets cera draws itself (not supported by fyneform: signatures,
  `/MK /R` rotation; and every widget in thumbnails) use generated
  appearances from the `FormState`. A `Subscribe` callback marks the
  affected pages, and their thumbnails (and the main view, if it shows such a
  widget) are re-rendered, debounced like zoom renders.

### 7. API

```go
type Document struct {
	// ...
	// DisableForms shows form fields as saved in the PDF, not interactive.
	DisableForms bool
	// OnFormChanged is called on the main goroutine after the user changed
	// a field.
	OnFormChanged func(field *cera.Field)
	// OnFormButton is called when a push button is tapped. Actions of the
	// button (JavaScript, submit, reset) are not executed.
	OnFormButton func(widget *cera.Widget)
}

// Form returns the form of the loaded PDF, or nil if it has none.
func (d *Document) Form() *cera.Form

// FormState returns the values of the form fields, as entered by the user.
func (d *Document) FormState() *cera.FormState
```

The API exposes cera's types directly instead of wrapping them: both modules
are maintained together, and a copy of the field model would have to follow
every change of cera.

Applications that need the entered data (until saving exists: their own
storage, an API call, the existing `SaveCallback`) read it from
`FormState`.

### Out of scope

- **Saving into the PDF** (deferred on purpose): writing the `FormState` back
  as an incremental update. Building blocks exist in `go-pdfkit/forms` and
  `go-pdfkit/ops` (`ops.OpenForm`, `Filling.Fill`, `Filling.Bytes`), but with
  a field model separate from cera's, linked only by the fully qualified
  field name. ops refuses encrypted and repaired files, and the current
  versions of forms and ops need Go 1.27. To be decided in a separate ADR.
- **FDF/XFDF export.**
- **JavaScript** (`/AA`, calculate, format, validate): not executed, as in
  cera. `Field.HasActions` lets an application warn.
- **XFA**, **signature fields** and **signature validation**.
- **Tab navigation across pages** (`FormLayer.Next`): within the shown page
  first; across pages once the viewer scrolls through all pages
  continuously.

## Consequences

- Forms work without new rendering code in fyne-pdf; the viewer only wires
  page lifetime, geometry and state.
- fyne-pdf's public API depends on cera's form types. A breaking change in
  cera's form API is a breaking change in fyne-pdf.
- fyne-pdf gains a dependency on the fyneform module and cannot ship forms
  before it is tagged.
- `Page` changes from `ImageFillContain` to explicit layout. This is visible
  to anyone using `Page` on its own; the image keeps its aspect ratio as
  before.
- Native widgets can look different from the PDF's appearance (font, colours
  from `/DA` are not applied by fyneform yet). Thumbnails always show the
  generated appearance.
- Entered values are lost when the PDF is reloaded or the viewer is closed,
  unless the application stores them.

## Open questions

- `FormLayer.Update` resolves the default optional-content visibility from
  the document when `View.Layers` is nil. Check whether that reads the
  reader; if it does, the viewer passes an explicit `Visibility` computed
  once under `Source`'s mutex, so `Update` never touches the reader from the
  main goroutine.
- `Source.Bound` rounds the page size up to whole points. Placements come
  from cera and are exact, but the image rectangle in `Page` uses
  `PageSize`; either `Bound` returns float sizes or `Page` uses cera's
  `Page.Bounds(scale)`. Decide before implementing, so image and overlay
  match to the pixel.

## Testing

- Placement: with the fyne test driver, load a PDF with one field of each
  type and check that each overlay widget's position and size equal the
  widget rectangle at zoom 0.5, 1 and 3, with a scrolled viewport and with
  canvas scale 2.
- Alignment: render a page with and without `layer.Skip` and check that
  every placement covers the pixels that changed (the same check cera's
  ADR 0006 uses).
- State: typing into an entry updates `FormState`, calls `OnFormChanged` and
  re-renders the thumbnail of that page, not the main view.
- Reload and `Close` drop the state and hide all widgets.

## Alternatives considered

- **go-pdfkit/forms as the model, own placement.** Would prepare saving
  through ops, but duplicates what cera already parses, lacks the layer, the
  skip and the generated appearances, and needs its own transform from
  points to pixels. Rejected; saving can map values by field name later.
- **Render only, edit in a dialog.** Clicking a field opens an editor and the
  page is re-rendered with generated appearances. Simple, but no inline
  input, IME or tab order. Rejected.
- **A provider inside fyne-pdf.** Avoids the dependency on an unpublished
  module, but duplicates fyneform. Kept as fallback only if fyneform cannot
  be published.
