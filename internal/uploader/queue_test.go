package uploader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
)

// 7.4 Retry.

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// flaky fails the first n Gets with a timeout and counts Discards.
type flaky struct {
	Conns
	n        atomic.Int32
	discards atomic.Int32
}

func (f *flaky) Get(ctx context.Context, t sshconn.Target) (*sshconn.Conn, error) {
	if f.n.Add(-1) >= 0 {
		return nil, &sshconn.DialError{Step: sshconn.StepConnect, Err: &net.OpError{Op: "dial", Net: "tcp", Err: timeoutErr{}}}
	}
	return f.Conns.Get(ctx, t)
}

func (f *flaky) Discard(c *sshconn.Conn) { f.discards.Add(1); f.Conns.Discard(c) }

// Spec "Brief network blip": uploaded with 2 attempts.
func TestRetryAfterTimeout(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	fl := &flaky{Conns: e.pool}
	fl.n.Store(1)
	e.u.conns = fl
	e.u.Enqueue(e.job(1, localFile(t, "a.txt", []byte("a")), "~", "a.txt"))
	ev := e.rec.final(t, 1)
	if ev.Kind != Done || ev.Attempt != 2 {
		t.Errorf("%+v", ev)
	}
	if fl.discards.Load() != 1 {
		t.Errorf("reconnected %d times before the retry", fl.discards.Load())
	}
	starts := 0
	for _, k := range e.rec.kinds(1) {
		if k == Started {
			starts++
		}
	}
	if starts != 2 {
		t.Errorf("%d attempts started", starts)
	}
}

// Spec "Permission denied": fails at once with the server's message.
func TestPermissionDeniedNotRetried(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	e.s.DenyCreate(true)
	os.MkdirAll(e.s.Local("/home/alice/in"), 0o755)
	e.u.Enqueue(e.job(1, localFile(t, "a.txt", []byte("a")), "~/in", "a.txt"))
	ev := e.rec.final(t, 1)
	if ev.Kind != Failed || ev.Attempt != 1 || !strings.Contains(ev.Err.Error(), "permission denied") {
		t.Errorf("%+v", ev)
	}
}

// upload-history spec "Failure recorded": 4 attempts, "connection refused".
func TestConnectionRefusedFailsAfterFourAttempts(t *testing.T) {
	e := newEnv(t, Options{}, nil)
	e.s.Close()
	e.u.Enqueue(e.job(1, localFile(t, "a.txt", []byte("a")), "~", "a.txt"))
	ev := e.rec.final(t, 1)
	if ev.Kind != Failed || ev.Attempt != 4 || !sshtest.IsConnectionRefused(ev.Err) {
		t.Errorf("%+v", ev)
	}
}

func TestDefaultBackoff(t *testing.T) {
	u := New(nil, Options{})
	if len(u.opts.Backoff) != 3 || u.opts.Backoff[0] != time.Second || u.opts.Backoff[2] != 4*time.Second || u.opts.Retries != 3 {
		t.Errorf("backoff %v retries %d", u.opts.Backoff, u.opts.Retries)
	}
	for range 100 {
		if d := u.opts.Jitter(2 * time.Second); d < 2*time.Second || d > 2500*time.Millisecond {
			t.Fatalf("jitter %v", d)
		}
	}
}

func TestTransientClassification(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&sshconn.DialError{Step: sshconn.StepConnect, Err: errors.New("refused")}, true},
		{&sshconn.DialError{Step: sshconn.StepAuth, Err: errors.New("bad")}, false},
		{&sshconn.DialError{Step: sshconn.StepHostKey, Err: &sshconn.HostKeyError{}}, false},
		{&sshconn.DialError{Step: sshconn.StepProxy, Err: errors.New("SOCKS5 proxy p:1080: connection refused")}, true},
		{&sshconn.DialError{Step: sshconn.StepProxy, Err: fmt.Errorf("SOCKS5 proxy p:1080: %w: x", sshconn.ErrProxyAuth)}, false},
		{&sshconn.DialError{Step: sshconn.StepProxy, Err: &sshconn.SecretNeededError{Kind: secret.ProxyPassword}}, false},
		{&SourceError{ErrSourceNotFound}, false},
		{sshconn.ErrRemoteDirMissing, false},
		{&os.PathError{Op: "open", Path: "/x", Err: os.ErrPermission}, false},
		{io.ErrUnexpectedEOF, true},
		{sftp.ErrSSHFxConnectionLost, true},
		{&net.OpError{Op: "read", Err: timeoutErr{}}, true},
		{context.Canceled, false},
	} {
		if got := Transient(c.err, nil); got != c.want {
			t.Errorf("Transient(%v) = %v", c.err, got)
		}
	}
}

// 7.5 Queues.

