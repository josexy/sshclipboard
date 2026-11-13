package sshtest

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func dial(t *testing.T, s *Server, auth ...ssh.AuthMethod) (*ssh.Client, *sftp.Client) {
	t.Helper()
	c, err := ssh.Dial("tcp", s.Addr(), &ssh.ClientConfig{
		User: "alice", Auth: auth, HostKeyCallback: ssh.FixedHostKey(s.HostKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	sc, err := sftp.NewClient(c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sc.Close() })
	return c, sc
}

func TestSmokeConnectAndList(t *testing.T) {
	s := Start(t, Options{})
	if err := os.WriteFile(s.Local("/hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, sc := dial(t, s, ssh.Password("secret"))
	entries, err := sc.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != "hello.txt" || names[1] != "home" {
		t.Errorf("root lists %q", names)
	}
	if wd, err := sc.Getwd(); err != nil || wd != "/home/alice" {
		t.Errorf("home %q %v", wd, err)
	}
	if s.Accepted() != 1 {
		t.Errorf("accepted %d", s.Accepted())
	}
}

func TestWrongPasswordRejected(t *testing.T) {
	s := Start(t, Options{})
	_, err := ssh.Dial("tcp", s.Addr(), &ssh.ClientConfig{User: "alice",
		Auth: []ssh.AuthMethod{ssh.Password("nope")}, HostKeyCallback: ssh.InsecureIgnoreHostKey()})
	if err == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestPublicKeyAndKeyboardInteractive(t *testing.T) {
	key := NewSigner(t)
	s := Start(t, Options{AuthorizedKeys: []ssh.PublicKey{key.PublicKey()}, KeyboardInteractive: true})
	dial(t, s, ssh.PublicKeys(key))
	dial(t, s, ssh.KeyboardInteractive(func(_, _ string, q []string, _ []bool) ([]string, error) {
		return []string{"secret"}, nil
	}))
}

func TestOpenSSHRenameSemantics(t *testing.T) {
	s := Start(t, Options{})
	_, sc := dial(t, s, ssh.Password("secret"))
	for _, n := range []string{"/a", "/b"} {
		f, err := sc.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(n))
		f.Close()
	}
	if err := sc.Rename("/a", "/b"); err == nil {
		t.Error("rename replaced an existing file")
	}
	if err := sc.PosixRename("/a", "/b"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(s.Local("/b")); string(b) != "/a" {
		t.Errorf("posix-rename left %q", b)
	}
	if err := sc.Rename("/b", "/c"); err != nil {
		t.Errorf("rename to a free name: %v", err)
	}
}

func TestFaults(t *testing.T) {
	s := Start(t, Options{})
	_, sc := dial(t, s, ssh.Password("secret"))
	s.DenyCreate(true)
	_, err := sc.Create("/home/alice/x")
	// The client normalizes SSH_FX_PERMISSION_DENIED to os.ErrPermission.
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("deny create: %v", err)
	}
	if err := sc.Mkdir("/home/alice/d"); err == nil {
		t.Error("mkdir allowed")
	}
	s.DenyCreate(false)

	s.DropAfter(1000)
	f, err := sc.Create("/home/alice/big")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(make([]byte, 64*1024))
	if err == nil {
		_, err = io.Copy(f, io.LimitReader(zero{}, 1<<20))
	}
	if err == nil {
		t.Error("the transfer survived a dropped connection")
	}
	if _, err := sc.Stat("/"); err == nil {
		t.Error("the connection survived the drop")
	}
}

type zero struct{}

func (zero) Read(p []byte) (int, error) { clear(p); return len(p), nil }
