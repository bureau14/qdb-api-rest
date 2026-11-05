// Package v1 serves the old server's login and query at /api/v1/<path>
// and at their unversioned aliases. Each route calls its v2 endpoint in
// process and converts the answer into the old server's wire shape, so
// v1 and v2 share one implementation. It is the only package that knows
// that shape.
package v1

import "net/http"

// NewHandler builds the v1 router over the v2 router. It serves each v1
// route at both spellings and hands every other request to v2.
func NewHandler(v2 http.Handler) http.Handler {
	mux := http.NewServeMux()
	// Each spelling records under its own path, so the metrics show which
	// spelling clients send.
	mux.Handle("POST /api/v1/login", loginHandler(v2, "/api/v1/login"))
	mux.Handle("POST /api/login", loginHandler(v2, "/api/login"))
	// The v2 router keeps its own 404 and 405 for every path that is not
	// a v1 route.
	mux.Handle("/", v2)
	return mux
}
