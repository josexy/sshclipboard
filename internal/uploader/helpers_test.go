package uploader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

var bg = context.Background()

type pw struct{}

func (pw) Get(int64, secret.Kind) (string, error) { return "secret", nil }

type known struct {
	mu sync.Mutex
	m  map[string]store.KnownHost
}

func (k *known) Get(_ context.Context, host string, port int) (store.KnownHost, bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	kh, ok := k.m[fmt.Sprintf("%s:%d", host, port)]
	return kh, ok, nil
}

func (k *known) Put(kh store.KnownHost) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]store.KnownHost{}
	}
	k.m[fmt.Sprintf("%s:%d", kh.Host, kh.Port)] = kh
}

func trust(s *sshtest.Server) *known {
	k := &known{}
	k.Put(sshconn.KnownHostOf(s.Host, s.Port, s.HostKey()))
	return k
}

// recorder collects events and lets tests wait for them.
type recorder struct {
	mu   sync.Mutex
	evs  []Event
	cond *sync.Cond
}

func newRecorder() *recorder {
	r := &recorder{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *recorder) add(e Event) {
	r.mu.Lock()
	r.evs = append(r.evs, e)
	r.mu.Unlock()
	r.cond.Broadcast()
}

// final waits for a terminal event (Done, Skipped, Failed, Cancelled, or
// Parked) of a job.
func (r *recorder) final(t *testing.T, id int64) Event {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	go func() { time.Sleep(20 * time.Second); r.cond.Broadcast() }()
	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		for _, e := range r.evs {
			if e.Job.HistoryID == id && e.Kind != Started && e.Kind != Progress {
				return e
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d did not finish; events %v", id, r.kinds(id))
		}
		r.cond.Wait()
	}
}

func (r *recorder) kinds(id int64) []EventKind {
	var out []EventKind
	for _, e := range r.evs {
		if e.Job.HistoryID == id {
			out = append(out, e.Kind)
		}
	}
	return out
}

func (r *recorder) all() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.evs...)
}

type env struct {
	s    *sshtest.Server
	pool *sshconn.Pool
	rec  *recorder
	u    *Uploader
	tg   sshconn.Target
}

func newEnv(t *testing.T, opts Options, k sshconn.KnownHosts) *env {
	t.Helper()
	s := sshtest.Start(t, sshtest.Options{})
	if k == nil {
		k = trust(s)
	}
	pool := sshconn.NewPool(&sshconn.Config{Secrets: pw{}, KnownHosts: k})
	t.Cleanup(pool.Shutdown)
	rec := newRecorder()
	opts.OnEvent = rec.add
	if opts.Backoff == nil {
		opts.Backoff = []time.Duration{time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond}
	}
	if opts.Jitter == nil {
		opts.Jitter = func(d time.Duration) time.Duration { return d }
	}
	u := New(pool, opts)
	t.Cleanup(u.Shutdown)
	return &env{s: s, pool: pool, rec: rec, u: u, tg: sshconn.Target{ProfileID: 1, Name: "dev-box",
		Host: s.Host, Port: s.Port, User: "alice", AuthMethod: store.AuthPassword}}
}

// localFile writes a local source file.
func localFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (e *env) job(id int64, src, dir, name string) Job {
	return Job{HistoryID: id, CaptureID: "c", Target: e.tg, Source: src, Dir: dir, Name: name,
		Policy: store.ConflictRename, CreateDirs: true, FileMode: -1}
}

func (e *env) remote(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(e.s.Local(p))
	if err != nil {
		t.Fatalf("remote %s: %v", p, err)
	}
	return b
}

// parts lists leftover temporary files in a remote directory.
func (e *env) parts(dir string) []string {
	entries, _ := os.ReadDir(e.s.Local(dir))
	var out []string
	for _, en := range entries {
		if filepath.Ext(en.Name()) == ".part" {
			out = append(out, en.Name())
		}
	}
	return out
}

// slowSource serves n bytes per path, delay per 32 KiB read.
func slowSource(n int, delay time.Duration) func(string) (io.ReadCloser, int64, error) {
	return func(string) (io.ReadCloser, int64, error) {
		return io.NopCloser(&slowReader{left: n, delay: delay}), int64(n), nil
	}
}

type slowReader struct {
	left  int
	delay time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.left == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	n := min(len(p), s.left, 32*1024)
	copy(p, bytes.Repeat([]byte{'x'}, n))
	s.left -= n
	return n, nil
}
