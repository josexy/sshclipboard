package rules

import (
	"errors"
	"regexp/syntax"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/store"
)

func i64(v int64) *int64 { return &v }
func ip(v int) *int      { return &v }

func rule(id int64, name string, action store.Action, c store.Conditions) store.Rule {
	return store.Rule{ID: id, Position: int(id), Name: name, Enabled: true, Action: action, Conditions: c}
}

func kinds(k ...store.Kind) []store.Kind { return k }

// 5.1 Compile and Validate.

func TestValidateRejects(t *testing.T) {
	for _, c := range []struct {
		name  string
		rule  store.Rule
		field string
	}{
		// Spec "Invalid regex rejected".
		{"bad regex", rule(1, "r", store.ActionUpload, store.Conditions{TextRegex: "([a-z"}), FieldRegex},
		{"size min > max", rule(1, "r", store.ActionSkip, store.Conditions{MinSize: i64(10), MaxSize: i64(5)}), FieldSize},
		{"width min > max", rule(1, "r", store.ActionSkip, store.Conditions{MinWidth: ip(100), MaxWidth: ip(10)}), FieldWidth},
		{"height min > max", rule(1, "r", store.ActionSkip, store.Conditions{MinHeight: ip(2), MaxHeight: ip(1)}), FieldHeight},
		{"negative size", rule(1, "r", store.ActionSkip, store.Conditions{MaxSize: i64(-1)}), FieldSize},
		{"bad glob", rule(1, "r", store.ActionSkip, store.Conditions{Glob: "[a-"}), FieldGlob},
		{"bad template", store.Rule{ID: 1, Name: "r", Action: store.ActionUpload, TemplateOverride: "{nmae}.{ext}"}, FieldTemplate},
		{"bad dir", store.Rule{ID: 1, Name: "r", Action: store.ActionUpload, DirOverride: "~/../etc"}, FieldDir},
		{"no name", store.Rule{ID: 1, Name: " ", Action: store.ActionUpload}, FieldName},
		{"bad action", store.Rule{ID: 1, Name: "r", Action: "delete"}, FieldAction},
	} {
		err := Validate(c.rule)
		if err == nil || err.Field(c.field) == nil {
			t.Errorf("%s: got %v, want an error on %s", c.name, err, c.field)
		}
	}
	err := Validate(rule(1, "r", store.ActionUpload, store.Conditions{TextRegex: "([a-z"}))
	// The field shows the parser's own message.
	var se *syntax.Error
	if !errors.As(err.Field(FieldRegex), &se) {
		t.Errorf("regex error %v is not the parse error", err.Field(FieldRegex))
	}
	if err := Validate(rule(1, "ok", store.ActionUpload, store.Conditions{
		Kinds: kinds(store.KindText), TextRegex: `^https?://`, MinSize: i64(1), MaxSize: i64(1),
	})); err != nil {
		t.Errorf("valid rule: %v", err)
	}
}

func TestCompileReportsPerRuleErrors(t *testing.T) {
	set, errs := Compile([]store.Rule{
		rule(1, "good", store.ActionSkip, store.Conditions{Kinds: kinds(store.KindText)}),
		rule(2, "bad regex", store.ActionSkip, store.Conditions{TextRegex: "([a-z"}),
		rule(3, "bad size", store.ActionSkip, store.Conditions{MinSize: i64(9), MaxSize: i64(1)}),
	}, store.ActionUpload)
	if len(errs) != 2 {
		t.Fatalf("errors %v", errs)
	}
	var re *RuleError
	if !errors.As(errs[0], &re) || re.RuleID != 2 || !errors.As(errs[1], &re) || re.RuleID != 3 {
		t.Errorf("errors %v", errs)
	}
	if d := set.Evaluate(Entry{Kind: store.KindText, FileName: "text.txt", Text: "x"}); d.RuleName != "good" {
		t.Errorf("valid rules still evaluate: %+v", d)
	}
}

// 5.2 Evaluate: every capture-rules scenario.

func file(name string, size int64) Entry {
	return Entry{Kind: store.KindFile, FileName: name, Size: size}
}
func image(w, h int) Entry {
	return Entry{Kind: store.KindImage, FileName: PseudoName(store.KindImage), Width: w, Height: h, Size: 1000}
}
func text(s string) Entry {
	return Entry{Kind: store.KindText, FileName: PseudoName(store.KindText), Text: s, Size: int64(len(s))}
}

