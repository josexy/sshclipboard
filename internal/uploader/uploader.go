package uploader

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"github.com/pkg/sftp"

	"github.com/josexy/sshclipboard/internal/sshconn"
)

// Conns gives jobs their profile's connection. *sshconn.Pool implements
// it.
type Conns interface {
	Get(ctx context.Context, t sshconn.Target) (*sshconn.Conn, error)
	Discard(c *sshconn.Conn)
}

// EventKind is what happened to a job.
type EventKind int

const (
	Started   EventKind = iota + 1 // an attempt started (Attempt is its number)
	Progress                       // bytes sent (Sent, Total, Speed)
	Done                           // uploaded to RemotePath
	Skipped                        // the skip policy found RemotePath taken
	Failed                         // gave up (Err, Attempt)
	Cancelled                      // the user cancelled it
	Parked                         // the profile's queue waits for host key trust (Err is the *sshconn.HostKeyError)
)

// Event reports a job's progress.
type Event struct {
	Kind       EventKind
	Job        Job
	Attempt    int
	Sent       int64
	Total      int64
	Speed      float64 // bytes per second
	RemotePath string
	Err        error
}

// Options configure an Uploader. Zero values take the defaults.
type Options struct {
	Workers          int             // per profile; default 2
	Retries          int             // after the first attempt; default 3
	Backoff          []time.Duration // before each retry; default 1, 2, 4 s
	Jitter           func(time.Duration) time.Duration
	ProgressInterval time.Duration // default 250 ms (4 per second)
	// OnEvent hears every event, from the uploader's goroutines, in order
	// per job. It must not block or call the Uploader: it may run while
	// the Uploader holds its lock.
	OnEvent func(Event)
	// Open opens a job's source; nil opens the file.
	Open func(path string) (io.ReadCloser, int64, error)
}

// Uploader runs per-profile FIFO queues of jobs.
type Uploader struct {
	conns Conns
	opts  Options
	open  source

	mu     sync.Mutex
	queues map[int64]*queue
	jobs   map[int64]*jobState // by history ID
	seq    int64
	closed bool
	wg     sync.WaitGroup
}

type queue struct {
	pending []*jobState // in capture order
	running int
	parked  bool
}

type jobState struct {
	job     Job
	seq     int64
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
	attempt int
	state   attemptState
}

// New returns an Uploader that gets connections from conns.
func New(conns Conns, opts Options) *Uploader {
	if opts.Workers <= 0 {
		opts.Workers = 2
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	} else if opts.Retries == 0 {
		opts.Retries = 3
	}
	if opts.Backoff == nil {
		opts.Backoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	}
	if opts.Jitter == nil {
		opts.Jitter = func(d time.Duration) time.Duration { return d + rand.N(d/4+1) }
	}
	if opts.ProgressInterval <= 0 {
		opts.ProgressInterval = 250 * time.Millisecond
	}
	if opts.OnEvent == nil {
		opts.OnEvent = func(Event) {}
	}
	u := &Uploader{conns: conns, opts: opts, queues: map[int64]*queue{}, jobs: map[int64]*jobState{}, open: openFile}
	if opts.Open != nil {
		u.open = opts.Open
	}
	return u
}

// NoRetries is Options.Retries for no retry at all.
const NoRetries = -1

func (u *Uploader) queue(id int64) *queue {
	q := u.queues[id]
	if q == nil {
		q = &queue{}
		u.queues[id] = q
	}
	return q
}

// Enqueue queues j at the end of its profile's queue.
func (u *Uploader) Enqueue(j Job) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	u.seq++
	js := &jobState{job: j, seq: u.seq, ctx: ctx, cancel: cancel}
	u.jobs[j.HistoryID] = js
	q := u.queue(j.Target.ProfileID)
	q.pending = append(q.pending, js)
	u.dispatch(j.Target.ProfileID)
}

