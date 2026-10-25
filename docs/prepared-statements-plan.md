# Prepared statements on the Flight server, and the JDBC smoke step

Status: draft

## Outcome

When this unit lands, Arrow's Flight SQL JDBC driver runs queries on both
engines through the Flight port, and the round trip proves it on both
clusters.

- Each engine answers the schema of a query without running it. The
  native engine asks the C API's validate call through qdb-api-go's
  `Query.ValidateArrow`. DuckDB prepares the statement and this repo maps
  its column types to the Arrow types DuckDB's own conversion answers.
- `query.Describe` dispatches a query to the describe of its engine by
  the `#sql` prefix, as `query.Run` dispatches a run.
- The Flight server answers `CreatePreparedStatement` with the query
  text as the handle and the described schema, and
  `ClosePreparedStatement` with nothing. `GetFlightInfoPreparedStatement`
  answers the statement ticket of the handle, so the existing `DoGet`
  streams it. The statement's `FlightInfo` carries the described schema,
  and `GetSchemaStatement` and `GetSchemaPreparedStatement` answer it.
- A `#sql` text of more than one statement, and a query with parameter
  markers, fail the describe with `INVALID_ARGUMENT` on the Flight port.
- The round trip reads each table and the types query through a Maven
  project that drives Arrow's JDBC driver, on both clusters.
- ADR-0018, the brief, `docs/e2e.md` and the `AGENTS.md` files describe
  the prepared statements, the describe and the divergences this unit
  accepts.

Left for later units:

- Parameters: the parameter upload (`DoPutPreparedStatementQuery`) stays
  UNIMPLEMENTED (owner, 2026-10-24).
- The qdbd story for validate's disagreement with execution (Open
  questions).
- Refusing a multi-statement `#sql` text on `POST /api/v2/query`, which
  runs one today (Open questions).
- The Buildkite run that closes M5 is this unit's Verify step, and the
  log closes M5 in a later entry if it is green.

## Verified facts

- qdb-api-go PR #129 merged on 2026-10-25 as `6afd5e8` on master, and
  master was identical to that commit on 2026-10-25 (GitHub compare API).
  It adds `Query.Validate() (*QueryResult, error)` in `query.go` and
  `Query.ValidateArrow() (*arrow.Schema, error)` in `query_arrow.go`.
- `ValidateArrow` answers nil and no error for a statement without a
  result set (DDL, a plain insert), the same errors `FetchArrow` answers
  for a parse error, an unknown table and an unknown column
  (`ErrInvalidQuery`, `ErrAliasNotFound`, `ErrColumnNotFound`), every field
  nullable and no field metadata, and releases its C result before it
  returns (PR #129 diff, `query_arrow.go`; its tests in
  `query_arrow_test.go` and `query_test.go`).
- Validate executes nothing. It parses and runs the read-only prepare
  steps, and an insert keeps the table's row count (PR #129,
  `TestFetchArrowEdgeCases`, "validate does not execute an insert";
  `~/git/quasardb/api/src/query.cpp:40-47`).
- PR #129's tests pin the validated schema equal to the fetched one,
  metadata stripped, for `SELECT *` over a table of every column type and
  for count, min, max, first, last, sum and avg over each column type
  (`requireValidateArrowMatches`).
- Validate and the fetch disagree in three places, all inside quasardb:
  - The fetch adds a `max_width` key to string and blob fields, computed
    from the data (`~/git/quasardb/qdb/query_results/arrow_result.cpp`,
    the export just above line 1074). Validate has no data and adds none.
  - A bare NULL literal executes as a column of doubles holding NaN
    (`~/git/quasardb/qdb/query_results/execute_expression.cpp:751-758`),
    while validate's static typer reports it as unknown, which
    `ValidateArrow` answers as the null type
    (`~/git/quasardb/qdb/query_client/schema_generator.hpp:345-360`).
  - A statement other than SELECT runs on the row path, whose Arrow
    conversion types a column from its first non-null cell
    (`~/git/quasardb/qdb/query_client/run.cpp:289-305`,
    `arrow_result.cpp:1074-1083`), so a column of no rows or only nulls
    streams as the null type, while validate reports the declared type
    (`schema_generator.hpp:102-105` for `show tables`).
- duckdb-go's `Conn.PrepareContext` executes every statement of the
  text before the last
  (`vendor/github.com/duckdb/duckdb-go/v2/connection.go:219-245`).
  `Conn.Prepare`, the `driver.Conn` method, prepares one statement and
  fails a text of several with `errMissingPrepareContext`, or an empty
  text with `errEmptyQuery`, neither a `*duckdb.Error`
  (`connection.go:122-136`, `errors.go:130-132`). DuckDB's own parse and
  bind errors are `*duckdb.Error` (`errors.go:274-315`).
- `duckdb.Stmt` answers `ColumnCount`, `ColumnName`, `ColumnTypeInfo` and
  `NumInput` without executing (`statement.go:86-92`, `593-650`). A
  `TypeInfo` answers its `Type` and, for DECIMAL, ENUM, LIST, ARRAY, MAP,
  STRUCT and UNION, its details (`type_info.go:55-110`, `144-152`). Its
  logical type is unexported, so this repo cannot hand it to DuckDB's own
  Arrow conversion.
