package view

import (
	"github.com/egoist/mygo"

	"github.com/josexy/sshclipboard/internal/store"
)

// ThemeSource maps the Appearance setting to MyGo's theme source.
func ThemeSource(a store.Appearance) mygo.ThemeSource {
	switch a {
	case store.AppearanceLight:
		return mygo.ThemeLight
	case store.AppearanceDark:
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

// ApplyAppearance forces light or dark, or follows the system. Called
// before App.Run with the saved setting, the app starts in it; called on
// change, every window redraws in it, and the views pick their palette
// from c.Theme().Dark.
func ApplyAppearance(a store.Appearance) {
	mygo.Theme.SetSource(ThemeSource(a))
}