// dispatch starts pending jobs of a profile while fewer than Workers run.
// u.mu is held.
func (u *Uploader) dispatch(profileID int64) {
	q := u.queue(profileID)
	for !q.parked && q.running < u.opts.Workers && len(q.pending) > 0 {
		js := q.pending[0]
		q.pending = q.pending[1:]
		q.running++
		js.running = true
		// The first attempt starts here, under the lock, so Started
		// events come in capture order.
		js.attempt++
		u.opts.OnEvent(Event{Kind: Started, Job: js.job, Attempt: js.attempt})
		u.wg.Add(1)
		go u.run(js)
	}
}

// Cancel cancels a queued or running job. It reports whether the job was
// known.
func (u *Uploader) Cancel(historyID int64) bool {
	u.mu.Lock()
	js := u.jobs[historyID]
	if js == nil {
		u.mu.Unlock()
		return false
	}
	if !js.running {
		q := u.queue(js.job.Target.ProfileID)
		for i, p := range q.pending {
			if p == js {
				q.pending = append(q.pending[:i:i], q.pending[i+1:]...)
				break
			}
		}
		delete(u.jobs, historyID)
		js.cancel()
		u.mu.Unlock()
		u.opts.OnEvent(Event{Kind: Cancelled, Job: js.job, Attempt: js.attempt, Err: context.Canceled})
		return true
	}
	u.mu.Unlock()
	js.cancel() // the running attempt stops and reports Cancelled
	return true
}

// Resume restarts a parked profile queue, after the user trusted the host
// key.
func (u *Uploader) Resume(profileID int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.queue(profileID).parked = false
	u.dispatch(profileID)
}

// Parked tells whether a profile's queue waits for host key trust.
func (u *Uploader) Parked(profileID int64) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.queue(profileID).parked
}

// Busy tells whether a profile has queued or running jobs.
func (u *Uploader) Busy(profileID int64) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	q := u.queues[profileID]
	return q != nil && (q.running > 0 || len(q.pending) > 0)
}

// InFlight returns how many jobs are queued or running, across profiles.
func (u *Uploader) InFlight() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.jobs)
}

// Shutdown cancels every job without reporting them, as on quit, and waits
// for running attempts to stop. Their entries stay queued or uploading in
// history and are marked interrupted at the next start.
func (u *Uploader) Shutdown() {
	u.mu.Lock()
	u.closed = true
	for _, js := range u.jobs {
		js.cancel()
	}
	u.mu.Unlock()
	u.wg.Wait()
}

// finish removes a job and starts the next. requeue puts it back at the
// head of its queue in capture order, as when the queue is parked.
func (u *Uploader) finish(js *jobState, requeue bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	id := js.job.Target.ProfileID
	q := u.queue(id)
	q.running--
	js.running = false
	if requeue && !u.closed {
		i := 0
		for i < len(q.pending) && q.pending[i].seq < js.seq {
			i++
		}
		q.pending = append(q.pending[:i], append([]*jobState{js}, q.pending[i:]...)...)
	} else {
		delete(u.jobs, js.job.HistoryID)
		js.cancel()
	}
	u.dispatch(id)
}

