// Package pool is a bounded pool of connections generic over a dial
// function. It knows nothing about users, HTTP or qdb-api-go, so the
// package could move upstream unchanged; the REST layer above it owns the
// user map, the budget and the breaker.
//
// The pool never reads the wall clock directly: every age is measured
// against Options.Now, and idle expiry happens only inside Reap or
// Acquire. A pool driven by a fake clock is therefore fully deterministic,
// which is what lets the property tests advance time without waiting.
package pool

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Conn is what the pool hands out and, eventually, closes.
type Conn interface{ Close() error }

// Dial opens one Conn. It runs on the acquiring goroutine under the
// acquirer's context; an error consumes no slot.
type Dial[C Conn] func(ctx context.Context) (C, error)

// Options sizes a pool. Zero IdleTimeout or MaxLifetime means the
// respective age never expires; a nil Now means time.Now.
type Options struct {
	Max         int
	IdleTimeout time.Duration
	MaxLifetime time.Duration
	Now         func() time.Time
}

// Stats is a snapshot of the pool. InUse plus Idle never exceeds Max;
// Closing counts closes still running on their own goroutines, which do
// not hold a slot.
type Stats struct {
	InUse     int
	Idle      int
	Dialing   int
	Closing   int
	Dialed    uint64 // conns dialed successfully, ever
	Discarded uint64 // leases discarded by the caller, ever
}

// ErrClosed is returned by Acquire once Close has been called.
var ErrClosed = errors.New("pool: closed")

// idle is a conn waiting to be reused; lastUsed is when it was released.
type idle[C Conn] struct {
	conn     C
	created  time.Time
	lastUsed time.Time
}

// Pool is a bounded set of conns: at most Max leased or idle at once.
// The idle list is ordered by lastUsed, oldest first, so expiry always
// removes a prefix and reuse always takes the freshest.
type Pool[C Conn] struct {
	dial Dial[C]
	opts Options

	mu        sync.Mutex
	idle      []idle[C]
	leased    int
	dialing   int
	closing   int
	dialed    uint64
	discarded uint64
	closed    bool
	changed   chan struct{} // closed and replaced on every state change
}

// New returns an empty pool; nothing is dialed until Acquire.
func New[C Conn](dial Dial[C], opts Options) *Pool[C] {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Pool[C]{dial: dial, opts: opts, changed: make(chan struct{})}
}

// notifyLocked wakes every goroutine waiting for the state to change.
func (p *Pool[C]) notifyLocked() {
	close(p.changed)
	p.changed = make(chan struct{})
}

// closeLocked closes conn on its own goroutine: closing a conn can
// block for a long time, and the pool must never wait on it.
func (p *Pool[C]) closeLocked(conn C) {
	p.closing++
	go func() {
		_ = conn.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		p.closing--
		p.notifyLocked()
	}()
}

// expired reports whether a conn idle since lastUsed is past IdleTimeout.
func (p *Pool[C]) expired(lastUsed, now time.Time) bool {
	return p.opts.IdleTimeout > 0 && now.Sub(lastUsed) >= p.opts.IdleTimeout
}

// outlived reports whether a conn created at created is past MaxLifetime.
func (p *Pool[C]) outlived(created, now time.Time) bool {
	return p.opts.MaxLifetime > 0 && now.Sub(created) >= p.opts.MaxLifetime
}

// takeIdleLocked hands out the freshest idle conn. An expired one is
// closed instead, and since the list is ordered by lastUsed, an expired
// freshest conn means every idle conn has expired.
func (p *Pool[C]) takeIdleLocked() (*Lease[C], bool) {
	now := p.opts.Now()
	for n := len(p.idle); n > 0; n = len(p.idle) {
		e := p.idle[n-1]
		p.idle = p.idle[:n-1]
		if p.expired(e.lastUsed, now) {
			p.closeLocked(e.conn)
			continue
		}
		p.leased++
		return &Lease[C]{pool: p, conn: e.conn, created: e.created}, true
	}
	return nil, false
}

// dialLease dials on the caller's goroutine with a slot reserved. A dial
// that fails, or that completes after Close, returns the slot.
func (p *Pool[C]) dialLease(ctx context.Context) (*Lease[C], error) {
	conn, err := p.dial(ctx)
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dialing--
	defer p.notifyLocked()
	if err != nil {
		return nil, err
	}
	p.dialed++
	if p.closed {
		p.closeLocked(conn)
		return nil, ErrClosed
	}
	p.leased++
	return &Lease[C]{pool: p, conn: conn, created: p.opts.Now()}, nil
}

// Acquire returns a leased conn: the freshest idle one, else a freshly
// dialed one when a slot is free, else it waits for a release or for ctx
// to end.
func (p *Pool[C]) Acquire(ctx context.Context) (*Lease[C], error) {
	for {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, ErrClosed
		}
		if l, ok := p.takeIdleLocked(); ok {
			p.mu.Unlock()
			return l, nil
		}
		if p.leased+len(p.idle)+p.dialing < p.opts.Max {
			p.dialing++
			p.mu.Unlock()
			return p.dialLease(ctx)
		}
		wait := p.changed
		p.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Lease is one checked-out conn. It belongs to the holder until Release
// or Discard; the pool never closes a leased conn, not even in Close.
type Lease[C Conn] struct {
	pool    *Pool[C]
	conn    C
	created time.Time
	done    bool
}

// Conn returns the leased conn.
func (l *Lease[C]) Conn() C { return l.conn }

// Release returns the conn to the idle set, unless it has outlived
// MaxLifetime or the pool is closed, in which case it is closed. A
// second Release or Discard on the same lease is a no-op.
func (l *Lease[C]) Release() {
	p := l.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.done {
		return
	}
	l.done = true
	p.leased--
	now := p.opts.Now()
	if p.closed || p.outlived(l.created, now) {
		p.closeLocked(l.conn)
	} else {
		p.idle = append(p.idle, idle[C]{conn: l.conn, created: l.created, lastUsed: now})
	}
	p.notifyLocked()
}

// Discard closes the conn; it is never handed out again.
func (l *Lease[C]) Discard() {
	p := l.pool
	p.mu.Lock()
	defer p.mu.Unlock()
	if l.done {
		return
	}
	l.done = true
	p.leased--
	p.discarded++
	p.closeLocked(l.conn)
	p.notifyLocked()
}

// Reap closes every idle conn past IdleTimeout as of Now. The caller runs
// the ticker; the pool owns no goroutine besides closes.
func (p *Pool[C]) Reap() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.opts.Now()
	kept := p.idle[:0]
	for _, e := range p.idle {
		if p.expired(e.lastUsed, now) {
			p.closeLocked(e.conn)
		} else {
			kept = append(kept, e)
		}
	}
	if len(kept) != len(p.idle) {
		p.notifyLocked()
	}
	p.idle = kept
}

// Close refuses further acquires, closes every idle conn, and waits for
// outstanding leases, dials and closes until ctx ends. It returns
// ctx.Err() when something was still outstanding.
func (p *Pool[C]) Close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	for _, e := range p.idle {
		p.closeLocked(e.conn)
	}
	p.idle = nil
	p.notifyLocked()
	for p.leased > 0 || p.dialing > 0 || p.closing > 0 {
		wait := p.changed
		p.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
		p.mu.Lock()
	}
	p.mu.Unlock()
	return nil
}

// Stats returns a snapshot.
func (p *Pool[C]) Stats() Stats {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Stats{
		InUse:     p.leased,
		Idle:      len(p.idle),
		Dialing:   p.dialing,
		Closing:   p.closing,
		Dialed:    p.dialed,
		Discarded: p.discarded,
	}
}
