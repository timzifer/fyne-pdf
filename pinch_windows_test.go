//go:build windows

package pdf

import (
	"runtime"
	"testing"
	"unsafe"

	"fyne.io/fyne/v2"
	"golang.org/x/sys/windows"
)

// TestWheelHook sends wheel messages to a window of its own, hooked as the
// windows of the app are.
func TestWheelHook(t *testing.T) {
	// A window gets its messages on the thread that made it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var (
		createWindow  = user32.NewProc("CreateWindowExW")
		destroyWindow = user32.NewProc("DestroyWindow")
		sendMessage   = user32.NewProc("SendMessageW")
	)
	class, err := windows.UTF16PtrFromString("STATIC")
	if err != nil {
		t.Fatal(err)
	}
	// HWND_MESSAGE (-3): a window that only takes messages.
	hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, 0, 0)
	if hwnd == 0 {
		t.Fatal("CreateWindowEx:", err)
	}
	destroyed := false
	defer func() {
		if !destroyed {
			_, _, _ = destroyWindow.Call(hwnd)
		}
	}()

	// The window procedure below stands for GLFW's: it asks for the
	// modifiers while it handles a wheel message.
	var (
		original uintptr
		wheels   int
		control  bool
	)
	original = setWindowProc(hwnd, windows.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
		if msg == wmMouseWheel {
			wheels++
			control = wheelControl && currentKeyModifiers()&fyne.KeyModifierControl != 0
		}
		result, _, _ := procCallWindowProc.Call(original, hwnd, msg, wParam, lParam)
		return result
	}))
	if original == 0 {
		t.Fatal("could not set the window procedure")
	}
	if !hookWheel(hwnd) {
		t.Fatal("could not hook the window")
	}
	// A second time changes nothing.
	if !hookWheel(hwnd) {
		t.Fatal("could not hook the window again")
	}
	if len(wheelProcs) != 1 {
		t.Fatalf("%d windows hooked, want 1", len(wheelProcs))
	}

	const notch = 120 << 16 // WHEEL_DELTA in the high word
	_, _, _ = sendMessage.Call(hwnd, wmMouseWheel, notch|mkControl, 0)
	if wheels != 1 || !control {
		t.Errorf("wheel with MK_CONTROL: %d messages, Ctrl %v, want 1, true", wheels, control)
	}
	if wheelControl {
		t.Error("Ctrl still set after the message")
	}
	_, _, _ = sendMessage.Call(hwnd, wmMouseWheel, notch, 0)
	if wheels != 2 || control {
		t.Errorf("wheel without MK_CONTROL: %d messages, Ctrl %v, want 2, false", wheels, control)
	}

	_, _, _ = destroyWindow.Call(hwnd)
	destroyed = true
	if len(wheelProcs) != 0 {
		t.Errorf("%d windows hooked after it was destroyed, want 0", len(wheelProcs))
	}
}
