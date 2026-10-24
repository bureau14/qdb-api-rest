// This file holds the query endpoint's test helpers and its compression
// test. The good path in every media type is the round trip
// (roundtrip_test.go), and the error rows live in errors_test.go. The
// encoders' own tests prove the bytes decode, and this file proves
// negotiation and headers.
package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/encoding"
	flightsrv "github.com/bureau14/qdb-api-rest/internal/flightsql"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	qdbflight "github.com/bureau14/qdb-api-rest/internal/qdbtest/flight"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
	"github.com/bureau14/qdb-api-rest/internal/query"
	"github.com/bureau14/qdb-api-rest/internal/tlsconf"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// server is the root handler plus the request context main would give
// it: logger, cluster and keychain, and the user every request acts as,
// with one access token minted for that user. The keychain is kept so a
// test mints what a row needs, such as an expired refresh token. It also
// starts the Flight listener on a port of its own and holds a client on
// it, so the round trip reads one table on both planes (ADR-0018,
// Decision 14).
type server struct {
	ctx     context.Context
	c       *qdb.Cluster
	tk      *auth.Tokens
	handler http.Handler
	// user is who the direct oracle calls and the fixture's cleanups act
	// as, so on the secure cluster they pass the cluster's check.
	user  qdb.User
	token string
	// flight is the fixture's client on the Flight listener.
	flight *flightsql.Client
}

// newServer binds the server to f and acts as f's user.
func newServer(t *testing.T, f cluster.Fixture) server {
	t.Helper()
	return newServerOn(t, f.Cluster, f.User)
}

// newServerOn is newServer over any cluster and user, for the unreachable
// cluster. It starts the Flight listener on 127.0.0.1:0 under the
// ephemeral certificate and stops it on cleanup.
func newServerOn(t *testing.T, c *qdb.Cluster, u qdb.User) server {
	t.Helper()
	tk := tokensAt(t, time.Now())
	ctx := auth.WithTokens(qdb.WithCluster(observeContext(), c), tk)
	// The token carries the user's secret key, because the data routes
	// dial the cluster with the token's credentials.
	token := mint(t, tk, "access", u, time.Now().Add(time.Hour))
	// The ephemeral certificate, as a start with no tls block gets.
	tlsConfig, _, err := tlsconf.Load(config.TLS{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The Flight listener runs under the same ctx as the handler, because
	// both planes read the cluster and the keychain from it.
	fs, err := flightsrv.New(ctx, "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = fs.Serve() }()
	t.Cleanup(func() {
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		fs.Shutdown(drain)
	})
	return server{ctx: ctx, c: c, tk: tk, handler: NewHandler(), user: u, token: token, flight: qdbflight.Dial(t, fs.Addr().String())}
}

// post sends body to path with the given headers, nil meaning none.
func (s server) post(path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(s.ctx, http.MethodPost, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp := httptest.NewRecorder()
	s.handler.ServeHTTP(resp, req)
	return resp
}

// query posts body as the query.
func (s server) query(body string, headers map[string]string) *httptest.ResponseRecorder {
	return s.post("/api/v2/query", body, headers)
}

// direct runs q through query.Run as the server's user and encodes the
// stream with e. The endpoint must answer these exact bytes.
func (s server) direct(t *rapid.T, e encoding.Encoder, q string) []byte {
	t.Helper()
	var buf bytes.Buffer
	// The ctx is the server's, because query.Run reads the cluster from it.
	err := query.Run(s.ctx, s.user, q, func(batches qdb.Batches) error {
		return e.EncodeStream(s.ctx, &buf, batches)
	})
	if err != nil {
		t.Fatalf("%s %s: %v", e.ContentType(), q, err)
	}
	return buf.Bytes()
}

// TestQueryCompressed: on each cluster, for a generated table, each
// coding answers the encoder's own bytes under Content-Encoding, and
// identity stays bare.
func TestQueryCompressed(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		s := newServer(t, f)
		rapid.Check(t, func(rt *rapid.T) {
			// 1. A generated table, created as the cluster's user.
			tbl := table.Generate(rt)
			table.CreateAs(rt, s.c, s.user, tbl)
			// 2. The expected body: the same query run on the cluster
			//    directly and written by the JSON stream encoder, the array
			//    of one result. The requests
			//    below send no Accept header, so JSON is what the endpoint
			//    answers, and once decompressed its body must be these
			//    bytes.
			want := s.direct(rt, encoding.JSON{}, tbl.Select())
			// 3. Each coding answers 200, labels the body with its coding,
			//    and decompresses to the expected body.
			for _, c := range []coding{identityCoding, gzipCoding, zstdCoding} {
				resp := s.query(tbl.Select(), map[string]string{"Authorization": "Bearer " + s.token, "Accept-Encoding": string(c)})
				if resp.Code != http.StatusOK {
					rt.Fatalf("%s: status %d: %s", c, resp.Code, resp.Body.String())
				}
				// Identity is never labelled. A compressed body names its coding.
				wantLabel := string(c)
				if c == identityCoding {
					wantLabel = ""
				}
				if got := resp.Header().Get("Content-Encoding"); got != wantLabel {
					rt.Fatalf("%s: Content-Encoding = %q", c, got)
				}
				if !bytes.Equal(decompress(rt, c, resp.Body.Bytes()), want) {
					rt.Fatalf("%s: body differs from the encoder's own bytes", c)
				}
			}
		})
	})
}
