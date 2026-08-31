// The integration tests of Cluster against the live qdbd fixture
// (internal/qdbtest): breaker, per-user cap, retry-once, the fate of a
// fatal error, user-pool eviction, and the secure dial as the REST
// API's own user. The pool's own invariants are pinned upstream in
// qdb-api-go.
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

// insecureConfig is the default config pointed at the insecure cluster,
// with the pool sizes overridden per test.
func insecureConfig(mutate func(*config.Config)) config.Config {
	cfg := config.Default()
	cfg.Cluster.URI = qdbtest.InsecureURI
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

// anonymous names the anonymous user.
var anonymous = User{}

// TestFatalErrorReusesSession: a bad query is fatal (the cluster answered),
// so the session is returned to the pool, not discarded.
func TestFatalErrorReusesSession(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	c := New(insecureConfig(nil), nil)
	defer closeCluster(t, c)

	err := c.Query(context.Background(), anonymous, "NOT A QUERY", func(*qdbapi.QueryResult) error { return nil })
	if err == nil {
		t.Fatal("want an error for a malformed query")
	}
	if qdbapi.IsRetryable(err) {
		t.Fatalf("a malformed query should be fatal, got retryable: %v", err)
	}
	if s := c.poolFor(anonymous).Stats(); s.Idle != 1 {
		t.Fatalf("fatal error did not return the session: %+v", s)
	}
}

// TestPerUserCapAndSharing: one user's concurrent calls never exceed the
// per-user cap, and two User values with the same name share one pool.
func TestPerUserCapAndSharing(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	c := New(insecureConfig(func(cfg *config.Config) {
		cfg.Pool.PerUserMax = 2
		cfg.Pool.MaxSessions = 8
	}), nil)
	defer closeCluster(t, c)

	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			err := c.Query(context.Background(), anonymous, "SELECT 1", func(*qdbapi.QueryResult) error {
				if s := c.poolFor(anonymous).Stats(); s.InUse > 2 {
					t.Errorf("per-user cap exceeded: %+v", s)
				}
				return nil
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

// TestRetryOnceReturnsAfterRetryableFailure: a call that always fails
// retryably is attempted exactly twice with WithReadRetry.
func TestRetryOnceOnRetryableFailure(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	c := New(insecureConfig(nil), nil)
	defer closeCluster(t, c)

	attempts := 0
	err := c.Call(context.Background(), anonymous, func(*Session) error {
		attempts++
		return qdbapi.ErrConnectionReset // retryable
	}, WithReadRetry())
	if !qdbapi.IsRetryable(err) {
		t.Fatalf("want the retryable error back, got %v", err)
	}
	if attempts != 2 {
		t.Fatalf("want exactly two attempts, got %d", attempts)
	}
}

// TestSecureDialAsOwnUser: the readiness probe dials the secure cluster
// as the REST API's own user and runs the query.
func TestSecureDialAsOwnUser(t *testing.T) {
	qdbtest.Require(t, qdbtest.SecureURI)
	cfg := config.Default()
	cfg.Cluster.URI = qdbtest.SecureURI
	cfg.Cluster.PublicKeyFile = qdbtest.ClusterPublicKeyFile()
	cfg.Cluster.UserSecurityFile = qdbtest.UserSecurityFile()
	c := New(cfg, nil)
	defer closeCluster(t, c)

	if err := c.Probe(context.Background()); err != nil {
		t.Fatalf("probe against the secure cluster: %v", err)
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
	qdbtest.Require(t, qdbtest.InsecureURI)
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	c := New(insecureConfig(func(cfg *config.Config) {
		cfg.Pool.IdleTimeout = time.Minute
		cfg.Pool.MaxLifetime = time.Hour
	}), clk.Now)
	defer closeCluster(t, c)

	if err := c.Query(context.Background(), anonymous, "SELECT 1", func(*qdbapi.QueryResult) error { return nil }); err != nil {
		t.Fatalf("query: %v", err)
	}
	if s := c.Stats(); s.Users != 1 {
		t.Fatalf("want one user pool after a query, got %d", s.Users)
	}

	// Past idle_timeout the pool's own reaper closes the idle session; the
	// test runs that pass itself instead of waiting for the tick.
	clk.advance(2 * time.Minute)
	up := c.poolFor(anonymous)
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
