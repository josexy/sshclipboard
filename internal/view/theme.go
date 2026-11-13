package view

import (
	"math"

	"github.com/egoist/mygo/ui"
)

// Palette is the app's warm-neutral palette for one appearance: a theme
// for the widgets, and the colors the theme has no field for.
type Palette struct {
	Theme   *ui.Theme
	Sidebar ui.Color
}

// HeadingFont is the serif face of headings; Georgia ships with Windows.
const HeadingFont = "Georgia, serif"

// LightPalette returns the light palette: a warm off-white background and
// a terracotta accent.
func LightPalette() Palette {
	t := ui.LightTheme()
	t.Background = ui.Hex("#FAF9F5")
	t.Surface = ui.Hex("#F0EEE6")
	t.SurfaceHover = ui.Hex("#E8E5DA")
	t.SurfacePressed = ui.Hex("#DEDACD")
	t.Border = ui.Hex("#E1DED3")
	t.Text = ui.Hex("#1F1E1D")
	t.TextMuted = ui.Hex("#5F5E58")
	t.Accent = ui.Hex("#C96442")
	t.AccentHover = ui.Hex("#B5583A")
	t.AccentPressed = ui.Hex("#A04D33")
	t.AccentText = ui.Hex("#FFFFFF")
	t.Danger = ui.Hex("#B83A2E")
	t.Warning = ui.Hex("#B7791F")
	t.Success = ui.Hex("#3F7D4E")
	t.Selection = ui.RGBA(201, 100, 66, 0.25)
	t.Focus = ui.RGBA(201, 100, 66, 0.6)
	t.Inverse = ui.Hex("#30302E")
	t.InverseText = ui.Hex("#F5F4EE")
	finish(t)
	return Palette{Theme: t, Sidebar: ui.Hex("#F3F1EA")}
}

// DarkPalette returns the dark palette: a warm charcoal background and the
// same terracotta family.
func DarkPalette() Palette {
	t := ui.DarkTheme()
	t.Background = ui.Hex("#262624")
	t.Surface = ui.Hex("#30302E")
	t.SurfaceHover = ui.Hex("#3A3A37")
	t.SurfacePressed = ui.Hex("#44443F")
	t.Border = ui.Hex("#3E3E3A")
	t.Text = ui.Hex("#F5F4EE")
	t.TextMuted = ui.Hex("#A8A69C")
	t.Accent = ui.Hex("#D97757")
	t.AccentHover = ui.Hex("#E08A6D")
	t.AccentPressed = ui.Hex("#C96442")
	t.AccentText = ui.Hex("#FFFFFF")
	t.Danger = ui.Hex("#E5675A")
	t.Warning = ui.Hex("#D9A441")
	t.Success = ui.Hex("#6FB383")
	t.Selection = ui.RGBA(217, 119, 87, 0.35)
	t.Focus = ui.RGBA(224, 138, 109, 0.65)
	t.Inverse = ui.Hex("#F0EEE6")
	t.InverseText = ui.Hex("#1F1E1D")
	finish(t)
	return Palette{Theme: t, Sidebar: ui.Hex("#1F1E1D")}
}

func finish(t *ui.Theme) {
	t.Radius = 8
	t.Spacing = 4
	t.InactiveAccent = t.Accent // keep the terracotta in inactive windows
}

// PaletteFor returns the palette of an appearance.
func PaletteFor(dark bool) Palette {
	if dark {
		return DarkPalette()
	}
	return LightPalette()
}

// ApplyTheme themes the window for this frame: the palette of the
// appearance the window shows (c.Theme().Dark, which follows the
// Appearance setting through mygo.Theme.SetSource), with the desktop's
// contrast and text size, and the app's accent. It returns the palette.
func ApplyTheme(c *ui.Context) Palette {
	pal := PaletteFor(c.Theme().Dark)
	t := pal.Theme
	accent := *t
	t = t.WithPreferences(c.Preferences())
	// WithPreferences takes the desktop's accent; keep the app's.
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = accent.Accent, accent.AccentHover, accent.AccentPressed, accent.AccentText
	t.Selection, t.Focus, t.InactiveAccent = accent.Selection, accent.Focus, accent.Accent
	c.SetTheme(t)
	pal.Theme = t
	return pal
}

// Heading draws a heading in the serif face.
func Heading(c *ui.Context, text string, size float32) ui.Element {
	return ui.Text(c, text).Font(HeadingFont).FontSize(size).FontWeight(600).Role(ui.RoleHeading)
}

// Luminance returns a color's WCAG relative luminance.
func Luminance(col ui.Color) float64 {
	ch := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(col.R) + 0.7152*ch(col.G) + 0.0722*ch(col.B)
}

// Contrast returns the WCAG contrast ratio of two opaque colors.
func Contrast(a, b ui.Color) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