- DuckDB 1.5.4, the version qdb-duck pins (`~/git/qdb-duck` submodule
  `duckdb` at `v1.5.4`), converts a logical type to Arrow in
  `SetArrowFormat` (`src/common/arrow/arrow_converter.cpp:117-410`).
  Under the default client properties (`client_properties.hpp:29-35`:
  regular offsets, no list views, no string views, lossless conversion
  off, output version 1.0) it answers:

  | DuckDB type                                                                | Arrow type                                      |
  | -------------------------------------------------------------------------- | ----------------------------------------------- |
  | BOOLEAN                                                                    | bool                                            |
  | TINYINT, SMALLINT, INTEGER, BIGINT                                         | int8, int16, int32, int64                       |
  | UTINYINT, USMALLINT, UINTEGER, UBIGINT                                     | uint8, uint16, uint32, uint64                   |
  | FLOAT, DOUBLE                                                              | float32, float64                                |
  | HUGEINT, UHUGEINT                                                          | decimal128(38, 0)                               |
  | DECIMAL(w, s)                                                              | decimal128(w, s), 128 bits below version 1.5    |
  | VARCHAR, UUID                                                              | utf8                                            |
  | BLOB, BIT                                                                  | binary                                          |
  | DATE                                                                       | date32                                          |
  | TIME, TIME_TZ                                                              | time64[us]                                      |
  | TIMESTAMP, \_S, \_MS, \_NS                                                 | timestamp[us], [s], [ms], [ns], no zone         |
  | TIMESTAMP_TZ                                                               | timestamp[us] with the connection's TimeZone    |
  | INTERVAL                                                                   | month_day_nano_interval                         |
  | SQLNULL                                                                    | null                                            |
  | LIST(t)                                                                    | list with child `l` of t                        |
  | ARRAY(t, n)                                                                | fixed_size_list(n) with an unnamed child of t   |
  | STRUCT                                                                     | struct of the entries' names and types          |
  | MAP(k, v)                                                                  | map with a non-nullable `entries` struct        |
  | UNION                                                                      | sparse union, type codes 0 to n-1, member names |
  | ENUM                                                                       | dictionary of uint8, uint16 or uint32 over utf8 |
  | an aliased type with an Arrow extension, BIGNUM, GEOMETRY, VARIANT, others | an Arrow extension type, or an error            |

  Every field is nullable (`InitializeChild`, `arrow_converter.cpp:47-61`)
  except a map's `entries` (`:101-102`). The TIMESTAMP_TZ zone is the
  `TimeZone` setting of the connection's client context
  (`src/main/client_context.cpp:1474-1480`).

- `internal/olap/AGENTS.md` records that the instance's TimeZone defaults
  to the host's zone and that the endpoint answers what DuckDB answers.
- A bare NULL in a DuckDB result is int32 (`TestDuckDBScalars`,
  `internal/httpapi/duckdb_test.go:42`).
- arrow-go's base server routes `CommandPreparedStatementQuery` on
  GetFlightInfo, GetSchema and DoGet, and the `CreatePreparedStatement`
  and `ClosePreparedStatement` actions, to hooks whose defaults answer
  UNIMPLEMENTED (`vendor/github.com/apache/arrow-go/v18/arrow/flight/flightsql/server.go:324-345`,
  `496-521`, `768`, `844`, `892`, `1231-1316`). It serializes the
  result's `DatasetSchema` with `flight.SerializeSchema` and skips a nil
  one (`:1250-1257`).
- arrow-go's client answers a prepared statement through
  `Client.Prepare`, `PreparedStatement.DatasetSchema`,
  `PreparedStatement.Execute`, `PreparedStatement.GetSchema` and
  `PreparedStatement.Close`, and a statement's schema through
  `Client.GetExecuteSchema` (`client.go:147`, `504`, `1137`, `1365`,
  `1389`, `1446`).
- Flight SQL has no `SqlInfo` key for prepared statements. Nothing in
  `vendor/github.com/apache/arrow-go/v18/arrow/flight/flightsql/types.go`
  names one, and `internal/flightsql/sqlinfo.go` registers none. ADR-0018,
  Decision 10, says `GetSqlInfo` describes "no prepared statements",
  which no registered value carries.
- Arrow's Flight SQL JDBC driver 19.0.0 on JDK 21 sends every
  `executeQuery` as `CreatePreparedStatement`, then GetFlightInfo of the
  prepared statement, DoGet and `ClosePreparedStatement`, and classes a
  statement as an update unless the prepare answers a dataset schema with
  fields (`docs/e2e.md`, The Flight SQL smoke steps, observed
  2026-10-24). This machine has `java` (OpenJDK 21.0.13), `mvn` and the
  driver in `~/.m2` (2026-10-25). Nothing under `.buildkite` or
  `scripts/cicd` installs or names Java or Maven (2026-10-25).

## Design

### `go.mod`, `vendor/`

```text
- qdb-api-go moves to the pseudo-version of 6afd5e8 on master
  [Verified facts; internal/AGENTS.md, Building]
- go get github.com/bureau14/qdb-api-go/v3@6afd5e8, go mod tidy, go mod vendor;
  nothing under vendor/ is written by hand [internal/AGENTS.md, Building]
```

### `internal/qdb/cluster.go`

```go
// describe answers the schema q's result would have, without running q.
// A statement without a result set answers the empty schema, as fetch
// stands one in.
func (s *Session) describe(q string) (*arrow.Schema, error) {
	// style: doc comment + inline note at the nil branch
	// because: one call and one substitution
	//
	// at the nil branch: ValidateArrow answers nil for a statement with no
	//   result set, and the empty schema matches what Query hands the sink
	//   for it [PR #129 ValidateArrow; cluster.go:441-446]
}
```

