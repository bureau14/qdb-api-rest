// Package httpapi serves the REST server's /api/v2 resource API, its
// bearer middleware and the unauthenticated status probes. It never
// imports v1 compatibility code. The v1 package wraps this one, and the
// entry point composes the two.
package httpapi

import (
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// handleLiveness reports that the process is up and serving HTTP. It
// does not look at the cluster.
func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReadiness reports whether this instance can serve traffic. Every
// probe dials the cluster as the REST API's own user, caches nothing,
// and bypasses the pool, the budget and the breaker. It answers 200 or
// 503 with an empty body, and the cause of a failure is only logged.
func handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := qdb.ClusterFrom(ctx).Probe(ctx); err != nil {
		observe.Logger(ctx).WarnContext(ctx, "readiness probe failed", observe.Err(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// registerStatusRoutes serves the probes at their unversioned paths,
// which load balancers at customer sites health-check, and at their
// /api/v2 mirrors. Liveness answers 200 with an empty body and no
// Content-Type. Readiness dials the cluster.
func registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/status/readiness", handleReadiness)
	mux.HandleFunc("GET /api/v2/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/v2/status/readiness", handleReadiness)
}

// NewHandler returns the root handler, which serves every route behind
// the request middleware. The handlers take the logger and the cluster
// from the request context, which the server derives from the process
// context, so NewHandler injects nothing.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	registerStatusRoutes(mux)
	registerAuthRoutes(mux)
	registerQueryRoutes(mux)
	registerTableRoutes(mux)
	registerReadRoutes(mux)
	registerRowsRoutes(mux)
	return withRequestLogging(mux)
}
