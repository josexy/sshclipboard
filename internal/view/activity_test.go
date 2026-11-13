package view

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/josexy/sshclipboard/internal/core"
	"github.com/josexy/sshclipboard/internal/store"
)

func TestActivityShowsHistory(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	// Spec "Summary counts" (and "Plurals").
	hasText(t, tt, "1 file uploaded")
	hasText(t, tt, "4.2 MB today")
	hasText(t, tt, "1 failure")
	for _, s := range []string{"image.png", "text.txt", "report.pdf", "Failed", "Skipped", "Uploaded",
		"connection refused", "Rule: Skip text", "Rule: default action"} {
		hasText(t, tt, s)
	}
	f.edit(func(s *core.State) { s.Summary.Uploaded = 2 })
	tt.Frame()
	hasText(t, tt, "2 files uploaded")
}

// Spec "Search by remote path": the filter goes to the Controller.
func TestActivitySearch(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Search names and paths")
	tt.Type("2026-11")
	if got := f.lastFilter(); got.Search != "2026-11" || got.Limit != core.HistoryPageSize {
		t.Errorf("filter %+v", got)
	}
	// The Controller answers with the matching rows.
	filter := f.lastFilter()
	f.edit(func(s *core.State) {
		s.Filter = filter
		s.History, s.HistoryTotal = s.History[2:], 1
	})
	tt.Frame()
	if tt.HasText("image.png") || !tt.HasText("report.pdf") {
		t.Errorf("texts %q", tt.Texts())
	}
}

func TestActivityPagination(t *testing.T) {
	f := newFake()
	var rows []store.HistoryEntry
	for i := range 26 {
		rows = append(rows, store.HistoryEntry{
			ID: int64(26 - i), CapturedAt: now.Add(-time.Duration(i) * time.Minute),
			OriginalName: fmt.Sprintf("file-%02d.txt", i+1), Kind: store.KindText, Status: store.StatusSkipped,
		})
	}
	f.edit(func(s *core.State) { s.History, s.HistoryTotal = rows[:25], len(rows) })
	var complete func(error)
	f.historyRequested = func(_ store.Filter, done func(error)) { complete = done }
	a, tt := newTester(t, f)
	hasText(t, tt, "Page 1 of 2")
	checkButton := func(name string, disabled bool) {
		t.Helper()
		if b, ok := tt.AccessByRole(ui.RoleButton, name); !ok || b.Disabled != disabled {
			t.Fatalf("%q found=%v, disabled=%v, want disabled=%v", name, ok, b.Disabled, disabled)
		}
	}
	checkButton("Previous page", true)
	checkButton("Next page", false)
	click(t, tt, "file-01.txt")
	focusByTab(t, tt, "Next page")
	tt.Key(0, ui.KeyEnter)
	if got := f.lastFilter(); got.Offset != 25 || got.Limit != 25 {
		t.Fatalf("next page requested %+v", got)
	}
	checkButton("Next page", true)
	hasText(t, tt, "Page 1 of 2")

	filter := f.lastFilter()
	f.edit(func(s *core.State) { s.Filter, s.History = filter, rows[25:] })
	complete(nil)
	tt.Frame()
	hasText(t, tt, "Page 2 of 2")
	hasText(t, tt, "file-26.txt")
	checkButton("Previous page", false)
	checkButton("Next page", true)
	if a.activity.selected != -1 || tt.HasText("file-01.txt") {
		t.Fatalf("first page selection remained after flipping: %q", tt.Texts())
	}

	click(t, tt, "Previous page")
	if got := f.lastFilter(); got.Offset != 0 {
		t.Fatalf("previous page requested %+v", got)
	}
	oldComplete := complete
	click(t, tt, "Hide skipped")
	if got := f.lastFilter(); !got.HideSkipped || got.Offset != 0 {
		t.Fatalf("filter change requested %+v", got)
	}
	oldComplete(nil)
	tt.Frame()
	checkButton("Previous page", true)
	complete(errors.New("query failed"))
	tt.Frame()
	checkButton("Previous page", false)
	hasText(t, tt, "Page 2 of 2")

	click(t, tt, "Hide skipped")
	click(t, tt, "Hide skipped")
	filter = f.lastFilter()
	f.edit(func(s *core.State) { s.Filter, s.History, s.HistoryTotal = filter, nil, 0 })
	complete(nil)
	tt.Frame()
	hasText(t, tt, "No entries match.")
	if tt.HasText("Previous page") || tt.HasText("Next page") {
		t.Fatal("pagination is visible with no matching entries")
	}
}

