package store_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"

	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/store/storetest"
)

func TestOpenTempDB(t *testing.T) {
	st := storetest.Open(t)
	ctx := context.Background()
	if filepath.Base(st.Path()) != store.FileName {
		t.Errorf("path %q", st.Path())
	}
	if _, err := os.Stat(st.Path()); err != nil {
		t.Fatal(err)
	}
	if mode, err := st.Pragma(ctx, "journal_mode"); err != nil || mode != "wal" {
		t.Errorf("journal_mode %q, %v", mode, err)
	}
	if fk, err := st.Pragma(ctx, "foreign_keys"); err != nil || fk != "1" {
		t.Errorf("foreign_keys %q, %v", fk, err)
	}
}

func TestOpenDirCreatesFolder(t *testing.T) {
	storetest.RequireSQLite(t)
	dir := filepath.Join(t.TempDir(), "userData", "SSHClipboard")
	st, err := store.OpenDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Path() != filepath.Join(dir, "sshclipboard.db") {
		t.Errorf("path %q", st.Path())
	}
}

func TestOpenUserData(t *testing.T) {
	storetest.RequireSQLite(t)
	dir := t.TempDir()
	mygo.App.SetPath(mygo.PathUserData, dir)
	st, err := store.OpenUserData(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if st.Path() != filepath.Join(dir, store.FileName) {
		t.Errorf("path %q, want it in %q", st.Path(), dir)
	}
}
