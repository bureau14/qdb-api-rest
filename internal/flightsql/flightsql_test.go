// This file holds the Flight listener's tests: the Handshake, the bearer
// edge, the anonymous rule and the statement query, driven through
// arrow-go's Flight SQL client on both clusters. The catalog commands get
// their tests when they land.
package flightsql

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
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

// failer is the slice of testing.TB the helpers below need, so *testing.T
// and *rapid.T both serve.
type failer interface {
	Helper()
	Fatalf(string, ...any)
}

// readBatches reads the one endpoint of info to the end through DoGet and
// answers the stream's schema and its batches, retained, which the caller
// releases. It fails on any error of the stream.
func readBatches(t failer, cl *flightsql.Client, ctx context.Context, info *flight.FlightInfo) (*arrow.Schema, []arrow.RecordBatch) {
	t.Helper()
	if len(info.Endpoint) != 1 {
		t.Fatalf("want one endpoint, got %d", len(info.Endpoint))
	}
	rdr, err := cl.DoGet(ctx, info.Endpoint[0].Ticket)
	if err != nil {
		t.Fatalf("DoGet: %v", err)
	}
	defer rdr.Release()
	var recs []arrow.RecordBatch
	for rdr.Next() {
		rec := rdr.RecordBatch()
		rec.Retain()
		recs = append(recs, rec)
	}
	if err := rdr.Err(); err != nil {
		t.Fatalf("DoGet stream: %v", err)
	}
	return rdr.Schema(), recs
}

// readAll reads the one endpoint of info to the end through DoGet and
// fails on any error.
func readAll(t *testing.T, cl *flightsql.Client, ctx context.Context, info *flight.FlightInfo) {
	t.Helper()
	_, recs := readBatches(t, cl, ctx, info)
	release(recs)
}

// release releases every batch of recs.
func release(recs []arrow.RecordBatch) {
	for _, r := range recs {
		r.Release()
	}
}

// concat joins recs, which share schema, into one batch the caller
// releases. No batch is an empty batch of schema.
func concat(t failer, schema *arrow.Schema, recs []arrow.RecordBatch) arrow.RecordBatch {
	t.Helper()
	cols := make([]arrow.Array, schema.NumFields())
	rows := int64(0)
	for i := range cols {
		parts := make([]arrow.Array, len(recs))
		for n, r := range recs {
			parts[n] = r.Column(i)
		}
		if len(parts) == 0 {
			cols[i] = array.MakeArrayOfNull(memory.DefaultAllocator, schema.Field(i).Type, 0)
			continue
		}
		col, err := array.Concatenate(parts, memory.DefaultAllocator)
		if err != nil {
			t.Fatalf("concat: %v", err)
		}
		cols[i] = col
		rows = int64(col.Len())
	}
	rec := array.NewRecordBatch(schema, cols, rows)
	for _, c := range cols {
		c.Release()
	}
	return rec
}

// execute runs q over Flight as a whole: Execute, then DoGet on the one
// endpoint, joined into one batch the caller releases.
func execute(t failer, cl *flightsql.Client, ctx context.Context, q string) arrow.RecordBatch {
	t.Helper()
	info, err := cl.Execute(ctx, q)
	if err != nil {
		t.Fatalf("Execute %s: %v", q, err)
	}
	schema, recs := readBatches(t, cl, ctx, info)
	defer release(recs)
	return concat(t, schema, recs)
}

