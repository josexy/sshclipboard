package view

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// Spec "Contrast check": text and muted text on the background and the
// sidebar reach 4.5:1 in both themes.
func TestPaletteContrast(t *testing.T) {
	for _, pal := range []struct {
		name string
		p    Palette
	}{{"light", LightPalette()}, {"dark", DarkPalette()}} {
		th := pal.p.Theme
		for _, c := range []struct {
			what   string
			fg, bg ui.Color
		}{
			{"text on background", th.Text, th.Background},
			{"muted on background", th.TextMuted, th.Background},
			{"text on sidebar", th.Text, pal.p.Sidebar},
			{"muted on sidebar", th.TextMuted, pal.p.Sidebar},
			{"text on surface", th.Text, th.Surface},
			{"muted on surface", th.TextMuted, th.Surface},
		} {
			if r := Contrast(c.fg, c.bg); r < 4.5 {
				t.Errorf("%s %s: %.2f:1", pal.name, c.what, r)
			}
		}
		// White on the accent is for semibold labels: WCAG's 3:1 for
		// large or bold text.
		if r := Contrast(th.AccentText, th.Accent); r < 3 {
			t.Errorf("%s accent label: %.2f:1", pal.name, r)
		}
	}
}

func TestPaletteMetricsAndAccent(t *testing.T) {
	l, d := LightPalette().Theme, DarkPalette().Theme
	if l.Radius != 8 || l.Spacing != 4 || l.InactiveAccent != l.Accent || d.InactiveAccent != d.Accent {
		t.Errorf("metrics %+v", l)
	}
	if l.Accent != ui.Hex("#C96442") || d.Accent != ui.Hex("#D97757") || l.Dark || !d.Dark {
		t.Error("accent")
	}
	if l.Inverse == (ui.Color{}) || d.InverseText == (ui.Color{}) {
		t.Error("inverse colors keep toasts warm")
	}
	if r := Contrast(ui.Hex("#000000"), ui.Hex("#FFFFFF")); r < 20.9 || r > 21.1 {
		t.Errorf("contrast formula: %.2f", r)
	}
}
