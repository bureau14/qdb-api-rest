// The integration tests of Cluster against the live qdbd fixture
// (internal/qdbtest): breaker, per-user cap, retry-once, a rejected
// query's session and breaker, user-pool eviction, and the secure dial as
// the REST API's own user. The pool's own invariants and the error
// predicates are pinned upstream in qdb-api-go.
package qdb

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// insecureConfig is the default config pointed at the insecure cluster
// (or the secure one under qdbtest.TrafficToSecure), which fails t when
// that cluster is down, with the pool sizes overridden per test.
func insecureConfig(t *testing.T, mutate func(*config.Config)) config.Config {
	t.Helper()
	cfg := config.Default()
	qdbtest.BindInsecure(t, &cfg)
	if mutate != nil {
		mutate(&cfg)
	}
	return cfg
}

// closeCluster drains a cluster within a bounded context.
func closeCluster(t *testing.T, c *Cluster) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Close(ctx); err != nil {
		t.Errorf("close: %v", err)
	}
}

// anonymous names the anonymous user, for the tests whose cluster is
// never reached.
var anonymous = User{}

// caller is the fixture's caller (qdbtest.Caller) as a User.
func caller() User {
	name, secret := qdbtest.Caller()
	return User{Username: name, SecretKey: secret}
}

// TestRejectedQueryReusesSession: the cluster rejects a malformed query,
// and a rejection is an answer, so the session goes back to the pool and
// the breaker counts a success.
func TestRejectedQueryReusesSession(t *testing.T) {
	c := New(insecureConfig(t, nil), nil)
	defer closeCluster(t, c)

	_, err := c.Query(context.Background(), caller(), "NOT A QUERY")
	if err == nil {
		t.Fatal("want an error for a malformed query")
	}
	if qdbapi.IsBadSession(err) {
		t.Fatalf("a malformed query should not condemn the session: %v", err)
	}
	if s := c.poolFor(caller()).Stats(); s.Idle != 1 {
		t.Fatalf("rejected query did not return the session: %+v", s)
	}
	if st, n := c.breaker.state, c.breaker.failures; st != breakerClosed || n != 0 {
		t.Fatalf("rejected query fed the breaker: state %d, failures %d", st, n)
	}
}

// TestPerUserCapAndSharing: one user's concurrent calls never exceed the
// per-user cap, and two User values with the same name share one pool.
func TestPerUserCapAndSharing(t *testing.T) {
	c := New(insecureConfig(t, func(cfg *config.Config) {
		cfg.Pool.PerUserMax = 2
		cfg.Pool.MaxSessions = 8
	}), nil)
	defer closeCluster(t, c)

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			// The cap is checked while the session is held, so the check runs
			// inside Call: Query has already returned the session by the time it
			// answers.
			err := c.Call(context.Background(), caller(), func(s *Session) error {
				rec, err := s.fetch("SELECT 1")
				if rec != nil {
					rec.Release()
				}
				if st := c.poolFor(caller()).Stats(); st.InUse > 2 {
					t.Errorf("per-user cap exceeded: %+v", st)
				}
				return err
			})
			if err != nil {
				t.Errorf("query: %v", err)
			}
		})
	}
	wg.Wait()
	if s := c.Stats(); s.Users != 1 {
		t.Fatalf("one user should hold one pool, got %d", s.Users)
	}
}

// TestBreakerOpensOnUnreachable: dialing an unreachable cluster fails,
// and after the threshold the breaker fails fast with a Retry-After.
func TestBreakerOpensOnUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1" // connection refused, fast
	cfg.Pool.Breaker.Failures = 2
	cfg.Pool.Breaker.OpenFor = time.Minute
	c := New(cfg, nil)
	defer closeCluster(t, c)

	ctx := context.Background()
	noop := func(*Session) error { return nil }
	for range cfg.Pool.Breaker.Failures {
		if err := c.Call(ctx, anonymous, noop); err == nil {
			t.Fatal("want a dial error against an unreachable cluster")
		}
	}
	err := c.Call(ctx, anonymous, noop)
	var open *BreakerOpenError
	if !errors.As(err, &open) {
		t.Fatalf("breaker did not open: %v", err)
	}
	if open.RetryAfter <= 0 || open.RetryAfter > time.Minute {
		t.Fatalf("Retry-After out of range: %s", open.RetryAfter)
	}
}

