package capture

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/clipwatch"
	"github.com/josexy/sshclipboard/internal/store"
)

// fakeSource is a clipboard tests fill.
type fakeSource struct {
	mu      sync.Mutex
	formats []string
	files   []string
	png     []byte
	pngErr  error
	text    string
	textErr error
	raw     map[string][]byte
}

func (f *fakeSource) set(fn func(*fakeSource)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.formats, f.files, f.png, f.pngErr, f.text, f.textErr, f.raw = nil, nil, nil, nil, "", nil, nil
	fn(f)
}

func (f *fakeSource) Formats() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.formats...)
}
func (f *fakeSource) Files() ([]string, error) { f.mu.Lock(); defer f.mu.Unlock(); return f.files, nil }
func (f *fakeSource) Image() ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.png, f.pngErr
}
func (f *fakeSource) Text() (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.text, f.textErr
}
func (f *fakeSource) Format(n string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.raw[n], nil
}

// fakeWatcher delivers changes tests send.
type fakeWatcher struct {
	ch   chan clipwatch.Change
	mu   sync.Mutex
	seq  uint32
	self bool
}

func newWatcher() *fakeWatcher { return &fakeWatcher{ch: make(chan clipwatch.Change, 16)} }

func (w *fakeWatcher) change(self bool) {
	w.mu.Lock()
	w.seq++
	w.self = self
	c := clipwatch.Change{Seq: w.seq, Self: self}
	w.mu.Unlock()
	w.ch <- c
}
func (w *fakeWatcher) Changes() <-chan clipwatch.Change { return w.ch }
func (w *fakeWatcher) Seq() uint32                      { w.mu.Lock(); defer w.mu.Unlock(); return w.seq }
func (w *fakeWatcher) OwnedBySelf() bool                { w.mu.Lock(); defer w.mu.Unlock(); return w.self }
func (w *fakeWatcher) Close()                           {}

type harness struct {
	src     *fakeSource
	w       *fakeWatcher
	c       *Capturer
	items   chan Item
	staging string
	mu      sync.Mutex
	now     time.Time
	mode    store.CaptureMode
	conf    bool
}

func newHarness(t *testing.T, debounce time.Duration) *harness {
	h := &harness{src: &fakeSource{}, w: newWatcher(), items: make(chan Item, 16), staging: t.TempDir(),
		now: time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local), mode: store.CaptureAuto, conf: true}
	h.c = New(Options{
		Source: h.src, Watcher: h.w, StagingDir: func() string { return h.staging },
		Mode:         func() store.CaptureMode { h.mu.Lock(); defer h.mu.Unlock(); return h.mode },
		Confidential: func() bool { h.mu.Lock(); defer h.mu.Unlock(); return h.conf },
		OnItem:       func(it Item) { h.items <- it },
		OnError:      func(err error) { t.Errorf("capture error: %v", err) },
		Debounce:     debounce,
		Now:          func() time.Time { h.mu.Lock(); defer h.mu.Unlock(); return h.now },
	})
	go h.c.Run()
	t.Cleanup(h.c.Close)
	return h
}

func (h *harness) advance(d time.Duration) { h.mu.Lock(); h.now = h.now.Add(d); h.mu.Unlock() }

func (h *harness) item(t *testing.T) Item {
	t.Helper()
	select {
	case it := <-h.items:
		return it
	case <-time.After(2 * time.Second):
		t.Fatal("nothing captured")
	}
	return Item{}
}

func (h *harness) none(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case it := <-h.items:
		t.Fatalf("captured %+v", it)
	case <-time.After(wait):
	}
}