// duck runs q on DuckDB over the cluster directly, as u, and answers its
// batches joined into one batch the caller releases.
func duck(t failer, c *qdb.Cluster, u qdb.User, q string) arrow.RecordBatch {
	t.Helper()
	var schema *arrow.Schema
	var recs []arrow.RecordBatch
	err := c.Duck(context.Background(), u, q, func(batches qdb.Batches) error {
		for rec, err := range batches {
			if err != nil {
				return err
			}
			// The batch owns its buffers, so a Retain keeps it past yield.
			rec.Retain()
			recs = append(recs, rec)
			schema = rec.Schema()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("duckdb %s: %v", q, err)
	}
	defer release(recs)
	return concat(t, schema, recs)
}

// sameBatch reports whether got and want carry equal schemas and equal
// columns, by array equality, which reads no byte under a null slot.
func sameBatch(got, want arrow.RecordBatch) bool {
	if !got.Schema().Equal(want.Schema()) || got.NumRows() != want.NumRows() {
		return false
	}
	for i := range int(got.NumCols()) {
		if !array.Equal(got.Column(i), want.Column(i)) {
			return false
		}
	}
	return true
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

		// 2. The bearer passes the edge: Execute answers an info, and DoGet
		//    on its ticket reads to the end with no error.
		info, err := cl.Execute(authCtx, "select 1")
		if code(err) != codes.OK {
			t.Fatalf("with bearer: want OK, got %v", err)
		}
		readAll(t, cl, authCtx, info)

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
	info, err := cl.Execute(withHeader(s.ctx, "Bearer "+valid), "select 1")
	if code(err) != codes.OK {
		t.Fatalf("valid token: want OK, got %v", err)
	}
	readAll(t, cl, withHeader(s.ctx, "Bearer "+valid), info)
}

// epoch is the fixed clock the bearer rows are judged against.
var epoch = time.Unix(1_700_000_000, 0)

// TestStatement: for a generated table on each cluster, the native query
// read back over Flight equals the rows generated, column by column, and
// the #sql query read back over Flight equals the batches Cluster.Duck
// answers directly, decoded. A statement without a result set streams
// one schema and no batch.
func TestStatement(t *testing.T) {
	// Two engines, one oracle each, both clusters:
	//
	//  1. one server and client per cluster, a bearer from a Handshake,
	//     because the statement runs as the token's user;
	//  2. draw and create a table as the fixture's user;
	//  3. native: Execute(tbl.Select()), the batches joined, Check against
	//     the table;
	//  4. #sql: Execute("#sql " + tbl.SelectSQL()), compared decoded with
	//     Cluster.Duck's batches over the same SQL, because the bytes under
	//     a null slot are undefined;
	//  5. an empty result: a DROP TABLE streams one schema and no batch,
	//     and the reader reaches the end with no error.
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		// 1. the server and the bearer
		s := newServer(t, f)
		cl := s.dial(t)
		ctx, err := cl.Client.AuthenticateBasicToken(s.ctx, s.user.Username, s.user.SecretKey)
		if err != nil {
			t.Fatalf("handshake: %v", err)
		}
		rapid.Check(t, func(rt *rapid.T) {
			// 2. the table. The ranged Select is not needed, because a query
			//    opens no bulk reader.
			tbl := table.Generate(rt)
			table.CreateAs(rt, f.Cluster, f.User, tbl)

			// 3. native, against the rows generated
			got := execute(rt, cl, ctx, tbl.Select())
			table.Check(rt, tbl, got)
			got.Release()

			// 4. #sql, against DuckDB directly
			got = execute(rt, cl, ctx, "#sql "+tbl.SelectSQL())
			want := duck(rt, f.Cluster, f.User, tbl.SelectSQL())
			if !sameBatch(got, want) {
				rt.Fatalf("#sql over Flight differs from Cluster.Duck:\n%v\n%v", got, want)
			}
			got.Release()
			want.Release()

			// 5. no result set: the schema has no fields and no batch follows
			info, err := cl.Execute(ctx, "DROP TABLE "+tbl.Name)
			if err != nil {
				rt.Fatalf("drop: %v", err)
			}
			schema, recs := readBatches(rt, cl, ctx, info)
			if schema.NumFields() != 0 || len(recs) != 0 {
				rt.Fatalf("drop: want a schema of no fields and no batch, got %v and %d batches", schema, len(recs))
			}
		})
	})
}

// doGet runs q as the one endpoint's ticket of an Execute, and answers
// DoGet's error, which is where a statement's failure surfaces, because
// GetFlightInfo runs nothing.
func doGet(t *testing.T, cl *flightsql.Client, ctx context.Context, q string) error {
	t.Helper()
	info, err := cl.Execute(ctx, q)
	if err != nil {
		t.Fatalf("Execute %s: %v", q, err)
	}
	return doGetTicket(t, cl, ctx, info.Endpoint[0].Ticket)
}