```go
// Describe answers the Arrow schema of native query q's result as u,
// without running q. Field names, types and order are Query's for the
// same q. The fields carry no metadata, where Query's string and blob
// fields carry a max_width key. A bare NULL literal describes as the null
// type and a non-select statement's empty column as its declared type,
// where Query answers float64 and the null type. Errors are Query's.
func (c *Cluster) Describe(ctx context.Context, u User, q string, opts ...CallOption) (*arrow.Schema, error) {
	// style: doc comment alone
	// because: one leased call, the same shape as Query's fetch
	//
	// the divergences are qdbd's validate disagreeing with its own
	//   execution, accepted (Rationale, divergences) [Verified facts]
	// opts carry the read retry as Query's do, because validate holds a
	//   session for one round trip and sends nothing to the caller
	//   [query.go:65-74]
}
```

### `internal/qdb/cluster.go` (DuckDB plumbing)

```go
// withInstance runs f on u's DuckDB instance, opening it on u's first
// call, and counts the call as use of u's pool until f returns.
func (c *Cluster) withInstance(ctx context.Context, u User, f func(*olap.Instance) error) error {
	// style: doc comment + numbered overview + step comments
	// because: the steps of Duck move here unchanged, and their order is
	//   what keeps eviction from closing an instance in use
	//
	// 1. find the pool and count the call under one hold of mu
	//    [cluster.go:640-650]
	// 4. uncount the call, whatever happened [cluster.go:651-656]
	// 2. answer the instance [cluster.go:657-661]
	// 3. run f [cluster.go:662-663]
}
```

```go
// Duck: contract unchanged; its body becomes one withInstance call
// running in.Query(ctx, q, sink). Its numbered overview moves to
// withInstance with the steps.
```

```go
// DuckDescribe answers the Arrow schema of DuckDB query q's result on
// u's instance, without running q. Field names, types and order are the
// ones Duck streams for the same q. A text that is not one statement is
// olap.ErrNotOneStatement, a query with parameters is
// olap.ErrParameters, and a column type the describe does not map is
// olap.ErrUndescribable.
func (c *Cluster) DuckDescribe(ctx context.Context, u User, q string) (*arrow.Schema, error) {
	// style: doc comment alone
	// because: one withInstance call around Instance.Describe
	//
	// counted as use of the pool as Duck is, because the describe holds a
	//   connection of the instance [withInstance]
}
```

### `internal/olap/describe.go` (new file)

```go
// ErrNotOneStatement is the error of a text that holds no statement or
// more than one. A describe prepares exactly one statement.
var ErrNotOneStatement = errors.New("olap: a query is one statement")

// ErrParameters is the error of a statement with parameter markers,
// which nothing binds.
var ErrParameters = errors.New("olap: query parameters are not supported")

// ErrUndescribable is the error of a column whose DuckDB type the
// describe does not map to Arrow. It names the column and the type.
var ErrUndescribable = errors.New("olap: column type cannot be described")
```

```go
// arrowTypeOf answers the Arrow type DuckDB's Arrow conversion gives a
// column of t, under the default client properties, with tz as the
// zone of a TIMESTAMP_TZ. A type outside the conversion's own switch is
// ErrUndescribable.
func arrowTypeOf(t duckdb.TypeInfo, tz string) (arrow.DataType, error) {
	// style: doc comment + inline note at the nested cases and the
	//   default
	// because: one type switch that mirrors SetArrowFormat case by case,
	//   recursing for the nested types
	//
	// the cases are the table of Verified facts, which is
	//   arrow_converter.cpp:117-410 of DuckDB 1.5.4 under the defaults of
	//   client_properties.hpp:29-35 [Verified facts]
	// at HUGEINT and UHUGEINT: decimal128(38, 0), because lossless
	//   conversion is off [arrow_converter.cpp, HUGEINT case]
	// at DECIMAL: always 128 bits, because output version 1.0 is below
	//   1.5 [arrow_converter.cpp, DECIMAL case]
	// at LIST: the child is named l [arrow_converter.cpp, LIST case]
	// at MAP: the entries struct is not nullable
	//   [arrow_converter.cpp:101-102]
	// at ENUM: the index width follows the number of values as DuckDB's
	//   physical enum type does (checked at build, How the knowledge
	//   lands)
	// at the default: an extension or newer type has no case, and a
	//   wrong guess would promise a client a type the stream does not
	//   carry (Rationale, uncovered types)
	// rejected: a LIMIT 0 run through the reader, because it fails on a
	//   statement that cannot be a subquery; an upstream duckdb-go change,
	//   because it waits on an outside project (Rationale, DuckDB
	//   describe)
}
```

