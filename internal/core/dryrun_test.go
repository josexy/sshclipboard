package core

import (
	"os"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/capture"
	"github.com/josexy/sshclipboard/internal/store"
)

// capture-rules spec "Dry run": notes.md, the rule (or default action),
// the action, and the remote path; history and the counter unchanged.
func TestDryRun(t *testing.T) {
	e := newEnv(t, envOpts{})
	p := e.addProfile("dev-box", nil)
	call(t, func(d func(error)) {
		e.c.SaveNaming(store.Naming{Kind: store.KindFile, Template: "{counter}_{name}.{ext}"}, d)
	})
	notes := t.TempDir() + "/notes.md"
	os.WriteFile(notes, []byte("# notes"), 0o644)
	// Put it on the clipboard without a change event, so nothing is
	// captured.
	e.cb.mu.Lock()
	e.cb.formats, e.cb.files = []string{capture.FormatFileList}, []string{notes}
	e.cb.mu.Unlock()

	ch := make(chan []DryRunResult, 1)
	e.c.DryRun(func(r []DryRunResult) { ch <- r })
	var res []DryRunResult
	select {
	case res = <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("no result")
	}
	if len(res) != 1 || res[0].Name != "notes.md" || res[0].RuleName != store.RuleDefault ||
		res[0].Action != store.ActionUpload || res[0].RemotePath != "~/clipboard/001_notes.md" {
		t.Fatalf("%+v", res)
	}
	if n := len(e.entries()); n != 0 {
		t.Errorf("history changed: %d entries", n)
	}
	if v, _ := e.st.Counters.Current(ctx, p.ID, "2026-11-12"); v != 0 {
		t.Errorf("the dry run advanced the counter to %d", v)
	}

	// Text: the "Skip text" rule.
	e.cb.mu.Lock()
	e.cb.formats, e.cb.files, e.cb.text = []string{capture.FormatText}, nil, "hi"
	e.cb.mu.Unlock()
	e.c.DryRun(func(r []DryRunResult) { ch <- r })
	if r := <-ch; len(r) != 1 || r[0].RuleName != "Skip text" || r[0].Action != store.ActionSkip {
		t.Errorf("%+v", r)
	}
	staged, _ := os.ReadDir(e.dataDir + "/staging")
	if len(staged) != 0 {
		t.Errorf("the dry run staged files: %v", staged)
	}
}
