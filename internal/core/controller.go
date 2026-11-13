package core

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/josexy/sshclipboard/internal/capture"
	"github.com/josexy/sshclipboard/internal/clipwatch"
	"github.com/josexy/sshclipboard/internal/rules"
	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/store"
	"github.com/josexy/sshclipboard/internal/uploader"
)

// ClipboardWriter puts text on the clipboard. mygo.Clipboard implements
// it in the app.
type ClipboardWriter interface {
	WriteText(text string)
}

// Notifier shows system notifications.
type Notifier interface {
	Notify(n Notice)
}

// Hooks apply settings to the desktop. Each may be nil.
type Hooks struct {
	// Appearance applies the theme source.
	Appearance func(store.Appearance)
	// StartAtLogin registers or unregisters the app at login.
	StartAtLogin func(on bool) error
	// Hotkey replaces the "Upload clipboard now" hotkey: it registers next
	// ("" for none) before unregistering prev, and fails, keeping prev,
	// when next is taken.
	Hotkey func(prev, next string) error
	// WindowVisible tells whether the main window is shown.
	WindowVisible func() bool
}

// Options are the Controller's services.
type Options struct {
	Store     *store.Store
	Secrets   *secret.Store
	Source    capture.Source
	Watcher   clipwatch.Watcher
	Clipboard ClipboardWriter
	Notifier  Notifier
	Hooks     Hooks
	// Notify runs fn on the UI thread and redraws: Window.Update in the
	// app. nil runs fn at once, on the Controller's goroutine.
	Notify func(fn func())
	// DataDir is the per-user data folder; the default staging folder is
	// its "staging".
	DataDir           string
	OpenSSHKnownHosts string
	DialAgent         func() (net.Conn, error)
	KeepaliveInterval time.Duration
	// Uploader and Capture tune those services in tests; their callbacks
	// are the Controller's.
	Uploader uploader.Options
	Capture  capture.Options
	Now      func() time.Time
}

// Controller owns the services, runs the capture → rules → naming →
// history → upload pipeline, and keeps the State the UI shows. Its state
// changes on one goroutine, its loop; its methods are safe from any
// goroutine and never block on I/O: they post work to the loop and report
// back through Options.Notify.
type Controller struct {
	o      Options
	st     *store.Store
	sec    *secret.Store
	sshcfg *sshconn.Config
	pool   *sshconn.Pool
	up     *uploader.Uploader
	cap    *capture.Capturer

	qmu      sync.Mutex
	qcond    *sync.Cond
	queue    []func()
	stopped  bool
	loopDone chan struct{}

	state atomic.Pointer[State]

	// Owned by the loop.
	l loopState
}

// loopState is the Controller's state, touched only by its loop.
type loopState struct {
	settings   store.Settings
	profiles   []store.Profile
	rules      []store.Rule
	ruleSet    *rules.Set
	naming     []store.Naming
	active     map[int64]store.Status // queued and uploading entries
	progress   map[int64]Progress
	conn       map[int64]sshconn.StatusEvent
	captures   map[int64]*pendingCapture // by history ID
	errorBadge bool
	lastError  string
	hostKey    *HostKeyPrompt
	secretAsk  *SecretPrompt
	filter     store.Filter
	history    []store.HistoryEntry
	total      int
	summary    store.Summary
	notice     string
	saved      map[int64]SavedSecrets
}

var ctx = context.Background()

// New builds a Controller over its services. Start runs it.
func New(o Options) *Controller {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Notify == nil {
		o.Notify = func(fn func()) { fn() }
	}
	c := &Controller{o: o, st: o.Store, sec: o.Secrets, loopDone: make(chan struct{})}
	c.qcond = sync.NewCond(&c.qmu)
	c.l = loopState{active: map[int64]store.Status{}, progress: map[int64]Progress{},
		conn: map[int64]sshconn.StatusEvent{}, captures: map[int64]*pendingCapture{},
		filter: store.Filter{Limit: HistoryPageSize}}
	c.sshcfg = &sshconn.Config{
		Secrets: o.Secrets, KnownHosts: o.Store.KnownHosts, OpenSSHKnownHosts: o.OpenSSHKnownHosts,
		DialAgent: o.DialAgent, KeepaliveInterval: o.KeepaliveInterval,
		OnStatus: func(ev sshconn.StatusEvent) { c.post(func() { c.onStatus(ev) }) },
	}
	c.pool = sshconn.NewPool(c.sshcfg)
	uo := o.Uploader
	uo.OnEvent = func(ev uploader.Event) { c.post(func() { c.onUpload(ev) }) }
	c.up = uploader.New(c.pool, uo)
	co := o.Capture
	co.Source, co.Watcher = o.Source, o.Watcher
	co.StagingDir = c.stagingDir
	co.Mode = func() store.CaptureMode { return c.State().Settings.CaptureMode }
	co.Confidential = func() bool { return c.State().Settings.ConfidentialProtection }
	co.OnItem = func(it capture.Item) { c.post(func() { c.onItem(it) }) }
	co.OnError = func(err error) { c.post(func() { c.fail(err) }) }
	co.Now = o.Now
	c.cap = capture.New(co)
	c.state.Store(&State{})
	return c
}

