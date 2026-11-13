package view

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/locales"
)

// Controller is what the views read and command. *core.Controller
// implements it; tests use fakes. Its methods return at once and report
// back through their done functions, on the UI thread.
type Controller interface {
	State() *core.State
	SetMonitoring(on bool, done func(error))
	CaptureNow()
	SetActiveProfile(id int64, done func(error))
	SetSetting(key string, value any, done func(error))
	SaveProfile(in core.ProfileInput, done func(core.SaveResult, error))
	DeleteProfile(id int64, done func(error))
	DuplicateProfile(id int64, done func(error))
	TestConnection(in core.ProfileInput, done func(sshconn.CheckResult))
	TrustHostKey(done func(error))
	DismissHostKey()
	DismissSecret()
	ProvideSecret(profileID int64, kind secret.Kind, value string, done func(error))
	SaveRule(r store.Rule, done func(error))
	DeleteRule(id int64, done func(error))
	MoveRule(id int64, delta int, done func(error))
	ReorderRules(ids []int64, done func(error))
	SetRuleEnabled(id int64, on bool, done func(error))
	SetDefaultAction(a store.Action, done func(error))
	SaveNaming(n store.Naming, done func(error))
	DryRun(done func([]core.DryRunResult))
	SetHistoryFilter(f store.Filter)
	ActivityViewed()
	DismissNotice()
	Retry(id int64, done func(error))
	UploadAnyway(id int64, done func(error))
	CopyRemotePath(id int64, done func(error))
	Cancel(id int64)
	DeleteEntry(id int64, done func(error))
	ClearHistory(done func(error))
}

// Platform is what the views ask of the desktop. Every field may be nil.
type Platform struct {
	OpenFile     func(path string)
	ShowInFolder func(path string)
	// PickFile and PickFolder open the system's dialogs and report the
	// chosen path, "" when cancelled.
	PickFile   func(done func(path string))
	PickFolder func(done func(path string))
	Version    string
	// DefaultStaging is the default staging folder, shown in Settings.
	DefaultStaging string
	Now            func() time.Time
}

// Pages of the sidebar.
const (
	PageActivity = "activity"
	PageServers  = "servers"
	PageRules    = "rules"
	PageNaming   = "naming"
	PageSettings = "settings"
)

// App is the main window's view model: what must survive rebuilds.
type App struct {
	ctrl Controller
	plat Platform
	page string

	headerServer string
	activity     activityVM
	servers      serversVM
	rules        rulesVM
	naming       namingVM
	settings     settingsVM
	thumbs       *thumbCache

	hostKeyOpen bool
	quitOpen    bool
	quitCount   int
	quitConfirm func()
	secretOpen  bool
	secretValue string
	clearing    bool // ActivityViewed was sent for the current error
}

// NewApp returns the main window's view over ctrl. redraw runs a function
// on the UI thread and redraws, as Window.Update does; it brings in
// thumbnails decoded off the main thread.
func NewApp(ctrl Controller, plat Platform, redraw func(func())) *App {
	if plat.Now == nil {
		plat.Now = time.Now
	}
	return &App{ctrl: ctrl, plat: plat, page: PageActivity, thumbs: newThumbCache(redraw)}
}

// Page returns the page shown.
func (a *App) Page() string { return a.page }

// Navigate shows a page, as a notification's click opens Activity.
func (a *App) Navigate(page string) { a.page = page }

// AskQuit asks whether to quit while n uploads are in progress; confirm
// runs when the user chooses Quit. Unfinished uploads are marked
// interrupted at the next start.
func (a *App) AskQuit(n int, confirm func()) {
	a.quitOpen, a.quitCount, a.quitConfirm = true, n, confirm
}

// View builds the window: the sidebar, the header, and the page.
func (a *App) View(c *ui.Context) {
	pal := ApplyTheme(c)
	t := pal.Theme
	s := a.ctrl.State()
	if s == nil {
		s = &core.State{}
	}
	if a.page == PageActivity && s.ErrorBadge {
		if !a.clearing {
			a.clearing = true
			a.ctrl.ActivityViewed()
		}
	} else if !s.ErrorBadge {
		a.clearing = false
	}

	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Width(220).Background(pal.Sidebar).Padding(12, 8).Gap(12).Children(func() {
			Heading(c, locales.AppName(), 20).PaddingX(8)
			ui.Sidebar(c, &a.page, func() {
				ui.SidebarItem(c, PageActivity, nil, locales.NavActivity())
				ui.SidebarItem(c, PageServers, nil, locales.NavServers())
				ui.SidebarItem(c, PageRules, nil, locales.NavRules())
				ui.SidebarItem(c, PageNaming, nil, locales.NavNaming())
				ui.SidebarItem(c, PageSettings, nil, locales.NavSettings())
			}).Grow(1).Background(pal.Sidebar).Label(locales.AppName())
		})
		ui.Column(c).Grow(1).Children(func() {
			a.header(c, t, s)
			a.banners(c, t, s)
			ui.Scroll(c).Grow(1).Padding(20, 24).Gap(16).Children(func() {
				switch a.page {
				case PageServers:
					a.serversPage(c, t, s)
				case PageRules:
					a.rulesPage(c, t, s)
				case PageNaming:
					a.namingPage(c, t, s)
				case PageSettings:
					a.settingsPage(c, t, s)
				default:
					a.activityPage(c, t, s)
				}
			})
		})
	})
	a.prompts(c, s)
}

