package sshconn

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrNotResponding is the cause of a connection closed after two missed
// keepalive replies.
var ErrNotResponding = errors.New("the server stopped responding")

// ErrPoolClosed is returned by Get after Shutdown.
var ErrPoolClosed = errors.New("the connection pool is closed")

// Pool keeps one connection per profile, shared by that profile's
// uploads. Connections are dialed on first use, kept alive while idle, and
// dialed again on the next use after they are lost. It is safe from any
// goroutine.
type Pool struct {
	cfg *Config

	mu      sync.Mutex
	entries map[int64]*entry
	closed  bool
}

type entry struct {
	dial   sync.Mutex // serializes dialing for one profile
	mu     sync.Mutex
	conn   *Conn
	status Status
}

// NewPool returns an empty pool.
func NewPool(cfg *Config) *Pool {
	return &Pool{cfg: cfg, entries: map[int64]*entry{}}
}

func (p *Pool) entry(id int64) (*entry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, ErrPoolClosed
	}
	e := p.entries[id]
	if e == nil {
		e = &entry{status: StatusDisconnected}
		p.entries[id] = e
	}
	return e, nil
}

func (p *Pool) emit(id int64, e *entry, s Status, err error) {
	e.mu.Lock()
	e.status = s
	e.mu.Unlock()
	if p.cfg.OnStatus != nil {
		p.cfg.OnStatus(StatusEvent{ProfileID: id, Status: s, Err: err})
	}
}

// Get returns the profile's open connection, dialing one when there is
// none or it was lost.
func (p *Pool) Get(ctx context.Context, t Target) (*Conn, error) {
	e, err := p.entry(t.ProfileID)
	if err != nil {
		return nil, err
	}
	e.dial.Lock()
	defer e.dial.Unlock()
	e.mu.Lock()
	c := e.conn
	e.mu.Unlock()
	if c != nil && c.Alive() {
		return c, nil
	}
	p.emit(t.ProfileID, e, StatusConnecting, nil)
	c, err = p.cfg.Dial(ctx, t)
	if err != nil {
		p.emit(t.ProfileID, e, StatusError, err)
		return nil, err
	}
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		c.Close()
		return nil, ErrPoolClosed
	}
	e.mu.Lock()
	e.conn = c
	e.mu.Unlock()
	p.emit(t.ProfileID, e, StatusConnected, nil)
	go p.watch(t.ProfileID, e, c)
	return c, nil
}

// watch keeps c alive with keepalive@openssh.com requests and reports when
// it is lost. Two missed replies in a row close it.
func (p *Pool) watch(id int64, e *entry, c *Conn) {
	interval := p.cfg.keepalive()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var cause error
	misses := 0
loop:
	for {
		select {
		case <-c.Dead():
			break loop
		case <-tick.C:
		}
		reply := make(chan error, 1)
		go func() {
			_, _, err := c.SSH.SendRequest("keepalive@openssh.com", true, nil)
			reply <- err
		}()
		timer := time.NewTimer(interval)
		select {
		case err := <-reply:
			timer.Stop()
			if err != nil {
				cause = err
				c.Close()
				break loop
			}
			misses = 0
		case <-timer.C:
			misses++
		case <-c.Dead():
			timer.Stop()
			break loop
		}
		if misses >= 2 {
			cause = ErrNotResponding
			c.Close()
			break loop
		}
	}
	e.mu.Lock()
	current := e.conn == c
	if current {
		e.conn = nil
	}
	e.mu.Unlock()
	if current {
		p.emit(id, e, StatusDisconnected, cause)
	}
}

// Discard closes c if it is still the profile's connection, so the next
// Get dials again, as before a retry.
func (p *Pool) Discard(c *Conn) {
	if c != nil {
		c.Close()
	}
}

// Close closes a profile's connection, if any.
func (p *Pool) Close(profileID int64) {
	p.mu.Lock()
	e := p.entries[profileID]
	p.mu.Unlock()
	if e == nil {
		return
	}
	e.mu.Lock()
	c := e.conn
	e.mu.Unlock()
	if c != nil {
		c.Close()
	}
}

// CloseIdle closes the connections of the profiles keep rejects, such as
// inactive profiles whose queues are empty.
func (p *Pool) CloseIdle(keep func(profileID int64) bool) {
	p.mu.Lock()
	ids := make([]int64, 0, len(p.entries))
	for id := range p.entries {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	for _, id := range ids {
		if !keep(id) {
			p.Close(id)
		}
	}
}

// Status returns a profile's connection state.
func (p *Pool) Status(profileID int64) Status {
	p.mu.Lock()
	e := p.entries[profileID]
	p.mu.Unlock()
	if e == nil {
		return StatusDisconnected
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status
}

// Shutdown closes every connection; Get fails afterwards.
func (p *Pool) Shutdown() {
	p.mu.Lock()
	p.closed = true
	ids := make([]int64, 0, len(p.entries))
	for id := range p.entries {
		ids = append(ids, id)
	}
	p.mu.Unlock()
	for _, id := range ids {
		p.Close(id)
	}
}
