package sshconn

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

func pwConfig(known KnownHosts, openssh string) *Config {
	return &Config{Secrets: fakeSecrets{secret.Password: "secret"}, KnownHosts: known, OpenSSHKnownHosts: openssh}
}

func hostKeyErr(t *testing.T, err error) *HostKeyError {
	t.Helper()
	var hk *HostKeyError
	var de *DialError
	if !errors.As(err, &hk) || !errors.As(err, &de) || de.Step != StepHostKey {
		t.Fatalf("got %v, want a HostKeyError at the host-key step", err)
	}
	return hk
}

func TestHostKeyTrusted(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	dialOK(t, pwConfig(trusting(s), ""), target(s, store.AuthPassword))
}

// Spec "First connection": the key type and SHA-256 fingerprint are shown.
func TestHostKeyUnknown(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	_, err := pwConfig(&fakeKnown{}, filepath.Join(t.TempDir(), "absent")).Dial(bg, target(s, store.AuthPassword))
	hk := hostKeyErr(t, err)
	if hk.Changed() || hk.KeyType() != "ssh-ed25519" || hk.Fingerprint() != ssh.FingerprintSHA256(s.HostKey()) ||
		!strings.HasPrefix(hk.Fingerprint(), "SHA256:") || hk.Host != s.Host || hk.Port != s.Port {
		t.Errorf("unknown key error %+v", hk)
	}
	// After Trust, the same key connects.
	known := &fakeKnown{}
	known.Put(hk.KnownHost())
	dialOK(t, pwConfig(known, ""), target(s, store.AuthPassword))
}

// Spec "Changed host key": the old and new fingerprints, and Replace.
func TestHostKeyChanged(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	known := trusting(s)
	old := ssh.FingerprintSHA256(s.HostKey())
	s.SetHostKey(sshtest.NewSigner(t))
	_, err := pwConfig(known, "").Dial(bg, target(s, store.AuthPassword))
	hk := hostKeyErr(t, err)
	if !hk.Changed() || hk.Known.Fingerprint != old || hk.Fingerprint() != ssh.FingerprintSHA256(s.HostKey()) ||
		hk.Fingerprint() == old || !strings.Contains(hk.Error(), "host key changed") {
		t.Errorf("changed key error %+v: %v", hk, hk)
	}
	known.Put(hk.KnownHost()) // Replace
	dialOK(t, pwConfig(known, ""), target(s, store.AuthPassword))
}

// Hosts the user already trusts in OpenSSH are not prompted again; the
// file is only read.
func TestHostKeyOpenSSHKnown(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	file := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{knownhosts.Normalize(s.Addr())}, s.HostKey()) + "\n"
	if err := os.WriteFile(file, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	dialOK(t, pwConfig(&fakeKnown{}, file), target(s, store.AuthPassword))
	if b, _ := os.ReadFile(file); string(b) != line {
		t.Error("the OpenSSH known_hosts file was modified")
	}

	// A different key in OpenSSH's file counts as changed.
	s.SetHostKey(sshtest.NewSigner(t))
	_, err := pwConfig(&fakeKnown{}, file).Dial(bg, target(s, store.AuthPassword))
	if hk := hostKeyErr(t, err); !hk.Changed() || !hk.FromOpenSSH {
		t.Errorf("OpenSSH mismatch %+v", hk)
	}

	// The app's own trust wins over OpenSSH's file.
	dialOK(t, pwConfig(trusting(s), file), target(s, store.AuthPassword))
}
