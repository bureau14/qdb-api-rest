package flightsql

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/olap"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/query"
)

// userOf answers the call's user from the claims the auth edge placed.
// The anonymous user has zero claims. The HTTP handlers derive the user
// the same way.
func userOf(ctx context.Context) qdb.User {
	c := auth.ClaimsFrom(ctx)
	return qdb.User{Username: c.Username, SecretKey: c.SecretKey}
}

// describe answers a query's schema, and whether it has one. A DuckDB
// column type without a mapping answers no schema and no error. Every
// other failure goes through statusOf, with INVALID_ARGUMENT for what an
// engine answered.
func describe(ctx context.Context, q string) (*arrow.Schema, bool, error) {
	sc, err := query.Describe(ctx, userOf(ctx), q)
	switch {
	// No schema, so a plain statement over such a type runs as it would
	// without a describe, and only the calls whose answer is the schema
	// fail (schemaOf). Failing every call was rejected, because it stops
	// a query that runs over ADBC.
	case errors.Is(err, olap.ErrUndescribable):
		return nil, false, nil
	// A text that is not one statement and a query with parameters fall to
	// INVALID_ARGUMENT, because the request is at fault.
	case err != nil:
		return nil, false, statusOf(ctx, err, codes.InvalidArgument)
	}
	return sc, true, nil
}

// schemaOf answers a query's schema for the calls that answer nothing
// else: the prepare and both schema calls. A DuckDB column type without a
// mapping is INVALID_ARGUMENT naming the column and the type.
func schemaOf(ctx context.Context, q string) (*arrow.Schema, error) {
	sc, err := query.Describe(ctx, userOf(ctx), q)
	if err != nil {
		return nil, statusOf(ctx, err, codes.InvalidArgument)
	}
	return sc, nil
}

