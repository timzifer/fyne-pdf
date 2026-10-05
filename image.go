package pdf

import (
	"image"
)

// DocumentCallbackFn is called with the opened Source before rendering, see
// [NewImageFromMemory].
type DocumentCallbackFn func(src *Source)

// NewImageFromMemory renders page pageNumber (0-based) of the PDF in contents
// at 144 dpi. documentCallbacks are called with the opened Source before
// rendering.
func NewImageFromMemory(contents []byte, pageNumber int, documentCallbacks ...DocumentCallbackFn) (image.Image, error) {
	src, err := OpenSource(contents)
	if err != nil {
		return nil, err
	}
	defer func() { _ = src.Close() }()

	for _, cb := range documentCallbacks {
		cb(src)
	}

	return src.RenderPage(pageNumber, 144)
}
