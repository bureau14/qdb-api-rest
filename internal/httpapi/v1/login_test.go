package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/httpapi"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// paths are the two spellings of the v1 login.
var paths = []string{"/api/v1/login", "/api/login"}

// newServer is the v1 router over the v2 router, on a context carrying a
// discarding logger, a metrics registry, the HTTP plane, the cluster and
// an ephemeral keychain.
func newServer(t *testing.T, c *qdb.Cluster) (context.Context, http.Handler) {
	t.Helper()
	ctx := observe.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
	ctx = observe.WithMetrics(ctx, observe.NewMetrics(config.Default().Observability.Metrics, false))
	ctx = qdb.WithCluster(observe.WithPlane(ctx, observe.PlaneHTTP), c)
	tk, err := auth.New(ctx, config.Default().Auth, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return auth.WithTokens(ctx, tk), NewHandler(httpapi.NewHandler(config.Observability{}))
}

// login posts body to path without a Content-Type and answers the
// recorded response.
func login(t *testing.T, ctx context.Context, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body)))
	return resp
}

// keysOf decodes a JSON object into its keys and values.
func keysOf(t *testing.T, resp *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var m map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", resp.Body.String(), err)
	}
	return m
}

// refused checks a 401 in the old server's error shape.
func refused(t *testing.T, name string, resp *httptest.ResponseRecorder) {
	t.Helper()
	if resp.Code != http.StatusUnauthorized || resp.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%s: status %d, Content-Type %q: %s", name, resp.Code, resp.Header().Get("Content-Type"), resp.Body.String())
	}
	// The old server's 401 carried a message and no challenge.
	if m := keysOf(t, resp); len(m) != 1 || m["message"] == "" || resp.Header().Get("WWW-Authenticate") != "" {
		t.Fatalf("%s: body %v, challenge %q", name, m, resp.Header().Get("WWW-Authenticate"))
	}
}

// TestLogin: on both clusters and at both spellings, the fixture's user
// gets 200 with a body of one key, token, which opens GET
// /api/v2/session as that user. Every failure is 401 {"message"}.
func TestLogin(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		ctx, h := newServer(t, f.Cluster)
		for _, path := range paths {
			// The request carries no Content-Type, because the v1 login
			// accepts a body under any media type.
			resp := login(t, ctx, h, path, fmt.Sprintf(`{"username":%q,"secret_key":%q}`, f.User.Username, f.User.SecretKey))
			if resp.Code != http.StatusOK {
				t.Fatalf("%s: status %d: %s", path, resp.Code, resp.Body.String())
			}
			// One key, because the refresh token must not reach a v1 client.
			m := keysOf(t, resp)
			if len(m) != 1 || m["token"] == "" {
				t.Fatalf("%s: body %v", path, m)
			}
			// The token opens a v2 data route, because one verifier serves
			// every token.
			req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v2/session", nil)
			req.Header.Set("Authorization", "Bearer "+m["token"])
			session := httptest.NewRecorder()
			h.ServeHTTP(session, req)
			var s struct {
				Username string `json:"username"`
			}
			if err := json.Unmarshal(session.Body.Bytes(), &s); session.Code != http.StatusOK || err != nil || s.Username != f.User.Username {
				t.Fatalf("%s: session status %d: %s", path, session.Code, session.Body.String())
			}
			refused(t, path+" undecodable", login(t, ctx, h, path, `{"username":`))
			// The insecure cluster accepts any secret and the anonymous user.
			if f.Cluster.Secured() {
				refused(t, path+" wrong secret", login(t, ctx, h, path, fmt.Sprintf(`{"username":%q,"secret_key":"bm90LWEta2V5"}`, f.User.Username)))
				refused(t, path+" anonymous", login(t, ctx, h, path, `{}`))
			}
		}
	})
}

// TestLoginUnreachable: a cluster that does not answer is 401 at both
// spellings, as the old server answered.
func TestLoginUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1" // the connection is refused at once
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	ctx, h := newServer(t, c)
	for _, path := range paths {
		refused(t, path, login(t, ctx, h, path, `{}`))
	}
}
