package view

import (
	"errors"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/locales"
)

type serversVM struct {
	editing     bool  // the form is open
	id          int64 // the profile edited, 0 for a new one
	p           store.Profile
	port        string
	fileMode    string
	auth        string // label of the auth method
	proxy       string // label of the proxy type
	proxyPort   string
	conflict    string
	separator   string
	password    string
	passphrase  string
	proxyPass   string
	errs        core.FieldErrors
	formErr     string
	saving      bool
	saved       bool
	sessionOnly bool
	testing     bool
	test        *sshconn.CheckResult
	deleteOpen  bool
}

func authLabels() []string {
	return []string{locales.AuthPassword(), locales.AuthKey(), locales.AuthAgent()}
}

func authOf(label string) store.AuthMethod {
	switch label {
	case locales.AuthKey():
		return store.AuthKey
	case locales.AuthAgent():
		return store.AuthAgent
	}
	return store.AuthPassword
}

func authLabel(m store.AuthMethod) string {
	switch m {
	case store.AuthKey:
		return locales.AuthKey()
	case store.AuthAgent:
		return locales.AuthAgent()
	}
	return locales.AuthPassword()
}

func proxyLabels() []string {
	return []string{locales.ProxyNone(), locales.ProxySOCKS5()}
}

func proxyOf(label string) store.ProxyType {
	if label == locales.ProxySOCKS5() {
		return store.ProxySOCKS5
	}
	return store.ProxyNone
}

func proxyLabel(t store.ProxyType) string {
	if t == store.ProxySOCKS5 {
		return locales.ProxySOCKS5()
	}
	return locales.ProxyNone()
}

func conflictLabels() []string {
	return []string{locales.ConflictRename(), locales.ConflictOverwrite(), locales.ConflictSkip()}
}

func conflictOf(label string) store.ConflictPolicy {
	switch label {
	case locales.ConflictOverwrite():
		return store.ConflictOverwrite
	case locales.ConflictSkip():
		return store.ConflictSkip
	}
	return store.ConflictRename
}

func conflictLabel(p store.ConflictPolicy) string {
	switch p {
	case store.ConflictOverwrite:
		return locales.ConflictOverwrite()
	case store.ConflictSkip:
		return locales.ConflictSkip()
	}
	return locales.ConflictRename()
}

// open loads a profile into the form; secrets never prefill.
func (vm *serversVM) open(p store.Profile) {
	*vm = serversVM{editing: true, id: p.ID, p: p, port: strconv.Itoa(p.Port), auth: authLabel(p.AuthMethod),
		proxy: proxyLabel(p.Proxy.Type), proxyPort: strconv.Itoa(p.Proxy.Port),
		conflict: conflictLabel(p.ConflictPolicy), separator: locales.SeparatorSpace()}
	if p.FileMode >= 0 {
		vm.fileMode = "0" + strconv.FormatInt(int64(p.FileMode), 8)
	}
	if p.CopySeparator == "\n" {
		vm.separator = locales.SeparatorNewline()
	}
}

// input builds what the form saves, with its own parse errors.
func (vm *serversVM) input() (core.ProfileInput, core.FieldErrors) {
	p := vm.p
	p.ID = vm.id
	fe := core.FieldErrors{}
	port, err := strconv.Atoi(strings.TrimSpace(vm.port))
	if err != nil {
		fe[core.FieldPort] = core.ErrPortRange
	}
	p.Port = port
	p.FileMode = -1
	if m := strings.TrimSpace(vm.fileMode); m != "" {
		v, err := strconv.ParseInt(m, 8, 32)
		if err != nil || v < 0 || v > 0o7777 {
			fe[core.FieldFileMode] = core.ErrFileMode
		}
		p.FileMode = int(v)
	}
	p.AuthMethod = authOf(vm.auth)
	p.Proxy.Type = proxyOf(vm.proxy)
	// A disabled proxy keeps its last saved port.
	if p.Proxy.Enabled() {
		port, err := strconv.Atoi(strings.TrimSpace(vm.proxyPort))
		if err != nil {
			fe[core.FieldProxyPort] = core.ErrPortRange
		}
		p.Proxy.Port = port
	}
	p.ConflictPolicy = conflictOf(vm.conflict)
	p.CopySeparator = " "
	if vm.separator == locales.SeparatorNewline() {
		p.CopySeparator = "\n"
	}
	in := core.ProfileInput{Profile: p}
	if vm.password != "" {
		in.Password = core.SecretInput{Value: vm.password, Set: true}
	}
	if vm.passphrase != "" {
		in.Passphrase = core.SecretInput{Value: vm.passphrase, Set: true}
	}
	if vm.proxyPass != "" {
		in.ProxyPassword = core.SecretInput{Value: vm.proxyPass, Set: true}
	}
	if len(fe) == 0 {
		fe = nil
	}
	return in, fe
}

