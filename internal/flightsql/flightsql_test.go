// This file holds the Flight listener's tests: the Handshake, the bearer
// edge and the anonymous rule, driven through arrow-go's Flight SQL client
// on both clusters. The statement query and the catalog commands get their
// tests when they land.
package flightsql

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/tlsconf"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// observeContext is a context with a discarding logger.
func observeContext() context.Context {
	return observe.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))
}

// tokensAt builds a keychain whose clock is fixed at now. It uses the
// default auth config, so the key is ephemeral and argon2id costs nothing.
func tokensAt(t *testing.T, now time.Time) *auth.Tokens {
	t.Helper()
	tk, err := auth.New(observeContext(), config.Default().Auth, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return tk
}

// mint seals a token of kind typ for u, expiring at exp.
func mint(t *testing.T, tk *auth.Tokens, typ string, u qdb.User, exp time.Time) string {
	t.Helper()
	token, err := tk.Mint(auth.Claims{Username: u.Username, SecretKey: u.SecretKey, Typ: typ, SessionID: "sid-1", ExpiresAt: exp.Unix()})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// server is the Flight server on a port of its own over the context main
// would give it, with an ephemeral certificate, plus the fixture's user
// and the keychain, so a test mints what a row needs.
type server struct {
	ctx  context.Context
	tk   *auth.Tokens
	srv  *Server
	user qdb.User
}

// newServerOn starts the server over c as u on 127.0.0.1:0 under the
// keychain tk, and stops it on cleanup.
func newServerOn(t *testing.T, c *qdb.Cluster, u qdb.User, tk *auth.Tokens) server {
	t.Helper()
	ctx := auth.WithTokens(qdb.WithCluster(observeContext(), c), tk)
	// The ephemeral certificate, as a start with no tls block gets.
	tlsConfig, _, err := tlsconf.Load(config.TLS{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(ctx, "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() {
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(drain)
	})
	return server{ctx: ctx, tk: tk, srv: srv, user: u}
}

// newServer starts the server for f and acts as f's user.
func newServer(t *testing.T, f cluster.Fixture) server {
	t.Helper()
	return newServerOn(t, f.Cluster, f.User, tokensAt(t, time.Now()))
}

// dial opens arrow-go's Flight SQL client on the server, with certificate
// verification off because the certificate is ephemeral, as the driver
// smoke tests connect.
func (s server) dial(t *testing.T) *flightsql.Client {
	t.Helper()
	creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // the test certificate is ephemeral
	cl, err := flightsql.NewClient(s.srv.Addr().String(), nil, nil, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	return cl
}

// bearerOf reads the Bearer token the client will present from the ctx
// AuthenticateBasicToken answered.
func bearerOf(t *testing.T, ctx context.Context) string {
	t.Helper()
	md, _ := metadata.FromOutgoingContext(ctx)
	vals := md.Get("authorization")
	if len(vals) != 1 || len(vals[0]) < len("Bearer ") || vals[0][:len("Bearer ")] != "Bearer " {
		t.Fatalf("want one Bearer authorization header, got %q", vals)
	}
	return vals[0][len("Bearer "):]
}

// code answers the gRPC code of err, OK for nil.
func code(err error) codes.Code { return status.Code(err) }

// withHeader is ctx with one authorization header.
func withHeader(ctx context.Context, authorization string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", authorization)
}

// TestHandshake: a Handshake with the fixture's credentials answers an
// access token for that user, the token opens a call, no token is the
// anonymous user on the insecure cluster and refused on the secure one,
// and a wrong secret is refused where the cluster checks it.
func TestHandshake(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		s := newServer(t, f)
		cl := s.dial(t)
		ctx := s.ctx

		// 1. The Handshake answers a Bearer the keychain verifies as an
		//    access token for the fixture's user.
		authCtx, err := cl.Client.AuthenticateBasicToken(ctx, s.user.Username, s.user.SecretKey)
		if err != nil {
			t.Fatalf("handshake: %v", err)
		}
		c, err := s.tk.Verify(bearerOf(t, authCtx))
		if err != nil || c.Typ != "access" || c.Username != s.user.Username {
			t.Fatalf("want an access token for %q, got %+v, %v", s.user.Username, c, err)
		}

		// 2. The bearer passes the edge, and Execute answers an info.
		_, err = cl.Execute(authCtx, "select 1")
		if code(err) != codes.OK {
			t.Fatalf("with bearer: want OK, got %v", err)
		}

		// 3. No header: anonymous on the insecure cluster, refused on the
		//    secure one (ADR-0018, Decision 8).
		_, err = cl.Execute(ctx, "select 1")
		want := codes.OK
		if f.Cluster.Secured() {
			want = codes.Unauthenticated
		}
		if code(err) != want {
			t.Fatalf("no header: want %v, got %v", want, err)
		}

		// 4. A wrong secret is refused by the secure cluster only, because
		//    the insecure cluster checks no credential.
		if f.Cluster.Secured() {
			_, err = cl.Client.AuthenticateBasicToken(ctx, s.user.Username, "wrong")
			if code(err) != codes.Unauthenticated {
				t.Fatalf("wrong secret: want UNAUTHENTICATED, got %v", err)
			}
		}
	})
}

// TestBearerRows: every way a presented credential fails the edge answers
// UNAUTHENTICATED with the detail naming which. The rows run on the
// insecure cluster, because none of them reaches the cluster.
func TestBearerRows(t *testing.T) {
	f := cluster.Bind(t, qdbtest.Insecure(), nil)
	tk := tokensAt(t, epoch)
	s := newServerOn(t, f.Cluster, f.User, tk)
	cl := s.dial(t)
	expired := mint(t, tk, "access", f.User, epoch.Add(-time.Minute))
	refresh := mint(t, tk, "refresh", f.User, epoch.Add(time.Hour))
	valid := mint(t, tk, "access", f.User, epoch.Add(time.Hour))
	for _, row := range []struct{ name, header, want string }{
		{"expired", "Bearer " + expired, "token expired"},
		{"refresh token", "Bearer " + refresh, "not an access token"},
		{"tampered", "Bearer " + valid[:len(valid)-2] + "xx", "invalid token"},
		{"wrong scheme", "Token " + valid, "malformed bearer credentials"},
		{"empty value", "Bearer ", "malformed bearer credentials"},
		{"empty header", "", "malformed bearer credentials"},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, err := cl.Execute(withHeader(s.ctx, row.header), "select 1")
			if st := status.Convert(err); st.Code() != codes.Unauthenticated || st.Message() != row.want {
				t.Fatalf("want UNAUTHENTICATED %q, got %v", row.want, err)
			}
		})
	}
	// The same valid token passes, which proves the rows fail for their
	// own reason and not for the keychain's.
	if _, err := cl.Execute(withHeader(s.ctx, "Bearer "+valid), "select 1"); code(err) != codes.OK {
		t.Fatalf("valid token: want OK, got %v", err)
	}
}

// epoch is the fixed clock the bearer rows are judged against.
var epoch = time.Unix(1_700_000_000, 0)
