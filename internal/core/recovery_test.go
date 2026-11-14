package core

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// Spec "Quit during upload": after a restart both entries are failed
// "interrupted", and Retry works.
func TestQuitDuringUpload(t *testing.T) {
	g := newGate()
	e := newEnv(t, envOpts{open: g.open})
	e.addProfile("dev-box", nil)
	dir := t.TempDir()
	a, b := dir+"/slow-a.txt", dir+"/slow-b.txt"
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(b, []byte("b"), 0o644)
	e.cb.copy(files(a, b))
	e.waitFor("uploading", func(s *State) bool { return s.Uploading == 2 })
	if e.c.InFlight() != 2 {
		t.Errorf("in flight %d", e.c.InFlight())
	}
	// Quit: the transfers are cut off, not reported.
	go func() { time.Sleep(20 * time.Millisecond); close(g.release) }()
	e.c.Close()
	for _, r := range e.entries() {
		if !r.Status.Active() {
			t.Errorf("before restart %s is %s", r.OriginalName, r.Status)
		}
	}
	e.open = nil
	e.start()
	rows := e.entries()
	for _, r := range rows {
		if r.Status != store.StatusFailed || r.Error != store.ReasonInterrupted {
			t.Errorf("after restart %+v", r)
		}
	}
	if err := call(t, func(d func(error)) { e.c.Retry(rows[0].ID, d) }); err != nil {
		t.Fatal(err)
	}
	e.waitFor("retried", func(s *State) bool {
		r, _ := e.st.History.Get(ctx, rows[0].ID)
		return r.Status == store.StatusUploaded
	})
}

// Spec "Unattended first connection": the upload waits, a notification
// asks to open the app, nothing is sent until Trust; then it resumes.
func TestFirstConnectionHiddenWindow(t *testing.T) {
	e := newEnv(t, envOpts{noTrust: true})
	e.addProfile("dev-box", nil)
	e.cb.copy(image1280(t))
	s := e.waitFor("host key prompt", func(s *State) bool { return s.HostKey != nil })
	hk := s.HostKey
	if hk.ProfileName != "dev-box" || hk.Err.Changed() || hk.Err.Fingerprint() == "" || s.Tray != TrayError {
		t.Errorf("prompt %+v tray %s", hk, s.Tray)
	}
	var shown bool
	for _, n := range e.notes.all() {
		shown = shown || (n.Kind == NoticeHostKey && n.ProfileName == "dev-box")
	}
	if !shown {
		t.Errorf("no host key notification: %+v", e.notes.all())
	}
	time.Sleep(50 * time.Millisecond)
	r := e.entries()[0]
	if r.Status != store.StatusQueued || e.srv.Written() != 0 || s.Queued != 1 {
		t.Errorf("before trust: %+v, %d bytes sent", r, e.srv.Written())
	}
	if err := call(t, func(d func(error)) { e.c.TrustHostKey(d) }); err != nil {
		t.Fatal(err)
	}
	if got := e.settled(1)[0]; got.Status != store.StatusUploaded {
		t.Errorf("after trust %+v", got)
	}
	if kh, ok, _ := e.st.KnownHosts.Get(ctx, e.srv.Host, e.srv.Port); !ok || kh.Fingerprint != hk.Err.Fingerprint() {
		t.Error("the key was not saved")
	}
	// The history row turns uploaded before the Controller publishes the
	// State without the upload, so the tray can still show uploading when
	// the prompt has closed. Wait for both.
	e.waitFor("prompt closed and tray monitoring", func(s *State) bool {
		return s.HostKey == nil && s.Tray == TrayMonitoring
	})
}

// With the window shown, the prompt opens in it; no notification.
func TestFirstConnectionVisibleWindow(t *testing.T) {
	e := newEnv(t, envOpts{noTrust: true})
	e.mu.Lock()
	e.visible = true
	e.mu.Unlock()
	e.addProfile("dev-box", nil)
	e.cb.copy(image1280(t))
	e.waitFor("host key prompt", func(s *State) bool { return s.HostKey != nil })
	for _, n := range e.notes.all() {
		if n.Kind == NoticeHostKey {
			t.Error("notified while the window is visible")
		}
	}
}

// Spec "Changed host key": uploads fail with "host key changed", and
// Replace trusts the new key.
func TestChangedHostKeyReplace(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{})
	e := newEnv(t, envOpts{srv: srv})
	e.addProfile("dev-box", nil)
	old := sshconn.KnownHostOf(srv.Host, srv.Port, srv.HostKey())
	srv.SetHostKey(sshtest.NewSigner(t))
	e.cb.copy(image1280(t))
	r := e.settled(1)[0]
	if r.Status != store.StatusFailed || r.Error != store.ReasonHostKeyChanged || r.Attempts != 1 {
		t.Fatalf("%+v", r)
	}
	s := e.waitFor("replace prompt", func(s *State) bool { return s.HostKey != nil })
	if !s.HostKey.Err.Changed() || s.HostKey.Err.Known.Fingerprint != old.Fingerprint {
		t.Errorf("prompt %+v", s.HostKey.Err)
	}
	if err := call(t, func(d func(error)) { e.c.TrustHostKey(d) }); err != nil {
		t.Fatal(err)
	}
	if err := call(t, func(d func(error)) { e.c.Retry(r.ID, d) }); err != nil {
		t.Fatal(err)
	}
	if got := e.settled(1)[0]; got.Status != store.StatusUploaded {
		t.Errorf("after replace %+v", got)
	}
}

// A test connection to an unknown host opens the same prompt.
func TestTestConnection(t *testing.T) {
	e := newEnv(t, envOpts{noTrust: true})
	p := e.addProfile("dev-box", nil)
	ch := make(chan sshconn.CheckResult, 1)
	e.c.TestConnection(ProfileInput{Profile: p}, func(r sshconn.CheckResult) { ch <- r })
	var hk *sshconn.HostKeyError
	if r := <-ch; r.Step != sshconn.StepHostKey || !errors.As(r.Err, &hk) {
		t.Fatalf("%+v", r)
	}
	e.waitFor("prompt", func(s *State) bool { return s.HostKey != nil })
	call(t, func(d func(error)) { e.c.TrustHostKey(d) })
	// The form's typed password wins over the stored one.
	e.c.TestConnection(ProfileInput{Profile: p, Password: SecretInput{Value: "wrong", Set: true}}, func(r sshconn.CheckResult) { ch <- r })
	if r := <-ch; r.Step != sshconn.StepAuth {
		t.Errorf("typed wrong password: %+v", r)
	}
	e.c.TestConnection(ProfileInput{Profile: p}, func(r sshconn.CheckResult) { ch <- r })
	if r := <-ch; !r.OK() || r.Dir != "/home/alice/clipboard" {
		t.Errorf("%+v", r)
	}
}
