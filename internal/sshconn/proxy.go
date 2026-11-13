package sshconn

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"golang.org/x/net/proxy"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/store"
)

// ErrProxyAuth is the cause of a failure to sign in to the proxy: it
// refused the username or password, or it needs a sign-in and the profile
// has no username. Unlike the proxy's network failures, retrying doesn't
// help.
var ErrProxyAuth = errors.New("sign-in refused")

// dialNet opens the TCP connection to addr, the server's host and port:
// directly, or through t's proxy. Both are bounded by the dial timeout.
func (c *Config) dialNet(ctx context.Context, t Target, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: c.dialTimeout()}
	if !t.Proxy.Enabled() {
		nc, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, &DialError{Step: StepConnect, Err: err}
		}
		return nc, nil
	}
	pd, err := proxyDialer(t, c.Secrets, d)
	if err != nil {
		return nil, &DialError{Step: StepProxy, Err: err}
	}
	// The timeout covers the proxy's handshake, not only its TCP connect.
	dctx, cancel := context.WithTimeout(ctx, c.dialTimeout())
	defer cancel()
	nc, err := pd.DialContext(dctx, "tcp", addr)
	if err != nil {
		if ctx.Err() != nil {
			return nil, &DialError{Step: StepConnect, Err: ctx.Err()}
		}
		return nil, &DialError{Step: StepProxy, Err: proxyErr(t.Proxy, err)}
	}
	return nc, nil
}

// proxyDialer returns the dialer of t's proxy, signed in with its stored
// password when it has a username. The proxy, not this computer,
// resolves the server's host name.
func proxyDialer(t Target, secrets Secrets, forward *net.Dialer) (proxy.ContextDialer, error) {
	px := t.Proxy
	if px.Type != store.ProxySOCKS5 {
		return nil, fmt.Errorf("unknown proxy type %q", px.Type)
	}
	var auth *proxy.Auth
	if px.Username != "" {
		pw, err := secrets.Get(t.ProfileID, secret.ProxyPassword)
		if err != nil {
			return nil, &SecretNeededError{ProfileID: t.ProfileID, Kind: secret.ProxyPassword, Err: err}
		}
		auth = &proxy.Auth{User: px.Username, Password: pw}
	}
	d, err := proxy.SOCKS5("tcp", proxyAddr(px), auth, forward)
	if err != nil {
		return nil, err
	}
	// SOCKS5 dialers always dial with a context.
	return d.(proxy.ContextDialer), nil
}

func proxyAddr(px store.Proxy) string {
	return net.JoinHostPort(px.Host, strconv.Itoa(px.Port))
}

// proxyErr names the proxy in a failure to connect through it, drops the
// SOCKS library's own "socks connect tcp a->b:" prefix, and marks refused
// sign-ins with ErrProxyAuth.
func proxyErr(px store.Proxy, err error) error {
	var oe *net.OpError
	if errors.As(err, &oe) && oe.Op == "socks connect" && oe.Err != nil {
		err = oe.Err
	}
	if signInRefused(err) {
		return fmt.Errorf("SOCKS5 proxy %s: %w: %v", proxyAddr(px), ErrProxyAuth, err)
	}
	return fmt.Errorf("SOCKS5 proxy %s: %w", proxyAddr(px), err)
}

// signInRefused recognizes the SOCKS library's sign-in failures, which are
// plain errors, by their text.
func signInRefused(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) {
		return false
	}
	msg := err.Error()
	for _, s := range []string{"no acceptable authentication methods", "authentication failed", "invalid username/password"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
