// Package storetest opens throwaway databases for tests.
package storetest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"

	"github.com/josexy/sshclipboard/internal/store"
)

// RequireSQLite skips the test when SQLite's native library can't be
// loaded, as offline before go test has downloaded it. In CI (CI is set)
// it fails instead, so a broken download can't pass as skipped tests.
func RequireSQLite(tb testing.TB) {
	tb.Helper()
	if err := sqlite.Load(); err != nil {
		if os.Getenv("CI") != "" {
			tb.Fatalf("the SQLite native library of plugins/sqlite is unavailable: %v", err)
		}
		tb.Skipf("skipping: the SQLite native library of plugins/sqlite is unavailable (go test downloads it on first use; are you offline?): %v", err)
	}
}

// Open opens a new database with defaults in a temporary directory, closed
// when the test ends.
func Open(tb testing.TB) *store.Store {
	tb.Helper()
	return OpenAt(tb, filepath.Join(tb.TempDir(), store.FileName))
}

// OpenAt opens the database at path, closed when the test ends.
func OpenAt(tb testing.TB, path string) *store.Store {
	tb.Helper()
	RequireSQLite(tb)
	st, err := store.Open(context.Background(), path)
	if err != nil {
		tb.Fatalf("open %s: %v", path, err)
	}
	tb.Cleanup(func() { st.Close() })
	return st
}
