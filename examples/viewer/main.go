// Command viewer is a minimal PDF viewer built on fyne-pdf. It is a module
// of its own, so the library does not depend on goprint; run it from its
// directory:
//
//	cd examples/viewer
//	go run . [-print-dialog native|fyne] [-find text] [file.pdf]
//
// Without a file it starts empty; open a PDF with File > Open (Ctrl+O).
// Edit > Find (Ctrl+F) searches it: the results are highlighted on the
// pages and listed beside them; Enter and F3 go to the next result,
// Shift+F3 to the previous one, a click in the list to that result;
// -find searches for text right away.
// The text tool of the toolbar selects text; Edit > Copy (Ctrl+C) copies it.
// Annotate picks a tool to highlight, underline or strike out text, to add
// notes and text boxes, to draw and to draw shapes; Edit > Undo and Redo
// (Ctrl+Z, Ctrl+Y) take changes back, Delete removes the selected
// annotation, the save button writes the document with them.
// The outline of a document with bookmarks is shown beside the pages.
// An encrypted PDF asks for its password; without the right to print,
// File > Print is disabled.
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
	"os/user"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	pdf "github.com/timzifer/fyne-pdf"
	"github.com/timzifer/fyne_lucide"
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

	mainMenu       *fyne.MainMenu
	printItem      *fyne.MenuItem
	copyItem       *fyne.MenuItem
	fullScreenItem *fyne.MenuItem
	// windowTitle is the window title without the mark of changes.
	windowTitle string
	// onLoaded, if set, runs once the next PDF is loaded.
	onLoaded func()
}