// header shows the active server, its connection, the monitoring switch,
// and "Upload clipboard now".
func (a *App) header(c *ui.Context, t *ui.Theme, s *core.State) {
	ui.Row(c).Padding(10, 24).Gap(12).AlignItems(ui.Center).BorderColor(t.Border).BorderWidth(0, 0, 1, 0).Children(func() {
		names := make([]string, 0, len(s.Profiles))
		for _, p := range s.Profiles {
			names = append(names, p.Name)
		}
		if s.Active != nil {
			a.headerServer = s.Active.Name
		} else {
			a.headerServer = locales.HeaderNoServer()
			names = append([]string{a.headerServer}, names...)
		}
		if ui.Select(c, &a.headerServer, names).Label(locales.HeaderServer()).Width(200).Changed() {
			for _, p := range s.Profiles {
				if p.Name == a.headerServer {
					a.ctrl.SetActiveProfile(p.ID, nil)
				}
			}
		}
		status := ConnLabel(s.Conn)
		if s.Active == nil {
			status = locales.ConnDisconnected()
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Role(ui.RoleStatus).Label(status).Children(func() {
			ui.Column(c).Size(8, 8).Radius(4).Background(ConnColor(t, s.Conn))
			txt := ui.Text(c, status).TextColor(t.TextMuted)
			if s.ConnErr != nil {
				txt.Tooltip(locales.ConnErrorDetail(s.ConnErr.Error()))
			}
		})
		ui.Spacer(c)
		on := s.Settings.Monitoring
		label := ui.Text(c, locales.HeaderMonitoring())
		if ui.Switch(c, &on).LabelledBy(label).Changed() {
			a.ctrl.SetMonitoring(on, nil)
		}
		if ui.PrimaryButton(c, locales.HeaderUploadNow()).FontWeight(600).Clicked() {
			a.ctrl.CaptureNow()
		}
	})
}

// banners show the startup notice and the last error.
func (a *App) banners(c *ui.Context, t *ui.Theme, s *core.State) {
	msg := ""
	switch {
	case s.Notice != "":
		msg = locales.StartupRecovered(s.Notice)
	case s.LastError != "":
		msg = locales.SomethingWrong(s.LastError)
	}
	if msg == "" {
		return
	}
	ui.Row(c).Margin(12, 24, 0, 24).Padding(10, 14).Gap(12).Radius(t.Radius).AlignItems(ui.Center).
		Background(t.Surface).Border(1, t.Warning).Role(ui.RoleAlert).Children(func() {
		ui.Text(c, msg).Grow(1).Wrap()
		if ui.Button(c, locales.Dismiss()).Clicked() {
			a.ctrl.DismissNotice()
		}
	})
}

// prompts are the host key and secret dialogs the Controller asks for,
// and the quit confirmation.
func (a *App) prompts(c *ui.Context, s *core.State) {
	if ui.AlertDialog(c, &a.quitOpen, locales.QuitTitle(), locales.QuitMessage(a.quitCount), locales.Cancel(), locales.Quit()) == 1 {
		if a.quitConfirm != nil {
			a.quitConfirm()
		}
	}
	if hk := s.HostKey; hk != nil {
		a.hostKeyOpen = true
		e := hk.Err
		server := hk.ProfileName
		var title, msg, action string
		if e.Changed() {
			title = locales.HostKeyChangedTitle(server)
			msg = locales.HostKeyChangedMessage(e.Known.Fingerprint, e.KeyType(), e.Fingerprint())
			action = locales.Replace()
		} else {
			title = locales.HostKeyTitle(server)
			msg = locales.HostKeyMessage(hostPort(e.Host, e.Port), e.KeyType(), e.Fingerprint())
			action = locales.Trust()
		}
		switch ui.AlertDialog(c, &a.hostKeyOpen, title, msg, locales.Cancel(), action) {
		case 1:
			a.ctrl.TrustHostKey(nil)
		case 0, ui.AlertDismissed:
			a.ctrl.DismissHostKey()
		}
	}
	if sp := s.SecretPrompt; sp != nil {
		a.secretOpen = true
		if ui.Modal(c, &a.secretOpen, func() {
			title := locales.SecretPromptTitle(sp.ProfileName)
			switch secret.Kind(sp.Kind) {
			case secret.Passphrase:
				title = locales.PassphrasePromptTitle(sp.ProfileName)
			case secret.ProxyPassword:
				title = locales.ProxyPasswordPromptTitle(sp.ProfileName)
			}
			Heading(c, title, 18)
			ui.Text(c, locales.SecretPromptMessage()).Wrap().MaxWidth(360)
			submitted := ui.TextInput(c, &a.secretValue).Password().AutoFocus().Label(title).Width(320).Submitted()
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, locales.Cancel()).Clicked() {
					a.secretOpen = false
					a.ctrl.DismissSecret()
				}
				if ui.PrimaryButton(c, locales.Connect()).Clicked() || submitted {
					a.ctrl.ProvideSecret(sp.ProfileID, secret.Kind(sp.Kind), a.secretValue, nil)
					a.secretValue, a.secretOpen = "", false
				}
			})
		}).Dismissed() {
			a.ctrl.DismissSecret()
		}
	}
}
