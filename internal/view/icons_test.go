package view

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Every embedded icon is declared in icons.go, is on Lucide's 24-unit
// grid, and draws: parsing alone can't tell shapes the renderer leaves out.
func TestIconsDeclaredAndDrawn(t *testing.T) {
	src, err := os.ReadFile("icons.go")
	if err != nil {
		t.Fatal(err)
	}
	paths, err := fs.Glob(iconFiles, "icons/*.svg")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no embedded icons: %v", err)
	}
	pal := LightPalette()
	for _, path := range paths {
		name := strings.TrimSuffix(strings.TrimPrefix(path, "icons/"), ".svg")
		if !strings.Contains(string(src), `lucide("`+name+`")`) {
			t.Errorf("%s is embedded but not declared in icons.go", path)
			continue
		}
		svg := lucide(name)
		if w, h := svg.Size(); w != 24 || h != 24 {
			t.Errorf("%s is %v×%v, want 24×24", name, w, h)
		}
		tt := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Background(pal.Theme.Background).Children(func() {
				ui.Icon(c, svg).Size(24, 24).TextColor(pal.Theme.Text)
			})
		}, 32, 32)
		im, ink := tt.Image(), 0
		for i := 0; i < len(im.Pix); i += 4 {
			if im.Pix[i] < 128 {
				ink++
			}
		}
		if ink == 0 {
			t.Errorf("%s draws nothing", name)
		}
	}
}

func TestEntryMenuIcons(t *testing.T) {
	ic := entryMenuIcons()
	for i, m := range []ui.MenuIcon{ic.copy, ic.retry, ic.upload, ic.cancel, ic.open, ic.folder, ic.delete} {
		if m == nil {
			t.Errorf("menu icon %d was not made", i)
		}
	}
}
