package view

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
			if menu := tt.Menu(); !slices.Equal(menu, []string{"Preview", "Upload anyway", "Delete"}) {
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

const previewText = "first line\nsecond line"

// previewStub stands in for the preview's reads, by entry ID: what
// contents has, else unavailable. The reads of the IDs held wait until
// released, and redraws queue for the test to run, as Window.Update would.
type previewStub struct {
	mu       sync.Mutex
	contents map[int64]previewContent
	hold     map[int64]chan struct{}
	reads    []int64
	redraws  chan func()
}

// newPreviewTester shows the app with the preview's reads stubbed:
// image.png (ID 3) is a 3840 × 2160 screenshot of 2.1 MB, and text.txt
// (ID 2) holds previewText.
func newPreviewTester(t *testing.T, f *fakeCtrl) (*App, *ui.Tester, *previewStub) {
	t.Helper()
	f.edit(func(s *core.State) {
		s.History = slices.Clone(s.History)
		for i, e := range s.History {
			if e.ID == 3 {
				s.History[i].Width, s.History[i].Height, s.History[i].Size = 3840, 2160, 2_202_010
			}
		}
	})
	a, tt := newTester(t, f)
	ps := &previewStub{
		contents: map[int64]previewContent{
			3: {bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 3840, 2160))), width: 3840, height: 2160, size: 2_202_010},
			2: {text: ui.NewTextBuffer(previewText), size: int64(len(previewText))},
		},
		hold:    map[int64]chan struct{}{},
		redraws: make(chan func(), 64),
	}
	a.preview.redraw = func(fn func()) { ps.redraws <- fn }
	a.preview.read = ps.read
	return a, tt, ps
}

func (ps *previewStub) read(e store.HistoryEntry) previewContent {
	ps.mu.Lock()
	pc, ok := ps.contents[e.ID]
	gate := ps.hold[e.ID]
	ps.reads = append(ps.reads, e.ID)
	ps.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if !ok {
		pc = previewContent{unavailable: true}
	}
	pc.kind = e.Kind
	return pc
}

func (ps *previewStub) set(id int64, pc previewContent) {
	ps.mu.Lock()
	ps.contents[id] = pc
	ps.mu.Unlock()
}

// holdReads has the reads of ids wait until released.
func (ps *previewStub) holdReads(ids ...int64) {
	ps.mu.Lock()
	for _, id := range ids {
		ps.hold[id] = make(chan struct{})
	}
	ps.mu.Unlock()
}

func (ps *previewStub) release(id int64) {
	ps.mu.Lock()
	close(ps.hold[id])
	ps.mu.Unlock()
}

func (ps *previewStub) readCount() int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return len(ps.reads)
}

// load runs the redraw of the next read to finish and builds a frame.
func (ps *previewStub) load(t *testing.T, tt *ui.Tester) {
	t.Helper()
	select {
	case fn := <-ps.redraws:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("no preview read finished")
	}
	tt.Frame()
}

// shownPreview returns the preview dialog, named by its entry.
func shownPreview(tt *ui.Tester) (ui.AccessInfo, bool) {
	for _, a := range tt.AccessAll() {
		if a.Role == ui.RoleDialog {
			return a, true
		}
	}
	return ui.AccessInfo{}, false
}

func wantPreview(t *testing.T, tt *ui.Tester, name string) ui.AccessInfo {
	t.Helper()
	d, ok := shownPreview(tt)
	if !ok || d.Name != name {
		t.Fatalf("preview of %q shows (%v), want %q", d.Name, ok, name)
	}
	return d
}

func wantNoPreview(t *testing.T, tt *ui.Tester) {
	t.Helper()
	if d, ok := shownPreview(tt); ok {
		t.Fatalf("preview of %q shows", d.Name)
	}
}

