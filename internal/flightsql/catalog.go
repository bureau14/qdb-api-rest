package flightsql

import (
	"context"
	"regexp"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql/schema_ref"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// The one cluster's place in Flight SQL's catalog tree: a driver shows a
// table under catalog qdb and schema main, the names a #sql query takes
// (qdb-duck attaches the cluster as catalog qdb, and its one schema is
// main), so the tree view and the query agree. A null catalog and schema
// were rejected, because the tree view then shows an unnamed node and the
// names differ from what #sql needs; the cluster URI as the catalog was
// rejected, because no query accepts it. The one table type is TABLE,
// because the native engine has no views and qdb-duck's views live in
// DuckDB, which the native listing does not see.
const (
	catalogName = "qdb"
	schemaName  = "main"
	tableType   = "TABLE"
)

// infoFor answers the FlightInfo of a catalog command: one endpoint whose
// ticket is the command itself, so DoGet decodes it and the server keeps
// nothing between the calls (ADR-0018, Decision 9), with schema as the
// command's reference schema and unknown totals.
func infoFor(desc *flight.FlightDescriptor, schema *arrow.Schema) *flight.FlightInfo {
	return &flight.FlightInfo{
		FlightDescriptor: desc,
		Endpoint:         []*flight.FlightEndpoint{{Ticket: &flight.Ticket{Ticket: desc.Cmd}}},
		// The router's GetFlightInfo sets no schema for a catalog command, so
		// each handler serializes its own, as the base server does for
		// GetSqlInfo.
		Schema:       flight.SerializeSchema(schema, memory.DefaultAllocator),
		TotalRecords: -1,
		TotalBytes:   -1,
	}
}

// oneChunk answers rec as the schema and channel a DoGet method returns,
// the channel closed after the one chunk, as the base server's DoGetSqlInfo
// answers its batch.
func oneChunk(ctx context.Context, rec arrow.RecordBatch) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// NewRecordReader retains rec, so the caller's release after this call
	// leaves the reader's reference, which StreamChunksFromReader releases
	// once drained.
	rdr, err := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan flight.StreamChunk)
	go flight.StreamChunksFromReader(ctx, rdr, ch)
	return rec.Schema(), ch, nil
}

// likePattern answers whether s matches the SQL LIKE pattern p of a Flight
// SQL filter, where % matches any run and _ one character and every other
// byte matches itself. A nil p matches everything.
func likePattern(p *string, s string) bool {
	if p == nil {
		return true
	}
	// The two wildcards become their regexp twins and everything else is
	// quoted, so a dot or a bracket in a table name is literal. The regexp
	// is compiled per call, because a catalog call is rare and a cache is
	// state (internal/AGENTS.md, Code).
	var re strings.Builder
	re.WriteString("(?s)^")
	for _, r := range *p {
		switch r {
		case '%':
			re.WriteString(".*")
		case '_':
			re.WriteString(".")
		default:
			re.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	re.WriteString("$")
	return regexp.MustCompile(re.String()).MatchString(s)
}

// holdsCluster answers whether a request's catalog and schema pattern admit
// the one cluster: a nil catalog or qdb, and a schema pattern main matches.
// A catalog other than qdb, or a pattern main does not match, is an empty
// result and not an error, because a driver that walks several catalogs
// asks for each and expects the empty ones to answer empty.
func holdsCluster(catalog, schemaPattern *string) bool {
	return (catalog == nil || *catalog == catalogName) && likePattern(schemaPattern, schemaName)
}

// GetFlightInfoCatalogs answers the info of the one catalog.
func (s *Server) GetFlightInfoCatalogs(_ context.Context, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	return infoFor(desc, schema_ref.Catalogs), nil
}

// DoGetCatalogs answers one row, the catalog qdb.
func (s *Server) DoGetCatalogs(ctx context.Context) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema_ref.Catalogs)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(catalogName)
	rec := b.NewRecordBatch()
	defer rec.Release()
	return oneChunk(ctx, rec)
}

// GetFlightInfoSchemas answers the info of the schema listing.
func (s *Server) GetFlightInfoSchemas(_ context.Context, _ flightsql.GetDBSchemas, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	return infoFor(desc, schema_ref.DBSchemas), nil
}

// DoGetDBSchemas answers one row, catalog qdb and schema main, or no row
// when cmd's catalog or schema pattern excludes it.
func (s *Server) DoGetDBSchemas(ctx context.Context, cmd flightsql.GetDBSchemas) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema_ref.DBSchemas)
	defer b.Release()
	if holdsCluster(cmd.GetCatalog(), cmd.GetDBSchemaFilterPattern()) {
		b.Field(0).(*array.StringBuilder).Append(catalogName)
		b.Field(1).(*array.StringBuilder).Append(schemaName)
	}
	rec := b.NewRecordBatch()
	defer rec.Release()
	return oneChunk(ctx, rec)
}

// GetFlightInfoTableTypes answers the info of the table type listing.
func (s *Server) GetFlightInfoTableTypes(_ context.Context, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	return infoFor(desc, schema_ref.TableTypes), nil
}

// DoGetTableTypes answers one row, TABLE, the one type the constants above
// explain.
func (s *Server) DoGetTableTypes(ctx context.Context) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema_ref.TableTypes)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(tableType)
	rec := b.NewRecordBatch()
	defer rec.Release()
	return oneChunk(ctx, rec)
}
