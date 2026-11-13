package view

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/locales"
)

type activityVM struct {
	search      string
	status      int // 0 all, 1 uploaded, 2 failed, 3 skipped, 4 in progress
	kind        int // 0 all, 1 images, 2 files, 3 text
	hideSkipped bool
	list        ui.ListState
	selected    int
	sent        *store.Filter
	clearOpen   bool
}

// filter returns the history filter the controls describe.
func (vm *activityVM) filter() store.Filter {
	f := store.Filter{Search: vm.search, HideSkipped: vm.hideSkipped, Limit: core.HistoryPageSize}
	switch vm.status {
	case 1:
		f.Statuses = []store.Status{store.StatusUploaded}
	case 2:
		f.Statuses = []store.Status{store.StatusFailed}
	case 3:
		f.Statuses = []store.Status{store.StatusSkipped}
	case 4:
		f.Statuses = []store.Status{store.StatusQueued, store.StatusUploading}
	}
	switch vm.kind {
	case 1:
		f.Kinds = []store.Kind{store.KindImage}
	case 2:
		f.Kinds = []store.Kind{store.KindFile}
	case 3:
		f.Kinds = []store.Kind{store.KindText}
	}
	return f
}

func sameFilter(a, b store.Filter) bool {
	if a.Search != b.Search || a.HideSkipped != b.HideSkipped || len(a.Statuses) != len(b.Statuses) || len(a.Kinds) != len(b.Kinds) {
		return false
	}
	for i := range a.Statuses {
		if a.Statuses[i] != b.Statuses[i] {
			return false
		}
	}
	for i := range a.Kinds {
		if a.Kinds[i] != b.Kinds[i] {
			return false
		}
	}
	return true
}

// entryActions are what can be done with an entry, as its menu and the
// action bar offer them.
type entryAction struct {
	label string
	run   func()
}

func (a *App) entryActions(e store.HistoryEntry) []entryAction {
	var out []entryAction
	add := func(label string, run func()) { out = append(out, entryAction{label, run}) }
	if e.Status == store.StatusUploaded && e.RemotePath != "" {
		add(locales.ActionCopyPath(), func() { a.ctrl.CopyRemotePath(e.ID, nil) })
	}
	if e.Status == store.StatusFailed || e.Status == store.StatusCancelled {
		add(locales.ActionRetry(), func() { a.ctrl.Retry(e.ID, nil) })
	}
	if e.Status == store.StatusSkipped && e.LocalPath != "" && e.Error != store.ReasonFolder {
		add(locales.ActionUploadAnyway(), func() { a.ctrl.UploadAnyway(e.ID, nil) })
	}
	if e.Status.Active() {
		add(locales.ActionCancel(), func() { a.ctrl.Cancel(e.ID) })
	}
	if a.plat.OpenFile != nil && core.LocalFileExists(e) {
		add(locales.ActionOpenFile(), func() { a.plat.OpenFile(e.LocalPath) })
	}
	if a.plat.ShowInFolder != nil && core.LocalFileExists(e) {
		add(locales.ActionShowInFolder(), func() { a.plat.ShowInFolder(e.LocalPath) })
	}
	add(locales.ActionDelete(), func() { a.ctrl.DeleteEntry(e.ID, nil) })
	return out
}

