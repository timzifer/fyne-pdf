package pdf

import (
	"fyne.io/fyne/v2"
	"image"
)

type PageRenderer func(pageNumber int, size fyne.Size) (image.Image, error)