// run runs a job's attempts: retrying transient failures after a backoff
// with a fresh connection, failing permanent ones at once, and parking the
// queue on an unknown host key.
func (u *Uploader) run(js *jobState) {
	defer u.wg.Done()
	j := js.job
	ctx := js.ctx
	emit := u.opts.OnEvent
	for first := true; ; first = false {
		if !first {
			js.attempt++
			emit(Event{Kind: Started, Job: j, Attempt: js.attempt})
		}
		attempt := js.attempt
		final, conn, err := u.attempt(ctx, js)
		// Terminal events go out before the next job is dispatched, so
		// observers never see more than Workers transfers at once.
		end := func(ev Event, requeue bool) {
			emit(ev)
			u.finish(js, requeue)
		}
		switch {
		case err == nil:
			end(Event{Kind: Done, Job: j, Attempt: attempt, RemotePath: final}, false)
			return
		case errors.Is(err, ErrExists):
			end(Event{Kind: Skipped, Job: j, Attempt: attempt, RemotePath: final, Err: err}, false)
			return
		case ctx.Err() != nil:
			if u.isClosed() {
				u.finish(js, false)
				return
			}
			end(Event{Kind: Cancelled, Job: j, Attempt: attempt, Err: context.Canceled}, false)
			return
		}
		var hk *sshconn.HostKeyError
		if errors.As(err, &hk) && !hk.Changed() {
			// Wait for the user to trust the key; nothing is sent.
			js.attempt--
			u.mu.Lock()
			u.queue(j.Target.ProfileID).parked = true
			u.mu.Unlock()
			end(Event{Kind: Parked, Job: j, Err: hk}, true)
			return
		}
		if !Transient(err, conn) || attempt > u.opts.Retries {
			end(Event{Kind: Failed, Job: j, Attempt: attempt, Err: err}, false)
			return
		}
		// Reconnect before the retry.
		u.conns.Discard(conn)
		wait := u.opts.Jitter(u.opts.Backoff[min(attempt-1, len(u.opts.Backoff)-1)])
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			if u.isClosed() {
				u.finish(js, false)
				return
			}
			end(Event{Kind: Cancelled, Job: j, Attempt: attempt, Err: context.Canceled}, false)
			return
		}
	}
}

func (u *Uploader) isClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}

func (u *Uploader) attempt(ctx context.Context, js *jobState) (string, *sshconn.Conn, error) {
	conn, err := u.conns.Get(ctx, js.job.Target)
	if err != nil {
		return "", nil, err
	}
	j := js.job
	final, err := u.transfer(ctx, conn, j, &js.state, func(sent, total int64, speed float64) {
		u.opts.OnEvent(Event{Kind: Progress, Job: j, Attempt: js.attempt, Sent: sent, Total: total, Speed: speed})
	})
	return final, conn, err
}

// Transient tells whether an attempt's error is worth a retry: network
// errors (to the server or its proxy), timeouts, and dropped connections.
// Authentication and host key problems, a proxy that refuses the sign-in
// or whose password is missing, server refusals (permission denied, no
// such file), a missing source file, and a missing directory are
// permanent. conn is the attempt's connection, nil if it never connected.
func Transient(err error, conn *sshconn.Conn) bool {
	var (
		de   *sshconn.DialError
		se   *SourceError
		hk   *sshconn.HostKeyError
		need *sshconn.SecretNeededError
		ne   net.Error
	)
	switch {
	case err == nil, errors.As(err, &se), errors.As(err, &hk), errors.Is(err, sshconn.ErrRemoteDirMissing),
		errors.Is(err, ErrNoFreeName), errors.Is(err, context.Canceled):
		return false
	case errors.As(err, &de):
		return de.Step == sshconn.StepConnect || de.Step == sshconn.StepProxy &&
			!errors.Is(err, sshconn.ErrProxyAuth) && !errors.As(err, &need)
	case errors.Is(err, sftp.ErrSSHFxConnectionLost), errors.Is(err, sftp.ErrSSHFxNoConnection),
		errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.As(err, &ne):
		return true
	}
	// Whatever the server said, a connection that is gone makes it a
	// dropped connection.
	if conn != nil && !connAlive(conn) {
		return true
	}
	return !(errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist) || isStatus(err))
}

func isStatus(err error) bool {
	var st *sftp.StatusError
	return errors.As(err, &st)
}

// connAlive probes conn with a keepalive, waiting briefly.
func connAlive(c *sshconn.Conn) bool {
	if !c.Alive() {
		return false
	}
	reply := make(chan error, 1)
	go func() {
		_, _, err := c.SSH.SendRequest("keepalive@openssh.com", true, nil)
		reply <- err
	}()
	select {
	case err := <-reply:
		return err == nil
	case <-c.Dead():
		return false
	case <-time.After(2 * time.Second):
		return false
	}
}
