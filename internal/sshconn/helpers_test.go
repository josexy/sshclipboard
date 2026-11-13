package sshconn

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// fakeSecrets is a profile's stored secrets.
type fakeSecrets map[secret.Kind]string

func (f fakeSecrets) Get(_ int64, kind secret.Kind) (string, error) {
	if v, ok := f[kind]; ok {
		return v, nil
	}
	return "", secret.ErrNotFound
}

// fakeKnown is the app's trusted keys.
type fakeKnown struct {
	mu sync.Mutex
	m  map[string]store.KnownHost
}

func (f *fakeKnown) key(host string, port int) string { return fmt.Sprintf("%s:%d", host, port) }

func (f *fakeKnown) Get(_ context.Context, host string, port int) (store.KnownHost, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kh, ok := f.m[f.key(host, port)]
	return kh, ok, nil
}

func (f *fakeKnown) Put(kh store.KnownHost) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]store.KnownHost{}
	}
	f.m[f.key(kh.Host, kh.Port)] = kh
}

// trusting returns known hosts that trust s's current key.
func trusting(s *sshtest.Server) *fakeKnown {
	k := &fakeKnown{}
	k.Put(KnownHostOf(s.Host, s.Port, s.HostKey()))
	return k
}

func target(s *sshtest.Server, method store.AuthMethod) Target {
	return Target{ProfileID: 1, Name: "dev-box", Host: s.Host, Port: s.Port, User: "alice", AuthMethod: method}
}

// writeKey writes a new ed25519 key in OpenSSH format, encrypted when
// passphrase is not empty, and returns its path and public key.
func writeKey(t *testing.T, passphrase string) (string, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(priv, "test")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(priv, "test", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	sp, _ := ssh.NewPublicKey(pub)
	return path, sp
}
