package store_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/store/storetest"
)

var ctx = context.Background()

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func TestSettingsRepo(t *testing.T) {
	st := storetest.Open(t)
	if err := st.Settings.Set(ctx, store.KeyStartAtLogin, true); err != nil {
		t.Fatal(err)
	}
	if err := st.Settings.Set(ctx, store.KeyAppearance, store.AppearanceDark); err != nil {
		t.Fatal(err)
	}
	if err := st.Settings.Set(ctx, store.KeyRetentionCount, 50); err != nil {
		t.Fatal(err)
	}
	s := must[store.Settings](t)(st.Settings.Load(ctx))
	if !s.StartAtLogin || s.Appearance != store.AppearanceDark || s.RetentionCount != 50 {
		t.Errorf("loaded %+v", s)
	}
	if err := st.Settings.Set(ctx, "no_such_key", 1); err == nil {
		t.Error("an unknown key was stored")
	}
	if err := st.Settings.Set(ctx, store.KeyRetentionDays, "thirty"); err == nil {
		t.Error("a string was stored for an int setting")
	}

	// Spec "Survives a crash": a committed setting is on disk before Set
	// returns, so a second connection (a restarted process) sees it while
	// the first is still open.
	other := storetest.OpenAt(t, st.Path())
	if s := must[store.Settings](t)(other.Settings.Load(ctx)); !s.StartAtLogin {
		t.Error("start at login is not visible to a new connection")
	}

	s.Notifications = store.NotifyOff
	s.Hotkey = ""
	if err := st.Settings.Save(ctx, s); err != nil {
		t.Fatal(err)
	}
	if got := must[store.Settings](t)(st.Settings.Load(ctx)); got != s {
		t.Errorf("saved %+v, loaded %+v", s, got)
	}
}

func newProfile(name string) store.Profile {
	p := store.NewProfile()
	p.Name, p.Host, p.Username = name, "10.0.0.5", "alice"
	return p
}

func TestProfilesRepo(t *testing.T) {
	st := storetest.Open(t)

	// Spec "Create a profile with defaults".
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	got := must[store.Profile](t)(st.Profiles.Get(ctx, dev.ID))
	if got.Port != 22 || got.RemoteDir != "~/clipboard" || got.Name != "dev-box" || got.FileMode != -1 ||
		got.ConflictPolicy != store.ConflictRename || !got.CreateDirs || !got.CopyBack || !got.ShellQuote ||
		got.CopyTemplate != "{path}" || got.CopySeparator != " " || got.CreatedAt.IsZero() {
		t.Errorf("created %+v", got)
	}
	list := must[[]store.Profile](t)(st.Profiles.List(ctx))
	if len(list) != 1 || list[0].ID != dev.ID {
		t.Errorf("list %+v", list)
	}

	// Spec "Reject invalid profile": a name already used.
	if _, err := st.Profiles.Create(ctx, newProfile("Dev-Box")); !errors.Is(err, store.ErrDuplicateName) {
		t.Errorf("duplicate name: %v", err)
	}
	prod := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("prod-box")))
	prod.Name = "dev-box"
	if err := st.Profiles.Update(ctx, prod); !errors.Is(err, store.ErrDuplicateName) {
		t.Errorf("rename to a used name: %v", err)
	}
	prod.Name, prod.Port, prod.FileMode, prod.AuthMethod, prod.KeyPath = "prod-box", 2222, 0o640, store.AuthKey, `C:\k`
	prod.CopySeparator = "\n"
	if err := st.Profiles.Update(ctx, prod); err != nil {
		t.Fatal(err)
	}
	if got := must[store.Profile](t)(st.Profiles.Get(ctx, prod.ID)); got.Port != 2222 || got.FileMode != 0o640 ||
		got.AuthMethod != store.AuthKey || got.KeyPath != `C:\k` || got.CopySeparator != "\n" {
		t.Errorf("updated %+v", got)
	}
	if _, err := st.Profiles.Get(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing profile: %v", err)
	}
}

