package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCorruptDatabaseRecovered(t *testing.T) {
	requireSQLite(t)
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	junk := make([]byte, 64*1024)
	rand.Read(junk)
	if err := os.WriteFile(path, junk, 0o600); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local)
	st, err := openWith(context.Background(), path, schema{appMigrations, seedStatements}, func() time.Time { return at })
	if err != nil {
		t.Fatalf("open after corruption: %v", err)
	}
	defer st.Close()

	want := path + ".corrupt-20261112-153045"
	if st.RecoveredFrom != want {
		t.Errorf("RecoveredFrom %q, want %q", st.RecoveredFrom, want)
	}
	kept, err := os.ReadFile(want)
	if err != nil || !bytes.Equal(kept, junk) {
		t.Errorf("the old file was not kept intact at %s: %v", want, err)
	}
	// The app starts with defaults.
	rules, err := st.Rules.List(context.Background())
	if err != nil || len(rules) != 2 {
		t.Errorf("fresh database rules %v, %v", rules, err)
	}
}

func TestHealthyDatabaseNotRecovered(t *testing.T) {
	requireSQLite(t)
	path := filepath.Join(t.TempDir(), FileName)
	st, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.RecoveredFrom != "" {
		t.Errorf("a healthy database was moved to %s", st.RecoveredFrom)
	}
}