// Spec "Burst of captures": capture order, at most two at once.
func TestFIFOAndConcurrencyCap(t *testing.T) {
	var running, peak atomic.Int32
	var mu sync.Mutex
	var order []int64
	e := newEnv(t, Options{Open: slowSource(256*1024, 10*time.Millisecond)}, nil)
	inner := e.u.opts.OnEvent
	e.u.opts.OnEvent = func(ev Event) {
		switch ev.Kind {
		case Started:
			mu.Lock()
			order = append(order, ev.Job.HistoryID)
			mu.Unlock()
			if n := running.Add(1); n > peak.Load() {
				peak.Store(n)
			}
		case Done, Failed:
			running.Add(-1)
		}
		inner(ev)
	}
	for id := int64(1); id <= 5; id++ {
		e.u.Enqueue(e.job(id, "x", "~", "f"+string(rune('0'+id))))
	}
	if e.u.InFlight() != 5 || !e.u.Busy(1) {
		t.Errorf("in flight %d", e.u.InFlight())
	}
	for id := int64(1); id <= 5; id++ {
		if ev := e.rec.final(t, id); ev.Kind != Done {
			t.Errorf("job %d: %+v", id, ev)
		}
	}
	if peak.Load() != 2 {
		t.Errorf("peak concurrency %d, want 2", peak.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	for i, id := range order {
		if id != int64(i+1) {
			t.Errorf("start order %v", order)
			break
		}
	}
	if e.u.InFlight() != 0 || e.u.Busy(1) {
		t.Error("jobs left after finishing")
	}
}

// Spec "Cancel a large transfer": stops within a second, no file left.
func TestCancelRunningJob(t *testing.T) {
	e := newEnv(t, Options{Open: slowSource(16*1024*1024, 20*time.Millisecond), ProgressInterval: time.Millisecond}, nil)
	e.u.Enqueue(e.job(1, "x", "~/in", "big.bin"))
	// Wait until bytes flow.
	deadline := time.Now().Add(10 * time.Second)
	for {
		sent := int64(0)
		for _, ev := range e.rec.all() {
			if ev.Kind == Progress {
				sent = ev.Sent
			}
		}
		if sent > 128*1024 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no progress")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	if !e.u.Cancel(1) {
		t.Fatal("unknown job")
	}
	ev := e.rec.final(t, 1)
	if ev.Kind != Cancelled || time.Since(start) > time.Second {
		t.Errorf("%+v after %v", ev, time.Since(start))
	}
	if p := e.parts("/home/alice/in"); len(p) != 0 {
		t.Errorf("temporary files left: %v", p)
	}
	if _, err := os.Stat(e.s.Local("/home/alice/in/big.bin")); err == nil {
		t.Error("a cancelled entry has its final name")
	}
}

func TestCancelQueuedJob(t *testing.T) {
	e := newEnv(t, Options{Open: slowSource(256*1024, 10*time.Millisecond)}, nil)
	for id := int64(1); id <= 3; id++ {
		e.u.Enqueue(e.job(id, "x", "~", "f"+string(rune('0'+id))))
	}
	e.u.Cancel(3)
	if ev := e.rec.final(t, 3); ev.Kind != Cancelled {
		t.Errorf("%+v", ev)
	}
	e.rec.final(t, 1)
	e.rec.final(t, 2)
	for _, k := range e.rec.kinds(3) {
		if k == Started {
			t.Error("a cancelled queued job started")
		}
	}
	if e.u.Cancel(99) {
		t.Error("cancelled an unknown job")
	}
}

// Spec "Unattended first connection": the queue waits, nothing is sent
// until the key is trusted, then it resumes in order.
func TestParkOnUnknownHostKeyAndResume(t *testing.T) {
	k := &known{}
	e := newEnv(t, Options{}, k)
	for id := int64(1); id <= 3; id++ {
		e.u.Enqueue(e.job(id, localFile(t, "a", []byte{byte('0' + id)}), "~", "f"+string(rune('0'+id))))
	}
	ev := e.rec.final(t, 1)
	var hk *sshconn.HostKeyError
	if ev.Kind != Parked || !errors.As(ev.Err, &hk) || hk.Changed() {
		t.Fatalf("%+v", ev)
	}
	time.Sleep(50 * time.Millisecond)
	if !e.u.Parked(1) || !e.u.Busy(1) || e.u.InFlight() != 3 || e.s.Written() != 0 {
		t.Errorf("parked %v busy %v inflight %d written %d", e.u.Parked(1), e.u.Busy(1), e.u.InFlight(), e.s.Written())
	}

	k.Put(hk.KnownHost()) // Trust
	start := len(e.rec.all())
	e.u.Resume(1)
	var done []int64
	deadline := time.Now().Add(10 * time.Second)
	for len(done) < 3 && time.Now().Before(deadline) {
		done = done[:0]
		for _, ev := range e.rec.all()[start:] {
			if ev.Kind == Done {
				done = append(done, ev.Job.HistoryID)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(done) != 3 {
		t.Fatalf("done %v", done)
	}
	for id := int64(1); id <= 3; id++ {
		if got := e.remote(t, "/home/alice/f"+string(rune('0'+id))); string(got) != string(rune('0'+id)) {
			t.Errorf("file %d: %q", id, got)
		}
	}
}

// Spec "Changed host key": uploads fail with "host key changed".
func TestChangedHostKeyFails(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	k := trust(s)
	e := newEnv(t, Options{}, k)
	e.tg.Port = s.Port // a server whose key k trusts differently below
	k.Put(sshconn.KnownHostOf(s.Host, s.Port, sshtest.NewSigner(t).PublicKey()))
	e.u.Enqueue(e.job(1, localFile(t, "a", []byte("a")), "~", "a"))
	ev := e.rec.final(t, 1)
	var hk *sshconn.HostKeyError
	if ev.Kind != Failed || ev.Attempt != 1 || !errors.As(ev.Err, &hk) || !hk.Changed() ||
		!strings.Contains(ev.Err.Error(), "host key changed") {
		t.Errorf("%+v", ev)
	}
}
