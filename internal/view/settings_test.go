package view

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/store"
)

var ctx = context.Background()

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// Task 11.6: each control persists through the store.
func TestSettingsPersist(t *testing.T) {
	e := newReal(t, 1000)
	tt := e.tt
	click(t, tt, "Settings")

	// Spec "Survives a crash": a toggled setting is in the database.
	click(t, tt, "Start at login")
	e.settle("start at login", func(s store.Settings) bool { return s.StartAtLogin })
	click(t, tt, "Start hidden in the tray")
	e.settle("start hidden", func(s store.Settings) bool { return s.StartHidden })
	click(t, tt, "Close to tray")
	e.settle("close to tray", func(s store.Settings) bool { return !s.CloseToTray })
	click(t, tt, "Skip confidential content")
	e.settle("confidential", func(s store.Settings) bool { return !s.ConfidentialProtection })

	click(t, tt, "Automatic")
	click(t, tt, "Manual (hotkey or tray only)")
	e.settle("capture mode", func(s store.Settings) bool { return s.CaptureMode == store.CaptureManual })

	// Spec "Switch to dark".
	click(t, tt, "Dark")
	e.settle("appearance", func(s store.Settings) bool { return s.Appearance == store.AppearanceDark })

	click(t, tt, "Errors only")
	click(t, tt, "All")
	e.settle("notifications", func(s store.Settings) bool { return s.Notifications == store.NotifyAll })

	replaceText(t, tt, "Keep at most (entries)", "50")
	tt.Key(0, ui.KeyEnter)
	e.settle("retention count", func(s store.Settings) bool { return s.RetentionCount == 50 })
	replaceText(t, tt, "Delete entries after (days)", "7")
	tt.Key(0, ui.KeyEnter)
	e.settle("retention days", func(s store.Settings) bool { return s.RetentionDays == 7 })

	e.picked = "/mnt/d/clip-staging"
	click(t, tt, "Choose…")
	e.settle("staging folder", func(s store.Settings) bool { return s.StagingDir == "/mnt/d/clip-staging" })
	click(t, tt, "Use default")
	e.settle("staging default", func(s store.Settings) bool { return s.StagingDir == "" })

	replaceText(t, tt, "Hotkey", "Ctrl+Shift+V")
	click(t, tt, "Set")
	e.settle("hotkey", func(s store.Settings) bool { return s.Hotkey == "Ctrl+Shift+V" })
	click(t, tt, "Disable")
	e.settle("hotkey off", func(s store.Settings) bool { return s.Hotkey == "" })

	e.mu.Lock()
	hooks := e.hooks
	e.mu.Unlock()
	for _, want := range []string{"login true", "appearance dark", `hotkey "Ctrl+Alt+U"→"Ctrl+Shift+V"`, `hotkey "Ctrl+Shift+V"→""`} {
		if !slices.Contains(hooks, want) {
			t.Errorf("hook %q not applied: %q", want, hooks)
		}
	}
	hasText(t, tt, "Version 0.1.0")
}

// Spec "Hotkey taken": an error, and the previous hotkey stays.
func TestSettingsHotkeyTaken(t *testing.T) {
	e := newReal(t, 1000)
	tt := e.tt
	click(t, tt, "Settings")
	e.hotkeyErr = errTaken
	replaceText(t, tt, "Hotkey", "Ctrl+C")
	click(t, tt, "Set")
	e.settle("hotkey kept", func(s store.Settings) bool { return s.Hotkey == store.DefaultHotkey })
	for range 20 {
		e.settle("frame", func(store.Settings) bool { return true })
		if tt.HasText("This hotkey is unavailable: another application uses it.") {
			return
		}
	}
	t.Errorf("no error shown: %q", tt.Texts())
}
