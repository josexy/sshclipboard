package clipwatch

import (
	"sync"
	"testing"
	"time"
)

// fakeClipboard is a clipboard whose content tests set.
type fakeClipboard struct {
	mu      sync.Mutex
	content string
}

func (f *fakeClipboard) set(s string) { f.mu.Lock(); f.content = s; f.mu.Unlock() }
func (f *fakeClipboard) fingerprint() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.content
}

func next(t *testing.T, w Watcher) Change {
	t.Helper()
	select {
	case c, ok := <-w.Changes():
		if !ok {
			t.Fatal("changes closed")
		}
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("no change reported")
	}
	return Change{}
}

func TestFallbackReportsChanges(t *testing.T) {
	cb := &fakeClipboard{content: "a"}
	var w Watcher = NewFallback(cb.fingerprint, 5*time.Millisecond)
	defer w.Close()
	if w.Seq() != 0 {
		t.Errorf("initial content counted as a change: %d", w.Seq())
	}
	// No change, no event.
	select {
	case c := <-w.Changes():
		t.Fatalf("spurious change %+v", c)
	case <-time.After(30 * time.Millisecond):
	}
	cb.set("b")
	if c := next(t, w); c.Seq != 1 || c.Self {
		t.Errorf("change %+v", c)
	}
	cb.set("c")
	if c := next(t, w); c.Seq != 2 || w.Seq() != 2 {
		t.Errorf("change %+v seq %d", c, w.Seq())
	}
	if w.OwnedBySelf() {
		t.Error("the fallback cannot know the owner")
	}
}

func TestFallbackCoalescesUnreadChanges(t *testing.T) {
	cb := &fakeClipboard{content: "a"}
	w := NewFallback(cb.fingerprint, 2*time.Millisecond)
	defer w.Close()
	for _, s := range []string{"b", "c", "d"} {
		cb.set(s)
		time.Sleep(20 * time.Millisecond)
	}
	// Only the latest undelivered change is kept.
	if c := next(t, w); c.Seq != 3 {
		t.Errorf("got %+v, want the latest (3)", c)
	}
}

func TestFallbackClose(t *testing.T) {
	w := NewFallback(func() string { return "" }, time.Millisecond)
	w.Close()
	w.Close() // idempotent
	if _, ok := <-w.Changes(); ok {
		t.Error("changes not closed")
	}
}

func TestNewOnThisSystem(t *testing.T) {
	w := New(func() string { return "x" })
	defer w.Close()
	if w.Changes() == nil {
		t.Error("no change channel")
	}
}
