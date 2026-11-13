package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/store"
)

// Spec "Retry failed entry": queued again to its original profile with
// its stored name, then uploaded.
func TestRetryFailedEntry(t *testing.T) {
	e := newEnv(t, envOpts{})
	dev := e.addProfile("dev-box", nil)
	e.addProfile("other", nil)
	os.MkdirAll(e.srv.Local("/home/alice/clipboard"), 0o755)
	e.srv.DenyCreate(true)
	e.cb.copy(image1280(t))
	r := e.settled(1)[0]
	if r.Status != store.StatusFailed || r.Attempts != 1 {
		t.Fatalf("%+v", r)
	}
	if s := e.waitFor("error badge", func(s *State) bool { return s.ErrorBadge }); s.Tray != TrayError {
		t.Errorf("tray %s", s.Tray)
	}
	e.srv.DenyCreate(false)
	// Retry goes to the original profile even when another is active.
	other := e.c.State().Profiles[1]
	call(t, func(d func(error)) { e.c.SetActiveProfile(other.ID, d) })
	e.mu.Lock()
	e.now = e.now.Add(time.Hour) // a later template render would differ
	e.mu.Unlock()
	if err := call(t, func(d func(error)) { e.c.Retry(r.ID, d) }); err != nil {
		t.Fatal(err)
	}
	got := e.settled(1)[0]
	if got.Status != store.StatusUploaded || got.ProfileID != dev.ID || got.RemoteName != r.RemoteName ||
		got.RemotePath != "/home/alice/clipboard/screenshot_20261112-153045.png" || got.Attempts != 1 {
		t.Errorf("after retry %+v", got)
	}
	// A success clears the tray's error.
	e.waitFor("badge cleared", func(s *State) bool { return !s.ErrorBadge && s.Tray == TrayMonitoring })
}

// Spec "Upload anyway": queued with the active profile's defaults, trigger
// manual.
func TestUploadAnyway(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	e.cb.copy(text("hello world"))
	r := e.settled(1)[0]
	if r.Status != store.StatusSkipped {
		t.Fatalf("%+v", r)
	}
	if err := call(t, func(d func(error)) { e.c.UploadAnyway(r.ID, d) }); err != nil {
		t.Fatal(err)
	}
	got := e.settled(1)[0]
	if got.Status != store.StatusUploaded || got.Trigger != store.TriggerManual || got.RuleName != store.RuleManual ||
		got.RemotePath != "/home/alice/clipboard/clip_20261112-153045.txt" {
		t.Errorf("%+v", got)
	}
	if b, _ := os.ReadFile(e.srv.Local(got.RemotePath)); string(b) != "hello world" {
		t.Errorf("remote %q", b)
	}
}

// Spec "Upload content a rule skipped": "Upload clipboard now" with the
// same text uploads it, with a manual trigger.
func TestCaptureNowBypassesRules(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	e.cb.copy(text("hello world"))
	e.settled(1)
	e.c.CaptureNow()
	rows := e.settled(2)
	if r := rows[0]; r.Status != store.StatusUploaded || r.Trigger != store.TriggerManual || r.RuleName != store.RuleManual {
		t.Errorf("%+v", r)
	}
}

// Spec "Copy remote path": the change is not captured.
func TestCopyRemotePath(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", func(p *store.Profile) { p.CopyBack = false })
	e.cb.copy(image1280(t))
	r := e.settled(1)[0]
	if err := call(t, func(d func(error)) { e.c.CopyRemotePath(r.ID, d) }); err != nil {
		t.Fatal(err)
	}
	if w := e.cb.writes(); len(w) != 1 || w[0] != r.RemotePath {
		t.Errorf("clipboard %q", w)
	}
	time.Sleep(60 * time.Millisecond)
	if n := len(e.entries()); n != 1 {
		t.Errorf("the copied path was captured: %d entries", n)
	}
}

