package core

import (
	"os"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/store"
)

func prof(edit func(*store.Profile)) store.Profile {
	p := store.NewProfile()
	p.Name, p.Host, p.Port, p.Username = "dev-box", "10.0.0.5", 22, "alice"
	if edit != nil {
		edit(&p)
	}
	return p
}

func TestCopyBackText(t *testing.T) {
	for _, c := range []struct {
		name  string
		p     store.Profile
		paths []string
		want  string
	}{
		// Spec "Screenshot path copied back".
		{"default", prof(nil), []string{"/home/alice/clipboard/screenshot_20261112-153045.png"},
			"/home/alice/clipboard/screenshot_20261112-153045.png"},
		// Spec "Multiple files".
		{"multiple", prof(nil), []string{"/srv/in/a.txt", "/srv/in/b.txt"}, "/srv/in/a.txt /srv/in/b.txt"},
		{"newline", prof(func(p *store.Profile) { p.CopySeparator = "\n" }), []string{"/a", "/b"}, "/a\n/b"},
		// Spec "Prefixed reference".
		{"prefixed", prof(func(p *store.Profile) { p.CopyTemplate = "@{path}" }), []string{"/home/alice/clipboard/a.png"},
			"@/home/alice/clipboard/a.png"},
		// Spec "Path with spaces".
		{"spaces", prof(nil), []string{"/srv/in/My Report.pdf"}, "'/srv/in/My Report.pdf'"},
		{"no quoting", prof(func(p *store.Profile) { p.ShellQuote = false }), []string{"/srv/in/My Report.pdf"}, "/srv/in/My Report.pdf"},
		// Spec "scp-style reference".
		{"scp", prof(func(p *store.Profile) { p.CopyTemplate = "{user}@{host}:{path}" }), []string{"/srv/in/a.txt"},
			"alice@10.0.0.5:/srv/in/a.txt"},
		{"all tokens", prof(func(p *store.Profile) { p.CopyTemplate = "{profile} {port} {dir} {name}"; p.Port = 2222 }),
			[]string{"/srv/my in/a b.txt"}, "dev-box 2222 '/srv/my in' 'a b.txt'"},
		{"quote in name", prof(nil), []string{"/srv/it's.txt"}, `'/srv/it'\''s.txt'`},
	} {
		if got := CopyBackText(c.p, c.paths); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/home/a/b.png": "/home/a/b.png", "a-b_c.d:e,f@g%h+i=j": "a-b_c.d:e,f@g%h+i=j",
		"": "''", "a b": "'a b'", "$HOME": "'$HOME'", "a;b": "'a;b'", "~x": "'~x'", "x*": "'x*'",
	} {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateCopyTemplate(t *testing.T) {
	if tok := ValidateCopyTemplate("{user}@{host}:{path}"); tok != "" {
		t.Error(tok)
	}
	if tok := ValidateCopyTemplate("@{paht}"); tok != "{paht}" {
		t.Error(tok)
	}
}

// Spec "Setting off": the clipboard keeps what the user copied.
func TestCopyBackOff(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", func(p *store.Profile) { p.CopyBack = false })
	e.cb.copy(image1280(t))
	if r := e.settled(1)[0]; r.Status != store.StatusUploaded {
		t.Fatalf("%+v", r)
	}
	time.Sleep(50 * time.Millisecond)
	if w := e.cb.writes(); len(w) != 0 {
		t.Errorf("copied back %q", w)
	}
}

// Spec "User copied something else meanwhile".
func TestNewerClipboardNotReplaced(t *testing.T) {
	g := newGate()
	e := newEnv(t, envOpts{open: g.open})
	e.addProfile("dev-box", nil)
	big := t.TempDir() + "/slow-big.bin"
	os.WriteFile(big, []byte("data"), 0o644)
	e.cb.copy(files(big))
	e.waitFor("uploading", func(s *State) bool { return s.Uploading == 1 })
	e.cb.copy(text("newer text")) // skipped by "Skip text"
	e.waitFor("text recorded", func(s *State) bool { return s.HistoryTotal == 2 })
	close(g.release)
	rows := e.settled(2)
	for _, r := range rows {
		if r.OriginalName == "slow-big.bin" && (r.Status != store.StatusUploaded || r.RemotePath == "") {
			t.Errorf("history must still record the path: %+v", r)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if w := e.cb.writes(); len(w) != 0 {
		t.Errorf("replaced the newer clipboard with %q", w)
	}
}

// Copy-back waits for every entry and needs at least one upload.
func TestCopyBackAfterPartialFailure(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	dir := t.TempDir()
	ok, gone := dir+"/ok.txt", dir+"/gone.txt"
	os.WriteFile(ok, []byte("ok"), 0o644)
	os.WriteFile(gone, []byte("x"), 0o644)
	e.cb.copy(func(f *fakeClipboard) {
		files(ok, gone)(f)
		os.Remove(gone) // deleted before its upload starts
	})
	rows := e.settled(2)
	failed := 0
	for _, r := range rows {
		if r.Status == store.StatusFailed && r.Error == store.ReasonSourceNotFound {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("rows %+v", rows)
	}
	e.waitFor("copy-back", func(*State) bool { return len(e.cb.writes()) == 1 })
	if got := e.cb.writes()[0]; got != "/home/alice/clipboard/ok.txt" {
		t.Errorf("clipboard %q", got)
	}
}