```go
// Describe answers the Arrow schema of q's result without running q, in
// the types Query streams for the same q. q is one statement without
// parameter markers. A text of no statement or several is
// ErrNotOneStatement, a statement with parameters is ErrParameters, a
// column type outside the map is ErrUndescribable, and a parse or bind
// error is DuckDB's.
func (i *Instance) Describe(ctx context.Context, q string) (*arrow.Schema, error) {
	// style: doc comment + numbered overview + step comments
	// because: five steps, three ways to fail, and an order that keeps
	//   the prepare from executing anything
	//
	// The statement is prepared and never executed, and the connection
	// lives for one describe, as Query's does [instance.go:190-224]:
	//
	// 1. open a connection of its own;
	// 2. prepare q as one statement;
	// 3. refuse parameter markers;
	// 4. read the TimeZone when a column is a TIMESTAMP_TZ;
	// 5. map each column to its field.
	//
	// 1. close the connection and the statement, whatever happened
	// 2. the driver.Conn Prepare, because PrepareContext executes every
	//    statement before the last [connection.go:219-245]
	//    an error that is not a *duckdb.Error is duckdb-go's own refusal of
	//    a text of several statements or of none, which becomes
	//    ErrNotOneStatement [connection.go:122-136; errors.go:130-132]
	// 3. NumInput above zero is ErrParameters, because nothing binds a
	//    value (Rationale, parameters) [statement.go:86-92]
	// 4. SELECT current_setting('TimeZone') on the same connection, only
	//    when a column needs it, because the reader tags TIMESTAMP_TZ with
	//    that setting [client_context.cpp:1474-1480]
	// 5. each field nullable, named by ColumnName, typed by arrowTypeOf
	//    [arrow_converter.cpp:47-61]
}
```

### `internal/query/query.go`

```go
// Describe answers the schema of q's result as u without running q, on
// the engine Run would run it on. The errors are those of
// qdb.Cluster.Describe and qdb.Cluster.DuckDescribe.
func Describe(ctx context.Context, u qdb.User, q string) (*arrow.Schema, error) {
	// style: doc comment alone
	// because: split, then one call per engine, as Run does
	//
	// the native describe takes the read retry, as Run's native query
	//   does [query.go:65-74]
}
```

The package doc comment says the Flight server calls `Run` and
`Describe`, and drops "will" [query.go:1-4].

### `internal/flightsql/statement.go`

```go
// describe answers q's schema as the call's user, and whether it has
// one. A column type the DuckDB describe does not map answers no schema
// and no error. Every other failure is statusOf's, with INVALID_ARGUMENT
// for what an engine answered.
func describe(ctx context.Context, q string) (*arrow.Schema, bool, error) {
	// style: doc comment + inline note at the ErrUndescribable branch
	// because: one call, one exception, one mapping
	//
	// the user comes from the claims, as DoGet derives it
	//   [statement.go:78-81]
	// at ErrUndescribable: no schema, so a plain statement over such a
	//   type runs as it does today, and only the calls whose answer is the
	//   schema fail (Rationale, uncovered types)
	// ErrNotOneStatement and ErrParameters fall to statusOf's
	//   INVALID_ARGUMENT, because the request is at fault (owner,
	//   2026-10-25; Rationale, parameters)
}
```

```go
// schemaOf answers q's schema for a call whose whole answer is the
// schema: the prepare and both schema calls. A column type the describe
// does not map is INVALID_ARGUMENT naming the column and the type.
func schemaOf(ctx context.Context, q string) (*arrow.Schema, error) {
	// style: doc comment alone
	// because: describe, with the one exception turned into a status
}
```

```go
// GetFlightInfoStatement: contract rewritten. The info is one endpoint
// whose ticket is the query text, the described schema when there is
// one, no location and unknown totals. A query that fails to describe
// fails here with describe's status, before any ticket.
func (s *Server) GetFlightInfoStatement(ctx context.Context, q flightsql.StatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment + inline notes
	// because: the existing comment loses the claim that nothing knows the
	//   schema, and gains the describe
	//
	// the two-call explanation of Flight stays [statement.go:19-29]
	// the describe runs here and the query at DoGet, so the instance
	//   still stores nothing between calls (ADR-0018, Decision 9)
	// the schema is serialized with flight.SerializeSchema, as the base
	//   server serializes a prepare's [server.go:1250-1257]
	// totals stay -1, because nothing has run [statement.go:41-43]
}
```

```go
// GetSchemaStatement answers the described schema of the statement's
// query, without running it.
func (s *Server) GetSchemaStatement(ctx context.Context, q flightsql.StatementQuery, _ *flight.FlightDescriptor) (*flight.SchemaResult, error) {
	// style: doc comment alone
	// because: schemaOf and one serialization
}
```

### `internal/flightsql/prepared.go` (new file)

```go
// CreatePreparedStatement answers the query text as the handle and the
// query's described schema as the dataset schema, with no parameter
// schema. The server keeps nothing, so the handle is the whole
// statement.
func (s *Server) CreatePreparedStatement(ctx context.Context, req flightsql.ActionCreatePreparedStatementRequest) (flightsql.ActionCreatePreparedStatementResult, error) {
	// style: doc comment + inline notes
	// because: three facts a reader needs at the lines, no sequence
	//
	// the handle is the text verbatim, so any instance behind a load
	//   balancer serves the later calls (ADR-0018, Decision 9)
	// the dataset schema is required, because the JDBC driver classes a
	//   statement as an update without one [docs/e2e.md, The Flight SQL
	//   smoke steps]
	// a transaction id is ignored, because GetSqlInfo announces no
	//   transactions [sqlinfo.go:33-34]
	// IsUpdate stays nil, because the server is read-only and the driver
	//   reads the dataset schema
}
```

```go
// ClosePreparedStatement answers nothing, because the server stores
// nothing for a handle.
func (s *Server) ClosePreparedStatement(context.Context, flightsql.ActionClosePreparedStatementRequest) error
```