// TestRetryOnceOnRetryableFailure: a call that always fails retryably
// is attempted exactly twice with WithReadRetry.
func TestRetryOnceOnRetryableFailure(t *testing.T) {
	c := New(insecureConfig(t, nil), nil)
	defer closeCluster(t, c)

	attempts := 0
	err := c.Call(context.Background(), caller(), func(*Session) error {
		attempts++
		return qdbapi.ErrConnectionReset // retryable
	}, WithReadRetry())
	if !errors.Is(err, qdbapi.ErrConnectionReset) {
		t.Fatalf("want the retryable error back, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("want exactly two attempts, got %d", attempts)
	}
}

// TestSecureDialAsOwnUser: the readiness probe dials the secure cluster
// as the REST API's own user and runs the query.
func TestSecureDialAsOwnUser(t *testing.T) {
	c := New(secureConfig(t), nil)
	defer closeCluster(t, c)

	if err := c.Probe(context.Background()); err != nil {
		t.Fatalf("probe against the secure cluster: %v", err)
	}
}

// secureConfig is the default config pointed at the secure cluster, the
// REST API's own user being the fixture's test user; it fails t when the
// cluster is down.
func secureConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.Default()
	qdbtest.BindSecure(t, &cfg)
	return cfg
}

// TestAuthenticate: the secure cluster accepts its user and refuses a
// wrong secret, the refusal being an answer that leaves the breaker
// closed and no pool behind; the fixture's caller passes the insecure one.
func TestAuthenticate(t *testing.T) {
	c := New(secureConfig(t), nil)
	defer closeCluster(t, c)

	ctx := context.Background()
	name, secret := qdbtest.SecureUser(t)
	u := User{Username: name, SecretKey: secret}
	if err := c.Authenticate(ctx, u); err != nil {
		t.Fatalf("the fixture user refused: %v", err)
	}
	err := c.Authenticate(ctx, User{Username: u.Username, SecretKey: "bm90LWEta2V5"})
	if err == nil || qdbapi.IsClusterUnavailable(err) {
		t.Fatalf("want a refusal from a reachable cluster, got %v", err)
	}
	if got := c.Stats(); got.Users != 0 {
		t.Fatalf("a login left %d pool(s) behind", got.Users)
	}
	if _, ok := c.breaker.allow(); !ok {
		t.Fatal("a refused credential opened the breaker")
	}

	i := New(insecureConfig(t, nil), nil)
	defer closeCluster(t, i)
	if err := i.Authenticate(ctx, caller()); err != nil {
		t.Fatalf("the caller refused by the insecure cluster: %v", err)
	}
}

// TestAuthenticateFailsFastWhenOpen: an unreachable cluster opens the
// breaker through logins too, and the next login fails at once.
func TestAuthenticateFailsFastWhenOpen(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1"
	cfg.Pool.Breaker.Failures = 2
	cfg.Pool.Breaker.OpenFor = time.Minute
	c := New(cfg, nil)
	defer closeCluster(t, c)

	ctx := context.Background()
	for range cfg.Pool.Breaker.Failures {
		if err := c.Authenticate(ctx, anonymous); err == nil {
			t.Fatal("want a dial error against an unreachable cluster")
		}
	}
	var open *BreakerOpenError
	if err := c.Authenticate(ctx, anonymous); !errors.As(err, &open) {
		t.Fatalf("breaker did not open: %v", err)
	}
}

// fakeClock is a manually advanced clock for eviction and lifetime tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestIdleUserPoolEvicted: a user pool that has held no session for
// idle_timeout is reaped away, so the map is bounded by distinct users,
// not by logins.
func TestIdleUserPoolEvicted(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	c := New(insecureConfig(t, func(cfg *config.Config) {
		cfg.Pool.IdleTimeout = time.Minute
		cfg.Pool.MaxLifetime = time.Hour
	}), clk.Now)
	defer closeCluster(t, c)

	if _, err := c.Query(context.Background(), caller(), "SELECT 1"); err != nil {
		t.Fatalf("query: %v", err)
	}
	if s := c.Stats(); s.Users != 1 {
		t.Fatalf("want one user pool after a query, got %d", s.Users)
	}

	// Past idle_timeout the pool's own reaper closes the idle session; the
	// test runs that pass itself instead of waiting for the tick.
	clk.advance(2 * time.Minute)
	up := c.poolFor(caller())
	up.Reap()
	deadline := time.Now().Add(10 * time.Second)
	for s := up.Stats(); s.Idle != 0 || s.Closing != 0; s = up.Stats() {
		if time.Now().After(deadline) {
			t.Fatalf("idle session never closed: %+v", s)
		}
		time.Sleep(2 * time.Millisecond)
	}

	// One reap marks it empty, the next past idle_timeout evicts it.
	c.Reap()
	clk.advance(2 * time.Minute)
	c.Reap()
	if s := c.Stats(); s.Users != 0 {
		t.Fatalf("idle user pool was not evicted: %d users remain", s.Users)
	}
}
