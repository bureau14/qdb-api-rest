// Package httpapi serves the REST server's /api/v2 resource API, its
// bearer middleware and the unauthenticated operational routes: the
// status probes, the metrics exposition and the runtime profiler. It
// never imports v1 compatibility code. The v1 package wraps this one,
// and the entry point composes the two.
package httpapi

import (
	"net/http"
	"net/http/pprof"

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

// registerPprofRoutes serves the Go runtime profiler under
// /debug/pprof/ on this mux when debug.pprof is true, for goroutine,
// heap and CPU profiles of a live instance. It is not a metrics source.
func registerPprofRoutes(mux *http.ServeMux, cfg config.Debug) {
	if !cfg.Pprof {
		return
	}
	// Index, Cmdline, Profile, Symbol and Trace are registered by hand on
	// this mux, because net/http/pprof's init registers them on the
	// default mux, which this binary never serves. The patterns carry no
	// method, because Symbol takes POST and Index serves every named
	// profile under the /debug/pprof/ prefix.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}

// NewHandler returns the root handler, which serves every route behind
// the request middleware. It takes the two switches of the
// unauthenticated operational routes, because configuration does not
// travel in the context. The handlers take the logger, the cluster, the
// keychain and the metrics from the request context, which the server
// derives from the process context.
func NewHandler(metrics config.Metrics, debug config.Debug) http.Handler {
	// The switches are the two configuration blocks. Two bare bools were
	// rejected, because the call site would not say which is which, and
	// the whole config.Config, because the handler reads two keys.
	mux := http.NewServeMux()
	// The unauthenticated routes sit together, before the data routes.
	registerStatusRoutes(mux)
	registerMetricsRoute(mux, metrics)
	registerPprofRoutes(mux, debug)
	registerAuthRoutes(mux)
	registerQueryRoutes(mux)
	registerTableRoutes(mux)
	registerReadRoutes(mux)
	registerRowsRoutes(mux)
	return withRequestLogging(mux)
}