func compileOK(t *testing.T, def store.Action, rs ...store.Rule) *Set {
	t.Helper()
	set, errs := Compile(rs, def)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	return set
}

func TestEvaluateScenarios(t *testing.T) {
	skipExe := rule(1, "No executables", store.ActionSkip, store.Conditions{Extensions: []string{"exe"}})
	pngToShots := rule(1, "PNG to shots", store.ActionUpload, store.Conditions{Extensions: []string{"png"}})
	pngToShots.DirOverride = "~/shots"
	skipImages := rule(2, "Skip images", store.ActionSkip, store.Conditions{Kinds: kinds(store.KindImage)})
	logs := rule(1, "Logs", store.ActionUpload, store.Conditions{Kinds: kinds(store.KindFile),
		Extensions: []string{"log", " .TXT"}, MaxSize: i64(10 * store.MB)})
	tiny := rule(1, "Tiny images", store.ActionSkip, store.Conditions{Kinds: kinds(store.KindImage), MaxWidth: ip(31)})
	urls := rule(1, "URLs", store.ActionUpload, store.Conditions{Kinds: kinds(store.KindText), TextRegex: `^https?://`})
	regexOnly := rule(1, "Regex only", store.ActionSkip, store.Conditions{TextRegex: `.`})
	glob := rule(1, "Glob", store.ActionSkip, store.Conditions{Glob: "IMAGE.*"})
	disabled := rule(1, "Off", store.ActionSkip, store.Conditions{})
	disabled.Enabled = false
	defaults := store.DefaultRules()
	defaults[0].ID, defaults[1].ID = 1, 2

	for _, c := range []struct {
		name     string
		set      *Set
		entry    Entry
		rule     string
		action   store.Action
		override string
	}{
		// "Mixed files": entries of one files item get different outcomes.
		{"mixed: photo", compileOK(t, store.ActionUpload, skipExe), file("photo.jpg", 10), store.RuleDefault, store.ActionUpload, ""},
		{"mixed: setup", compileOK(t, store.ActionUpload, skipExe), file("setup.EXE", 10), "No executables", store.ActionSkip, ""},
		// "Earlier rule wins".
		{"earlier wins", compileOK(t, store.ActionUpload, pngToShots, skipImages), image(800, 600), "PNG to shots", store.ActionUpload, "~/shots"},
		// "No rule matches" with default skip.
		{"no match", compileOK(t, store.ActionSkip, skipExe), file("a.txt", 1), store.RuleDefault, store.ActionSkip, ""},
		// "Extension and size".
		{"ext and size", compileOK(t, store.ActionSkip, logs), file("server.LOG", 2*store.MB), "Logs", store.ActionUpload, ""},
		{"too big", compileOK(t, store.ActionSkip, logs), file("server.log", 11*store.MB), store.RuleDefault, store.ActionSkip, ""},
		{"wrong kind", compileOK(t, store.ActionSkip, logs), text("x"), store.RuleDefault, store.ActionSkip, ""},
		// "Tiny image filtered".
		{"tiny image", compileOK(t, store.ActionUpload, tiny), image(16, 16), "Tiny images", store.ActionSkip, ""},
		{"normal image", compileOK(t, store.ActionUpload, tiny), image(32, 16), store.RuleDefault, store.ActionUpload, ""},
		// "Text regex".
		{"text regex", compileOK(t, store.ActionSkip, urls), text("https://example.com/a"), "URLs", store.ActionUpload, ""},
		{"text regex miss", compileOK(t, store.ActionSkip, urls), text("see https://x"), store.RuleDefault, store.ActionSkip, ""},
		// "Condition does not apply to kind".
		{"regex vs image", compileOK(t, store.ActionUpload, regexOnly), image(100, 100), store.RuleDefault, store.ActionUpload, ""},
		{"dims vs file", compileOK(t, store.ActionUpload, tiny), file("a.png", 1), store.RuleDefault, store.ActionUpload, ""},
		// "Glob on an image entry", case-insensitive.
		{"glob image", compileOK(t, store.ActionUpload, glob), image(10, 10), "Glob", store.ActionSkip, ""},
		{"glob ?", compileOK(t, store.ActionUpload, rule(1, "Q", store.ActionSkip, store.Conditions{Glob: "a?c.txt"})), file("ABC.txt", 1), "Q", store.ActionSkip, ""},
		{"glob text", compileOK(t, store.ActionUpload, rule(1, "T", store.ActionSkip, store.Conditions{Extensions: []string{"txt"}})), text("hi"), "T", store.ActionSkip, ""},
		// Disabled rules are ignored.
		{"disabled", compileOK(t, store.ActionUpload, disabled), file("a", 1), store.RuleDefault, store.ActionUpload, ""},
		// "Fresh install": text skipped by "Skip text", a screenshot uploads.
		{"fresh text", compileOK(t, store.ActionUpload, defaults...), text("hello world"), "Skip text", store.ActionSkip, ""},
		{"fresh image", compileOK(t, store.ActionUpload, defaults...), image(1280, 720), store.RuleDefault, store.ActionUpload, ""},
		{"fresh big file", compileOK(t, store.ActionUpload, defaults...), file("iso.iso", 500*store.MB), "Skip large files", store.ActionSkip, ""},
		{"fresh small file", compileOK(t, store.ActionUpload, defaults...), file("a.pdf", 99*store.MB), store.RuleDefault, store.ActionUpload, ""},
	} {
		d := c.set.Evaluate(c.entry)
		if d.RuleName != c.rule || d.Action != c.action || d.DirOverride != c.override {
			t.Errorf("%s: got %+v, want rule %q action %s override %q", c.name, d, c.rule, c.action, c.override)
		}
		if d.Default() != (c.rule == store.RuleDefault) {
			t.Errorf("%s: Default() %v", c.name, d.Default())
		}
	}
}