func TestDeleteActiveProfile(t *testing.T) {
	st := storetest.Open(t)
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	other := newProfile("other")
	other.Host = "10.0.0.9"
	other = must[store.Profile](t)(st.Profiles.Create(ctx, other))
	if err := st.Settings.Set(ctx, store.KeyActiveProfile, dev.ID); err != nil {
		t.Fatal(err)
	}
	for _, kh := range []store.KnownHost{
		{Host: "10.0.0.5", Port: 22, KeyType: "ssh-ed25519", KeyBase64: "AAA", Fingerprint: "SHA256:a"},
		{Host: "10.0.0.9", Port: 22, KeyType: "ssh-ed25519", KeyBase64: "BBB", Fingerprint: "SHA256:b"},
	} {
		if err := st.KnownHosts.Put(ctx, kh); err != nil {
			t.Fatal(err)
		}
	}
	must[int](t)(st.Counters.Next(ctx, dev.ID, "2026-11-12"))
	ids := must[[]int64](t)(st.History.Insert(ctx, []store.HistoryEntry{entry("c1", dev, store.StatusUploaded, at(0))}, nil))

	if err := st.Profiles.Delete(ctx, dev.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Profiles.Get(ctx, dev.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("profile still there: %v", err)
	}
	if s := must[store.Settings](t)(st.Settings.Load(ctx)); s.ActiveProfile != 0 {
		t.Errorf("active profile %d after deleting it", s.ActiveProfile)
	}
	if _, ok, _ := st.KnownHosts.Get(ctx, "10.0.0.5", 22); ok {
		t.Error("the deleted profile's host key is still trusted")
	}
	if _, ok, _ := st.KnownHosts.Get(ctx, "10.0.0.9", 22); !ok {
		t.Error("another profile's host key was removed")
	}
	if v := must[int](t)(st.Counters.Current(ctx, dev.ID, "2026-11-12")); v != 0 {
		t.Errorf("counter %d survived its profile", v)
	}
	h := must[store.HistoryEntry](t)(st.History.Get(ctx, ids[0]))
	if h.ProfileID != 0 || h.ProfileName != "dev-box" {
		t.Errorf("history after delete: profile %d %q", h.ProfileID, h.ProfileName)
	}
	_ = other
}

func TestDeleteKeepsSharedHostKey(t *testing.T) {
	st := storetest.Open(t)
	a := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("a")))
	must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("b"))) // same host and port
	if err := st.KnownHosts.Put(ctx, store.KnownHost{Host: "10.0.0.5", Port: 22, KeyType: "t", KeyBase64: "k", Fingerprint: "f"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Profiles.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.KnownHosts.Get(ctx, "10.0.0.5", 22); !ok {
		t.Error("a host key another profile uses was removed")
	}
}

func ruleNames(t *testing.T, st *store.Store) []string {
	t.Helper()
	var names []string
	for _, r := range must[[]store.Rule](t)(st.Rules.List(ctx)) {
		names = append(names, r.Name)
	}
	return names
}

func TestRulesRepo(t *testing.T) {
	st := storetest.Open(t)
	minW := 32
	r := must[store.Rule](t)(st.Rules.Create(ctx, store.Rule{
		Name: "Tiny images", Enabled: true, Action: store.ActionSkip,
		Conditions: store.Conditions{Kinds: []store.Kind{store.KindImage}, MaxWidth: &minW},
	}))
	if r.Position != 3 || r.ID == 0 {
		t.Errorf("created %+v", r)
	}
	if got := ruleNames(t, st); !reflect.DeepEqual(got, []string{"Skip text", "Skip large files", "Tiny images"}) {
		t.Errorf("order %q", got)
	}
	got := must[store.Rule](t)(st.Rules.Get(ctx, r.ID))
	if got.Conditions.MaxWidth == nil || *got.Conditions.MaxWidth != 32 || got.Conditions.MinWidth != nil {
		t.Errorf("conditions %+v", got.Conditions)
	}

	r.Action, r.DirOverride, r.TemplateOverride = store.ActionUpload, "screenshots/{date}", "{name}.{ext}"
	r.Conditions.TextRegex = "^x"
	if err := st.Rules.Update(ctx, r); err != nil {
		t.Fatal(err)
	}
	got = must[store.Rule](t)(st.Rules.Get(ctx, r.ID))
	if got.Action != store.ActionUpload || got.DirOverride != "screenshots/{date}" ||
		got.TemplateOverride != "{name}.{ext}" || got.Conditions.TextRegex != "^x" {
		t.Errorf("updated %+v", got)
	}
	if err := st.Rules.SetEnabled(ctx, r.ID, false); err != nil {
		t.Fatal(err)
	}
	if got := must[store.Rule](t)(st.Rules.Get(ctx, r.ID)); got.Enabled {
		t.Error("still enabled")
	}

	// Move up and down, then an explicit order.
	if err := st.Rules.Move(ctx, r.ID, -1); err != nil {
		t.Fatal(err)
	}
	if got := ruleNames(t, st); !reflect.DeepEqual(got, []string{"Skip text", "Tiny images", "Skip large files"}) {
		t.Errorf("after move up %q", got)
	}
	if err := st.Rules.Move(ctx, r.ID, -5); err != nil {
		t.Fatal(err)
	}
	if got := ruleNames(t, st); got[0] != "Tiny images" {
		t.Errorf("after move to top %q", got)
	}
	rules := must[[]store.Rule](t)(st.Rules.List(ctx))
	if err := st.Rules.Reorder(ctx, []int64{rules[2].ID, rules[1].ID, rules[0].ID}); err != nil {
		t.Fatal(err)
	}
	if got := ruleNames(t, st); !reflect.DeepEqual(got, []string{"Skip large files", "Skip text", "Tiny images"}) {
		t.Errorf("after reorder %q", got)
	}
	if err := st.Rules.Reorder(ctx, []int64{rules[0].ID, rules[0].ID, rules[1].ID}); err == nil {
		t.Error("reorder with a repeated id succeeded")
	}
	if err := st.Rules.Delete(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if got := ruleNames(t, st); len(got) != 2 {
		t.Errorf("after delete %q", got)
	}
}

func TestNamingRepo(t *testing.T) {
	st := storetest.Open(t)
	n := store.Naming{Kind: store.KindFile, Template: "{profile}_{name}.{ext}", Lowercase: true, SpacesToUnderscore: true}
	if err := st.Naming.Save(ctx, n); err != nil {
		t.Fatal(err)
	}
	if got := must[store.Naming](t)(st.Naming.Get(ctx, store.KindFile)); got != n {
		t.Errorf("got %+v", got)
	}
	all := must[[]store.Naming](t)(st.Naming.All(ctx))
	if len(all) != 3 || all[0].Template != "screenshot_{datetime}.{ext}" || all[1] != n {
		t.Errorf("all %+v", all)
	}
}

func TestKnownHostsRepo(t *testing.T) {
	st := storetest.Open(t)
	if _, ok, err := st.KnownHosts.Get(ctx, "h", 22); ok || err != nil {
		t.Errorf("empty: %v %v", ok, err)
	}
	kh := store.KnownHost{Host: "h", Port: 22, KeyType: "ssh-ed25519", KeyBase64: "AAA", Fingerprint: "SHA256:old"}
	if err := st.KnownHosts.Put(ctx, kh); err != nil {
		t.Fatal(err)
	}
	kh.KeyBase64, kh.Fingerprint = "BBB", "SHA256:new"
	if err := st.KnownHosts.Put(ctx, kh); err != nil { // Replace
		t.Fatal(err)
	}
	got, ok, err := st.KnownHosts.Get(ctx, "h", 22)
	if !ok || err != nil || got.KeyBase64 != "BBB" || got.Fingerprint != "SHA256:new" || got.AddedAt.IsZero() {
		t.Errorf("got %+v %v %v", got, ok, err)
	}
	if _, ok, _ := st.KnownHosts.Get(ctx, "h", 2222); ok {
		t.Error("keys are per host and port")
	}
	if err := st.KnownHosts.Delete(ctx, "h", 22); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.KnownHosts.Get(ctx, "h", 22); ok {
		t.Error("deleted key still trusted")
	}
}

func TestCountersRepo(t *testing.T) {
	st := storetest.Open(t)
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	prod := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("prod-box")))
	for want := 1; want <= 3; want++ {
		if got := must[int](t)(st.Counters.Next(ctx, dev.ID, "2026-11-12")); got != want {
			t.Errorf("next %d, want %d", got, want)
		}
	}
	// Spec "Counter resets daily", and it is per profile.
	if got := must[int](t)(st.Counters.Next(ctx, dev.ID, "2026-11-13")); got != 1 {
		t.Errorf("next day %d", got)
	}
	if got := must[int](t)(st.Counters.Next(ctx, prod.ID, "2026-11-12")); got != 1 {
		t.Errorf("other profile %d", got)
	}
	if got := must[int](t)(st.Counters.Current(ctx, dev.ID, "2026-11-12")); got != 3 {
		t.Errorf("current %d", got)
	}
	// A counter value stored with the history rows that used it.
	must[[]int64](t)(st.History.Insert(ctx, []store.HistoryEntry{entry("c", dev, store.StatusQueued, at(0))},
		&store.CounterValue{ProfileID: dev.ID, Day: "2026-11-12", Value: 4}))
	// Spec "Counter survives restart".
	path := st.Path()
	st.Close()
	st = storetest.OpenAt(t, path)
	if got := must[int](t)(st.Counters.Next(ctx, dev.ID, "2026-11-12")); got != 5 {
		t.Errorf("after restart %d, want 5", got)
	}
	if store.Day(time.Date(2026, 11, 12, 23, 59, 0, 0, time.Local)) != "2026-11-12" {
		t.Error("Day format")
	}
}

