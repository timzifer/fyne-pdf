# fyne-pdf

[![CI](https://github.com/timzifer/fyne-pdf/actions/workflows/ci.yml/badge.svg)](https://github.com/timzifer/fyne-pdf/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/timzifer/fyne-pdf.svg)](https://pkg.go.dev/github.com/timzifer/fyne-pdf)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A PDF viewer widget for [Fyne](https://fyne.io), written in pure Go.

Pages are rendered with [cera](https://github.com/timzifer/cera) — no MuPDF,
no cgo PDF library, no external binaries.

## Features

- `Document` widget: page view with thumbnail strip, zoom slider,
  zoom in/out, 100 %, fit page / fit width / fit height
- Optional save button via `SaveCallback`
- `Source`: goroutine-safe access to an opened PDF — page count, page bounds,
  rendering at arbitrary DPI, document metadata
- `NewImageFromMemory`: render a single page to an `image.Image` without any UI
- Render timeout per page (15 s); slow pages fall back to the partially drawn
  image instead of blocking the UI

## Installation

```sh
go get github.com/timzifer/fyne-pdf
```

Requires Go 1.26+. Fyne itself needs a C compiler and the platform graphics
headers — see the [Fyne prerequisites](https://docs.fyne.io/started/).

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
Known gaps:

- Page rendering uses a fixed resolution (144 dpi) independent of zoom level.
- No text selection, search, links or annotations.

## Contributing

Bug reports and pull requests are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md).
For rendering bugs, please attach the PDF (or a minimal one reproducing the issue)
if you can share it.

## License

[MIT](LICENSE)
