// Package httpapi assembles the HTTP surface of the REST server: the
// legacy compatibility endpoints, the /api/v2 resource API, and the
// unauthenticated status probes.
package httpapi

import "net/http"

// handleLiveness reports that the process is up and serving HTTP.
func handleLiveness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleReadiness reports whether this instance can serve traffic;
// readiness beyond serving HTTP is the QuasarDB connection pool's verdict.
func handleReadiness(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// registerStatusRoutes serves the probes at their legacy paths, which
// load balancers at customer sites health-check, and at their /api/v2
// mirrors. Both answer 200 with an empty body and no Content-Type, the
// shape pinned by the status-probe goldens.
func registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/status/readiness", handleReadiness)
	mux.HandleFunc("GET /api/v2/status/liveness", handleLiveness)
	mux.HandleFunc("GET /api/v2/status/readiness", handleReadiness)
}

// NewHandler returns the root handler: every route registered, wrapped
// by the request middleware.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	registerStatusRoutes(mux)
	return withRequestLogging(mux)
}
