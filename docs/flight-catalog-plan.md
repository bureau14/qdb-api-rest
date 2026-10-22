# Plan: flight catalog -- GetSqlInfo and the catalog commands, names only

Status: approved

This plan answers the first Next item of `docs/log.md`, Current state,
cut to its medium scope by the owner on 2026-10-22: `GetSqlInfo` on
arrow-go's registered map, and `GetCatalogs`, `GetDbSchemas`,
`GetTableTypes` and `GetTables` over a table list core in
`internal/qdb`, with `GetTables` answering names only. It is written
for an executor who has none of the seed conversation. Everything the
build needs is here.

## 1. Outcome

When the unit lands:

- A Flight SQL client that asks `GetSqlInfo` reads the server's name,
  version and Arrow version, that it is read-only, speaks SQL and no
  Substrait, supports no transactions and no cancel, and DuckDB's
  identifier rules: identifiers are case-insensitive, quoted or not,
  and the quote character is the double quote. A request for an id
  the server does not register is `NOT_FOUND`, which is arrow-go's
  answer and stays.
- `GetCatalogs` answers one row, `qdb`. `GetDbSchemas` answers one
  row, catalog `qdb` and schema `main`, or no row when the request's
  catalog or schema pattern excludes it. `GetTableTypes` answers one
  row, `TABLE`.
- `GetTables` answers one row per table name the cluster reports, in
  the cluster's order, with catalog `qdb`, schema `main` and type
  `TABLE`, filtered by the request's table name pattern (SQL `LIKE`,
  `%` and `_`), and empty when the request's catalog, schema pattern
  or table types exclude the one cluster. A request with
  `include_schema` set is `UNIMPLEMENTED`.
- `Cluster.Tables` in `internal/qdb` lists the cluster's table names
  as the caller's user, through the native `show tables` query under
  the read retry, qdbd's own `$qdb.` tables included.
- A failed listing answers through `statusOf`: `UNAUTHENTICATED` for
  a refused credential, `UNAVAILABLE` for an open breaker or an
  unreachable cluster, the caller's code when the caller left, and
  `INTERNAL` for anything else, because nothing in a `GetTables`
  request reaches the engine as text.
- Each catalog `GetFlightInfo` answers one endpoint whose ticket is
  the command itself and whose schema is the command's reference
  schema; it runs nothing, so the server keeps storing nothing between
  calls.
- The tests read every command back through arrow-go's client on
  both clusters, with a generated table found in `GetTables`.

Left for later units, each named:

- `GetTables` with `include_schema`: the fifth column holding each
  table's reader schema as IPC bytes, through one leased session
  (`docs/log.md`, Next, after this unit);
- the format-equivalence property extended with a Flight SQL client,
  the dispatch property and the ADBC and JDBC smoke tests
  (`docs/e2e.md`, The Flight SQL smoke tests);
- `GET /api/v2/tables` over `Cluster.Tables`, in M7 (`docs/log.md`,
  Handoff).

## 2. Verified facts

1. Neither the C headers nor the binding enumerates tables. The
   headers under `qdb/include/qdb/` declare `qdb_prefix_get`
   [`qdb/include/qdb/prefix.h:70`], `qdb_query_find`
   [`qdb/include/qdb/query.h:69`] and `qdb_ts_list_columns`
   [`qdb/include/qdb/ts.h:918`], and no table listing. The binding
   exposes `PrefixGet`, `GetTagged` and `Find`, which answer aliases of
   every entry type and need a `GetMetadata` per alias to keep the
   timeseries ones
   [`vendor/github.com/bureau14/qdb-api-go/v3/handle.go:974,932,1133`,
   `vendor/github.com/bureau14/qdb-api-go/v3/entry.go:133-140`].
2. qdbd answers the native query `show tables` with one string column
   named `$table`, one row per table, `$qdb.firehose` among the user
   tables, through `qdb_query_arrow`, the path `Session.fetch` uses.
   `show tables like '...'`, `show tables '...'` and
   `show tables where ...` are refused as invalid queries
   [qdbsh against the insecure test cluster, qdbd 3.15.0.dev0, session
   of 2026-10-22]. `show table <name>` answers the columns with shard
   size and symtable, which the follow-up slice may use [same session].
3. `Cluster.Query` fetches the result on a leased session, retried once
   under `WithReadRetry`, returns the session, stands in an empty batch
   for a statement without a result set, hands the sink slices of
   `model.BatchRows` rows that share the batch's buffers, and releases
   the batch after the sink returns, so a value that outlives the sink
   is copied [`internal/qdb/cluster.go:412-456`, `internal/AGENTS.md`,
   Code, the query bullet].
