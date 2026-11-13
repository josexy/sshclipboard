package core

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// proxied starts a SOCKS5 proxy with a sign-in that alone resolves
// "dev-box.internal" to the test server, whose key is trusted under that
// name.
func proxied(t *testing.T, e *env) (*sshtest.SOCKS5, func(*store.Profile)) {
	t.Helper()
	px := sshtest.StartSOCKS5(t, sshtest.SOCKS5Options{Username: "proxy-user", Password: "proxy-pw",
		Hosts: map[string]string{"dev-box.internal": e.srv.Host}})
	if err := e.st.KnownHosts.Put(ctx, sshconn.KnownHostOf("dev-box.internal", e.srv.Port, e.srv.HostKey())); err != nil {
		t.Fatal(err)
	}
	return px, func(p *store.Profile) {
		p.Host = "dev-box.internal"
		p.Proxy = store.Proxy{Type: store.ProxySOCKS5, Host: px.Host, Port: px.Port, Username: "proxy-user"}
	}
}

// Uploads go through the profile's proxy. A proxy password the credential
// store doesn't have is asked for once, not retried, and providing it
// uploads the entry.
func TestUploadThroughProxyAsksForItsPassword(t *testing.T) {
	e := newEnv(t, envOpts{})
	px, viaProxy := proxied(t, e)
	p := e.addProfile("dev-box", viaProxy)
	if e.c.State().Saved[p.ID].ProxyPassword {
		t.Error("a proxy password is saved")
	}

	e.cb.copy(image1280(t))
	s := e.waitFor("the proxy password prompt", func(s *State) bool { return s.SecretPrompt != nil })
	if sp := s.SecretPrompt; sp.ProfileID != p.ID || sp.Kind != string(secret.ProxyPassword) {
		t.Errorf("prompt %+v", sp)
	}
	r := e.settled(1)[0]
	if r.Status != store.StatusFailed || r.Attempts != 1 || !strings.Contains(r.Error, "the proxy password of") {
		t.Fatalf("without the proxy password: %+v", r)
	}

	if err := call(t, func(d func(error)) { e.c.ProvideSecret(p.ID, secret.ProxyPassword, "proxy-pw", d) }); err != nil {
		t.Fatal(err)
	}
	r = e.settled(1)[0]
	if r.Status != store.StatusUploaded {
		t.Fatalf("after providing the proxy password: %+v", r)
	}
	if _, err := os.Stat(e.srv.Local(r.RemotePath)); err != nil {
		t.Errorf("not on the server: %v", err)
	}
	want := net.JoinHostPort("dev-box.internal", strconv.Itoa(e.srv.Port))
	if got := px.Targets(); len(got) == 0 || got[len(got)-1] != want {
		t.Errorf("the proxy connected to %q, want %q", got, want)
	}
}

// Test connection uses the proxy password typed in the form, before it is
// saved.
func TestTestConnectionThroughProxy(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, viaProxy := proxied(t, e)
	p := store.NewProfile()
	p.Name, p.Port, p.Username = "dev-box", e.srv.Port, "alice"
	viaProxy(&p)
	test := func(proxyPW string) sshconn.CheckResult {
		ch := make(chan sshconn.CheckResult, 1)
		e.c.TestConnection(ProfileInput{Profile: p, Password: SecretInput{Value: "secret", Set: true},
			ProxyPassword: SecretInput{Value: proxyPW, Set: true}}, func(r sshconn.CheckResult) { ch <- r })
		select {
		case r := <-ch:
			return r
		case <-time.After(10 * time.Second):
			t.Fatal("the test did not finish")
		}
		return sshconn.CheckResult{}
	}
	if r := test("proxy-pw"); !r.OK() || r.Dir != "/home/alice/clipboard" {
		t.Errorf("result %+v", r)
	}
	if r := test("wrong"); r.Step != sshconn.StepProxy || !strings.Contains(r.Err.Error(), "authentication failed") {
		t.Errorf("wrong proxy password: %+v", r)
	}
}

// The proxy password is saved, flagged as saved, copied with Duplicate,
// and removed with the profile.
func TestProxyPasswordLifecycle(t *testing.T) {
	e := newEnv(t, envOpts{})
	_, viaProxy := proxied(t, e)
	p := store.NewProfile()
	p.Name, p.Port, p.Username = "dev-box", e.srv.Port, "alice"
	viaProxy(&p)
	ch := make(chan SaveResult, 1)
	e.c.SaveProfile(ProfileInput{Profile: p, ProxyPassword: SecretInput{Value: "proxy-pw", Set: true}},
		func(r SaveResult, err error) {
			if err != nil {
				t.Errorf("save: %v", err)
			}
			ch <- r
		})
	saved := (<-ch).Profile
	if saved.Proxy != p.Proxy {
		t.Errorf("saved proxy %+v, want %+v", saved.Proxy, p.Proxy)
	}
	if string(e.creds[secret.Key(saved.ID, secret.ProxyPassword)]) != "proxy-pw" {
		t.Errorf("credential store %q", e.creds)
	}
	if s := e.c.State(); !s.Saved[saved.ID].ProxyPassword || s.Saved[saved.ID].Password {
		t.Errorf("saved secrets %+v", s.Saved[saved.ID])
	}

	if err := call(t, func(d func(error)) { e.c.DuplicateProfile(saved.ID, d) }); err != nil {
		t.Fatal(err)
	}
	for _, q := range e.c.State().Profiles {
		if q.Proxy != p.Proxy {
			t.Errorf("%s proxy %+v", q.Name, q.Proxy)
		}
		if pw, err := e.sec.Get(q.ID, secret.ProxyPassword); err != nil || pw != "proxy-pw" {
			t.Errorf("%s proxy password %q %v", q.Name, pw, err)
		}
		if err := call(t, func(d func(error)) { e.c.DeleteProfile(q.ID, d) }); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.creds) != 0 {
		t.Errorf("secrets left: %q", e.creds)
	}
}
