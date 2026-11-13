package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/plugins/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migration is one schema step: the statements of migrations/NNNN_name.sql.
type migration struct {
	Version    int
	Name       string
	Statements []string
}

var appMigrations = mustLoadMigrations(migrationFiles, "migrations")

func mustLoadMigrations(fsys fs.FS, dir string) []migration {
	migs, err := loadMigrations(fsys, dir)
	if err != nil {
		panic(err)
	}
	return migs
}

// loadMigrations reads NNNN_name.sql files, which must be numbered 1, 2, …
// without gaps, and splits each into single statements, because the driver
// runs one statement per call and rejects BEGIN/COMMIT.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var migs []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || path.Ext(name) != ".sql" {
			continue
		}
		num, rest, ok := strings.Cut(strings.TrimSuffix(name, ".sql"), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil || v <= 0 {
			return nil, fmt.Errorf("migration %s: name must be NNNN_name.sql", name)
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, err
		}
		stmts, err := splitSQL(string(body))
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", name, err)
		}
		migs = append(migs, migration{Version: v, Name: rest, Statements: stmts})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].Version < migs[j].Version })
	for i, m := range migs {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migration %04d_%s: expected version %d", m.Version, m.Name, i+1)
		}
	}
	return migs, nil
}

func latestVersion(migs []migration) int {
	if len(migs) == 0 {
		return 0
	}
	return migs[len(migs)-1].Version
}

// MigrationError reports a migration that failed and was rolled back; the
// database stays at its previous version.
type MigrationError struct {
	Version int
	Name    string
	Err     error
}

func (e *MigrationError) Error() string {
	return fmt.Sprintf("schema migration %04d_%s failed: %v", e.Version, e.Name, e.Err)
}

func (e *MigrationError) Unwrap() error { return e.Err }

// migrate applies each migration newer than from, in order, each one with
// its PRAGMA user_version in a single transaction. A database created from
// nothing also gets the first-run defaults, in the last migration's
// transaction, so it never exists half-seeded.
func (s *Store) migrate(ctx context.Context, migs []migration, from int, seed func() []sqlite.Statement) error {
	latest := latestVersion(migs)
	for _, m := range migs {
		if m.Version <= from {
			continue
		}
		stmts := make([]sqlite.Statement, 0, len(m.Statements)+1)
		for _, sql := range m.Statements {
			stmts = append(stmts, sqlite.Statement{SQL: sql})
		}
		if from == 0 && m.Version == latest && seed != nil {
			stmts = append(stmts, seed()...)
		}
		// The version is an int we own, never user input.
		stmts = append(stmts, sqlite.Statement{SQL: fmt.Sprintf("PRAGMA user_version = %d", m.Version)})
		if _, err := s.db.Transaction(ctx, stmts); err != nil {
			return &MigrationError{Version: m.Version, Name: m.Name, Err: err}
		}
	}
	return nil
}

// splitSQL splits a script into statements at semicolons outside quotes,
// identifiers in brackets or backticks, and comments. Comments and empty
// statements are dropped.
func splitSQL(script string) ([]string, error) {
	var (
		out []string
		cur strings.Builder
	)
	flush := func() {
		if s := strings.TrimSpace(cur.String()); s != "" {
			out = append(out, s)
		}
		cur.Reset()
	}
	for i := 0; i < len(script); i++ {
		ch := script[i]
		switch {
		case ch == '-' && i+1 < len(script) && script[i+1] == '-':
			for i < len(script) && script[i] != '\n' {
				i++
			}
			cur.WriteByte('\n')
		case ch == '/' && i+1 < len(script) && script[i+1] == '*':
			end := strings.Index(script[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("unterminated comment")
			}
			i += end + 3
			cur.WriteByte(' ')
		case ch == '\'' || ch == '"' || ch == '`' || ch == '[':
			closer := ch
			if ch == '[' {
				closer = ']'
			}
			j := i + 1
			for {
				k := strings.IndexByte(script[j:], closer)
				if k < 0 {
					return nil, fmt.Errorf("unterminated %c", ch)
				}
				j += k + 1
				// A doubled quote escapes itself inside quotes.
				if closer != ']' && j < len(script) && script[j] == closer {
					j++
					continue
				}
				break
			}
			cur.WriteString(script[i:j])
			i = j - 1
		case ch == ';':
			flush()
		default:
			cur.WriteByte(ch)
		}
	}
	flush()
	return out, nil
}
