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
- Rendering in the background, the UI never waits for a page; the resolution
  of the main view follows the zoom level and the screen scale
- Optional save button via `SaveCallback`, render errors via `OnError`
- `Source`: goroutine-safe access to an opened PDF — page count, page bounds,
  rendering at arbitrary DPI, document metadata
- `NewImageFromMemory`: render a single page to an `image.Image` without any UI
- Render timeout per page (15 s); slow pages fall back to the partially drawn
  image instead of blocking the UI

## Installation

```sh
go get github.com/timzifer/fyne-pdf
```

Requires Go 1.26+ and Fyne 2.6+ (uses `fyne.Do`). Fyne itself needs a C
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

	// doc.ShowPage(2) // jump to the third page

	w.SetContent(doc)
	w.Resize(fyne.NewSize(1024, 768))
	w.ShowAndRun()
}
```

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

fmt.Println(src.PageCount(), src.Metadata()["title"])
img, err := src.RenderPage(0, 300) // 300 dpi
```

## Status

Usable, but young. The API may still change before v1.0.0.
Not supported yet: text selection, search, links, annotations, scrolling
through all pages continuously.

## Contributing

Bug reports and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).
For rendering bugs, please attach the PDF (or a minimal one reproducing the issue)
if you can share it.

## License

[MIT](LICENSE)
