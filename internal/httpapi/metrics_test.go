package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// TestOperationalRoutes is one table over the two switches: /metrics
// answers 200 with text/plain and the build-info series when on and 404
// when off, and /debug/pprof/ answers 200 when on and 404 when off. It
// runs through NewHandler with a context carrying the metrics and no
// cluster, because neither route reads one.
func TestOperationalRoutes(t *testing.T) {
	rows := []struct {
		name    string
		metrics config.Metrics
		debug   config.Debug
		path    string
		status  int
		media   string // the Content-Type's media type, before its parameters
		body    string // a substring the body carries
	}{
		{"metrics on", config.Metrics{Enabled: true}, config.Debug{}, "/metrics", http.StatusOK, "text/plain", "\nqdb_rest_build_info{"},
		// Off, the route is not registered, so the body is the mux's
		// plain text, as every unknown path answers.
		{"metrics off", config.Metrics{}, config.Debug{}, "/metrics", http.StatusNotFound, "text/plain", "404 page not found"},
		{"pprof on", config.Metrics{}, config.Debug{Pprof: true}, "/debug/pprof/", http.StatusOK, "text/html", "Types of profiles available"},
		{"pprof off", config.Metrics{}, config.Debug{}, "/debug/pprof/", http.StatusNotFound, "text/plain", "404 page not found"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			// observeContext places the metrics, and no cluster is placed,
			// because the probes are not requested.
			req := httptest.NewRequestWithContext(observeContext(), http.MethodGet, row.path, nil)
			resp := httptest.NewRecorder()
			NewHandler(row.metrics, row.debug).ServeHTTP(resp, req)
			if resp.Code != row.status {
				t.Fatalf("%s = %d, want %d: %s", row.path, resp.Code, row.status, resp.Body.String())
			}
			if !strings.Contains(resp.Body.String(), row.body) {
				t.Fatalf("%s body lacks %q:\n%s", row.path, row.body, resp.Body.String())
			}
			// The exposition's Content-Type carries version and charset
			// parameters, so the assertion is on the media type alone.
			if ct := resp.Header().Get("Content-Type"); !strings.HasPrefix(ct, row.media) {
				t.Fatalf("%s Content-Type = %q, want %s", row.path, ct, row.media)
			}
		})
	}
}
