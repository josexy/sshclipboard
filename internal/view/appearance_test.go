package view

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/store"
)

func TestThemeSource(t *testing.T) {
	for a, want := range map[store.Appearance]mygo.ThemeSource{
		store.AppearanceSystem: mygo.ThemeSystem, store.AppearanceLight: mygo.ThemeLight,
		store.AppearanceDark: mygo.ThemeDark, "": mygo.ThemeSystem,
	} {
		if got := ThemeSource(a); got != want {
			t.Errorf("%q → %q", a, got)
		}
	}
}

func rgba(c ui.Color) color.RGBA { return color.RGBA{c.R, c.G, c.B, 255} }

// Toggling dark changes the rendered background to the dark palette's.
func TestDarkChangesBackground(t *testing.T) {
	view := func(c *ui.Context) {
		ApplyTheme(c)
		ui.Column(c).Fill().Children(func() { ui.Text(c, "x") })
	}
	tt := ui.NewTester(view, 200, 120)
	if got := tt.Image().RGBAAt(150, 100); got != rgba(LightPalette().Theme.Background) {
		t.Errorf("light background %v", got)
	}
	tt.SetDark(true)
	tt.Frame()
	if got := tt.Image().RGBAAt(150, 100); got != rgba(DarkPalette().Theme.Background) {
		t.Errorf("dark background %v, want %v", got, rgba(DarkPalette().Theme.Background))
	}
	tt.SetDark(false)
	tt.Frame()
	if got := tt.Image().RGBAAt(150, 100); got != rgba(LightPalette().Theme.Background) {
		t.Errorf("back to light %v", got)
	}
}
