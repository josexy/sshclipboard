package sshconn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Config configures connections.
type Config struct {
	Secrets    Secrets
	KnownHosts KnownHosts
	// OpenSSHKnownHosts is the OpenSSH known_hosts file read (never
	// written) after the app's own trusted keys; "" disables it.
	OpenSSHKnownHosts string
	// DialAgent connects to the SSH agent; nil uses DialAgent.
	DialAgent func() (net.Conn, error)
	// DialTimeout bounds the TCP connect (through the proxy, if any) and
	// the SSH handshake; 0 is 15 s.
	DialTimeout time.Duration
	// KeepaliveInterval is how often idle connections are probed; 0 is
	// 30 s. Two missed replies close the connection.
	KeepaliveInterval time.Duration
	// OnStatus hears every connection state change. It must not block.
	OnStatus func(StatusEvent)
}

func (c *Config) dialTimeout() time.Duration {
	if c.DialTimeout > 0 {
		return c.DialTimeout
	}
	return 15 * time.Second
}

func (c *Config) keepalive() time.Duration {
	if c.KeepaliveInterval > 0 {
		return c.KeepaliveInterval
	}
	return 30 * time.Second
}

// Step is a stage of connecting, which errors and connection tests name.
type Step string

const (
	StepConnect   Step = "connect"
	StepProxy     Step = "proxy" // connecting through the profile's proxy
	StepHostKey   Step = "host-key"
	StepAuth      Step = "authenticate"
	StepRemoteDir Step = "remote-directory"
)

// DialError is a failure to connect, at a step.
type DialError struct {
	Step Step
	Err  error
}

func (e *DialError) Error() string { return e.Err.Error() }
func (e *DialError) Unwrap() error { return e.Err }

// Conn is an open connection of a profile: an SSH client, an SFTP client
// over it, and the login user's home directory.
type Conn struct {
	ProfileID int64
	SSH       *ssh.Client
	SFTP      *sftp.Client
	Home      string

	once sync.Once
	dead chan struct{}
}

// Close closes the connection.
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		c.SFTP.Close()
		err = c.SSH.Close()
		close(c.dead)
	})
	return err
}

// Dead is closed once the connection is closed or lost.
func (c *Conn) Dead() <-chan struct{} { return c.dead }

// Alive tells whether the connection is still open.
func (c *Conn) Alive() bool {
	select {
	case <-c.dead:
		return false
	default:
		return true
	}
}

// Resolve resolves a remote directory against the home directory: "~"
// and "~/x" and relative paths are under home; absolute paths stay.
func (c *Conn) Resolve(dir string) string { return ResolvePath(c.Home, dir) }

// ResolvePath resolves dir against home.
func ResolvePath(home, dir string) string {
	switch {
	case dir == "~" || dir == "" || dir == ".":
		return path.Clean(home)
	case strings.HasPrefix(dir, "~/"):
		return path.Join(home, dir[2:])
	case strings.HasPrefix(dir, "/"):
		return path.Clean(dir)
	}
	return path.Join(home, dir)
}

// Dial opens a new connection to t, without the pool. The caller closes
// it.
func (c *Config) Dial(ctx context.Context, t Target) (*Conn, error) {
	methods, agentConn, err := authMethods(t, c.Secrets, c.DialAgent)
	if err != nil {
		return nil, &DialError{Step: StepAuth, Err: err}
	}
	if agentConn != nil {
		// The agent signs during the handshake only.
		defer agentConn.Close()
	}
	// Host keys are verified against addr, the server's own host and port,
	// even through a proxy.
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
	nc, err := c.dialNet(ctx, t, addr)
	if err != nil {
		return nil, err
	}
	// The handshake honors the context and the timeout.
	nc.SetDeadline(time.Now().Add(c.dialTimeout()))
	stop := context.AfterFunc(ctx, func() { nc.Close() })
	sc, chans, reqs, err := ssh.NewClientConn(nc, addr, &ssh.ClientConfig{
		User:            t.User,
		Auth:            methods,
		HostKeyCallback: hostKeyCallback(ctx, c.KnownHosts, c.OpenSSHKnownHosts, t.Host, t.Port),
		Timeout:         c.dialTimeout(),
	})
	if !stop() || err != nil {
		nc.Close()
		if ctx.Err() != nil {
			return nil, &DialError{Step: StepConnect, Err: ctx.Err()}
		}
		return nil, classifyHandshake(err)
	}
	nc.SetDeadline(time.Time{})
	client := ssh.NewClient(sc, chans, reqs)
	sftpc, err := sftp.NewClient(client, sftp.UseConcurrentWrites(true))
	if err != nil {
		client.Close()
		return nil, &DialError{Step: StepConnect, Err: fmt.Errorf("starting SFTP: %w", err)}
	}
	home, err := sftpc.Getwd()
	if err != nil {
		sftpc.Close()
		client.Close()
		return nil, &DialError{Step: StepConnect, Err: fmt.Errorf("reading the home directory: %w", err)}
	}
	conn := &Conn{ProfileID: t.ProfileID, SSH: client, SFTP: sftpc, Home: home, dead: make(chan struct{})}
	go func() {
		client.Wait()
		conn.Close()
	}()
	return conn, nil
}

func classifyHandshake(err error) error {
	var hk *HostKeyError
	switch {
	case errors.As(err, &hk):
		return &DialError{Step: StepHostKey, Err: hk}
	case strings.Contains(err.Error(), "unable to authenticate"):
		return &DialError{Step: StepAuth, Err: errors.New("authentication failed: the server rejected the credentials")}
	}
	return &DialError{Step: StepConnect, Err: err}
}
