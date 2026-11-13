package sshconn

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/josexy/sshclipboard/internal/secret"
	"github.com/josexy/sshclipboard/internal/sshconn/sshtest"
	"github.com/josexy/sshclipboard/internal/store"
)

// events records status events.
type events struct {
	mu  sync.Mutex
	evs []StatusEvent
	ch  chan StatusEvent
}

func newEvents() *events { return &events{ch: make(chan StatusEvent, 64)} }

func (e *events) add(ev StatusEvent) {
	e.mu.Lock()
	e.evs = append(e.evs, ev)
	e.mu.Unlock()
	e.ch <- ev
}

// wait returns the next event with status s, within d.
func (e *events) wait(t *testing.T, s Status, d time.Duration) StatusEvent {
	t.Helper()
	deadline := time.After(d)
	for {
		select {
		case ev := <-e.ch:
			if ev.Status == s {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s event within %v; got %+v", s, d, e.all())
		}
	}
}

func (e *events) all() []Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []Status
	for _, ev := range e.evs {
		out = append(out, ev.Status)
	}
	return out
}

func newPool(t *testing.T, s *sshtest.Server, interval time.Duration) (*Pool, *events) {
	ev := newEvents()
	p := NewPool(&Config{Secrets: fakeSecrets{secret.Password: "secret"}, KnownHosts: trusting(s),
		KeepaliveInterval: interval, OnStatus: ev.add})
	t.Cleanup(p.Shutdown)
	return p, ev
}

func TestPoolLazyAndShared(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	p, ev := newPool(t, s, time.Hour)
	if s.Accepted() != 0 || p.Status(1) != StatusDisconnected {
		t.Fatal("the pool dialed before first use")
	}
	tg := target(s, store.AuthPassword)
	c1, err := p.Get(bg, tg)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := p.Get(bg, tg)
	if err != nil || c1 != c2 || s.Accepted() != 1 {
		t.Errorf("second Get dialed again: %v, %d connections", err, s.Accepted())
	}
	if c1.Home != "/home/alice" || c1.SFTP == nil || p.Status(1) != StatusConnected {
		t.Errorf("conn %+v status %s", c1, p.Status(1))
	}
	if got := ev.all(); len(got) != 2 || got[0] != StatusConnecting || got[1] != StatusConnected {
		t.Errorf("events %v", got)
	}
}

// Spec "Server reboot": the connection drops, and the next use
// reconnects without user action.
func TestPoolReconnectsAfterServerDrop(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	p, ev := newPool(t, s, time.Hour)
	tg := target(s, store.AuthPassword)
	c1, err := p.Get(bg, tg)
	if err != nil {
		t.Fatal(err)
	}
	s.KillConnections()
	ev.wait(t, StatusDisconnected, 5*time.Second)
	if c1.Alive() || p.Status(1) != StatusDisconnected {
		t.Fatal("the lost connection still looks alive")
	}
	c2, err := p.Get(bg, tg)
	if err != nil {
		t.Fatal(err)
	}
	if c2 == c1 || s.Accepted() != 2 || p.Status(1) != StatusConnected {
		t.Errorf("no reconnect: accepted %d, status %s", s.Accepted(), p.Status(1))
	}
	if _, err := c2.SFTP.Stat("/home/alice"); err != nil {
		t.Errorf("the new connection does not work: %v", err)
	}
}

func TestPoolKeepaliveTwoMisses(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	p, ev := newPool(t, s, 40*time.Millisecond)
	tg := target(s, store.AuthPassword)
	c, err := p.Get(bg, tg)
	if err != nil {
		t.Fatal(err)
	}
	// Answered keepalives keep it open.
	time.Sleep(200 * time.Millisecond)
	if !c.Alive() {
		t.Fatal("a responsive connection was closed")
	}
	s.Stall(true)
	got := ev.wait(t, StatusDisconnected, 5*time.Second)
	if !errors.Is(got.Err, ErrNotResponding) || c.Alive() {
		t.Errorf("after two missed keepalives: %+v alive %v", got, c.Alive())
	}
	s.Stall(false)
	if _, err := p.Get(bg, tg); err != nil || s.Accepted() != 2 {
		t.Errorf("redial: %v, %d", err, s.Accepted())
	}
}

func TestPoolDialErrorStatus(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	p, ev := newPool(t, s, time.Hour)
	tg := target(s, store.AuthPassword)
	tg.User = "mallory"
	if _, err := p.Get(bg, tg); err == nil {
		t.Fatal("bad user connected")
	}
	if e := ev.wait(t, StatusError, time.Second); e.Err == nil || p.Status(1) != StatusError {
		t.Errorf("error event %+v", e)
	}
}

func TestPoolCloseIdleAndShutdown(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	p, _ := newPool(t, s, time.Hour)
	a, b := target(s, store.AuthPassword), target(s, store.AuthPassword)
	b.ProfileID = 2
	ca, _ := p.Get(bg, a)
	cb, _ := p.Get(bg, b)
	p.CloseIdle(func(id int64) bool { return id == 1 })
	if !ca.Alive() || cb.Alive() {
		t.Errorf("CloseIdle: active alive %v, idle alive %v", ca.Alive(), cb.Alive())
	}
	p.Shutdown()
	if ca.Alive() {
		t.Error("Shutdown left a connection open")
	}
	if _, err := p.Get(bg, a); !errors.Is(err, ErrPoolClosed) {
		t.Errorf("Get after Shutdown: %v", err)
	}
}

func TestPoolConcurrentGetDialsOnce(t *testing.T) {
	s := sshtest.Start(t, sshtest.Options{})
	p, _ := newPool(t, s, time.Hour)
	tg := target(s, store.AuthPassword)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Get(bg, tg); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.Accepted() != 1 {
		t.Errorf("%d connections for one profile", s.Accepted())
	}
}
