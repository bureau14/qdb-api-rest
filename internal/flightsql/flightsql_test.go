// This file holds the Flight listener's tests: the Handshake, the bearer
// edge, the anonymous rule, the statement query, GetSqlInfo and the
// catalog commands, driven through arrow-go's Flight SQL client on both
// clusters.
package flightsql

import (
	"bytes"
	"context"
	"crypto/tls"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	pb "github.com/apache/arrow-go/v18/arrow/flight/gen/flight"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
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

// ticketOf runs Execute for q and answers the one endpoint's ticket.
// GetFlightInfo runs nothing, so a statement's failure surfaces on DoGet.
func ticketOf(t failer, cl *flightsql.Client, ctx context.Context, q string) *flight.Ticket {
	t.Helper()
	info, err := cl.Execute(ctx, q)
	if err != nil {
		t.Fatalf("Execute %s: %v", q, err)
	}
	if len(info.Endpoint) != 1 {
		t.Fatalf("want one endpoint, got %d", len(info.Endpoint))
	}
	return info.Endpoint[0].Ticket
}

// readAll presents ticket on DoGet and reads the stream to the end: its
// schema, its batches retained for the caller to release, and the error
// that ended it.
func readAll(t failer, cl *flightsql.Client, ctx context.Context, ticket *flight.Ticket) (*arrow.Schema, []arrow.RecordBatch, error) {
	t.Helper()
	rdr, err := cl.DoGet(ctx, ticket)
	if err != nil {
		return nil, nil, err
	}
	defer rdr.Release()
	var recs []arrow.RecordBatch
	for rdr.Next() {
		rec := rdr.RecordBatch()
		rec.Retain()
		recs = append(recs, rec)
	}
	return rdr.Schema(), recs, rdr.Err()
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

// execute runs q over Flight as a whole, Execute then DoGet, and answers
// the result joined into one batch the caller releases.
func execute(t failer, cl *flightsql.Client, ctx context.Context, q string) arrow.RecordBatch {
	t.Helper()
	schema, recs, err := readAll(t, cl, ctx, ticketOf(t, cl, ctx, q))
	if err != nil {
		t.Fatalf("DoGet %s: %v", q, err)
	}
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

		// 2. The bearer passes the edge, so Execute answers an info. The
		//    statement itself is TestStatement's.
		if _, err := cl.Execute(authCtx, "select 1"); code(err) != codes.OK {
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

// TestStatement: for a generated table on each cluster, the native query
// read back over Flight equals the rows generated, column by column, and
// the #sql query read back over Flight equals the batches Cluster.Duck
// answers directly, decoded. A statement without a result set streams
// one schema and no batch. The two engines are compared with their own
// oracles and not with each other; the HTTP format-equivalence property
// extended with a Flight client is a later unit's.
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
			schema, recs, err := readAll(rt, cl, ctx, ticketOf(rt, cl, ctx, "DROP TABLE "+tbl.Name))
			if err != nil || schema.NumFields() != 0 || len(recs) != 0 {
				rt.Fatalf("drop: want a schema of no fields and no batch, got %v, %d batches, %v", schema, len(recs), err)
			}
		})
	})
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

// statementErr runs q as a statement, Execute then DoGet, and answers the
// error that ends the stream.
func statementErr(t *testing.T, cl *flightsql.Client, ctx context.Context, q string) error {
	t.Helper()
	_, recs, err := readAll(t, cl, ctx, ticketOf(t, cl, ctx, q))
	release(recs)
	return err
}

