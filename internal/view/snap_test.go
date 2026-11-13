package view

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestSnapshots writes the pages as PNGs when SNAPSHOT_DIR is set, to look
// at the layout.
func TestSnapshots(t *testing.T) {
	dir := os.Getenv("SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set SNAPSHOT_DIR to write snapshots")
	}
	for _, dark := range []bool{false, true} {
		f := newFake()
		_, tt := newTester(t, f)
		if dark {
			tt.SetDark(true)
		}
		for _, page := range []string{"Activity", "Servers", "Rules", "Naming", "Settings"} {
			click(t, tt, page)
			if page == "Servers" {
				click(t, tt, "prod-box")
			}
			if page == "Activity" {
				click(t, tt, "image.png")
			}
			name := page
			if dark {
				name += "-dark"
			}
			out, _ := os.Create(filepath.Join(dir, name+".png"))
			png.Encode(out, tt.Image())
			out.Close()
			if page == "Rules" && !dark {
				click(t, tt, "Edit: Skip large files")
				out, _ := os.Create(filepath.Join(dir, "RuleDialog.png"))
				png.Encode(out, tt.Image())
				out.Close()
				click(t, tt, "Cancel")
			}
		}
	}
}
