// Package sshtest runs an in-process SSH server with an SFTP subsystem for
// tests: it listens on 127.0.0.1:0, serves a temporary directory as the
// remote filesystem with OpenSSH's semantics (a plain rename never
// overwrites; posix-rename@openssh.com does), and injects faults.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Options configure a Server. The zero value serves user "alice" with
// password "secret" and home /home/alice.
type Options struct {
	User     string // default "alice"
	Password string // default "secret"; "-" disables password auth
	// KeyboardInteractive offers keyboard-interactive (answered with the
	// password) instead of the password method.
	KeyboardInteractive bool
	// AuthorizedKeys may authenticate with public keys.
	AuthorizedKeys []ssh.PublicKey
	// HostKey is the server's key; nil generates an ed25519 key.
	HostKey ssh.Signer
	Home    string // default "/home/<user>"
}

// Server is a running test server.
type Server struct {
	Host string // "127.0.0.1"
	Port int
	Root string // the directory on disk that is the remote "/"
	Home string // the remote home directory

	opts     Options
	listener net.Listener

	mu         sync.Mutex
	hostKey    ssh.Signer
	conns      map[net.Conn]bool
	dropAfter  int64 // > 0: drop every connection once this many bytes were written
	denyCreate bool
	stall      bool // hold global requests (keepalives) without replying
	chmods     []ChmodRequest

	written  atomic.Int64
	accepted atomic.Int64
	wg       sync.WaitGroup
}

// Start starts a server, stopped when the test ends.
func Start(tb testing.TB, opts Options) *Server {
	tb.Helper()
	if opts.User == "" {
		opts.User = "alice"
	}
	if opts.Password == "" {
		opts.Password = "secret"
	}
	if opts.Home == "" {
		opts.Home = "/home/" + opts.User
	}
	if opts.HostKey == nil {
		opts.HostKey = NewSigner(tb)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	s := &Server{
		Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, Root: tb.TempDir(), Home: opts.Home,
		opts: opts, listener: l, hostKey: opts.HostKey, conns: map[net.Conn]bool{},
	}
	if err := os.MkdirAll(s.Local(opts.Home), 0o755); err != nil {
		tb.Fatal(err)
	}
	s.wg.Add(1)
	go s.serve()
	tb.Cleanup(s.Close)
	return s
}

// NewSigner returns a new ed25519 signer.
func NewSigner(tb testing.TB) ssh.Signer {
	tb.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

// Addr returns "host:port".
func (s *Server) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// HostKey returns the server's current public host key.
func (s *Server) HostKey() ssh.PublicKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hostKey.PublicKey()
}

// SetHostKey changes the key new connections are offered, as a
// reinstalled server does.
func (s *Server) SetHostKey(k ssh.Signer) {
	s.mu.Lock()
	s.hostKey = k
	s.mu.Unlock()
}

// Local returns the path on disk of a remote absolute path.
func (s *Server) Local(remote string) string {
	return filepath.Join(s.Root, filepath.FromSlash(path.Clean("/"+remote)))
}

// DropAfter makes the server drop every connection once n more bytes of
// file data have been written, once; n <= 0 turns it off.
func (s *Server) DropAfter(n int64) {
	s.mu.Lock()
	if n > 0 {
		s.dropAfter = s.written.Load() + n
	} else {
		s.dropAfter = 0
	}
	s.mu.Unlock()
}

// DenyCreate makes creating files and directories fail with permission
// denied.
func (s *Server) DenyCreate(deny bool) {
	s.mu.Lock()
	s.denyCreate = deny
	s.mu.Unlock()
}

// Stall makes the server stop replying to global requests, such as
// keepalives, as a hung server does.
func (s *Server) Stall(stall bool) {
	s.mu.Lock()
	s.stall = stall
	s.mu.Unlock()
}

// KillConnections closes every open connection, as a server reboot does.
func (s *Server) KillConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		c.Close()
	}
}

