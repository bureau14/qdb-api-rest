package pool

import (
	"context"
	"errors"
	"flag"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

// The tests dial real handles to the insecure cluster; nothing is skipped
// or faked. Every handle a test opens is closed before the next step, so
// the cluster never sees more than a handful of qdbd sessions at once.

// The property tests dial and close a real handle per step, so the
// example count is held down unless -rapid.checks says otherwise.
const defaultChecks = "25"

func TestMain(m *testing.M) {
	qdbapi.SetLogger(&qdbapi.NilLogger{})
	flag.Parse()
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if !set["rapid.checks"] {
		_ = flag.Set("rapid.checks", defaultChecks)
	}
	os.Exit(m.Run())
}

// handle is a real handle with an identity that survives close: the C
// API reuses addresses, so the pointer alone cannot tell conns apart.
type handle struct {
	qdbapi.HandleType
	id uint64
}

var handleIDs atomic.Uint64

// dialHandle opens a real handle with one worker thread, the cheapest
// handle the C API makes.
func dialHandle(context.Context) (handle, error) {
	opts := qdbapi.NewHandleOptions().
		WithClusterUri(qdbtest.InsecureURI).
		WithCompression(qdbapi.CompNone).
		WithClientMaxParallelism(1).
		WithTimeout(5 * time.Second)
	h, err := qdbapi.NewHandleFromOptions(opts)
	return handle{HandleType: h, id: handleIDs.Add(1)}, err
}

// clock is the fake behind Options.Now; advance moves it, nothing sleeps.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// failer is what waitClosed needs from *testing.T and *rapid.T alike.
type failer interface {
	Helper()
	Fatalf(format string, args ...any)
}

// waitClosed polls until every close has finished or the bound passes.
func waitClosed(t failer, p *Pool[handle]) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for p.Stats().Closing > 0 {
		if time.Now().After(deadline) {
			t.Fatalf("closes still in flight: %+v", p.Stats())
		}
		time.Sleep(time.Millisecond)
	}
}

// model mirrors what the pool must be doing: which conns are leased,
// which are idle and since when, which were ever handed out or discarded.
type model struct {
	pool      *Pool[handle]
	clock     *clock
	opts      Options
	leases    []*Lease[handle]
	idle      []idle[handle]
	seen      map[handle]bool
	discarded map[handle]bool
}

func newModel(t *rapid.T) *model {
	c := &clock{now: time.Unix(1_700_000_000, 0)}
	opts := Options{
		Max:         rapid.IntRange(1, 4).Draw(t, "max"),
		IdleTimeout: time.Duration(rapid.IntRange(1, 100).Draw(t, "idle_timeout")) * time.Second,
		MaxLifetime: time.Duration(rapid.IntRange(1, 300).Draw(t, "max_lifetime")) * time.Second,
		Now:         c.Now,
	}
	return &model{
		pool:      New(dialHandle, opts),
		clock:     c,
		opts:      opts,
		seen:      map[handle]bool{},
		discarded: map[handle]bool{},
	}
}

// dropExpiredIdle is the model's view of expiry: the idle list is ordered
// by lastUsed, so the expired conns are a prefix.
func (m *model) dropExpiredIdle() {
	now := m.clock.Now()
	kept := m.idle[:0]
	for _, e := range m.idle {
		if now.Sub(e.lastUsed) < m.opts.IdleTimeout {
			kept = append(kept, e)
		}
	}
	m.idle = kept
}

func (m *model) acquire(t *rapid.T) {
	m.dropExpiredIdle()
	switch {
	case len(m.idle) > 0:
		want := m.idle[len(m.idle)-1]
		m.idle = m.idle[:len(m.idle)-1]
		l, err := m.pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		if l.Conn() != want.conn {
			t.Fatalf("acquire handed out %v, want the freshest idle conn %v", l.Conn(), want.conn)
		}
		m.leases = append(m.leases, l)
	case len(m.leases) < m.opts.Max:
		l, err := m.pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		if m.seen[l.Conn()] {
			t.Fatalf("acquire handed out a conn seen before: %v", l.Conn())
		}
		if m.discarded[l.Conn()] {
			t.Fatalf("acquire handed out a discarded conn: %v", l.Conn())
		}
		m.seen[l.Conn()] = true
		m.leases = append(m.leases, l)
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if _, err := m.pool.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("acquire at capacity: got %v, want the context's error", err)
		}
	}
}

// pick removes and returns a random lease.
func (m *model) pick(t *rapid.T) *Lease[handle] {
	i := rapid.IntRange(0, len(m.leases)-1).Draw(t, "lease")
	l := m.leases[i]
	m.leases = append(m.leases[:i], m.leases[i+1:]...)
	return l
}