// TestStatementRows: every way a statement fails before the stream
// answers its code with the engine's or the cluster's message, and an
// unreachable cluster answers UNAVAILABLE, with a RetryInfo detail once
// the breaker opens, for a statement and for a table listing alike. The
// rows run on the insecure cluster, because none reaches a secured check.
func TestStatementRows(t *testing.T) {
	// 1. the engine's rows: a native parse error and a #sql parse error
	//    are INVALID_ARGUMENT with the engine's message, a query naming an
	//    unknown table is INVALID_ARGUMENT, and a DoGet on a hand-built
	//    ticket of a bad query is the same, because the ticket is the text
	f := cluster.Bind(t, qdbtest.Insecure(), nil)
	s := newServer(t, f)
	cl := s.dial(t)
	badTicket, err := flightsql.CreateStatementQueryTicket([]byte("selec 1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name string
		err  error
	}{
		{"native parse error", statementErr(t, cl, s.ctx, "selec 1")},
		{"#sql parse error", statementErr(t, cl, s.ctx, "#sql selec 1")},
		{"unknown table", statementErr(t, cl, s.ctx, "SELECT * FROM qdbtest_no_such_table")},
		{"hand-built ticket", func() error {
			_, recs, err := readAll(t, cl, s.ctx, &flight.Ticket{Ticket: badTicket})
			release(recs)
			return err
		}()},
	} {
		if st := status.Convert(row.err); st.Code() != codes.InvalidArgument || st.Message() == "" {
			t.Fatalf("%s: want INVALID_ARGUMENT with a message, got %v", row.name, row.err)
		}
	}

	// 2. the cluster's rows: a server over a cluster that does not answer
	//    is UNAVAILABLE with no detail, and once the breaker opens,
	//    UNAVAILABLE with a RetryInfo detail whose delay is the breaker's.
	//    Two failures open the breaker, so the first statement answers the
	//    unreachable cluster itself whether or not the read retry ran, and
	//    the open breaker shows within two more statements.
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1" // the connection is refused at once
	cfg.Pool.Breaker.Failures = 2
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Close(ctx)
	})
	down := newServerOn(t, c, qdb.User{}, tokensAt(t, time.Now()))
	dcl := down.dial(t)
	err = statementErr(t, dcl, down.ctx, "select 1")
	if code(err) != codes.Unavailable || retryInfo(err) != nil {
		t.Fatalf("unreachable: want UNAVAILABLE with no detail, got %v", err)
	}
	var ri *errdetails.RetryInfo
	for range 2 {
		err = statementErr(t, dcl, down.ctx, "select 1")
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

	// 3. the table listing takes the same road: GetTables on the down
	//    server answers UNAVAILABLE at DoGet, through statusOf
	info, err := dcl.GetTables(down.ctx, &flightsql.GetTablesOpts{})
	if err != nil {
		t.Fatalf("GetTables: %v", err)
	}
	_, recs, err := readAll(t, dcl, down.ctx, info.Endpoint[0].Ticket)
	release(recs)
	if code(err) != codes.Unavailable {
		t.Fatalf("GetTables unreachable: want UNAVAILABLE, got %v", err)
	}
}

// catalogRows answers a reader of catalog commands: given a GetFlightInfo
// call's two results, it presents the one ticket on DoGet and answers the
// stream concatenated into one batch the caller releases. It takes the
// call's results as its only parameters, so the call sits inside its
// parentheses.
func catalogRows(t *testing.T, cl *flightsql.Client, ctx context.Context) func(*flight.FlightInfo, error) arrow.RecordBatch {
	return func(info *flight.FlightInfo, err error) arrow.RecordBatch {
		t.Helper()
		if err != nil {
			t.Fatalf("GetFlightInfo: %v", err)
		}
		if len(info.Endpoint) != 1 {
			t.Fatalf("want one endpoint, got %d", len(info.Endpoint))
		}
		schema, recs, err := readAll(t, cl, ctx, info.Endpoint[0].Ticket)
		if err != nil {
			t.Fatalf("DoGet: %v", err)
		}
		defer release(recs)
		return concat(t, schema, recs)
	}
}

// column answers the string column named name of rec as a slice.
func column(t *testing.T, rec arrow.RecordBatch, name string) []string {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	if len(idx) != 1 {
		t.Fatalf("want one column %q in %v", name, rec.Schema())
	}
	col := rec.Column(idx[0]).(*array.String)
	out := make([]string, col.Len())
	for i := range out {
		out[i] = col.Value(i)
	}
	return out
}

// binaryColumn answers the binary column named name of rec as a slice,
// the sibling of column for the table_schema column.
func binaryColumn(t *testing.T, rec arrow.RecordBatch, name string) [][]byte {
	t.Helper()
	idx := rec.Schema().FieldIndices(name)
	if len(idx) != 1 {
		t.Fatalf("want one column %q in %v", name, rec.Schema())
	}
	col := rec.Column(idx[0]).(*array.Binary)
	out := make([][]byte, col.Len())
	for i := range out {
		out[i] = col.Value(i)
	}
	return out
}

