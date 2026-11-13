package main

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/capture"
	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/store"
)

// Task 12.2: four state icons composed from resources/icon.png.
func TestTrayIcons(t *testing.T) {
	icons, err := trayIcons(appIcon)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]core.TrayState{}
	for _, s := range []core.TrayState{core.TrayMonitoring, core.TrayPaused, core.TrayUploading, core.TrayError} {
		b := icons[s]
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if r := img.Bounds(); r.Dx() != TrayIconSize || r.Dy() != TrayIconSize {
			t.Errorf("%s is %v", s, r)
		}
		if other, dup := seen[string(b)]; dup {
			t.Errorf("%s looks like %s", s, other)
		}
		seen[string(b)] = s
	}
}

func TestTrayTooltipAndMenu(t *testing.T) {
	dev := store.Profile{ID: 1, Name: "dev-box"}
	s := &core.State{Settings: store.Settings{Monitoring: true}, Profiles: []store.Profile{dev}, Active: &dev,
		Tray: core.TrayMonitoring}
	m := core.Tray(s)
	if got := trayTooltip(m); got != "SSHClipboard — dev-box — monitoring" {
		t.Errorf("tooltip %q", got)
	}
	var calls []string
	act := trayActions{
		setMonitoring: func(on bool) { calls = append(calls, fmt.Sprint("monitor ", on)) },
		uploadNow:     func() { calls = append(calls, "upload") },
		setActive:     func(id int64) { calls = append(calls, fmt.Sprint("active ", id)) },
		open:          func() { calls = append(calls, "open") },
		quit:          func() { calls = append(calls, "quit") },
	}
	items := trayMenu(m, act)
	var labels []string
	for _, it := range items {
		labels = append(labels, it.Label)
		if it.Click != nil {
			it.Click(it, nil)
		}
		for _, sub := range it.Submenu {
			if !sub.Checked || sub.Label != "dev-box" {
				t.Errorf("server item %+v", sub)
			}
			sub.Click(sub, nil)
		}
	}
	if got := strings.Join(labels, "|"); got != "Pause monitoring|Upload clipboard now|Servers|||Open SSHClipboard|Quit" &&
		got != "Pause monitoring|Upload clipboard now|Servers||Open SSHClipboard|Quit" {
		t.Errorf("menu %q", got)
	}
	if got := strings.Join(calls, ","); got != "monitor false,upload,active 1,open,quit" {
		t.Errorf("actions %q", got)
	}
	// Spec "Pause from the tray": then the item reads Resume monitoring.
	s.Settings.Monitoring, s.Tray, s.Active = false, core.TrayPaused, nil
	m = core.Tray(s)
	if items := trayMenu(m, act); items[0].Label != "Resume monitoring" {
		t.Errorf("paused menu starts with %q", items[0].Label)
	}
	if got := trayTooltip(m); got != "SSHClipboard — no server — paused" {
		t.Errorf("tooltip %q", got)
	}
}

// Spec "Error notification": the file name and the error.
func TestNoticeText(t *testing.T) {
	for _, c := range []struct {
		n           core.Notice
		title, body string
	}{
		{core.Notice{Kind: core.NoticeFailed, Name: "shot.png", Err: errors.New("dial tcp: connection refused")},
			"Upload failed: shot.png", "dial tcp: connection refused"},
		{core.Notice{Kind: core.NoticeUploaded, Name: "shot.png", RemotePath: "/home/alice/clipboard/shot.png"},
			"Uploaded shot.png", "/home/alice/clipboard/shot.png"},
		{core.Notice{Kind: core.NoticeHostKey, ProfileName: "dev-box"}, "Verify dev-box",
			"Open SSHClipboard to check the server's host key. Uploads wait until then."},
		{core.Notice{Kind: core.NoticeStillRunning}, "SSHClipboard is still running",
			"It keeps uploading from the tray. To quit, choose Quit in the tray menu."},
	} {
		title, body := noticeText(c.n)
		if title != c.title || body != c.body {
			t.Errorf("%d: %q / %q", c.n.Kind, title, body)
		}
	}
}

func TestTooLarge(t *testing.T) {
	if err := tooLarge(errors.New("mygo: clipboard representation exceeds 64 MiB")); !errors.Is(err, capture.ErrTooLarge) {
		t.Errorf("%v", err)
	}
	if err := tooLarge(errors.New("busy")); errors.Is(err, capture.ErrTooLarge) {
		t.Error("an unrelated error is too large")
	}
	if tooLarge(nil) != nil {
		t.Error("nil")
	}
}

// The window shown when the data comes from a newer version.
func TestFatalView(t *testing.T) {
	err := &store.NewerSchemaError{Found: 5, Supported: 1}
	tt := ui.NewTester(fatalView(err), 560, 220)
	found := false
	for _, s := range tt.Texts() {
		found = found || strings.Contains(s, "created by a newer version")
	}
	if !found || !tt.HasText("SSHClipboard") {
		t.Errorf("texts %q", tt.Texts())
	}
}