func (a *App) serversPage(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.servers
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		Heading(c, locales.ServersTitle(), 24).Grow(1)
		if ui.PrimaryButton(c, locales.ServersAdd()).FontWeight(600).Clicked() {
			vm.open(store.NewProfile())
		}
	})
	if len(s.Profiles) == 0 && !vm.editing {
		ui.Text(c, locales.ServersEmpty()).TextColor(t.TextMuted)
		return
	}
	ui.Row(c).Gap(24).AlignItems(ui.Start).Children(func() {
		// The list.
		ui.Column(c).Width(200).Gap(4).Role(ui.RoleList).Label(locales.ServersTitle()).Children(func() {
			for _, p := range s.Profiles {
				p := p
				label := p.Name
				b := ui.Button(c, label).Key(p.ID).FillWidth().Selected(vm.editing && vm.id == p.ID)
				if s.Active != nil && s.Active.ID == p.ID {
					b.Description(locales.ServerActive()).Children(func() {
						ui.Badge(c, locales.ServerActive())
					})
				}
				if b.Clicked() {
					vm.open(p)
				}
			}
		})
		if vm.editing {
			ui.Column(c).Grow(1).MaxWidth(620).Gap(12).Children(func() { a.serverForm(c, t, s) })
		}
	})
}

func (a *App) serverForm(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.servers
	title := vm.p.Name
	if vm.id == 0 {
		title = locales.ServerNew()
	}
	errOf := func(field string) string { return ErrorText(vm.errs[field]) }
	saved := s.Saved[vm.id]
	// The actions come first, where they show without scrolling.
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		Heading(c, title, 18).Grow(1)
	})
	if vm.formErr != "" {
		ui.Text(c, vm.formErr).TextColor(t.Danger).Role(ui.RoleAlert)
	}
	if vm.sessionOnly {
		ui.Text(c, locales.SecretSessionOnly()).TextColor(t.Warning).Wrap()
	}

	ui.Row(c).Gap(8).Wrap().Children(func() {
		if ui.PrimaryButton(c, locales.Save()).FontWeight(600).Disabled(vm.saving).Clicked() {
			a.saveServer()
		}
		label := locales.TestConnection()
		if vm.testing {
			label = locales.Testing()
		}
		if ui.Button(c, label).Disabled(vm.testing).Clicked() {
			in, fe := vm.input()
			if fe != nil {
				vm.errs = fe
			} else {
				vm.testing, vm.test = true, nil
				a.ctrl.TestConnection(in, func(r sshconn.CheckResult) {
					vm.testing, vm.test = false, &r
				})
			}
		}
		if vm.id != 0 {
			if s.Active == nil || s.Active.ID != vm.id {
				if ui.Button(c, locales.MakeActive()).Clicked() {
					a.ctrl.SetActiveProfile(vm.id, nil)
				}
			}
			if ui.Button(c, locales.Duplicate()).Clicked() {
				a.ctrl.DuplicateProfile(vm.id, nil)
			}
			if ui.Button(c, locales.Delete()).Clicked() {
				vm.deleteOpen = true
			}
		}
		if vm.saved {
			ui.Text(c, locales.Saved()).TextColor(t.Success).Role(ui.RoleStatus)
		}
	})

	if r := vm.test; r != nil {
		ui.Row(c).Padding(10, 14).Radius(t.Radius).Background(t.Surface).Role(ui.RoleStatus).Children(func() {
			if r.OK() {
				ui.Text(c, locales.TestOK(r.Dir)).TextColor(t.Success).Wrap()
			} else {
				ui.Text(c, locales.TestFailed(StepText(r.Step), r.Err.Error())).TextColor(t.Danger).Wrap()
			}
		})
	}

	ui.Form(c, func() {
		ui.Field(c, locales.FieldName(), func() { ui.TextInput(c, &vm.p.Name).Width(320) }).Error(errOf(core.FieldName))
		ui.Field(c, locales.FieldHost(), func() { ui.TextInput(c, &vm.p.Host).Width(320) }).Error(errOf(core.FieldHost))
		ui.Field(c, locales.FieldPort(), func() { ui.TextInput(c, &vm.port).Width(100) }).Error(errOf(core.FieldPort))
		ui.Field(c, locales.FieldUsername(), func() { ui.TextInput(c, &vm.p.Username).Width(320) }).Error(errOf(core.FieldUsername))
		ui.Field(c, locales.FieldAuth(), func() { ui.Select(c, &vm.auth, authLabels()).Width(200) })
		switch authOf(vm.auth) {
		case store.AuthPassword:
			ph := ""
			if saved.Password {
				ph = locales.SecretSaved()
			}
			ui.Field(c, locales.FieldPassword(), func() {
				ui.TextInput(c, &vm.password).Password().Placeholder(ph).Width(320)
			})
		case store.AuthKey:
			ui.Field(c, locales.FieldKeyPath(), func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.TextInput(c, &vm.p.KeyPath).Width(320).Label(locales.FieldKeyPath())
					if ui.Button(c, locales.Browse()).Clicked() && a.plat.PickFile != nil {
						a.plat.PickFile(func(p string) {
							if p != "" {
								vm.p.KeyPath = p
							}
						})
					}
				})
			}).Error(errOf(core.FieldKeyPath))
			ph := ""
			if saved.Passphrase {
				ph = locales.SecretSaved()
			}
			ui.Field(c, locales.FieldPassphrase(), func() {
				ui.TextInput(c, &vm.passphrase).Password().Placeholder(ph).Width(320)
			})
		case store.AuthAgent:
			ui.Text(c, locales.AgentHint()).TextColor(t.TextMuted).Wrap().MaxWidth(420)
		}
		a.proxyFields(c, saved)
		ui.Field(c, locales.FieldRemoteDir(), func() { ui.TextInput(c, &vm.p.RemoteDir).Width(320) }).
			Description(locales.RemoteDirHint()).Error(errOf(core.FieldRemoteDir))
		ui.Field(c, locales.FieldCreateDirs(), func() { ui.Switch(c, &vm.p.CreateDirs) })
		ui.Field(c, locales.FieldConflict(), func() { ui.Select(c, &vm.conflict, conflictLabels()).Width(200) })
		ui.Field(c, locales.FieldFileMode(), func() { ui.TextInput(c, &vm.fileMode).Placeholder("0644").Width(100) }).
			Description(locales.FileModeHint()).Error(errOf(core.FieldFileMode))
		ui.Field(c, locales.FieldCopyBack(), func() { ui.Switch(c, &vm.p.CopyBack) }).Description(locales.CopyBackHint())
		ui.Field(c, locales.FieldCopyTemplate(), func() { ui.TextInput(c, &vm.p.CopyTemplate).Width(320) }).
			Description(locales.CopyTemplateHint()).Error(errOf(core.FieldTemplate))
		ui.Field(c, locales.FieldSeparator(), func() {
			ui.Select(c, &vm.separator, []string{locales.SeparatorSpace(), locales.SeparatorNewline()}).Width(200)
		})
		ui.Field(c, locales.FieldShellQuote(), func() { ui.Switch(c, &vm.p.ShellQuote) })
	})

	switch ui.AlertDialog(c, &vm.deleteOpen, locales.DeleteServerTitle(vm.p.Name), locales.DeleteServerMessage(),
		locales.Cancel(), locales.Delete()) {
	case 1:
		id := vm.id
		a.ctrl.DeleteProfile(id, func(err error) {
			if err == nil {
				*vm = serversVM{}
			}
		})
	}
}

