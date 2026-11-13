package core

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/capture"
	"github.com/josexy/sshclipboard/internal/clipwatch"
	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/store/storetest"
	"github.com/josexy/sshclipboard/internal/uploader"
)

// fakeClipboard is the system clipboard: what the source reads, what
// the watcher reports, and what the app writes.
type fakeClipboard struct {
	mu      sync.Mutex
	formats []string
	files   []string
	png     []byte
	text    string
	seq     uint32
	self    bool
	ch      chan clipwatch.Change
	written []string
}

func newClipboard() *fakeClipboard { return &fakeClipboard{ch: make(chan clipwatch.Change, 16)} }

// copy replaces the content, as another application does.
func (f *fakeClipboard) copy(fill func(*fakeClipboard)) {
	f.mu.Lock()
	f.formats, f.files, f.png, f.text = nil, nil, nil, ""
	fill(f)
	f.seq++
	f.self = false
	c := clipwatch.Change{Seq: f.seq}
	f.mu.Unlock()
	f.ch <- c
}

// Source
func (f *fakeClipboard) Formats() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.formats...)
}
func (f *fakeClipboard) Files() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files, nil
}
func (f *fakeClipboard) Image() ([]byte, error)        { f.mu.Lock(); defer f.mu.Unlock(); return f.png, nil }
func (f *fakeClipboard) Text() (string, error)         { f.mu.Lock(); defer f.mu.Unlock(); return f.text, nil }
func (f *fakeClipboard) Format(string) ([]byte, error) { return nil, nil }

// Watcher
func (f *fakeClipboard) Changes() <-chan clipwatch.Change { return f.ch }
func (f *fakeClipboard) Seq() uint32                      { f.mu.Lock(); defer f.mu.Unlock(); return f.seq }
func (f *fakeClipboard) OwnedBySelf() bool                { f.mu.Lock(); defer f.mu.Unlock(); return f.self }
func (f *fakeClipboard) Close()                           {}

// ClipboardWriter: the app's own write, which the watcher reports as Self.
func (f *fakeClipboard) WriteText(s string) {
	f.mu.Lock()
	f.formats, f.files, f.png, f.text = []string{capture.FormatText}, nil, nil, s
	f.seq++
	f.self = true
	f.written = append(f.written, s)
	c := clipwatch.Change{Seq: f.seq, Self: true}
	f.mu.Unlock()
	f.ch <- c
}

func (f *fakeClipboard) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.written...)
}

func image1280(t *testing.T) func(*fakeClipboard) {
	img := image.NewNRGBA(image.Rect(0, 0, 1280, 720))
	img.Set(3, 3, color.NRGBA{R: 200, A: 255})
	var b bytes.Buffer
	png.Encode(&b, img)
	data := b.Bytes()
	return func(f *fakeClipboard) { f.formats, f.png = []string{capture.FormatPNG}, data }
}

func text(s string) func(*fakeClipboard) {
	return func(f *fakeClipboard) { f.formats, f.text = []string{capture.FormatText}, s }
}

func files(paths ...string) func(*fakeClipboard) {
	return func(f *fakeClipboard) { f.formats, f.files = []string{capture.FormatFileList}, paths }
}

// notices records notifications.
type notices struct {
	mu sync.Mutex
	ns []Notice
}

func (n *notices) Notify(x Notice) { n.mu.Lock(); n.ns = append(n.ns, x); n.mu.Unlock() }
func (n *notices) all() []Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Notice(nil), n.ns...)
}

var at153045 = time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local)

type env struct {
	t       *testing.T
	st      *store.Store
	creds   memSecrets
	sec     *secret.Store
	srv     *sshtest.Server
	cb      *fakeClipboard
	notes   *notices
	c       *Controller
	dataDir string
	visible bool
	open    func(string) (io.ReadCloser, int64, error)
	mu      sync.Mutex
	now     time.Time
}

type envOpts struct {
	noTrust bool // don't trust the server's host key
	st      *store.Store
	srv     *sshtest.Server
	open    func(string) (io.ReadCloser, int64, error)
}

func newEnv(t *testing.T, o envOpts) *env {
	t.Helper()
	e := &env{t: t, creds: memSecrets{}, cb: newClipboard(), notes: &notices{}, dataDir: t.TempDir(), now: at153045}
	e.st = o.st
	if e.st == nil {
		e.st = storetest.Open(t)
	}
	e.srv = o.srv
	if e.srv == nil {
		e.srv = sshtest.Start(t, sshtest.Options{})
	}
	if !o.noTrust {
		if err := e.st.KnownHosts.Put(ctx, sshconn.KnownHostOf(e.srv.Host, e.srv.Port, e.srv.HostKey())); err != nil {
			t.Fatal(err)
		}
	}
	e.sec = secret.New(e.creds)
	e.open = o.open
	e.start()
	return e
}

func (e *env) start() {
	e.c = New(Options{
		Store: e.st, Secrets: e.sec, Source: e.cb, Watcher: e.cb, Clipboard: e.cb, Notifier: e.notes,
		Hooks:   Hooks{WindowVisible: func() bool { e.mu.Lock(); defer e.mu.Unlock(); return e.visible }},
		DataDir: e.dataDir,
		Uploader: uploader.Options{Backoff: []time.Duration{time.Millisecond}, Jitter: func(d time.Duration) time.Duration { return d },
			Open: e.open},
		Capture: capture.Options{Debounce: 10 * time.Millisecond},
		Now:     func() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.now },
	})
	if err := e.c.Start(); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(e.c.Close)
}

// call runs an async action and returns its error.
func call(t *testing.T, fn func(done func(error))) error {
	t.Helper()
	ch := make(chan error, 1)
	fn(func(err error) { ch <- err })
	select {
	case err := <-ch:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("action did not finish")
	}
	return nil
}

// addProfile saves a profile for the test server with password "secret"
// and returns it.
func (e *env) addProfile(name string, edit func(*store.Profile)) store.Profile {
	e.t.Helper()
	p := store.NewProfile()
	p.Name, p.Host, p.Port, p.Username = name, e.srv.Host, e.srv.Port, "alice"
	if edit != nil {
		edit(&p)
	}
	ch := make(chan SaveResult, 1)
	e.c.SaveProfile(ProfileInput{Profile: p, Password: SecretInput{Value: "secret", Set: true}}, func(r SaveResult, err error) {
		if err != nil {
			e.t.Errorf("save profile: %v", err)
		}
		ch <- r
	})
	return (<-ch).Profile
}

// waitFor polls the state until cond holds.
func (e *env) waitFor(what string, cond func(s *State) bool) *State {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := e.c.State()
		if cond(s) {
			return s
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("timed out waiting for %s; history %+v", what, s.History)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// entries returns all history rows, newest first.
func (e *env) entries() []store.HistoryEntry {
	rows, _, err := e.st.History.Query(ctx, store.Filter{})
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

// settled waits until history has n rows and none is queued or uploading.
func (e *env) settled(n int) []store.HistoryEntry {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		rows := e.entries()
		busy := false
		for _, r := range rows {
			busy = busy || r.Status.Active()
		}
		if len(rows) >= n && !busy {
			return rows
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("history did not settle: %+v", rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