// checkSqlInfo reads every registered SqlInfo id back through cl and
// asserts one row per value type: the server name, read-only, transaction
// NONE and the quote char. An id the server does not register is NOT_FOUND.
func checkSqlInfo(t *testing.T, cl *flightsql.Client, ctx context.Context) {
	t.Helper()
	// The batch is read whole and not through catalogRows, because Arrow
	// does not concatenate a dense union and the base server answers one
	// batch.
	info, err := cl.GetSqlInfo(ctx, nil)
	if err != nil {
		t.Fatalf("GetSqlInfo: %v", err)
	}
	_, recs, err := readAll(t, cl, ctx, info.Endpoint[0].Ticket)
	if err != nil || len(recs) != 1 {
		t.Fatalf("want one batch, got %d, %v", len(recs), err)
	}
	defer release(recs)
	rec := recs[0]

	// The value column is a dense union, so each row is read through its
	// type code and child. Rows are looked up by id and not by position,
	// because DoGetSqlInfo iterates a map when no id is requested.
	ids := rec.Column(0).(*array.Uint32)
	vals := rec.Column(1).(*array.DenseUnion)
	valueOf := func(id flightsql.SqlInfo) (arrow.Array, int) {
		for i := range ids.Len() {
			if ids.Value(i) == uint32(id) {
				return vals.Field(vals.ChildID(i)), int(vals.ValueOffset(i))
			}
		}
		t.Fatalf("id %d is not in %v", id, ids)
		return nil, 0
	}
	if a, i := valueOf(flightsql.SqlInfoFlightSqlServerName); a.(*array.String).Value(i) != "QuasarDB REST (native engine, DuckDB through #sql)" {
		t.Fatalf("server name: got %q", a.(*array.String).Value(i))
	}
	if a, i := valueOf(flightsql.SqlInfoFlightSqlServerReadOnly); !a.(*array.Boolean).Value(i) {
		t.Fatal("read-only: want true")
	}
	if a, i := valueOf(flightsql.SqlInfoFlightSqlServerTransaction); a.(*array.Int32).Value(i) != int32(flightsql.SqlTransactionNone) {
		t.Fatalf("transaction: got %d", a.(*array.Int32).Value(i))
	}
	if a, i := valueOf(flightsql.SqlInfoIdentifierQuoteChar); a.(*array.String).Value(i) != `"` {
		t.Fatalf("quote char: got %q", a.(*array.String).Value(i))
	}

	// An unregistered id: GetFlightInfo answers, because the base server
	// checks ids at DoGet, where the stream is NOT_FOUND.
	info, err = cl.GetSqlInfo(ctx, []flightsql.SqlInfo{flightsql.SqlInfoKeywords})
	if err != nil {
		t.Fatalf("GetSqlInfo keywords: %v", err)
	}
	_, missing, err := readAll(t, cl, ctx, info.Endpoint[0].Ticket)
	release(missing)
	if code(err) != codes.NotFound {
		t.Fatalf("unregistered id: want NOT_FOUND, got %v", err)
	}
}