// wantTableFocus checks that the history table has the focus, on the
// chosen row named row, or with none chosen when row is "".
func wantTableFocus(t *testing.T, tt *ui.Tester, row string) {
	t.Helper()
	f, ok := focused(tt)
	switch {
	case !ok:
		t.Fatal("nothing has the focus")
	case row == "" && (f.Role != ui.RoleTable || f.Name != "Activity"):
		t.Fatalf("focus on %v %q, want the table", f.Role, f.Name)
	case row != "" && (f.Role != ui.RoleRow || f.Name != row || !f.Selected):
		t.Fatalf("focus on %v %q (selected %v), want the table on row %q", f.Role, f.Name, f.Selected, row)
	}
}

// previewedText returns the text the preview of the entry named name
// shows.
func previewedText(tt *ui.Tester, name string) (string, bool) {
	a, ok := tt.AccessByRole(ui.RoleTextField, name)
	return a.Value, ok
}

func inside(r, outer ui.Rect) bool {
	return r.W > 0 && r.H > 0 && r.X >= outer.X && r.Y >= outer.Y && r.X+r.W <= outer.X+outer.W && r.Y+r.H <= outer.Y+outer.H
}

// Specs "Preview a screenshot", "Large screenshot" and "Window stays
// responsive".
func TestPreviewScreenshot(t *testing.T) {
	f := newFake()
	_, tt, ps := newPreviewTester(t, f)
	click(t, tt, "image.png")
	tt.Key(0, ui.KeySpace)
	d := wantPreview(t, tt, "image.png")
	hasText(t, tt, "Loading…")
	hasText(t, tt, "3840 × 2160")
	hasText(t, tt, "2.1 MB")
	ps.load(t, tt)
	if tt.HasText("Loading…") {
		t.Error("still loading")
	}
	img, ok := tt.AccessByRole(ui.RoleImage, "image.png")
	if !ok || !inside(img.Bounds, d.Bounds) {
		t.Fatalf("image %v at %+v, outside the dialog %+v", ok, img.Bounds, d.Bounds)
	}
	// Scaled down to fit: the dialog is narrower than the image.
	if img.Bounds.W >= 3840 || img.Bounds.W < d.Bounds.W/2 {
		t.Errorf("image %+v in the dialog %+v", img.Bounds, d.Bounds)
	}
}

// Spec "Small image": never enlarged.
func TestPreviewSmallImage(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) { s.History = slices.Clone(s.History) })
	_, tt, ps := newPreviewTester(t, f)
	ps.set(3, previewContent{bitmap: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 64, 64))), width: 64, height: 64, size: 300})
	f.edit(func(s *core.State) {
		s.History = slices.Clone(s.History)
		s.History[0].Width, s.History[0].Height, s.History[0].Size = 64, 64, 300
	})
	click(t, tt, "image.png")
	tt.Key(0, ui.KeySpace)
	ps.load(t, tt)
	hasText(t, tt, "64 × 64")
	if img, ok := tt.AccessByRole(ui.RoleImage, "image.png"); !ok || img.Bounds.W != 64 || img.Bounds.H != 64 {
		t.Errorf("image %v at %+v, want 64 × 64", ok, img.Bounds)
	}
}

// Spec "Preview copied text", and "Text is not edited".
func TestPreviewText(t *testing.T) {
	f := newFake()
	_, tt, ps := newPreviewTester(t, f)
	click(t, tt, "text.txt")
	tt.Key(0, ui.KeySpace)
	wantPreview(t, tt, "text.txt")
	hasText(t, tt, "Loading…")
	ps.load(t, tt)
	text, ok := previewedText(tt, "text.txt")
	if !ok || text != previewText {
		t.Fatalf("preview shows %q (%v), want %q", text, ok, previewText)
	}
	if a, _ := tt.AccessByRole(ui.RoleTextField, "text.txt"); !a.ReadOnly || !a.Multiline {
		t.Errorf("text field %+v, want read-only and multiline", a)
	}
	hasText(t, tt, "22 B")
	if tt.HasText("Showing the first 1 MB of 22 B") {
		t.Error("a short text says it is cut")
	}
	tt.Key(0, ui.KeyTab)
	tt.Type("typed")
	tt.Key(0, ui.KeyBackspace)
	tt.Key(0, ui.KeyDelete)
	if text, _ := previewedText(tt, "text.txt"); text != previewText {
		t.Errorf("typing changed the text to %q", text)
	}
	if got := ps.contents[2].text.String(); got != previewText {
		t.Errorf("typing changed the buffer to %q", got)
	}
}