func main() {
	find := flag.String("find", "", "search the document for this text at the start")
	printDialog := flag.String("print-dialog", "native", `print dialog: "native" (the platform's) or "fyne" (drawn by fyneprint)`)
	flag.Usage = func() {
		_, _ = fmt.Fprintln(flag.CommandLine.Output(), "usage: viewer [-print-dialog native|fyne] [-find text] [file.pdf]")
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
	v.printItem = printItem
	printItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyP, Modifier: fyne.KeyModifierShortcutDefault}
	w.Canvas().AddShortcut(printItem.Shortcut, func(fyne.Shortcut) {
		if !printItem.Disabled {
			v.showPrintDialog()
		}
	})
	// File > Properties (Ctrl+D) shows and edits the metadata.
	propertiesItem := fyne.NewMenuItem("Properties…", v.showProperties)
	propertiesItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyD, Modifier: fyne.KeyModifierShortcutDefault}
	w.Canvas().AddShortcut(propertiesItem.Shortcut, func(fyne.Shortcut) { v.showProperties() })

	// Edit > Find (Ctrl+F) shows the search bar; F3 and Shift+F3 go to the
	// next and previous result, in the search bar and in the document.
	search := newSearchPanel(v.doc)
	findItem := fyne.NewMenuItem("Find…", func() { search.show(w.Canvas()) })
	findItem.Shortcut = &desktop.CustomShortcut{KeyName: fyne.KeyF, Modifier: fyne.KeyModifierShortcutDefault}
	w.Canvas().AddShortcut(findItem.Shortcut, func(fyne.Shortcut) { search.show(w.Canvas()) })
	// Edit > Copy copies the text selected with the text tool of the
	// toolbar; Ctrl+C does so while the pages have the focus.
	copyItem := fyne.NewMenuItem("Copy", func() { v.doc.CopySelection() })
	v.copyItem = copyItem
	nextItem := fyne.NewMenuItem("Find Next (F3)", v.doc.NextSearchResult)
	previousItem := fyne.NewMenuItem("Find Previous (Shift+F3)", v.doc.PreviousSearchResult)

	// View > Link Borders draws the boxes hyperref puts around links, as
	// Acrobat and Firefox do.
	linkBordersItem := fyne.NewMenuItem("Link Borders", nil)
	// View > Full Screen; a document that asks for it is offered it, see
	// loaded.
	v.fullScreenItem = fyne.NewMenuItem("Full Screen", func() { v.setFullScreen(!w.FullScreen()) })
	undoItem := fyne.NewMenuItem("Undo (Ctrl+Z)", v.doc.Undo)
	redoItem := fyne.NewMenuItem("Redo (Ctrl+Y)", v.doc.Redo)
	// The tools are in the Annotate menu and in the bar above the pages.
	annotateMenu := fyne.NewMenu("Annotate")
	toolBar := container.NewHBox()
	var toolButtons []*widget.Button
	tools := viewerTools()
	for _, tool := range tools {
		annotateMenu.Items = append(annotateMenu.Items, fyne.NewMenuItem(tool.label, func() { v.doc.SetTool(tool.tool) }))
		b := widget.NewButtonWithIcon("", tool.icon, func() { v.doc.SetTool(tool.tool) })
		b.Importance = widget.LowImportance
		toolButtons = append(toolButtons, b)
		toolBar.Add(b)
	}
	undoButton := widget.NewButtonWithIcon("", fyne_lucide.Icon(fyne_lucide.IconUndo2), v.doc.Undo)
	redoButton := widget.NewButtonWithIcon("", fyne_lucide.Icon(fyne_lucide.IconRedo2), v.doc.Redo)
	undoButton.Importance, redoButton.Importance = widget.LowImportance, widget.LowImportance
	toolBar.Add(widget.NewSeparator())
	toolBar.Add(undoButton)
	toolBar.Add(redoButton)
	mainMenu := fyne.NewMainMenu(
		fyne.NewMenu("File", openItem, printItem, fyne.NewMenuItemSeparator(), propertiesItem),
		fyne.NewMenu("Edit", undoItem, redoItem, fyne.NewMenuItemSeparator(), copyItem, fyne.NewMenuItemSeparator(), findItem, nextItem, previousItem),
		fyne.NewMenu("View", linkBordersItem, v.fullScreenItem),
		annotateMenu,
	)
	// The menus and the bar follow the tool and the history.
	update := func() {
		undoItem.Disabled, redoItem.Disabled = !v.doc.CanUndo(), !v.doc.CanRedo()
		setEnabled(undoButton, v.doc.CanUndo())
		setEnabled(redoButton, v.doc.CanRedo())
		for i, item := range annotateMenu.Items {
			item.Checked = v.doc.Tool() == tools[i].tool
			if item.Checked {
				toolButtons[i].Importance = widget.HighImportance
			} else {
				toolButtons[i].Importance = widget.LowImportance
			}
			toolButtons[i].Refresh()
		}
		mainMenu.Refresh()
		// The title of the document may be what changed.
		v.titleWindow()
	}
	v.doc.OnChanged = update
	v.doc.OnToolChanged = update
	if u, err := user.Current(); err == nil {
		v.doc.Author = u.Username
	}
	// Changes are not lost without asking.
	w.SetCloseIntercept(func() {
		if !v.doc.Modified() {
			w.Close()
			return
		}
		dialog.ShowConfirm("Unsaved changes", "Close without saving the changes?", func(ok bool) {
			if ok {
				w.Close()
			}
		}, w)
	})
	linkBordersItem.Action = func() {
		v.doc.SetShowLinkBorders(!v.doc.ShowLinkBorders())
		linkBordersItem.Checked = v.doc.ShowLinkBorders()
		mainMenu.Refresh()
	}
	w.SetMainMenu(mainMenu)
	v.mainMenu = mainMenu
	update()

	w.SetContent(container.NewBorder(container.NewVBox(toolBar, search.bar), nil, nil, nil, search.content))
	w.Resize(fyne.NewSize(1024, 768))

	if path := flag.Arg(0); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		if *find != "" {
			// After the password, if the PDF asks for one.
			v.onLoaded = func() {
				search.entry.SetText(*find)
				search.show(w.Canvas())
				search.search()
			}
		}
		// Loaded once the window is shown: the password dialog of an
		// encrypted PDF needs it.
		a.Lifecycle().SetOnStarted(func() {
			if err := v.load(contents, filepath.Base(path)); err != nil {
				log.Fatal(err)
			}
		})
	}
	w.ShowAndRun()
}

// toolItem is a tool of the Annotate menu and the bar.
type toolItem struct {
	label string
	icon  fyne.Resource
	tool  pdf.Tool
}

