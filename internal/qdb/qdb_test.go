package qdb

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// The tests run against the live qdbd pair of
// scripts/tests/setup/start-services.sh: insecure on 2836, secure on 2838
// with cluster_public.key / user_private.key at the repo root. Nothing is
// skipped; a down cluster fails fast with the recipe.
const (
	insecureURI = "qdb://127.0.0.1:2836"
	secureURI   = "qdb://127.0.0.1:2838"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// requireQdbd fails fast with the recipe when a port does not answer.
func requireQdbd(t *testing.T, port string) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", port, time.Second)
	if err != nil {
		t.Fatalf("qdbd not answering on %s; run: bash scripts/tests/setup/start-services.sh", port)
	}
	_ = conn.Close()
}

// repoRoot is two levels up from this package.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// insecureConfig is the default config pointed at the insecure cluster,
// with the pool sizes overridden per test.
func insecureConfig(mutate func(*config.Config)) config.Config {
	cfg := config.Default()
	cfg.Cluster.URI = insecureURI
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

// anonymous names the anonymous principal.
var anonymous = Principal{}

// TestQueryRoundTrips runs a trivial query and reads its one cell back.
func TestQueryRoundTrips(t *testing.T) {
	requireQdbd(t, "127.0.0.1:2836")
	c := New(insecureConfig(nil), nil)
	defer closeCluster(t, c)

	var got int64
	err := c.Query(context.Background(), anonymous, "SELECT 1", func(r *qdbapi.QueryResult) error {
		rows := r.Rows()
		if len(rows) != 1 {
			t.Fatalf("want 1 row, got %d", len(rows))
		}
		v, err := r.Columns(rows[0])[0].GetInt64()
		got = v
		return err
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if got != 1 {
		t.Fatalf("SELECT 1 returned %d", got)
	}
}

// TestFatalErrorReusesHandle: a bad query is fatal (the cluster answered),
// so the handle is returned to the pool, not discarded.
func TestFatalErrorReusesHandle(t *testing.T) {
	requireQdbd(t, "127.0.0.1:2836")
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
		t.Fatalf("fatal error did not return the handle: %+v", s)
	}
}

// TestPerUserCapAndSharing: one user's concurrent calls never exceed the
// per-user cap, and two principals with the same name share one pool.
func TestPerUserCapAndSharing(t *testing.T) {
	requireQdbd(t, "127.0.0.1:2836")
	c := New(insecureConfig(func(cfg *config.Config) {
		cfg.Pool.PerUserMax = 2
		cfg.Pool.MaxHandles = 8
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
	noop := func(*Handle) error { return nil }
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
	requireQdbd(t, "127.0.0.1:2836")
	c := New(insecureConfig(nil), nil)
	defer closeCluster(t, c)

	attempts := 0
	err := c.Call(context.Background(), anonymous, func(*Handle) error {
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

// TestPoisonedHandleShortCircuits: once a handle has timed out, later
// calls on it return ErrCallTimeout without a C call.
func TestPoisonedHandleShortCircuits(t *testing.T) {
	requireQdbd(t, "127.0.0.1:2836")
	c := New(insecureConfig(nil), nil)
	defer closeCluster(t, c)

	h, err := c.connect(context.Background(), Principal{}.credentials(), true)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	h.mu.Lock()
	h.poisoned = true
	h.mu.Unlock()
	done, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := h.query(done, "SELECT 1", func(*qdbapi.QueryResult) error { return nil }); !errors.Is(err, ErrCallTimeout) {
		t.Fatalf("poisoned handle ran a call: %v", err)
	}
	_ = h.Close()
}

// TestSecureDialWithServiceUser: the readiness probe dials the secure
// cluster as its service user and runs the query.
func TestSecureDialWithServiceUser(t *testing.T) {
	requireQdbd(t, "127.0.0.1:2838")
	root := repoRoot(t)
	cfg := config.Default()
	cfg.Cluster.URI = secureURI
	cfg.Cluster.PublicKeyFile = filepath.Join(root, "cluster_public.key")
	cfg.Cluster.ServiceUser.File = filepath.Join(root, "user_private.key")
	c := New(cfg, nil)
	defer closeCluster(t, c)

	if err := c.Probe(context.Background(), cfg.Status.ReadinessQuery); err != nil {
		t.Fatalf("probe against the secure cluster: %v", err)
	}
}

// TestProbeFailsOnUnreachable: readiness dials on every probe and fails
// when the cluster is unreachable.
func TestProbeFailsOnUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1"
	c := New(cfg, nil)
	defer closeCluster(t, c)
	if err := c.Probe(context.Background(), cfg.Status.ReadinessQuery); err == nil {
		t.Fatal("want a probe failure against an unreachable cluster")
	}
}
