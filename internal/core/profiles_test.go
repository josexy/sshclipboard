package core

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// gate holds uploads of files whose name contains "slow" until released.
type gate struct{ release chan struct{} }

func newGate() *gate { return &gate{release: make(chan struct{})} }

func (g *gate) open(p string) (io.ReadCloser, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	fi, _ := f.Stat()
	if strings.Contains(p, "slow") {
		<-g.release
	}
	return f, fi.Size(), nil
}

// Spec "In-flight uploads during a switch": the running upload finishes to
// the previous profile; new items use the new one.
func TestSwitchActiveProfileDuringUpload(t *testing.T) {
	g := newGate()
	e := newEnv(t, envOpts{open: g.open})
	prod := sshtest.Start(t, sshtest.Options{})
	e.st.KnownHosts.Put(ctx, sshconn.KnownHostOf(prod.Host, prod.Port, prod.HostKey()))
	dev := e.addProfile("dev-box", nil)
	pb := e.addProfile("prod-box", func(p *store.Profile) { p.Port = prod.Port })

	dir := t.TempDir()
	slow, next := dir+"/slow.txt", dir+"/next.txt"
	os.WriteFile(slow, []byte("first"), 0o644)
	os.WriteFile(next, []byte("second"), 0o644)
	e.cb.copy(files(slow))
	e.waitFor("uploading", func(s *State) bool { return s.Uploading == 1 })

	if err := call(t, func(d func(error)) { e.c.SetActiveProfile(pb.ID, d) }); err != nil {
		t.Fatal(err)
	}
	if s := e.c.State(); s.Active == nil || s.Active.Name != "prod-box" {
		t.Fatalf("active %+v", s.Active)
	}
	e.cb.copy(files(next))
	close(g.release)
	rows := e.settled(2)
	byName := map[string]store.HistoryEntry{}
	for _, r := range rows {
		byName[r.OriginalName] = r
	}
	if r := byName["slow.txt"]; r.Status != store.StatusUploaded || r.ProfileID != dev.ID {
		t.Errorf("in-flight entry %+v", r)
	}
	if r := byName["next.txt"]; r.Status != store.StatusUploaded || r.ProfileID != pb.ID {
		t.Errorf("new entry %+v", r)
	}
	if _, err := os.Stat(e.srv.Local("/home/alice/clipboard/slow.txt")); err != nil {
		t.Error("the in-flight upload did not reach dev-box")
	}
	if _, err := os.Stat(prod.Local("/home/alice/clipboard/next.txt")); err != nil {
		t.Error("the new item did not reach prod-box")
	}
}

// Spec "Restart keeps the active profile".
func TestRestartKeepsActiveProfile(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.addProfile("dev-box", nil)
	pb := e.addProfile("prod-box", nil)
	if err := call(t, func(d func(error)) { e.c.SetActiveProfile(pb.ID, d) }); err != nil {
		t.Fatal(err)
	}
	e.c.Close()
	e.start()
	if s := e.c.State(); s.Active == nil || s.Active.Name != "prod-box" {
		t.Errorf("after restart the active profile is %+v", s.Active)
	}
}

// Spec "Delete the active profile".
func TestDeleteActiveProfile(t *testing.T) {
	e := newEnv(t, envOpts{})
	p := e.addProfile("dev-box", func(p *store.Profile) { p.AuthMethod = store.AuthPassword })
	e.sec.Set(p.ID, secret.Passphrase, "pp")
	if err := call(t, func(d func(error)) { e.c.DeleteProfile(p.ID, d) }); err != nil {
		t.Fatal(err)
	}
	s := e.c.State()
	if s.Active != nil || len(s.Profiles) != 0 || s.Settings.ActiveProfile != 0 {
		t.Errorf("state after delete: active %+v, %d profiles", s.Active, len(s.Profiles))
	}
	if len(e.creds) != 0 {
		t.Errorf("secrets left: %v", e.creds)
	}
	if _, ok, _ := e.st.KnownHosts.Get(ctx, e.srv.Host, e.srv.Port); ok {
		t.Error("the trusted host key is still there")
	}
	// Automatic uploads stop.
	e.cb.copy(image1280(t))
	if r := e.settled(1)[0]; r.Status != store.StatusSkipped || r.Error != store.ReasonNoProfile {
		t.Errorf("after deleting the active profile: %+v", r)
	}
}

func TestDuplicateProfile(t *testing.T) {
	e := newEnv(t, envOpts{})
	p := e.addProfile("dev-box", nil)
	if err := call(t, func(d func(error)) { e.c.DuplicateProfile(p.ID, d) }); err != nil {
		t.Fatal(err)
	}
	if err := call(t, func(d func(error)) { e.c.DuplicateProfile(p.ID, d) }); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, q := range e.c.State().Profiles {
		names = append(names, q.Name)
		if pw, err := e.sec.Get(q.ID, secret.Password); err != nil || pw != "secret" {
			t.Errorf("%s password %q %v", q.Name, pw, err)
		}
	}
	if strings.Join(names, ",") != "dev-box,dev-box copy,dev-box copy 2" {
		t.Errorf("names %v", names)
	}
}

func TestSaveProfileRejectsTraversalAndUnknownCopyToken(t *testing.T) {
	e := newEnv(t, envOpts{})
	p := store.NewProfile()
	p.Name, p.Host, p.Username = "x", "h", "u"
	p.RemoteDir, p.CopyTemplate = "~/clipboard/../../etc", "{paht}"
	ch := make(chan error, 1)
	e.c.SaveProfile(ProfileInput{Profile: p}, func(_ SaveResult, err error) { ch <- err })
	fe, ok := (<-ch).(FieldErrors)
	if !ok || fe[FieldRemoteDir] == nil || fe[FieldTemplate] == nil {
		t.Errorf("got %v", fe)
	}
}