var base = time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local)

func at(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }

func entry(capture string, p store.Profile, status store.Status, when time.Time) store.HistoryEntry {
	e := store.HistoryEntry{
		CaptureID: capture, CapturedAt: when, Trigger: store.TriggerAuto, Kind: store.KindImage,
		OriginalName: "image.png", LocalPath: "/staging/" + capture + ".png", Staged: true,
		ThumbPath: "/staging/" + capture + ".thumb.png", Size: 1000, Width: 1280, Height: 720,
		SHA256: "ab", ProfileID: p.ID, ProfileName: p.Name, RuleName: store.RuleDefault,
		Action: store.ActionUpload, Status: status, RemoteDir: "~/clipboard", RemoteName: capture + ".png",
	}
	if status == store.StatusSkipped {
		e.Action = store.ActionSkip
	}
	if !status.Active() {
		e.FinishedAt = when
	}
	return e
}

func TestHistoryInsertAndTransitions(t *testing.T) {
	st := storetest.Open(t)
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	skipped := entry("c1", dev, store.StatusSkipped, at(0))
	skipped.Kind, skipped.RuleName, skipped.OriginalName = store.KindText, "Skip text", "text.txt"
	ids := must[[]int64](t)(st.History.Insert(ctx, []store.HistoryEntry{skipped, entry("c2", dev, store.StatusQueued, at(1))}, nil))
	if len(ids) != 2 || ids[0] == 0 || ids[1] != ids[0]+1 {
		t.Fatalf("ids %v", ids)
	}
	// Spec "Skipped entry recorded".
	got := must[store.HistoryEntry](t)(st.History.Get(ctx, ids[0]))
	want := skipped
	want.ID = ids[0]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip\n got %+v\nwant %+v", got, want)
	}

	// queued → uploading → uploaded.
	id := ids[1]
	if err := st.History.SetStatus(ctx, id, store.Update{Status: store.StatusUploading, Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := st.History.SetStatus(ctx, id, store.Update{Status: store.StatusUploaded, Attempts: 2,
		RemotePath: "/home/alice/clipboard/c2.png", FinishedAt: at(2)}); err != nil {
		t.Fatal(err)
	}
	got = must[store.HistoryEntry](t)(st.History.Get(ctx, id))
	if got.Status != store.StatusUploaded || got.Attempts != 2 || got.RemotePath != "/home/alice/clipboard/c2.png" ||
		!got.FinishedAt.Equal(at(2)) {
		t.Errorf("uploaded %+v", got)
	}

	// Spec "Failure recorded".
	if err := st.History.SetStatus(ctx, id, store.Update{Status: store.StatusFailed, Attempts: 4,
		Error: "connection refused", FinishedAt: at(3)}); err != nil {
		t.Fatal(err)
	}
	got = must[store.HistoryEntry](t)(st.History.Get(ctx, id))
	if got.Status != store.StatusFailed || got.Attempts != 4 || got.Error != "connection refused" {
		t.Errorf("failed %+v", got)
	}

	// Retry keeps the profile and name and resets the outcome.
	if err := st.History.Requeue(ctx, id, store.Requeue{Trigger: store.TriggerAuto, ProfileID: dev.ID,
		ProfileName: dev.Name, RuleName: got.RuleName, RemoteDir: got.RemoteDir, RemoteName: got.RemoteName}); err != nil {
		t.Fatal(err)
	}
	got = must[store.HistoryEntry](t)(st.History.Get(ctx, id))
	if got.Status != store.StatusQueued || got.Attempts != 0 || got.Error != "" || !got.FinishedAt.IsZero() ||
		got.RemoteName != "c2.png" || got.RemotePath != "" {
		t.Errorf("requeued %+v", got)
	}
	// Upload anyway: manual trigger, upload action.
	if err := st.History.Requeue(ctx, ids[0], store.Requeue{Trigger: store.TriggerManual, ProfileID: dev.ID,
		ProfileName: dev.Name, RuleName: store.RuleManual, RemoteDir: "~/clipboard", RemoteName: "clip.txt"}); err != nil {
		t.Fatal(err)
	}
	got = must[store.HistoryEntry](t)(st.History.Get(ctx, ids[0]))
	if got.Trigger != store.TriggerManual || got.Action != store.ActionUpload || got.RuleName != store.RuleManual {
		t.Errorf("upload anyway %+v", got)
	}

	byCapture := must[[]store.HistoryEntry](t)(st.History.ByCapture(ctx, "c2"))
	if len(byCapture) != 1 || byCapture[0].ID != id {
		t.Errorf("by capture %+v", byCapture)
	}
	if err := st.History.SetStatus(ctx, 999, store.Update{Status: store.StatusFailed}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing entry: %v", err)
	}
}

func TestHistoryQuery(t *testing.T) {
	st := storetest.Open(t)
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	var entries []store.HistoryEntry
	for i := range 6 {
		e := entry(fmt.Sprint("c", i), dev, store.StatusUploaded, at(i))
		e.RemotePath = "/home/alice/clipboard/2026-10/x.png"
		if i >= 3 {
			e.RemotePath = "/home/alice/clipboard/2026-11/x.png"
		}
		entries = append(entries, e)
	}
	entries[1].Status, entries[1].RemotePath = store.StatusSkipped, ""
	entries[2].Kind, entries[2].OriginalName = store.KindFile, "Quarterly Report.PDF"
	entries[4].Status = store.StatusFailed
	must[[]int64](t)(st.History.Insert(ctx, entries, nil))

	names := func(f store.Filter) []string {
		t.Helper()
		out, _, err := st.History.Query(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, e := range out {
			s = append(s, e.CaptureID)
		}
		return s
	}
	if got := names(store.Filter{}); !reflect.DeepEqual(got, []string{"c5", "c4", "c3", "c2", "c1", "c0"}) {
		t.Errorf("newest first: %q", got)
	}
	// Spec "Search by remote path".
	if got := names(store.Filter{Search: "2026-11"}); !reflect.DeepEqual(got, []string{"c5", "c4", "c3"}) {
		t.Errorf("search: %q", got)
	}
	if got := names(store.Filter{Search: "quarterly report"}); !reflect.DeepEqual(got, []string{"c2"}) {
		t.Errorf("case-insensitive name search: %q", got)
	}
	// Spec "Hide skipped".
	if got := names(store.Filter{HideSkipped: true}); !reflect.DeepEqual(got, []string{"c5", "c4", "c3", "c2", "c0"}) {
		t.Errorf("hide skipped: %q", got)
	}
	if got := names(store.Filter{Statuses: []store.Status{store.StatusFailed, store.StatusSkipped}}); !reflect.DeepEqual(got, []string{"c4", "c1"}) {
		t.Errorf("status filter: %q", got)
	}
	if got := names(store.Filter{Kinds: []store.Kind{store.KindFile}}); !reflect.DeepEqual(got, []string{"c2"}) {
		t.Errorf("kind filter: %q", got)
	}
	page, total, err := st.History.Query(ctx, store.Filter{Limit: 2, Offset: 2})
	if err != nil || total != 6 || len(page) != 2 || page[0].CaptureID != "c3" {
		t.Errorf("page %+v total %d %v", page, total, err)
	}
}

func TestHistorySummary(t *testing.T) {
	st := storetest.Open(t)
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	var entries []store.HistoryEntry
	for i, size := range []int64{1_400_000, 1_400_000, 1_604_320} { // 4,404,320 bytes ≈ 4.2 MB
		e := entry(fmt.Sprint("u", i), dev, store.StatusUploaded, at(i))
		e.Size = size
		entries = append(entries, e)
	}
	entries = append(entries, entry("f", dev, store.StatusFailed, at(4)), entry("s", dev, store.StatusSkipped, at(5)))
	yesterday := entry("y", dev, store.StatusUploaded, base.Add(-24*time.Hour))
	entries = append(entries, yesterday, entry("q", dev, store.StatusQueued, at(6)))
	must[[]int64](t)(st.History.Insert(ctx, entries, nil))

	day := time.Date(base.Year(), base.Month(), base.Day(), 0, 0, 0, 0, time.Local)
	sum := must[store.Summary](t)(st.History.Summary(ctx, day, day.AddDate(0, 0, 1)))
	if sum.Uploaded != 3 || sum.Bytes != 4_404_320 || sum.Failed != 1 {
		t.Errorf("summary %+v", sum)
	}
}

func TestHistoryPrune(t *testing.T) {
	st := storetest.Open(t)
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	var entries []store.HistoryEntry
	// The oldest entry is uploading: it must survive.
	entries = append(entries, entry("active", dev, store.StatusUploading, at(-100)))
	for i := range 5 {
		entries = append(entries, entry(fmt.Sprint("e", i), dev, store.StatusUploaded, at(i)))
	}
	must[[]int64](t)(st.History.Insert(ctx, entries, nil))

	// Spec "Count limit": over the limit by one, the oldest finished
	// entry goes.
	removed := must[[]store.HistoryEntry](t)(st.History.Prune(ctx, 5, 0, at(10)))
	if len(removed) != 1 || removed[0].CaptureID != "e0" || removed[0].LocalPath != "/staging/e0.png" {
		t.Errorf("count prune removed %+v", removed)
	}
	// Age: entries older than 1 day; the uploading one stays.
	old := entry("old", dev, store.StatusFailed, base.AddDate(0, 0, -31))
	must[[]int64](t)(st.History.Insert(ctx, []store.HistoryEntry{old}, nil))
	removed = must[[]store.HistoryEntry](t)(st.History.Prune(ctx, 1000, 30, base))
	if len(removed) != 1 || removed[0].CaptureID != "old" {
		t.Errorf("age prune removed %+v", removed)
	}
	removed = must[[]store.HistoryEntry](t)(st.History.Prune(ctx, 1, 0, base))
	if len(removed) != 4 {
		t.Errorf("prune to 1 removed %d", len(removed))
	}
	left, _, _ := st.History.Query(ctx, store.Filter{})
	if len(left) != 1 || left[0].CaptureID != "active" {
		t.Errorf("left %+v: queued/uploading entries must never be pruned", left)
	}
}

func TestHistoryInterruptedDeleteClear(t *testing.T) {
	st := storetest.Open(t)
	dev := must[store.Profile](t)(st.Profiles.Create(ctx, newProfile("dev-box")))
	ids := must[[]int64](t)(st.History.Insert(ctx, []store.HistoryEntry{
		entry("q1", dev, store.StatusQueued, at(0)),
		entry("q2", dev, store.StatusUploading, at(1)),
		entry("done", dev, store.StatusUploaded, at(2)),
		entry("skip", dev, store.StatusSkipped, at(3)),
	}, nil))
	files := must[map[string]bool](t)(st.History.StagedFiles(ctx))
	if len(files) != 8 || !files["/staging/q1.png"] || !files["/staging/q1.thumb.png"] {
		t.Errorf("staged files %v", files)
	}

	// Spec "Quit during upload": both become failed with "interrupted".
	if n := must[int](t)(st.History.MarkInterrupted(ctx, at(5))); n != 2 {
		t.Errorf("marked %d", n)
	}
	for _, id := range ids[:2] {
		e := must[store.HistoryEntry](t)(st.History.Get(ctx, id))
		if e.Status != store.StatusFailed || e.Error != store.ReasonInterrupted || e.FinishedAt.IsZero() {
			t.Errorf("after restart %+v", e)
		}
	}

	del := must[store.HistoryEntry](t)(st.History.Delete(ctx, ids[2]))
	if del.CaptureID != "done" {
		t.Errorf("deleted %+v", del)
	}
	if _, err := st.History.Delete(ctx, ids[2]); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}

	// Clear keeps queued and uploading entries.
	must[[]int64](t)(st.History.Insert(ctx, []store.HistoryEntry{entry("live", dev, store.StatusUploading, at(9))}, nil))
	cleared := must[[]store.HistoryEntry](t)(st.History.Clear(ctx))
	if len(cleared) != 3 {
		t.Errorf("cleared %d", len(cleared))
	}
	left, total, _ := st.History.Query(ctx, store.Filter{})
	if total != 1 || left[0].CaptureID != "live" {
		t.Errorf("left %+v", left)
	}
}
