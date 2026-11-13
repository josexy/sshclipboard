package core

import (
	"strings"
	"testing"

	"github.com/josexy/sshclipboard/internal/store"
)

func TestShouldNotify(t *testing.T) {
	for _, c := range []struct {
		policy store.NotifyPolicy
		kind   NoticeKind
		want   bool
	}{
		{store.NotifyAll, NoticeUploaded, true},
		{store.NotifyAll, NoticeFailed, true},
		{store.NotifyErrors, NoticeUploaded, false},
		{store.NotifyErrors, NoticeFailed, true},
		{store.NotifyOff, NoticeUploaded, false},
		{store.NotifyOff, NoticeFailed, false},
		// Verification and close-to-tray notices are not upload outcomes.
		{store.NotifyOff, NoticeHostKey, true},
		{store.NotifyOff, NoticeStillRunning, true},
	} {
		if got := ShouldNotify(c.policy, c.kind); got != c.want {
			t.Errorf("ShouldNotify(%s, %d) = %v", c.policy, c.kind, got)
		}
	}
}

func TestDeriveTray(t *testing.T) {
	for _, c := range []struct {
		monitoring, uploading, err bool
		want                       TrayState
	}{
		{true, false, false, TrayMonitoring},
		{false, false, false, TrayPaused},
		{true, true, false, TrayUploading},
		{false, true, false, TrayUploading},
		{true, true, true, TrayError},
		{false, false, true, TrayError},
	} {
		if got := DeriveTray(c.monitoring, c.uploading, c.err); got != c.want {
			t.Errorf("DeriveTray(%v, %v, %v) = %s, want %s", c.monitoring, c.uploading, c.err, got, c.want)
		}
	}
}

// Spec "Error notification" and "Notifications off", through the
// pipeline; and "Error state": the tray shows the error until a success
// or until Activity is viewed.
func TestNotificationsAndErrorState(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	e.srv.Close() // connection refused
	e.cb.copy(image1280(t))
	r := e.settled(1)[0]
	if r.Status != store.StatusFailed || r.Attempts != 4 {
		t.Fatalf("%+v", r)
	}
	var failed *Notice
	for _, n := range e.notes.all() {
		if n.Kind == NoticeFailed {
			failed = &n
		}
	}
	if failed == nil || failed.Name != "screenshot_20261112-153045.png" || failed.Err == nil || failed.HistoryID != r.ID {
		t.Fatalf("notices %+v", e.notes.all())
	}
	if !strings.Contains(failed.Err.Error(), "connection refused") {
		t.Errorf("error %v", failed.Err)
	}
	s := e.waitFor("error tray", func(s *State) bool { return s.Tray == TrayError })
	if !s.ErrorBadge {
		t.Error("badge")
	}
	e.c.ActivityViewed()
	e.waitFor("cleared", func(s *State) bool { return s.Tray == TrayMonitoring })

	// Off: no notification, still in history and the tray.
	call(t, func(d func(error)) { e.c.SetSetting(store.KeyNotifications, store.NotifyOff, d) })
	before := len(e.notes.all())
	e.cb.copy(text("x"))
	e.c.CaptureNow()
	e.waitFor("second failure", func(s *State) bool { return s.Tray == TrayError })
	if len(e.notes.all()) != before {
		t.Errorf("notified with notifications off: %+v", e.notes.all()[before:])
	}
}
