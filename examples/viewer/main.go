// Command viewer is a minimal PDF viewer built on fyne-pdf. It is a module
// of its own, so the library does not depend on goprint; run it from its
// directory:
//
//	cd examples/viewer
//	go run . [-print-dialog native|fyne] [file.pdf]
//
// Without a file it starts empty; open a PDF with File > Open (Ctrl+O).
// File > Print (Ctrl+P) prints it through goprint: -print-dialog native
// (the default) shows the platform's print dialog, -print-dialog fyne the
// one fyneprint draws with Fyne, with a preview and a "Save as PDF" button.
package main

import (
	"errors"
	"flag"
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
	"github.com/timzifer/goprint"
	"github.com/timzifer/goprint/fyneprint"
)

type viewer struct {
	window   fyne.Window
	doc      *pdf.Document
	contents []byte
	name     string
	// title names the print job: the document's title or the file name.
	title string
	// fynePrintDialog selects fyneprint's dialog over the platform's.
	fynePrintDialog bool
}

func main() {
	printDialog := flag.String("print-dialog", "native", `print dialog: "native" (the platform's) or "fyne" (drawn by fyneprint)`)
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "usage: viewer [-print-dialog native|fyne] [file.pdf]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if (*printDialog != "native" && *printDialog != "fyne") || flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	a := app.NewWithID("io.github.timzifer.fyne-pdf.viewer")
	w := a.NewWindow("PDF Viewer")

	v := &viewer{window: w, doc: pdf.NewDocument(), fynePrintDialog: *printDialog == "fyne"}
	defer func() { _ = v.doc.Close() }()
	// All pages one below the other; the toolbar switches to single pages.
	v.doc.SetViewMode(pdf.ViewContinuous)

	// Render errors arrive on the main goroutine; a broken page should not
	// end the program.
	v.doc.OnError = func(page int, err error) {
		dialog.ShowError(fmt.Errorf("page %d: %w", page+1, err), w)
	}

	openItem := fyne.NewMenuItem("Open…", v.showOpenDialog)
	openItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyO, Modifier: fyne.KeyModifierShortcutDefault}
	w.Canvas().AddShortcut(openItem.Shortcut, func(fyne.Shortcut) { v.showOpenDialog() })
	printItem := fyne.NewMenuItem("Print…", v.showPrintDialog)
	printItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: fyne.KeyModifierShortcutDefault}
	w.Canvas().AddShortcut(printItem.Shortcut, func(fyne.Shortcut) { v.showPrintDialog() })
	w.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("File", openItem, printItem)))

	if path := flag.Arg(0); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		if err := v.load(contents, filepath.Base(path)); err != nil {
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
	v.title = name
	if src, err := pdf.OpenSource(contents); err == nil {
		if t := src.Metadata().Title; t != "" {
			title = t + " – " + name
			v.title = t
		}
		_ = src.Close()
	}
	v.window.SetTitle(fmt.Sprintf("%s (%d pages)", title, v.doc.PageCount()))
	return nil
}

// showPrintDialog prints the open PDF after the user confirmed the print
// dialog: the platform's or, with -print-dialog fyne, fyneprint's.
func (v *viewer) showPrintDialog() {
	if v.contents == nil {
		dialog.ShowInformation("Print", "Open a PDF first.", v.window)
		return
	}
	doc := goprint.PDFBytes(v.title, v.contents)
	// Both dialogs report back on the UI goroutine. Cancelling, and saving
	// as PDF in fyneprint's dialog, count as goprint.ErrCanceled.
	done := func(_ *goprint.Job, _ goprint.Settings, err error) {
		if err != nil && !errors.Is(err, goprint.ErrCanceled) {
			dialog.ShowError(err, v.window)
		}
	}
	if v.fynePrintDialog {
		fyneprint.ShowPrintDialog(v.window, doc, fyneprint.PrintDialogOptions{PrintNow: true}, done)
		return
	}
	fyneprint.ShowDialog(v.window, doc, goprint.DialogOptions{PrintNow: true}, done)
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
