package secret

import (
	"errors"
	"fmt"
	"testing"

	"github.com/egoist/mygo"
)

// fakeBackend is an in-memory credential store; fail makes every call
// fail with err.
type fakeBackend struct {
	m    map[string][]byte
	fail error
}

func newFake() *fakeBackend { return &fakeBackend{m: map[string][]byte{}} }

func (f *fakeBackend) Set(k string, v []byte) error {
	if f.fail != nil {
		return f.fail
	}
	f.m[k] = append([]byte(nil), v...)
	return nil
}

func (f *fakeBackend) Get(k string) ([]byte, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	v, ok := f.m[k]
	if !ok {
		return nil, mygo.ErrSecretNotFound
	}
	return v, nil
}

func (f *fakeBackend) Delete(k string) error {
	if f.fail != nil {
		return f.fail
	}
	delete(f.m, k)
	return nil
}

var unsupported = fmt.Errorf("mygo: secrets: %w", errors.ErrUnsupported)

func TestSetGet(t *testing.T) {
	b := newFake()
	s := New(b)
	if s.Set(7, Password, "hunter2") {
		t.Error("a working store reported session-only")
	}
	if string(b.m["profile/7/password"]) != "hunter2" {
		t.Errorf("backend %v", b.m)
	}
	if v, err := s.Get(7, Password); err != nil || v != "hunter2" {
		t.Errorf("get %q %v", v, err)
	}
	s.Set(7, Passphrase, "pp")
	if v, _ := s.Get(7, Passphrase); v != "pp" || string(b.m["profile/7/passphrase"]) != "pp" {
		t.Errorf("passphrase %q", v)
	}
	if !s.Has(7, Password) || s.Has(8, Password) {
		t.Error("Has")
	}
}

func TestNotFound(t *testing.T) {
	s := New(newFake())
	if _, err := s.Get(1, Password); !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// Spec "Credential store unavailable": the secret is used for the session
// only, and the caller learns it will not be remembered.
func TestUnsupportedStoreFallsBackToSession(t *testing.T) {
	b := newFake()
	b.fail = unsupported
	s := New(b)
	if _, err := s.Get(3, Password); !errors.Is(err, ErrUnavailable) {
		t.Errorf("get before any secret: %v, want ErrUnavailable so the app asks", err)
	}
	if !s.Set(3, Password, "typed") {
		t.Error("Set into a failing store did not report session-only")
	}
	if v, err := s.Get(3, Password); err != nil || v != "typed" {
		t.Errorf("session secret %q %v", v, err)
	}
	s.SetSession(4, Passphrase, "asked")
	if v, _ := s.Get(4, Passphrase); v != "asked" {
		t.Errorf("SetSession %q", v)
	}
	// A new session forgets it.
	if _, err := New(b).Get(3, Password); err == nil {
		t.Error("a session-only secret outlived its session")
	}
	if err := s.Delete(3); err != nil {
		t.Errorf("deleting from an unsupported store: %v", err)
	}
	if _, err := s.Get(3, Password); err == nil {
		t.Error("deleted session secret still there")
	}
}

func TestDeleteRemovesEverySecret(t *testing.T) {
	b := newFake()
	s := New(b)
	s.Set(5, Password, "a")
	s.Set(5, Passphrase, "b")
	s.Set(5, ProxyPassword, "c")
	s.Set(6, Password, "other")
	if string(b.m["profile/5/proxy-password"]) != "c" {
		t.Fatalf("stored %v", b.m)
	}
	if err := s.Delete(5); err != nil {
		t.Fatal(err)
	}
	if len(b.m) != 1 || string(b.m["profile/6/password"]) != "other" {
		t.Errorf("left %v", b.m)
	}
	for _, kind := range []Kind{Password, Passphrase, ProxyPassword} {
		if _, err := s.Get(5, kind); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s after delete: %v", kind, err)
		}
	}
	if err := s.Delete(5); err != nil {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestStoreRecoversAfterFailure(t *testing.T) {
	b := newFake()
	b.fail = errors.New("locked")
	s := New(b)
	s.Set(1, Password, "old")
	b.fail = nil
	if s.Set(1, Password, "new") {
		t.Error("store works again but Set reported session-only")
	}
	if v, _ := s.Get(1, Password); v != "new" {
		t.Errorf("got %q: the stale session copy shadows the store", v)
	}
}

func TestSystemBackendIsMygoSecrets(t *testing.T) {
	var _ Backend = mygo.Secrets
	if Key(12, Passphrase) != "profile/12/passphrase" {
		t.Error(Key(12, Passphrase))
	}
}
