package sshconn

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/josexy/sshclipboard/internal/store"
)

// KnownHosts is the app's store of trusted host keys.
// store.KnownHostsRepo implements it.
type KnownHosts interface {
	Get(ctx context.Context, host string, port int) (store.KnownHost, bool, error)
}

// HostKeyError reports a host key that is not trusted: unknown (Known is
// nil) or different from the trusted key (Changed). The handshake fails
// at once; the app asks the user and retries after Trust or Replace.
type HostKeyError struct {
	Host  string
	Port  int
	Key   ssh.PublicKey
	Known *store.KnownHost // the trusted key, when the key changed
	// FromOpenSSH is true when Known came from ~/.ssh/known_hosts.
	FromOpenSSH bool
}

// Changed tells whether a different key was trusted for this host.
func (e *HostKeyError) Changed() bool { return e.Known != nil }

// Fingerprint returns the presented key's SHA-256 fingerprint.
func (e *HostKeyError) Fingerprint() string { return ssh.FingerprintSHA256(e.Key) }

// KeyType returns the presented key's type, such as "ssh-ed25519".
func (e *HostKeyError) KeyType() string { return e.Key.Type() }

// KnownHost returns the presented key as a record to trust.
func (e *HostKeyError) KnownHost() store.KnownHost {
	return KnownHostOf(e.Host, e.Port, e.Key)
}

func (e *HostKeyError) Error() string {
	if e.Changed() {
		return fmt.Sprintf("host key changed for %s: trusted %s, presented %s %s",
			net.JoinHostPort(e.Host, strconv.Itoa(e.Port)), e.Known.Fingerprint, e.KeyType(), e.Fingerprint())
	}
	return fmt.Sprintf("unknown host key for %s: %s %s",
		net.JoinHostPort(e.Host, strconv.Itoa(e.Port)), e.KeyType(), e.Fingerprint())
}

// KnownHostOf returns key as a known_hosts record.
func KnownHostOf(host string, port int, key ssh.PublicKey) store.KnownHost {
	return store.KnownHost{Host: host, Port: port, KeyType: key.Type(),
		KeyBase64: base64.StdEncoding.EncodeToString(key.Marshal()), Fingerprint: ssh.FingerprintSHA256(key),
		AddedAt: time.Now()}
}

// DefaultOpenSSHKnownHosts returns %USERPROFILE%\.ssh\known_hosts.
func DefaultOpenSSHKnownHosts() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "known_hosts")
}

// hostKeyCallback verifies a server's key without blocking: the app's
// trusted keys first, then OpenSSH's known_hosts file (read only), and a
// *HostKeyError for an unknown or changed key.
func hostKeyCallback(ctx context.Context, db KnownHosts, openSSHFile string, host string, port int) ssh.HostKeyCallback {
	return func(_ string, remote net.Addr, key ssh.PublicKey) error {
		if db != nil {
			kh, ok, err := db.Get(ctx, host, port)
			if err != nil {
				return err
			}
			if ok {
				trusted, err := base64.StdEncoding.DecodeString(kh.KeyBase64)
				if err == nil && bytes.Equal(trusted, key.Marshal()) {
					return nil
				}
				return &HostKeyError{Host: host, Port: port, Key: key, Known: &kh}
			}
		}
		if openSSHFile != "" {
			if _, err := os.Stat(openSSHFile); err == nil {
				cb, err := knownhosts.New(openSSHFile)
				if err != nil {
					return fmt.Errorf("reading %s: %w", openSSHFile, err)
				}
				addr := net.JoinHostPort(host, strconv.Itoa(port))
				err = cb(addr, remote, key)
				var ke *knownhosts.KeyError
				var re *knownhosts.RevokedError
				switch {
				case err == nil:
					return nil
				case errors.As(err, &re):
					return err
				case errors.As(err, &ke) && len(ke.Want) > 0:
					want := ke.Want[0].Key
					known := KnownHostOf(host, port, want)
					return &HostKeyError{Host: host, Port: port, Key: key, Known: &known, FromOpenSSH: true}
				}
			}
		}
		return &HostKeyError{Host: host, Port: port, Key: key}
	}
}
