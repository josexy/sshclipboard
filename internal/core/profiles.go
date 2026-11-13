package core

import (
	"context"
	"errors"
	"strings"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/store"
)

// Profile form fields, the keys of FieldErrors.
const (
	FieldName      = "name"
	FieldHost      = "host"
	FieldPort      = "port"
	FieldUsername  = "username"
	FieldKeyPath   = "key_path"
	FieldProxyHost = "proxy_host"
	FieldProxyPort = "proxy_port"
	FieldRemoteDir = "remote_dir"
	FieldFileMode  = "file_mode"
	FieldTemplate  = "copy_template"
)

// Validation problems, the values of FieldErrors. The view translates
// them.
var (
	ErrRequired    = errors.New("required")
	ErrPortRange   = errors.New("port must be between 1 and 65535")
	ErrNameTaken   = store.ErrDuplicateName
	ErrFileMode    = errors.New("file mode must be an octal mode such as 0644")
	ErrAuthMethod  = errors.New("unknown authentication method")
	ErrProxyType   = errors.New("unknown proxy type")
	ErrConflictPol = errors.New("unknown conflict policy")
)

// FieldErrors maps form fields to what is wrong with them.
type FieldErrors map[string]error

func (fe FieldErrors) Error() string {
	var parts []string
	for _, f := range []string{FieldName, FieldHost, FieldPort, FieldUsername, FieldKeyPath, FieldProxyHost,
		FieldProxyPort, FieldRemoteDir, FieldFileMode, FieldTemplate} {
		if err := fe[f]; err != nil {
			parts = append(parts, f+": "+err.Error())
		}
	}
	return "invalid profile: " + strings.Join(parts, "; ")
}

// ValidateProfile checks a profile's fields, as the Servers form shows
// them. extra validates templates (remote directory, copy template); it
// may be nil.
func ValidateProfile(p store.Profile, extra func(store.Profile, FieldErrors)) FieldErrors {
	fe := FieldErrors{}
	if strings.TrimSpace(p.Name) == "" {
		fe[FieldName] = ErrRequired
	}
	if strings.TrimSpace(p.Host) == "" {
		fe[FieldHost] = ErrRequired
	}
	if p.Port < 1 || p.Port > 65535 {
		fe[FieldPort] = ErrPortRange
	}
	if strings.TrimSpace(p.Username) == "" {
		fe[FieldUsername] = ErrRequired
	}
	switch p.AuthMethod {
	case store.AuthPassword, store.AuthAgent:
	case store.AuthKey:
		if strings.TrimSpace(p.KeyPath) == "" {
			fe[FieldKeyPath] = ErrRequired
		}
	default:
		fe[FieldKeyPath] = ErrAuthMethod
	}
	// A disabled proxy's fields are kept but not checked.
	switch p.Proxy.Type {
	case store.ProxyNone, "":
	case store.ProxySOCKS5:
		if strings.TrimSpace(p.Proxy.Host) == "" {
			fe[FieldProxyHost] = ErrRequired
		}
		if p.Proxy.Port < 1 || p.Proxy.Port > 65535 {
			fe[FieldProxyPort] = ErrPortRange
		}
	default:
		fe[FieldProxyHost] = ErrProxyType
	}
	switch p.ConflictPolicy {
	case store.ConflictRename, store.ConflictOverwrite, store.ConflictSkip:
	default:
		fe[FieldRemoteDir] = ErrConflictPol
	}
	if strings.TrimSpace(p.RemoteDir) == "" {
		fe[FieldRemoteDir] = ErrRequired
	}
	if p.FileMode < -1 || p.FileMode > 0o7777 {
		fe[FieldFileMode] = ErrFileMode
	}
	if extra != nil {
		extra(p, fe)
	}
	if len(fe) == 0 {
		return nil
	}
	return fe
}

// SecretInput is a secret field of the Servers form. The form never shows
// a stored secret: Keep leaves it as stored, a value replaces it, and
// Clear removes it.
type SecretInput struct {
	Value string
	Set   bool // the user typed a value (possibly empty, which clears)
}

// ProfileInput is what the Servers form saves.
type ProfileInput struct {
	Profile       store.Profile
	Password      SecretInput
	Passphrase    SecretInput
	ProxyPassword SecretInput
}

// secret returns the form's input for a kind of secret.
func (in ProfileInput) secret(kind secret.Kind) SecretInput {
	switch kind {
	case secret.Passphrase:
		return in.Passphrase
	case secret.ProxyPassword:
		return in.ProxyPassword
	}
	return in.Password
}

// SaveResult says what SaveProfile stored.
type SaveResult struct {
	Profile store.Profile
	// SessionOnly is true when the credential store failed and a secret
	// is kept for this session only.
	SessionOnly bool
}

// saveProfile validates and stores a profile, and its secrets in the
// credential store only: the database never sees them. A new profile
// (ID 0) is created.
func saveProfile(ctx context.Context, st *store.Store, sec *secret.Store, in ProfileInput,
	extra func(store.Profile, FieldErrors)) (SaveResult, error) {
	p := in.Profile
	if fe := ValidateProfile(p, extra); fe != nil {
		return SaveResult{}, fe
	}
	var err error
	if p.ID == 0 {
		p, err = st.Profiles.Create(ctx, p)
	} else {
		err = st.Profiles.Update(ctx, p)
	}
	if errors.Is(err, store.ErrDuplicateName) {
		return SaveResult{}, FieldErrors{FieldName: ErrNameTaken}
	}
	if err != nil {
		return SaveResult{}, err
	}
	res := SaveResult{Profile: p}
	for _, kind := range secret.Kinds {
		si := in.secret(kind)
		if !si.Set {
			continue
		}
		if si.Value == "" {
			if err := sec.Clear(p.ID, kind); err != nil {
				return res, err
			}
			continue
		}
		if sec.Set(p.ID, kind, si.Value) {
			res.SessionOnly = true
		}
	}
	return res, nil
}