// Spec "Reorder applies immediately": the next evaluation uses the new
// order.
func TestReorderAppliesToNextEntry(t *testing.T) {
	up := rule(1, "Upload PDFs", store.ActionUpload, store.Conditions{Extensions: []string{"pdf"}})
	skip := rule(2, "Skip documents", store.ActionSkip, store.Conditions{Glob: "*.pdf"})
	if d := compileOK(t, store.ActionUpload, up, skip).Evaluate(file("a.pdf", 1)); d.Action != store.ActionUpload {
		t.Errorf("before: %+v", d)
	}
	if d := compileOK(t, store.ActionUpload, skip, up).Evaluate(file("a.pdf", 1)); d.Action != store.ActionSkip || d.RuleName != "Skip documents" {
		t.Errorf("after moving the skip rule up: %+v", d)
	}
}

func TestSkipRuleCarriesNoOverrides(t *testing.T) {
	r := rule(1, "S", store.ActionSkip, store.Conditions{})
	r.DirOverride, r.TemplateOverride = "x", "{name}"
	if d := compileOK(t, store.ActionUpload, r).Evaluate(file("a", 1)); d.DirOverride != "" || d.TemplateOverride != "" {
		t.Errorf("%+v", d)
	}
}

// 5.3 Directory overrides.

func TestResolveDir(t *testing.T) {
	v := naming.Vars{Now: time.Date(2026, 11, 12, 15, 30, 45, 0, time.Local), Kind: "image"}
	r := rule(1, "Shots", store.ActionUpload, store.Conditions{Kinds: kinds(store.KindImage)})
	r.DirOverride = "screenshots/{date}"
	d := compileOK(t, store.ActionUpload, r).Evaluate(image(10, 10))
	// Spec "Relative directory override".
	if got, err := d.Dir("~/clipboard", v); err != nil || got != "~/clipboard/screenshots/2026-11-12" {
		t.Errorf("relative: %q %v", got, err)
	}
	for _, c := range []struct{ profile, override, want string }{
		{"~/clipboard", "/srv/{kind}", "/srv/image"},
		{"~/clipboard", "~/shots", "~/shots"},
		{"~/clipboard/{date:YYYY-MM}", "", "~/clipboard/2026-11"},
		{"/data", "in", "/data/in"},
	} {
		if got, err := ResolveDir(c.profile, c.override, v); err != nil || got != c.want {
			t.Errorf("ResolveDir(%q, %q) = %q %v, want %q", c.profile, c.override, got, err, c.want)
		}
	}
	if _, err := ResolveDir("~/clipboard", "../x", v); err == nil {
		t.Error("a traversing override was resolved")
	}
}
