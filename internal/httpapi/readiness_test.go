package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

// probe runs one GET against the readiness route with a cluster built
// for cfg on the request context, and returns the response.
func probe(t *testing.T, cfg config.Config) *httptest.ResponseRecorder {
	t.Helper()
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	ctx := qdb.WithCluster(observeContext(t), c)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/status/readiness", nil)
	resp := httptest.NewRecorder()
	NewHandler().ServeHTTP(resp, req)
	return resp
}

// observeContext carries a REST server logger whose output is shown with
// the test's own when the test fails, and discarded otherwise. A failed
// round trip against the live daemon is read from three sides: the
// test's draws, the server's log and the daemon's log
// (docs/ci-qdbd-logs-plan.md while it is alive; internal/AGENTS.md, Tests).
//
// The level is debug for the duration of that investigation, so a
// failure shows every request the server saw with its details; it
// returns to the default when the plan is deleted.
func observeContext(t testing.TB) context.Context {
	// The buffer is per test, so a passing test adds no output and a failing
	// one shows every request the server saw, in order.
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	t.Cleanup(func() {
		if t.Failed() && buf.Len() > 0 {
			t.Logf("REST server log:\n%s", buf.String())
		}
	})
	return observe.WithLogger(context.Background(), logger)
}

// TestReadinessOKAgainstLiveCluster: a reachable cluster answers 200 with
// no Retry-After and an empty body.
func TestReadinessOKAgainstLiveCluster(t *testing.T) {
	cfg := config.Default()
	qdbtest.BindInsecure(t, &cfg)
	resp := probe(t, cfg)
	if resp.Code != http.StatusOK {
		t.Fatalf("readiness = %d, want 200", resp.Code)
	}
	if resp.Body.Len() != 0 {
		t.Fatalf("readiness body = %q, want empty", resp.Body.String())
	}
	if resp.Header().Get("Retry-After") != "" {
		t.Fatal("readiness set Retry-After")
	}
}

// TestReadinessUnavailableAgainstUnreachable: an unreachable cluster
// answers 503 with no Retry-After and an empty body.
func TestReadinessUnavailableAgainstUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1"
	resp := probe(t, cfg)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("readiness = %d, want 503", resp.Code)
	}
	if resp.Body.Len() != 0 {
		t.Fatalf("readiness body = %q, want empty", resp.Body.String())
	}
	if resp.Header().Get("Retry-After") != "" {
		t.Fatal("readiness set Retry-After")
	}
}
