package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/egoist/mygo/plugins/sqlite"
)

// diskFull fails every write, as SQLite does when the disk is full.
type diskFull struct{ conn }

var errFull = &sqlite.Error{Code: 13, ExtendedCode: 13, Message: "database or disk is full"}

func (d diskFull) Execute(ctx context.Context, sql string, args ...any) (sqlite.Result, error) {
	return sqlite.Result{}, errFull
}

func (d diskFull) Transaction(ctx context.Context, s []sqlite.Statement) ([]sqlite.Result, error) {
	return nil, errFull
}

func (d diskFull) Query(ctx context.Context, sql string, args ...any) (sqlite.Rows, error) {
	if strings.HasPrefix(strings.TrimSpace(sql), "SELECT") {
		return d.conn.Query(ctx, sql, args...)
	}
	return sqlite.Rows{}, errFull
}

// Spec "Write fails": saving a rule fails, the error is returned, and the
// list is unchanged.
func TestWriteFailureLeavesRulesUnchanged(t *testing.T) {
	st := openApp(t)
	ctx := context.Background()
	before, _ := st.Rules.List(ctx)
	st.db = diskFull{st.db}
	if _, err := st.Rules.Create(ctx, Rule{Name: "x", Enabled: true, Action: ActionSkip}); !errors.Is(err, errFull) {
		t.Errorf("create: %v", err)
	}
	changed := before[0]
	changed.Name = "renamed"
	if err := st.Rules.Update(ctx, changed); !errors.Is(err, errFull) {
		t.Errorf("update: %v", err)
	}
	if err := st.Settings.Set(ctx, KeyStartAtLogin, true); !errors.Is(err, errFull) {
		t.Errorf("setting: %v", err)
	}
	after, err := st.Rules.List(ctx)
	if err != nil || len(after) != len(before) || after[0].Name != before[0].Name {
		t.Errorf("rules changed: %+v", after)
	}
	if s, _ := st.Settings.Load(ctx); s.StartAtLogin {
		t.Error("a failed setting took effect")
	}
}
