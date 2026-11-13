package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/sqlite"
)

// FileName is the database's file name inside the user data folder.
const FileName = "sshclipboard.db"

// conn is the narrow driver surface the store uses. *sqlite.DB implements
// it; keeping every SQL call behind it lets the driver be swapped (design
// D5) and lets tests inject write failures.
type conn interface {
	Execute(ctx context.Context, sql string, args ...any) (sqlite.Result, error)
	Query(ctx context.Context, sql string, args ...any) (sqlite.Rows, error)
	Transaction(ctx context.Context, statements []sqlite.Statement) ([]sqlite.Result, error)
	Close() error
}

// Store is the app's database. It is safe from any goroutine: the driver
// serializes operations on its one connection.
type Store struct {
	db   conn
	path string

	// RecoveredFrom is where Open moved an unreadable database before
	// starting fresh, or "" when it opened the file it found.
	RecoveredFrom string

	Settings   SettingsRepo
	Profiles   ProfilesRepo
	Rules      RulesRepo
	Naming     NamingRepo
	KnownHosts KnownHostsRepo
	Counters   CountersRepo
	History    HistoryRepo
}

// ErrNewerSchema is returned (wrapped in *NewerSchemaError) when the
// database was created by a newer version of the app.
var ErrNewerSchema = errors.New("the data was created by a newer version of SSHClipboard")

// NewerSchemaError reports a database whose schema version is newer than
// this app knows. The database is left untouched.
type NewerSchemaError struct {
	Found, Supported int
}

func (e *NewerSchemaError) Error() string {
	return fmt.Sprintf("%v (schema version %d, this version supports up to %d)", ErrNewerSchema, e.Found, e.Supported)
}

func (e *NewerSchemaError) Unwrap() error { return ErrNewerSchema }

// errCorrupt marks a database that failed its integrity check or is not a
// database at all.
var errCorrupt = errors.New("database is unreadable")

// OpenDir opens (or creates) sshclipboard.db in dir, the app's per-user
// data folder, creating dir when missing.
func OpenDir(ctx context.Context, dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return Open(ctx, filepath.Join(dir, FileName))
}

// Open opens the database at path: it checks its integrity, applies pending
// migrations, and seeds defaults into a new database. An unreadable file is
// renamed to <path>.corrupt-<timestamp> and replaced by a fresh database,
// and RecoveredFrom names the renamed file.
func Open(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, schema{appMigrations, seedStatements}, time.Now)
}

// schema is the migrations a database is brought up to, and the defaults
// a new one is seeded with.
type schema struct {
	migrations []migration
	seed       func() []sqlite.Statement
}

func openWith(ctx context.Context, path string, sc schema, now func() time.Time) (*Store, error) {
	st, err := openOnce(ctx, path, sc)
	if err == nil || !errors.Is(err, errCorrupt) {
		return st, err
	}
	moved := fmt.Sprintf("%s.corrupt-%s", path, now().Format("20060102-150405"))
	if rerr := os.Rename(path, moved); rerr != nil {
		return nil, fmt.Errorf("%w; moving it aside failed: %v", err, rerr)
	}
	// A stale WAL or shared-memory file belongs to the old database.
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, serr := os.Stat(path + suffix); serr == nil {
			_ = os.Rename(path+suffix, moved+suffix)
		}
	}
	st, err = openOnce(ctx, path, sc)
	if err != nil {
		return nil, err
	}
	st.RecoveredFrom = moved
	return st, nil
}

func openOnce(ctx context.Context, path string, sc schema) (*Store, error) {
	db, err := sqlite.Open(ctx, path, sqlite.OpenOptions{})
	if err != nil {
		return nil, classify(err)
	}
	st := newStore(db, path)
	if err := st.init(ctx, sc); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func newStore(db conn, path string) *Store {
	st := &Store{db: db, path: path}
	st.Settings = SettingsRepo{st}
	st.Profiles = ProfilesRepo{st}
	st.Rules = RulesRepo{st}
	st.Naming = NamingRepo{st}
	st.KnownHosts = KnownHostsRepo{st}
	st.Counters = CountersRepo{st}
	st.History = HistoryRepo{st}
	return st
}

func (s *Store) init(ctx context.Context, sc schema) error {
	rows, err := s.db.Query(ctx, "PRAGMA quick_check")
	if err != nil {
		return classify(err)
	}
	if len(rows.Values) == 0 || str(rows.Values[0][0]) != "ok" {
		return fmt.Errorf("%w: integrity check failed", errCorrupt)
	}
	version, err := s.userVersion(ctx)
	if err != nil {
		return classify(err)
	}
	// Refuse before changing anything, the journal mode included.
	if latest := latestVersion(sc.migrations); version > latest {
		return &NewerSchemaError{Found: version, Supported: latest}
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := s.db.Query(ctx, pragma); err != nil {
			return classify(err)
		}
	}
	return s.migrate(ctx, sc.migrations, version, sc.seed)
}

func (s *Store) userVersion(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, "PRAGMA user_version")
	if err != nil {
		return 0, err
	}
	if len(rows.Values) == 0 {
		return 0, nil
	}
	return int(i64(rows.Values[0][0])), nil
}

// SchemaVersion returns the database's recorded schema version.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) { return s.userVersion(ctx) }

// Path returns the database file's path.
func (s *Store) Path() string { return s.path }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// classify marks SQLite's "not a database" and "corrupt" errors as
// errCorrupt so Open recovers from them.
func classify(err error) error {
	var e *sqlite.Error
	if errors.As(err, &e) && (e.Code == 26 /* SQLITE_NOTADB */ || e.Code == 11 /* SQLITE_CORRUPT */) {
		return fmt.Errorf("%w: %v", errCorrupt, err)
	}
	return err
}

// Value conversions for driver results: INTEGER is int64, REAL float64,
// TEXT string, BLOB []byte, NULL nil.

func i64(v any) int64 {
	switch v := v.(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

func str(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

func boolean(v any) bool { return i64(v) != 0 }

func millis(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func timeOf(v any) time.Time {
	if v == nil {
		return time.Time{}
	}
	return time.UnixMilli(i64(v))
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Pragma returns a pragma's value as text, for diagnostics and tests.
func (s *Store) Pragma(ctx context.Context, name string) (string, error) {
	rows, err := s.db.Query(ctx, "PRAGMA "+name)
	if err != nil || len(rows.Values) == 0 {
		return "", err
	}
	return fmt.Sprint(rows.Values[0][0]), nil
}

// OpenUserData opens sshclipboard.db in the app's per-user data folder,
// mygo.App.Path(mygo.PathUserData) (%APPDATA%\SSHClipboard on Windows).
func OpenUserData(ctx context.Context) (*Store, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return nil, err
	}
	return OpenDir(ctx, dir)
}
