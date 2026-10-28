// Package httpapi serves the REST server's /api/v2 resource API, its
// bearer middleware and the unauthenticated operational routes: the
// status probes, the metrics exposition and the runtime profiler. It
// never imports v1 compatibility code. The v1 package wraps this one,
// and the entry point composes the two.
package httpapi

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// handleLiveness reports that the process is up and serving HTTP. It
// does not look at the cluster.
func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReadiness reports whether this instance may take new traffic:
// 200 or 503 with an empty body and no Retry-After, and the cause of a
// 503 goes to the log and not the wire. It reads nothing of the serving
// lifecycle: the drain closes the listeners before anything else, so a
// prober's next connection is refused.
func handleReadiness(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// Dial as the REST API's own user, bypassing the pool, the budget and
	// the breaker. A failure is 503 with the cause at warn.
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
// Content-Type. Readiness dials the cluster, and answers 503 without a
// dial while the server drains.
func registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/status/readiness", handleReadiness)
	mux.HandleFunc("GET /api/v2/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/v2/status/readiness", handleReadiness)
}

// handleMetrics serves the registry's exposition, with the client's
// handler built over the registry the request context carries. A scrape
// reads in-process counters only and never touches the cluster
// (ADR-0004, rule 3).
func handleMetrics(w http.ResponseWriter, r *http.Request) {
	// The handler is built per scrape and not once at registration,
	// because the registry travels in the context and NewHandler has no
	// context. A scrape is rare and the build is a small allocation. The
	// zero HandlerOpts: a gather error is a 500, and the exposition is
	// gzip-compressed when the scraper asks, by the client's own writer,
	// because the route sits outside withCompression.
	reg := observe.MetricsFrom(r.Context()).Registry
	promhttp.HandlerFor(reg, promhttp.HandlerOpts{}).ServeHTTP(w, r)
}

// registerMetricsRoute serves GET /metrics beside the probes, outside
// the bearer edge and the compression wrapper, when the exposition is
// on. Off, the route is not registered and the path is the mux's 404.
func registerMetricsRoute(mux *http.ServeMux, cfg config.Metrics) {
	// A registered route answering 404 itself was rejected, because
	// ADR-0004 says the key disables the endpoint and nothing else
	// switches it, and the mux already says 404.
	if !cfg.Enabled {
		return
	}
	mux.HandleFunc("GET /metrics", handleMetrics)
}

// NewHandler returns the root handler, which serves every route behind
// the request middleware. It takes the switch of the unauthenticated
// operational route, because configuration does not travel in the
// context. The handlers take the logger, the cluster, the keychain and
// the metrics from the request context, which the server derives from
// the process context.
func NewHandler(metrics config.Metrics) http.Handler {
	mux := http.NewServeMux()
	// The unauthenticated routes sit together, before the data routes.
	registerStatusRoutes(mux)
	registerMetricsRoute(mux, metrics)
	registerAuthRoutes(mux)
	registerQueryRoutes(mux)
	registerTableRoutes(mux)
	registerReadRoutes(mux)
	registerRowsRoutes(mux)
	return withRequestLogging(mux)
}