// Spec "Long text".
func TestPreviewLongText(t *testing.T) {
	f := newFake()
	_, tt, ps := newPreviewTester(t, f)
	ps.set(2, previewContent{text: ui.NewTextBuffer("the beginning"), truncated: true, size: 5 << 20})
	click(t, tt, "text.txt")
	tt.Key(0, ui.KeySpace)
	ps.load(t, tt)
	hasText(t, tt, "Showing the first 1 MB of 5 MB")
	if text, _ := previewedText(tt, "text.txt"); text != "the beginning" {
		t.Errorf("preview shows %q", text)
	}
}

// Specs "Pruned entry" and "Skipped because too large", with the real
// reads of staged files.
func TestPreviewReadsStagedFiles(t *testing.T) {
	dir := t.TempDir()
	png := writePNG(t, 320, 200)
	txt := filepath.Join(dir, "c5.txt")
	if err := os.WriteFile(txt, []byte("staged text"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFake()
	f.edit(func(s *core.State) {
		s.History = []store.HistoryEntry{
			{ID: 5, CapturedAt: now, Kind: store.KindText, OriginalName: "kept.txt", Status: store.StatusSkipped,
				LocalPath: txt, Staged: true, Size: 11},
			{ID: 6, CapturedAt: now, Kind: store.KindImage, OriginalName: "shot.png", Status: store.StatusUploaded,
				LocalPath: png, Staged: true, Width: 320, Height: 200, Size: 900},
			{ID: 7, CapturedAt: now, Kind: store.KindText, OriginalName: "pruned.txt", Status: store.StatusFailed,
				LocalPath: filepath.Join(dir, "gone.txt"), Staged: true, Size: 40},
			{ID: 8, CapturedAt: now, Kind: store.KindImage, OriginalName: "huge.png", Status: store.StatusSkipped,
				Error: store.ReasonTooLarge, Size: 90 << 20},
		}
		s.HistoryTotal = 4
	})
	a, tt := newTester(t, f)
	redraws := make(chan func(), 8)
	a.preview.redraw = func(fn func()) { redraws <- fn }
	load := func() {
		t.Helper()
		select {
		case fn := <-redraws:
			fn()
		case <-time.After(10 * time.Second):
			t.Fatal("no preview read finished")
		}
		tt.Frame()
	}
	click(t, tt, "kept.txt")
	tt.Key(0, ui.KeySpace)
	load()
	if text, _ := previewedText(tt, "kept.txt"); text != "staged text" {
		t.Errorf("kept.txt shows %q", text)
	}
	tt.Key(0, ui.KeyDown)
	load()
	if _, ok := tt.AccessByRole(ui.RoleImage, "shot.png"); !ok {
		t.Errorf("shot.png shows no image: %q", tt.Texts())
	}
	hasText(t, tt, "320 × 200")
	for _, name := range []string{"pruned.txt", "huge.png"} {
		tt.Key(0, ui.KeyDown)
		load()
		wantPreview(t, tt, name)
		hasText(t, tt, "This content is no longer available")
	}
	// The pruned entry's other actions are still there.
	tt.Key(0, ui.KeyEscape)
	tt.Key(0, ui.KeyUp)
	tt.Key(ui.Shift, ui.KeyF10)
	if menu := tt.Menu(); !slices.Equal(menu, []string{"Preview", "Retry", "Delete"}) {
		t.Errorf("pruned entry's menu %q", menu)
	}
	tt.CloseMenu()
}

// The dialog keeps its footer in view in the smallest window and the
// default one.
func TestPreviewLayout(t *testing.T) {
	for _, size := range []struct{ w, h int }{{860, 560}, {1100, 720}} {
		for _, row := range []string{"image.png", "text.txt"} {
			t.Run(fmt.Sprintf("%s %dx%d", row, size.w, size.h), func(t *testing.T) {
				f := newFake()
				_, tt, ps := newPreviewTester(t, f)
				tt.SetSize(size.w, size.h)
				ps.set(2, previewContent{text: ui.NewTextBuffer(strings.Repeat("a line of text\n", 400)), size: 6000})
				click(t, tt, row)
				tt.Key(0, ui.KeySpace)
				ps.load(t, tt)
				d := wantPreview(t, tt, row)
				if !inside(d.Bounds, ui.Rect{W: float32(size.w), H: float32(size.h)}) {
					t.Fatalf("dialog %+v outside the window", d.Bounds)
				}
				for _, name := range []string{"Close preview", "Space or Esc to close · ↑↓ to browse"} {
					if r, ok := tt.Find(name); !ok || !inside(r, d.Bounds) {
						t.Errorf("%q at %+v (%v), outside the dialog %+v", name, r, ok, d.Bounds)
					}
				}
				hint, _ := tt.Find("Space or Esc to close · ↑↓ to browse")
				var body ui.Rect
				if row == "image.png" {
					a, _ := tt.AccessByRole(ui.RoleImage, row)
					body = a.Bounds
				} else {
					a, _ := tt.AccessByRole(ui.RoleTextField, row)
					body = a.Bounds
				}
				if !inside(body, d.Bounds) || body.Y+body.H > hint.Y || body.H < 100 {
					t.Errorf("body %+v overlaps the footer %+v or leaves the dialog %+v", body, hint, d.Bounds)
				}
			})
		}
	}
}

// Spec "File entry not previewed": Space neither previews a file nor
// pages the table.
func TestPreviewNotForFiles(t *testing.T) {
	f := newFake()
	f.edit(func(s *core.State) {
		s.History = nil
		for i := range core.HistoryPageSize {
			s.History = append(s.History, store.HistoryEntry{
				ID: int64(100 - i), OriginalName: fmt.Sprintf("diagram-%02d.png", i), CapturedAt: now,
				Kind: store.KindFile, Status: store.StatusUploaded, LocalPath: `C:\pictures\diagram.png`,
			})
		}
		s.HistoryTotal = len(s.History)
	})
	a, tt, ps := newPreviewTester(t, f)
	click(t, tt, "diagram-00.png")
	for range 3 {
		tt.Key(0, ui.KeySpace)
		wantNoPreview(t, tt)
	}
	if first, _ := a.activity.list.Visible(); first != 0 {
		t.Errorf("Space scrolled the table to row %d", first)
	}
	if a.activity.selected != 0 || ps.readCount() != 0 {
		t.Errorf("selected %d, %d reads", a.activity.selected, ps.readCount())
	}
	wantTableFocus(t, tt, "diagram-00.png")
}

// Spec "Nothing selected".
func TestPreviewNothingSelected(t *testing.T) {
	f := newFake()
	a, tt, _ := newPreviewTester(t, f)
	click(t, tt, "image.png")
	a.activity.list.Choose(-1)
	tt.Frame()
	wantTableFocus(t, tt, "")
	tt.Key(0, ui.KeySpace)
	wantNoPreview(t, tt)
	if a.activity.previewOpen {
		t.Error("preview open with nothing selected")
	}
}

// Spec "Holding Space": the repeats neither close the preview nor open
// it again.
func TestPreviewHoldingSpace(t *testing.T) {
	f := newFake()
	_, tt, _ := newPreviewTester(t, f)
	click(t, tt, "image.png")
	tt.KeyDown(ui.KeySpace)
	wantPreview(t, tt, "image.png")
	for range 5 {
		tt.KeyRepeat(0, ui.KeySpace)
		wantPreview(t, tt, "image.png")
	}
	tt.KeyUp(ui.KeySpace)
	wantPreview(t, tt, "image.png")
	tt.Key(0, ui.KeySpace)
	wantNoPreview(t, tt)
	// Held again on the table, the repeats don't open it again.
	tt.KeyDown(ui.KeySpace)
	wantPreview(t, tt, "image.png")
	tt.KeyUp(ui.KeySpace)
	tt.KeyDown(ui.KeySpace)
	wantNoPreview(t, tt)
	tt.KeyRepeat(0, ui.KeySpace)
	tt.KeyRepeat(0, ui.KeySpace)
	wantNoPreview(t, tt)
	tt.KeyUp(ui.KeySpace)
}

// Specs "Next entry", "Browse onto a file entry" and "Edge of the page".
func TestPreviewBrowse(t *testing.T) {
	f := newFake()
	a, tt, ps := newPreviewTester(t, f)
	click(t, tt, "image.png")
	tt.Key(0, ui.KeySpace)
	ps.load(t, tt)
	tt.Key(0, ui.KeyDown)
	wantPreview(t, tt, "text.txt")
	if a.activity.selected != 1 {
		t.Fatalf("selected %d, want the second row", a.activity.selected)
	}
	ps.load(t, tt)
	if text, _ := previewedText(tt, "text.txt"); text != previewText {
		t.Errorf("preview shows %q", text)
	}

	tt.Key(0, ui.KeyDown)
	wantPreview(t, tt, "report.pdf")
	hasText(t, tt, "Files have no preview")
	hasText(t, tt, "4.2 MB")
	tt.KeyRepeat(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	wantPreview(t, tt, "report.pdf")
	if a.activity.selected != 2 {
		t.Errorf("Down past the last row selected %d", a.activity.selected)
	}

	tt.Key(0, ui.KeyUp)
	tt.KeyRepeat(0, ui.KeyUp)
	tt.KeyRepeat(0, ui.KeyUp)
	wantPreview(t, tt, "image.png")
	if a.activity.selected != 0 {
		t.Errorf("Up past the first row selected %d", a.activity.selected)
	}
	ps.load(t, tt)
	if _, ok := tt.AccessByRole(ui.RoleImage, "image.png"); !ok {
		t.Error("back on image.png, no image")
	}
}

// noteEntries are n text entries, note-0.txt first, whose previews say
// "content of note-N".
func noteEntries(f *fakeCtrl, ps *previewStub, n int) {
	f.edit(func(s *core.State) {
		s.History = nil
		for i := range n {
			s.History = append(s.History, store.HistoryEntry{
				ID: int64(10 + i), OriginalName: fmt.Sprintf("note-%d.txt", i), CapturedAt: now,
				Kind: store.KindText, Status: store.StatusSkipped, LocalPath: fmt.Sprintf("/staging/n%d.txt", i), Staged: true,
			})
		}
		s.HistoryTotal = n
	})
	for i := range n {
		text := fmt.Sprintf("content of note-%d", i)
		ps.set(int64(10+i), previewContent{text: ui.NewTextBuffer(text), size: int64(len(text))})
	}
}

// Spec "Fast browsing": reads finished out of order never show an
// earlier row's content.
func TestPreviewFastBrowsing(t *testing.T) {
	f := newFake()
	a, tt, ps := newPreviewTester(t, f)
	noteEntries(f, ps, 5)
	tt.Frame()
	ps.holdReads(10, 11, 12, 13)
	click(t, tt, "note-0.txt")
	tt.Key(0, ui.KeySpace)
	for range 3 {
		tt.Key(0, ui.KeyDown)
	}
	wantPreview(t, tt, "note-3.txt")
	if a.activity.selected != 3 {
		t.Fatalf("selected %d", a.activity.selected)
	}
	check := func(final bool) {
		t.Helper()
		wantPreview(t, tt, "note-3.txt")
		text, ok := previewedText(tt, "note-3.txt")
		switch {
		case final && text != "content of note-3":
			t.Fatalf("preview shows %q (%v), want note-3's content", text, ok)
		case !final && (ok || !tt.HasText("Loading…")):
			t.Fatalf("preview shows %q while note-3 loads", text)
		}
	}
	for _, id := range []int64{11, 10} {
		ps.release(id)
		ps.load(t, tt)
		check(false)
	}
	ps.release(13)
	ps.load(t, tt)
	check(true)
	ps.release(12)
	ps.load(t, tt)
	check(true)
}

// Specs "Space closes" and "Escape closes", and the Close button and a
// click outside the dialog: the table gets the focus back on the row
// previewed, after browsing too.
func TestPreviewClose(t *testing.T) {
	for _, how := range []string{"Space", "Escape", "Escape on Close reached by Tab", "Close button", "outside click"} {
		t.Run(how, func(t *testing.T) {
			f := newFake()
			a, tt, ps := newPreviewTester(t, f)
			click(t, tt, "image.png")
			tt.Key(0, ui.KeySpace)
			ps.load(t, tt)
			tt.Key(0, ui.KeyDown)
			ps.load(t, tt)
			wantPreview(t, tt, "text.txt")
			switch how {
			case "Space":
				tt.Key(0, ui.KeySpace)
			case "Escape":
				tt.Key(0, ui.KeyEscape)
			case "Escape on Close reached by Tab":
				tt.Key(0, ui.KeyTab)
				tt.Key(ui.Shift, ui.KeyTab)
				if !tt.Focused("Close preview") {
					t.Fatal("Shift+Tab did not come back to Close preview")
				}
				tt.Advance(3 * time.Second)
				tt.Key(0, ui.KeyEscape)
			case "Close button":
				click(t, tt, "Close preview")
			case "outside click":
				tt.ClickAt(5, 5)
			}
			wantNoPreview(t, tt)
			wantTableFocus(t, tt, "text.txt")
			if a.preview.has || a.preview.wants {
				t.Error("closing kept the preview's content")
			}
			// It opens again on the row it was closed on.
			tt.Key(0, ui.KeySpace)
			wantPreview(t, tt, "text.txt")
		})
	}
}

// Specs "Keyboard-only copy" and "Select and copy", and browsing from the
// text with Up and Down.
func TestPreviewTextKeyboard(t *testing.T) {
	f := newFake()
	a, tt, ps := newPreviewTester(t, f)
	click(t, tt, "text.txt")
	tt.Key(0, ui.KeySpace)
	ps.load(t, tt)
	if !tt.Focused("Close preview") {
		t.Fatal("Close preview doesn't start with the focus")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("text.txt") {
		t.Fatal("Tab did not reach the text")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != previewText {
		t.Errorf("Ctrl+A, Ctrl+C copied %q", got)
	}
	// Dragged across from its left edge, the first line.
	tt.SetClipboard("")
	r, _ := tt.AccessByRole(ui.RoleTextField, "text.txt")
	x, y := r.Bounds.X+2, r.Bounds.Y+18
	tt.Press(x, y)
	tt.Move(r.Bounds.X+r.Bounds.W-20, y)
	tt.Release(r.Bounds.X+r.Bounds.W-20, y)
	tt.Key(ui.Cmd, ui.KeyC)
	if got := tt.Clipboard(); got != "first line" {
		t.Errorf("selecting the first line copied %q", got)
	}
	wantPreview(t, tt, "text.txt")

	tt.Key(0, ui.KeyUp)
	wantPreview(t, tt, "image.png")
	ps.load(t, tt)
	tt.Key(0, ui.KeyDown)
	ps.load(t, tt)
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("text.txt") {
		t.Fatal("Tab did not reach the text again")
	}
	tt.Key(0, ui.KeyDown)
	wantPreview(t, tt, "report.pdf")
	if a.activity.selected != 2 {
		t.Errorf("selected %d", a.activity.selected)
	}
	// The text went with its entry: the focus is on Close, and the keys
	// still browse.
	tt.Key(0, ui.KeyUp)
	wantPreview(t, tt, "text.txt")
}

// Specs "Entry leaves the page" and "New capture while previewing".
func TestPreviewFollowsHistory(t *testing.T) {
	t.Run("deleted", func(t *testing.T) {
		f := newFake()
		_, tt, ps := newPreviewTester(t, f)
		click(t, tt, "text.txt")
		tt.Key(0, ui.KeySpace)
		ps.load(t, tt)
		f.edit(func(s *core.State) {
			s.History = []store.HistoryEntry{s.History[0], s.History[2]}
			s.HistoryTotal = 2
		})
		tt.Frame()
		wantNoPreview(t, tt)
		wantTableFocus(t, tt, "")
	})
	t.Run("pushed off the page", func(t *testing.T) {
		f := newFake()
		a, tt, ps := newPreviewTester(t, f)
		noteEntries(f, ps, core.HistoryPageSize)
		tt.Frame()
		click(t, tt, "note-0.txt")
		tt.Key(0, ui.KeyEnd)
		tt.Key(0, ui.KeySpace)
		last := fmt.Sprintf("note-%d.txt", core.HistoryPageSize-1)
		wantPreview(t, tt, last)
		ps.load(t, tt)
		f.edit(func(s *core.State) {
			e := store.HistoryEntry{ID: 99, OriginalName: "arrival.txt", Kind: store.KindText, Status: store.StatusQueued, CapturedAt: now}
			s.History = append([]store.HistoryEntry{e}, s.History[:core.HistoryPageSize-1]...)
			s.HistoryTotal++
			s.HistoryArrival++
		})
		tt.Frame()
		wantNoPreview(t, tt)
		wantTableFocus(t, tt, "")
		if a.activity.previewOpen {
			t.Error("preview still open")
		}
	})
	t.Run("new capture above", func(t *testing.T) {
		f := newFake()
		a, tt, ps := newPreviewTester(t, f)
		click(t, tt, "text.txt")
		tt.Key(0, ui.KeySpace)
		ps.load(t, tt)
		reads := ps.readCount()
		f.edit(func(s *core.State) {
			e := store.HistoryEntry{ID: 99, OriginalName: "arrival.png", Kind: store.KindImage, Status: store.StatusQueued, CapturedAt: now}
			s.History = append([]store.HistoryEntry{e}, s.History...)
			s.HistoryTotal++
			s.HistoryArrival++
		})
		tt.Frame()
		wantPreview(t, tt, "text.txt")
		if text, _ := previewedText(tt, "text.txt"); text != previewText {
			t.Errorf("preview shows %q", text)
		}
		if a.activity.selected != 2 || ps.readCount() != reads {
			t.Errorf("selected %d, %d reads, want row 2 and no new read", a.activity.selected, ps.readCount()-reads)
		}
	})
	t.Run("page changed", func(t *testing.T) {
		f := newFake()
		_, tt, ps := newPreviewTester(t, f)
		click(t, tt, "text.txt")
		tt.Key(0, ui.KeySpace)
		ps.load(t, tt)
		f.edit(func(s *core.State) {
			s.Filter.Offset = core.HistoryPageSize
			s.History = []store.HistoryEntry{{ID: 50, OriginalName: "older.txt", Kind: store.KindText, CapturedAt: now}}
			s.HistoryTotal = core.HistoryPageSize + 1
		})
		tt.Frame()
		wantNoPreview(t, tt)
		wantTableFocus(t, tt, "")
	})
}

// Specs "Preview from the menu" and "No Preview for files".
func TestPreviewFromMenu(t *testing.T) {
	f := newFake()
	a, tt, ps := newPreviewTester(t, f)
	click(t, tt, "image.png")
	if err := tt.RightClick("text.txt"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); len(menu) == 0 || menu[0] != "Preview" {
		t.Fatalf("text menu %q, want Preview first", menu)
	}
	if err := tt.ChooseMenuItem("Preview"); err != nil {
		t.Fatal(err)
	}
	wantPreview(t, tt, "text.txt")
	if a.activity.selected != 1 {
		t.Errorf("selected %d, want text.txt's row", a.activity.selected)
	}
	ps.load(t, tt)
	if text, _ := previewedText(tt, "text.txt"); text != previewText {
		t.Errorf("preview shows %q", text)
	}
	tt.Key(0, ui.KeyEscape)
	wantTableFocus(t, tt, "text.txt")

	if err := tt.RightClick("image.png"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); !slices.Equal(menu, []string{"Preview", "Retry", "Delete"}) {
		t.Errorf("image menu %q", menu)
	}
	tt.CloseMenu()
	if err := tt.RightClick("report.pdf"); err != nil {
		t.Fatal(err)
	}
	if menu := tt.Menu(); slices.Contains(menu, "Preview") {
		t.Errorf("file menu %q has Preview", menu)
	}
	tt.CloseMenu()
}
