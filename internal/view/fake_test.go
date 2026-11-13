package view

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/crypto/ssh"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/rules"
	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// fakeCtrl records the commands the views give and answers them at once.
type fakeCtrl struct {
	mu      sync.Mutex
	state   *core.State
	calls   []string
	filters []store.Filter
	saved   []core.ProfileInput
	rules   []store.Rule
	namings []store.Naming
	test    sshconn.CheckResult
	dry     []core.DryRunResult
	setErr  map[string]error
	saveErr error
}

var now = time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local)

func newFake() *fakeCtrl {
	dev := store.NewProfile()
	dev.ID, dev.Name, dev.Host, dev.Username = 1, "dev-box", "10.0.0.5", "alice"
	prod := store.NewProfile()
	prod.ID, prod.Name, prod.Host, prod.Username = 2, "prod-box", "10.0.0.6", "bob"
	rs := store.DefaultRules()
	rs[0].ID, rs[1].ID = 1, 2
	st := &core.State{
		Settings: store.DefaultSettings(), Profiles: []store.Profile{dev, prod}, Active: &dev,
		Conn: sshconn.StatusConnected, Rules: rs, Naming: store.DefaultNaming(),
		Saved: map[int64]core.SavedSecrets{1: {Password: true}},
		History: []store.HistoryEntry{
			{ID: 3, CaptureID: "c3", CapturedAt: now, Kind: store.KindImage, OriginalName: "image.png", Status: store.StatusFailed,
				Error: "connection refused", ProfileID: 1, RuleName: store.RuleDefault, Size: 2048, Attempts: 4},
			{ID: 2, CaptureID: "c2", CapturedAt: now.Add(-time.Minute), Kind: store.KindText, OriginalName: "text.txt",
				Status: store.StatusSkipped, RuleName: "Skip text", LocalPath: "/staging/c2.txt", Staged: true},
			{ID: 1, CaptureID: "c1", CapturedAt: now.Add(-2 * time.Minute), Kind: store.KindFile, OriginalName: "report.pdf",
				Status: store.StatusUploaded, RemotePath: "/home/alice/clipboard/2026-11/report.pdf", Size: 4_404_320,
				ProfileID: 1, RuleName: store.RuleDefault},
		},
		HistoryTotal: 3,
		Summary:      store.Summary{Uploaded: 1, Bytes: 4_404_320, Failed: 1},
	}
	st.Settings.ActiveProfile = 1
	return &fakeCtrl{state: st, setErr: map[string]error{}}
}

func (f *fakeCtrl) record(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

func (f *fakeCtrl) called(t *testing.T, want string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.calls, want) {
		t.Errorf("no call %q; calls %q", want, f.calls)
	}
}

func (f *fakeCtrl) reset() { f.mu.Lock(); f.calls = nil; f.mu.Unlock() }

func (f *fakeCtrl) edit(fn func(s *core.State)) {
	f.mu.Lock()
	s := *f.state
	fn(&s)
	f.state = &s
	f.mu.Unlock()
}

func ok(done func(error)) {
	if done != nil {
		done(nil)
	}
}

