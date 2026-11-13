package secret

import (
	"errors"
	"fmt"
	"sync"

	"github.com/egoist/mygo"
)

// Kind names one of a profile's secrets.
type Kind string

const (
	Password   Kind = "password"
	Passphrase Kind = "passphrase"
)

// Backend is a credential store. mygo.Secrets implements it.
type Backend interface {
	Set(key string, value []byte) error
	Get(key string) ([]byte, error)
	Delete(key string) error
}

// ErrNotFound is returned by Get when no secret is stored.
var ErrNotFound = errors.New("secret not found")

// ErrUnavailable is returned (wrapped) by Get when the credential store
// failed and the session holds no copy: the caller should ask the user for
// the secret and keep it with SetSession.
var ErrUnavailable = errors.New("the credential store is unavailable")

// Store keeps profile secrets in a credential store under
// profile/<id>/password and profile/<id>/passphrase. When the store fails,
// as when it is unsupported, secrets are kept in memory for this session
// only. It is safe from any goroutine.
type Store struct {
	backend Backend

	mu      sync.Mutex
	session map[string]string
}

// New returns a Store over backend.
func New(backend Backend) *Store {
	return &Store{backend: backend, session: map[string]string{}}
}

// NewSystem returns a Store over the OS credential store (the Windows
// Credential Manager).
func NewSystem() *Store { return New(mygo.Secrets) }

// Key returns the credential store key of a profile's secret.
func Key(profileID int64, kind Kind) string {
	return fmt.Sprintf("profile/%d/%s", profileID, kind)
}

// Set stores a secret. When the credential store fails, the secret is kept
// for this session only and sessionOnly is true, so the UI can say it will
// not be remembered.
func (s *Store) Set(profileID int64, kind Kind, value string) (sessionOnly bool) {
	key := Key(profileID, kind)
	err := s.backend.Set(key, []byte(value))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.session[key] = value
		return true
	}
	delete(s.session, key)
	return false
}

// SetSession keeps a secret the user typed when asked for it, for this
// session only.
func (s *Store) SetSession(profileID int64, kind Kind, value string) {
	s.mu.Lock()
	s.session[Key(profileID, kind)] = value
	s.mu.Unlock()
}

// Get returns a secret: the session's copy if there is one, else the
// credential store's. It fails with ErrNotFound when none is stored, and
// with ErrUnavailable when the store failed.
func (s *Store) Get(profileID int64, kind Kind) (string, error) {
	key := Key(profileID, kind)
	s.mu.Lock()
	v, ok := s.session[key]
	s.mu.Unlock()
	if ok {
		return v, nil
	}
	b, err := s.backend.Get(key)
	switch {
	case err == nil:
		return string(b), nil
	case errors.Is(err, mygo.ErrSecretNotFound):
		return "", ErrNotFound
	default:
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
}

// Has tells whether a secret is stored, for the form's "saved" hint.
func (s *Store) Has(profileID int64, kind Kind) bool {
	_, err := s.Get(profileID, kind)
	return err == nil
}

// Clear removes one secret, as when a profile's password field is
// emptied.
func (s *Store) Clear(profileID int64, kind Kind) error {
	key := Key(profileID, kind)
	s.mu.Lock()
	delete(s.session, key)
	s.mu.Unlock()
	if err := s.backend.Delete(key); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	return nil
}

// Delete removes every secret of a profile, as when it is deleted. A store
// that is unsupported has nothing to delete.
func (s *Store) Delete(profileID int64) error {
	var errs []error
	for _, kind := range []Kind{Password, Passphrase} {
		errs = append(errs, s.Clear(profileID, kind))
	}
	return errors.Join(errs...)
}