// proxyFields are the form's proxy settings: the type, and for SOCKS5 its
// address and optional sign-in.
func (a *App) proxyFields(c *ui.Context, saved core.SavedSecrets) {
	vm := &a.servers
	errOf := func(field string) string { return ErrorText(vm.errs[field]) }
	ui.Field(c, locales.FieldProxy(), func() { ui.Select(c, &vm.proxy, proxyLabels()).Width(200) }).
		Description(locales.ProxyHint())
	if proxyOf(vm.proxy) != store.ProxySOCKS5 {
		return
	}
	ui.Field(c, locales.FieldProxyHost(), func() { ui.TextInput(c, &vm.p.Proxy.Host).Width(320) }).
		Error(errOf(core.FieldProxyHost))
	ui.Field(c, locales.FieldProxyPort(), func() { ui.TextInput(c, &vm.proxyPort).Width(100) }).
		Error(errOf(core.FieldProxyPort))
	ui.Field(c, locales.FieldProxyUsername(), func() { ui.TextInput(c, &vm.p.Proxy.Username).Width(320) }).
		Description(locales.ProxyUsernameHint())
	ph := ""
	if saved.ProxyPassword {
		ph = locales.SecretSaved()
	}
	ui.Field(c, locales.FieldProxyPassword(), func() {
		ui.TextInput(c, &vm.proxyPass).Password().Placeholder(ph).Width(320)
	})
}

func (a *App) saveServer() {
	vm := &a.servers
	in, fe := vm.input()
	if fe != nil {
		vm.errs = fe
		return
	}
	vm.saving, vm.saved, vm.formErr = true, false, ""
	a.ctrl.SaveProfile(in, func(res core.SaveResult, err error) {
		vm.saving = false
		var fe core.FieldErrors
		switch {
		case errors.As(err, &fe):
			vm.errs = fe
		case err != nil:
			vm.formErr = locales.SomethingWrong(err.Error())
		default:
			sessionOnly := res.SessionOnly
			vm.open(res.Profile)
			vm.saved, vm.sessionOnly = true, sessionOnly
		}
	})
}
