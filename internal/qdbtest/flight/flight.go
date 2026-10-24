// Package flight is the Flight client fixture: a client on a server of
// the tests with verification off, a statement read into one batch, an
// Arrow IPC body read the same way, and batch equality. It is a
// subpackage of qdbtest for the reason table and cluster are, and it
// holds no server start, because a fixture that imported
// internal/flightsql would be a test import cycle for that package's own
// tests. The rules are in internal/AGENTS.md, Tests.
package flight

import (
	"context"
	"crypto/tls"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// Dial opens arrow-go's Flight SQL client on addr with certificate
// verification off, because every test server runs on the ephemeral
// certificate, as the driver smoke steps connect. The client closes on
// t's cleanup.
func Dial(t table.T, addr string) *flightsql.Client {
	t.Helper()
	creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // the test certificate is ephemeral
	cl, err := flightsql.NewClient(addr, nil, nil, grpc.WithTransportCredentials(creds))
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = cl.Close() })
	return cl
}

// Bearer is ctx with one authorization header carrying token, the form
// every RPC presents.
func Bearer(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// TicketOf runs Execute for q and answers the one endpoint's ticket.
// GetFlightInfo runs nothing, so a statement's failure surfaces on DoGet.
func TicketOf(t table.T, cl *flightsql.Client, ctx context.Context, q string) *flight.Ticket {
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

// ReadAll presents ticket on DoGet and reads the stream to its end: its
// schema, its batches retained for the caller to release, and the error
// that ended it.
func ReadAll(t table.T, cl *flightsql.Client, ctx context.Context, ticket *flight.Ticket) (*arrow.Schema, []arrow.RecordBatch, error) {
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

// Release releases every batch of recs.
func Release(recs []arrow.RecordBatch) {
	for _, r := range recs {
		r.Release()
	}
}

// Concat joins recs, which share schema, into one batch the caller
// releases. No batch is an empty batch of schema.
func Concat(t table.T, schema *arrow.Schema, recs []arrow.RecordBatch) arrow.RecordBatch {
	t.Helper()
	cols := make([]arrow.Array, schema.NumFields())
	rows := int64(0)
	for i := range cols {
		parts := make([]arrow.Array, len(recs))
		for n, r := range recs {
			parts[n] = r.Column(i)
		}
		// An empty array of the field's type, because Concatenate needs at
		// least one input.
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

// Execute runs q over Flight as a whole, Execute then DoGet, and answers
// the result joined into one batch the caller releases, so a comparison
// does not depend on how the server cut the stream: the native engine
// sends model.BatchRows rows per message and DuckDB one vector.
func Execute(t table.T, cl *flightsql.Client, ctx context.Context, q string) arrow.RecordBatch {
	t.Helper()
	schema, recs, err := ReadAll(t, cl, ctx, TicketOf(t, cl, ctx, q))
	if err != nil {
		t.Fatalf("DoGet %s: %v", q, err)
	}
	defer Release(recs)
	return Concat(t, schema, recs)
}

// FromIPC reads an Arrow IPC stream into one batch the caller releases,
// the shape Execute answers, so a Flight result and an HTTP Arrow body
// compare through Same.
func FromIPC(t table.T, r io.Reader) arrow.RecordBatch {
	t.Helper()
	rdr, err := ipc.NewReader(r)
	if err != nil {
		t.Fatalf("arrow stream: %v", err)
	}
	defer rdr.Release()
	var recs []arrow.RecordBatch
	for rdr.Next() {
		rec := rdr.RecordBatch()
		rec.Retain()
		recs = append(recs, rec)
	}
	if rdr.Err() != nil {
		t.Fatalf("arrow stream: %v", rdr.Err())
	}
	defer Release(recs)
	return Concat(t, rdr.Schema(), recs)
}

// Same reports whether got and want carry equal schemas and equal
// columns by array equality, which reads no byte under a null slot,
// because DuckDB leaves those bytes undefined.
func Same(got, want arrow.RecordBatch) bool {
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