// Accepted returns how many connections the server accepted.
func (s *Server) Accepted() int { return int(s.accepted.Load()) }

// Written returns how many bytes of file data were written.
func (s *Server) Written() int64 { return s.written.Load() }

// ChmodRequest records the path and permission bits sent in an SFTP Setstat.
type ChmodRequest struct {
	Path string
	Mode uint32
}

// ChmodRequests returns the successful permission changes received over SFTP.
func (s *Server) ChmodRequests() []ChmodRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ChmodRequest(nil), s.chmods...)
}

// Close stops the server.
func (s *Server) Close() {
	s.listener.Close()
	s.KillConnections()
	s.wg.Wait()
}

func (s *Server) config() *ssh.ServerConfig {
	s.mu.Lock()
	key := s.hostKey
	s.mu.Unlock()
	cfg := &ssh.ServerConfig{}
	if s.opts.Password != "-" {
		check := func(user, pw string) (*ssh.Permissions, error) {
			if user == s.opts.User && pw == s.opts.Password {
				return nil, nil
			}
			return nil, errors.New("wrong password")
		}
		if s.opts.KeyboardInteractive {
			cfg.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
				ans, err := ask("", "", []string{"Password: "}, []bool{false})
				if err != nil || len(ans) != 1 {
					return nil, errors.New("no answer")
				}
				return check(c.User(), ans[0])
			}
		} else {
			cfg.PasswordCallback = func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
				return check(c.User(), string(pw))
			}
		}
	}
	if len(s.opts.AuthorizedKeys) > 0 {
		cfg.PublicKeyCallback = func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			for _, ak := range s.opts.AuthorizedKeys {
				if c.User() == s.opts.User && string(ak.Marshal()) == string(k.Marshal()) {
					return nil, nil
				}
			}
			return nil, errors.New("unknown key")
		}
	}
	cfg.AddHostKey(key)
	return cfg
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.accepted.Add(1)
		s.mu.Lock()
		s.conns[c] = true
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() {
				c.Close()
				s.mu.Lock()
				delete(s.conns, c)
				s.mu.Unlock()
			}()
			s.handle(c)
		}()
	}
}

func (s *Server) handle(c net.Conn) {
	sc, chans, reqs, err := ssh.NewServerConn(c, s.config())
	if err != nil {
		return
	}
	defer sc.Close()
	go func() {
		for r := range reqs {
			s.mu.Lock()
			stall := s.stall
			s.mu.Unlock()
			if stall {
				continue // never reply
			}
			if r.WantReply {
				r.Reply(r.Type == "keepalive@openssh.com", nil)
			}
		}
	}()
	var wg sync.WaitGroup
	defer wg.Wait()
	for nc := range chans {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "only sessions")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer ch.Close()
			for r := range creqs {
				if r.Type == "subsystem" && len(r.Payload) > 4 && string(r.Payload[4:]) == "sftp" {
					r.Reply(true, nil)
					srv := sftp.NewRequestServer(ch, s.handlers(), sftp.WithStartDirectory(s.Home))
					srv.Serve()
					srv.Close()
					return
				}
				if r.WantReply {
					r.Reply(false, nil)
				}
			}
		}()
	}
}

// fs implements the request server's handlers over Root.
type fs struct{ s *Server }

func (s *Server) handlers() sftp.Handlers {
	f := fs{s}
	return sftp.Handlers{FileGet: f, FilePut: f, FileCmd: f, FileList: f}
}

func (f fs) local(p string) string { return f.s.Local(p) }

func (f fs) denied() bool {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.denyCreate
}

func (f fs) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	return os.Open(f.local(r.Filepath))
}

