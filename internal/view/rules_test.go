package view

import (
	"testing"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/store"
)

// Spec "Invalid regex rejected": not saved, and the field shows the parse
// error.
func TestRulesInvalidRegex(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Rules")
	click(t, tt, "Add rule")
	hasText(t, tt, "New rule")
	tt.Type("URLs") // the name field has the focus
	focusByTab(t, tt, "Text matches")
	tt.Type("([a-z")
	click(t, tt, "Save")
	f.called(t, "SaveRule(URLs)")
	if len(f.rules) != 0 {
		t.Error("an invalid rule was saved")
	}
	hasTextContaining(t, tt, "missing closing ]")
	hasText(t, tt, "New rule") // still open
}

func TestRulesSaveValidRule(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Rules")
	click(t, tt, "Add rule")
	tt.Type("Logs")
	click(t, tt, "Files")
	typeInto(t, tt, "Extensions", "log, .TXT")
	focusByTab(t, tt, "Maximum size (MB)")
	tt.Type("10")
	focusByTab(t, tt, "Folder")
	tt.Type("logs/{date}")
	click(t, tt, "Save")
	if len(f.rules) != 1 {
		t.Fatalf("rules %+v", f.rules)
	}
	r := f.rules[0]
	c := r.Conditions
	if r.Name != "Logs" || r.Action != store.ActionUpload || len(c.Kinds) != 1 || c.Kinds[0] != store.KindFile ||
		len(c.Extensions) != 2 || c.Extensions[1] != "TXT" || c.MaxSize == nil || *c.MaxSize != 10*store.MB ||
		r.DirOverride != "logs/{date}" || !r.Enabled {
		t.Errorf("saved %+v %+v", r, c)
	}
	if tt.HasText("New rule") {
		t.Error("the dialog stayed open")
	}
}

// Spec "Reorder applies immediately": Move up and drag both reorder.
func TestRulesReorder(t *testing.T) {
	f := newFake()
	a, tt := newTester(t, f)
	click(t, tt, "Rules")
	hasText(t, tt, "Skip text")
	hasText(t, tt, "Skip large files")
	if err := tt.Click("Move up: Skip large files"); err != nil {
		t.Fatal(err)
	}
	f.called(t, "MoveRule(2, -1)")
	if err := tt.Click("Move down: Skip text"); err != nil {
		t.Fatal(err)
	}
	f.called(t, "MoveRule(1, 1)")
	// A drag of the second row before the first.
	a.rules.list.Reorder([]int{1}, 0)
	f.called(t, "ReorderRules([2 1])")
}

func TestRulesToggleEditDeleteDefault(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Rules")
	if err := tt.Click("Enabled: Skip text"); err != nil {
		t.Fatal(err)
	}
	f.called(t, "SetRuleEnabled(1, false)")
	if err := tt.Click("Edit: Skip large files"); err != nil {
		t.Fatal(err)
	}
	hasText(t, tt, "Edit rule")
	click(t, tt, "Cancel")
	if err := tt.Click("Delete: Skip text"); err != nil {
		t.Fatal(err)
	}
	f.called(t, "DeleteRule(1)")
	click(t, tt, "Upload") // the default action select
	click(t, tt, "Skip")
	f.called(t, "SetDefaultAction(skip)")
}

// Spec "Dry run": the entry, its rule or "default action", the action,
// and the remote path.
func TestRulesDryRun(t *testing.T) {
	f := newFake()
	f.dry = []core.DryRunResult{{Name: "notes.md", Kind: store.KindFile, RuleName: store.RuleDefault,
		Action: store.ActionUpload, RemotePath: "/home/alice/clipboard/notes.md"}}
	_, tt := newTester(t, f)
	click(t, tt, "Rules")
	click(t, tt, "Test with current clipboard")
	f.called(t, "DryRun()")
	for _, s := range []string{"What the clipboard would do", "notes.md", "default action", "Upload", "/home/alice/clipboard/notes.md"} {
		hasText(t, tt, s)
	}
	f.dry = nil
	click(t, tt, "Test with current clipboard")
	hasText(t, tt, "The clipboard has nothing to upload.")
}
