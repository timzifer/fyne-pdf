package pdf

import (
	"errors"
	"testing"

	"fyne.io/fyne/v2"
)

func TestDocumentPasswordCallback(t *testing.T) {
	d := newTestDocument(t)
	loaded := 0
	d.OnLoaded = func() { loaded++ }
	var asked []bool
	passwords := []string{"wrong", "user"}
	d.PasswordCallback = func(retry bool) (string, bool) {
		asked = append(asked, retry)
		p := passwords[0]
		passwords = passwords[1:]
		return p, true
	}
	loadAndWait(t, d, encryptedPDF("user", "owner", restricted))
	if len(asked) != 2 || asked[0] || !asked[1] {
		t.Errorf("asked with retry %v, want [false true]", asked)
	}
	if d.PageCount() != 1 || loaded != 1 {
		t.Errorf("%d pages, OnLoaded called %d times; want 1, 1", d.PageCount(), loaded)
	}
	if !d.Encrypted() || d.Permissions()&PermPrint != 0 {
		t.Errorf("encrypted %v, permissions %b", d.Encrypted(), d.Permissions())
	}
}

func TestDocumentPasswordCallbackCancelled(t *testing.T) {
	d := newTestDocument(t)
	loadAndWait(t, d, multiPagePDF(3))
	d.PasswordCallback = func(bool) (string, bool) { return "", false }
	err := d.LoadFromMemory(encryptedPDF("user", "owner", -4))
	if !errors.Is(err, ErrPassword) {
		t.Errorf("err %v, want ErrPassword", err)
	}
	if d.PageCount() != 3 {
		t.Errorf("%d pages, want the previous PDF's 3", d.PageCount())
	}
}

func TestDocumentPasswordDialog(t *testing.T) {
	d := newTestDocument(t)
	loaded := 0
	d.OnLoaded = func() { loaded++ }
	if err := d.LoadFromMemory(encryptedPDF("user", "owner", -4)); err != nil {
		t.Fatal(err)
	}
	first := d.passwordDialog
	if first == nil || loaded != 0 {
		t.Fatalf("dialog %v, loaded %d: want a dialog and nothing loaded", first, loaded)
	}

	d.passwordEntry.SetText("wrong")
	first.Submit()
	if d.passwordDialog == nil || d.passwordDialog == first || loaded != 0 {
		t.Fatal("not asked again after a wrong password")
	}

	d.passwordEntry.SetText("owner")
	d.passwordEntry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	settle(d)
	if d.passwordDialog != nil || loaded != 1 || d.PageCount() != 1 {
		t.Fatalf("after the right password: dialog %v, loaded %d, %d pages", d.passwordDialog, loaded, d.PageCount())
	}
	if d.Permissions() != AllPermissions {
		t.Errorf("owner password: permissions %b", d.Permissions())
	}
}

func TestDocumentPasswordDialogReplaced(t *testing.T) {
	d := newTestDocument(t)
	if err := d.LoadFromMemory(encryptedPDF("user", "owner", -4)); err != nil {
		t.Fatal(err)
	}
	old, entry := d.passwordDialog, d.passwordEntry
	loadAndWait(t, d, multiPagePDF(3))
	if d.passwordDialog != nil {
		t.Error("dialog still shown after loading another PDF")
	}
	// An answer to the old dialog does not replace the PDF loaded since.
	entry.SetText("user")
	old.Submit()
	if d.PageCount() != 3 {
		t.Errorf("%d pages, want 3", d.PageCount())
	}
}

func TestDocumentUnsupportedEncryption(t *testing.T) {
	d := newTestDocument(t)
	asked := false
	d.PasswordCallback = func(bool) (string, bool) { asked = true; return "", false }
	err := d.LoadFromMemory(unsupportedEncryptionPDF())
	if !errors.Is(err, ErrUnsupportedEncryption) || asked {
		t.Errorf("err %v, asked for a password %v; want ErrUnsupportedEncryption without asking", err, asked)
	}
}
