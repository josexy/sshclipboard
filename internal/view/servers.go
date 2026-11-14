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
		if primaryButton(c, iconPlus, locales.ServersAdd()).FontWeight(600).Clicked() {
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
				active := s.Active != nil && s.Active.ID == p.ID
				b := ui.Button(c, "").Key(p.ID).FillWidth().Selected(vm.editing && vm.id == p.ID).Label(p.Name)
				if active {
					b.Description(locales.ServerActive())
				}
				b.Children(func() {
					icon(c, iconServer)
					ui.Text(c, p.Name).SingleLine().Grow(1)
					if active {
						ui.Badge(c, locales.ServerActive())
					}
				})
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
		if primaryButton(c, iconSave, locales.Save()).FontWeight(600).Disabled(vm.saving).Clicked() {
			a.saveServer()
		}
		label := locales.TestConnection()
		if vm.testing {
			label = locales.Testing()
		}
		if button(c, iconPlugZap, label).Disabled(vm.testing).Clicked() {
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
				if button(c, iconCircleDot, locales.MakeActive()).Clicked() {
					a.ctrl.SetActiveProfile(vm.id, nil)
				}
			}
			// Secondary actions show only their icons, to keep the row on one line.
			if iconButton(c, iconCopy, locales.Duplicate(), locales.Duplicate()).Clicked() {
				a.ctrl.DuplicateProfile(vm.id, nil)
			}
			if iconButton(c, iconTrash, locales.Delete(), locales.Delete()).Clicked() {
				vm.deleteOpen = true
			}
		}
		if vm.saved {
			iconText(c, iconCheck, locales.Saved(), t.Success).AlignSelf(ui.Center).Role(ui.RoleStatus)
		}
	})

	if r := vm.test; r != nil {
		ui.Row(c).Padding(10, 14).Radius(t.Radius).Background(t.Surface).Role(ui.RoleStatus).Children(func() {
			if r.OK() {
				iconText(c, iconCircleCheck, locales.TestOK(r.Dir), t.Success)
			} else {
				iconText(c, iconCircleX, locales.TestFailed(StepText(r.Step), r.Err.Error()), t.Danger)
			}
		})
	}

	ui.Column(c).Gap(t.Space(3)).Children(func() {
		serverField(c, locales.FieldName(), func() { ui.TextInput(c, &vm.p.Name).MaxWidth(320) }).Error(errOf(core.FieldName))
		serverField(c, locales.FieldHost(), func() { ui.TextInput(c, &vm.p.Host).MaxWidth(320) }).Error(errOf(core.FieldHost))
		serverField(c, locales.FieldPort(), func() { ui.TextInput(c, &vm.port).MaxWidth(100) }).Error(errOf(core.FieldPort))
		serverField(c, locales.FieldUsername(), func() { ui.TextInput(c, &vm.p.Username).MaxWidth(320) }).Error(errOf(core.FieldUsername))
		serverField(c, locales.FieldAuth(), func() { ui.Select(c, &vm.auth, authLabels()).MaxWidth(200) })
		switch authOf(vm.auth) {
		case store.AuthPassword:
			ph := ""
			if saved.Password {
				ph = locales.SecretSaved()
			}
			serverField(c, locales.FieldPassword(), func() {
				ui.TextInput(c, &vm.password).Password().Placeholder(ph).MaxWidth(320)
			})
		case store.AuthKey:
			serverField(c, locales.FieldKeyPath(), func() {
				ui.Row(c).Gap(8).Children(func() {
					ui.TextInput(c, &vm.p.KeyPath).Grow(1).MaxWidth(320).Label(locales.FieldKeyPath())
					if button(c, iconFolderOpen, locales.Browse()).Shrink(0).Clicked() && a.plat.PickFile != nil {
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
			serverField(c, locales.FieldPassphrase(), func() {
				ui.TextInput(c, &vm.passphrase).Password().Placeholder(ph).MaxWidth(320)
			})
		case store.AuthAgent:
			ui.Text(c, locales.AgentHint()).TextColor(t.TextMuted).Wrap().MaxWidth(420)
		}
		a.proxyFields(c, saved)
		serverField(c, locales.FieldRemoteDir(), func() { ui.TextInput(c, &vm.p.RemoteDir).MaxWidth(320) }).
			Description(locales.RemoteDirHint()).Error(errOf(core.FieldRemoteDir))
		serverField(c, locales.FieldCreateDirs(), func() { ui.Switch(c, &vm.p.CreateDirs) }).AlignItems(ui.Start)
		serverField(c, locales.FieldConflict(), func() { ui.Select(c, &vm.conflict, conflictLabels()).MaxWidth(200) })
		serverField(c, locales.FieldFileMode(), func() { ui.TextInput(c, &vm.fileMode).Placeholder("0644").MaxWidth(100) }).
			Description(locales.FileModeHint()).Error(errOf(core.FieldFileMode))
		serverField(c, locales.FieldCopyBack(), func() { ui.Switch(c, &vm.p.CopyBack) }).
			AlignItems(ui.Start).Description(locales.CopyBackHint())
		serverField(c, locales.FieldCopyTemplate(), func() { ui.TextInput(c, &vm.p.CopyTemplate).MaxWidth(320) }).
			Description(locales.CopyTemplateHint()).Error(errOf(core.FieldTemplate))
		serverField(c, locales.FieldSeparator(), func() {
			ui.Select(c, &vm.separator, []string{locales.SeparatorSpace(), locales.SeparatorNewline()}).MaxWidth(200)
		})
		serverField(c, locales.FieldShellQuote(), func() { ui.Switch(c, &vm.p.ShellQuote) }).AlignItems(ui.Start)
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

func serverField(c *ui.Context, label string, control func()) ui.Element {
	return leftAlignedField(c, 160, label, control)
}

// proxyFields are the form's proxy settings: the type, and for SOCKS5 its
// address and optional sign-in.
func (a *App) proxyFields(c *ui.Context, saved core.SavedSecrets) {
	vm := &a.servers
	errOf := func(field string) string { return ErrorText(vm.errs[field]) }
	serverField(c, locales.FieldProxy(), func() { ui.Select(c, &vm.proxy, proxyLabels()).MaxWidth(200) }).
		Description(locales.ProxyHint())
	if proxyOf(vm.proxy) != store.ProxySOCKS5 {
		return
	}
	serverField(c, locales.FieldProxyHost(), func() { ui.TextInput(c, &vm.p.Proxy.Host).MaxWidth(320) }).
		Error(errOf(core.FieldProxyHost))
	serverField(c, locales.FieldProxyPort(), func() { ui.TextInput(c, &vm.proxyPort).MaxWidth(100) }).
		Error(errOf(core.FieldProxyPort))
	serverField(c, locales.FieldProxyUsername(), func() { ui.TextInput(c, &vm.p.Proxy.Username).MaxWidth(320) }).
		Description(locales.ProxyUsernameHint())
	ph := ""
	if saved.ProxyPassword {
		ph = locales.SecretSaved()
	}
	serverField(c, locales.FieldProxyPassword(), func() {
		ui.TextInput(c, &vm.proxyPass).Password().Placeholder(ph).MaxWidth(320)
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
