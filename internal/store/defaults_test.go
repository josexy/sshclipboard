package store

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestFreshDatabaseHasExactlyTheDefaults(t *testing.T) {
	st := openApp(t)
	ctx := context.Background()

	// Settings: every key, with the spec's values.
	rows, err := st.db.Query(ctx, "SELECT key, value FROM settings ORDER BY key")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]any{}
	for _, r := range rows.Values {
		var v any
		if err := json.Unmarshal([]byte(str(r[1])), &v); err != nil {
			t.Fatal(err)
		}
		got[str(r[0])] = v
	}
	want := map[string]any{
		"monitoring": true, "capture_mode": "auto", "confidential_protection": true,
		"default_action": "upload", "active_profile": 0.0, "appearance": "system",
		"notifications": "errors", "retention_count": 1000.0, "retention_days": 30.0,
		"start_at_login": false, "start_hidden": false, "close_to_tray": true,
		"close_notice_shown": false, "hotkey": "Ctrl+Alt+U", "staging_dir": "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("settings\n got %v\nwant %v", got, want)
	}
	s, err := st.Settings.Load(ctx)
	if err != nil || s != DefaultSettings() {
		t.Errorf("loaded settings %+v, %v", s, err)
	}

	// Naming templates per kind.
	naming, err := st.Naming.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ = st.db.Query(ctx, "SELECT count(*) FROM naming")
	wantNaming := []Naming{
		{Kind: KindImage, Template: "screenshot_{datetime}.{ext}"},
		{Kind: KindFile, Template: "{name}.{ext}"},
		{Kind: KindText, Template: "clip_{datetime}.{ext}"},
	}
	if i64(rows.Values[0][0]) != 3 || !reflect.DeepEqual(naming, wantNaming) {
		t.Errorf("naming %+v (%v rows)", naming, rows.Values[0][0])
	}

	// "Skip text" then "Skip large files"; the default action is upload.
	rules, err := st.Rules.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules %+v", rules)
	}
	r0, r1 := rules[0], rules[1]
	if r0.Name != "Skip text" || !r0.Enabled || r0.Action != ActionSkip ||
		!reflect.DeepEqual(r0.Conditions, Conditions{Kinds: []Kind{KindText}}) {
		t.Errorf("first rule %+v", r0)
	}
	if r1.Name != "Skip large files" || !r1.Enabled || r1.Action != ActionSkip ||
		!reflect.DeepEqual(r1.Conditions.Kinds, []Kind{KindFile}) || r1.Conditions.MinSize == nil ||
		*r1.Conditions.MinSize != 100*MB || r1.Conditions.MaxSize != nil || r1.DirOverride != "" {
		t.Errorf("second rule %+v", r1)
	}

	// No profiles, known hosts, counters, or history.
	for _, table := range []string{"profiles", "known_hosts", "counters", "history"} {
		rows, err := st.db.Query(ctx, "SELECT count(*) FROM "+table)
		if err != nil || i64(rows.Values[0][0]) != 0 {
			t.Errorf("%s has %v rows, %v", table, rows.Values, err)
		}
	}
}

func TestDefaultsSeededOnce(t *testing.T) {
	st := openApp(t)
	ctx := context.Background()
	if err := st.Rules.Delete(ctx, 1); err != nil {
		t.Fatal(err)
	}
	path := st.Path()
	st.Close()
	st2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	rules, _ := st2.Rules.List(ctx)
	if len(rules) != 1 {
		t.Errorf("reopening reseeded the rules: %+v", rules)
	}
}
