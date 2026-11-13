package view

import (
	"errors"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/locales"
)

type settingsVM struct {
	hotkey      string
	hotkeyDirty bool
	errs        map[string]string
	retCount    string
	retDays     string
	retLoaded   bool
	mode        string
	notify      string
	appearance  int
}

func errorsAs(err error, target any) bool { return errors.As(err, target) }

func (a *App) settingsPage(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.settings
	if vm.errs == nil {
		vm.errs = map[string]string{}
	}
	st := s.Settings
	set := func(key string, value any) {
		delete(vm.errs, key)
		a.ctrl.SetSetting(key, value, func(err error) {
			if err != nil {
				vm.errs[key] = ErrorText(err)
			}
		})
	}
	if !vm.hotkeyDirty {
		vm.hotkey = st.Hotkey
	}
	if !vm.retLoaded {
		vm.retCount, vm.retDays, vm.retLoaded = strconv.Itoa(st.RetentionCount), strconv.Itoa(st.RetentionDays), true
	}

	Heading(c, locales.SettingsTitle(), 24)
	ui.Form(c, func() {
		ui.Fieldset(c, locales.GroupGeneral(), func() {
			sw := func(label, key string, on bool) {
				ui.Field(c, label, func() {
					if ui.Switch(c, &on).Changed() {
						set(key, on)
					}
				}).Error(vm.errs[key])
			}
			sw(locales.StartAtLogin(), store.KeyStartAtLogin, st.StartAtLogin)
			sw(locales.StartHidden(), store.KeyStartHidden, st.StartHidden)
			sw(locales.CloseToTray(), store.KeyCloseToTray, st.CloseToTray)
		})
		ui.Fieldset(c, locales.GroupCapture(), func() {
			modes := []string{locales.ModeAutomatic(), locales.ModeManual()}
			vm.mode = modes[0]
			if st.CaptureMode == store.CaptureManual {
				vm.mode = modes[1]
			}
			ui.Field(c, locales.CaptureMode(), func() {
				if ui.Select(c, &vm.mode, modes).Width(260).Changed() {
					mode := store.CaptureAuto
					if vm.mode == modes[1] {
						mode = store.CaptureManual
					}
					set(store.KeyCaptureMode, mode)
				}
			})
			ui.Field(c, locales.Hotkey(), func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					if ui.TextInput(c, &vm.hotkey).Placeholder(locales.HotkeyNone()).Width(180).Label(locales.Hotkey()).Changed() {
						vm.hotkeyDirty = true
					}
					if ui.Button(c, locales.HotkeySet()).Disabled(!vm.hotkeyDirty).Clicked() {
						hk := vm.hotkey
						delete(vm.errs, store.KeyHotkey)
						a.ctrl.SetSetting(store.KeyHotkey, hk, func(err error) {
							if err != nil {
								vm.errs[store.KeyHotkey] = ErrorText(err)
								return
							}
							vm.hotkeyDirty = false
						})
					}
					if ui.Button(c, locales.HotkeyDisable()).Disabled(st.Hotkey == "").Clicked() {
						vm.hotkey, vm.hotkeyDirty = "", false
						set(store.KeyHotkey, "")
					}
				})
			}).Error(vm.errs[store.KeyHotkey])
			conf := st.ConfidentialProtection
			ui.Field(c, locales.Confidential(), func() {
				if ui.Switch(c, &conf).Changed() {
					set(store.KeyConfidentialProtection, conf)
				}
			}).Description(locales.ConfidentialHint())
		})
		ui.Fieldset(c, locales.GroupAppearance(), func() {
			sources := []store.Appearance{store.AppearanceSystem, store.AppearanceLight, store.AppearanceDark}
			vm.appearance = 0
			for i, src := range sources {
				if st.Appearance == src {
					vm.appearance = i
				}
			}
			ui.Field(c, locales.GroupAppearance(), func() {
				if ui.Segmented(c, &vm.appearance, locales.AppearanceSystem(), locales.AppearanceLight(), locales.AppearanceDark()).
					AlignSelf(ui.Start).Changed() {
					set(store.KeyAppearance, sources[vm.appearance])
				}
			})
		})
		ui.Fieldset(c, locales.GroupNotifications(), func() {
			policies := []store.NotifyPolicy{store.NotifyAll, store.NotifyErrors, store.NotifyOff}
			labels := []string{locales.NotifyAll(), locales.NotifyErrors(), locales.NotifyOff()}
			vm.notify = labels[1]
			for i, p := range policies {
				if st.Notifications == p {
					vm.notify = labels[i]
				}
			}
			ui.Field(c, locales.GroupNotifications(), func() {
				if ui.Select(c, &vm.notify, labels).Width(200).Changed() {
					for i, l := range labels {
						if l == vm.notify {
							set(store.KeyNotifications, policies[i])
						}
					}
				}
			})
		})
		ui.Fieldset(c, locales.GroupStorage(), func() {
			ui.Field(c, locales.StagingFolder(), func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					shown := st.StagingDir
					if shown == "" {
						shown = locales.StagingDefault(a.plat.DefaultStaging)
					}
					ui.Text(c, shown).TruncateMiddle().MaxWidth(320)
					if ui.Button(c, locales.Choose()).Clicked() && a.plat.PickFolder != nil {
						a.plat.PickFolder(func(p string) {
							if p != "" {
								set(store.KeyStagingDir, p)
							}
						})
					}
					if ui.Button(c, locales.UseDefault()).Disabled(st.StagingDir == "").Clicked() {
						set(store.KeyStagingDir, "")
					}
				})
			}).Error(vm.errs[store.KeyStagingDir])
			num := func(label, key string, value *string) {
				ui.Field(c, label, func() {
					in := ui.TextInput(c, value).Width(100)
					if in.Submitted() || (in.FocusLeft()) {
						n, err := strconv.Atoi(*value)
						if err != nil || n < 1 {
							vm.errs[key] = locales.ErrNumber()
							return
						}
						set(key, n)
					}
				}).Error(vm.errs[key])
			}
			num(locales.RetentionCount(), store.KeyRetentionCount, &vm.retCount)
			num(locales.RetentionDays(), store.KeyRetentionDays, &vm.retDays)
		})
		ui.Fieldset(c, locales.GroupAbout(), func() {
			ui.Field(c, locales.AppName(), func() { ui.Text(c, locales.Version(a.plat.Version)) })
		})
	})
}
