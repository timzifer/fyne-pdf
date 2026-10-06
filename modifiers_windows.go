//go:build windows

package pdf

import (
	"fyne.io/fyne/v2"
	"golang.org/x/sys/windows"
)

var procGetAsyncKeyState = windows.NewLazySystemDLL("user32.dll").NewProc("GetAsyncKeyState")

// Virtual-key codes of the modifier keys.
var modifierKeys = []struct {
	vk  uintptr
	mod fyne.KeyModifier
}{
	{0x10, fyne.KeyModifierShift},   // VK_SHIFT
	{0x11, fyne.KeyModifierControl}, // VK_CONTROL
	{0x12, fyne.KeyModifierAlt},     // VK_MENU
	{0x5B, fyne.KeyModifierSuper},   // VK_LWIN
	{0x5C, fyne.KeyModifierSuper},   // VK_RWIN
}

// currentKeyModifiers asks Windows which modifier keys are held. Windows
// auto-repeats a held Ctrl after about half a second, and Fyne's driver then
// forgets it (see driverKeyModifiers): Ctrl+wheel would zoom only briefly.
//
// Remove this once the fix in Fyne (https://github.com/fyne-io/fyne/pull/6572)
// is released and required in go.mod.
func currentKeyModifiers() fyne.KeyModifier {
	if procGetAsyncKeyState.Find() != nil {
		return driverKeyModifiers()
	}
	var m fyne.KeyModifier
	for _, k := range modifierKeys {
		// The most significant bit is set while the key is down.
		if state, _, _ := procGetAsyncKeyState.Call(k.vk); state&0x8000 != 0 {
			m |= k.mod
		}
	}
	return m
}
