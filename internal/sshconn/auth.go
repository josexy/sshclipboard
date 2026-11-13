package sshconn

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/store"
)

// Target is what a connection needs to know about a profile.
type Target struct {
	ProfileID  int64
	Name       string
	Host       string
	Port       int
	User       string
	AuthMethod store.AuthMethod
	KeyPath    string
	Proxy      store.Proxy
}

// TargetOf returns the connection target of a profile.
func TargetOf(p store.Profile) Target {
	return Target{ProfileID: p.ID, Name: p.Name, Host: p.Host, Port: p.Port, User: p.Username,
		AuthMethod: p.AuthMethod, KeyPath: p.KeyPath, Proxy: p.Proxy}
}

// Secrets is where auth methods and proxies read passwords and passphrases.
// *secret.Store implements it.
type Secrets interface {
	Get(profileID int64, kind secret.Kind) (string, error)
}

var (
	// ErrAgentUnavailable is returned when the SSH agent can't be reached.
	ErrAgentUnavailable = errors.New("the SSH agent is unavailable: start the \"OpenSSH Authentication Agent\" service")
	// ErrAgentNoKeys is returned when the agent holds no keys.
	ErrAgentNoKeys = errors.New("the SSH agent has no keys")
)

// SecretNeededError says a profile needs a secret that is not stored, or
// that the credential store couldn't provide: the app asks for it and
// keeps it for the session.
type SecretNeededError struct {
	ProfileID int64
	Kind      secret.Kind
	Err       error
}

func (e *SecretNeededError) Error() string {
	return fmt.Sprintf("the %s of this server is not available: %v", e.Kind.Noun(), e.Err)
}

func (e *SecretNeededError) Unwrap() error { return e.Err }

// KeyError reports a private key file that can't be used, naming the file
// and the reason.
type KeyError struct {
	Path string
	Err  error
}

func (e *KeyError) Error() string { return fmt.Sprintf("private key %s: %v", e.Path, e.Err) }
func (e *KeyError) Unwrap() error { return e.Err }

// errWrongPassphrase is the reason of a KeyError for a passphrase that
// doesn't decrypt the key.
var errWrongPassphrase = errors.New("the passphrase is wrong")

// authMethods builds the auth methods of t. closer, if not nil, releases
// the agent connection once the handshake is over.
func authMethods(t Target, secrets Secrets, dialAgent func() (net.Conn, error)) (methods []ssh.AuthMethod, closer io.Closer, err error) {
	switch t.AuthMethod {
	case store.AuthPassword:
		pw, err := secrets.Get(t.ProfileID, secret.Password)
		if err != nil {
			return nil, nil, &SecretNeededError{ProfileID: t.ProfileID, Kind: secret.Password, Err: err}
		}
		// Servers that only offer keyboard-interactive get the password
		// as the answer to every prompt.
		return []ssh.AuthMethod{
			ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pw
				}
				return answers, nil
			}),
		}, nil, nil

	case store.AuthKey:
		signer, err := loadKey(t, secrets)
		if err != nil {
			return nil, nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil, nil

	case store.AuthAgent:
		if dialAgent == nil {
			dialAgent = DialAgent
		}
		conn, err := dialAgent()
		if err != nil {
			return nil, nil, fmt.Errorf("%w (%v)", ErrAgentUnavailable, err)
		}
		client := agent.NewClient(conn)
		signers, err := client.Signers()
		if err != nil {
			conn.Close()
			return nil, nil, fmt.Errorf("%w (%v)", ErrAgentUnavailable, err)
		}
		if len(signers) == 0 {
			conn.Close()
			return nil, nil, ErrAgentNoKeys
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, conn, nil
	}
	return nil, nil, fmt.Errorf("unknown authentication method %q", t.AuthMethod)
}

// loadKey reads and parses the profile's private key (OpenSSH or PEM),
// decrypting it with the stored passphrase when it is encrypted.
func loadKey(t Target, secrets Secrets) (ssh.Signer, error) {
	pem, err := os.ReadFile(t.KeyPath)
	if err != nil {
		reason := err
		var pe *fs.PathError
		if errors.As(err, &pe) {
			reason = pe.Err // the path is already in KeyError
		}
		if errors.Is(err, fs.ErrNotExist) {
			reason = errors.New("the file does not exist")
		}
		return nil, &KeyError{Path: t.KeyPath, Err: reason}
	}
	signer, err := ssh.ParsePrivateKey(pem)
	var missing *ssh.PassphraseMissingError
	switch {
	case err == nil:
		return signer, nil
	case errors.As(err, &missing):
		pass, gerr := secrets.Get(t.ProfileID, secret.Passphrase)
		if gerr != nil {
			return nil, &SecretNeededError{ProfileID: t.ProfileID, Kind: secret.Passphrase, Err: gerr}
		}
		signer, err = ssh.ParsePrivateKeyWithPassphrase(pem, []byte(pass))
		if err != nil {
			if errors.Is(err, x509.IncorrectPasswordError) {
				return nil, &KeyError{Path: t.KeyPath, Err: errWrongPassphrase}
			}
			return nil, &KeyError{Path: t.KeyPath, Err: err}
		}
		return signer, nil
	default:
		return nil, &KeyError{Path: t.KeyPath, Err: fmt.Errorf("not a valid private key (%v)", err)}
	}
}
