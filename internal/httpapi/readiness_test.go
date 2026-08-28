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
	ctx := qdb.WithCluster(observeContext(), c)
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

// TestReadinessOKAgainstLiveCluster: a reachable cluster answers 200 with
// no Retry-After and an empty body.
func TestReadinessOKAgainstLiveCluster(t *testing.T) {
	qdbtest.Require(t, qdbtest.InsecureURI)
	cfg := config.Default()
	cfg.Cluster.URI = qdbtest.InsecureURI
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
