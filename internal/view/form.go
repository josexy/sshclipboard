package view

import "github.com/egoist/mygo/ui"

// leftAlignedField lays out a Field in two columns, retaining its control's label, help, and errors.
func leftAlignedField(c *ui.Context, labelWidth float32, label string, control func()) ui.Element {
	return ui.Field(c, label, control).Grid().ColumnTracks(ui.Fixed(labelWidth), ui.Fr(1)).
		GapX(c.Theme().Space(3)).AlignItems(ui.Baseline)
}