```go
// GetFlightInfoPreparedStatement answers what GetFlightInfoStatement
// answers for the handle's text: one endpoint whose ticket is a
// statement ticket of that text, and its described schema.
func (s *Server) GetFlightInfoPreparedStatement(ctx context.Context, q flightsql.PreparedStatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment + inline note at the ticket
	// because: one delegation, and one reason for the ticket's kind
	//
	// at the ticket: a statement ticket and not a prepared one, so the
	//   existing DoGet streams it and arrow-go's channel hook stays
	//   unused [statement.go:57-77; DoGet's rejected alternative]
	// rejected: a DoGet branch for CommandPreparedStatementQuery tickets,
	//   because it repeats the statement branch for the same text
}
```

```go
// GetSchemaPreparedStatement answers the described schema of the
// handle's text, without running it.
func (s *Server) GetSchemaPreparedStatement(ctx context.Context, q flightsql.PreparedStatementQuery, _ *flight.FlightDescriptor) (*flight.SchemaResult, error)
```

### `internal/qdbtest/flight/flight.go`

```go
// TicketOf: contract rewritten. GetFlightInfo describes the query, so a
// query that fails to describe fails here and t fails. The tests of a
// failing statement call Execute directly.

// Prepare prepares q, answers the prepare's dataset schema and the
// result read through the prepared statement joined into one batch the
// caller releases, and closes the statement.
func Prepare(t table.T, cl *flightsql.Client, ctx context.Context, q string) (*arrow.Schema, arrow.RecordBatch) {
	// style: doc comment + numbered overview + step comments
	// because: four calls in the order the JDBC driver makes them
	//
	// 1. Client.Prepare, whose result carries the dataset schema
	//    [client.go:504, 1365]
	// 2. PreparedStatement.Execute, one endpoint [client.go:1137]
	// 3. ReadAll and Concat, as Execute reads a statement
	// 4. Close, as the driver closes every statement [docs/e2e.md]
}
```

### `internal/flightsql/flightsql_test.go`

```go
// statementErr: runs q as a statement and answers the error of whichever
// call failed, Execute or DoGet, so a row passes whether the describe or
// the run refused the query.

// TestStatement gains step 6: each generated table read through Prepare
// on both engines. The rows equal the statement's, and the prepare's
// schema equals the stream's schema. On the native engine the stream's
// field metadata is stripped before the comparison.
//   - rejected: comparing with metadata on the native engine, because
//     validate has no data to compute max_width from (Rationale,
//     divergences) [Verified facts]
//   - the #sql comparison keeps metadata, because DuckDB adds none to the
//     types a generated table has [arrow_converter.cpp:47-61]
//   - GetExecuteSchema of the same query answers the same schema

// TestStatementRows step 1 gains rows, each INVALID_ARGUMENT with a
// message: a #sql text of two statements at Execute and at Prepare, a
// native and a #sql query with a ? marker at Prepare, and a native
// parse error at Prepare.
```

### `internal/olap/olap_test.go`

```go
// TestDescribe: on each cluster, the schema Describe answers equals the
// schema of the reader Query answers, for one query per row of the
// conversion table and for a generated table read through qdb.<name>,
// and two statements, a ? marker and a GEOMETRY column answer their
// errors.
func TestDescribe(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: the map is this repo's own copy of DuckDB's conversion, and
	//   the test is what keeps them equal
	//
	// 1. one instance per cluster, as TestQueryReadsGeneratedTable opens
	//    it [olap_test.go:84-100]
	// 2. one cast per row of the conversion table, TIMESTAMPTZ, an ENUM of
	//    3 values and one of 300, a LIST, an ARRAY, a STRUCT, a MAP and a
	//    UNION included, each compared with Schema.Equal, field metadata
	//    included
	// 3. a generated table through tbl.SelectSQL(), under rapid
	// 4. "SELECT 1; SELECT 2" and "" are ErrNotOneStatement, "SELECT ?" is
	//    ErrParameters, and a GEOMETRY column is ErrUndescribable
	//    (TYPE_GEOMETRY, type.go:48)
}
```

### `internal/flightsql/AGENTS.md`, Statement (bullets replaced and added)

```text
- GetFlightInfo for a statement describes the query and answers one
  endpoint whose ticket is the query text verbatim, with the described
  schema, no location and unknown totals; it runs nothing and keeps
  nothing (GetFlightInfoStatement; ADR-0018, Decision 9).
- A query describes through query.Describe. A DuckDB column type the
  describe does not map answers no schema in the info and
  INVALID_ARGUMENT from the calls whose answer is the schema (describe,
  schemaOf).
```

### `internal/flightsql/AGENTS.md`, Prepared (new section after Statement)

```text
- The handle of a prepared statement is the query text, so the server
  stores nothing and closing answers nothing (CreatePreparedStatement).
- GetFlightInfo of a prepared statement answers a statement ticket of
  the handle, and DoGet streams it as a statement
  (GetFlightInfoPreparedStatement).
- No parameters: a query with markers fails the describe, and the
  parameter upload is UNIMPLEMENTED (ADR-0018, Decision 12).
```

### `internal/flightsql/AGENTS.md`, Tests (sentences added)

```text
- TestStatement reads each table through a prepared statement too, the
  prepare's schema equal to the stream's, native metadata stripped.
- TestStatementRows covers the describe's refusals at Execute and at
  Prepare.
```

### `internal/olap/AGENTS.md` (bullet added)