4. `Cluster.CreateTable` and `Cluster.RemoveTable` run as the caller
   and are in package `qdb`, so a test in that package can create a
   table without the table fixture, which `package qdb` cannot import
   [`internal/qdb/table.go:71-84`, `internal/AGENTS.md`, Tests].
   `eachCluster` runs a test on both clusters as subtests named by the
   cluster, and `userOf` is the user a test acts as
   [`internal/qdb/cluster_test.go:32-44`].
5. arrow-go's `BaseServer` carries `RegisterSqlInfo`, which takes
   `string`, `bool`, `int64`, `int32`, `[]string` or
   `map[int32][]int32` and refuses any other type, on a pointer
   receiver with no lock [`vendor/github.com/apache/arrow-go/v18/arrow/flight/flightsql/server.go:302-313`].
   `GetFlightInfoSqlInfo` answers `NOT_FOUND` when nothing is
   registered, else one endpoint whose ticket is the descriptor's
   command bytes and whose schema is `schema_ref.SqlInfo`
   [same file, 368-383]. `DoGetSqlInfo` answers the requested ids, or
   every registered id in map order when none is requested, as one
   batch on a channel; an unknown id is `NOT_FOUND` [same file,
   387-436]. Its `debug.Assert` compiles only under the `assert` build
   tag, which `.envrc` does not set
   [`vendor/github.com/apache/arrow-go/v18/arrow/internal/debug/assert_off.go`,
   `.envrc:73`].
6. The base server's catalog methods answer `UNIMPLEMENTED`:
   `GetFlightInfoCatalogs`, `DoGetCatalogs`, `GetFlightInfoSchemas`,
   `DoGetDBSchemas`, `GetFlightInfoTables`, `DoGetTables`,
   `GetFlightInfoTableTypes`, `DoGetTableTypes`
   [`vendor/.../flightsql/server.go:348-352,440-460`]. The router
   dispatches `GetFlightInfo` by command type to them, with the
   descriptor; its `GetSchema` answers the reference schema itself,
   `TablesWithIncludedSchema` when `include_schema` is set; its `DoGet`
   calls the `DoGet*` method with the stream's context, writes the
   answered schema through `flight.NewRecordWriter`, writes each chunk
   of the channel and releases it, and returns a chunk's `Err` as the
   stream's status [same file, 771-781, 840-863, 895-945].
7. The reference schemas: `Catalogs` is `catalog_name` string;
   `DBSchemas` is `catalog_name` nullable string and `db_schema_name`
   string; `Tables` is `catalog_name` and `db_schema_name` nullable
   strings, `table_name` and `table_type` strings;
   `TablesWithIncludedSchema` adds `table_schema` binary; `TableTypes`
   is `table_type` string
   [`vendor/.../flightsql/schema_ref/reference_schemas.go:24-45`].
8. A `GetTables` request carries `GetCatalog() *string`,
   `GetDBSchemaFilterPattern() *string`,
   `GetTableNameFilterPattern() *string`, `GetTableTypes() []string`
   and `GetIncludeSchema() bool`; a `GetDBSchemas` request carries the
   first two [`vendor/.../flightsql/server.go:240-260`]. The Flight SQL
   protocol defines the patterns as SQL `LIKE` patterns, `%` any
   sequence and `_` one character
   [`vendor/github.com/apache/arrow-go/v18/arrow/flight/gen/flight/FlightSql.pb.go`,
   the comments on `CommandGetTables`].
9. A `StreamChunk` carries `Data`, `Desc`, `AppMetadata` and `Err`;
   `StreamChunksFromReader` drains a record reader onto a channel,
   closes the channel and releases the reader
   [`vendor/github.com/apache/arrow-go/v18/arrow/flight/record_batch_reader.go:193-240`].
10. The SqlInfo ids and their Go value types: server name, version and
    Arrow version are strings; read-only, SQL, Substrait and cancel are
    bools; transaction is an `int32` of `SqlSupportedTransaction`,
    whose `NONE` is 0; identifier case and quoted identifier case are
    `int32` of `SqlSupportedCaseSensitivity`, whose
    `CASE_INSENSITIVE` is 1; the identifier quote char is a string;
    `SqlInfoTransactionsSupported` is a bool; `SqlInfoDDLCatalog`,
    `SqlInfoDDLSchema` and `SqlInfoDDLTable` are bools
    [`vendor/.../flightsql/types.go:159-261,615`,
    `vendor/.../gen/flight/FlightSql.pb.go:816,870`]. arrow-go's
    version is `arrow.PkgVersion`, `18.8.0`
    [`vendor/github.com/apache/arrow-go/v18/arrow/doc.go:37`].
