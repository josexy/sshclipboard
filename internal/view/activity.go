package view

import (
	"slices"
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
	sent        store.Filter // last criteria sent from the controls
	shown       store.Filter // criteria and offset of the last rendered page
	arrival     uint64
	requestID   uint64
	pending     bool
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

func sameHistoryCriteria(a, b store.Filter) bool {
	return a.Search == b.Search && a.HideSkipped == b.HideSkipped &&
		slices.Equal(a.Statuses, b.Statuses) && slices.Equal(a.Kinds, b.Kinds)
}

func (a *App) requestHistory(f store.Filter) {
	vm := &a.activity
	vm.requestID++
	id := vm.requestID
	vm.pending = true
	a.ctrl.SetHistoryFilter(f, func(error) {
		if vm.requestID == id {
			vm.pending = false
		}
	})
}

func (a *App) activityPagination(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.activity
	page := s.Filter.Offset/core.HistoryPageSize + 1
	pages := (s.HistoryTotal-1)/core.HistoryPageSize + 1
	changePage := func(delta int) {
		if vm.pending {
			return
		}
		f := s.Filter
		f.Limit = core.HistoryPageSize
		f.Offset = (page - 1 + delta) * core.HistoryPageSize
		a.requestHistory(f)
	}
	ui.Row(c).Key("pagination").AlignItems(ui.Center).Justify(ui.End).Gap(8).Children(func() {
		button(c.Key("previous-page"), iconChevronLeft, locales.ActivityPreviousPage()).
			Disabled(vm.pending || page <= 1).OnClick(func() { changePage(-1) })
		ui.Text(c, locales.ActivityPageNumber(page, pages)).TextColor(t.TextMuted).Role(ui.RoleStatus)
		// The arrow of Next follows its label.
		ui.Button(c.Key("next-page"), "").Children(func() {
			ui.Text(c, locales.ActivityNextPage()).SingleLine()
			icon(c, iconChevronRight)
		}).Disabled(vm.pending || page >= pages).OnClick(func() { changePage(1) })
	})
}

func (a *App) activityEntryMenu(m *ui.Menu, e store.HistoryEntry) {
	ic := entryMenuIcons()
	add := func(label string, img ui.MenuIcon, run func()) {
		if m.Item(label).Icon(img).Chosen() {
			run()
		}
	}
	if e.Status == store.StatusUploaded && e.RemotePath != "" {
		add(locales.ActionCopyPath(), ic.copy, func() { a.ctrl.CopyRemotePath(e.ID, nil) })
	}
	if e.Status == store.StatusFailed || e.Status == store.StatusCancelled {
		add(locales.ActionRetry(), ic.retry, func() { a.ctrl.Retry(e.ID, nil) })
	}
	if e.Status == store.StatusSkipped && e.LocalPath != "" && e.Error != store.ReasonFolder {
		add(locales.ActionUploadAnyway(), ic.upload, func() { a.ctrl.UploadAnyway(e.ID, nil) })
	}
	if e.Status.Active() {
		add(locales.ActionCancel(), ic.cancel, func() { a.ctrl.Cancel(e.ID) })
	}
	if a.plat.OpenFile != nil && core.LocalFileExists(e) {
		add(locales.ActionOpenFile(), ic.open, func() { a.plat.OpenFile(e.LocalPath) })
	}
	if a.plat.ShowInFolder != nil && core.LocalFileExists(e) {
		add(locales.ActionShowInFolder(), ic.folder, func() { a.plat.ShowInFolder(e.LocalPath) })
	}
	add(locales.ActionDelete(), ic.delete, func() { a.ctrl.DeleteEntry(e.ID, nil) })
}

func (a *App) activityPage(c *ui.Context, t *ui.Theme, s *core.State) {
	vm := &a.activity
	now := a.plat.Now()
	Heading(c, locales.ActivityTitle(), 24)

	// Today's summary.
	ui.Row(c).Gap(24).Role(ui.RoleStatus).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			icon(c, iconCircleCheck).TextColor(t.Success)
			ui.Text(c, locales.SummaryUploaded(s.Summary.Uploaded)).Bold()
		})
		iconText(c, iconHardDriveUpload, locales.SummaryBytes(HumanSize(s.Summary.Bytes)), t.TextMuted)
		failed := t.TextMuted
		if s.Summary.Failed > 0 {
			failed = t.Danger
		}
		iconText(c, iconCircleX, locales.SummaryFailed(s.Summary.Failed), failed)
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
		if button(c, iconTrash, locales.ActionClearHistory()).Clicked() {
			vm.clearOpen = true
		}
	})
	if f := vm.filter(); !sameHistoryCriteria(vm.sent, f) {
		vm.sent = f
		a.requestHistory(f)
	}

	rows := s.History
	vm.list.Selected = &vm.selected
	pageChanged := vm.shown.Offset != s.Filter.Offset || !sameHistoryCriteria(vm.shown, s.Filter)
	if len(rows) == 0 || pageChanged {
		vm.list.Choose(-1)
	}
	if len(rows) == 0 || pageChanged || vm.arrival != s.HistoryArrival {
		vm.list.ScrollTo(0, ui.Start)
	}
	vm.shown = s.Filter
	vm.arrival = s.HistoryArrival
	if len(rows) == 0 {
		msg, svg := locales.ActivityEmpty(), iconClipboardList
		if !sameHistoryCriteria(s.Filter, store.Filter{}) {
			msg, svg = locales.ActivityNoMatch(), iconSearchX
		}
		ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(12).Padding(24, 0).
			TextColor(t.TextMuted).Children(func() {
			ui.Icon(c, svg).Size(40, 40)
			ui.Text(c, msg).Wrap().MaxWidth(360).TextAlign(ui.Center)
		})
	} else {
		vm.list.Key = func(i int) any {
			if i < len(rows) {
				return rows[i].ID
			}
			return i
		}
		vm.list.Label = func(i int) string { return rows[i].OriginalName }
		vm.list.CellMenu = func(m *ui.Menu, row, _ int) {
			a.activityEntryMenu(m, rows[row])
		}
		cols := []ui.TableColumn{
			{Title: locales.ColPreview(), Width: 84},
			{Title: locales.ColName()},
			{Title: locales.ColStatus(), Width: 150},
			{Title: locales.ColRemotePath()},
			{Title: locales.ColSize(), Width: 80, Align: ui.End},
			{Title: locales.ColTime(), Width: 120},
		}
		ui.Table(c.Key("history-table"), &vm.list, cols, len(rows), func(row, col int) {
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
			ui.Column(c).Gap(2).FillWidth().Children(func() {
				a.activityCell(c, t, s, e, col, fg, now)
			})
		}).Grow(1).KeepScrollbars(true).Label(locales.ActivityTitle()).Busy(vm.pending)
		a.activityPagination(c, t, s)
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
			ui.Icon(c, kindIcon(e.Kind)).Size(20, 20).TextColor(fg(t.TextMuted)).Label(kindLabel(e.Kind))
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
		ui.Row(c).Gap(6).AlignItems(ui.Center).TextColor(fg(StatusColor(t, e.Status))).Children(func() {
			icon(c, statusIcon(e.Status))
			ui.Text(c, StatusLabel(e.Status)).FontWeight(600)
		})
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
