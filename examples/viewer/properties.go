package main

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	pdf "github.com/timzifer/fyne-pdf"
)

// permissionNames are the permissions of an encrypted PDF in words.
var permissionNames = []struct {
	perm pdf.Permissions
	name string
}{
	{pdf.PermPrint, "printing"},
	{pdf.PermCopy, "copying"},
	{pdf.PermModify, "changing"},
	{pdf.PermAnnotate, "commenting"},
	{pdf.PermFillForms, "filling in forms"},
	{pdf.PermAssemble, "arranging pages"},
}

// showProperties shows what the document says about itself, File >
// Properties: its metadata, which can be changed where the document
// allows changes, the size of the current page, and what it permits.
func (v *viewer) showProperties() {
	if v.contents == nil {
		dialog.ShowInformation("Properties", "Open a PDF first.", v.window)
		return
	}
	m := v.doc.Metadata()
	editable := v.doc.Permissions()&pdf.PermModify != 0

	title, author := widget.NewEntry(), widget.NewEntry()
	subject, keywords := widget.NewEntry(), widget.NewEntry()
	title.SetText(m.Title)
	author.SetText(m.Author)
	subject.SetText(m.Subject)
	keywords.SetText(m.Keywords)
	if !editable {
		for _, e := range []*widget.Entry{title, author, subject, keywords} {
			e.Disable()
		}
	}

	page := v.doc.CurrentPage()
	items := []*widget.FormItem{
		widget.NewFormItem("Title", title),
		widget.NewFormItem("Author", author),
		widget.NewFormItem("Subject", subject),
		widget.NewFormItem("Keywords", keywords),
		widget.NewFormItem("Created with", widget.NewLabel(orDash(m.Creator))),
		widget.NewFormItem("Producer", widget.NewLabel(orDash(m.Producer))),
		widget.NewFormItem("Created", widget.NewLabel(dateText(m.Created))),
		widget.NewFormItem("Modified", widget.NewLabel(dateText(m.Modified))),
		widget.NewFormItem("File", widget.NewLabel(fmt.Sprintf("%s, %s", v.name, sizeText(len(v.doc.Contents()))))),
		widget.NewFormItem("Pages", widget.NewLabel(fmt.Sprint(v.doc.PageCount()))),
		widget.NewFormItem(fmt.Sprintf("Size of page %d", page+1), widget.NewLabel(pageSizeText(v.doc.PageSize(page)))),
		widget.NewFormItem("Security", widget.NewLabel(securityText(v.doc))),
	}
	d := dialog.NewForm("Properties", "OK", "Cancel", items, func(ok bool) {
		if !ok || !editable {
			return
		}
		if title.Text == m.Title && author.Text == m.Author && subject.Text == m.Subject && keywords.Text == m.Keywords {
			return
		}
		m.Title, m.Author, m.Subject, m.Keywords = title.Text, author.Text, subject.Text, keywords.Text
		m.Modified = time.Now()
		// A step of undo; errors arrive through OnError.
		v.doc.SetMetadata(m)
	}, v.window)
	d.Resize(fyne.NewSize(520, d.MinSize().Height))
	d.Show()
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

func dateText(t time.Time) string {
	if t.IsZero() {
		return "–"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func sizeText(bytes int) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(bytes)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", bytes)
}

// pageSizeText gives a size in points as millimetres, with the points.
func pageSizeText(width, height float64) string {
	const mm = 25.4 / 72
	return fmt.Sprintf("%.0f × %.0f mm (%.0f × %.0f pt)", width*mm, height*mm, width, height)
}

// securityText says whether the document is encrypted and what it
// permits then.
func securityText(doc *pdf.Document) string {
	if !doc.Encrypted() {
		return "Not encrypted"
	}
	perms := doc.Permissions()
	if perms == pdf.AllPermissions {
		return "Encrypted; permits everything"
	}
	var allowed []string
	for _, p := range permissionNames {
		if perms&p.perm != 0 {
			allowed = append(allowed, p.name)
		}
	}
	if len(allowed) == 0 {
		return "Encrypted; permits reading only"
	}
	return "Encrypted; permits " + strings.Join(allowed, ", ")
}
