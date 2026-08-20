// Package httpapi assembles the HTTP surface of the REST server: the
// legacy compatibility endpoints, the /api/v2 resource API, and the
// unauthenticated status probes.
package httpapi

import "net/http"

// Both probes answer 200 with an empty body and no Content-Type; load
// balancers at customer sites health-check the legacy paths, and the wire
// shape is pinned by the legacy status-probe goldens.

// handleLiveness reports that the process is up and serving HTTP.
func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReadiness reports whether this instance can serve traffic; it gains
// a QuasarDB dependency check once the connection pool exists.
func handleReadiness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// registerStatusRoutes serves the probes at their legacy paths and their
// /api/v2 mirrors.
func registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/status/readiness", handleReadiness)
	mux.HandleFunc("GET /api/v2/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/v2/status/readiness", handleReadiness)
}

// NewHandler returns the root handler with every route registered.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	registerStatusRoutes(mux)
	return mux
}
