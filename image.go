package pdf

import (
	"github.com/cockroachdb/errors"
	"github.com/gen2brain/go-fitz"
	"image"
)

type DocumentCallbackFn func(document *fitz.Document)

// NewImageFromMemory rendert eine PDF-Seite. Läuft komplett unter dem
// MuPDFMutex - documentCallbacks werden also unter dem Lock aufgerufen und
// dürfen ihn nicht selbst nehmen.
func NewImageFromMemory(contents []byte, pageNumber int, documentCallbacks ...DocumentCallbackFn) (image.Image, error) {
	MuPDFMutex.Lock()
	defer MuPDFMutex.Unlock()

	if doc, err := fitz.NewFromMemory(contents); err != nil {
		return nil, errors.Wrap(err, "could not open pdf from memory")
	} else {

		for _, cb := range documentCallbacks {
			cb(doc)
		}

		defer func() {
			_ = doc.Close()
		}()
		// render page beforehand, as we are going to close the doc as soon as this func is done
		if thumbnailImage, renderErr := doc.ImageDPI(pageNumber, 144); renderErr != nil {
			return nil, errors.Wrap(renderErr, "could not render pdf-page")
		} else {
			return thumbnailImage, nil
		}
	}
}