11. The binary's version is `build.Version`, set by the linker and
    `dev` otherwise [`internal/build/build.go:13`].
12. qdb-duck attaches the cluster as the DuckDB catalog `qdb`, so a
    `#sql` query addresses a table as `qdb.<name>`
    [`internal/olap/instance.go:150`, `docs/brief.md`, Embedded
    DuckDB]. The schema name inside that catalog is not in the tree;
    `main` is DuckDB's default schema name [DuckDB documentation,
    Schemas; unverified against qdb-duck, see section 8].
13. `New` builds the gRPC server by hand, registers arrow-go's router
    over `*Server` with this package's `service` in front of it, and
    `service.DoGet` hands every ticket that is not a statement to the
    router [`internal/flightsql/server.go:40-70`,
    `internal/flightsql/statement.go:50-77`]. The auth edge places the
    claims on the ctx, zero claims for the anonymous user, and a
    handler builds `qdb.User` from them
    [`internal/flightsql/auth.go:98-101`,
    `internal/flightsql/statement.go:80-81`]. `statusOf` maps a failed
    cluster call to a status with the caller's `answered` code as its
    default [`internal/flightsql/status.go:33-79`].
14. The package's tests start the server on a port of its own, dial
    arrow-go's client with verification off, run on both clusters
    through `cluster.Each`, and read a ticket with `readAll`; an
    unreachable cluster is `qdb://127.0.0.1:1` with a breaker of one
    failure [`internal/flightsql/flightsql_test.go:79-118,156-171,433-475`].
    arrow-go's client offers `GetSqlInfo`, `GetCatalogs`,
    `GetDBSchemas`, `GetTables`, `GetTableTypes` and `DoGet`
    [`vendor/.../flightsql/client.go:342-378,462,486`]. The table
    fixture draws a table and creates it as a user
    [`internal/qdbtest/table/table.go:205,482`].
15. The file comment of `flightsql_test.go` says the catalog commands
    get their tests when they land
    [`internal/flightsql/flightsql_test.go:1-4`].
16. ADR-0018 fixes: `GetSqlInfo` describes the server fully, with the
    field list the handler's, described for DuckDB as the SQL engine
    and the native engine in the server name (Decision 10); the
    catalog commands are in this milestone over a table list core in
    `internal/qdb` that the exploration milestone reuses (Decision
    11); everything else is `UNIMPLEMENTED` (Decision 12); the status
    codes (Decision 13) [`docs/adr/0018-flight-sql.md:78-98`]. The
    handoff says `GET /api/v2/tables` wraps the table list core
    [`docs/log.md:46-47`].

## 3. Design

### `internal/qdb/table.go`, after `RemoveTable`

```go
// Tables lists the names of the cluster's tables as user u, in the order
// the cluster reports them, qdbd's own $qdb. tables included. [fact 2; Rationale: system tables]
// It is retried once on a retryable failure, as a read is. [fact 3]
func (c *Cluster) Tables(ctx context.Context, u User) ([]string, error) {
	// style: doc comment + numbered overview + step comments
	// because: the names come out of a query result whose buffers the
	// sink must not keep, and the query form is one the headers do not
	// document, so both facts need telling at the step
	//
	// The list is the native query `show tables`, because the C API has no
	// enumeration call and that query is the server's own notion of a
	// table, answered in one round trip [facts 1, 2; Rationale: show tables]
	//   - rejected: a prefix scan over the empty prefix with a metadata call
	//     per alias, because it is N+1 round trips over every entry type
	//     [Rationale: show tables]
	//
	// 1. run the query under the read retry [fact 3]
	// 2. inside the sink, copy each value of the one string column out,
	//    because the batch is released when the sink returns and a Go
	//    string of an Arrow array aliases its buffer [fact 3]
	//    - the column is read by position, as the result has one column;
	//      a result of another shape is an error naming it, because a
	//      server change would otherwise read as an empty cluster [fact 2]
}
```

### `internal/qdb/cluster_test.go`, after `TestSlices`

```go
// TestTables: a table created as the user appears in Tables on both
// clusters and is gone from it once removed. [fact 4]
func TestTables(t *testing.T) {
	// style: doc comment alone
	// because: create, list, remove, list, four assertions and no branch
	//
	// at the name: qdbtest_ plus a fresh suffix, so a killed run is found
	//   by the qdbsh sweep the rules name [internal/AGENTS.md, Tests]
	// at the create: CreateTable with one double column, removed on
	//   cleanup, so the test needs no fixture [fact 4]
}
```

### `internal/flightsql/server.go`, `New`

`New` gains a step between registering the service and listening, and
its overview gains the matching line:

