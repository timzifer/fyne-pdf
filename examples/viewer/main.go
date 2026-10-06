// Command viewer is a minimal PDF viewer built on fyne-pdf.
//
//	go run ./examples/viewer [file.pdf]
//
// Without an argument it starts empty; open a PDF with File > Open (Ctrl+O).
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	pdf "github.com/timzifer/fyne-pdf"
)

type viewer struct {
	window   fyne.Window
	doc      *pdf.Document
	contents []byte
	name     string
}

func main() {
	a := app.NewWithID("io.github.timzifer.fyne-pdf.viewer")
	w := a.NewWindow("PDF Viewer")

	v := &viewer{window: w, doc: pdf.NewDocument()}
	defer func() { _ = v.doc.Close() }()

	// Render errors arrive on the main goroutine; a broken page should not
	// end the program.
	v.doc.OnError = func(page int, err error) {
		dialog.ShowError(fmt.Errorf("page %d: %w", page+1, err), w)
	}

	openItem := fyne.NewMenuItem("Open…", v.showOpenDialog)
	openItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: fyne.KeyModifierShortcutDefault}
	w.Canvas().AddShortcut(openItem.Shortcut, func(fyne.Shortcut) { v.showOpenDialog() })
	w.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("File", openItem)))

	if len(os.Args) > 1 {
		contents, err := os.ReadFile(os.Args[1])
		if err != nil {
			log.Fatal(err)
		}
		if err := v.load(contents, filepath.Base(os.Args[1])); err != nil {
			log.Fatal(err)
		}
	}

	w.SetContent(v.doc)
	w.Resize(fyne.NewSize(1024, 768))
	w.ShowAndRun()
}

// load shows contents in the viewer and titles the window after the
// document.
func (v *viewer) load(contents []byte, name string) error {
	if err := v.doc.LoadFromMemory(contents); err != nil {
		return err
	}
	v.contents, v.name = contents, name

	// The save button appears in the toolbar once a callback is set.
	v.doc.SaveCallback = v.showSaveDialog
	v.doc.Refresh()

	title := name
	if src, err := pdf.OpenSource(contents); err == nil {
		if t := src.Metadata().Title; t != "" {
			title = t + " – " + name
		}
		_ = src.Close()
	}
	v.window.SetTitle(fmt.Sprintf("%s (%d pages)", title, v.doc.PageCount()))
	return nil
}

func (v *viewer) showOpenDialog() {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		defer func() { _ = r.Close() }()
		contents, err := io.ReadAll(r)
		if err == nil {
			err = v.load(contents, r.URI().Name())
		}
		if err != nil {
			dialog.ShowError(err, v.window)
		}
	}, v.window)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".pdf"}))
	d.Show()
}

// showSaveDialog saves a copy of the open PDF.
func (v *viewer) showSaveDialog() {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil || w == nil {
			return
		}
		_, err = w.Write(v.contents)
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			dialog.ShowError(err, v.window)
		}
	}, v.window)
	d.SetFileName(v.name)
	d.Show()
}