// Start loads the configuration, recovers from the last run, and starts
// watching the clipboard. It returns the first error loading the
// configuration.
func (c *Controller) Start() error {
	if err := c.reload(); err != nil {
		return err
	}
	c.publish() // the capture settings and staging folder come from the state
	if c.st.RecoveredFrom != "" {
		c.l.notice = c.st.RecoveredFrom
	}
	// Entries the last run left queued or uploading can't finish now.
	if _, err := c.st.History.MarkInterrupted(ctx, c.o.Now()); err != nil {
		return err
	}
	c.prune()
	c.sweepOrphans()
	c.refreshHistory()
	c.cap.SetPaused(!c.l.settings.Monitoring)
	c.publish()
	go c.loop()
	go c.cap.Run()
	return nil
}

// Close stops capturing and uploading. Unfinished entries stay queued or
// uploading in history and are marked interrupted at the next start.
func (c *Controller) Close() {
	c.cap.Close()
	if c.o.Watcher != nil {
		c.o.Watcher.Close()
	}
	c.up.Shutdown()
	c.pool.Shutdown()
	c.qmu.Lock()
	c.stopped = true
	c.qmu.Unlock()
	c.qcond.Broadcast()
	<-c.loopDone
}

// InFlight returns how many uploads are queued or running, for the quit
// confirmation.
func (c *Controller) InFlight() int { return c.up.InFlight() }

// post queues fn on the loop. It never blocks.
func (c *Controller) post(fn func()) {
	c.qmu.Lock()
	if !c.stopped {
		c.queue = append(c.queue, fn)
	}
	c.qmu.Unlock()
	c.qcond.Signal()
}

func (c *Controller) loop() {
	defer close(c.loopDone)
	for {
		c.qmu.Lock()
		for len(c.queue) == 0 && !c.stopped {
			c.qcond.Wait()
		}
		if c.stopped && len(c.queue) == 0 {
			c.qmu.Unlock()
			return
		}
		batch := c.queue
		c.queue = nil
		c.qmu.Unlock()
		for _, fn := range batch {
			fn()
		}
		c.publish()
	}
}

// do runs fn on the loop and reports its error to done on the UI thread,
// after the state shows fn's changes.
func (c *Controller) do(fn func() error, done func(error)) {
	c.post(func() {
		err := fn()
		if err != nil && !isUserError(err) {
			c.l.lastError = err.Error()
		}
		if done != nil {
			c.publish()
			c.o.Notify(func() { done(err) })
		}
	})
}

// isUserError tells whether err is a validation problem the form shows,
// rather than something that went wrong.
func isUserError(err error) bool {
	var fe FieldErrors
	var re *rules.RuleError
	return errors.As(err, &fe) || errors.As(err, &re) || errors.Is(err, ErrHotkeyTaken)
}

// fail records an unexpected error for the UI.
func (c *Controller) fail(err error) {
	if err != nil {
		c.l.lastError = err.Error()
	}
}

// reload reads the configuration from the store.
func (c *Controller) reload() error {
	s, err := c.st.Settings.Load(ctx)
	if err != nil {
		return err
	}
	ps, err := c.st.Profiles.List(ctx)
	if err != nil {
		return err
	}
	rs, err := c.st.Rules.List(ctx)
	if err != nil {
		return err
	}
	ns, err := c.st.Naming.All(ctx)
	if err != nil {
		return err
	}
	c.l.settings, c.l.profiles, c.l.rules, c.l.naming = s, ps, rs, ns
	c.l.saved = make(map[int64]SavedSecrets, len(ps))
	for _, p := range ps {
		c.l.saved[p.ID] = SavedSecrets{Password: c.sec.Has(p.ID, secret.Password),
			Passphrase: c.sec.Has(p.ID, secret.Passphrase), ProxyPassword: c.sec.Has(p.ID, secret.ProxyPassword)}
	}
	set, errs := rules.Compile(rs, s.DefaultAction)
	if len(errs) > 0 {
		c.fail(errors.Join(errs...))
	}
	c.l.ruleSet = set
	return nil
}

// stagingDir returns the staging folder: the setting, or "staging" in
// the data folder.
func (c *Controller) stagingDir() string {
	if d := c.State().Settings.StagingDir; d != "" {
		return d
	}
	return filepath.Join(c.o.DataDir, "staging")
}

// activeProfile returns the active profile, or nil.
func (c *Controller) activeProfile() *store.Profile {
	return c.profile(c.l.settings.ActiveProfile)
}

func (c *Controller) profile(id int64) *store.Profile {
	for i := range c.l.profiles {
		if c.l.profiles[i].ID == id {
			p := c.l.profiles[i]
			return &p
		}
	}
	return nil
}
