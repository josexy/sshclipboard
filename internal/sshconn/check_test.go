package sshconn

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// Spec "Successful test": success with the resolved absolute directory.
func TestCheckSuccess(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	r := pwConfig(trusting(s), "").Check(bg, target(s, store.AuthPassword), "~/clipboard", true)
	if !r.OK() || r.Dir != "/home/alice/clipboard" {
		t.Fatalf("result %+v", r)
	}
	fi, err := os.Stat(s.Local("/home/alice/clipboard"))
	if err != nil || !fi.IsDir() {
		t.Error("the directory was not created")
	}
	entries, _ := os.ReadDir(s.Local("/home/alice/clipboard"))
	if len(entries) != 0 {
		t.Errorf("the probe file was left behind: %v", entries)
	}
}

// Spec "Directory not writable": failure at the remote directory step
// with the server's message.
func TestCheckNotWritable(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	os.MkdirAll(s.Local("/home/alice/clipboard"), 0o755)
	s.DenyCreate(true)
	r := pwConfig(trusting(s), "").Check(bg, target(s, store.AuthPassword), "~/clipboard", true)
	if r.OK() || r.Step != StepRemoteDir || !errors.Is(r.Err, os.ErrPermission) ||
		!strings.Contains(r.Err.Error(), "permission denied") {
		t.Errorf("result %+v", r)
	}
	// And when the directory can't be created.
	r = pwConfig(trusting(s), "").Check(bg, target(s, store.AuthPassword), "/srv/new", true)
	if r.Step != StepRemoteDir || r.Dir != "/srv/new" {
		t.Errorf("mkdir denied: %+v", r)
	}
}

func TestCheckMissingDirWithoutCreate(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	r := pwConfig(trusting(s), "").Check(bg, target(s, store.AuthPassword), "~/nope", false)
	if r.Step != StepRemoteDir || !errors.Is(r.Err, ErrRemoteDirMissing) {
		t.Errorf("result %+v", r)
	}
}

func TestCheckReportsEarlierSteps(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	if r := pwConfig(&fakeKnown{}, "").Check(bg, target(s, store.AuthPassword), "~", true); r.Step != StepHostKey {
		t.Errorf("unknown host: %+v", r)
	}
	bad := pwConfig(trusting(s), "")
	bad.Secrets = fakeSecrets{"password": "wrong"}
	if r := bad.Check(bg, target(s, store.AuthPassword), "~", true); r.Step != StepAuth {
		t.Errorf("bad password: %+v", r)
	}
	tg := target(s, store.AuthPassword)
	s.Close()
	if r := pwConfig(trusting(s), "").Check(bg, tg, "~", true); r.Step != StepConnect {
		t.Errorf("server down: %+v", r)
	}
}
