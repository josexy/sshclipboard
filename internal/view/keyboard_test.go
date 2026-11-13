package view

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

type stop struct {
	role ui.Role
	name string
}

func focused(tt *ui.Tester) (ui.AccessInfo, bool) {
	for _, a := range tt.AccessAll() {
		if a.Focused {
			return a, true
		}
	}
	return ui.AccessInfo{}, false
}

// controls returns the window's focusable controls in reading order, the
// sidebar and each radio group (a segmented control) counting as one stop,
// its chosen item, whose siblings the arrow keys reach.
func controls(tt *ui.Tester) []stop {
	var out []stop
	for _, a := range tt.AccessAll() {
		if !a.Focusable || a.Disabled || a.Role == ui.RoleTree {
			continue
		}
		if a.Role == ui.RoleTreeItem && !a.Selected {
			continue
		}
		if a.Role == ui.RoleRadio && !a.Checked {
			continue
		}
		out = append(out, stop{a.Role, a.Name})
	}
	return out
}

// cycle presses key until the focus comes back to where it started and
// returns the stops in order.
func cycle(t *testing.T, tt *ui.Tester, mods ui.Modifiers, n int) []stop {
	t.Helper()
	var seq []stop
	for range n * 2 {
		tt.Key(mods, ui.KeyTab)
		a, ok := focused(tt)
		if !ok {
			t.Fatal("Tab left nothing focused")
		}
		s := stop{a.Role, a.Name}
		if len(seq) > 0 && s == seq[0] {
			return seq
		}
		seq = append(seq, s)
	}
	t.Fatalf("Tab did not come back around: %v", seq)
	return nil
}

// rotate returns s starting at first.
func rotate(s []stop, first stop) []stop {
	i := slices.Index(s, first)
	if i < 0 {
		return s
	}
	return append(slices.Clone(s[i:]), s[:i]...)
}

// checkTraversal: Tab reaches every control in reading order, Shift+Tab
// in the reverse order.
func checkTraversal(t *testing.T, tt *ui.Tester) {
	t.Helper()
	want := controls(tt)
	if len(want) < 10 {
		t.Fatalf("only %d controls: %v", len(want), want)
	}
	fwd := cycle(t, tt, 0, len(want))
	if got := rotate(fwd, want[0]); !slices.Equal(got, want) {
		t.Errorf("Tab order\n got %v\nwant %v", got, want)
	}
	back := cycle(t, tt, ui.Shift, len(want))
	rev := slices.Clone(want)
	slices.Reverse(rev)
	if got := rotate(back, rev[0]); !slices.Equal(got, rev) {
		t.Errorf("Shift+Tab order\n got %v\nwant %v", got, rev)
	}
}

// ringShows tells whether the focused control at r draws a ring outside
// its edge: the pixel just left of it differs from an unfocused frame.
func ringShows(tt *ui.Tester, name string, before [3]uint8) bool {
	return pixelLeftOf(tt, name) != before
}

// pixelLeftOf samples just outside a control's left edge, where the focus
// ring draws (offset 1, width 2).
func pixelLeftOf(tt *ui.Tester, name string) [3]uint8 {
	a, _ := tt.Access(name)
	r := a.Bounds
	c := tt.Image().RGBAAt(int(r.X-2), int(r.Y+r.H/2))
	return [3]uint8{c.R, c.G, c.B}
}

// Spec "Keyboard only" on the Servers view.
func TestKeyboardServers(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Servers")
	click(t, tt, "prod-box")
	checkTraversal(t, tt)

	// Enter clicks a button, with a visible focus ring.
	before := pixelLeftOf(tt, "Test connection")
	focusByTab(t, tt, "Test connection")
	if !ringShows(tt, "Test connection", before) {
		t.Error("no focus ring on Test connection")
	}
	tt.Key(0, ui.KeyEnter)
	f.called(t, "TestConnection(prod-box)")
	focusByTab(t, tt, "Save")
	tt.Key(0, ui.KeyEnter)
	f.called(t, "SaveProfile(prod-box)")

	// Space turns a switch over.
	focusByTab(t, tt, "Create missing folders")
	tt.Key(0, ui.KeySpace)
	if a, _ := tt.AccessByRole(ui.RoleSwitch, "Create missing folders"); a.Checked {
		t.Error("Space did not turn the switch off")
	}
	focusByTab(t, tt, "Save")
	tt.Key(0, ui.KeySpace)
	if in := f.saved[len(f.saved)-1]; in.Profile.CreateDirs {
		t.Errorf("saved %+v", in.Profile)
	}
	// And a button that opens a form.
	focusByTab(t, tt, "Add server")
	tt.Key(0, ui.KeySpace)
	hasText(t, tt, "New server")
}

// Spec "Keyboard only" on the Settings view.
func TestKeyboardSettings(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	tt.SetSize(1180, 1000)
	click(t, tt, "Settings")
	checkTraversal(t, tt)

	focusByTab(t, tt, "Start at login")
	tt.Key(0, ui.KeySpace)
	f.called(t, "SetSetting(start_at_login, true)")
	focusByTab(t, tt, "Close to tray")
	tt.Key(0, ui.KeySpace)
	f.called(t, "SetSetting(close_to_tray, false)")
	focusByTab(t, tt, "Disable")
	tt.Key(0, ui.KeyEnter)
	f.called(t, "SetSetting(hotkey, )")

	// The arrows reach the other appearances.
	focusByTab(t, tt, "System")
	tt.Key(0, ui.KeyRight)
	if !tt.Focused("Light") {
		t.Error("Right did not reach Light")
	}
	tt.Key(0, ui.KeySpace)
	f.called(t, "SetSetting(appearance, light)")
}