```go
	// 4. register what GetSqlInfo answers, because arrow-go's base server
	//    serves GetSqlInfo from its registered map and answers NOT_FOUND
	//    when the map is empty [fact 5]
	//    if err := registerSqlInfo(&s.BaseServer); err != nil { return nil, err }
```

The listening step becomes 5.

### `internal/flightsql/sqlinfo.go`, new file

```go
// registerSqlInfo registers on b what GetSqlInfo answers: the server, its
// versions, what it does not do, and DuckDB's identifier rules. [fact 16, Decision 10]
// It fails only for a value of a type the base server refuses, which no
// value here is. [fact 5]
func registerSqlInfo(b *flightsql.BaseServer) error {
	// style: doc comment alone + inline note at each group
	// because: one list of register calls, grouped by what they say
	//
	// at the server group: the name carries both engines, because a driver
	//   shows it and the query text alone says which engine runs
	//   [fact 16, Decision 10]; the version is build.Version [fact 11]; the
	//   Arrow version is arrow.PkgVersion [fact 10]
	// at the capability group: read-only true, SQL true, Substrait false,
	//   transactions NONE and TransactionsSupported false, cancel false,
	//   DDL catalog, schema and table false, because the Flight plane
	//   runs queries and nothing else (ADR-0018, Decisions 9 and 12)
	//   [fact 16]
	// at the identifier group: identifiers case-insensitive quoted or not,
	//   quote char the double quote, as DuckDB has them, because the owner
	//   chose DuckDB as the SQL engine the drivers are told about
	//   [fact 16, Decision 10; Rationale: SqlInfo fields]
	//   - rejected: a keyword and function list, because no document asks
	//     for one and a static copy of DuckDB's would go stale with every
	//     bump [Rationale: SqlInfo fields]
}
```

### `internal/flightsql/catalog.go`, new file

