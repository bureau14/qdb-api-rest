package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// TestOperationalRoutes is one table over the metrics switch: /metrics
// answers 200 with text/plain and the build-info series when on, and
// 404 when off. It runs through NewHandler with a context carrying the
// metrics and no cluster, because the route reads none.
func TestOperationalRoutes(t *testing.T) {
	rows := []struct {
		name    string
		metrics config.Metrics
		path    string
		status  int
		body    string // a substring the body carries
	}{
		{"metrics on", config.Metrics{Enabled: true}, "/metrics", http.StatusOK, "\nqdb_rest_build_info{"},
		// Off, the route is not registered, so the body is the mux's
		// plain text, as every unknown path answers.
		{"metrics off", config.Metrics{}, "/metrics", http.StatusNotFound, "404 page not found"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			// observeContext places the metrics, and no cluster is placed,
			// because the probes are not requested.
			req := httptest.NewRequestWithContext(observeContext(), http.MethodGet, row.path, nil)
			resp := httptest.NewRecorder()
			NewHandler(row.metrics).ServeHTTP(resp, req)
			if resp.Code != row.status {
				t.Fatalf("%s = %d, want %d: %s", row.path, resp.Code, row.status, resp.Body.String())
			}
			if !strings.Contains(resp.Body.String(), row.body) {
				t.Fatalf("%s body lacks %q:\n%s", row.path, row.body, resp.Body.String())
			}
			// The exposition's Content-Type carries version and charset
			// parameters, so the assertion is on the media type alone.
			if row.status == http.StatusOK && !strings.HasPrefix(resp.Header().Get("Content-Type"), "text/plain") {
				t.Fatalf("%s Content-Type = %q, want text/plain", row.path, resp.Header().Get("Content-Type"))
			}
		})
	}
}