```text
- A describe prepares one statement and maps its column types to the
  Arrow types DuckDB's conversion gives them (arrowTypeOf), because
  duckdb-go keeps the logical type it would need to ask DuckDB. A type
  outside the map is ErrUndescribable, and TestDescribe keeps the map
  equal to the conversion for every type it covers. A DuckDB bump
  re-runs that check before anything else.
```

### `internal/AGENTS.md`, Code (sentence added to the query.Run bullet)

```text
- query.Describe answers a query's schema without running it, through
  Cluster.Describe (qdb-api-go's ValidateArrow, under the read retry) or
  Cluster.DuckDescribe (Instance.Describe). The Flight server calls it.
```

### `tests/e2e/tools/jdbc/` (new Maven project)

```text
- pom.xml declares org.apache.arrow:flight-sql-jdbc-driver 19.0.0 and the
  shade plugin, producing one runnable jar; Java 21
  [docs/e2e.md, The Flight SQL smoke steps, decision log 2026-10-23]
- Main.java: takes --url, --query and optional --user and --password,
  runs the query through DriverManager, and prints two lines: the column
  names comma-separated from ResultSetMetaData, then the row count
  - it formats no cell, because cells are compared through the server's
    encoders and the ADBC step (owner, docs/e2e.md decision log
    2026-10-23)
  - the URL is jdbc:arrow-flight-sql://host:port with useEncryption=true
    and disableCertificateVerification=true, because the test servers
    run on the ephemeral certificate, as the ADBC step connects
```

### `tests/e2e/Makefile`

```text
- a jdbc target runs mvn -q package in tools/jdbc and logs the jar path;
  test-round-trip depends on it and passes --jdbc-jar
- a missing java or mvn fails the target, with no skip
  (owner; docs/e2e.md, The Flight SQL smoke steps)
```

### `tests/e2e/round-trip.sh`

```bash
# jdbc_back <out_dir> <flight_url> <login_body_file> <table> <rows>:
# SELECT * FROM <table> through the JDBC driver. The column names equal the
# header of the CSV the ADBC step decoded for the same query, and the row
# count equals the rows generated.
#   - the names come from the ADBC step's decoded CSV, because that result
#     is proved equal to the generated rows byte for byte
#   - java runs with --add-opens=java.base/java.nio=ALL-UNNAMED, which the
#     driver needs on JDK 17 and later [docs/e2e.md]
#   - the URL turns grpc+tls://host:port into jdbc:arrow-flight-sql://host:port

# jdbc_types <out_dir> <flight_url> <login_body_file>: TYPES_QUERY through
# JDBC, its names equal TYPES_CSV's header and its count one, so the DuckDB
# describe reaches a stock driver.
```

`run_cluster` calls both after `adbc_types`, and its OK line names JDBC.
`parse_args` takes `--jdbc-jar`.

### `docs/adr/0018-flight-sql.md` (edited in place)

```text
- Title: the minimal surface stays; the Context gains one paragraph:
  Arrow's JDBC driver runs every statement as a prepared statement
  (owner, 2026-10-24).
- Decision 9: the ticket is the query text and DoGet runs it. FlightInfo
  carries the schema the engine describes without running the query,
  through qdb-api-go's ValidateArrow on the native engine and DuckDB's
  prepare on #sql, and GetSchemaStatement answers it (owner,
  2026-10-24). The describe's known divergences from the stream, all
  qdbd's: no max_width metadata, a bare NULL literal as the null type,
  a non-select statement's empty column as its declared type (owner,
  2026-10-25).
- Decision 10: "no prepared statements" leaves the list, because no
  SqlInfo key carries it.
- Decision 12 becomes "Prepared statements without parameters": the
  handle is the query text, closing answers nothing, the prepared
  FlightInfo answers a statement ticket, a #sql text is one statement
  and a query with markers fails (owner, 2026-10-24 and 2026-10-25).
  Everything else is UNIMPLEMENTED, the parameter upload included.
- Decision 14: the JDBC step is in the round trip, and the sentence that
  defers it goes.
- Alternatives rejected gains: running the query at prepare (two runs
  per JDBC query), binding a native query through qdb-duck's qdb_query
  (DuckDB's types), a LIMIT 0 run for #sql (fails on non-subquery
  statements), casting the stream to the described schema (a second
  describe per fetch for a qdbd edge case), parameters now (no native
  binding).
```

### `docs/brief.md`, Arrow Flight SQL (edited in place)

```text
- The minimal scope lists prepared statements without parameters, and
  "Prepared statements, transactions and everything else return
  UNIMPLEMENTED" becomes "transactions and everything else".
- The rationale paragraph's "no prepared statements" claim about
  QuasarDB stays true of the native engine and gains that the server
  answers prepared statements statelessly, the query text as the handle.
- The stateless paragraph: FlightInfo carries the described schema, and
  "GetSchemaStatement is UNIMPLEMENTED rather than running every query
  twice" is replaced by the describe.
```

### `docs/e2e.md`

```text
- Step 7 of the round trip gains the JDBC reads, and "The JDBC read
  waits on prepared statements" goes.
- The Flight SQL smoke steps: the JDBC paragraph states the step as it
  is, the jar, the flag, what it compares, and that the FreeBSD agents
  are decided on the first build.
- The 2026-10-24 decision-log row stays as the record of why the step
  came with this unit.
```

### `tests/e2e/AGENTS.md`, `tests/e2e/README.md`