```go
// The one cluster's place in Flight SQL's catalog tree: a driver shows a
// table under catalog qdb and schema main, the names a #sql query takes,
// so the tree view and the query agree. [fact 12; Rationale: catalog shape]
const (
	catalogName = "qdb"
	schemaName  = "main"
	tableType   = "TABLE"
)

// infoFor answers the FlightInfo of a catalog command: one endpoint whose
// ticket is the command itself, so DoGet decodes it and the server keeps
// nothing between the calls, with the command's reference schema and
// unknown totals. [facts 5, 6; ADR-0018, Decision 9]
func infoFor(desc *flight.FlightDescriptor, schema *arrow.Schema) *flight.FlightInfo {
	// style: doc comment alone
	// because: one struct literal, the shape GetFlightInfoSqlInfo answers
	//
	// at the schema: the router's GetFlightInfo sets no schema for a catalog
	//   command, so each handler serializes its own [fact 6]
}

// oneChunk answers rec as the schema and channel a DoGet method returns,
// the channel closed after the one chunk, as the base server's
// DoGetSqlInfo answers its batch. [facts 5, 9]
func oneChunk(ctx context.Context, rec arrow.RecordBatch) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// style: doc comment alone
	// because: a record reader over one batch handed to
	// StreamChunksFromReader, as DoGetSqlInfo does
	//
	// at the reader: NewRecordReader retains rec, and the caller's release
	//   after this call leaves the reader's reference, which
	//   StreamChunksFromReader releases when drained [fact 9]
}

// likePattern answers whether s matches the SQL LIKE pattern p of a Flight
// SQL filter, where % matches any run and _ one character and every other
// byte matches itself. A nil p matches everything. [fact 8]
func likePattern(p *string, s string) bool {
	// style: doc comment alone
	// because: a translation of the two wildcards into a regexp, anchored
	//
	// at the translation: the pattern is quoted with regexp.QuoteMeta
	//   around the wildcards, so a dot or a bracket in a table name is
	//   literal [fact 8]
	// at the compile: the regexp is compiled per call, because a catalog
	//   call is rare and a cache is state [no cache, by the no-state rule
	//   of internal/AGENTS.md, Code]
}

// GetFlightInfoCatalogs answers the info of the one catalog. [fact 6]
func (s *Server) GetFlightInfoCatalogs(_ context.Context, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment alone
	// because: infoFor with schema_ref.Catalogs
}

// DoGetCatalogs answers one row, the catalog qdb. [Rationale: catalog shape]
func (s *Server) DoGetCatalogs(ctx context.Context) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// style: doc comment alone
	// because: one builder over schema_ref.Catalogs and oneChunk
}

// GetFlightInfoSchemas answers the info of the schema listing. [fact 6]
func (s *Server) GetFlightInfoSchemas(_ context.Context, _ flightsql.GetDBSchemas, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment alone
	// because: infoFor with schema_ref.DBSchemas
}

// DoGetDBSchemas answers one row, catalog qdb and schema main, or no row
// when cmd's catalog or schema pattern excludes it. [fact 8; Rationale: catalog shape]
func (s *Server) DoGetDBSchemas(ctx context.Context, cmd flightsql.GetDBSchemas) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// style: doc comment alone + inline note at the filter
	// because: one guard and one builder
	//
	// at the filter: a catalog other than qdb, or a pattern main does not
	//   match, is an empty result and not an error, because a driver that
	//   walks several catalogs asks for each and expects the empty ones
	//   to answer empty [fact 8]
}

// GetFlightInfoTables answers the info of the table listing, and
// UNIMPLEMENTED when cmd asks for each table's schema. [fact 6; Rationale: include_schema]
func (s *Server) GetFlightInfoTables(_ context.Context, cmd flightsql.GetTables, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment alone + inline note at the refusal
	// because: one guard and infoFor with schema_ref.Tables
	//
	// at the refusal: include_schema is refused here and not at DoGet, so
	//   the driver learns it before a ticket exists, and the refusal is
	//   UNIMPLEMENTED because the router's GetSchema promises the
	//   five-column shape for the flag and this unit cannot fill its
	//   fifth column [facts 6, 7; Rationale: include_schema]
	//   - rejected: ignoring the flag and answering four columns, because
	//     the driver would read a result that differs from the schema
	//     GetSchema promised [Rationale: include_schema]
}

// DoGetTables answers one row per table name the cluster reports as the
// caller's user, under catalog qdb, schema main and type TABLE, filtered
// by cmd's table name pattern, and empty when cmd's catalog, schema
// pattern or table types exclude the cluster. A cmd with include_schema is
// UNIMPLEMENTED. [facts 2, 8; Rationale: catalog shape, include_schema]
func (s *Server) DoGetTables(ctx context.Context, cmd flightsql.GetTables) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// style: doc comment + numbered overview + step comments
	// because: a refusal, a filter that can short-circuit, a cluster call
	// with its own error mapping, and a second filter per row
	//
	// 1. refuse include_schema again, because a ticket is bytes a client
	//    can mint without GetFlightInfo [Rationale: include_schema]
	// 2. a catalog other than qdb, a schema pattern main does not match,
	//    or table types without TABLE answer the empty result before the
	//    cluster is called, because the answer needs no listing [fact 8]
	// 3. the user from the claims, zero claims for the anonymous user, as
	//    DoGet derives it [fact 13]
	// 4. Cluster.Tables, mapped through statusOf with INTERNAL as the
	//    answered code, because nothing in the request reaches the engine
	//    as text, so a message from the engine is the server's failure
	//    and not the caller's [fact 13; Rationale: listing errors]
	// 5. one row per name the pattern admits, in the cluster's order,
	//    through oneChunk [fact 8]
}

// GetFlightInfoTableTypes answers the info of the table type listing. [fact 6]
func (s *Server) GetFlightInfoTableTypes(_ context.Context, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment alone
	// because: infoFor with schema_ref.TableTypes
}

// DoGetTableTypes answers one row, TABLE, because the native engine has no
// views and qdb-duck's views are DuckDB's. [Rationale: table types]
func (s *Server) DoGetTableTypes(ctx context.Context) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// style: doc comment alone
	// because: one builder over schema_ref.TableTypes and oneChunk
}
```

### `internal/flightsql/flightsql_test.go`

The file comment drops "The catalog commands get their tests when they
land." [fact 15]. Helpers, before use:

```go
// catalogRows presents info's one ticket on DoGet and answers the stream
// concatenated into one batch the caller releases. [fact 14]
func catalogRows(t *testing.T, cl *flightsql.Client, ctx context.Context, info *flight.FlightInfo, err error) arrow.RecordBatch {
	// style: doc comment alone
	// because: ticket out of info, readAll, concat
}

// column answers the string column named name of rec as a slice. [fact 7]
func column(t *testing.T, rec arrow.RecordBatch, name string) []string {
	// style: doc comment alone
	// because: one lookup by name and one loop
}
```

Tests, in this order:

