package core

import (
	"os"
	"testing"

	"github.com/josexy/sshclipboard/internal/store"
)

// Task 9.1: a captured image ends as uploaded, at the expected remote
// path, and the path is copied back.
func TestCapturedImageUploads(t *testing.T) {
	e := newEnv(t, envOpts{})
	p := e.addProfile("dev-box", nil)
	if s := e.c.State(); s.Active == nil || s.Active.ID != p.ID {
		t.Fatalf("the first profile is not active: %+v", s.Active)
	}
	e.cb.copy(image1280(t))
	rows := e.settled(1)
	r := rows[0]
	want := "/home/alice/clipboard/screenshot_20261112-153045.png"
	if r.Status != store.StatusUploaded || r.RemotePath != want || r.Kind != store.KindImage ||
		r.Width != 1280 || r.Height != 720 || r.ProfileName != "dev-box" || r.RuleName != store.RuleDefault ||
		r.Trigger != store.TriggerAuto || r.Attempts != 1 || r.SHA256 == "" || !r.Staged {
		t.Fatalf("history %+v", r)
	}
	if b, err := os.ReadFile(e.srv.Local(want)); err != nil || len(b) != int(r.Size) {
		t.Errorf("remote file: %v", err)
	}
	// Spec "Screenshot path copied back": exactly the path.
	e.waitFor("copy-back", func(*State) bool { return len(e.cb.writes()) == 1 })
	if got := e.cb.writes()[0]; got != want {
		t.Errorf("clipboard %q", got)
	}
	// The app's own write is not captured.
	s := e.waitFor("summary", func(s *State) bool { return s.Summary.Uploaded == 1 })
	if len(e.entries()) != 1 || s.Tray != TrayMonitoring || s.Queued+s.Uploading != 0 {
		t.Errorf("after copy-back: %d entries, tray %s", len(e.entries()), s.Tray)
	}
}

// capture-rules spec "Fresh install" and upload-history "Skipped entry
// recorded".
func TestDefaultRulesSkipText(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	e.cb.copy(text("hello world"))
	rows := e.settled(1)
	if r := rows[0]; r.Status != store.StatusSkipped || r.Kind != store.KindText || r.RuleName != "Skip text" {
		t.Errorf("%+v", r)
	}
	if len(e.cb.writes()) != 0 {
		t.Error("a skipped capture was copied back")
	}
}

// capture-rules spec "No rule matches" with default skip, and folders.
func TestSkipReasons(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	if err := call(t, func(d func(error)) { e.c.SetDefaultAction(store.ActionSkip, d) }); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	notes := dir + "/notes.txt"
	os.WriteFile(notes, []byte("n"), 0o644)
	os.Mkdir(dir+"/assets", 0o755)
	e.cb.copy(files(notes, dir+"/assets"))
	rows := e.settled(2)
	byName := map[string]store.HistoryEntry{}
	for _, r := range rows {
		byName[r.OriginalName] = r
	}
	if r := byName["notes.txt"]; r.Status != store.StatusSkipped || r.RuleName != store.RuleDefault || r.Error != store.ReasonNoMatchingRule {
		t.Errorf("notes %+v", r)
	}
	if r := byName["assets"]; r.Status != store.StatusSkipped || r.Error != store.ReasonFolder {
		t.Errorf("assets %+v", r)
	}
}

// Without an active profile, uploads stop: entries are skipped.
func TestNoActiveProfile(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.cb.copy(image1280(t))
	if r := e.settled(1)[0]; r.Status != store.StatusSkipped || r.Error != store.ReasonNoProfile {
		t.Errorf("%+v", r)
	}
}

// file-naming spec "Counter resets daily" through the pipeline: the
// counter value is stored with the history rows.
func TestCounterThroughPipeline(t *testing.T) {
	e := newEnv(t, envOpts{})
	p := e.addProfile("dev-box", nil)
	if err := call(t, func(d func(error)) {
		e.c.SaveNaming(store.Naming{Kind: store.KindFile, Template: "f_{counter}.{ext}"}, d)
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	a, b := dir+"/a.txt", dir+"/b.txt"
	os.WriteFile(a, []byte("a"), 0o644)
	os.WriteFile(b, []byte("b"), 0o644)
	e.cb.copy(files(a, b))
	rows := e.settled(2)
	names := map[string]string{}
	for _, r := range rows {
		names[r.OriginalName] = r.RemoteName
	}
	if names["a.txt"] != "f_001.txt" || names["b.txt"] != "f_002.txt" {
		t.Errorf("names %v", names)
	}
	if v, _ := e.st.Counters.Current(ctx, p.ID, "2026-11-12"); v != 2 {
		t.Errorf("stored counter %d", v)
	}
	// Spec "Multiple files": both paths, space separated, in order.
	e.waitFor("copy-back", func(*State) bool { return len(e.cb.writes()) == 1 })
	if got := e.cb.writes()[0]; got != "/home/alice/clipboard/f_001.txt /home/alice/clipboard/f_002.txt" {
		t.Errorf("clipboard %q", got)
	}
}
