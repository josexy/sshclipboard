package sshconn

import (
	"errors"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// internalName is a host name only the test proxy resolves.
const internalName = "dev-box.internal"

// viaProxy returns a target that reaches s by a name only px resolves,
// and known hosts that trust s's key under that name.
func viaProxy(t *testing.T, s *sshtest.Server, px *sshtest.SOCKS5, user string) (Target, *fakeKnown) {
	t.Helper()
	tg := target(s, store.AuthPassword)
	tg.Host = internalName
	tg.Proxy = store.Proxy{Type: store.ProxySOCKS5, Host: px.Host, Port: px.Port, Username: user}
	k := &fakeKnown{}
	k.Put(KnownHostOf(internalName, s.Port, s.HostKey()))
	return tg, k
}

func startProxy(t *testing.T, opts sshtest.SOCKS5Options) *sshtest.SOCKS5 {
	t.Helper()
	if opts.Hosts == nil {
		opts.Hosts = map[string]string{internalName: "127.0.0.1"}
	}
	return sshtest.StartSOCKS5(t, opts)
}

func dialStep(t *testing.T, err error) *DialError {
	t.Helper()
	var de *DialError
	if !errors.As(err, &de) {
		t.Fatalf("got %v, want a DialError", err)
	}
	return de
}

// The proxy, not this computer, resolves the server's name, and the host
// key is checked against the server's name and port, not the proxy's.
func TestDialThroughSOCKS5(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	px := startProxy(t, sshtest.SOCKS5Options{})
	tg, known := viaProxy(t, s, px, "")
	dialOK(t, pwConfig(known, ""), tg)
	want := []string{net.JoinHostPort(internalName, strconv.Itoa(s.Port))}
	if got := px.Targets(); !slices.Equal(got, want) {
		t.Errorf("the proxy connected to %q, want %q", got, want)
	}
	if s.Accepted() != 1 {
		t.Errorf("the server accepted %d connections", s.Accepted())
	}
}

func TestDialThroughSOCKS5HostKeyIsTheServers(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	px := startProxy(t, sshtest.SOCKS5Options{})
	tg, _ := viaProxy(t, s, px, "")
	_, err := pwConfig(&fakeKnown{}, "").Dial(bg, tg)
	hk := hostKeyErr(t, err)
	if hk.Host != internalName || hk.Port != s.Port {
		t.Errorf("host key asked for %s:%d, want %s:%d", hk.Host, hk.Port, internalName, s.Port)
	}
}

func TestDialThroughSOCKS5WithSignIn(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	px := startProxy(t, sshtest.SOCKS5Options{Username: "proxy-user", Password: "proxy-pw"})
	tg, known := viaProxy(t, s, px, "proxy-user")
	cfg := pwConfig(known, "")
	cfg.Secrets = fakeSecrets{secret.Password: "secret", secret.ProxyPassword: "proxy-pw"}
	dialOK(t, cfg, tg)

	// A wrong proxy password fails at the proxy step, naming the proxy.
	cfg.Secrets = fakeSecrets{secret.Password: "secret", secret.ProxyPassword: "wrong"}
	_, err := cfg.Dial(bg, tg)
	de := dialStep(t, err)
	msg := err.Error()
	if de.Step != StepProxy || !errors.Is(err, ErrProxyAuth) ||
		!strings.Contains(msg, "SOCKS5 proxy "+net.JoinHostPort(px.Host, strconv.Itoa(px.Port))) ||
		!strings.Contains(msg, "authentication failed") || strings.Contains(msg, "socks connect") {
		t.Errorf("wrong proxy password: step %q: %v", de.Step, err)
	}

	// No username for a proxy that needs one.
	tg.Proxy.Username = ""
	_, err = cfg.Dial(bg, tg)
	if de := dialStep(t, err); de.Step != StepProxy || !errors.Is(err, ErrProxyAuth) ||
		!strings.Contains(err.Error(), "no acceptable authentication methods") {
		t.Errorf("no username: step %q: %v", de.Step, err)
	}
	if s.Accepted() != 1 {
		t.Errorf("failed sign-ins reached the server: %d connections", s.Accepted())
	}
}

// A proxy username without a stored password asks for it, like the SSH
// password.
func TestDialProxyPasswordNeeded(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	px := startProxy(t, sshtest.SOCKS5Options{Username: "proxy-user", Password: "proxy-pw"})
	tg, known := viaProxy(t, s, px, "proxy-user")
	_, err := pwConfig(known, "").Dial(bg, tg)
	var need *SecretNeededError
	if !errors.As(err, &need) || need.Kind != secret.ProxyPassword || need.ProfileID != tg.ProfileID ||
		dialStep(t, err).Step != StepProxy {
		t.Fatalf("got %v, want the proxy password to be needed", err)
	}
	if !strings.Contains(err.Error(), "the proxy password of this server") {
		t.Errorf("message %q", err)
	}
}

func TestDialProxyFailures(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	px := startProxy(t, sshtest.SOCKS5Options{})
	tg, known := viaProxy(t, s, px, "")

	// The proxy can't reach the server.
	s.Close()
	_, err := pwConfig(known, "").Dial(bg, tg)
	if de := dialStep(t, err); de.Step != StepProxy || !strings.Contains(err.Error(), "connection refused") ||
		errors.Is(err, ErrProxyAuth) {
		t.Errorf("server down: step %q: %v", de.Step, err)
	}

	// The proxy is down.
	px.Close()
	_, err = pwConfig(known, "").Dial(bg, tg)
	if de := dialStep(t, err); de.Step != StepProxy || errors.Is(err, ErrProxyAuth) {
		t.Errorf("proxy down: step %q: %v", de.Step, err)
	}
}

// A proxy that accepts but never answers fails within the dial timeout.
func TestDialProxyTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close() // held open, silent
		}
	}()
	cfg := pwConfig(&fakeKnown{}, "")
	cfg.DialTimeout = 200 * time.Millisecond
	tg := Target{ProfileID: 1, Host: "10.0.0.5", Port: 22, User: "alice", AuthMethod: store.AuthPassword,
		Proxy: store.Proxy{Type: store.ProxySOCKS5, Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port}}
	start := time.Now()
	_, err = cfg.Dial(bg, tg)
	if de := dialStep(t, err); de.Step != StepProxy {
		t.Errorf("step %q: %v", de.Step, err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
}

// Spec "Test connection": a test through the proxy resolves the folder,
// and a failure at the proxy names that step.
func TestCheckThroughSOCKS5(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	px := startProxy(t, sshtest.SOCKS5Options{Username: "proxy-user", Password: "proxy-pw"})
	tg, known := viaProxy(t, s, px, "proxy-user")
	cfg := pwConfig(known, "")
	cfg.Secrets = fakeSecrets{secret.Password: "secret", secret.ProxyPassword: "proxy-pw"}
	if r := cfg.Check(bg, tg, "~/clipboard", true); !r.OK() || r.Dir != "/home/alice/clipboard" {
		t.Errorf("result %+v", r)
	}
	cfg.Secrets = fakeSecrets{secret.Password: "secret", secret.ProxyPassword: "wrong"}
	if r := cfg.Check(bg, tg, "~/clipboard", true); r.Step != StepProxy {
		t.Errorf("wrong proxy password: %+v", r)
	}
}

func TestTargetOfCarriesProxy(t *testing.T) {
	p := store.NewProfile()
	p.Proxy = store.Proxy{Type: store.ProxySOCKS5, Host: "proxy.lan", Port: 1081, Username: "u"}
	if got := TargetOf(p).Proxy; got != p.Proxy {
		t.Errorf("proxy %+v", got)
	}
	if TargetOf(store.NewProfile()).Proxy.Enabled() {
		t.Error("a new profile connects through a proxy")
	}
}
