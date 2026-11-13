//go:build windows

package clipwatch

import (
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                       = syscall.NewLazyDLL("user32.dll")
	procGetClipboardSequenceNum  = user32.NewProc("GetClipboardSequenceNumber")
	procGetClipboardOwner        = user32.NewProc("GetClipboardOwner")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

// PollInterval is how often the Windows watcher reads the sequence number.
const PollInterval = 150 * time.Millisecond

// windowsWatcher polls GetClipboardSequenceNumber, which needs no window
// and no clipboard lock and costs one syscall.
type windowsWatcher struct {
	ch   chan Change
	stop chan struct{}
	done chan struct{}
	once sync.Once
	pid  uint32
}

// New returns the Windows watcher. fingerprint is unused on Windows.
func New(fingerprint func() string) Watcher {
	w := &windowsWatcher{ch: make(chan Change, 1), stop: make(chan struct{}), done: make(chan struct{}),
		pid: uint32(os.Getpid())}
	go w.poll()
	return w
}

func sequence() uint32 {
	n, _, _ := procGetClipboardSequenceNum.Call()
	return uint32(n)
}

func (w *windowsWatcher) poll() {
	defer close(w.done)
	defer close(w.ch)
	t := time.NewTicker(PollInterval)
	defer t.Stop()
	last := sequence()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
		}
		if seq := sequence(); seq != last {
			last = seq
			send(w.ch, Change{Seq: seq, Self: w.OwnedBySelf()})
		}
	}
}

func (w *windowsWatcher) Changes() <-chan Change { return w.ch }

func (w *windowsWatcher) Seq() uint32 { return sequence() }

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
	w.once.Do(func() { close(w.stop) })
	<-w.done
}