```go
// TestSqlInfo: the registered ids read back with their values on the
// insecure cluster, and an id the server does not register is NOT_FOUND. [facts 5, 10]
func TestSqlInfo(t *testing.T) {
	// style: doc comment alone + inline note at the union
	// because: one request, one batch, a lookup per asserted id
	//
	// at the cluster: the insecure cluster alone, because no row reaches
	//   the cluster [internal/AGENTS.md, Tests, the TestBearerRows precedent]
	// at the union: the value column is a dense union, so each row is read
	//   through its type code and child, and the asserted rows are the
	//   server name, read-only, transaction NONE and the quote char, one
	//   per value type [fact 10]
	// at the order: rows are looked up by id and not by position, because
	//   DoGetSqlInfo iterates a map when no id is requested [fact 5]
}

// TestCatalog: on both clusters, GetCatalogs, GetDbSchemas and
// GetTableTypes answer their one row, GetTables lists a generated table
// under qdb, main and TABLE, the table name pattern narrows the list to
// it, an excluding catalog answers no row, and include_schema is
// UNIMPLEMENTED on GetFlightInfo and on DoGet. [facts 7, 8, 14]
func TestCatalog(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: one server and one table serve six assertions whose order
	// is the driver's connect sequence
	//
	// 1. one generated table created as the fixture's user, drawn once
	//    through rapid.Check with one iteration bound, because the listing
	//    is not data-shaped and one table is enough [fact 14]
	// 2. the three fixed commands, each one row [Rationale: catalog shape,
	//    table types]
	// 3. GetTables without a pattern contains the table's name [fact 2]
	// 4. GetTables with the table's name as the pattern is that one row,
	//    with a % pattern over its qdbtest_ prefix containing it [fact 8]
	// 5. GetTables with catalog "other" is no row [fact 8]
	// 6. include_schema: GetTables fails UNIMPLEMENTED, and a ticket built
	//    by hand from CommandGetTables with the flag fails UNIMPLEMENTED on
	//    DoGet [Rationale: include_schema]
}

// TestCatalogRows: the listing's codes on the insecure cluster: an
// unreachable cluster is UNAVAILABLE and a bad token UNAUTHENTICATED. [facts 13, 14]
func TestCatalogRows(t *testing.T) {
	// style: doc comment alone
	// because: a table of two rows, the shape of TestStatementRows
	//
	// at the unreachable row: qdb://127.0.0.1:1 with a breaker of one
	//   failure, as TestStatementRows builds it [fact 14]
}
```

### `internal/flightsql/AGENTS.md`, new section `## Catalog` after Statement

```
- GetSqlInfo is arrow-go's: `New` registers the values through
  `registerSqlInfo`, and the base server serves them. The list is the
  server, its versions, what it does not do, and DuckDB's identifier
  rules, with no keyword list. `registerSqlInfo` says why DuckDB is
  the engine described (ADR-0018, Decision 10; owner, 2026-10-22).
- The one cluster is catalog `qdb`, schema `main`, table type `TABLE`,
  the names a `#sql` query takes, so a driver's tree view and the query
  agree (owner, 2026-10-22). The constants of `catalog.go` carry it.
- GetTables answers `Cluster.Tables` under the request's table name
  pattern, applied in Go because the server has no filtered form.
  qdbd's own `$qdb.` tables are listed (owner, 2026-10-22). A request
  with include_schema is UNIMPLEMENTED on GetFlightInfo and on DoGet
  until the schema column lands; `GetFlightInfoTables` says why the
  flag is refused and not ignored (owner, 2026-10-22).
- A failed listing goes through `statusOf` with INTERNAL as its
  answered code, because nothing in the request reaches the engine as
  text. `DoGetTables` says so at the call.
- Each catalog GetFlightInfo answers one endpoint whose ticket is the
  command and runs nothing, and the catalog DoGets are the base
  server's channel methods, because the results are small and the
  router already dispatches them. `infoFor` and `oneChunk` carry it.
```

Under Tests, one bullet: `TestSqlInfo` and `TestCatalogRows` run on
the insecure cluster, because no row reaches a secured check;
`TestCatalog` draws a table and reads it back on both clusters.

### `internal/AGENTS.md`, Code, one bullet after the query bullet

```
- The table list is `Cluster.Tables`, the native `show tables` query
  under the read retry, answering names in the cluster's order with
  qdbd's own `$qdb.` tables included. The C API has no enumeration
  call, and `Tables` says why the query is the core and what was
  rejected. The Flight catalog reads it, and `GET /api/v2/tables`
  wraps it (ADR-0018, Decision 11).