func (m *model) release(t *rapid.T) {
	if len(m.leases) == 0 {
		t.Skip("nothing leased")
	}
	l := m.pick(t)
	now := m.clock.Now()
	l.Release()
	if now.Sub(l.created) < m.opts.MaxLifetime {
		m.idle = append(m.idle, idle[handle]{conn: l.Conn(), created: l.created, lastUsed: now})
	}
}

func (m *model) discard(t *rapid.T) {
	if len(m.leases) == 0 {
		t.Skip("nothing leased")
	}
	l := m.pick(t)
	l.Discard()
	m.discarded[l.Conn()] = true
}

func (m *model) advance(t *rapid.T) {
	m.clock.advance(time.Duration(rapid.IntRange(0, 120).Draw(t, "seconds")) * time.Second)
}

func (m *model) reap(*rapid.T) {
	m.pool.Reap()
	m.dropExpiredIdle()
}

// check is the invariant run after every step. It first lets closes in
// flight finish: a close still holds the handle's cluster sessions, and
// qdbd's session pool is finite (Verified facts in docs/pool-plan.md),
// so the test keeps its open handles bounded by Max at every step.
func (m *model) check(t *rapid.T) {
	waitClosed(t, m.pool)
	s := m.pool.Stats()
	if s.InUse != len(m.leases) || s.Idle != len(m.idle) {
		t.Fatalf("stats %+v, model has %d leased and %d idle", s, len(m.leases), len(m.idle))
	}
	if s.InUse+s.Idle > m.opts.Max {
		t.Fatalf("stats %+v exceed max %d", s, m.opts.Max)
	}
}

// The pool obeys its invariants under any sequence of acquire, release,
// discard, advance-clock and reap: never more than Max leased or idle,
// the freshest idle conn is reused, an expired or discarded conn is never
// handed out, a conn past MaxLifetime is closed on release, Acquire at
// capacity returns when its context ends, and Close drains everything.
func TestPoolInvariants(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	rapid.Check(t, func(rt *rapid.T) {
		m := newModel(rt)
		rt.Repeat(map[string]func(*rapid.T){
			"":        m.check,
			"acquire": m.acquire,
			"release": m.release,
			"discard": m.discard,
			"advance": m.advance,
			"reap":    m.reap,
		})
		for _, l := range m.leases {
			l.Release()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := m.pool.Close(ctx); err != nil {
			rt.Fatalf("close: %v", err)
		}
		if s := m.pool.Stats(); s.InUse != 0 || s.Idle != 0 || s.Closing != 0 || s.Dialing != 0 {
			rt.Fatalf("after close: %+v", s)
		}
	})
}

// Concurrent acquirers never exceed Max between them.
func TestConcurrentAcquireHonorsMax(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	const max, workers, rounds = 3, 12, 5
	p := New(dialHandle, Options{Max: max})
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range rounds {
				l, err := p.Acquire(context.Background())
				if err != nil {
					t.Errorf("acquire: %v", err)
					return
				}
				if s := p.Stats(); s.InUse+s.Idle > max {
					t.Errorf("stats %+v exceed max %d", s, max)
				}
				l.Release()
			}
		})
	}
	wg.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// A failed dial consumes no slot: the pool stays fully usable.
func TestFailedDialConsumesNoSlot(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	fail := errors.New("dial refused")
	calls := 0
	dial := func(ctx context.Context) (handle, error) {
		calls++
		if calls == 1 {
			return handle{}, fail
		}
		return dialHandle(ctx)
	}
	p := New(dial, Options{Max: 1})
	if _, err := p.Acquire(context.Background()); !errors.Is(err, fail) {
		t.Fatalf("first acquire: got %v, want %v", err, fail)
	}
	if s := p.Stats(); s.InUse != 0 || s.Dialing != 0 || s.Dialed != 0 {
		t.Fatalf("after failed dial: %+v", s)
	}
	l, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	l.Release()
	waitClosed(t, p)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// Close with a lease outstanding waits until ctx ends and says so; the
// leased conn is never closed underneath its holder.
func TestCloseWaitsForLeases(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	p := New(dialHandle, Options{Max: 1})
	l, err := p.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close with a lease out: got %v, want the context's error", err)
	}
	if _, err := p.Acquire(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("acquire after close: got %v, want ErrClosed", err)
	}
	if s := p.Stats(); s.InUse != 1 || s.Closing != 0 {
		t.Fatalf("leased conn touched by close: %+v", s)
	}
	l.Release()
	waitClosed(t, p)
}
