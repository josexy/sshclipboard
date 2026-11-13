package view

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/clipwatch"
	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/store/storetest"
)

type memSecrets map[string][]byte

func (m memSecrets) Set(k string, v []byte) error { m[k] = v; return nil }
func (m memSecrets) Get(k string) ([]byte, error) {
	if v, ok := m[k]; ok {
		return v, nil
	}
	return nil, mygo.ErrSecretNotFound
}
func (m memSecrets) Delete(k string) error { delete(m, k); return nil }

// emptyClipboard is a clipboard that never changes.
type emptyClipboard struct{ ch chan clipwatch.Change }

func (emptyClipboard) Formats() []string                  { return nil }
func (emptyClipboard) Files() ([]string, error)           { return nil, nil }
func (emptyClipboard) Image() ([]byte, error)             { return nil, nil }
func (emptyClipboard) Text() (string, error)              { return "", nil }
func (emptyClipboard) Format(string) ([]byte, error)      { return nil, nil }
func (e emptyClipboard) Changes() <-chan clipwatch.Change { return e.ch }
func (emptyClipboard) Seq() uint32                        { return 0 }
func (emptyClipboard) OwnedBySelf() bool                  { return false }
func (emptyClipboard) Close()                             {}

// realEnv shows the app over a real Controller and database. The
// Controller's notify queues functions for the test's thread, which
// settle runs before each frame, as Window.Update runs them on the main
// thread.
type realEnv struct {
	t         *testing.T
	st        *store.Store
	ctrl      *core.Controller
	tt        *ui.Tester
	mu        sync.Mutex
	queue     []func()
	hooks     []string
	hotkeyErr error
	picked    string
}

func newReal(t *testing.T, height int) *realEnv {
	t.Helper()
	e := &realEnv{t: t, st: storetest.Open(t)}
	notify := func(fn func()) { e.mu.Lock(); e.queue = append(e.queue, fn); e.mu.Unlock() }
	e.ctrl = core.New(core.Options{
		Store: e.st, Secrets: secret.New(memSecrets{}), Source: emptyClipboard{}, Watcher: emptyClipboard{ch: make(chan clipwatch.Change)},
		Notify: notify, DataDir: t.TempDir(),
		Hooks: core.Hooks{
			Appearance:   func(a store.Appearance) { e.hook("appearance %s", a) },
			StartAtLogin: func(on bool) error { e.hook("login %v", on); return nil },
			Hotkey: func(prev, next string) error {
				e.hook("hotkey %q→%q", prev, next)
				return e.hotkeyErr
			},
		},
	})
	if err := e.ctrl.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.ctrl.Close)
	app := NewApp(e.ctrl, Platform{Version: "0.1.0", DefaultStaging: "/data/staging",
		PickFolder: func(done func(string)) { done(e.picked) }}, notify)
	e.tt = ui.NewTester(app.View, 1180, height)
	return e
}

func (e *realEnv) hook(format string, args ...any) {
	e.mu.Lock()
	e.hooks = append(e.hooks, sprintf(format, args...))
	e.mu.Unlock()
}

// settle runs queued UI work and frames until cond holds.
func (e *realEnv) settle(what string, cond func(s store.Settings) bool) {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.mu.Lock()
		q := e.queue
		e.queue = nil
		e.mu.Unlock()
		for _, fn := range q {
			fn()
		}
		e.tt.Frame()
		s, err := e.st.Settings.Load(ctx)
		if err == nil && cond(s) {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("%s did not persist: %+v", what, s)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

var errTaken = errors.New("taken")
