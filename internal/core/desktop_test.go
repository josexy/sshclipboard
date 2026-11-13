package core

import (
	"errors"
	"reflect"
	"testing"

	"github.com/josexy/sshclipboard/internal/store"
)

func kinds(items []TrayItem) []TrayItemKind {
	var out []TrayItemKind
	for _, it := range items {
		out = append(out, it.Kind)
	}
	return out
}

// Task 12.2: the tray menu model for each state.
func TestTrayModel(t *testing.T) {
	dev, prod := store.Profile{ID: 1, Name: "dev-box"}, store.Profile{ID: 2, Name: "prod-box"}
	s := &State{Settings: store.Settings{Monitoring: true}, Profiles: []store.Profile{dev, prod}, Active: &dev, Tray: TrayMonitoring}
	m := Tray(s)
	want := []TrayItemKind{TrayItemPause, TrayItemUploadNow, TrayItemServers, TrayItemSeparator, TrayItemOpen, TrayItemQuit}
	if !reflect.DeepEqual(kinds(m.Items), want) || m.Icon != TrayMonitoring || m.Server != "dev-box" {
		t.Errorf("monitoring %+v", m)
	}
	srv := m.Items[2].Submenu
	if len(srv) != 2 || !srv[0].Checked || srv[1].Checked || srv[1].Label != "prod-box" || srv[1].ProfileID != 2 {
		t.Errorf("servers %+v", srv)
	}

	// Spec "Pause from the tray": Resume, and the paused icon.
	paused := *s
	paused.Settings.Monitoring, paused.Tray = false, TrayPaused
	pm := Tray(&paused)
	if pm.Items[0].Kind != TrayItemResume || pm.Icon != TrayPaused || pm.Equal(m) {
		t.Errorf("paused %+v", pm)
	}
	// Spec "Switch from the tray": the check follows the active profile.
	switched := *s
	switched.Active = &prod
	sm := Tray(&switched)
	if !sm.Items[2].Submenu[1].Checked || sm.Items[2].Submenu[0].Checked || sm.Server != "prod-box" {
		t.Errorf("switched %+v", sm)
	}
	// No server: an empty submenu.
	none := &State{Settings: store.Settings{Monitoring: true}}
	if nm := Tray(none); nm.Server != "" || len(nm.Items[2].Submenu) != 0 {
		t.Errorf("no server %+v", nm)
	}
	if !Tray(s).Equal(m) {
		t.Error("the same state gives a different model")
	}
	errState := *s
	errState.Tray = TrayError
	if Tray(&errState).Equal(m) {
		t.Error("the error icon is not a change")
	}
}

// fakeRegistrar holds hotkeys; taken ones fail as another app's do.
type fakeRegistrar struct {
	held  map[string]bool
	taken map[string]bool
	log   []string
}

func (f *fakeRegistrar) Register(acc string, fn func()) error {
	f.log = append(f.log, "register "+acc)
	if f.taken[acc] || f.held[acc] {
		return errors.New("hotkey in use")
	}
	f.held[acc] = true
	return nil
}

func (f *fakeRegistrar) Unregister(acc string) {
	f.log = append(f.log, "unregister "+acc)
	delete(f.held, acc)
}

// Task 12.3: register the new hotkey before unregistering the old one;
// spec "Hotkey taken" keeps the previous one.
func TestHotkeySwap(t *testing.T) {
	reg := &fakeRegistrar{held: map[string]bool{}, taken: map[string]bool{"Ctrl+C": true}}
	pressed := 0
	h := NewHotkey(reg, func() { pressed++ })
	if err := h.Start("Ctrl+Alt+U"); err != nil || h.Current() != "Ctrl+Alt+U" || !reg.held["Ctrl+Alt+U"] {
		t.Fatalf("start: %v %q", err, h.Current())
	}
	if err := h.Swap("Ctrl+Alt+U", "Ctrl+C"); err == nil {
		t.Error("a taken hotkey registered")
	}
	if h.Current() != "Ctrl+Alt+U" || !reg.held["Ctrl+Alt+U"] {
		t.Errorf("the previous hotkey was lost: %q %v", h.Current(), reg.held)
	}
	reg.log = nil
	if err := h.Swap("Ctrl+Alt+U", "Ctrl+Shift+V"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reg.log, []string{"register Ctrl+Shift+V", "unregister Ctrl+Alt+U"}) {
		t.Errorf("order %q", reg.log)
	}
	if err := h.Swap("Ctrl+Shift+V", ""); err != nil || h.Current() != "" || len(reg.held) != 0 {
		t.Errorf("disable: %v %q %v", err, h.Current(), reg.held)
	}
	if err := h.Swap("", "Ctrl+Alt+U"); err != nil || !reg.held["Ctrl+Alt+U"] {
		t.Errorf("enable again: %v", err)
	}
	if err := h.Swap("Ctrl+Alt+U", "Ctrl+Alt+U"); err != nil {
		t.Errorf("same hotkey: %v", err)
	}
}

// The hotkey hook through SetSetting: a taken hotkey is an error and the
// setting keeps its value.
func TestHotkeySettingTaken(t *testing.T) {
	e := newEnv(t, envOpts{})
	reg := &fakeRegistrar{held: map[string]bool{}, taken: map[string]bool{"Ctrl+C": true}}
	h := NewHotkey(reg, func() {})
	h.Start(store.DefaultHotkey)
	e.c.o.Hooks.Hotkey = h.Swap
	err := call(t, func(d func(error)) { e.c.SetSetting(store.KeyHotkey, "Ctrl+C", d) })
	if !errors.Is(err, ErrHotkeyTaken) {
		t.Errorf("got %v", err)
	}
	if s, _ := e.st.Settings.Load(ctx); s.Hotkey != store.DefaultHotkey || h.Current() != store.DefaultHotkey {
		t.Errorf("setting %q, registered %q", s.Hotkey, h.Current())
	}
}