func pngOf(t *testing.T, w, hgt int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, hgt))
	for x := range w {
		img.Set(x, x%hgt, color.NRGBA{R: uint8(x), A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func copyImage(png []byte) func(*fakeSource) {
	return func(f *fakeSource) {
		f.formats, f.png = []string{FormatPNG, "text/html", FormatText}, png
		f.text = "alt"
	}
}

func copyText(s string) func(*fakeSource) {
	return func(f *fakeSource) { f.formats, f.text = []string{FormatText}, s }
}

// 8.2

func TestDefaults(t *testing.T) {
	c := New(Options{Watcher: newWatcher()})
	if c.opts.Debounce != 300*time.Millisecond || c.opts.DedupeWindow != 5*time.Second {
		t.Errorf("debounce %v dedupe %v", c.opts.Debounce, c.opts.DedupeWindow)
	}
}

// Spec "Screenshot detected": within one second, with the default
// debounce.
func TestDetectWithinOneSecond(t *testing.T) {
	h := newHarness(t, 0)
	h.src.set(copyImage(pngOf(t, 64, 32)))
	start := time.Now()
	h.w.change(false)
	it := h.item(t)
	if time.Since(start) > time.Second || it.Kind != store.KindImage || it.Trigger != store.TriggerAuto {
		t.Errorf("%+v after %v", it, time.Since(start))
	}
}

// Spec "Application writes several formats".
func TestDebounceBurst(t *testing.T) {
	h := newHarness(t, 60*time.Millisecond)
	h.src.set(copyImage(pngOf(t, 10, 10)))
	for range 3 {
		h.w.change(false)
		time.Sleep(30 * time.Millisecond)
	}
	h.item(t)
	h.none(t, 200*time.Millisecond)
}

func TestClassification(t *testing.T) {
	dir := t.TempDir()
	report := filepath.Join(dir, "report.pdf")
	chart := filepath.Join(dir, "chart.png")
	os.WriteFile(report, []byte("pdf"), 0o644)
	os.WriteFile(chart, pngOf(t, 4, 4), 0o644)

	h := newHarness(t, 10*time.Millisecond)
	// Spec "Files copied in Explorer": one files item, no image.
	h.src.set(func(f *fakeSource) {
		f.formats, f.files, f.png = []string{FormatFileList, FormatPNG}, []string{report, chart}, pngOf(t, 4, 4)
	})
	h.w.change(false)
	it := h.item(t)
	if it.Kind != store.KindFile || len(it.Entries) != 2 || it.Entries[0].FileName != "report.pdf" ||
		it.Entries[1].LocalPath != chart || it.Entries[0].Staged || it.Entries[0].Size != 3 {
		t.Errorf("files item %+v", it)
	}
	// Spec "Image copied from a browser".
	h.src.set(copyImage(pngOf(t, 8, 8)))
	h.w.change(false)
	if it := h.item(t); it.Kind != store.KindImage || len(it.Entries) != 1 {
		t.Errorf("image item %+v", it)
	}
	// Spec "Plain text".
	h.src.set(copyText("hello world"))
	h.w.change(false)
	if it := h.item(t); it.Kind != store.KindText || it.Entries[0].Text != "hello world" || it.Entries[0].FileName != "text.txt" {
		t.Errorf("text item %+v", it)
	}
	// Whitespace-only text and empty clipboards are ignored.
	h.src.set(copyText(" \n\t "))
	h.w.change(false)
	h.src.set(func(f *fakeSource) { f.formats = []string{"application/x-unknown"} })
	h.w.change(false)
	h.none(t, 100*time.Millisecond)
}

// Spec "Mixed files and folder".
func TestFoldersSkipped(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")
	assets := filepath.Join(dir, "assets")
	os.WriteFile(notes, []byte("n"), 0o644)
	os.Mkdir(assets, 0o755)
	h := newHarness(t, 10*time.Millisecond)
	h.src.set(func(f *fakeSource) { f.formats, f.files = []string{FormatFileList}, []string{notes, assets} })
	h.w.change(false)
	it := h.item(t)
	if len(it.Entries) != 1 || it.Entries[0].FileName != "notes.txt" || len(it.Skipped) != 1 ||
		it.Skipped[0].FileName != "assets" || it.Skipped[0].Reason != store.ReasonFolder {
		t.Errorf("%+v", it)
	}
	// Only folders: the item is all skipped.
	h.src.set(func(f *fakeSource) { f.formats, f.files = []string{FormatFileList}, []string{assets} })
	h.w.change(false)
	if it := h.item(t); it.Kind != store.KindFile || len(it.Entries) != 0 || len(it.Skipped) != 1 {
		t.Errorf("%+v", it)
	}
}

// Spec "Password manager copy".
func TestConfidentialContentSkipped(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	dword := func(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
	for _, mark := range []func(*fakeSource){
		func(f *fakeSource) { f.formats = append(f.formats, FormatExcludeFromMonitor) },
		func(f *fakeSource) { f.formats = append(f.formats, FormatViewerIgnore) },
		func(f *fakeSource) {
			f.formats = append(f.formats, FormatCanIncludeHistory)
			f.raw = map[string][]byte{FormatCanIncludeHistory: dword(0)}
		},
	} {
		h.src.set(func(f *fakeSource) { copyText("hunter2")(f); mark(f) })
		h.w.change(false)
		h.none(t, 60*time.Millisecond)
	}
	// Allowed in history: captured.
	h.src.set(func(f *fakeSource) {
		copyText("public")(f)
		f.formats = append(f.formats, FormatCanIncludeHistory)
		f.raw = map[string][]byte{FormatCanIncludeHistory: dword(1)}
	})
	h.w.change(false)
	h.item(t)
	// The setting turns the protection off.
	h.mu.Lock()
	h.conf = false
	h.mu.Unlock()
	h.src.set(func(f *fakeSource) { copyText("hunter2")(f); f.formats = append(f.formats, FormatExcludeFromMonitor) })
	h.w.change(false)
	h.item(t)
}

// Spec "Remote path written back".
func TestOwnWritesIgnored(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	h.src.set(copyText("/home/alice/clipboard/shot.png"))
	h.w.change(true) // Windows: the owner is this process
	h.none(t, 60*time.Millisecond)

	// Where the owner is unknown, the app's own text is recognized.
	h.c.NoteSelfWrite("/home/alice/clipboard/a.png")
	h.src.set(copyText("/home/alice/clipboard/a.png"))
	h.w.change(false)
	h.none(t, 60*time.Millisecond)
	// …but not for long.
	h.advance(3 * time.Second)
	h.w.change(false)
	h.item(t)
}

// 8.3

// Spec "Image staged".
func TestImageStaged(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	data := pngOf(t, 1280, 720)
	h.src.set(copyImage(data))
	h.w.change(false)
	it := h.item(t)
	e := it.Entries[0]
	sum := sha256.Sum256(data)
	if e.Width != 1280 || e.Height != 720 || e.Size != int64(len(data)) || e.SHA256 != hex.EncodeToString(sum[:]) || !e.Staged {
		t.Errorf("entry %+v", e)
	}
	if filepath.Dir(e.LocalPath) != filepath.Join(h.staging, "2026-11") || filepath.Base(e.LocalPath) != it.CaptureID+".png" {
		t.Errorf("staged at %s", e.LocalPath)
	}
	if b, err := os.ReadFile(e.LocalPath); err != nil || !bytes.Equal(b, data) {
		t.Errorf("staged file: %v", err)
	}
	if fi, _ := os.Stat(e.LocalPath); fi.Mode().Perm() != 0o600 {
		t.Errorf("staged file mode %v", fi.Mode())
	}
	f, err := os.Open(e.ThumbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Height != ThumbHeight || cfg.Width != 170 {
		t.Errorf("thumbnail %dx%d %v", cfg.Width, cfg.Height, err)
	}
	if it.CapturedAt.IsZero() || len(it.CaptureID) != 21 || it.Seq != 1 {
		t.Errorf("item %+v", it)
	}
}

func TestTextStaged(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	h.src.set(copyText("héllo"))
	h.w.change(false)
	e := h.item(t).Entries[0]
	if b, _ := os.ReadFile(e.LocalPath); string(b) != "héllo" || !strings.HasSuffix(e.LocalPath, ".txt") || e.Size != 6 {
		t.Errorf("text entry %+v", e)
	}
}

// Spec "Huge bitmap": skipped with "content too large", nothing staged.
func TestTooLargeNotStaged(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	h.src.set(func(f *fakeSource) { f.formats, f.pngErr = []string{FormatPNG}, ErrTooLarge })
	h.w.change(false)
	it := h.item(t)
	if it.Kind != store.KindImage || len(it.Entries) != 0 || len(it.Skipped) != 1 || it.Skipped[0].Reason != store.ReasonTooLarge {
		t.Errorf("%+v", it)
	}
	if entries, _ := os.ReadDir(h.staging); len(entries) != 0 {
		t.Errorf("staged %v", entries)
	}
	h.src.set(func(f *fakeSource) { f.formats, f.textErr = []string{FormatText}, ErrTooLarge })
	h.w.change(false)
	if it := h.item(t); it.Kind != store.KindText || it.Skipped[0].Reason != store.ReasonTooLarge {
		t.Errorf("%+v", it)
	}
}

// Spec "Large file is not limited".
func TestLargeFileNotLimited(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.iso")
	f, _ := os.Create(p)
	f.Truncate(500 << 20) // sparse
	f.Close()
	h := newHarness(t, 10*time.Millisecond)
	h.src.set(func(f *fakeSource) { f.formats, f.files = []string{FormatFileList}, []string{p} })
	h.w.change(false)
	if it := h.item(t); len(it.Entries) != 1 || it.Entries[0].Size != 500<<20 {
		t.Errorf("%+v", it)
	}
}

// Specs "Same screenshot rewritten" and "Same content later".
func TestDuplicates(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	data := pngOf(t, 20, 20)
	h.src.set(copyImage(data))
	h.w.change(false)
	first := h.item(t)
	h.advance(time.Second)
	h.w.change(false)
	h.none(t, 60*time.Millisecond)
	// The duplicate's staged files are not left behind.
	month, _ := os.ReadDir(filepath.Join(h.staging, "2026-11"))
	if len(month) != 2 { // first image and its thumbnail
		t.Errorf("staging has %d files", len(month))
	}
	h.advance(10 * time.Second)
	h.w.change(false)
	if again := h.item(t); again.CaptureID == first.CaptureID {
		t.Error("same capture id")
	}
	// Different kind, same moment: not a duplicate.
	h.src.set(copyText("x"))
	h.w.change(false)
	h.item(t)
}

// 8.4

// Spec "Manual mode ignores changes".
func TestManualMode(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	h.mu.Lock()
	h.mode = store.CaptureManual
	h.mu.Unlock()
	h.src.set(copyImage(pngOf(t, 5, 5)))
	h.w.change(false)
	h.none(t, 60*time.Millisecond)
	it, ok := h.c.CaptureNow()
	if !ok || it.Trigger != store.TriggerManual || it.Kind != store.KindImage {
		t.Errorf("%+v %v", it, ok)
	}
	h.item(t) // OnItem got it too
}

// Spec "Upload content a rule skipped": manual capture takes the same
// content again, without duplicate suppression, in automatic mode too.
func TestCaptureNowBypassesDedupe(t *testing.T) {
	h := newHarness(t, 10*time.Millisecond)
	h.src.set(copyText("same"))
	h.w.change(false)
	h.item(t)
	if it, ok := h.c.CaptureNow(); !ok || it.Trigger != store.TriggerManual {
		t.Errorf("%+v %v", it, ok)
	}
	h.item(t)
	h.src.set(copyText("  "))
	if _, ok := h.c.CaptureNow(); ok {
		t.Error("captured an empty clipboard")
	}
}

// Spec "Monitoring paused".
func TestPauseResume(t *testing.T) {
	h := newHarness(t, 30*time.Millisecond)
	h.c.SetPaused(true)
	h.src.set(copyText("copied while paused"))
	h.w.change(false)
	h.none(t, 80*time.Millisecond)
	h.c.SetPaused(false)
	h.none(t, 80*time.Millisecond) // resuming captures nothing
	h.src.set(copyText("after resume"))
	h.w.change(false)
	if it := h.item(t); it.Entries[0].Text != "after resume" {
		t.Errorf("%+v", it)
	}
	// Pausing during a burst drops it.
	h.src.set(copyText("burst"))
	h.w.change(false)
	h.c.SetPaused(true)
	h.none(t, 80*time.Millisecond)
	if !h.c.Paused() {
		t.Error("Paused")
	}
	// "Upload clipboard now" works while paused.
	if _, ok := h.c.CaptureNow(); !ok {
		t.Error("CaptureNow while paused")
	}
}