func (f fs) open(r *sftp.Request) (*os.File, error) {
	pf := r.Pflags()
	flags := os.O_WRONLY
	if pf.Read {
		flags = os.O_RDWR
	}
	if pf.Append {
		flags |= os.O_APPEND
	}
	if pf.Creat {
		flags |= os.O_CREATE
	}
	if pf.Trunc {
		flags |= os.O_TRUNC
	}
	if pf.Excl {
		flags |= os.O_EXCL
	}
	local := f.local(r.Filepath)
	if f.denied() {
		if _, err := os.Stat(local); os.IsNotExist(err) {
			return nil, sftp.ErrSSHFxPermissionDenied
		}
	}
	return os.OpenFile(local, flags, 0o644)
}

func (f fs) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	file, err := f.open(r)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: file, s: f.s}, nil
}

func (f fs) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	file, err := f.open(r)
	if err != nil {
		return nil, err
	}
	return &countingFile{File: file, s: f.s}, nil
}

// countingFile counts written bytes and drops the connections when the
// DropAfter budget is spent.
type countingFile struct {
	*os.File
	s *Server
}

func (c *countingFile) WriteAt(p []byte, off int64) (int, error) {
	total := c.s.written.Add(int64(len(p)))
	c.s.mu.Lock()
	drop := c.s.dropAfter > 0 && total > c.s.dropAfter
	if drop {
		c.s.dropAfter = 0
	}
	c.s.mu.Unlock()
	if drop {
		c.s.KillConnections()
		return 0, errors.New("connection dropped")
	}
	return c.File.WriteAt(p, off)
}

func (f fs) Filecmd(r *sftp.Request) error {
	local := f.local(r.Filepath)
	switch r.Method {
	case "Setstat":
		attrs, flags := r.Attributes(), r.AttrFlags()
		if flags.Permissions {
			if err := os.Chmod(local, os.FileMode(attrs.Mode).Perm()); err != nil {
				return err
			}
			f.s.mu.Lock()
			f.s.chmods = append(f.s.chmods, ChmodRequest{Path: r.Filepath, Mode: attrs.Mode})
			f.s.mu.Unlock()
		}
		if flags.Size {
			if err := os.Truncate(local, int64(attrs.Size)); err != nil {
				return err
			}
		}
		if flags.Acmodtime {
			if err := os.Chtimes(local, time.Unix(int64(attrs.Atime), 0), time.Unix(int64(attrs.Mtime), 0)); err != nil {
				return err
			}
		}
		return nil
	case "Rename":
		// OpenSSH (SFTP v3) refuses to replace an existing target. A
		// hard link fails atomically when the target exists.
		target := f.local(r.Target)
		if err := os.Link(local, target); err != nil {
			if os.IsExist(err) {
				return sftp.ErrSSHFxFailure
			}
			if _, serr := os.Lstat(target); serr == nil {
				return sftp.ErrSSHFxFailure
			}
			return os.Rename(local, target) // directories can't be linked
		}
		return os.Remove(local)
	case "Rmdir":
		return os.Remove(local)
	case "Remove":
		return os.Remove(local)
	case "Mkdir":
		if f.denied() {
			return sftp.ErrSSHFxPermissionDenied
		}
		return os.Mkdir(local, 0o755)
	}
	return sftp.ErrSSHFxOpUnsupported
}

func (f fs) PosixRename(r *sftp.Request) error {
	return os.Rename(f.local(r.Filepath), f.local(r.Target))
}

type lister []os.FileInfo

func (l lister) ListAt(out []os.FileInfo, off int64) (int, error) {
	if off >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(out, l[off:])
	if int(off)+n >= len(l) {
		return n, io.EOF
	}
	return n, nil
}

func (f fs) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	local := f.local(r.Filepath)
	switch r.Method {
	case "List":
		entries, err := os.ReadDir(local)
		if err != nil {
			return nil, err
		}
		var infos lister
		for _, e := range entries {
			if fi, err := e.Info(); err == nil {
				infos = append(infos, fi)
			}
		}
		return infos, nil
	case "Stat":
		fi, err := os.Stat(local)
		if err != nil {
			return nil, err
		}
		return lister{fi}, nil
	}
	return nil, fmt.Errorf("unsupported %s", r.Method)
}
