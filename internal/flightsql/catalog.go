package flightsql

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql/schema_ref"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"google.golang.org/grpc/codes"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// The cluster's place in Flight SQL's catalog tree. A driver shows a
// table under catalog qdb and schema main, the names a #sql query takes,
// because qdb-duck attaches the cluster as catalog qdb with schema main.
// A null catalog and schema were rejected, because the tree view would
// show an unnamed node and the names would differ from what #sql needs.
// The table type is TABLE, because the native engine has no views and the
// native listing does not see DuckDB's.
const (
	catalogName = "qdb"
	schemaName  = "main"
	tableType   = "TABLE"
)

// infoFor answers the FlightInfo of a catalog command: one endpoint whose
// ticket is the command itself, the command's reference schema and
// unknown totals. DoGet decodes the ticket, so the server keeps nothing
// between the calls (ADR-0018, Decision 9).
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

// oneChunk answers a batch as the schema and the channel a DoGet method
// returns. The channel closes after its one chunk, as in the base
// server's DoGetSqlInfo.
func oneChunk(ctx context.Context, rec arrow.RecordBatch) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// NewRecordReader retains the batch, so the caller's release after this
	// call leaves the reader's reference, which StreamChunksFromReader
	// releases once drained.
	rdr, err := array.NewRecordReader(rec.Schema(), []arrow.RecordBatch{rec})
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan flight.StreamChunk)
	go flight.StreamChunksFromReader(ctx, rdr, ch)
	return rec.Schema(), ch, nil
}

// likePattern answers whether a name matches the SQL LIKE pattern of a
// Flight SQL filter. % matches any run, _ matches one character, and
// every other byte matches itself. A nil pattern matches everything.
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

// holdsCluster answers whether a request's catalog and schema pattern
// admit the cluster: a nil catalog or qdb, and a schema pattern that main
// matches.
// A catalog other than qdb, or a pattern main does not match, is an empty
// result and not an error, because a driver that walks several catalogs
// asks for each and expects the empty ones to answer empty.
func holdsCluster(catalog, schemaPattern *string) bool {
	return (catalog == nil || *catalog == catalogName) && likePattern(schemaPattern, schemaName)
}

// GetFlightInfoCatalogs answers the info of the catalog listing.
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
// when the request's catalog or schema pattern excludes it.
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

// GetFlightInfoTables answers the info of the table listing: four
// columns, or five with each table's schema when the request asks for it.
func (s *Server) GetFlightInfoTables(_ context.Context, cmd flightsql.GetTables, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// The schema is the one the router's GetSchema promises for the flag,
	// so the info and the schema call agree.
	schema := schema_ref.Tables
	if cmd.GetIncludeSchema() {
		schema = schema_ref.TablesWithIncludedSchema
	}
	return infoFor(desc, schema), nil
}

// DoGetTables answers one row per table the cluster lists for the caller,
// under catalog qdb, schema main and type TABLE, filtered by the
// request's table name pattern. The result is empty when the request's
// catalog, schema pattern or table types exclude the cluster. With
// include_schema each row carries the table's schema as IPC bytes in
// table_schema.
func (s *Server) DoGetTables(ctx context.Context, cmd flightsql.GetTables) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// The listing is the cluster's, filtered here, because `show tables`
	// has no filtered form, so the whole list crosses the wire per call and
	// the pattern is applied in Go:
	//
	//  1. answer the empty result when the request excludes the cluster;
	//  2. the user from the claims;
	//  3. the names, or the names with their schemas, mapped through
	//     statusOf;
	//  4. one row per name the pattern admits, the schema serialized when
	//     asked.

	// 1. a catalog other than qdb, a schema pattern main does not match, or
	//    table types without TABLE answer the empty result before the
	//    cluster is called, because the answer needs no listing. The
	//    result's shape is the one GetFlightInfoTables promised for the
	//    flag.
	withSchema := cmd.GetIncludeSchema()
	ref := schema_ref.Tables
	if withSchema {
		ref = schema_ref.TablesWithIncludedSchema
	}
	b := array.NewRecordBuilder(memory.DefaultAllocator, ref)
	defer b.Release()
	types := cmd.GetTableTypes()
	if !holdsCluster(cmd.GetCatalog(), cmd.GetDBSchemaFilterPattern()) || (len(types) > 0 && !slices.Contains(types, tableType)) {
		rec := b.NewRecordBatch()
		defer rec.Release()
		return oneChunk(ctx, rec)
	}
	// 2. the auth edge placed the claims, zero claims for the anonymous
	//    user, as DoGet derives it
	c := auth.ClaimsFrom(ctx)
	u := qdb.User{Username: c.Username, SecretKey: c.SecretKey}
	// 3. without the flag Cluster.Tables, with it Cluster.TableSchemas,
	//    because the schemas cost one metadata call per table and a plain
	//    listing should not pay it. INTERNAL is the answered code, because
	//    nothing in the request reaches the engine as text, so a message
	//    from the engine is the server's failure and not the caller's.
	//    INVALID_ARGUMENT, as the statement uses, was rejected: it blames
	//    the caller for a request that has no text.
	var tables []qdb.TableSchema
	if withSchema {
		var err error
		if tables, err = qdb.ClusterFrom(ctx).TableSchemas(ctx, u); err != nil {
			return nil, nil, statusOf(ctx, err, codes.Internal)
		}
	} else {
		names, err := qdb.ClusterFrom(ctx).Tables(ctx, u)
		if err != nil {
			return nil, nil, statusOf(ctx, err, codes.Internal)
		}
		for _, name := range names {
			tables = append(tables, qdb.TableSchema{Name: name})
		}
	}
	// 4. in the cluster's order, under the fixed catalog, schema and type.
	//    The schema goes out as flight.SerializeSchema writes it, a
	//    schema-only IPC stream, because that is the form every Flight
	//    implementation reads back. Ignoring the flag and answering four
	//    columns was rejected: the driver would read a result that differs
	//    from the schema GetSchema promised.
	for _, t := range tables {
		if !likePattern(cmd.GetTableNameFilterPattern(), t.Name) {
			continue
		}
		b.Field(0).(*array.StringBuilder).Append(catalogName)
		b.Field(1).(*array.StringBuilder).Append(schemaName)
		b.Field(2).(*array.StringBuilder).Append(t.Name)
		b.Field(3).(*array.StringBuilder).Append(tableType)
		if withSchema {
			b.Field(4).(*array.BinaryBuilder).Append(flight.SerializeSchema(t.Schema, memory.DefaultAllocator))
		}
	}
	rec := b.NewRecordBatch()
	defer rec.Release()
	return oneChunk(ctx, rec)
}

// GetFlightInfoTableTypes answers the info of the table type listing.
func (s *Server) GetFlightInfoTableTypes(_ context.Context, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	return infoFor(desc, schema_ref.TableTypes), nil
}

// DoGetTableTypes answers one row, TABLE, the type the constants above
// explain.
func (s *Server) DoGetTableTypes(ctx context.Context) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema_ref.TableTypes)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(tableType)
	rec := b.NewRecordBatch()
	defer rec.Release()
	return oneChunk(ctx, rec)
}