```text
- The ADBC bullet becomes "The ADBC and JDBC steps": the JDBC step runs
  the jar from tools/jdbc and compares names and a count, never a cell.
- The README names make jdbc and the java and mvn requirement.
```

## Rationale

| decision                                                                                                          | why                                                                                                                    | rejected, and why                                                                                                                                 | gained                                                    | given up                                                             | settled by                                        |
| ----------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------- |
| Support prepared statements, reversing ADR-0018 Decisions 10 and 12                                               | Arrow's JDBC driver runs every query as a prepared statement                                                           | keeping them UNIMPLEMENTED, because JDBC then fails on every query                                                                                | JDBC works                                                | a larger Flight surface                                              | owner, 2026-10-24                                 |
| The native describe is qdb-api-go's ValidateArrow over the C API's validate call                                  | validate answers the schema without running the query                                                                  | running the query at prepare, because every JDBC query would run twice; binding through qdb-duck's qdb_query, because the types would be DuckDB's | one validate round trip per describe                      | a dependency on PR #129                                              | owner, 2026-10-24                                 |
| Vendor qdb-api-go at 6afd5e8 on master                                                                            | the owner asked to vendor the PR head so the unit need not wait; the PR merged on 2026-10-25 and master is that commit | a PR-head commit then a master commit, because both now name the same tree                                                                        | one vendor commit                                         | none                                                                 | owner, 2026-10-25; proposal for the single commit |
| The DuckDB describe prepares and maps types in this repo                                                          | DuckDB binds without running, and duckdb-go hides the logical type                                                     | a LIMIT 0 run, because SHOW, DESCRIBE, PRAGMA and SET cannot be a subquery; an upstream duckdb-go change, because it waits on an outside project  | every one-statement query describes                       | a type map that must track DuckDB's conversion, kept by TestDescribe | owner, 2026-10-24                                 |
| The prepare uses driver.Conn's Prepare                                                                            | PrepareContext executes every statement before the last                                                                | PrepareContext, because a describe would execute statements                                                                                       | a describe executes nothing                               | none                                                                 | proposal                                          |
| A #sql text is one statement on the Flight port                                                                   | the owner expects one statement per invocation and could not say what a batch's response would be                      | answering no schema and running the batch, as today                                                                                               | one rule                                                  | a multi-statement #sql over Flight, which runs today                 | owner, 2026-10-25                                 |
| No parameters                                                                                                     | the JDBC step needs only plain statements, and the native engine has no binding                                        | #sql-only parameters, because the handle would carry values; both engines, because native substitution is textual                                 | a smaller unit                                            | `?` queries fail                                                     | owner, 2026-10-24                                 |
| Every schema call answers the describe                                                                            | the reason Decision 9 gave for UNIMPLEMENTED no longer holds                                                           | describing at prepare only, because the ADR would carry two rules for one question                                                                | ADBC and the Python client see the schema before the data | one describe per GetFlightInfo, so errors surface there              | owner, 2026-10-24                                 |
| Accept the describe's divergences from the stream                                                                 | all three are qdbd's validate disagreeing with its own execution, and no select over a table hits one                  | casting the stream to the described schema, because each prepared fetch would describe again for an edge case                                     | no second describe                                        | a prepared SELECT NULL or empty SHOW sees mismatched types           | owner, 2026-10-25                                 |
| A DuckDB type outside the map answers no schema in FlightInfo and INVALID_ARGUMENT where the schema is the answer | a plain statement over such a type runs over ADBC today                                                                | failing every call, because that stops a working query                                                                                            | nothing that works today breaks                           | two behaviours for one failure                                       | proposal; the plan's approval settles it          |
| The prepared FlightInfo answers a statement ticket                                                                | the existing DoGet already streams a statement ticket                                                                  | a DoGet branch for prepared tickets, which repeats it; arrow-go's channel hook, which DoGet's comment rejects                                     | no new DoGet code                                         | none                                                                 | proposal                                          |
| GetSqlInfo is unchanged                                                                                           | Flight SQL has no SqlInfo key for prepared statements                                                                  | a new value, because none exists                                                                                                                  | none                                                      | none                                                                 | tree                                              |
| The JDBC tool prints names and a count                                                                            | Java must not format a cell                                                                                            | printing CSV, a second formatter                                                                                                                  | no formatter to keep in step                              | JDBC cell values are not compared                                    | owner, docs/e2e.md decision log 2026-10-23        |

## Knowledge

