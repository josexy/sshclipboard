package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrDuplicateName is returned when a profile's name is already used by
// another profile (names compare case-insensitively).
var ErrDuplicateName = errors.New("a server with this name already exists")

// ProfilesRepo stores server profiles.
type ProfilesRepo struct{ s *Store }

const profileCols = `id, name, host, port, username, auth_method, key_path, proxy_type, proxy_host,
	proxy_port, proxy_username, remote_dir, create_dirs, conflict_policy, file_mode, copy_back, copy_template,
	copy_separator, shell_quote, created_at, updated_at`

func scanProfile(row []any) Profile {
	p := Profile{
		ID:         i64(row[0]),
		Name:       str(row[1]),
		Host:       str(row[2]),
		Port:       int(i64(row[3])),
		Username:   str(row[4]),
		AuthMethod: AuthMethod(str(row[5])),
		KeyPath:    str(row[6]),
		Proxy: Proxy{
			Type:     ProxyType(str(row[7])),
			Host:     str(row[8]),
			Port:     int(i64(row[9])),
			Username: str(row[10]),
		},
		RemoteDir:      str(row[11]),
		CreateDirs:     boolean(row[12]),
		ConflictPolicy: ConflictPolicy(str(row[13])),
		FileMode:       -1,
		CopyBack:       boolean(row[15]),
		CopyTemplate:   str(row[16]),
		CopySeparator:  str(row[17]),
		ShellQuote:     boolean(row[18]),
		CreatedAt:      timeOf(row[19]),
		UpdatedAt:      timeOf(row[20]),
	}
	if row[14] != nil {
		p.FileMode = int(i64(row[14]))
	}
	return p
}

// proxyTypeArg stores an unset proxy type as "none".
func proxyTypeArg(t ProxyType) string {
	if t == "" {
		return string(ProxyNone)
	}
	return string(t)
}

func fileModeArg(mode int) any {
	if mode < 0 {
		return nil
	}
	return mode
}

// List returns every profile, by name.
func (r ProfilesRepo) List(ctx context.Context) ([]Profile, error) {
	rows, err := r.s.db.Query(ctx, "SELECT "+profileCols+" FROM profiles ORDER BY name COLLATE NOCASE, id")
	if err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(rows.Values))
	for _, row := range rows.Values {
		out = append(out, scanProfile(row))
	}
	return out, nil
}

// Get returns the profile with id, or ErrNotFound.
func (r ProfilesRepo) Get(ctx context.Context, id int64) (Profile, error) {
	rows, err := r.s.db.Query(ctx, "SELECT "+profileCols+" FROM profiles WHERE id = ?", id)
	if err != nil {
		return Profile{}, err
	}
	if len(rows.Values) == 0 {
		return Profile{}, ErrNotFound
	}
	return scanProfile(rows.Values[0]), nil
}

// Create inserts p and returns it with its ID and timestamps.
func (r ProfilesRepo) Create(ctx context.Context, p Profile) (Profile, error) {
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	res, err := r.s.db.Execute(ctx, `INSERT INTO profiles (name, host, port, username, auth_method, key_path,
		proxy_type, proxy_host, proxy_port, proxy_username, remote_dir, create_dirs, conflict_policy, file_mode,
		copy_back, copy_template, copy_separator, shell_quote, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(p.Name), strings.TrimSpace(p.Host), p.Port, p.Username, string(p.AuthMethod), p.KeyPath,
		proxyTypeArg(p.Proxy.Type), strings.TrimSpace(p.Proxy.Host), p.Proxy.Port, p.Proxy.Username,
		p.RemoteDir, p.CreateDirs, string(p.ConflictPolicy), fileModeArg(p.FileMode), p.CopyBack,
		p.CopyTemplate, p.CopySeparator, p.ShellQuote, millis(now), millis(now))
	if err != nil {
		return Profile{}, profileErr(err)
	}
	p.ID = res.LastInsertID
	p.Name, p.Host = strings.TrimSpace(p.Name), strings.TrimSpace(p.Host)
	p.Proxy.Type, p.Proxy.Host = ProxyType(proxyTypeArg(p.Proxy.Type)), strings.TrimSpace(p.Proxy.Host)
	return p, nil
}

// Update saves every field of p.
func (r ProfilesRepo) Update(ctx context.Context, p Profile) error {
	res, err := r.s.db.Execute(ctx, `UPDATE profiles SET name = ?, host = ?, port = ?, username = ?,
		auth_method = ?, key_path = ?, proxy_type = ?, proxy_host = ?, proxy_port = ?, proxy_username = ?,
		remote_dir = ?, create_dirs = ?, conflict_policy = ?, file_mode = ?, copy_back = ?, copy_template = ?,
		copy_separator = ?, shell_quote = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(p.Name), strings.TrimSpace(p.Host), p.Port, p.Username, string(p.AuthMethod), p.KeyPath,
		proxyTypeArg(p.Proxy.Type), strings.TrimSpace(p.Proxy.Host), p.Proxy.Port, p.Proxy.Username,
		p.RemoteDir, p.CreateDirs, string(p.ConflictPolicy), fileModeArg(p.FileMode), p.CopyBack,
		p.CopyTemplate, p.CopySeparator, p.ShellQuote, millis(time.Now()), p.ID)
	if err != nil {
		return profileErr(err)
	}
	if res.Changes == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the profile, the trusted key of its host and port unless
// another profile still uses them, and clears the active profile setting
// when it named this profile, in one transaction. Its counters go with it
// (ON DELETE CASCADE), and its history keeps the profile's name.
func (r ProfilesRepo) Delete(ctx context.Context, id int64) error {
	p, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	_, err = r.s.db.Transaction(ctx, []sqlite.Statement{
		{SQL: `DELETE FROM known_hosts WHERE host = ? AND port = ?
			AND NOT EXISTS (SELECT 1 FROM profiles WHERE host = ? AND port = ? AND id <> ?)`,
			Args: []any{p.Host, p.Port, p.Host, p.Port, id}},
		{SQL: "UPDATE settings SET value = '0' WHERE key = ? AND CAST(value AS INTEGER) = ?",
			Args: []any{KeyActiveProfile, id}},
		{SQL: "DELETE FROM profiles WHERE id = ?", Args: []any{id}},
	})
	return err
}

// profileErr maps the unique-name constraint to ErrDuplicateName.
func profileErr(err error) error {
	var e *sqlite.Error
	if errors.As(err, &e) && e.ExtendedCode == 2067 /* SQLITE_CONSTRAINT_UNIQUE */ &&
		strings.Contains(e.Message, "profiles.name") {
		return ErrDuplicateName
	}
	return err
}
