package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/josexy/sshclipboard/internal/store"
)

// Task 13.4: every relative link of the README points at a file, and every
// documented default is the shipped one.
func TestReadme(t *testing.T) {
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(b)

	links := regexp.MustCompile(`\]\(([^)#]+)(#[^)]*)?\)`).FindAllStringSubmatch(readme, -1)
	if len(links) < 3 {
		t.Errorf("found %d links", len(links))
	}
	for _, l := range links {
		if strings.Contains(l[1], "://") {
			continue
		}
		if _, err := os.Stat(l[1]); err != nil {
			t.Errorf("broken link %s: %v", l[1], err)
		}
	}

	// The defaults table, row by row.
	_, table, ok := strings.Cut(readme, "## Defaults")
	if !ok {
		t.Fatal("no Defaults section")
	}
	table, _, _ = strings.Cut(table, "\n## ")
	rows := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		cells := strings.Split(strings.Trim(line, "| "), "|")
		if len(cells) == 2 && !strings.HasPrefix(cells[0], "---") && strings.TrimSpace(cells[0]) != "Setting" {
			rows[strings.TrimSpace(cells[0])] = strings.Trim(strings.TrimSpace(cells[1]), "`")
		}
	}
	s := store.DefaultSettings()
	p := store.NewProfile()
	n := store.DefaultNaming()
	r := store.DefaultRules()
	onOff := func(b bool) string {
		if b {
			return "On"
		}
		return "Off"
	}
	want := map[string]string{
		"Monitoring":                          onOff(s.Monitoring),
		"Capture mode":                        map[store.CaptureMode]string{store.CaptureAuto: "Automatic", store.CaptureManual: "Manual"}[s.CaptureMode],
		"Hotkey for \"Upload clipboard now\"": s.Hotkey,
		"Skip confidential content":           onOff(s.ConfidentialProtection),
		"Rules": fmt.Sprintf(`"%s", then "%s" (%d MB or more)`, r[0].Name, r[1].Name,
			*r[1].Conditions.MinSize/store.MB),
		"When no rule matches":          map[store.Action]string{store.ActionUpload: "Upload", store.ActionSkip: "Skip"}[s.DefaultAction],
		"Image file names":              n[0].Template,
		"File names":                    n[1].Template,
		"Text file names":               n[2].Template,
		"Port":                          fmt.Sprint(p.Port),
		"Proxy":                         map[store.ProxyType]string{store.ProxyNone: "No proxy"}[p.Proxy.Type],
		"Proxy port":                    fmt.Sprint(p.Proxy.Port),
		"Remote folder":                 p.RemoteDir,
		"Create missing folders":        onOff(p.CreateDirs),
		"If the file exists":            map[store.ConflictPolicy]string{store.ConflictRename: "Rename"}[p.ConflictPolicy],
		"Copy remote path after upload": onOff(p.CopyBack),
		"Clipboard text":                p.CopyTemplate,
		"Shell-quote paths":             onOff(p.ShellQuote),
		"Notifications":                 map[store.NotifyPolicy]string{store.NotifyErrors: "Errors only", store.NotifyAll: "All", store.NotifyOff: "Off"}[s.Notifications],
		"Appearance":                    map[store.Appearance]string{store.AppearanceSystem: "System"}[s.Appearance],
		"Close to tray":                 onOff(s.CloseToTray),
		"Start at login":                onOff(s.StartAtLogin),
		"History kept":                  fmt.Sprintf("%d entries, %d days", s.RetentionCount, s.RetentionDays),
	}
	for k, v := range want {
		if got, ok := rows[k]; !ok || got != v {
			t.Errorf("README default %q is %q, the app ships %q", k, got, v)
		}
	}
	if len(rows) != len(want) {
		t.Errorf("README lists %d defaults, the test checks %d", len(rows), len(want))
	}

	// Defaults stated in the prose, which wraps.
	prose := strings.Join(strings.Fields(readme), " ")
	for _, claim := range []string{"Ctrl+Alt+U", "~/clipboard", "port defaults to 22", "100 MB or more",
		"at most 1000 entries for 30 days", "over 64 MB"} {
		if !strings.Contains(prose, claim) {
			t.Errorf("README no longer says %q", claim)
		}
	}
	if s.Hotkey != "Ctrl+Alt+U" || p.RemoteDir != "~/clipboard" || p.Port != 22 || s.RetentionCount != 1000 ||
		s.RetentionDays != 30 || *r[1].Conditions.MinSize != 100*store.MB {
		t.Error("a default the README's prose states changed")
	}
}
