package store

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

func openApp(t *testing.T) *Store {
	t.Helper()
	requireSQLite(t)
	st, err := Open(context.Background(), filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSchemaV1Objects(t *testing.T) {
	st := openApp(t)
	ctx := context.Background()
	if v, _ := st.SchemaVersion(ctx); v != 1 {
		t.Errorf("schema version %d, want 1", v)
	}
	rows, err := st.db.Query(ctx, "SELECT type, name FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' ORDER BY type, name")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows.Values {
		got = append(got, str(r[0])+" "+str(r[1]))
	}
	want := []string{
		"index history_capture_id", "index history_captured_at", "index history_status", "index rules_position",
		"table counters", "table history", "table known_hosts", "table naming", "table profiles", "table rules",
		"table settings",
	}
	if !slices.Equal(got, want) {
		t.Errorf("schema objects\n got %q\nwant %q", got, want)
	}

	fk := func(table string) map[string]string {
		rows, err := st.db.Query(ctx, "SELECT \"table\", on_delete FROM pragma_foreign_key_list(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, r := range rows.Values {
			m[str(r[0])] = str(r[1])
		}
		return m
	}
	if got := fk("history"); got["profiles"] != "SET NULL" {
		t.Errorf("history → profiles on delete %q, want SET NULL", got["profiles"])
	}
	if got := fk("counters"); got["profiles"] != "CASCADE" {
		t.Errorf("counters → profiles on delete %q, want CASCADE", got["profiles"])
	}
}

func TestSchemaV1Checks(t *testing.T) {
	st := openApp(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) error {
		_, err := st.db.Execute(ctx, sql, args...)
		return err
	}
	const profile = `INSERT INTO profiles (name, host, port, username, auth_method, conflict_policy, file_mode,
		copy_separator, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0)`
	if err := exec(profile, "ok", "h", 22, "u", "password", "rename", nil, " "); err != nil {
		t.Fatalf("a valid profile was rejected: %v", err)
	}
	const history = `INSERT INTO history (capture_id, captured_at, "trigger", kind, original_name, action, status)
		VALUES ('c', 0, ?, ?, 'n', ?, ?)`
	if err := exec(history, "auto", "image", "upload", "queued"); err != nil {
		t.Fatalf("a valid history row was rejected: %v", err)
	}

	bad := []struct {
		name string
		sql  string
		args []any
	}{
		{"auth method", profile, []any{"a", "h", 22, "u", "kerberos", "rename", nil, " "}},
		{"port 0", profile, []any{"b", "h", 0, "u", "password", "rename", nil, " "}},
		{"port 70000", profile, []any{"c", "h", 70000, "u", "password", "rename", nil, " "}},
		{"empty host", profile, []any{"d", " ", 22, "u", "password", "rename", nil, " "}},
		{"conflict policy", profile, []any{"e", "h", 22, "u", "password", "replace", nil, " "}},
		{"file mode", profile, []any{"f", "h", 22, "u", "password", "rename", 0o10000, " "}},
		{"separator", profile, []any{"g", "h", 22, "u", "password", "rename", nil, ","}},
		{"rule action", "INSERT INTO rules (position, name, action) VALUES (1, 'r', 'delete')", nil},
		{"rule conditions", "INSERT INTO rules (position, name, action, conditions) VALUES (1, 'r', 'skip', '{')", nil},
		{"naming kind", "INSERT INTO naming (kind, template) VALUES ('video', 'x')", nil},
		{"settings json", "INSERT INTO settings (key, value) VALUES ('k', 'not json')", nil},
		{"history trigger", history, []any{"hotkey", "image", "upload", "queued"}},
		{"history kind", history, []any{"auto", "folder", "upload", "queued"}},
		{"history action", history, []any{"auto", "image", "maybe", "queued"}},
		{"history status", history, []any{"auto", "image", "upload", "done"}},
	}
	for _, b := range bad {
		err := exec(b.sql, b.args...)
		var se *sqlite.Error
		if !errors.As(err, &se) || se.Code != 19 /* SQLITE_CONSTRAINT */ {
			t.Errorf("%s: got %v, want a constraint error", b.name, err)
		}
	}
	if err := exec(profile, "OK", "h", 22, "u", "password", "rename", nil, " "); err == nil {
		t.Error("profile names must be unique, case-insensitively")
	}
}
