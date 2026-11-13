package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// replaceText focuses a field by its accessible name and replaces its
// text, as select-all and typing do.
func replaceText(t *testing.T, tt *ui.Tester, name, text string) {
	t.Helper()
	info, ok := tt.AccessByRole(ui.RoleTextField, name)
	if !ok {
		t.Fatalf("no field %q", name)
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type(text)
}

// Spec "Unknown token": not saved, and the error names {nmae}.
func TestNamingUnknownToken(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Naming")
	replaceText(t, tt, "Files Template", "{nmae}.{ext}")
	hasText(t, tt, "{nmae} is an unknown token (position 1)")
	if info, _ := tt.AccessByRole(ui.RoleButton, "Save: Files"); !info.Disabled {
		t.Error("Save is enabled for an invalid template")
	}
	if err := tt.Click("Save: Files"); err == nil {
		tt.Frame()
	}
	if len(f.namings) != 0 {
		t.Errorf("saved %+v", f.namings)
	}
}

// Spec "Live preview": dev-box_example.txt for the active dev-box.
func TestNamingLivePreview(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Naming")
	hasText(t, tt, "Preview: screenshot_20261112-153045.png")
	hasText(t, tt, "Preview: example.txt")
	replaceText(t, tt, "Files Template", "{profile}_{name}.{ext}")
	hasText(t, tt, "Preview: dev-box_example.txt")
	// Transforms preview too, and Save sends the template.
	lc, ok := tt.Access("Lowercase: Files")
	if !ok {
		t.Fatal("no Lowercase: Files")
	}
	tt.ClickAt(lc.Bounds.X+lc.Bounds.W/2, lc.Bounds.Y+lc.Bounds.H/2)
	if err := tt.Click("Save: Files"); err != nil {
		t.Fatal(err)
	}
	f.called(t, "SaveNaming(file, {profile}_{name}.{ext})")
	if n := f.namings[0]; !n.Lowercase || n.SpacesToUnderscore {
		t.Errorf("saved %+v", n)
	}
	hasText(t, tt, "Saved")
}

func TestNamingTokenHelp(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Naming")
	click(t, tt, "Tokens")
	hasTextContaining(t, tt, "{date:FORMAT}")
}
