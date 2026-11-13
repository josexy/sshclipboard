package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/clipwatch"
	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/view"
	"github.com/josexy/sshclipboard/locales"
)

func main() {
	// One instance per user: a second launch shows the first's window
	// (OnSecondInstance) and exits.
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	// English whatever the system's language: the only catalog shipped.
	mygo.App.SetLanguages("en")
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return err
	}
	st, err := store.OpenDir(ctx, dataDir)
	if err != nil {
		return showFatal(err)
	}
	defer st.Close()
	settings, err := st.Settings.Load(ctx)
	if err != nil {
		return showFatal(err)
	}
	// Before Run, so the app starts in the saved appearance.
	view.ApplyAppearance(settings.Appearance)

	var (
		win           *mygo.Window
		app           *view.App
		ctrl          *core.Controller
		tray          *mygo.Tray
		icons         map[core.TrayState][]byte
		trayModel     core.TrayModel
		trayShown     bool
		quitting      bool // a quit is under way: closes are its own
		quitConfirmed bool
	)
	show := func() {
		win.Show()
		win.Focus()
	}
	actions := trayActions{
		setMonitoring: func(on bool) { ctrl.SetMonitoring(on, nil) },
		uploadNow:     func() { ctrl.CaptureNow() },
		setActive:     func(id int64) { ctrl.SetActiveProfile(id, nil) },
		open:          show,
		quit:          func() { mygo.App.Quit() },
	}
	// updateTray rebuilds the tray when what it shows changed. Main
	// thread.
	updateTray := func() {
		if tray == nil || ctrl == nil {
			return
		}
		m := core.Tray(ctrl.State())
		if trayShown && m.Equal(trayModel) {
			return
		}
		trayModel, trayShown = m, true
		if icon := icons[m.Icon]; icon != nil {
			tray.SetIcon(icon, false)
		}
		tray.SetToolTip(trayTooltip(m))
		if runtime.GOOS != "windows" {
			// Elsewhere the menu opens on click; on Windows a click shows
			// the window and the menu is built at right-click.
			tray.SetMenu(mygo.NewMenu(trayMenu(m, actions)))
		}
	}
	// notify runs the Controller's updates on the main thread and redraws.
	notify := func(fn func()) {
		win.Update(func() {
			fn()
			updateTray()
		})
	}

	mygo.App.WhenReady(func() {
		hotkey := core.NewHotkey(mygo.GlobalShortcut, func() { ctrl.CaptureNow() })
		ctrl = core.New(core.Options{
			Store: st, Secrets: secret.NewSystem(),
			Source: clipboardSource{}, Watcher: clipwatch.New(fingerprint), Clipboard: mygo.Clipboard,
			Notifier: notifier{}, Notify: notify, DataDir: dataDir,
			OpenSSHKnownHosts: sshconn.DefaultOpenSSHKnownHosts(),
			Hooks: core.Hooks{
				Appearance:    view.ApplyAppearance,
				StartAtLogin:  mygo.App.SetOpenAtLogin,
				Hotkey:        hotkey.Swap,
				WindowVisible: func() bool { return win.IsVisible() },
			},
		})
		app = view.NewApp(ctrl, platform(dataDir, func() *mygo.Window { return win }), notify)
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     locales.WindowTitle(),
			Width:     1100,
			Height:    720,
			MinWidth:  860,
			MinHeight: 560,
			StateKey:  "main", // reopens at its last size and position
			Hidden:    mygo.App.WasOpenedAtLogin() || settings.StartHidden,
			Content:   ui.View(app.View),
		})

		// Close to tray, or quit through OnBeforeQuit's confirmation.
		win.OnClose(func(e *mygo.CloseEvent) {
			if quitting {
				return
			}
			e.PreventDefault()
			s := ctrl.State()
			if !s.Settings.CloseToTray {
				mygo.App.Quit()
				return
			}
			win.Hide()
			if !s.Settings.CloseNoticeShown {
				notifier{}.Notify(core.Notice{Kind: core.NoticeStillRunning})
				ctrl.SetSetting(store.KeyCloseNoticeShown, true, nil)
			}
		})
		mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
			if n := ctrl.InFlight(); n > 0 && !quitConfirmed && !e.SessionEnding {
				e.PreventDefault()
				show()
				win.Update(func() {
					app.AskQuit(n, func() {
						quitConfirmed = true
						mygo.App.Quit()
					})
				})
				return
			}
			quitting = true
		})
		mygo.App.OnSecondInstance(func([]string, string) { show() })
		mygo.App.OnNotificationClick(func(string) {
			show()
			win.Update(func() { app.Navigate(view.PageActivity) })
		})

		if err := ctrl.Start(); err != nil {
			log.Println("start:", err)
		}
		if err := hotkey.Start(settings.Hotkey); err != nil {
			log.Printf("hotkey %s: %v", settings.Hotkey, err)
		}
		// The login item follows the saved setting.
		if mygo.App.OpenAtLogin() != settings.StartAtLogin {
			if err := mygo.App.SetOpenAtLogin(settings.StartAtLogin); err != nil {
				log.Println("start at login:", err)
			}
		}

		var err error
		if icons, err = trayIcons(appIcon); err != nil {
			log.Println("tray icons:", err)
		}
		tray, err = mygo.NewTray(mygo.TrayOptions{ID: "main", Icon: icons[core.TrayMonitoring], ToolTip: locales.AppName()})
		if err != nil {
			log.Println("tray:", err)
			return
		}
		tray.OnClick(show)
		tray.OnRightClick(func() {
			tray.SetMenu(mygo.NewMenu(trayMenu(core.Tray(ctrl.State()), actions)))
			tray.PopUpMenu()
			tray.SetMenu(nil)
		})
		updateTray()
	})
	err = mygo.App.Run()
	if ctrl != nil {
		// Unfinished uploads stay recorded and are marked interrupted at
		// the next start.
		ctrl.Close()
	}
	return err
}

// platform is what the views ask of the desktop.
func platform(dataDir string, win func() *mygo.Window) view.Platform {
	pick := func(dir bool) func(done func(string)) {
		return func(done func(string)) {
			go func() {
				paths, _ := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: win(), Directory: dir, CreateDirectories: dir})
				p := ""
				if len(paths) > 0 {
					p = paths[0]
				}
				win().Update(func() { done(p) })
			}()
		}
	}
	return view.Platform{
		OpenFile:       func(p string) { go mygo.Shell.OpenPath(p) },
		ShowInFolder:   mygo.Shell.ShowItemInFolder,
		PickFile:       pick(false),
		PickFolder:     pick(true),
		Version:        mygo.App.Version(),
		DefaultStaging: filepath.Join(dataDir, "staging"),
	}
}

// fatalView says why the app can't start, as a database from a newer
// version.
func fatalView(err error) func(c *ui.Context) {
	msg := locales.SomethingWrong(err.Error())
	if errors.Is(err, store.ErrNewerSchema) {
		msg = locales.StartupNewer(err.Error())
	}
	return func(c *ui.Context) {
		view.ApplyTheme(c)
		ui.Column(c).Fill().Padding(24).Gap(12).Children(func() {
			view.Heading(c, locales.AppName(), 20)
			ui.Text(c, msg).Wrap().Role(ui.RoleAlert)
		})
	}
}

// showFatal shows fatalView in a window until it is closed.
func showFatal(err error) error {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: locales.WindowTitle(), Width: 560, Height: 220,
			Content: ui.View(fatalView(err))})
	})
	return mygo.App.Run()
}