func (a *App) activityPage(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.activity
	now := a.plat.Now()
	Heading(c, locales.ActivityTitle(), 24)

	// Today's summary.
	ui.Row(c).Gap(24).Role(ui.RoleStatus).Children(func() {
		ui.Text(c, locales.SummaryUploaded(s.Summary.Uploaded)).Bold()
		ui.Text(c, locales.SummaryBytes(HumanSize(s.Summary.Bytes))).TextColor(t.TextMuted)
		failed := ui.Text(c, locales.SummaryFailed(s.Summary.Failed))
		if s.Summary.Failed > 0 {
			failed.TextColor(t.Danger)
		} else {
			failed.TextColor(t.TextMuted)
		}
	})

	// Search and filters.
	ui.Row(c).Gap(12).AlignItems(ui.Center).Wrap().Children(func() {
		ui.SearchField(c, &vm.search).Placeholder(locales.ActivitySearch()).Label(locales.ActivitySearch()).Width(240)
		ui.Segmented(c, &vm.status, locales.FilterAll(), locales.FilterUploaded(), locales.FilterFailed(),
			locales.FilterSkipped(), locales.FilterInProgress()).Label(locales.ColStatus())
		ui.Segmented(c, &vm.kind, locales.KindAll(), locales.KindImages(), locales.KindFiles(), locales.KindText()).
			Label(locales.KindAll())
		ui.Checkbox(c, &vm.hideSkipped, locales.HideSkipped())
		ui.Spacer(c)
		if ui.Button(c, locales.ActionClearHistory()).Clicked() {
			vm.clearOpen = true
		}
	})
	if f := vm.filter(); vm.sent == nil || !sameFilter(*vm.sent, f) {
		vm.sent = &f
		a.ctrl.SetHistoryFilter(f)
	}

	rows := s.History
	if len(rows) == 0 {
		msg := locales.ActivityEmpty()
		if vm.sent != nil && (vm.search != "" || vm.status != 0 || vm.kind != 0 || vm.hideSkipped) {
			msg = locales.ActivityNoMatch()
		}
		ui.Text(c, msg).TextColor(t.TextMuted).Padding(24, 0)
	} else {
		vm.list.Selected = &vm.selected
		vm.list.Key = func(i int) any {
			if i < len(rows) {
				return rows[i].ID
			}
			return i
		}
		vm.list.Label = func(i int) string { return rows[i].OriginalName }
		cols := []ui.TableColumn{
			{Title: locales.ColPreview(), Width: 84},
			{Title: locales.ColName()},
			{Title: locales.ColStatus(), Width: 150},
			{Title: locales.ColRemotePath()},
			{Title: locales.ColSize(), Width: 80, Align: ui.End},
			{Title: locales.ColTime(), Width: 120},
		}
		ui.Table(c, &vm.list, cols, len(rows), func(row, col int) {
			e := rows[row]
			// Text on the chosen row's accent fill takes the accent's
			// text color, which the status colors don't contrast with.
			chosen := row == vm.selected
			fg := func(col ui.Color) ui.Color {
				if chosen {
					return t.AccentText
				}
				return col
			}
			// Every cell opens the entry's menu, so a right-click anywhere
			// on the row does; the menu is set before the children.
			cell := ui.Column(c).Gap(2).FillWidth()
			cell.ContextMenu(func(m *ui.Menu) {
				for _, act := range a.entryActions(e) {
					if m.Item(act.label).Chosen() {
						act.run()
					}
				}
			})
			cell.Children(func() { a.activityCell(c, t, s, e, col, fg, now) })
		}).Grow(1).MinHeight(240).Label(locales.ActivityTitle())

		// The chosen entry's actions, for the keyboard and the pointer.
		if vm.selected >= 0 && vm.selected < len(rows) {
			e := rows[vm.selected]
			ui.Row(c).Gap(8).Wrap().Role(ui.RoleToolbar).Label(locales.ActionsFor(e.OriginalName)).Children(func() {
				for _, act := range a.entryActions(e) {
					if ui.Button(c, act.label).Clicked() {
						act.run()
					}
				}
			})
		}
	}

	switch ui.AlertDialog(c, &vm.clearOpen, locales.ClearTitle(), locales.ClearMessage(), locales.Cancel(), locales.ClearConfirm()) {
	case 1:
		a.ctrl.ClearHistory(nil)
	}
}

// activityCell builds one cell of an Activity row.
func (a *App) activityCell(c *ui.Context, t *ui.Theme, s *core.State, e store.HistoryEntry, col int,
	fg func(ui.Color) ui.Color, now time.Time) {
	switch col {
	case 0:
		if b := a.thumbs.get(e.ThumbPath); b != nil {
			ui.Image(c, b).Height(28).MaxWidth(56).Fit(ui.Contain).Radius(4).Label(e.OriginalName)
		} else {
			ui.Text(c, kindLabel(e.Kind)).TextColor(fg(t.TextMuted)).FontSize(11)
		}
	case 1:
		ui.Text(c, e.OriginalName).TextColor(fg(t.Text)).SingleLine()
		detail := locales.EntryRule(RuleText(e.RuleName))
		if e.RuleName == "" {
			detail = ""
		}
		if e.Error != "" {
			detail = ReasonText(e.Error)
		}
		if detail != "" {
			ui.Text(c, detail).TextColor(fg(t.TextMuted)).FontSize(12).SingleLine()
		}
	case 2:
		ui.Text(c, StatusLabel(e.Status)).TextColor(fg(StatusColor(t, e.Status))).FontWeight(600)
		if p, ok := s.Progress[e.ID]; ok && p.Total > 1024*1024 && e.Status == store.StatusUploading {
			pct := int(p.Sent * 100 / max(p.Total, 1))
			ui.Progress(c, float64(p.Sent)/float64(max(p.Total, 1))).Width(110)
			ui.Text(c, locales.Progress(pct, HumanSize(int64(p.Speed)))).FontSize(11).TextColor(fg(t.TextMuted))
		}
	case 3:
		path, color := e.RemotePath, t.Text
		if path == "" && e.RemoteName != "" {
			path, color = e.RemoteDir+"/"+e.RemoteName, t.TextMuted
		}
		ui.Text(c, path).TextColor(fg(color)).TruncateMiddle().SingleLine()
	case 4:
		if e.Size > 0 {
			ui.Text(c, HumanSize(e.Size)).TextColor(fg(t.Text))
		}
	case 5:
		ui.Text(c, TimeText(e.CapturedAt, now)).TextColor(fg(t.TextMuted))
	}
}

func kindLabel(k store.Kind) string {
	switch k {
	case store.KindImage:
		return locales.KindImages()
	case store.KindFile:
		return locales.KindFiles()
	}
	return locales.KindText()
}