```

## 4. Rationale

| decision                                                                                     | why                                                                                                                                                                           | rejected, and why                                                                                                                                                                                     | gained                                                               | given up                                                                                      | settled by                                   |
| -------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | -------------------------------------------- |
| show tables: the table list core runs the native `show tables` query through `Cluster.Query` | one round trip, the server's own notion of a table, an Arrow batch already, the read retry applies                                                                            | a prefix scan over the empty prefix with a metadata call per alias: N+1 round trips over every entry type, and a new `Session` method for `PrefixGet`                                                 | one method, no new binding surface                                   | a dependency on a query form the C headers do not document (section 8, question 1)            | owner, 2026-10-22                            |
| catalog shape: catalog `qdb`, schema `main`, one row each                                    | a `#sql` query already addresses a table as `qdb.<name>`, so the tree view a driver shows and the query agree                                                                 | null catalog and schema with zero rows: legal, but the tree view shows an unnamed node and the names differ from what `#sql` needs; the cluster URI as catalog: a name no query accepts               | the two engines agree on where a table lives                         | the schema name `main` is assumed from DuckDB's default until checked (section 8, question 2) | owner, 2026-10-22                            |
| system tables: the core lists what the server lists, `$qdb.` included                        | hiding one needs a prefix rule this project would own, and a client that queries `$qdb.firehose` finds it                                                                     | a filter on the `$qdb.` prefix in `internal/qdb`                                                                                                                                                      | no rule of this project's                                            | a driver's tree view shows qdbd's tables                                                      | owner, 2026-10-22                            |
| include_schema: UNIMPLEMENTED on GetFlightInfo and on DoGet until the follow-up              | the router's GetSchema promises the five-column shape for the flag, and this unit cannot fill its fifth column; an honest code beats a wrong shape                            | ignoring the flag: the result's schema differs from the one promised, and how each driver copes is unverified; an empty schema per table: looks like a server answer and shows tables with no columns | drivers' table listings work; column listings fail with a clear code | JDBC's getColumns and ADBC's GetObjects at column depth fail until the next slice             | owner, 2026-10-22                            |
| SqlInfo fields: server, versions, capabilities, DuckDB's identifier rules, no keyword list   | the owner left the field list to the handler and named DuckDB as the engine described; a keyword list has no document asking for it and would go stale with every DuckDB bump | a static copy of DuckDB's keyword and function lists                                                                                                                                                  | a short list that stays true                                         | a driver that reads keywords for quoting decisions gets none                                  | ADR-0018, Decision 10; proposal for the list |
| table types: one type, `TABLE`                                                               | the native engine has no views, and qdb-duck's views live in DuckDB, which the native listing does not see                                                                    | a `VIEW` type from DuckDB's catalog: a second listing source for one command                                                                                                                          | one source of names                                                  | views created through `#sql` are not listed                                                   | proposal                                     |
| name pattern in Go                                                                           | `show tables` has no filtered form (fact 2)                                                                                                                                   | none available                                                                                                                                                                                        | the pattern works                                                    | the whole list crosses the wire per call                                                      | tree, fact 2                                 |
| listing errors: `statusOf` with INTERNAL as the answered code                                | nothing in a GetTables request reaches the engine as text, so an engine message is the server's failure                                                                       | INVALID_ARGUMENT as the statement uses: blames the caller for a request that has no text                                                                                                              | codes that say who failed                                            | none                                                                                          | proposal                                     |
| catalog DoGets on the base server's channel methods                                          | the results are small and the router already dispatches them                                                                                                                  | a second override in `service.DoGet` as the statement has: a goroutine-free path the catalog does not need                                                                                            | no new routing                                                       | a goroutine per catalog call, arrow-go's                                                      | proposal                                     |
| GetSqlInfo registered in `New`, not in a package-level map                                   | the base server's map is per instance and has no lock, and the no-state rule                                                                                                  | a package-level `init` registering on a global                                                                                                                                                        | each server owns its map                                             | none                                                                                          | tree, fact 5; `internal/AGENTS.md`, Code     |

## 5. Knowledge

Per commit, where each why lands.

- Commit 2, `Cluster.Tables`: the overview carries the query choice and
  the rejected prefix scan (Rationale: show tables), the copy-out of
  the strings (fact 3) and the shape check (fact 2). `internal/AGENTS.md`
  gains the table list bullet. Facts 1 and 2 live in the overview.
- Commit 3, `TestTables`: the name and create notes.
- Commit 4, `registerSqlInfo` and `New`'s step 4: the three group
  notes carry Decision 10, the DuckDB choice and the rejected keyword
  list (Rationale: SqlInfo fields). `internal/flightsql/AGENTS.md`,
  Catalog, first bullet.
- Commit 5, the constants, `infoFor`, `oneChunk`, the catalog, schema
  and table type handlers: the constants' comment carries the catalog
  shape (Rationale: catalog shape); `DoGetTableTypes` carries the one
  type (Rationale: table types); `infoFor` carries the stateless ticket
  (ADR-0018, Decision 9) and the schema the router does not set;
  `oneChunk` the retain rule; `DoGetDBSchemas` the empty-not-error
  filter. `AGENTS.md`, Catalog, second and fifth bullets.