// GetFlightInfoStatement is the first half of a Flight SQL query. Flight
// fetches a result in two calls: the client sends its query text here and
// gets a FlightInfo back, which lists the endpoints holding the result and
// a ticket per endpoint, and then presents each ticket on DoGet, which
// streams that endpoint's batches. A server that partitions a result or
// runs the query ahead of time does its work here. This server has one
// endpoint, keeps nothing between calls and runs the query at DoGet
// (ADR-0018, Decision 9). The info is one endpoint whose ticket is the
// query text, the described schema when there is one, no location, which
// means this server, and unknown totals. A query that fails to describe
// fails here, before any ticket.
func (s *Server) GetFlightInfoStatement(ctx context.Context, q flightsql.StatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// The describe runs here and the query at DoGet, so the server stores
	// nothing between the calls.
	sc, ok, err := describe(ctx, q.GetQuery())
	if err != nil {
		return nil, err
	}
	// The text travels verbatim and DoGet dispatches it, so the #sql rule
	// has one home, in query.Run. A mint failure is INTERNAL.
	ticket, err := flightsql.CreateStatementQueryTicket([]byte(q.GetQuery()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	info := &flight.FlightInfo{
		// The descriptor is echoed, as arrow-go's examples do.
		FlightDescriptor: desc,
		Endpoint:         []*flight.FlightEndpoint{{Ticket: &flight.Ticket{Ticket: ticket}}},
		// -1 means unknown, because nothing has run yet.
		TotalRecords: -1,
		TotalBytes:   -1,
	}
	// The schema is serialized as the base server serializes a prepare's
	// dataset schema.
	if ok {
		info.Schema = flight.SerializeSchema(sc, memory.DefaultAllocator)
	}
	return info, nil
}

// GetSchemaStatement answers the described schema of the statement's
// query, without running it.
func (s *Server) GetSchemaStatement(ctx context.Context, q flightsql.StatementQuery, _ *flight.FlightDescriptor) (*flight.SchemaResult, error) {
	sc, err := schemaOf(ctx, q.GetQuery())
	if err != nil {
		return nil, err
	}
	return &flight.SchemaResult{Schema: flight.SerializeSchema(sc, memory.DefaultAllocator)}, nil
}

// service is the Flight RPC service the gRPC server registers: arrow-go's
// router from Flight RPCs to Flight SQL commands, with DoGet overridden
// for a statement ticket.
type service struct {
	flight.FlightServer
	s *Server
}

// DoGet streams the result of a statement ticket, and hands every other
// ticket to arrow-go's router.
func (v *service) DoGet(t *flight.Ticket, stream flight.FlightService_DoGetServer) error {
	// The query runs inside the sink, which writes each lent batch to the
	// stream as it comes, in one function and no goroutine, as the HTTP
	// handler does. arrow-go's DoGetStatement hook, which answers a
	// channel the router drains, was rejected: it needs a producer
	// goroutine, a schema handoff before the first chunk, and the
	// error-before-first-byte split carried across the channel.
	//
	//  1. decode the ticket;
	//  2. find the user;
	//  3. run the query, with the Flight writer inside the sink;
	//  4. on a failed stream, answer INTERNAL or the context's code;
	//  5. on a run that failed before the sink, answer statusOf's code.
	ctx := stream.Context()

	// 1. decode the ticket. A ticket that is not a statement goes to the
	//    router.
	ticket, err := flightsql.GetStatementQueryTicket(t)
	if err != nil {
		return v.FlightServer.DoGet(t, stream)
	}
	// 2. find the user
	u := userOf(ctx)
	// 3. run the query. query.Run already retries the native engine once
	//    before the sink, so this method retries nothing. The sink opens
	//    the Flight writer on the first batch's schema and writes each
	//    batch as one message. It checks the context per batch, so a
	//    client that left is noticed at the next batch. Close runs after
	//    the last batch, because it writes the schema when no batch did, so
	//    an empty result reaches the client too. A batch of no rows is
	//    skipped, as the Arrow encoder skips it, so a statement without a
	//    result set streams a schema with no fields and no batch.
	var streamErr error
	err = query.Run(ctx, u, string(ticket.GetStatementHandle()), func(batches qdb.Batches) error {
		var w *flight.Writer
		for rec, err := range batches {
			if err != nil {
				streamErr = err
				return err
			}
			if w == nil {
				w = flight.NewRecordWriter(stream, ipc.WithSchema(rec.Schema()))
			}
			if err := ctx.Err(); err != nil {
				streamErr = err
				return err
			}
			if rec.NumRows() == 0 {
				continue
			}
			if err := w.Write(rec); err != nil {
				streamErr = err
				return err
			}
		}
		// The sequence is never empty (the Batches contract), so the writer
		// is open. If it were not, Close on a nil writer would panic, so an
		// empty schema is streamed and the client still reads a stream.
		if w == nil {
			w = flight.NewRecordWriter(stream, ipc.WithSchema(arrow.NewSchema(nil, nil)))
		}
		if err := w.Close(); err != nil {
			streamErr = err
			return err
		}
		return nil
	})
	switch {
	// 4. the client sees only the closing status, so the error goes to the
	//    log in one warning line, and the status says the server failed:
	//    INTERNAL, or the ctx's own code when the caller left. INVALID_
	//    ARGUMENT for a DuckDB reader error mid-stream was rejected: the
	//    engine spoke, but the client cannot act on it after data arrived,
	//    so its message reaches the client as INTERNAL's detail
	case streamErr != nil:
		if ctx.Err() != nil {
			observe.Logger(ctx).DebugContext(ctx, "stream abandoned by the caller", observe.Err(streamErr))
			return status.FromContextError(ctx.Err()).Err()
		}
		observe.Logger(ctx).WarnContext(ctx, "stream cut", observe.Err(streamErr))
		return status.Error(codes.Internal, streamErr.Error())
	// 5. INVALID_ARGUMENT for what an engine answered, and statusOf's rows
	//    for the rest (ADR-0018, Decision 13)
	case err != nil:
		return statusOf(ctx, err, codes.InvalidArgument)
	}
	return nil
}