func TestActivityTableScrollsIndependently(t *testing.T) {
	for _, size := range []struct{ w, h int }{{1100, 720}, {860, 560}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			f := newFake()
			f.edit(func(s *core.State) {
				for i := range core.HistoryPageSize - len(s.History) {
					s.History = append(s.History, store.HistoryEntry{
						ID: int64(i + 4), OriginalName: fmt.Sprintf("entry-%02d.txt", i),
						CapturedAt: now, Kind: store.KindText, Status: store.StatusSkipped, RuleName: store.RuleDefault,
					})
				}
				s.HistoryTotal = len(s.History) + 1
			})
			a, tt := newTester(t, f)
			tt.SetSize(size.w, size.h)
			click(t, tt, "image.png")
			tt.Move(0, 0)
			table, ok := tt.AccessByRole(ui.RoleTable, "Activity")
			if !ok || table.Bounds.H <= 32 {
				t.Fatalf("table has no room for rows: %+v", table.Bounds)
			}
			fixed := map[string]ui.Rect{}
			for _, name := range []string{"Search names and paths", "Preview", "Next page"} {
				r, found := tt.Find(name)
				if !found || r.Y < 0 || r.Y+r.H > float32(size.h) {
					t.Fatalf("%q is offscreen: %+v", name, r)
				}
				fixed[name] = r
			}
			checkFixed := func() {
				t.Helper()
				for name, before := range fixed {
					if after, ok := tt.Find(name); !ok || after != before {
						t.Errorf("scrolling moved %q from %+v to %+v", name, before, after)
					}
				}
			}
			r := table.Bounds
			first, _ := a.activity.list.Visible()
			// Grab the thumb near the top of the row viewport at the
			// table's right edge, starting with the pointer away.
			x, y := r.X+r.W-6, r.Y+44
			tt.Press(x, y)
			if after, _ := a.activity.list.Visible(); after != first {
				t.Fatalf("pressing the thumb jumped from row %d to %d", first, after)
			}
			tt.Move(x, y+r.H/3)
			tt.Release(x, y+r.H/3)
			if after, _ := a.activity.list.Visible(); after <= first {
				t.Fatalf("dragging the right scrollbar left the first visible row at %d", after)
			}
			checkFixed()
			tt.Wheel(r.X+r.W/2, r.Y+r.H/2, 0, -5000)
			tt.Advance(time.Second)
			if after, _ := a.activity.list.Visible(); after != 0 {
				t.Fatalf("wheel did not scroll the table to the top: first row %d", after)
			}
			checkFixed()
			focusByTab(t, tt, "Next page")
			tt.Key(0, ui.KeyEnter)
			if got := f.lastFilter(); got.Offset != core.HistoryPageSize {
				t.Fatalf("keyboard next page requested %+v", got)
			}
		})
	}
}

