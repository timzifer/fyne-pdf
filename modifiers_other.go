//go:build !windows

package pdf

import "fyne.io/fyne/v2"

// currentKeyModifiers returns the modifier keys held. Only Windows needs a
// workaround for auto-repeating modifiers (see modifiers_windows.go): macOS
// does not repeat modifier keys, and X11 does not by default.
func currentKeyModifiers() fyne.KeyModifier {
	return driverKeyModifiers()
}