// TestCatalog: on both clusters, GetSqlInfo answers the registered values,
// GetCatalogs, GetDbSchemas and GetTableTypes answer their one row,
// GetTables lists a created table under qdb, main and TABLE, the table
// name pattern narrows the list to it, an excluding catalog answers no
// row, and under include_schema a generated table's table_schema
// deserializes to the fixture's schema with its keys.
func TestCatalog(t *testing.T) {
	// One server and one table serve the assertions, in the order of a
	// driver's connect sequence:
	//
	//  1. GetSqlInfo, every registered id and one unregistered;
	//  2. one table created as the fixture's user;
	//  3. the three fixed commands, each one row;
	//  4. GetTables without a pattern contains the table's name;
	//  5. GetTables with the table's name as the pattern is that one row,
	//     and a % pattern over its qdbtest_ prefix contains it;
	//  6. GetTables with catalog "other" is no row;
	//  7. include_schema: a generated table's row carries five columns and
	//     its bytes deserialize to the fixture's schema, and a ticket built
	//     by hand from CommandGetTables with the flag answers the five
	//     columns too.
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		s := newServer(t, f)
		cl := s.dial(t)
		ctx, err := cl.Client.AuthenticateBasicToken(s.ctx, s.user.Username, s.user.SecretKey)
		if err != nil {
			t.Fatalf("handshake: %v", err)
		}

		// 1. the server describes itself
		checkSqlInfo(t, cl, ctx)

		// 2. one table with no rows, created through the fixture so it is
		//    removed on cleanup
		tbl := table.Table{Name: "qdbtest_catalog_" + f.Spec.Name, Columns: []table.Column{{Name: "v", Type: qdbapi.TsColumnDouble}}}
		table.CreateAs(t, f.Cluster, f.User, tbl)

		// 3. the fixed shape of one cluster
		rows := catalogRows(t, cl, ctx)
		rec := rows(cl.GetCatalogs(ctx))
		if got := column(t, rec, "catalog_name"); len(got) != 1 || got[0] != catalogName {
			t.Fatalf("catalogs: got %v", got)
		}
		rec.Release()
		rec = rows(cl.GetDBSchemas(ctx, &flightsql.GetDBSchemasOpts{}))
		if got := column(t, rec, "db_schema_name"); len(got) != 1 || got[0] != schemaName || column(t, rec, "catalog_name")[0] != catalogName {
			t.Fatalf("schemas: got %v", rec)
		}
		rec.Release()
		rec = rows(cl.GetTableTypes(ctx))
		if got := column(t, rec, "table_type"); len(got) != 1 || got[0] != tableType {
			t.Fatalf("table types: got %v", got)
		}
		rec.Release()

		// 4. the whole list holds the table under the fixed names
		rec = rows(cl.GetTables(ctx, &flightsql.GetTablesOpts{}))
		names := column(t, rec, "table_name")
		at := slices.Index(names, tbl.Name)
		if at < 0 {
			t.Fatalf("tables: want %s in %v", tbl.Name, names)
		}
		if c, sc, ty := column(t, rec, "catalog_name")[at], column(t, rec, "db_schema_name")[at], column(t, rec, "table_type")[at]; c != catalogName || sc != schemaName || ty != tableType {
			t.Fatalf("tables: row %d is %s.%s %s", at, c, sc, ty)
		}
		rec.Release()

		// 5. the exact name is one row, and the prefix pattern contains it
		rec = rows(cl.GetTables(ctx, &flightsql.GetTablesOpts{TableNameFilterPattern: &tbl.Name}))
		if got := column(t, rec, "table_name"); len(got) != 1 || got[0] != tbl.Name {
			t.Fatalf("tables by name: got %v", got)
		}
		rec.Release()
		prefix := "qdbtest_catalog_%"
		rec = rows(cl.GetTables(ctx, &flightsql.GetTablesOpts{TableNameFilterPattern: &prefix}))
		if got := column(t, rec, "table_name"); !slices.Contains(got, tbl.Name) {
			t.Fatalf("tables by prefix: want %s among %v", tbl.Name, got)
		}
		rec.Release()

		// 6. another catalog is no row and no error
		other := "other"
		rec = rows(cl.GetTables(ctx, &flightsql.GetTablesOpts{Catalog: &other}))
		if rec.NumRows() != 0 {
			t.Fatalf("tables of catalog other: got %v", rec)
		}
		rec.Release()

		// 7. include_schema. rapid draws the table, so symbol columns and
		//    their symtables are covered, and the fixture creates every table
		//    with a one-day shard and no TTL. Schema.Equal compares the
		//    fields with their metadata and ignores the schema's own, so the
		//    two schema keys are read by name.
		rapid.Check(t, func(rt *rapid.T) {
			gen := table.GenerateSchema(rt)
			table.CreateAs(rt, f.Cluster, f.User, gen)
			rec := rows(cl.GetTables(ctx, &flightsql.GetTablesOpts{TableNameFilterPattern: &gen.Name, IncludeSchema: true}))
			defer rec.Release()
			if rec.NumCols() != 5 || rec.NumRows() != 1 {
				rt.Fatalf("include_schema: want one row of five columns, got %v", rec)
			}
			got, err := flight.DeserializeSchema(binaryColumn(t, rec, "table_schema")[0], memory.DefaultAllocator)
			if err != nil {
				rt.Fatalf("table_schema: %v", err)
			}
			want := table.SchemaWithMeta(gen, 24*time.Hour, 0)
			if !got.Equal(want) {
				rt.Fatalf("table_schema:\n got %v\nwant %v", got, want)
			}
			for _, k := range []string{qdb.MetaShardSize, qdb.MetaTTL} {
				g, _ := got.Metadata().GetValue(k)
				if w, _ := want.Metadata().GetValue(k); g != w {
					rt.Fatalf("%s: got %q, want %q", k, g, w)
				}
			}
		})
		// A ticket minted by hand with the flag answers the five columns
		// too, because a ticket is bytes a client can mint without
		// GetFlightInfo.
		cmd, err := anypb.New(&pb.CommandGetTables{IncludeSchema: true})
		if err != nil {
			t.Fatal(err)
		}
		ticket, err := proto.Marshal(cmd)
		if err != nil {
			t.Fatal(err)
		}
		schema, recs, err := readAll(t, cl, ctx, &flight.Ticket{Ticket: ticket})
		release(recs)
		if err != nil || schema.NumFields() != 5 {
			t.Fatalf("DoGet include_schema: want five columns, got %v, %v", schema, err)
		}
	})
}