// Delete removes the staged file but never the remote one.
func TestDeleteEntry(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	e.cb.copy(image1280(t))
	r := e.settled(1)[0]
	if err := call(t, func(d func(error)) { e.c.DeleteEntry(r.ID, d) }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.LocalPath); !os.IsNotExist(err) {
		t.Error("staged file kept")
	}
	if _, err := os.Stat(r.ThumbPath); !os.IsNotExist(err) {
		t.Error("thumbnail kept")
	}
	if _, err := os.Stat(e.srv.Local(r.RemotePath)); err != nil {
		t.Error("the remote file was deleted")
	}
	if len(e.entries()) != 0 {
		t.Error("entry kept")
	}
}

// Spec "Clear all": queued and uploading entries stay; remote files too.
func TestClearHistory(t *testing.T) {
	g := newGate()
	e := newEnv(t, envOpts{open: g.open})
	e.addProfile("dev-box", nil)
	e.cb.copy(image1280(t))
	done := e.settled(1)[0]
	slow := t.TempDir() + "/slow.txt"
	os.WriteFile(slow, []byte("s"), 0o644)
	e.cb.copy(files(slow))
	e.waitFor("uploading", func(s *State) bool { return s.Uploading == 1 })
	if err := call(t, func(d func(error)) { e.c.ClearHistory(d) }); err != nil {
		t.Fatal(err)
	}
	rows := e.entries()
	if len(rows) != 1 || rows[0].OriginalName != "slow.txt" {
		t.Errorf("left %+v", rows)
	}
	if _, err := os.Stat(done.LocalPath); !os.IsNotExist(err) {
		t.Error("staged file kept")
	}
	if _, err := os.Stat(e.srv.Local(done.RemotePath)); err != nil {
		t.Error("remote file deleted")
	}
	close(g.release)
	e.settled(1)
}

// Spec "Count limit".
func TestRetentionCount(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	if err := call(t, func(d func(error)) { e.c.SetSetting(store.KeyRetentionCount, 2, d) }); err != nil {
		t.Fatal(err)
	}
	var ids []int64
	var first store.HistoryEntry
	for i := range 3 {
		e.mu.Lock()
		e.now = e.now.Add(time.Minute)
		e.mu.Unlock()
		e.cb.copy(text(string(rune('a'+i)) + " text"))
		// Wait for this capture's row.
		e.waitFor("capture", func(s *State) bool {
			return len(s.History) > 0 && (len(ids) == 0 || s.History[0].ID > ids[len(ids)-1])
		})
		row := e.c.State().History[0]
		ids = append(ids, row.ID)
		if i == 0 {
			first = row
		}
	}
	rows := e.entries()
	if len(rows) != 2 || rows[0].ID != ids[2] || rows[1].ID != ids[1] {
		t.Fatalf("kept %+v, want the two newest", rows)
	}
	if _, err := os.Stat(first.LocalPath); !os.IsNotExist(err) {
		t.Error("the pruned entry's staged file was kept")
	}
}

// Spec "Orphaned staged files", and pruning by age at startup.
func TestStartupSweepAndAgePrune(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	e.cb.copy(image1280(t))
	kept := e.settled(1)[0]
	orphan := filepath.Join(e.dataDir, "staging", "2026-10", "lost.png")
	os.MkdirAll(filepath.Dir(orphan), 0o700)
	os.WriteFile(orphan, []byte("x"), 0o600)
	e.c.Close()
	e.start()
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphaned staged file kept")
	}
	if _, err := os.Stat(kept.LocalPath); err != nil {
		t.Error("a referenced staged file was deleted")
	}
	// 31 days later, at startup, the entry and its file go.
	e.c.Close()
	e.mu.Lock()
	e.now = e.now.AddDate(0, 0, 31)
	e.mu.Unlock()
	e.start()
	if n := len(e.entries()); n != 0 {
		t.Errorf("%d entries older than 30 days kept", n)
	}
	if _, err := os.Stat(kept.LocalPath); !os.IsNotExist(err) {
		t.Error("an expired entry's staged file was kept")
	}
}
