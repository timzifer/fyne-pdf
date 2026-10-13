//go:build windows

package pdf

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"golang.org/x/sys/windows"
)

const (
	wmNCDestroy  = 0x0082
	wmMouseWheel = 0x020A
	// mkControl is set in the key state of a mouse message while Ctrl is
	// down.
	mkControl = 0x0008
)

// gwlpWndProc is the index of the window procedure (GWLP_WNDPROC, -4).
const gwlpWndProc = ^uintptr(3)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procCallWindowProc = user32.NewProc("CallWindowProcW")
	// 32-bit Windows has SetWindowLongW only.
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procSetWindowLong    = user32.NewProc("SetWindowLongW")

	// wheelProcs are the window procedures of the windows watched, as they
	// were before wheelHook took their place.
	wheelProcs = map[uintptr]uintptr{}
	wheelHook  = windows.NewCallback(watchWheel)
	// wheelControl is set while a wheel message that says Ctrl is down is
	// handled.
	wheelControl bool
)

// hookWindow makes a pinch on a precision touchpad zoom. Windows turns it
// into wheel messages that say Ctrl is down, as it does with a pinch on a
// touch screen the window does not handle itself, but no Ctrl key is
// pressed: GLFW passes on the wheel without its key state, and the page
// view would scroll. It reports false while w has no native window yet.
func hookWindow(w fyne.Window) bool {
	native, ok := w.(driver.NativeWindow)
	if !ok {
		return true
	}
	hooked := true
	native.RunNative(func(context any) {
		if c, ok := context.(driver.WindowsWindowContext); ok {
			hooked = c.HWND != 0 && hookWheel(c.HWND)
		}
	})
	return hooked
}

// hookWheel puts watchWheel in front of the window procedure of hwnd. It
// has to be called on the thread of the window, as the Fyne main goroutine
// is for the windows of the app.
func hookWheel(hwnd uintptr) bool {
	if _, ok := wheelProcs[hwnd]; ok {
		return true
	}
	prev := setWindowProc(hwnd, wheelHook)
	if prev == 0 {
		return false
	}
	wheelProcs[hwnd] = prev
	return true
}

// setWindowProc sets the window procedure of hwnd and returns the one it
// had, 0 if it could not.
func setWindowProc(hwnd, proc uintptr) uintptr {
	set := procSetWindowLongPtr
	if set.Find() != nil {
		set = procSetWindowLong
	}
	if set.Find() != nil || procCallWindowProc.Find() != nil {
		return 0
	}
	prev, _, _ := set.Call(hwnd, gwlpWndProc, proc)
	return prev
}

// watchWheel is the window procedure of the windows watched. It notes
// whether a wheel message says Ctrl is down while the window handles it:
// Fyne calls Scrolled from there, which asks currentKeyModifiers.
func watchWheel(hwnd, msg, wParam, lParam uintptr) uintptr {
	prev := wheelProcs[hwnd]
	switch msg {
	case wmMouseWheel:
		if wParam&mkControl != 0 {
			wheelControl = true
			defer func() { wheelControl = false }()
		}
	case wmNCDestroy:
		// The last message of a window.
		delete(wheelProcs, hwnd)
		setWindowProc(hwnd, prev)
	}
	result, _, _ := procCallWindowProc.Call(prev, hwnd, msg, wParam, lParam)
	return result
}
