package view

import (
	"slices"
	"testing"

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
	f.edit(func(s *core.State) { s.History = s.History[2:] })
	tt.Frame()
	if tt.HasText("image.png") || !tt.HasText("report.pdf") {
		t.Errorf("texts %q", tt.Texts())
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
	_, tt := newTester(t, f)
	hasText(t, tt, "Queued")
	hasText(t, tt, "~/clipboard/big.iso")
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
		s.History = []store.HistoryEntry{s.History[0]}
		s.History[0].Status, s.History[0].RemotePath = store.StatusUploaded, "/home/alice/clipboard/big.iso"
		s.Progress = nil
	})
	tt.Frame()
	hasText(t, tt, "Uploaded")
	hasText(t, tt, "/home/alice/clipboard/big.iso")
}

// Each entry action calls the Controller, from the action bar and from
// the row's context menu.
func TestActivityRowActions(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) {
		s.History = append(s.History,
			store.HistoryEntry{ID: 4, CapturedAt: now, Kind: store.KindFile, OriginalName: "big.iso", Status: store.StatusUploading},
			store.HistoryEntry{ID: 5, CapturedAt: now, Kind: store.KindFile, OriginalName: "old.log", Status: store.StatusCancelled})
	})
	_, tt := newTester(t, f)
	for _, c := range []struct{ row, action, call string }{
		{"image.png", "Retry", "Retry(3)"},
		{"old.log", "Retry", "Retry(5)"},
		{"text.txt", "Upload anyway", "UploadAnyway(2)"},
		{"report.pdf", "Copy remote path", "CopyRemotePath(1)"},
		{"big.iso", "Cancel upload", "Cancel(4)"},
		{"report.pdf", "Delete", "DeleteEntry(1)"},
	} {
		click(t, tt, c.row)
		click(t, tt, c.action)
		f.called(t, c.call)
	}
	// Actions that don't apply aren't offered.
	click(t, tt, "report.pdf")
	if tt.HasText("Retry") || tt.HasText("Upload anyway") {
		t.Errorf("an uploaded entry offers %q", tt.Texts())
	}

	// The context menu offers the same.
	f.reset()
	if err := tt.RightClick("image.png"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Contains(menu, "Retry") || !slices.Contains(menu, "Delete") {
		t.Errorf("menu %q", menu)
	}
	if err := tt.ChooseMenuItem("Retry"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	f.called(t, "Retry(3)")
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
