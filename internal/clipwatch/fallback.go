package clipwatch

import (
	"sync"
	"time"
)

// Fallback watches a clipboard that has no change counter, by polling a
// fingerprint of its content (its formats and a hash of its text or image)
// and numbering the changes itself. It is for running the app on systems
// other than Windows while developing.
type Fallback struct {
	fingerprint func() string
	ch          chan Change
	stop        chan struct{}
	done        chan struct{}

	mu   sync.Mutex
	seq  uint32
	last string
	once sync.Once
}

// NewFallback polls fingerprint every interval (1 s when 0). The first
// fingerprint is the baseline and is not a change.
func NewFallback(fingerprint func() string, interval time.Duration) *Fallback {
	if interval <= 0 {
		interval = time.Second
	}
	w := &Fallback{fingerprint: fingerprint, ch: make(chan Change, 1), stop: make(chan struct{}), done: make(chan struct{})}
	w.last = fingerprint()
	go w.poll(interval)
	return w
}

func (w *Fallback) poll(interval time.Duration) {
	defer close(w.done)
	defer close(w.ch)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-t.C:
		}
		fp := w.fingerprint()
		w.mu.Lock()
		changed := fp != w.last
		if changed {
			w.last = fp
			w.seq++
		}
		seq := w.seq
		w.mu.Unlock()
		if changed {
			send(w.ch, Change{Seq: seq})
		}
	}
}

// Changes implements Watcher.
func (w *Fallback) Changes() <-chan Change { return w.ch }

// Seq implements Watcher.
func (w *Fallback) Seq() uint32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq
}

// OwnedBySelf implements Watcher. The fallback cannot tell; the capture
// layer recognizes the app's own text writes instead.
func (w *Fallback) OwnedBySelf() bool { return false }

// Close implements Watcher.
func (w *Fallback) Close() {
	w.once.Do(func() { close(w.stop) })
	<-w.done
}
