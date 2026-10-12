package pdf

import (
	"errors"
	"testing"
)

func TestOpenSourceEncrypted(t *testing.T) {
	src := encryptedPDF("user", "owner", restricted)
	if _, err := OpenSource(src); !errors.Is(err, ErrPassword) {
		t.Errorf("no password: err %v, want ErrPassword", err)
	}
	if _, err := OpenSourceWithPassword(src, "wrong"); !errors.Is(err, ErrPassword) {
		t.Errorf("wrong password: err %v, want ErrPassword", err)
	}

	s, err := OpenSourceWithPassword(src, "user")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Encrypted() {
		t.Error("Encrypted() = false")
	}
	p := s.Permissions()
	if p&(PermPrint|PermCopy) != 0 || p&PermAnnotate == 0 {
		t.Errorf("user password: permissions %b, want all but print and copy", p)
	}
	img, err := s.RenderPage(0, 72)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, _ := img.At(200, 592).RGBA(); r>>8 != 255 || g>>8 != 0 || b>>8 != 0 {
		t.Errorf("decrypted page: pixel %v, want red", img.At(200, 592))
	}
	_ = s.Close()
	if s.Encrypted() || s.Permissions() != 0 {
		t.Error("closed source still reports encryption or permissions")
	}

	s, err = OpenSourceWithPassword(src, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if p := s.Permissions(); p != AllPermissions {
		t.Errorf("owner password: permissions %b, want all", p)
	}
}

func TestOpenSourceUnsupportedEncryption(t *testing.T) {
	_, err := OpenSourceWithPassword(unsupportedEncryptionPDF(), "any")
	if !errors.Is(err, ErrUnsupportedEncryption) || errors.Is(err, ErrPassword) {
		t.Errorf("err %v, want ErrUnsupportedEncryption", err)
	}
}

func TestOpenSourceNotEncrypted(t *testing.T) {
	s, err := OpenSource(minimalPDF())
	if err != nil {
		t.Fatal(err)
	}
	if s.Encrypted() || s.Permissions() != AllPermissions {
		t.Errorf("plain PDF: encrypted %v, permissions %b", s.Encrypted(), s.Permissions())
	}
}
