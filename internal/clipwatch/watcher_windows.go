//go:build windows

package clipwatch

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procGetClipboardSequenceNum  = user32.NewProc("GetClipboardSequenceNumber")
	procGetClipboardOwner        = user32.NewProc("GetClipboardOwner")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procCreateWindowEx           = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procAddClipboardListener     = user32.NewProc("AddClipboardFormatListener")
	procRemoveClipboardListener  = user32.NewProc("RemoveClipboardFormatListener")
	procMsgWait                  = user32.NewProc("MsgWaitForMultipleObjectsEx")
	procPeekMessage              = user32.NewProc("PeekMessageW")
	procDispatchMessage          = user32.NewProc("DispatchMessageW")
)

const (
	listenerWindowName = "SSHClipboard clipboard listener"
	hwndMessage        = ^uintptr(2) // (HWND)-3
	wmQuit             = 0x0012
	wmClipboardUpdate  = 0x031D
	qsPostedAndSent    = 0x0008 | 0x0040 // QS_POSTMESSAGE | QS_SENDMESSAGE
	mwmoInputAvailable = 0x0004
	pmRemove           = 0x0001
	pollInterval       = 150 * time.Millisecond
)

// message matches the Win32 MSG layout.
type message struct {
	hwnd    uintptr
	id      uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

type windowsWatcher struct {
	ch        chan Change
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
	pid       uint32
	stopEvent windows.Handle
}

// New returns the Windows watcher and uses sequence polling if the native
// listener fails. The clipboard content fingerprint is unused on Windows.
func New(_ func() string) Watcher {
	stopEvent, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		log.Printf("clipboard watcher: CreateEvent: %v, falling back to polling", err)
	}
	w := &windowsWatcher{ch: make(chan Change, 1), stop: make(chan struct{}), done: make(chan struct{}),
		pid: uint32(os.Getpid()), stopEvent: stopEvent}
	ready := make(chan struct{})
	go w.run(sync.OnceFunc(func() { close(ready) }))
	<-ready
	return w
}

func (w *windowsWatcher) run(ready func()) {
	defer close(w.done)
	defer close(w.ch)
	if w.stopEvent != 0 {
		err := w.listen(ready)
		if err == nil {
			return
		}
		log.Printf("clipboard watcher: %v, falling back to polling", err)
	}
	w.poll(ready)
}

func (w *windowsWatcher) listen(ready func()) error {
	// The window, message queue and cleanup belong to this OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Use a system window class and consume the posted clipboard messages
	// in the loop, so the watcher needs no custom window procedure.
	class, _ := windows.UTF16PtrFromString("STATIC")
	name, _ := windows.UTF16PtrFromString(listenerWindowName)
	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(name)),
		0, 0, 0, 0, 0, hwndMessage, 0, 0, 0)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW: %w", err)
	}
	defer procDestroyWindow.Call(hwnd)
	if ok, _, err := procAddClipboardListener.Call(hwnd); ok == 0 {
		return fmt.Errorf("AddClipboardFormatListener: %w", err)
	}
	defer procRemoveClipboardListener.Call(hwnd)
	ready()

	stopEvent := w.stopEvent
	for {
		result, _, err := procMsgWait.Call(1, uintptr(unsafe.Pointer(&stopEvent)),
			windows.INFINITE, qsPostedAndSent, mwmoInputAvailable)
		switch result {
		case windows.WAIT_OBJECT_0:
			return nil
		case windows.WAIT_OBJECT_0 + 1:
			var msg message
			if ok, _, _ := procPeekMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, pmRemove); ok == 0 {
				continue // A sent message or system event can also wake the wait.
			}
			switch {
			case msg.id == wmQuit:
				return nil
			case msg.hwnd == hwnd && msg.id == wmClipboardUpdate:
				send(w.ch, Change{Seq: w.Seq(), Self: w.OwnedBySelf()})
			default:
				procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
			}
		default:
			return fmt.Errorf("MsgWaitForMultipleObjectsEx: %w", err)
		}
	}
}

func (w *windowsWatcher) poll(ready func()) {
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	last := w.Seq()
	ready()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
		}
		if seq := w.Seq(); seq != last {
			last = seq
			send(w.ch, Change{Seq: seq, Self: w.OwnedBySelf()})
		}
	}
}

func (w *windowsWatcher) Changes() <-chan Change { return w.ch }

func (w *windowsWatcher) Seq() uint32 {
	n, _, _ := procGetClipboardSequenceNum.Call()
	return uint32(n)
}

// OwnedBySelf compares the clipboard owner window's process with ours.
// MyGo writes through OleSetClipboard, whose owner window is ours.
func (w *windowsWatcher) OwnedBySelf() bool {
	hwnd, _, _ := procGetClipboardOwner.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid == w.pid
}

func (w *windowsWatcher) Close() {
	w.once.Do(func() {
		close(w.stop)
		if w.stopEvent != 0 {
			windows.SetEvent(w.stopEvent)
		}
		<-w.done
		if w.stopEvent != 0 {
			windows.CloseHandle(w.stopEvent)
		}
	})
}
