# fyne-pdf

[![CI](https://github.com/timzifer/fyne-pdf/actions/workflows/ci.yml/badge.svg)](https://github.com/timzifer/fyne-pdf/actions/workflows/ci.yml)
[![Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/timzifer/fyne-pdf/badges/coverage.json)](https://github.com/timzifer/fyne-pdf/actions/workflows/ci.yml?query=branch%3Amain)
[![Go Reference](https://pkg.go.dev/badge/github.com/timzifer/fyne-pdf.svg)](https://pkg.go.dev/github.com/timzifer/fyne-pdf)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A PDF viewer widget for [Fyne](https://fyne.io), written in pure Go.

Pages are rendered with [cera](https://github.com/timzifer/cera) — no MuPDF,
no cgo PDF library, no external binaries.

## Features

- `Document` widget: page view with thumbnail strip, zoom slider,
  zoom in/out, 100 % (physical size), fit page / fit width / fit height
- Two view modes: one page at a time, or all pages one below the other,
  scrolled through without a break (`SetViewMode(pdf.ViewContinuous)` or the
  toolbar button). Only the pages in view get objects and renders, so
  documents with hundreds of pages scroll smoothly
- Viewer gestures: Ctrl+wheel zoom around the pointer, page turns when
  scrolling past a page's edge (single page mode), drag to pan, double tap
  for fit/100 %; keyboard: Page Up/Down, Space, Home/End, arrow keys,
  Ctrl +/-/0
- Rendering in the background, the UI never waits for a page; pages in view
  first, then their neighbours; the resolution of the main view follows the
  zoom level and the screen scale, the page images kept share a memory
  budget
- Text search: `Search` finds a query in the background (match case, whole
  words, ignore diacritics), highlights the results on the pages and scrolls
  to the current one; `NextSearchResult` / `PreviousSearchResult` (also F3 /
  Shift+F3), `ShowSearchResult(i)` and `SearchResults()` for a result list
- Link borders: `SetShowLinkBorders(true)` draws the boxes hyperref puts
  around links, as Acrobat and Firefox do (off by default, never on thumbnails)
- Encrypted PDFs: `LoadFromMemory` asks for the password with a dialog (or
  `PasswordCallback`), `OnLoaded` reports the loaded PDF; `Encrypted()` and
  `Permissions()` (`PermPrint`, `PermCopy`, …) tell what the author allows —
  enforcing it is up to the application. `OpenSourceWithPassword`,
  `ErrPassword` and `ErrUnsupportedEncryption` for use without the widget
- Optional save button via `SaveCallback`, render errors via `OnError`
- `Source`: goroutine-safe access to an opened PDF — page count, page bounds,
  rendering at arbitrary DPI (`RenderPageOptions` for link borders), document metadata, page text and search
- `NewImageFromMemory`: render a single page to an `image.Image` without any UI
- Render timeout per page (15 s); slow pages fall back to the partially drawn
  image instead of blocking the UI

## Installation

```sh
go get github.com/timzifer/fyne-pdf
```

Requires Go 1.27.1+, Fyne 2.6+ (uses `fyne.Do`) and cera v0.9.0+. This
module, like cera, replaces `github.com/ajroetker/go-highway` with
`github.com/timzifer/go-highway v0.0.13-timzifer.1`; replace directives do not
carry over to dependents, so add the same line to your `go.mod` if you want
the identical SIMD code paths (it builds without). Fyne itself needs a C
compiler and the platform graphics headers — see the [Fyne prerequisites](https://docs.fyne.io/started/).

## Usage

### Viewer widget

```go
package main

import (
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	pdf "github.com/timzifer/fyne-pdf"
)

func main() {
	a := app.New()
	w := a.NewWindow("PDF")

	contents, err := os.ReadFile("example.pdf")
	if err != nil {
		panic(err)
	}

	doc := pdf.NewDocument()
	defer doc.Close()
	if err := doc.LoadFromMemory(contents); err != nil {
		panic(err)
	}

	// doc.SetViewMode(pdf.ViewContinuous) // all pages one below the other
	// doc.ShowPage(2)                     // jump to the third page

	w.SetContent(doc)
	w.Resize(fyne.NewSize(1024, 768))
	w.ShowAndRun()
}
```

A complete viewer with an open dialog, save button, printing, search and
error reporting is in [examples/viewer](examples/viewer/main.go):

```sh
cd examples/viewer
go run . [-print-dialog native|fyne] [-find text] [file.pdf]
```

The example is a module of its own that builds against the library in this
repository (`replace` in its `go.mod`), so the printing packages stay out of
the library's dependencies.

File > Print (Ctrl+P) prints through [goprint](https://github.com/timzifer/goprint):
`-print-dialog native` (the default) shows the platform's print dialog,
`-print-dialog fyne` the dialog `fyneprint` draws with Fyne, with a preview
and a "Save as PDF" button.

Edit > Find (Ctrl+F) opens a search bar with options and a result count;
the results are listed beside the pages ([search.go](examples/viewer/search.go)).
View > Link Borders switches the link borders on and off. An encrypted PDF
asks for its password; File > Print is disabled when it does not allow
printing.

### Search

```go
doc.OnSearchChanged = func() { // on the main goroutine
	results := doc.SearchResults() // Page, Prefix, Text, Suffix, Rects
	fmt.Println(len(results), "found, current:", doc.CurrentSearchResult(), "searching:", doc.Searching())
}
doc.Search("gamma", pdf.SearchOptions{WholeWord: true})
doc.NextSearchResult()  // or F3 in the page view
doc.ShowSearchResult(3) // scroll to a result, e.g. picked from a list
doc.ClearSearch()
```

The results are highlighted on the pages (`HighlightColor`,
`CurrentHighlightColor`). Without UI, `Source.Search`, `Source.SearchPage` and
`Source.PageText` return the results and the text; `Rect`s are in points of
the displayed page, origin top left. `Page.SetHighlights` marks any areas of
a page.

### Render a page to an image

```go
img, err := pdf.NewImageFromMemory(contents, 0) // page index is 0-based
```

### Low-level access

```go
src, err := pdf.OpenSource(contents)
if err != nil {
	return err
}
defer src.Close()

fmt.Println(src.PageCount(), src.Metadata().Title)
img, err := src.RenderPage(0, 300) // 300 dpi
```

## Status

Usable, but young. The API may still change before v1.0.0.
Not supported yet: text selection, search, links, annotations.

## Contributing

Bug reports and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).
For rendering bugs, please attach the PDF (or a minimal one reproducing the issue)
if you can share it.

## License

[MIT](LICENSE)
