//go:build windows

package clipwatch

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowsWatcherReceivesClipboardMessage(t *testing.T) {
	w := New(nil)
	t.Cleanup(w.Close)

	findWindow := user32.NewProc("FindWindowExW")
	name, _ := windows.UTF16PtrFromString(listenerWindowName)
	var hwnd uintptr
	for {
		found, _, err := findWindow.Call(hwndMessage, hwnd, 0, uintptr(unsafe.Pointer(name)))
		if found == 0 {
			t.Fatalf("finding the listener window: %v", err)
		}
		hwnd = found
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == uint32(os.Getpid()) {
			break
		}
	}
	// Exercise the native message loop without changing the user's clipboard.
	postMessage := user32.NewProc("PostMessageW")
	if ok, _, err := postMessage.Call(hwnd, wmClipboardUpdate, 0, 0); ok == 0 {
		t.Fatalf("posting WM_CLIPBOARDUPDATE: %v", err)
	}
	next(t, w)

	w.Close()
	if alive, _, _ := user32.NewProc("IsWindow").Call(hwnd); alive != 0 {
		t.Error("listener window still exists after Close")
	}
}
