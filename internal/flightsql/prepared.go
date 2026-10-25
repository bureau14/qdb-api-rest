package flightsql

import (
	"context"

	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// CreatePreparedStatement answers the query text as the handle and the
// query's described schema as the dataset schema, with no parameter
// schema. The server keeps nothing, so the handle is the whole
// statement.
func (s *Server) CreatePreparedStatement(ctx context.Context, req flightsql.ActionCreatePreparedStatementRequest) (flightsql.ActionCreatePreparedStatementResult, error) {
	// The dataset schema is required, because Arrow's JDBC driver classes a
	// statement as an update unless the prepare answers a schema with
	// fields. A transaction id is ignored, because GetSqlInfo announces no
	// transactions. IsUpdate stays nil, because the server is read-only and
	// the driver reads the dataset schema.
	sc, err := schemaOf(ctx, req.GetQuery())
	if err != nil {
		return flightsql.ActionCreatePreparedStatementResult{}, err
	}
	// The handle is the text verbatim, so any instance behind a load
	// balancer serves the later calls (ADR-0018, Decision 9).
	return flightsql.ActionCreatePreparedStatementResult{
		Handle:        []byte(req.GetQuery()),
		DatasetSchema: sc,
	}, nil
}

// ClosePreparedStatement answers nothing, because the server stores
// nothing for a handle.
func (s *Server) ClosePreparedStatement(context.Context, flightsql.ActionClosePreparedStatementRequest) error {
	return nil
}

// textQuery is a handle's text as a statement query, outside any
// transaction.
type textQuery string

func (q textQuery) GetQuery() string         { return string(q) }
func (q textQuery) GetTransactionId() []byte { return nil }

// GetFlightInfoPreparedStatement answers what GetFlightInfoStatement
// answers for the handle's text: one endpoint whose ticket is a
// statement ticket of that text, and its described schema.
func (s *Server) GetFlightInfoPreparedStatement(ctx context.Context, q flightsql.PreparedStatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// The ticket is a statement ticket and not a prepared one, so the
	// existing DoGet streams it and arrow-go's channel hook stays unused. A
	// DoGet branch for prepared tickets was rejected, because it repeats
	// the statement branch for the same text.
	return s.GetFlightInfoStatement(ctx, textQuery(q.GetPreparedStatementHandle()), desc)
}

// GetSchemaPreparedStatement answers the described schema of the
// handle's text, without running it.
func (s *Server) GetSchemaPreparedStatement(ctx context.Context, q flightsql.PreparedStatementQuery, _ *flight.FlightDescriptor) (*flight.SchemaResult, error) {
	sc, err := schemaOf(ctx, string(q.GetPreparedStatementHandle()))
	if err != nil {
		return nil, err
	}
	return &flight.SchemaResult{Schema: flight.SerializeSchema(sc, memory.DefaultAllocator)}, nil
}
