package core

import (
	"maps"
	"slices"
	"time"

	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
)

// HistoryPageSize is how many history rows the Activity view gets.
const HistoryPageSize = 25

// Progress is a running transfer's progress.
type Progress struct {
	Sent, Total int64
	Speed       float64 // bytes per second
}

// HostKeyPrompt asks the user to trust an unknown host key, or replace a
// changed one.
type HostKeyPrompt struct {
	ProfileID   int64
	ProfileName string
	Err         *sshconn.HostKeyError
}

// SecretPrompt asks for a password or passphrase the credential store
// could not provide.
type SecretPrompt struct {
	ProfileID   int64
	ProfileName string
	Kind        string // a secret.Kind: "password", "passphrase", or "proxy-password"
}

// State is what the UI shows: a snapshot the Controller replaces whenever
// something changes. Never modify it.
type State struct {
	Settings     store.Settings
	Profiles     []store.Profile
	Active       *store.Profile
	Conn         sshconn.Status
	ConnErr      error
	Queued       int
	Uploading    int
	Progress     map[int64]Progress
	ErrorBadge   bool
	LastError    string
	Tray         TrayState
	Rules        []store.Rule
	Naming       []store.Naming
	History      []store.HistoryEntry
	HistoryTotal int
	Filter       store.Filter
	Summary      store.Summary
	HostKey      *HostKeyPrompt
	SecretPrompt *SecretPrompt
	// HistoryArrival advances when a new capture appears on the first page of
	// the current history filter.
	HistoryArrival uint64
	// Notice is the path where an unreadable database was moved, shown
	// once.
	Notice string
	// Saved tells which secrets each profile has stored, for the form's
	// "saved" hint.
	Saved map[int64]SavedSecrets
}

// SavedSecrets tells which of a profile's secrets are stored.
type SavedSecrets struct {
	Password, Passphrase, ProxyPassword bool
}

// Monitoring tells whether automatic capture is on.
func (s *State) Monitoring() bool { return s.Settings.Monitoring }

// NamingOf returns the template of kind.
func (s *State) NamingOf(kind store.Kind) store.Naming {
	for _, n := range s.Naming {
		if n.Kind == kind {
			return n
		}
	}
	return store.Naming{Kind: kind}
}

// State returns the current snapshot. It is safe from any goroutine.
func (c *Controller) State() *State { return c.state.Load() }

// publish replaces the snapshot and asks the UI to redraw. It runs on the
// loop.
func (c *Controller) publish() {
	l := &c.l
	s := &State{
		Settings:       l.settings,
		Profiles:       slices.Clone(l.profiles),
		Active:         c.activeProfile(),
		Progress:       maps.Clone(l.progress),
		ErrorBadge:     l.errorBadge,
		LastError:      l.lastError,
		Rules:          slices.Clone(l.rules),
		Naming:         slices.Clone(l.naming),
		History:        l.history,
		HistoryTotal:   l.total,
		HistoryArrival: l.arrival,
		Filter:         l.filter,
		Summary:        l.summary,
		HostKey:        l.hostKey,
		SecretPrompt:   l.secretAsk,
		Notice:         l.notice,
		Saved:          maps.Clone(l.saved),
		Conn:           sshconn.StatusDisconnected,
	}
	if s.Active != nil {
		if ev, ok := l.conn[s.Active.ID]; ok {
			s.Conn, s.ConnErr = ev.Status, ev.Err
		}
	}
	for _, st := range l.active {
		if st == store.StatusUploading {
			s.Uploading++
		} else {
			s.Queued++
		}
	}
	s.Tray = DeriveTray(s.Settings.Monitoring, s.Uploading > 0, s.ErrorBadge || s.HostKey != nil)
	c.state.Store(s)
	c.o.Notify(func() {})
}

// refreshHistory reloads the Activity page and today's summary.
func (c *Controller) refreshHistory() {
	c.fail(c.loadHistory(c.l.filter))
}

func (c *Controller) refreshHistoryAfterCapture(captureID string) {
	f := c.l.filter
	f.Offset = 0
	rows, total, err := c.st.History.Query(ctx, f)
	if err != nil {
		c.fail(err)
		return
	}
	if slices.ContainsFunc(rows, func(row store.HistoryEntry) bool { return row.CaptureID == captureID }) {
		c.l.arrival++
	} else if c.l.filter.Offset != 0 {
		c.refreshHistory()
		return
	}
	c.setHistoryPage(f, rows, total)
}

// loadHistory keeps the published filter, rows and total in sync, even when
// entries have disappeared from the last page.
func (c *Controller) loadHistory(f store.Filter) error {
	rows, total, err := c.st.History.Query(ctx, f)
	if err != nil {
		return err
	}
	if f.Offset > 0 && f.Offset >= total {
		f.Offset = max(total-1, 0) / f.Limit * f.Limit
		rows, total, err = c.st.History.Query(ctx, f)
		if err != nil {
			return err
		}
	}
	c.setHistoryPage(f, rows, total)
	return nil
}

func (c *Controller) setHistoryPage(f store.Filter, rows []store.HistoryEntry, total int) {
	c.l.filter, c.l.history, c.l.total = f, rows, total
	now := c.o.Now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if sum, err := c.st.History.Summary(ctx, day, day.AddDate(0, 0, 1)); err == nil {
		c.l.summary = sum
	}
}
