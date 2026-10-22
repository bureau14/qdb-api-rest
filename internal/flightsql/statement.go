package flightsql

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		// nothing and runs nothing here.
		TotalRecords: -1,
		TotalBytes:   -1,
	}, nil
}
