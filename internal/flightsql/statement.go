package flightsql

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/query"
)

// GetFlightInfoStatement answers one endpoint whose ticket is the query
// text, unchanged. The info carries no schema, because the native engine
// does not know a result's columns before running the query, and no
// location, which means this server (ADR-0018, Decision 9).
func (s *Server) GetFlightInfoStatement(_ context.Context, q flightsql.StatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// The text travels verbatim and DoGet dispatches it, so the #sql rule
	// has one home, in query.Run. Splitting the prefix here and encoding
	// engine plus text was rejected: a ticket format of this server's own,
	// and the split exported. A mint failure is INTERNAL.
	ticket, err := flightsql.CreateStatementQueryTicket([]byte(q.GetQuery()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &flight.FlightInfo{
		// The descriptor is echoed, as arrow-go's examples do.
		FlightDescriptor: desc,
		Endpoint:         []*flight.FlightEndpoint{{Ticket: &flight.Ticket{Ticket: ticket}}},
		// -1 for records and bytes means unknown, because the server stores
		// nothing and runs nothing here. Running the query for the totals
		// was rejected: the query twice.
		TotalRecords: -1,
		TotalBytes:   -1,
	}, nil
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
	// One decode, one run inside a sink, and two kinds of failure that
	// answer differently, the HTTP handler's shape. The query runs inside
	// the sink and writes to the stream while each batch is lent, so no
	// batch is copied, retained or crossed to another goroutine. arrow-go's
	// DoGetStatement hook, which answers a schema and a channel the router
	// drains, was rejected: correct without a copy, because a Retain keeps
	// a batch alive, but it needs a producer goroutine, a schema handoff
	// before the first chunk, ctx-aware sends, and the error-before-first-
	// byte split carried across the channel. Given up with it: a depth-one
	// overlap between DuckDB's chunk conversion and gRPC serialization, and
	// the ticket decode runs here as well as in the router.
	//
	//  1. decode the ticket;
	//  2. the user from the claims;
	//  3. run through query.Run, the Flight writer inside the sink;
	//  4. the stream failed: INTERNAL, or the ctx's code;
	//  5. the run failed before the sink: statusOf.
	ctx := stream.Context()

	// 1. GetStatementQueryTicket answers an error for any ticket that is
	//    not a statement, and that ticket is the router's
	ticket, err := flightsql.GetStatementQueryTicket(t)
	if err != nil {
		return v.FlightServer.DoGet(t, stream)
	}
	// 2. the auth edge placed the claims, zero claims for the anonymous
	//    user, as the HTTP handlers derive it
	c := auth.ClaimsFrom(ctx)
	u := qdb.User{Username: c.Username, SecretKey: c.SecretKey}
	// 3. query.Run retries the native engine once before the sink and
	//    DuckDB never, so this method retries nothing. The sink opens the
	//    Flight writer on the first batch's schema and writes each batch as
	//    one message, with a ctx check per batch, because a client that
	//    left is noticed at the next batch. Close runs after the last batch,
	//    because Close writes the schema when no batch did, so an empty
	//    result reaches the client too. A batch of no rows is not written,
	//    as the Arrow encoder skips it, so a statement without a result set
	//    streams a schema with no fields and no batch.
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
		// The sequence is never empty (the Batches contract), so w is open;
		// if it were not, Close on a nil writer would panic, and an empty
		// schema is streamed instead so the client still reads a stream.
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
