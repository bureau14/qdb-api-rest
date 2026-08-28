// Package httpapi assembles the HTTP surface of the REST server: the
// legacy compatibility endpoints, the /api/v2 resource API, and the
// unauthenticated status probes.
package httpapi

import (
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// handleLiveness reports that the process is up and serving HTTP. It is
// never cluster-aware (ADR-0004).
func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// readiness answers whether this instance can serve traffic. It dials the
// cluster as the service user on every probe, with no cached verdict and
// no effect on the pool, the budget or the breaker (ADR-0004): 200 when
// the probe succeeds, 503 when it fails, both with an empty body. The
// cause goes to the log line, not the wire.
type readiness struct {
	cluster        *qdb.Cluster
	readinessQuery string
}

func (h readiness) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.cluster.Probe(r.Context(), h.readinessQuery); err != nil {
		observe.Logger(r.Context()).WarnContext(r.Context(), "readiness probe failed", observe.Err(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// registerStatusRoutes serves the probes at their legacy paths, which
// load balancers at customer sites health-check, and at their /api/v2
// mirrors. Liveness answers 200 with an empty body and no Content-Type,
// the shape pinned by the status-probe goldens; readiness dials the
// cluster.
func registerStatusRoutes(mux *http.ServeMux, ready readiness) {
	mux.HandleFunc("GET /api/status/liveness", handleLiveness)
	mux.Handle("GET /api/status/readiness", ready)
	mux.HandleFunc("GET /api/v2/status/liveness", handleLiveness)
	mux.Handle("GET /api/v2/status/readiness", ready)
}

// NewHandler returns the root handler: every route registered, wrapped by
// the request middleware. The cluster and the readiness query are read
// from config; the cluster also travels on the request context for the
// handlers below.
func NewHandler(cluster *qdb.Cluster, cfg config.Config) http.Handler {
	mux := http.NewServeMux()
	registerStatusRoutes(mux, readiness{cluster: cluster, readinessQuery: cfg.Status.ReadinessQuery})
	return withRequestLogging(mux)
}