func TestActivityScrollsToNewClipboardCaptureOnce(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) {
		for i := range core.HistoryPageSize - len(s.History) {
			s.History = append(s.History, store.HistoryEntry{
				ID: int64(i + 4), OriginalName: fmt.Sprintf("entry-%02d.txt", i),
				CapturedAt: now, Kind: store.KindText, Status: store.StatusSkipped, RuleName: store.RuleDefault,
			})
		}
		s.HistoryTotal = len(s.History)
	})
	a, tt := newTester(t, f)
	table, ok := tt.AccessByRole(ui.RoleTable, "Activity")
	if !ok {
		t.Fatal("missing Activity table")
	}
	r := table.Bounds
	scrollDown := func() int {
		t.Helper()
		tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, 450)
		first, _ := a.activity.list.Visible()
		if first <= 0 {
			t.Fatalf("did not scroll down: first row %d", first)
		}
		return first
	}
	scrollDown()
	f.edit(func(s *core.State) {
		entry := store.HistoryEntry{ID: 100, OriginalName: "new-clipboard.txt", Kind: store.KindText, Status: store.StatusQueued}
		s.History = append([]store.HistoryEntry{entry}, s.History[:core.HistoryPageSize-1]...)
		s.HistoryTotal++
		s.HistoryArrival++
	})
	tt.Frame()
	if first, _ := a.activity.list.Visible(); first != 0 {
		t.Fatalf("new capture left the first visible row at %d", first)
	}
	hasText(t, tt, "new-clipboard.txt")

	first := scrollDown()
	f.edit(func(s *core.State) {
		s.History = slices.Clone(s.History)
		s.History[0].Status = store.StatusUploading
	})
	tt.Frame()
	if after, _ := a.activity.list.Visible(); after != first {
		t.Fatalf("status refresh moved the first visible row from %d to %d", first, after)
	}
}

// Spec "Hide skipped", and the status and kind filters.
func TestActivityFilters(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Hide skipped")
	if !f.lastFilter().HideSkipped {
		t.Error("hide skipped not sent")
	}
	click(t, tt, "Hide skipped")
	if f.lastFilter().HideSkipped {
		t.Error("turning it off was not sent")
	}
	click(t, tt, "Failed") // the segment, before the table's "Failed"
	if got := f.lastFilter().Statuses; !slices.Equal(got, []store.Status{store.StatusFailed}) {
		t.Errorf("statuses %v", got)
	}
	click(t, tt, "In progress")
	if got := f.lastFilter().Statuses; !slices.Equal(got, []store.Status{store.StatusQueued, store.StatusUploading}) {
		t.Errorf("statuses %v", got)
	}
	click(t, tt, "Images")
	if got := f.lastFilter().Kinds; !slices.Equal(got, []store.Kind{store.KindImage}) {
		t.Errorf("kinds %v", got)
	}
}

// Spec "Live update": the row follows queued → uploading → uploaded as the
// Controller publishes, through notify's redraw.
func TestActivityLiveStatus(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) {
		s.History = []store.HistoryEntry{{ID: 7, CapturedAt: now, Kind: store.KindFile, OriginalName: "big.iso",
			Status: store.StatusQueued, RemoteDir: "~/clipboard", RemoteName: "big.iso", Size: 20 << 20}}
	})
	a, tt := newTester(t, f)
	hasText(t, tt, "Queued")
	hasText(t, tt, "~/clipboard/big.iso")
	click(t, tt, "big.iso")
	f.edit(func(s *core.State) {
		s.History = []store.HistoryEntry{s.History[0]}
		s.History[0].Status = store.StatusUploading
		s.Uploading = 1
		s.Progress = map[int64]core.Progress{7: {Sent: 6 << 20, Total: 20 << 20, Speed: 2.1 * 1024 * 1024}}
	})
	tt.Frame() // what Window.Update does after notify
	hasText(t, tt, "Uploading")
	// Spec "Progress shown": a percentage and a speed.
	hasText(t, tt, "30% · 2.1 MB/s")
	f.edit(func(s *core.State) {
		s.History = []store.HistoryEntry{
			{ID: 8, OriginalName: "new.txt", Status: store.StatusSkipped, Kind: store.KindText, CapturedAt: now},
			s.History[0],
		}
		s.History[1].Status, s.History[1].RemotePath = store.StatusUploaded, "/home/alice/clipboard/big.iso"
		s.HistoryTotal = len(s.History)
		s.Progress = nil
	})
	tt.Frame()
	hasText(t, tt, "Uploaded")
	hasText(t, tt, "/home/alice/clipboard/big.iso")
	if a.activity.selected != 1 {
		t.Fatalf("selected row = %d, want the moved upload", a.activity.selected)
	}
	tt.Key(ui.Shift, ui.KeyF10)
	if menu := tt.Menu(); !slices.Equal(menu, []string{"Copy remote path", "Delete"}) {
		t.Fatalf("keyboard menu %q, want the moved upload's actions", menu)
	}
	if err := tt.ChooseMenuItem("Copy remote path"); err != nil {
		t.Fatal(err)
	}
	f.called(t, "CopyRemotePath(7)")
}