// viewerTools returns the tools of the Annotate menu and the bar. The icons
// take the colour of the theme, so the app has to be there.
func viewerTools() []toolItem {
	return []toolItem{
		{"Pan", fyne_lucide.Icon(fyne_lucide.IconHand), pdf.ToolNone},
		{"Select Text", fyne_lucide.Icon(fyne_lucide.IconTextCursor), pdf.ToolSelect},
		{"Highlight", fyne_lucide.Icon(fyne_lucide.IconHighlighter), pdf.ToolHighlight},
		{"Underline", fyne_lucide.Icon(fyne_lucide.IconUnderline), pdf.ToolUnderline},
		{"Strikethrough", fyne_lucide.Icon(fyne_lucide.IconStrikethrough), pdf.ToolStrikeOut},
		{"Note", fyne_lucide.Icon(fyne_lucide.IconStickyNote), pdf.ToolNote},
		{"Text Box", fyne_lucide.Icon(fyne_lucide.IconType), pdf.ToolFreeText},
		{"Draw", fyne_lucide.Icon(fyne_lucide.IconPencil), pdf.ToolInk},
		{"Rectangle", fyne_lucide.Icon(fyne_lucide.IconSquare), pdf.ToolRect},
		{"Ellipse", fyne_lucide.Icon(fyne_lucide.IconCircle), pdf.ToolEllipse},
		{"Line", fyne_lucide.Icon(fyne_lucide.IconMinus), pdf.ToolLine},
	}
}

func setEnabled(b *widget.Button, enabled bool) {
	if enabled {
		b.Enable()
	} else {
		b.Disable()
	}
}

// load shows contents in the viewer once it is loaded: at once, or after
// the password dialog of an encrypted PDF.
func (v *viewer) load(contents []byte, name string) error {
	v.doc.OnLoaded = func() { v.loaded(contents, name) }
	return v.doc.LoadFromMemory(contents)
}

// loaded titles the window after the document, and enables saving and,
// if the document allows it, printing.
func (v *viewer) loaded(contents []byte, name string) {
	v.contents, v.name = contents, name

	// The save button appears in the toolbar once a callback is set.
	v.doc.SaveCallback = v.showSaveDialog
	v.doc.Refresh()

	v.titleWindow()

	// Documents with bookmarks open with the outline beside the pages.
	v.doc.ShowOutline(len(v.doc.Outline()) > 0)

	// cera renders whatever the permissions say; not printing a document
	// that does not allow it is up to the viewer.
	v.printItem.Disabled = v.doc.Permissions()&pdf.PermPrint == 0
	v.copyItem.Disabled = v.doc.Permissions()&pdf.PermCopy == 0
	v.mainMenu.Refresh()

	// The widget follows what the document asks for but full screen,
	// which is the user's to decide.
	if v.doc.ViewSettings().PageMode == "FullScreen" && !v.window.FullScreen() {
		dialog.ShowConfirm("Full screen", "The document asks to be shown in full screen.", func(ok bool) {
			if ok {
				v.setFullScreen(true)
			}
		}, v.window)
	}

	if f := v.onLoaded; f != nil {
		v.onLoaded = nil
		f()
	}
}

// setFullScreen shows the window in full screen or ends it; View > Full
// Screen is checked meanwhile.
func (v *viewer) setFullScreen(full bool) {
	v.window.SetFullScreen(full)
	v.fullScreenItem.Checked = full
	v.mainMenu.Refresh()
}

// showPrintDialog prints the open PDF after the user confirmed the print
// dialog: the platform's or, with -print-dialog fyne, fyneprint's.
func (v *viewer) showPrintDialog() {
	if v.contents == nil {
		dialog.ShowInformation("Print", "Open a PDF first.", v.window)
		return
	}
	doc := goprint.PDFBytes(v.title, v.doc.Contents())
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
		err = v.doc.Save(w)
		if cerr := w.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			dialog.ShowError(err, v.window)
			return
		}
		v.contents = v.doc.Contents()
	}, v.window)
	d.SetFileName(v.name)
	d.Show()
}

// titleWindow titles the window after the document: its title and the name
// of its file, or the title alone if the document asks for that
// (DisplayDocTitle).
func (v *viewer) titleWindow() {
	if v.contents != nil {
		title := v.name
		v.title = v.name
		if t := v.doc.Metadata().Title; t != "" {
			title = t + " – " + v.name
			if v.doc.ViewSettings().DisplayDocTitle {
				title = t
			}
			v.title = t
		}
		v.windowTitle = fmt.Sprintf("%s (%d pages)", title, v.doc.PageCount())
	}
	v.showTitle()
}

// showTitle titles the window after the document, marked while it has
// unsaved changes.
func (v *viewer) showTitle() {
	title := v.windowTitle
	if title == "" {
		title = "PDF Viewer"
	}
	if v.doc.Modified() {
		title = "* " + title
	}
	v.window.SetTitle(title)
}
