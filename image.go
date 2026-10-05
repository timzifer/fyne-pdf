package pdf

import (
	"image"
)

type DocumentCallbackFn func(src *Source)

// NewImageFromMemory rendert eine PDF-Seite (0-basiert). documentCallbacks
// werden vor dem Rendern mit der geöffneten Source aufgerufen.
func NewImageFromMemory(contents []byte, pageNumber int, documentCallbacks ...DocumentCallbackFn) (image.Image, error) {
	src, err := OpenSource(contents)
	if err != nil {
		return nil, err
	}

	for _, cb := range documentCallbacks {
		cb(src)
	}

	return src.RenderPage(pageNumber, 144)
}
