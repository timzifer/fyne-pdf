//go:build windows

package pdf

import (
	"testing"

	"fyne.io/fyne/v2/test"
)

func TestCurrentKeyModifiersWindows(t *testing.T) {
	test.NewTempApp(t)
	if err := procGetAsyncKeyState.Find(); err != nil {
		t.Fatal("GetAsyncKeyState not found:", err)
	}
	// Nothing is pressed while the tests run; mainly this must not fail.
	if m := currentKeyModifiers(); m != 0 {
		t.Logf("modifiers held during the test: %v", m)
	}
}
