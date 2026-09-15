// The query endpoint is pinned against the live qdbd fixture: for a
// generated table, each media type answers 200 with the encoder's
// Content-Type and the bytes the encoder writes when run directly over
// Cluster.Query; the error rows of the handler are checked one by one on
// the same fixture. The encoders' own tests prove the bytes decode; this
// file proves routing, negotiation and headers.
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// server is the root handler plus the request context main would give
// it: logger, cluster and keychain.
type server struct {
	ctx     context.Context
	c       *qdb.Cluster
	handler http.Handler
	token   string // an anonymous access token
}

// newServer binds the fixture cluster and mints one anonymous access
// token, the caller every query here runs as.
func newServer(t *testing.T) server {
	t.Helper()
	c := cluster.New(t)
	tk := tokensAt(t, time.Now())
	ctx := auth.WithTokens(qdb.WithCluster(observeContext(), c), tk)
	return server{ctx: ctx, c: c, handler: NewHandler(), token: mint(t, tk, "access", "", time.Now().Add(time.Hour))}
}

// query posts body as the query with the given headers, nil meaning none.
func (s server) query(body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(s.ctx, http.MethodPost, "/api/v2/query", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp := httptest.NewRecorder()
	s.handler.ServeHTTP(resp, req)
	return resp
}

// direct runs q over the cluster and encodes it with e, the bytes the
// endpoint must match.
func (s server) direct(t *rapid.T, e encoding.Encoder, q string) []byte {
	t.Helper()
	rec, err := s.c.Query(context.Background(), qdb.User{}, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	if rec != nil {
		defer rec.Release()
	}
	var buf bytes.Buffer
	if err := e.Encode(context.Background(), &buf, rec); err != nil {
		t.Fatalf("%s: %v", e.ContentType(), err)
	}
	return buf.Bytes()
}

// TestQueryPerMediaType: for a generated table, each Accept answers 200,
// the encoder's Content-Type, and the encoder's own bytes.
func TestQueryPerMediaType(t *testing.T) {
	s := newServer(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, s.c, tbl)
		for accept, e := range encoders {
			resp := s.query(tbl.Select(), map[string]string{"Authorization": "Bearer " + s.token, "Accept": accept})
			if resp.Code != http.StatusOK {
				rt.Fatalf("%s: status %d: %s", accept, resp.Code, resp.Body.String())
			}
			if ct := resp.Header().Get("Content-Type"); ct != e.ContentType() {
				rt.Fatalf("%s: Content-Type = %q", accept, ct)
			}
			if want := s.direct(rt, e, tbl.Select()); !bytes.Equal(resp.Body.Bytes(), want) {
				rt.Fatalf("%s: body differs from the encoder's own bytes", accept)
			}
		}
	})
}

// TestQueryErrors: each error row answers its status with a problem body
// and, where the status has one, its header.
func TestQueryErrors(t *testing.T) {
	s := newServer(t)
	bearer := "Bearer " + s.token
	cases := map[string]struct {
		body      string
		headers   map[string]string
		status    int
		challenge string
	}{
		"invalid query": {"NOT A QUERY", map[string]string{"Authorization": bearer}, http.StatusBadRequest, ""},
		"json body":     {`{"query":"SELECT 1"}`, map[string]string{"Authorization": bearer, "Content-Type": "application/json"}, http.StatusUnsupportedMediaType, ""},
		"no token":      {"SELECT 1", nil, http.StatusUnauthorized, "Bearer"},
		"bad token":     {"SELECT 1", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized, `Bearer error="invalid_token"`},
	}
	for name, tc := range cases {
		resp := s.query(tc.body, tc.headers)
		if resp.Code != tc.status {
			t.Errorf("%s: status %d, want %d: %s", name, resp.Code, tc.status, resp.Body.String())
		}
		if got := resp.Header().Get("WWW-Authenticate"); got != tc.challenge {
			t.Errorf("%s: WWW-Authenticate = %q, want %q", name, got, tc.challenge)
		}
		// The body is a problem whose status repeats the status line and
		// whose title is its standard text.
		var p problem
		if err := json.Unmarshal(resp.Body.Bytes(), &p); err != nil || p.Status != tc.status || p.Title != http.StatusText(tc.status) {
			t.Errorf("%s: problem body %s (%v)", name, resp.Body.String(), err)
		}
	}
}
