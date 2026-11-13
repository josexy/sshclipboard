package view

import (
	"errors"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
)

// typeInto focuses a labelled field and types.
func typeInto(t *testing.T, tt *ui.Tester, label, text string) {
	t.Helper()
	click(t, tt, label)
	tt.Type(text)
}

// Spec "Create a profile with defaults".
func TestServersCreateWithDefaults(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Servers")
	click(t, tt, "Add server")
	hasText(t, tt, "New server")
	typeInto(t, tt, "Name", "staging")
	typeInto(t, tt, "Host", "10.0.0.7")
	typeInto(t, tt, "Username", "alice")
	click(t, tt, "Save")
	if len(f.saved) != 1 {
		t.Fatalf("saved %d", len(f.saved))
	}
	p := f.saved[0].Profile
	if p.Name != "staging" || p.Host != "10.0.0.7" || p.Username != "alice" || p.Port != 22 ||
		p.RemoteDir != "~/clipboard" || p.AuthMethod != store.AuthPassword || p.ConflictPolicy != store.ConflictRename ||
		p.FileMode != -1 || !p.CopyBack || !p.CreateDirs || p.CopySeparator != " " || p.ID != 0 {
		t.Errorf("saved %+v", p)
	}
	hasText(t, tt, "Saved")
}

// Spec "Reject invalid profile": the invalid field shows its error.
func TestServersInvalidFields(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Servers")
	click(t, tt, "Add server")
	typeInto(t, tt, "Name", "x")
	typeInto(t, tt, "Username", "alice")
	click(t, tt, "Port")
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyBackspace)
	tt.Type("70000")
	click(t, tt, "Save")
	hasText(t, tt, "Required") // the empty host
	hasText(t, tt, "Enter a port from 1 to 65535")
	// A name another server uses.
	f.saveErr = core.FieldErrors{core.FieldName: core.ErrNameTaken}
	typeInto(t, tt, "Host", "h")
	click(t, tt, "Port")
	for range 5 {
		tt.Key(0, ui.KeyBackspace)
	}
	tt.Type("22")
	click(t, tt, "Save")
	hasText(t, tt, "Another server already uses this name")
}

// Secrets are never shown: a stored password only says so.
func TestServersSecretsNeverPrefill(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Servers")
	// The header's select also says dev-box; the list's button has the
	// Active badge.
	click(t, tt, "Active")
	// The password field is empty, and its placeholder says one is saved.
	pw, ok := tt.AccessByRole(ui.RoleTextField, "Password")
	if !ok || pw.Value != "" || pw.Placeholder != "Saved. Type to replace it." {
		t.Fatalf("password field %+v", pw)
	}
	tt.ClickAt(pw.Bounds.X+pw.Bounds.W/2, pw.Bounds.Y+pw.Bounds.H/2)
	tt.Type("new-secret")
	click(t, tt, "Save")
	in := f.saved[len(f.saved)-1]
	if !in.Password.Set || in.Password.Value != "new-secret" || in.Passphrase.Set {
		t.Errorf("secrets %+v %+v", in.Password, in.Passphrase)
	}
}

func TestServersAuthMethodSwitch(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Servers")
	click(t, tt, "prod-box")
	click(t, tt, "Password") // the select shows the method
	click(t, tt, "Private key")
	hasText(t, tt, "Private key file")
	hasText(t, tt, "Key passphrase")
	hasText(t, tt, "Browse…")
	click(t, tt, "Private key")
	click(t, tt, "SSH agent")
	hasTextContaining(t, tt, "OpenSSH Authentication Agent")
}

func TestServersTestConnectionPanel(t *testing.T) {
	f := newFake()
	f.test = sshconn.CheckResult{Dir: "/home/bob/clipboard"}
	_, tt := newTester(t, f)
	click(t, tt, "Servers")
	click(t, tt, "prod-box")
	click(t, tt, "Test connection")
	f.called(t, "TestConnection(prod-box)")
	hasText(t, tt, "Connected. Uploads go to /home/bob/clipboard")
	f.test = sshconn.CheckResult{Dir: "/home/bob/clipboard", Step: sshconn.StepRemoteDir, Err: errors.New("permission denied")}
	click(t, tt, "Test connection")
	hasText(t, tt, "Failed while checking the remote folder: permission denied")
}

func TestServersDuplicateDeleteActivate(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Servers")
	click(t, tt, "prod-box")
	click(t, tt, "Make active")
	f.called(t, "SetActiveProfile(2)")
	click(t, tt, "Duplicate")
	f.called(t, "DuplicateProfile(2)")
	click(t, tt, "Delete")
	hasText(t, tt, "Delete “prod-box”?")
	click(t, tt, "Delete") // the alert's default button comes last
	f.called(t, "DeleteProfile(2)")
}

// Spec "First connection": the key type and fingerprint, and Trust saves.
func TestServersTrustDialog(t *testing.T) {
	f := newFake()
	key := sshtestKey(t)
	f.edit(func(s *core.State) {
		s.HostKey = &core.HostKeyPrompt{ProfileID: 1, ProfileName: "dev-box",
			Err: &sshconn.HostKeyError{Host: "10.0.0.5", Port: 22, Key: key}}
	})
	_, tt := newTester(t, f)
	hasText(t, tt, "Verify dev-box")
	hasTextContaining(t, tt, "ssh-ed25519 SHA256:")
	click(t, tt, "Trust")
	f.called(t, "TrustHostKey()")
	tt.Frame()
	if tt.HasText("Verify dev-box") {
		t.Error("the dialog stayed open")
	}
}

// Spec "Changed host key": old and new fingerprints, and Replace.
func TestServersReplaceDialog(t *testing.T) {
	f := newFake()
	key := sshtestKey(t)
	f.edit(func(s *core.State) {
		s.HostKey = &core.HostKeyPrompt{ProfileID: 1, ProfileName: "dev-box",
			Err: &sshconn.HostKeyError{Host: "10.0.0.5", Port: 22, Key: key,
				Known: &store.KnownHost{Fingerprint: "SHA256:oldoldold"}}}
	})
	_, tt := newTester(t, f)
	hasText(t, tt, "The host key of dev-box changed")
	hasTextContaining(t, tt, "Trusted: SHA256:oldoldold")
	click(t, tt, "Cancel")
	f.called(t, "DismissHostKey()")
}

// Spec "Primary action color": "Add server" is filled with the terracotta
// accent in both themes.
func TestAddServerIsAccent(t *testing.T) {
	for _, dark := range []bool{false, true} {
		f := newFake()
		_, tt := newTester(t, f)
		tt.SetDark(dark)
		click(t, tt, "Servers")
		r, ok := tt.Find("Add server")
		if !ok {
			t.Fatal("no Add server")
		}
		// A point inside the button, left of its label.
		got := tt.Image().RGBAAt(int(r.X-6), int(r.Y+r.H/2))
		want := rgba(PaletteFor(dark).Theme.Accent)
		if got != want {
			t.Errorf("dark %v: button fill %v, want %v", dark, got, want)
		}
	}
}