func (f *fakeCtrl) State() *core.State { f.mu.Lock(); defer f.mu.Unlock(); return f.state }
func (f *fakeCtrl) SetMonitoring(on bool, done func(error)) {
	f.record("SetMonitoring(%v)", on)
	f.edit(func(s *core.State) { s.Settings.Monitoring = on })
	ok(done)
}
func (f *fakeCtrl) CaptureNow() { f.record("CaptureNow()") }
func (f *fakeCtrl) SetActiveProfile(id int64, done func(error)) {
	f.record("SetActiveProfile(%d)", id)
	ok(done)
}
func (f *fakeCtrl) SetSetting(key string, value any, done func(error)) {
	f.record("SetSetting(%s, %v)", key, value)
	if err := f.setErr[key]; err != nil {
		done(err)
		return
	}
	ok(done)
}
func (f *fakeCtrl) SaveProfile(in core.ProfileInput, done func(core.SaveResult, error)) {
	f.record("SaveProfile(%s)", in.Profile.Name)
	f.mu.Lock()
	f.saved = append(f.saved, in)
	f.mu.Unlock()
	if fe := core.ValidateProfile(in.Profile, nil); fe != nil {
		done(core.SaveResult{}, fe)
		return
	}
	if f.saveErr != nil {
		done(core.SaveResult{}, f.saveErr)
		return
	}
	p := in.Profile
	if p.ID == 0 {
		p.ID = 9
	}
	done(core.SaveResult{Profile: p}, nil)
}
func (f *fakeCtrl) DeleteProfile(id int64, done func(error)) {
	f.record("DeleteProfile(%d)", id)
	ok(done)
}
func (f *fakeCtrl) DuplicateProfile(id int64, done func(error)) {
	f.record("DuplicateProfile(%d)", id)
	ok(done)
}
func (f *fakeCtrl) TestConnection(in core.ProfileInput, done func(sshconn.CheckResult)) {
	f.record("TestConnection(%s)", in.Profile.Name)
	done(f.test)
}
func (f *fakeCtrl) TrustHostKey(done func(error)) {
	f.record("TrustHostKey()")
	f.edit(func(s *core.State) { s.HostKey = nil })
	ok(done)
}
func (f *fakeCtrl) DismissHostKey() {
	f.record("DismissHostKey()")
	f.edit(func(s *core.State) { s.HostKey = nil })
}
func (f *fakeCtrl) DismissSecret() { f.record("DismissSecret()") }
func (f *fakeCtrl) ProvideSecret(id int64, kind secret.Kind, v string, done func(error)) {
	f.record("ProvideSecret(%d, %s, %s)", id, kind, v)
	ok(done)
}
func (f *fakeCtrl) SaveRule(r store.Rule, done func(error)) {
	f.record("SaveRule(%s)", r.Name)
	if err := rules.Validate(r); err != nil {
		done(err)
		return
	}
	f.mu.Lock()
	f.rules = append(f.rules, r)
	f.mu.Unlock()
	ok(done)
}
func (f *fakeCtrl) DeleteRule(id int64, done func(error)) { f.record("DeleteRule(%d)", id); ok(done) }
func (f *fakeCtrl) MoveRule(id int64, d int, done func(error)) {
	f.record("MoveRule(%d, %d)", id, d)
	ok(done)
}
func (f *fakeCtrl) ReorderRules(ids []int64, done func(error)) {
	f.record("ReorderRules(%v)", ids)
	ok(done)
}
func (f *fakeCtrl) SetRuleEnabled(id int64, on bool, done func(error)) {
	f.record("SetRuleEnabled(%d, %v)", id, on)
	ok(done)
}
func (f *fakeCtrl) SetDefaultAction(a store.Action, done func(error)) {
	f.record("SetDefaultAction(%s)", a)
	ok(done)
}
func (f *fakeCtrl) SaveNaming(n store.Naming, done func(error)) {
	f.record("SaveNaming(%s, %s)", n.Kind, n.Template)
	f.mu.Lock()
	f.namings = append(f.namings, n)
	f.mu.Unlock()
	ok(done)
}
func (f *fakeCtrl) DryRun(done func([]core.DryRunResult)) { f.record("DryRun()"); done(f.dry) }
func (f *fakeCtrl) SetHistoryFilter(fl store.Filter) {
	f.mu.Lock()
	f.filters = append(f.filters, fl)
	f.mu.Unlock()
}
func (f *fakeCtrl) lastFilter() store.Filter {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.filters) == 0 {
		return store.Filter{}
	}
	return f.filters[len(f.filters)-1]
}
func (f *fakeCtrl) ActivityViewed()                  { f.record("ActivityViewed()") }
func (f *fakeCtrl) DismissNotice()                   { f.record("DismissNotice()") }
func (f *fakeCtrl) Retry(id int64, done func(error)) { f.record("Retry(%d)", id); ok(done) }
func (f *fakeCtrl) UploadAnyway(id int64, done func(error)) {
	f.record("UploadAnyway(%d)", id)
	ok(done)
}
func (f *fakeCtrl) CopyRemotePath(id int64, done func(error)) {
	f.record("CopyRemotePath(%d)", id)
	ok(done)
}
func (f *fakeCtrl) Cancel(id int64)                        { f.record("Cancel(%d)", id) }
func (f *fakeCtrl) DeleteEntry(id int64, done func(error)) { f.record("DeleteEntry(%d)", id); ok(done) }
func (f *fakeCtrl) ClearHistory(done func(error))          { f.record("ClearHistory()"); ok(done) }

// newTester shows the app over a fake controller.
func newTester(t *testing.T, f *fakeCtrl) (*App, *ui.Tester) {
	t.Helper()
	opened := []string{}
	a := NewApp(f, Platform{Now: func() time.Time { return now }, Version: "0.1.0", DefaultStaging: `C:\data\staging`,
		OpenFile: func(p string) { opened = append(opened, p) }}, nil)
	tt := ui.NewTester(a.View, 1180, 820)
	return a, tt
}

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("click %q: %v; texts %q", label, err, tt.Texts())
	}
}

func hasText(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	if !tt.HasText(s) {
		t.Errorf("no text %q in %q", s, tt.Texts())
	}
}

func hasTextContaining(t *testing.T, tt *ui.Tester, sub string) {
	t.Helper()
	for _, s := range tt.Texts() {
		if strings.Contains(s, sub) {
			return
		}
	}
	t.Errorf("no text containing %q in %q", sub, tt.Texts())
}

func sshtestKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	return sshtest.NewSigner(t).PublicKey()
}

// focusByTab presses Tab until the element named name has the focus, as
// a keyboard user reaches a field below the fold.
func focusByTab(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	for range 80 {
		if tt.Focused(name) {
			return
		}
		tt.Key(0, ui.KeyTab)
	}
	t.Fatalf("Tab never reached %q", name)
}
