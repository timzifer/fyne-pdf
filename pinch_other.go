//go:build !windows

package pdf

import "fyne.io/fyne/v2"

// hookWindow makes w report the pinches of the platform that Fyne does not
// pass on. Only Windows has a hook so far (see pinch_windows.go); the
// mobile driver reports the fingers to the page view itself.
func hookWindow(fyne.Window) bool {
	return true
}