| commit | lands                                                                                                                                                       | evidence                                           |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| 2      | none beyond `go.mod`                                                                                                                                        | PR #129                                            |
| 3      | `Session.describe`, `Cluster.Describe` comments; divergences in `Describe`'s doc                                                                            | Verified facts                                     |
| 4      | `withInstance` overview moves from `Duck`                                                                                                                   | `cluster.go:635-664`                               |
| 5      | `describe.go` comments; `internal/olap/AGENTS.md` bullet                                                                                                    | DuckDB conversion table, duckdb-go facts           |
| 6      | `TestDescribe` overview; `internal/AGENTS.md` Tests is unchanged, the test is in the existing `olap_test` package                                           | `olap_test.go`                                     |
| 7      | `DuckDescribe` doc                                                                                                                                          | `withInstance`                                     |
| 8      | `query.Describe` doc, package doc; `internal/AGENTS.md` Code sentence                                                                                       | `query.go`                                         |
| 9      | `describe`, `schemaOf`, `GetFlightInfoStatement`, `GetSchemaStatement` comments; flight fixture `TicketOf`; `statementErr`; flightsql `AGENTS.md` Statement | Rationale rows on schema calls and uncovered types |
| 10     | `prepared.go` comments; flightsql `AGENTS.md` Prepared                                                                                                      | Rationale rows on the handle and the ticket        |
| 11     | flight fixture `Prepare`; `TestStatement` step 6, `TestStatementRows` rows; flightsql `AGENTS.md` Tests                                                     | Rationale row on divergences                       |
| 12     | ADR-0018                                                                                                                                                    | every owner row                                    |
| 13     | brief                                                                                                                                                       | ADR-0018                                           |
| 14     | `tests/e2e/tools/jdbc` comments                                                                                                                             | docs/e2e.md                                        |
| 15     | `round-trip.sh`, `Makefile` comments                                                                                                                        | docs/e2e.md                                        |
| 16     | `docs/e2e.md`, `tests/e2e/AGENTS.md`, `README.md`                                                                                                           | steps 14-15                                        |
| 18     | `docs/log.md` Current state and entry                                                                                                                       | this plan                                          |

The qdbd story for the divergences has no home in this repo until it is
filed. Its text is the three bullets of Verified facts on validate and
the fetch, and the log's Handoff carries it until the owner files it.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message.

Specific to this unit:

- Before commit 5, the executor reads DuckDB 1.5.4's
  `SetArrowStructFormat`, `EnumTypeInfo` physical type and the union
  child naming in `~/git/qdb-duck/duckdb`, and confirms how arrow-go's
  C data import names and flags the map's key and value, the array's
  child and an enum's dictionary. A difference from the table changes
  `arrowTypeOf`, and `TestDescribe` must pass before the commit.
- Before commit 5, the executor confirms that a prepared `SELECT NULL`
  answers INTEGER as the result does. If it answers SQLNULL, the null
  case maps to int32 only if the reader does too, and `TestDescribe`
  decides.
- Commit 9 changes when a failing statement fails, from DoGet to
  GetFlightInfo. The ADBC step of the round trip reads only good queries,
  so it is unaffected. If a Go test outside `internal/flightsql` calls
  `TicketOf` on a failing query, it moves to `statementErr` in the same
  commit.
- If the JDBC driver refuses a stream whose schema carries `max_width`
  metadata the prepare did not announce, the round trip fails at commit 15. The executor then stops and asks the owner, because the answer is
  the cast this plan rejected.

## Commits

1. `docs(plan): prepared-statements-plan.md, stateless prepared statements on both engines and the JDBC smoke step`
2. `build(vendor): qdb-api-go at master, the squash commit of PR #129, Query.ValidateArrow answers a query's schema without running it`
3. `feat(qdb): Cluster.Describe answers a native query's schema through ValidateArrow under the read retry`
4. `refactor(qdb): Cluster.Duck counts its call around the instance through withInstance`
5. `feat(olap): Instance.Describe prepares one statement and maps its columns to the Arrow types DuckDB's conversion gives them`
6. `test(olap): TestDescribe equals the reader's schema for every mapped type and a generated table, and the refusals answer their errors`
7. `feat(qdb): Cluster.DuckDescribe describes on the user's instance`
8. `feat(query): Describe answers the schema of q on the engine its prefix names`
9. `feat(flightsql): the statement's FlightInfo carries the described schema and GetSchemaStatement answers it`
10. `feat(flightsql): prepared statements, the query text as the handle, a statement ticket for the fetch and nothing stored`
11. `test(flightsql): each table reads back through a prepared statement on both engines, and the describe's refusals answer INVALID_ARGUMENT`
12. `docs(adr): ADR-0018 carries the describe, prepared statements without parameters and the accepted divergences`
13. `docs(brief): Arrow Flight SQL lists prepared statements and the described schema`
14. `test(e2e): tools/jdbc, a Maven project whose jar runs a query through Arrow's JDBC driver and prints its column names and row count`
15. `test(e2e): the round trip reads each table and the types query through JDBC on both clusters`
16. `docs(e2e): e2e.md, tests/e2e/AGENTS.md and the README carry the JDBC step`
17. `/doc-discipline all check` over `internal/olap internal/qdb internal/query internal/flightsql internal/qdbtest/flight tests/e2e docs` against this plan; each finding is fixed in a further `docs(<scope>)` commit.
18. `docs(log): prepared-statements-plan.md deleted, facts moved to ADR-0018, the brief, docs/e2e.md, the AGENTS.md files and the comments of internal/olap, internal/qdb, internal/query and internal/flightsql`
19. Verify: push `sc-19567/rr-prepared-statements`, build its head in
    Buildkite, wait for the result. Green: report the build number. Red:
    fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. A DuckDB column type outside the map. Recommendation: no schema in
   FlightInfo, INVALID_ARGUMENT naming the type from the prepare and both
   schema calls, so a plain ADBC statement keeps working.
2. The qdbd story for validate's disagreement with execution: the null
   literal and the non-select empty column. Recommendation: the owner
   files one R&D story with the three Verified facts bullets, and the
   log's Handoff carries it until then.
3. A multi-statement `#sql` text still runs on `POST /api/v2/query`.
   Recommendation: refuse it there too in a later unit, so both planes
   share the one-statement rule.
4. Java and Maven on the Buildkite agents. Recommendation: let the first
   build show it, as `docs/e2e.md` prescribes, and provision outside this
   repo where they are missing.
