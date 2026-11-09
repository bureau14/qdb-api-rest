// Package v1 serves the old server's login and query at /api/v1/<path>
// and at their unversioned aliases. Each route calls its v2 endpoint in
// process and converts the answer into the old server's wire shape, so
// v1 and v2 share one implementation. It is the only package that knows
// that shape.
package v1

import (
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/httpapi"
)

// NewHandler builds the v1 router over the v2 router. It serves each v1
// route at both spellings, compressed as a v2 route is, and hands every
// other request to v2.
func NewHandler(v2 http.Handler) http.Handler {
	mux := http.NewServeMux()
	// Each spelling records under its own path, so the metrics show which
	// spelling clients send. The compressor sits outside the handler, so
	// the handler writes the old body plain and the writer labels and
	// compresses it, and the duration the handler records includes the
	// compression.
	mux.Handle("POST /api/v1/login", httpapi.WithCompression(loginHandler(v2, "/api/v1/login")))
	mux.Handle("POST /api/login", httpapi.WithCompression(loginHandler(v2, "/api/login")))
	mux.Handle("POST /api/v1/query", httpapi.WithCompression(queryHandler(v2, "/api/v1/query")))
	mux.Handle("POST /api/query", httpapi.WithCompression(queryHandler(v2, "/api/query")))
	// The v2 router keeps its own 404 and 405 for every path that is not
	// a v1 route.
	mux.Handle("/", v2)
	return mux
}