// doGetTicket presents ticket on DoGet and answers the stream's error,
// read to the end.
func doGetTicket(t *testing.T, cl *flightsql.Client, ctx context.Context, ticket *flight.Ticket) error {
	t.Helper()
	rdr, err := cl.DoGet(ctx, ticket)
	if err != nil {
		return err
	}
	defer rdr.Release()
	for rdr.Next() {
	}
	return rdr.Err()
}

// retryInfo answers the RetryInfo detail of err's status, nil without one.
func retryInfo(err error) *errdetails.RetryInfo {
	for _, d := range status.Convert(err).Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok {
			return ri
		}
	}
	return nil
}

// TestStatementRows: every way a statement fails before the stream
// answers its code with the engine's or the cluster's message. The rows
// run on the insecure cluster, because none reaches a secured check.
func TestStatementRows(t *testing.T) {
	f := cluster.Bind(t, qdbtest.Insecure(), nil)
	s := newServer(t, f)
	cl := s.dial(t)
	// A native parse error and a #sql parse error are INVALID_ARGUMENT with
	// the engine's message, a query naming an unknown table is
	// INVALID_ARGUMENT, and a DoGet on a hand-built ticket of a bad query
	// is the same, because the ticket is the text.
	for _, row := range []struct{ name, query string }{
		{"native parse error", "selec 1"},
		{"#sql parse error", "#sql selec 1"},
		{"unknown table", "SELECT * FROM qdbtest_no_such_table"},
	} {
		t.Run(row.name, func(t *testing.T) {
			err := doGet(t, cl, s.ctx, row.query)
			if st := status.Convert(err); st.Code() != codes.InvalidArgument || st.Message() == "" {
				t.Fatalf("want INVALID_ARGUMENT with a message, got %v", err)
			}
		})
	}
	t.Run("hand-built ticket", func(t *testing.T) {
		ticket, err := flightsql.CreateStatementQueryTicket([]byte("selec 1"))
		if err != nil {
			t.Fatal(err)
		}
		err = doGetTicket(t, cl, s.ctx, &flight.Ticket{Ticket: ticket})
		if st := status.Convert(err); st.Code() != codes.InvalidArgument || st.Message() == "" {
			t.Fatalf("want INVALID_ARGUMENT with a message, got %v", err)
		}
	})
}

// TestStatementUnreachable: a server over a cluster that does not answer
// is UNAVAILABLE with no detail, and once the breaker opens, UNAVAILABLE
// with a RetryInfo detail whose delay is the breaker's.
func TestStatementUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1" // the connection is refused at once
	// Two failures open the breaker, so the first statement answers the
	// unreachable cluster itself whether or not the read retry ran, and a
	// later one meets the open breaker.
	cfg.Pool.Breaker.Failures = 2
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	s := newServerOn(t, c, qdb.User{}, tokensAt(t, time.Now()))
	cl := s.dial(t)
	err := doGet(t, cl, s.ctx, "select 1")
	if code(err) != codes.Unavailable || retryInfo(err) != nil {
		t.Fatalf("unreachable: want UNAVAILABLE with no detail, got %v", err)
	}
	// The breaker opens on the second failure, which the first statement's
	// retry may already have been, so the open breaker shows within two
	// more statements.
	var ri *errdetails.RetryInfo
	for range 2 {
		err = doGet(t, cl, s.ctx, "select 1")
		if code(err) != codes.Unavailable {
			t.Fatalf("want UNAVAILABLE, got %v", err)
		}
		if ri = retryInfo(err); ri != nil {
			break
		}
	}
	if ri == nil {
		t.Fatalf("open breaker: want a RetryInfo detail, got %v", err)
	}
	if d := ri.RetryDelay.AsDuration(); d <= 0 || d > cfg.Pool.Breaker.OpenFor {
		t.Fatalf("retry delay %s, want within (0, %s]", d, cfg.Pool.Breaker.OpenFor)
	}
}