- Commit 6, `likePattern`, `GetFlightInfoTables`, `DoGetTables`: the
  refusal and its rejected alternative (Rationale: include_schema), the
  pre-listing filter, the user from the claims, the error code
  (Rationale: listing errors), the pattern in Go (Rationale: name
  pattern). `AGENTS.md`, Catalog, third and fourth bullets.
- Commit 7, the tests: the file comment, the helpers, the three tests
  with their notes. `AGENTS.md`, Tests, one bullet.
- The seed report's separate docs commit is folded into commits 2 to
  7: each `AGENTS.md` row lands with the code it describes.
- Commit 9, the plan's deletion: `docs/log.md` Current state moves
  this item out of Next, names the include_schema slice first in Next,
  and the entry names the homes above. Section 8's answers, once
  given, land in the `AGENTS.md` bullets they qualify.

Every Rationale row lands: show tables and system tables in
`Cluster.Tables` and `internal/AGENTS.md`; catalog shape in the
constants and `AGENTS.md`; include_schema in `GetFlightInfoTables`,
`DoGetTables` and `AGENTS.md`; SqlInfo fields in `registerSqlInfo` and
`AGENTS.md`; table types in `DoGetTableTypes`; name pattern in
`DoGetTables`; listing errors in `DoGetTables` and `AGENTS.md`; the
channel methods in `AGENTS.md`; the per-instance map in `New`'s step.

## 6. How the knowledge lands

The build stage is `/rr-build docs/flight-catalog-plan.md`, which
writes every comment and document row of sections 3 and 5 in the same
commit as the code and reconciles the code against this plan before
the build-stage message.

Specific to this unit:

- Before commit 5, the executor checks the schema name of qdb-duck's
  catalog with one `#sql` query against the insecure cluster
  (`select schema_name from information_schema.schemata where
catalog_name = 'qdb'`, through `Cluster.Duck` in a throwaway test or
  through the built binary). If it is not `main`, `schemaName` takes
  the name found and the owner is told in the build-stage message; the
  Rationale row and the `AGENTS.md` bullet say the name is qdb-duck's.
- Commit 2's shape check (one string column) is written from what the
  query answers on the test cluster at build time; if the column name
  or type differs from fact 2, the executor stops and asks the owner
  before committing.
- Commit 4's identifier claims about DuckDB are stated as DuckDB's
  rules, cited to the DuckDB documentation in the comment, because the
  tree holds no evidence for them.

## 7. Commits

1. `docs(plan): flight-catalog-plan.md, GetSqlInfo and the catalog commands on a table list core`
   (this document)
2. `feat(qdb): Cluster.Tables lists the cluster's table names through the show tables query`
3. `test(qdb): a table created as the user appears in Cluster.Tables on both clusters`
4. `feat(flightsql): New registers GetSqlInfo with the server name, versions, read-only, no transactions and DuckDB's identifier rules`
5. `feat(flightsql): GetCatalogs, GetDbSchemas and GetTableTypes answer the one cluster as qdb, main and TABLE`
6. `feat(flightsql): GetTables answers Cluster.Tables under the name pattern, and include_schema is UNIMPLEMENTED`
7. `test(flightsql): GetSqlInfo and the catalog commands read back through arrow-go's client on both clusters`
8. `/doc-discipline all internal/qdb internal/flightsql docs/flight-catalog-plan.md`,
   then `/doc-discipline all check internal/qdb internal/flightsql docs/flight-catalog-plan.md`;
   findings become further small commits before the next step.
9. `docs(log): flight-catalog-plan.md deleted, facts moved to internal/flightsql/AGENTS.md, internal/AGENTS.md and the comments of internal/qdb and internal/flightsql`
10. Verify: push `sc-19567/rr-flight-catalog`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again.

## 8. Open questions and recommendations

1. Whether `show tables` is a query R&D supports, as the C headers do
   not document it. Recommendation: build on it, as the owner chose,
   and ask R&D once; if it is not supported, the core becomes the
   prefix scan of the Rationale and nothing above `internal/qdb`
   changes.
2. Whether qdb-duck's attached catalog uses schema `main`.
   Recommendation: `main`, checked at build time as section 6 says.
3. Whether GetSqlInfo carries a keyword list. Recommendation: no list,
   as the Rationale says.
4. The DuckDB identifier rules are cited to DuckDB's documentation and
   not to the tree. Recommendation: accept the citation; a test
   against the DuckDB instance would pin DuckDB, which this project
   does not test.
