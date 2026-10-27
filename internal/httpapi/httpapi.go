// Package httpapi serves the REST server's /api/v2 resource API, its
// bearer middleware and the unauthenticated status probes. It never
// imports v1 compatibility code. The v1 package wraps this one, and the
// entry point composes the two.
package httpapi

import (
	"context"
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// The drain signal travels in the request context next to the logger
// and the cluster: the entry point places it, and the readiness handler
// reads it. A flag on the cluster was rejected, because the binding
// carries no serving lifecycle.
type drainKey struct{}

// WithDrain returns ctx carrying done, a channel closed once the
// shutdown drain has begun.
func WithDrain(ctx context.Context, done <-chan struct{}) context.Context {
	return context.WithValue(ctx, drainKey{}, done)
}

// draining reports whether the drain has begun.
func draining(ctx context.Context) bool {
	done, ok := ctx.Value(drainKey{}).(<-chan struct{})
	if !ok {
		// Only the entry point builds request contexts, so a missing
		// signal is a wiring error, as a missing cluster or logger is.
		// Reading absence as "not draining" would hide that error behind
		// a server that dials through its drain.
		panic("httpapi: no drain signal in context")
	}
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// handleLiveness reports that the process is up and serving HTTP. It
// does not look at the cluster.
func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReadiness reports whether this instance may take new traffic.
// It answers 200 or 503 with an empty body and no Retry-After, and the
// cause of a 503 goes to the log line and not the wire.
func handleReadiness(w http.ResponseWriter, r *http.Request) {
	// The drain is checked before the dial, because the answer during a
	// drain is no whatever the cluster would say, and a dial there would
	// cost a session for an answer already known.
	//
	//  1. Answer 503 while draining, without a dial.
	//  2. Dial the cluster and run the readiness query; a failure is 503.
	//  3. 200 with an empty body.
	ctx := r.Context()
	// 1. The question the probe is named for ("may this instance take new
	//    traffic") has the answer no during a drain, and a load balancer
	//    that polls it takes the instance out of rotation before a
	//    listener closes and a connection is refused (ADR-0004, rule 2).
	//    The line logs at debug, because the drain is not a failure and a
	//    prober polls through the whole grace.
	if draining(ctx) {
		observe.Logger(ctx).DebugContext(ctx, "readiness refused: server is draining")
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// 2. Dial as the REST API's own user and run the readiness query,
	//    caching nothing and bypassing the pool, the budget and the
	//    breaker. A failure is 503 with the cause at warn.
	if err := qdb.ClusterFrom(ctx).Probe(ctx); err != nil {
		observe.Logger(ctx).WarnContext(ctx, "readiness probe failed", observe.Err(err))
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	// 3. 200 with an empty body.
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
