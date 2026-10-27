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
// for cfg and the drain signal drain on the request context, and returns
// the response. Every caller passes a signal, because the handler panics
// without one.
func probe(t *testing.T, cfg config.Config, drain <-chan struct{}) *httptest.ResponseRecorder {
	t.Helper()
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	ctx := WithDrain(qdb.WithCluster(observeContext(), c), drain)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/status/readiness", nil)
	resp := httptest.NewRecorder()
	NewHandler().ServeHTTP(resp, req)
	return resp
}

// observeContext carries a discarding logger so handlers can log.
func observeContext() context.Context {
	logger := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	return observe.WithLogger(context.Background(), logger)
}

// TestReadinessOKAgainstLiveCluster: each reachable cluster answers 200
// with no Retry-After and an empty body. On the secure cluster the probe
// dials as the fixture's test user, the REST API's own user there.
func TestReadinessOKAgainstLiveCluster(t *testing.T) {
	for _, spec := range qdbtest.Clusters(t) {
		t.Run(spec.Name, func(t *testing.T) {
			resp := probe(t, spec.Config(t), make(chan struct{}))
			if resp.Code != http.StatusOK {
				t.Fatalf("readiness = %d, want 200", resp.Code)
			}
			if resp.Body.Len() != 0 {
				t.Fatalf("readiness body = %q, want empty", resp.Body.String())
			}
			if resp.Header().Get("Retry-After") != "" {
				t.Fatal("readiness set Retry-After")
			}
		})
	}
}

// TestReadinessUnavailableAgainstUnreachable: an unreachable cluster
// answers 503 with no Retry-After and an empty body.
func TestReadinessUnavailableAgainstUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1"
	resp := probe(t, cfg, make(chan struct{}))
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

// TestReadinessDuringDrain: on each reachable cluster the route answers
// 200 while the drain signal is open and 503 once it has fired, with an
// empty body and no Retry-After both times. The 503 on a live cluster
// proves the signal decided and not the dial.
func TestReadinessDuringDrain(t *testing.T) {
	for _, spec := range qdbtest.Clusters(t) {
		t.Run(spec.Name, func(t *testing.T) {
			cfg := spec.Config(t)
			// 1. An open channel answers 200, because the signal's presence
			//    is not the drain, its close is.
			drain := make(chan struct{})
			resp := probe(t, cfg, drain)
			if resp.Code != http.StatusOK {
				t.Fatalf("readiness before the drain = %d, want 200", resp.Code)
			}
			// 2. A closed channel answers 503 with an empty body and no
			//    Retry-After (ADR-0004, rule 2).
			close(drain)
			resp = probe(t, cfg, drain)
			if resp.Code != http.StatusServiceUnavailable {
				t.Fatalf("readiness during the drain = %d, want 503", resp.Code)
			}
			if resp.Body.Len() != 0 {
				t.Fatalf("readiness body = %q, want empty", resp.Body.String())
			}
			if resp.Header().Get("Retry-After") != "" {
				t.Fatal("readiness set Retry-After")
			}
		})
	}
}
