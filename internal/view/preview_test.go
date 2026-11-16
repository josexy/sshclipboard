package view

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/josexy/sshclipboard/internal/store"
)

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadPreviewText(t *testing.T) {
	const limit = previewTextLimit
	a := func(n int) string { return strings.Repeat("a", n) }
	for _, c := range []struct {
		name      string
		data      string
		want      string
		truncated bool
	}{
		{"short", "hello\nworld", "hello\nworld", false},
		{"exactly 1 MiB", a(limit), a(limit), false},
		{"1 MiB + 1", a(limit + 1), a(limit), true},
		// é takes the bytes limit-1 and limit: it goes whole.
		{"two-byte rune across the cut", a(limit-1) + "é" + "more", a(limit - 1), true},
		{"four-byte rune across the cut", a(limit-2) + "😀" + "more", a(limit - 2), true},
		{"four-byte rune ending at the cut", a(limit-4) + "😀" + "more", a(limit-4) + "😀", true},
		{"invalid UTF-8", "ab\xffcd\xc3", "ab�cd�", false},
		{"empty", "", "", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			pc := readPreviewText(writeFile(t, "c.txt", []byte(c.data)))
			if pc.unavailable || pc.text == nil {
				t.Fatalf("unavailable: %+v", pc)
			}
			got := pc.text.String()
			if got != c.want {
				t.Errorf("text has %d bytes ending %q, want %d ending %q",
					len(got), got[max(0, len(got)-8):], len(c.want), c.want[max(0, len(c.want)-8):])
			}
			if !utf8.ValidString(got) {
				t.Error("text is not valid UTF-8")
			}
			if pc.truncated != c.truncated {
				t.Errorf("truncated = %v, want %v", pc.truncated, c.truncated)
			}
			if pc.size != int64(len(c.data)) {
				t.Errorf("size = %d, want %d", pc.size, len(c.data))
			}
			if pc.kind != store.KindText {
				t.Errorf("kind = %v", pc.kind)
			}
		})
	}
	t.Run("missing", func(t *testing.T) {
		if pc := readPreviewText(filepath.Join(t.TempDir(), "gone.txt")); !pc.unavailable || pc.text != nil {
			t.Errorf("missing file: %+v", pc)
		}
	})
	t.Run("never staged", func(t *testing.T) {
		if pc := readPreviewText(""); !pc.unavailable {
			t.Errorf("no path: %+v", pc)
		}
	})
	t.Run("a folder", func(t *testing.T) {
		if pc := readPreviewText(t.TempDir()); !pc.unavailable {
			t.Errorf("folder: %+v", pc)
		}
	})
}

// writePNG writes a black w×h grayscale PNG, which encodes quickly.
func writePNG(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, w, h))
	p := filepath.Join(t.TempDir(), "i.png")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadPreviewImage(t *testing.T) {
	for _, c := range []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{"small kept", 64, 64, 64, 64},
		{"at the limit kept", previewMaxSide, 16, previewMaxSide, 16},
		{"wide scaled", 8000, 2000, 4096, 1024},
		{"tall scaled", 1000, 5000, 819, 4096},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := writePNG(t, c.w, c.h)
			pc := readPreviewImage(p)
			if pc.unavailable || pc.bitmap == nil {
				t.Fatalf("unavailable: %+v", pc)
			}
			if w, h := pc.bitmap.Size(); w != c.wantW || h != c.wantH {
				t.Errorf("bitmap %d×%d, want %d×%d", w, h, c.wantW, c.wantH)
			}
			if pc.width != c.w || pc.height != c.h {
				t.Errorf("original size %d×%d, want %d×%d", pc.width, pc.height, c.w, c.h)
			}
			st, _ := os.Stat(p)
			if pc.size != st.Size() || pc.kind != store.KindImage {
				t.Errorf("size %d kind %v, want %d image", pc.size, pc.kind, st.Size())
			}
		})
	}
	t.Run("corrupt", func(t *testing.T) {
		data, err := os.ReadFile(writePNG(t, 64, 64))
		if err != nil {
			t.Fatal(err)
		}
		for name, bad := range map[string][]byte{
			"not a PNG": []byte("plain text, not a PNG"),
			"cut short": data[:len(data)/2],
		} {
			if pc := readPreviewImage(writeFile(t, "bad.png", bad)); !pc.unavailable || pc.bitmap != nil {
				t.Errorf("%s: %+v", name, pc)
			}
		}
	})
	t.Run("missing", func(t *testing.T) {
		if pc := readPreviewImage(filepath.Join(t.TempDir(), "gone.png")); !pc.unavailable {
			t.Errorf("missing file: %+v", pc)
		}
	})
}

func TestScaleToSide(t *testing.T) {
	for _, c := range []struct{ w, h, wantW, wantH int }{
		{8000, 2000, 4096, 1024},
		{2000, 8000, 1024, 4096},
		{8192, 8192, 4096, 4096},
		{100000, 1, 4096, 1},
	} {
		if w, h := scaleToSide(c.w, c.h, 4096); w != c.wantW || h != c.wantH {
			t.Errorf("scaleToSide(%d, %d) = %d×%d, want %d×%d", c.w, c.h, w, h, c.wantW, c.wantH)
		}
	}
}

