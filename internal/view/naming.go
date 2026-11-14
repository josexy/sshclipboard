package view

import (
	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/naming"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/locales"
)

// namingEdit is one kind's template being edited.
type namingEdit struct {
	loaded bool
	n      store.Naming
	saved  bool
	err    string
}

type namingVM struct {
	edits      [3]namingEdit
	tokensOpen bool
}

func (a *App) namingPage(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.naming
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		Heading(c, locales.NamingTitle(), 24).Grow(1)
		anchor := button(c, iconBraces, locales.Tokens())
		if anchor.Clicked() {
			vm.tokensOpen = !vm.tokensOpen
		}
		ui.Popover(c, anchor, &vm.tokensOpen, func() {
			ui.Text(c, locales.TokensHelp()).Wrap().MaxWidth(420)
		})
	})
	ui.Text(c, locales.NamingIntro()).TextColor(t.TextMuted)

	profile, host := "", ""
	if s.Active != nil {
		profile, host = s.Active.Name, s.Active.Host
	}
	for i, kind := range store.Kinds {
		ed := &vm.edits[i]
		if !ed.loaded {
			ed.n, ed.loaded = s.NamingOf(kind), true
		}
		title := kindLabel(kind)
		ui.Column(c).Gap(8).Padding(14).Radius(t.Radius).Background(t.Surface).Role(ui.RoleGroup).Label(title).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				icon(c, kindIcon(kind)).TextColor(t.Accent)
				ui.Text(c, title).Bold()
			})
			// Validate and preview as the user types.
			tm, perr := naming.Parse(ed.n.Template)
			errText := ed.err
			if perr != nil {
				errText = ErrorText(perr)
			}
			ui.Field(c, locales.FieldTemplate(), func() {
				if ui.TextInput(c, &ed.n.Template).Width(360).Label(title + " " + locales.FieldTemplate()).Changed() {
					ed.saved, ed.err = false, ""
				}
			}).Error(errText)
			if perr == nil {
				name := naming.RenderName(tm, naming.SampleVars(string(kind), profile, host, a.plat.Now()),
					naming.Transforms{Lowercase: ed.n.Lowercase, SpacesToUnderscore: ed.n.SpacesToUnderscore})
				ui.Text(c, locales.Preview(name)).TextColor(t.TextMuted).Role(ui.RoleStatus)
			}
			ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
				if ui.Checkbox(c, &ed.n.Lowercase, locales.Lowercase()).Label(locales.Lowercase() + ": " + title).Changed() {
					ed.saved = false
				}
				if ui.Checkbox(c, &ed.n.SpacesToUnderscore, locales.SpacesToUnderscore()).Label(locales.SpacesToUnderscore() + ": " + title).Changed() {
					ed.saved = false
				}
				ui.Spacer(c)
				if ed.saved {
					iconText(c, iconCheck, locales.Saved(), t.Success)
				}
				if button(c, iconSave, locales.Save()).Label(locales.Save() + ": " + title).Disabled(perr != nil).Clicked() {
					n := ed.n
					a.ctrl.SaveNaming(n, func(err error) {
						if err != nil {
							var fe core.FieldErrors
							if errorsAs(err, &fe) {
								ed.err = ErrorText(fe["template"])
							} else {
								ed.err = locales.SomethingWrong(err.Error())
							}
							return
						}
						ed.saved = true
					})
				}
			})
		})
	}
}
