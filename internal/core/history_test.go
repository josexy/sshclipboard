package core

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/store"
)

func TestHistoryPagination(t *testing.T) {
	e := newEnv(t, envOpts{})
	entries := make([]store.HistoryEntry, 501)
	for i := range entries {
		entries[i] = store.HistoryEntry{
			CaptureID: fmt.Sprint(i), CapturedAt: e.now, Kind: store.KindText, Trigger: store.TriggerAuto,
			OriginalName: fmt.Sprintf("capture-%03d.txt", i), Status: store.StatusSkipped, Action: store.ActionSkip,
		}
	}
	ids, err := e.st.History.Insert(ctx, entries, nil)
	if err != nil {
		t.Fatal(err)
	}
	query := func(f store.Filter) {
		t.Helper()
		if err := call(t, func(done func(error)) { e.c.SetHistoryFilter(f, done) }); err != nil {
			t.Fatal(err)
		}
	}
	check := func(offset, total, length int, firstID int64) {
		t.Helper()
		s := e.c.State()
		if s.Filter.Limit != HistoryPageSize || s.Filter.Offset != offset || s.HistoryTotal != total || len(s.History) != length {
			t.Fatalf("filter %+v, %d rows of %d; want offset %d, %d rows of %d", s.Filter, len(s.History), s.HistoryTotal, offset, length, total)
		}
		if length > 0 && s.History[0].ID != firstID {
			t.Fatalf("first history ID = %d, want %d", s.History[0].ID, firstID)
		}
	}
	query(store.Filter{})
	check(0, 501, 25, ids[500])
	query(store.Filter{Offset: 500})
	check(500, 501, 1, ids[0])

	if err := call(t, func(done func(error)) { e.c.SetSetting(store.KeyRetentionCount, 26, done) }); err != nil {
		t.Fatal(err)
	}
	check(25, 26, 1, ids[475])
	query(store.Filter{Search: "capture-50"})
	check(0, 1, 1, ids[500])
	query(store.Filter{Offset: 25})
	check(25, 26, 1, ids[475])
	if err := call(t, func(done func(error)) { e.c.DeleteEntry(ids[475], done) }); err != nil {
		t.Fatal(err)
	}
	check(0, 25, 25, ids[500])
	if err := call(t, e.c.ClearHistory); err != nil {
		t.Fatal(err)
	}
	check(0, 0, 0, 0)
}

func TestNewClipboardCaptureReturnsHistoryToFirstPage(t *testing.T) {
	e := newEnv(t, envOpts{})
	entries := make([]store.HistoryEntry, HistoryPageSize+1)
	for i := range entries {
		entries[i] = store.HistoryEntry{
			CaptureID: fmt.Sprint(i), CapturedAt: e.now.Add(-time.Minute), Trigger: store.TriggerAuto,
			Kind: store.KindImage, OriginalName: "image.png", Status: store.StatusSkipped, Action: store.ActionSkip,
		}
	}
	if _, err := e.st.History.Insert(ctx, entries, nil); err != nil {
		t.Fatal(err)
	}
	setPage := func(f store.Filter) {
		t.Helper()
		f.Offset = HistoryPageSize
		if err := call(t, func(done func(error)) { e.c.SetHistoryFilter(f, done) }); err != nil {
			t.Fatal(err)
		}
	}
	setPage(store.Filter{Kinds: []store.Kind{store.KindImage}})
	e.cb.copy(text("a capture hidden by the filter"))
	e.waitFor("hidden capture saved", func(*State) bool { return len(e.entries()) == len(entries)+1 })
	// Wait for a command on the loop after the capture's history refresh.
	if err := call(t, func(done func(error)) { e.c.do(func() error { return nil }, done) }); err != nil {
		t.Fatal(err)
	}
	if s := e.c.State(); s.Filter.Offset != HistoryPageSize || s.HistoryArrival != 0 {
		t.Fatalf("hidden capture moved history to offset %d, arrival %d", s.Filter.Offset, s.HistoryArrival)
	}

	setPage(store.Filter{Statuses: []store.Status{store.StatusSkipped}})
	e.cb.copy(text("a capture visible in the filter"))
	s := e.waitFor("visible capture refreshed history", func(s *State) bool { return s.HistoryTotal == len(entries)+2 })
	if s.Filter.Offset != 0 || s.HistoryArrival != 1 || s.History[0].Kind != store.KindText ||
		len(s.Filter.Statuses) != 1 || s.Filter.Statuses[0] != store.StatusSkipped {
		t.Fatalf("offset %d, arrival %d, filter %+v, first row %+v", s.Filter.Offset, s.HistoryArrival, s.Filter, s.History[0])
	}
}

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
