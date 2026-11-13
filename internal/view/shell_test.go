package view

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/sshconn"
)

// Task 11.1: every page opens from the sidebar with the header kept, and
// the header commands the Controller.
func TestShellNavigatesEveryPage(t *testing.T) {
	f := newFake()
	a, tt := newTester(t, f)
	hasText(t, tt, "Activity")
	for _, page := range []struct{ item, heading, id string }{
		{"Servers", "Servers", PageServers},
		{"Rules", "Rules", PageRules},
		{"Naming", "File names", PageNaming},
		{"Settings", "Settings", PageSettings},
		{"Activity", "Activity", PageActivity},
	} {
		click(t, tt, page.item)
		if a.Page() != page.id {
			t.Errorf("clicking %s shows %s", page.item, a.Page())
		}
		hasText(t, tt, page.heading)
		// Spec "Navigate to Rules": the header stays visible.
		hasText(t, tt, "Upload clipboard now")
		hasText(t, tt, "Monitoring")
		hasText(t, tt, "Connected")
	}
}

func TestShellHeader(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	hasText(t, tt, "dev-box")
	// The monitoring switch, through its label.
	click(t, tt, "Monitoring")
	f.called(t, "SetMonitoring(false)")
	tt.Frame()
	if info, ok := tt.AccessByRole(ui.RoleSwitch, "Monitoring"); ok && info.Checked {
		t.Error("the switch still shows monitoring on")
	}
	click(t, tt, "Monitoring")
	f.called(t, "SetMonitoring(true)")

	click(t, tt, "Upload clipboard now")
	f.called(t, "CaptureNow()")

	// The active server select.
	click(t, tt, "dev-box")
	click(t, tt, "prod-box")
	f.called(t, "SetActiveProfile(2)")
}

func TestShellNoServerAndBanner(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) {
		s.Active, s.Settings.ActiveProfile = nil, 0
		s.Conn = sshconn.StatusDisconnected
		s.Notice = `C:\Users\alice\AppData\Roaming\SSHClipboard\sshclipboard.db.corrupt-20261112-153045`
	})
	_, tt := newTester(t, f)
	hasText(t, tt, "No server")
	hasText(t, tt, "Not connected")
	hasTextContaining(t, tt, "sshclipboard.db.corrupt-20261112-153045")
	click(t, tt, "Dismiss")
	f.called(t, "DismissNotice()")
}

// Spec "Quit with uploads in progress": the app asks first.
func TestQuitConfirmation(t *testing.T) {
	f := newFake()
	a, tt := newTester(t, f)
	quit := false
	a.AskQuit(2, func() { quit = true })
	tt.Frame()
	hasText(t, tt, "Quit while uploading?")
	hasTextContaining(t, tt, "2 uploads are in progress")
	click(t, tt, "Cancel")
	if quit {
		t.Fatal("Cancel quit")
	}
	a.AskQuit(1, func() { quit = true })
	tt.Frame()
	hasTextContaining(t, tt, "1 upload is in progress")
	click(t, tt, "Quit")
	if !quit {
		t.Error("Quit did not confirm")
	}
}
