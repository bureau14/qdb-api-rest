// The login endpoint against the live fixtures: a minted token opens the
// query endpoint, the secure cluster's verdict on the credentials is the
// status, and the error rows answer problems.
package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
)

// login posts a JSON login body.
func (s server) login(body string) *httptest.ResponseRecorder {
	return s.post("/api/v2/auth/login", body, map[string]string{"Content-Type": "application/json"})
}

// tokenOf decodes a 200 login into its token response.
func tokenOf(t *testing.T, resp *httptest.ResponseRecorder) tokenResponse {
	t.Helper()
	if resp.Code != http.StatusOK || resp.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("login: status %d, Content-Type %q: %s", resp.Code, resp.Header().Get("Content-Type"), resp.Body.String())
	}
	var tr tokenResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	return tr
}

// TestLoginThenQuery: an anonymous login on the insecure cluster answers
// the RFC 6749 shape, and its token opens the query endpoint.
func TestLoginThenQuery(t *testing.T) {
	s := newServer(t)
	tr := tokenOf(t, s.login(`{"username":"","secret_key":""}`))
	if tr.TokenType != "Bearer" || tr.AccessToken == "" || tr.ExpiresIn != int64(config.Default().Auth.AccessTTL.Seconds()) {
		t.Fatalf("token response %+v", tr)
	}
	if resp := s.query("SELECT 1", map[string]string{"Authorization": "Bearer " + tr.AccessToken}); resp.Code != http.StatusOK {
		t.Fatalf("query with the minted token: status %d: %s", resp.Code, resp.Body.String())
	}
}

// TestLoginVerdicts: the secure cluster accepts its user and refuses a
// wrong secret with 401 and no challenge; the body rows answer 415 and
// 400.
func TestLoginVerdicts(t *testing.T) {
	s := newServerOn(t, cluster.NewSecure(t))
	name, secret := qdbtest.User(t)
	tokenOf(t, s.login(fmt.Sprintf(`{"username":%q,"secret_key":%q}`, name, secret)))
	cases := map[string]struct {
		resp   *httptest.ResponseRecorder
		status int
	}{
		"wrong secret":  {s.login(fmt.Sprintf(`{"username":%q,"secret_key":"bm90LWEta2V5"}`, name)), http.StatusUnauthorized},
		"text body":     {s.post("/api/v2/auth/login", "username=x", map[string]string{"Content-Type": "text/plain"}), http.StatusUnsupportedMediaType},
		"not json":      {s.login(`{"username":`), http.StatusBadRequest},
		"not an object": {s.login(`"someone"`), http.StatusBadRequest},
	}
	for n, tc := range cases {
		if tc.resp.Code != tc.status {
			t.Errorf("%s: status %d, want %d: %s", n, tc.resp.Code, tc.status, tc.resp.Body.String())
		}
		if got := tc.resp.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("%s: unexpected challenge %q", n, got)
		}
		var p problem
		if err := json.Unmarshal(tc.resp.Body.Bytes(), &p); err != nil || p.Status != tc.status {
			t.Errorf("%s: problem body %s (%v)", n, tc.resp.Body.String(), err)
		}
	}
}

// TestLoginUnreachable: a cluster that does not answer is 503, and once
// the breaker opens the next login carries Retry-After.
func TestLoginUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1" // connection refused, fast
	cfg.Pool.Breaker.Failures = 1
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	s := newServerOn(t, c)
	first := s.login(`{}`)
	if first.Code != http.StatusServiceUnavailable || first.Header().Get("Retry-After") != "" {
		t.Fatalf("unreachable: status %d, Retry-After %q", first.Code, first.Header().Get("Retry-After"))
	}
	second := s.login(`{}`)
	if second.Code != http.StatusServiceUnavailable || second.Header().Get("Retry-After") == "" {
		t.Fatalf("breaker open: status %d, Retry-After %q", second.Code, second.Header().Get("Retry-After"))
	}
}
