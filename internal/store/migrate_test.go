package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// requireSQLite is storetest.RequireSQLite, which this package's tests
// can't import.
func requireSQLite(t *testing.T) {
	t.Helper()
	if err := sqlite.Load(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("the SQLite native library is unavailable: %v", err)
		}
		t.Skipf("skipping: the SQLite native library is unavailable: %v", err)
	}
}

// testMigrations: each one logs its version, so tests see the order.
func testMigrations(t *testing.T, n int) []migration {
	t.Helper()
	files := fstest.MapFS{
		"m/0001_log.sql": {Data: []byte(`
			CREATE TABLE log (version INTEGER NOT NULL); -- a comment; with a semicolon
			INSERT INTO log VALUES (1);`)},
		"m/0002_items.sql": {Data: []byte(`
			/* block; comment */
			CREATE TABLE items (name TEXT NOT NULL DEFAULT 'a;b');
			INSERT INTO log VALUES (2);`)},
		"m/0003_more.sql": {Data: []byte(`
			ALTER TABLE items ADD COLUMN size INTEGER NOT NULL DEFAULT 0;
			INSERT INTO log VALUES (3);`)},
		"m/0004_four.sql": {Data: []byte(`INSERT INTO log VALUES (4);`)},
		"m/0005_five.sql": {Data: []byte(`INSERT INTO log VALUES (5);`)},
	}
	migs, err := loadMigrations(files, "m")
	if err != nil {
		t.Fatal(err)
	}
	return migs[:n]
}

func openTest(t *testing.T, path string, migs []migration) (*Store, error) {
	t.Helper()
	requireSQLite(t)
	st, err := openWith(context.Background(), path, schema{migrations: migs}, time.Now)
	if err == nil {
		t.Cleanup(func() { st.Close() })
	}
	return st, err
}

func logged(t *testing.T, st *Store) []int64 {
	t.Helper()
	rows, err := st.db.Query(context.Background(), "SELECT version FROM log ORDER BY rowid")
	if err != nil {
		t.Fatal(err)
	}
	var out []int64
	for _, r := range rows.Values {
		out = append(out, i64(r[0]))
	}
	return out
}

func TestMigrateFromZeroInOrder(t *testing.T) {
	st, err := openTest(t, filepath.Join(t.TempDir(), "a.db"), testMigrations(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := st.SchemaVersion(context.Background()); v != 3 {
		t.Errorf("version %d, want 3", v)
	}
	if got := logged(t, st); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Errorf("applied %v", got)
	}
}

func TestUpgradeAppliesOnlyPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	st, err := openTest(t, path, testMigrations(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = openTest(t, path, testMigrations(t, 3))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := st.SchemaVersion(context.Background()); v != 3 {
		t.Errorf("version %d, want 3", v)
	}
	// Spec: an app at version 3 opening a version 1 database applies 2
	// and 3, in order.
	if got := logged(t, st); !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Errorf("applied %v", got)
	}
}

func TestFailingMigrationRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	migs := testMigrations(t, 1)
	if _, err := openTest(t, path, migs); err != nil {
		t.Fatal(err)
	}
	bad := append(migs, migration{Version: 2, Name: "bad", Statements: []string{
		"INSERT INTO log VALUES (2)",
		"CREATE TABLE items (x)",
		"INSERT INTO nowhere VALUES (1)", // fails
	}})
	_, err := openTest(t, path, bad)
	var me *MigrationError
	if !errors.As(err, &me) || me.Version != 2 {
		t.Fatalf("got %v, want a MigrationError for version 2", err)
	}
	st, err := openTest(t, path, migs)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := st.SchemaVersion(context.Background()); v != 1 {
		t.Errorf("version %d after a failed migration, want 1", v)
	}
	if got := logged(t, st); !reflect.DeepEqual(got, []int64{1}) {
		t.Errorf("log %v: the failed migration's first statement was kept", got)
	}
	if _, err := st.db.Query(context.Background(), "SELECT * FROM items"); err == nil {
		t.Error("the failed migration's table exists")
	}
}

func TestNewerDatabaseRefusedUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.db")
	st, err := openTest(t, path, testMigrations(t, 5))
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = openTest(t, path, testMigrations(t, 3))
	var ne *NewerSchemaError
	if !errors.As(err, &ne) || !errors.Is(err, ErrNewerSchema) || ne.Found != 5 || ne.Supported != 3 {
		t.Fatalf("got %v, want NewerSchemaError{5, 3}", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Error("the newer database was modified")
	}
}

func TestLoadMigrationsRejectsGaps(t *testing.T) {
	_, err := loadMigrations(fstest.MapFS{
		"m/0001_a.sql": {Data: []byte("SELECT 1")},
		"m/0003_c.sql": {Data: []byte("SELECT 1")},
	}, "m")
	if err == nil {
		t.Error("a gap in migration numbers was accepted")
	}
}

func TestSplitSQL(t *testing.T) {
	got, err := splitSQL(`
		CREATE TABLE "a;b" (x TEXT DEFAULT 'it''s; fine'); -- c; d
		/* e; f */ INSERT INTO [g;h] VALUES (1);
		;
		SELECT 1`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`CREATE TABLE "a;b" (x TEXT DEFAULT 'it''s; fine')`,
		`INSERT INTO [g;h] VALUES (1)`,
		`SELECT 1`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
	if _, err := splitSQL("SELECT 'open"); err == nil {
		t.Error("unterminated string accepted")
	}
}

func TestAppMigrationsLoad(t *testing.T) {
	if len(appMigrations) == 0 || appMigrations[0].Version != 1 {
		t.Fatalf("app migrations %+v", appMigrations)
	}
}