// stubLoader is a previewLoader whose reads wait until the test releases
// them, and whose redraws queue for the test to run, as Window.Update
// would on the UI thread.
type stubLoader struct {
	*previewLoader
	reads   atomic.Int32
	release map[int64]chan struct{}
	redraws chan func()
}

func newStubLoader(ids ...int64) *stubLoader {
	s := &stubLoader{release: map[int64]chan struct{}{}, redraws: make(chan func(), 16)}
	for _, id := range ids {
		s.release[id] = make(chan struct{})
	}
	s.previewLoader = newPreviewLoader(func(fn func()) { s.redraws <- fn })
	s.read = func(e store.HistoryEntry) previewContent {
		s.reads.Add(1)
		<-s.release[e.ID]
		return previewContent{kind: e.Kind, size: e.ID}
	}
	return s
}

// finish lets id's read end and runs its redraw.
func (s *stubLoader) finish(t *testing.T, id int64) {
	t.Helper()
	close(s.release[id])
	select {
	case fn := <-s.redraws:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatalf("read of %d never redrew", id)
	}
}

func entry(id int64) store.HistoryEntry {
	return store.HistoryEntry{ID: id, Kind: store.KindText, LocalPath: "/staging/" + string(rune('a'+id)) + ".txt"}
}

func TestPreviewLoader(t *testing.T) {
	t.Run("result after redraw", func(t *testing.T) {
		s := newStubLoader(1)
		if _, ok := s.get(entry(1)); ok {
			t.Fatal("content ready before it was read")
		}
		s.finish(t, 1)
		pc, ok := s.get(entry(1))
		if !ok || pc.size != 1 {
			t.Fatalf("get = %+v, %v after redraw", pc, ok)
		}
		if n := s.reads.Load(); n != 1 {
			t.Errorf("%d reads", n)
		}
	})
	t.Run("one read per key", func(t *testing.T) {
		s := newStubLoader(1)
		for range 5 {
			s.get(entry(1))
		}
		s.finish(t, 1)
		s.get(entry(1))
		if n := s.reads.Load(); n != 1 {
			t.Errorf("%d reads, want 1", n)
		}
	})
	t.Run("stale result dropped", func(t *testing.T) {
		s := newStubLoader(1, 2)
		s.get(entry(1))
		s.get(entry(2))
		s.finish(t, 1) // A's result arrives late
		if pc, ok := s.get(entry(2)); ok {
			t.Fatalf("B shows A's result %+v", pc)
		}
		if s.has || s.content != (previewContent{}) {
			t.Fatalf("A's result kept: %+v", s.content)
		}
		s.finish(t, 2)
		if pc, ok := s.get(entry(2)); !ok || pc.size != 2 {
			t.Fatalf("get(B) = %+v, %v", pc, ok)
		}
	})
	t.Run("new result dropped once another is wanted", func(t *testing.T) {
		s := newStubLoader(1, 2)
		s.get(entry(1))
		s.get(entry(2))
		s.finish(t, 2)
		s.finish(t, 1)
		if pc, ok := s.get(entry(2)); !ok || pc.size != 2 {
			t.Fatalf("get(B) = %+v, %v", pc, ok)
		}
	})
	t.Run("a moved file is another key", func(t *testing.T) {
		s := newStubLoader(1)
		s.get(entry(1))
		s.finish(t, 1)
		moved := entry(1)
		moved.LocalPath = "/elsewhere/b.txt"
		s.release[1] = make(chan struct{})
		if _, ok := s.get(moved); ok {
			t.Fatal("moved file shows the old file's content")
		}
		s.finish(t, 1)
		if _, ok := s.get(moved); !ok || s.reads.Load() != 2 {
			t.Fatalf("moved file not read again: %d reads", s.reads.Load())
		}
	})
	t.Run("reset", func(t *testing.T) {
		s := newStubLoader(1)
		s.get(entry(1))
		s.finish(t, 1)
		s.reset()
		if s.has || s.content != (previewContent{}) {
			t.Fatalf("reset kept %+v", s.content)
		}
		s.release[1] = make(chan struct{})
		if _, ok := s.get(entry(1)); ok {
			t.Fatal("content after reset")
		}
		s.finish(t, 1)
		if _, ok := s.get(entry(1)); !ok || s.reads.Load() != 2 {
			t.Fatalf("not read again after reset: %d reads", s.reads.Load())
		}
	})
	t.Run("result after reset dropped", func(t *testing.T) {
		s := newStubLoader(1)
		s.get(entry(1))
		s.reset()
		s.finish(t, 1)
		if s.has {
			t.Fatal("result kept after reset")
		}
	})
	t.Run("no redraw reads nothing", func(t *testing.T) {
		l := newPreviewLoader(nil)
		l.read = func(store.HistoryEntry) previewContent { panic("read without redraw") }
		if _, ok := l.get(entry(1)); ok {
			t.Fatal("content without a read")
		}
	})
}