// The context menu offers the actions for the row that was right-clicked.
func TestActivityRowActions(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) {
		s.History = append(s.History,
			store.HistoryEntry{ID: 4, CapturedAt: now, Kind: store.KindFile, OriginalName: "big.iso", Status: store.StatusUploading},
			store.HistoryEntry{ID: 5, CapturedAt: now, Kind: store.KindFile, OriginalName: "old.log", Status: store.StatusCancelled})
	})
	_, tt := newTester(t, f)
	click(t, tt, "report.pdf")
	if tt.HasText("Copy remote path") || tt.HasText("Delete") {
		t.Fatalf("selected row still shows action buttons: %q", tt.Texts())
	}
	for _, c := range []struct{ row, action, call string }{
		{"image.png", "Retry", "Retry(3)"},
		{"old.log", "Retry", "Retry(5)"},
		{"text.txt", "Upload anyway", "UploadAnyway(2)"},
		{"report.pdf", "Copy remote path", "CopyRemotePath(1)"},
		{"big.iso", "Cancel upload", "Cancel(4)"},
		{"report.pdf", "Delete", "DeleteEntry(1)"},
	} {
		if err := tt.RightClick(c.row); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem(c.action); err != nil {
			t.Fatal(err)
		}
		f.called(t, c.call)
	}
	if err := tt.RightClick("report.pdf"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Equal(menu, []string{"Copy remote path", "Delete"}) {
		t.Errorf("uploaded row offers %q", menu)
	}
	tt.CloseMenu()
}

func TestActivityContextMenuInCellPadding(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	row, ok := tt.AccessByRole(ui.RoleRow, "text.txt")
	if !ok {
		t.Fatal("missing text.txt row")
	}
	for _, col := range []string{"Preview", "Name", "Status", "Remote path", "Size", "Time"} {
		t.Run(col, func(t *testing.T) {
			header, ok := tt.AccessByRole(ui.RoleColumnHeader, col)
			if !ok {
				t.Fatalf("missing %q column", col)
			}
			// The last DIP of the row is below the contents of every cell,
			// including the empty size and remote path cells.
			tt.RightClickAt(header.Bounds.X+header.Bounds.W/2, row.Bounds.Y+row.Bounds.H-1)
			if menu := tt.Menu(); !slices.Equal(menu, []string{"Upload anyway", "Delete"}) {
				t.Fatalf("menu %q, want text.txt actions", menu)
			}
			if err := tt.ChooseMenuItem("Upload anyway"); err != nil {
				t.Fatal(err)
			}
			f.called(t, "UploadAnyway(2)")
			f.reset()
		})
	}
}

// Spec "Clear all" asks first.
func TestActivityClearHistory(t *testing.T) {
	f := newFake()
	_, tt := newTester(t, f)
	click(t, tt, "Clear history…")
	hasText(t, tt, "Clear history?")
	click(t, tt, "Clear")
	f.called(t, "ClearHistory()")
}

// Opening Activity clears the tray's error state.
func TestActivityViewedClearsError(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) { s.ErrorBadge = true })
	_, tt := newTester(t, f)
	f.called(t, "ActivityViewed()")
	_ = tt
}
