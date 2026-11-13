package sshconn

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

var bg = context.Background()

func dialOK(t *testing.T, cfg *Config, tg Target) *Conn {
	t.Helper()
	c, err := cfg.Dial(bg, tg)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	if c.Home != "/home/alice" {
		t.Errorf("home %q", c.Home)
	}
	return c
}

func TestAuthPassword(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	dialOK(t, &Config{Secrets: fakeSecrets{secret.Password: "secret"}, KnownHosts: trusting(s)}, target(s, store.AuthPassword))
}

func TestAuthKeyboardInteractiveAnsweredWithPassword(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{KeyboardInteractive: true})
	dialOK(t, &Config{Secrets: fakeSecrets{secret.Password: "secret"}, KnownHosts: trusting(s)}, target(s, store.AuthPassword))
}

func TestAuthPasswordErrors(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	_, err := (&Config{Secrets: fakeSecrets{}, KnownHosts: trusting(s)}).Dial(bg, target(s, store.AuthPassword))
	var need *SecretNeededError
	if !errors.As(err, &need) || need.Kind != secret.Password {
		t.Errorf("no stored password: %v", err)
	}
	_, err = (&Config{Secrets: fakeSecrets{secret.Password: "wrong"}, KnownHosts: trusting(s)}).Dial(bg, target(s, store.AuthPassword))
	var de *DialError
	if !errors.As(err, &de) || de.Step != StepAuth {
		t.Errorf("wrong password: %v", err)
	}
}

// Spec "Private key with passphrase".
func TestAuthPrivateKey(t *testing.T) {
	plain, plainPub := writeKey(t, "")
	enc, encPub := writeKey(t, "correct horse")
	s := sshtest.Start(t, sshtest.Options{Password: "-", AuthorizedKeys: []ssh.PublicKey{plainPub, encPub}})

	tg := target(s, store.AuthKey)
	tg.KeyPath = plain
	dialOK(t, &Config{Secrets: fakeSecrets{}, KnownHosts: trusting(s)}, tg)

	tg.KeyPath = enc
	dialOK(t, &Config{Secrets: fakeSecrets{secret.Passphrase: "correct horse"}, KnownHosts: trusting(s)}, tg)
}

func TestAuthPEMKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	path := filepath.Join(t.TempDir(), "id_ecdsa.pem")
	os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600)
	pub, _ := ssh.NewPublicKey(&key.PublicKey)
	s := sshtest.Start(t, sshtest.Options{Password: "-", AuthorizedKeys: []ssh.PublicKey{pub}})
	tg := target(s, store.AuthKey)
	tg.KeyPath = path
	dialOK(t, &Config{Secrets: fakeSecrets{}, KnownHosts: trusting(s)}, tg)
}

// Spec "Unreadable key file": the error names the file and the reason.
func TestAuthKeyErrors(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	enc, _ := writeKey(t, "right")
	garbage := filepath.Join(t.TempDir(), "garbage")
	os.WriteFile(garbage, []byte("not a key"), 0o600)
	missing := filepath.Join(t.TempDir(), "nope", "id_ed25519")

	for _, c := range []struct {
		name, path string
		secrets    fakeSecrets
		reason     string
	}{
		{"missing file", missing, fakeSecrets{}, "does not exist"},
		{"unparseable", garbage, fakeSecrets{}, "not a valid private key"},
		{"wrong passphrase", enc, fakeSecrets{secret.Passphrase: "wrong"}, "passphrase is wrong"},
	} {
		tg := target(s, store.AuthKey)
		tg.KeyPath = c.path
		_, err := (&Config{Secrets: c.secrets, KnownHosts: trusting(s)}).Dial(bg, tg)
		var ke *KeyError
		var de *DialError
		if !errors.As(err, &ke) || !errors.As(err, &de) || de.Step != StepAuth {
			t.Errorf("%s: got %v, want a KeyError at the auth step", c.name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.path) || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s: message %q must name %s and %q", c.name, err, c.path, c.reason)
		}
	}

	tg := target(s, store.AuthKey)
	tg.KeyPath = enc
	_, err := (&Config{Secrets: fakeSecrets{}, KnownHosts: trusting(s)}).Dial(bg, tg)
	var need *SecretNeededError
	if !errors.As(err, &need) || need.Kind != secret.Passphrase {
		t.Errorf("encrypted key without a stored passphrase: %v", err)
	}
}

// serveAgent returns a dialer of an in-process agent holding keys.
func serveAgent(t *testing.T, keys ...any) func() (net.Conn, error) {
	ring := agent.NewKeyring()
	for _, k := range keys {
		if err := ring.Add(agent.AddedKey{PrivateKey: k}); err != nil {
			t.Fatal(err)
		}
	}
	return func() (net.Conn, error) {
		a, b := net.Pipe()
		go agent.ServeAgent(ring, b)
		return a, nil
	}
}

func TestAuthAgent(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, _ := ssh.NewPublicKey(&key.PublicKey)
	s := sshtest.Start(t, sshtest.Options{Password: "-", AuthorizedKeys: []ssh.PublicKey{pub}})
	dialOK(t, &Config{Secrets: fakeSecrets{}, KnownHosts: trusting(s), DialAgent: serveAgent(t, key)}, target(s, store.AuthAgent))
}

// Spec "Agent not running".
func TestAuthAgentUnavailable(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	down := func() (net.Conn, error) { return nil, errors.New("pipe not found") }
	_, err := (&Config{KnownHosts: trusting(s), DialAgent: down}).Dial(bg, target(s, store.AuthAgent))
	if !errors.Is(err, ErrAgentUnavailable) || !strings.Contains(err.Error(), "SSH agent is unavailable") {
		t.Errorf("agent down: %v", err)
	}
	_, err = (&Config{KnownHosts: trusting(s), DialAgent: serveAgent(t)}).Dial(bg, target(s, store.AuthAgent))
	if !errors.Is(err, ErrAgentNoKeys) {
		t.Errorf("empty agent: %v", err)
	}
	t.Setenv("SSH_AUTH_SOCK", "")
	if _, err := DialAgent(); err == nil {
		t.Error("DialAgent without SSH_AUTH_SOCK succeeded")
	}
}

func TestResolvePath(t *testing.T) {
	for _, c := range []struct{ dir, want string }{
		{"~/clipboard", "/home/alice/clipboard"},
		{"~", "/home/alice"},
		{"clipboard/x", "/home/alice/clipboard/x"},
		{"/srv/in/", "/srv/in"},
		{".", "/home/alice"},
	} {
		if got := ResolvePath("/home/alice", c.dir); got != c.want {
			t.Errorf("ResolvePath(%q) = %q, want %q", c.dir, got, c.want)
		}
	}
}
