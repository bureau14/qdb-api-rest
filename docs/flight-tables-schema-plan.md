# Plan: GetTables with include_schema from the native metadata through one leased session

Status: approved

## Outcome

When the unit lands, a Flight SQL `GetTables` request with
`include_schema` answers the five-column reference shape, the fifth
column holding each table's schema as the IPC bytes arrow-go's
`SerializeSchema` writes. The schema is `$timestamp`, non-nullable,
then the table's columns in order, nullable, in the binding's Arrow
types. A symbol column's field carries `qdb:symtable` with its
symtable name. The schema carries `qdb:shard_size` and `qdb:ttl`, each
an integer count of milliseconds, a TTL of `0` meaning disabled. The
schema comes from the C API's table metadata call and not from a
query, through a new binding method that answers the columns, the
shard size and the TTL in one call. `internal/qdb` gains
`Cluster.TableSchemas`, which leases one session, lists the names on
it and runs one metadata call per name on the same session, and fails
the whole listing when any call fails. `GetFlightInfoTables` and
`DoGetTables` lose their `UNIMPLEMENTED` refusal. The binding is
re-vendored twice: at the head of the sibling pull request, and at
master once it merges.

Left for later units: the Flight format-equivalence property and the
ADBC and JDBC smoke tests (`docs/log.md`, Next); `GET
/api/v2/tables/{name}` in M7, which wraps `TableSchemas` or a
one-table sibling of it; any field metadata beyond the symtable.

## Verified facts

- `table_schema` is "bytes not null (schema of the table as described in
  Schema.fbs::Schema, it is serialized as an IPC message)", and fields
  inside it may carry the `ARROW:FLIGHT:SQL:*` keys, which this unit
  does not use
  (`vendor/github.com/apache/arrow-go/v18/arrow/flight/gen/flight/FlightSql.pb.go:2680-2697`).
- arrow-go writes a schema as a full IPC stream with no record batch,
  because every implementation does, and reads it back the same way
  (`vendor/.../arrow/flight/record_batch_writer.go:102-112`,
  `record_batch_reader.go:177-189`).
- The five-column reference schema is `schema_ref.TablesWithIncludedSchema`,
  `table_schema` a non-nullable Binary
  (`vendor/.../flight/flightsql/schema_ref/reference_schemas.go:36-42`).
  The router's `GetSchema` already answers it when the flag is set
  (`vendor/.../flight/flightsql/server.go:850-854`).
- arrow-go's client hands the fifth column through undecoded; a reader
  deserializes each Binary cell itself
  (`vendor/.../flight/flightsql/client.go:378-385`).
- `Schema.Equal` ignores schema metadata; `Field.Equal` compares name,
  nullability, type and field metadata
  (`vendor/.../arrow/schema.go:243-245`,
  `vendor/.../arrow/datatype_nested.go:965-975`). A test that checks the
  schema keys reads them with `Metadata.GetValue`
  (`vendor/.../arrow/schema.go:103`).
- `qdb_ts_get_metadata` answers columns, shard size in ms, TTL in ms and
  an aggregated-table pointer in one struct
  (`qdb/include/qdb/ts.h:806-842`). `qdb_ttl_disabled` is `0`
  (`ts.h:318`). The binding's `ColumnsInfo` makes that call and keeps the
  columns only; nothing in the binding exposes the TTL
  (`vendor/github.com/bureau14/qdb-api-go/v3/entry_timeseries_common.go:466-486`).
- The C API has no table enumeration; `qdb_prefix_get` lists every
  entry type under a limit (`qdb/include/qdb/prefix.h:70-74`). The name
  listing stays the `show tables` query (`internal/qdb/table.go:92-131`).
- `Cluster.Query` hands the session back before the sink runs
  (`internal/qdb/cluster.go:431-438`), so the names and the lookups can
  share one lease only through `Session.fetch` on the held session
  (`cluster.go:287-297`) inside one `Cluster.Call` (`cluster.go:361-386`).
- `Call` retries once under `WithReadRetry` and re-runs the whole
  closure, on a fresh session after a bad one (`cluster.go:356-385`).
- `Session` is the only door to the binding's session and every method
  runs one C operation (`cluster.go:257-267`); `Session.schemaOf` already
  runs `ColumnsInfo` on a held session for the ingest
  (`internal/qdb/ingest.go:86-97`).
- `specialFields["$timestamp"]` is non-nullable in the binding's
  timestamp type, and `dataFields` makes every column nullable in
  `TsColumnType.ArrowType` (`internal/qdb/read.go:46-65`). The fixture's
  `Schema` builds the same fields, `$timestamp` then the columns
  (`internal/qdbtest/table/table.go:148-158`), creates every table with
  a one-day shard and no TTL (`table.go:495-497`), and draws symbol
  columns whose symtable is `<table>_<column>` (`table.go:185-191`).
- `$qdb.firehose` is a `timeseries` entry on the insecure cluster, so
  the metadata call applies to it as to any table (qdbsh `get_metadata`,
  2026-10-23). `show tables` lists it (`table.go:99-103`).
- The refusals are `errIncludeSchema` at `internal/flightsql/catalog.go:138-140`,
  `GetFlightInfoTables:145-153` and `DoGetTables:174-177`;
  `TestCatalog` step 7 pins both (`internal/flightsql/flightsql_test.go:700-716`);
  `TestStatementRows` step 3 drives `GetTables` without the flag on the
  unreachable cluster (`flightsql_test.go:505-515`) and is unaffected.
- The test helper `column` is string-only (`flightsql_test.go:542-554`).
- The binding is pinned at `v3.9.1-0.20261014065943-5b88cb766ef3`
  (`go.mod:9`); the last two bumps were one commit at a PR head and one
  at master after the squash (`ef988ff`, `4d156e0`).
- The binding method is qdb-api-go PR #128, branch
  `sc-20034/unified-table-metadata-retrieval`, head
  `5b6b71bee0516de288d19b198dc5373d4332fbef`, open and not merged
  (sibling session, 2026-10-23): `TimeseriesEntry.Metadata()
(TableMetadata, error)` makes one `qdb_ts_get_metadata` call;
  `TableMetadata{Name string, Columns []TsColumnInfo, ShardSize
time.Duration, TTL time.Duration}` with TTL `0` meaning disabled, and
  `Column(name) (TsColumnInfo, bool)`; an unknown table satisfies
  `errors.Is(err, ErrAliasNotFound)`; `ColumnsInfo()` and `Columns()`
  keep their signatures as views over `Metadata()`; `CachedMetadata()`,
  `SetTTL` and `HandleOptions.WithTableCacheTTL` exist and this unit
  calls none of them. `Columns` does not include the implicit
  `$timestamp`, as `ColumnsInfo` does not, so the builder prepends it.

## Design

### `internal/qdb/table.go`

```go
// The metadata keys of the schema TableSchemas answers, this server's own
// (Flight SQL reserves ARROW:FLIGHT:SQL:*) [FlightSql.pb.go:2684-2695].
// MetaSymtable is a field key on a symbol column, its value the symtable
// name; MetaShardSize and MetaTTL are schema keys, each an integer count
// of milliseconds, and a TTL of 0 means disabled [Rationale: keys].
const (
	MetaSymtable  = "qdb:symtable"
	MetaShardSize = "qdb:shard_size"
	MetaTTL       = "qdb:ttl"
)
```

```go
// TableSchema is one table of a listing: its name and the schema
// TableSchemas builds for it.
type TableSchema struct {
	Name   string
	Schema *arrow.Schema
}
```

```go
// tables lists the names of the cluster's tables on the held session, in
// the order the cluster reports them, qdbd's own $qdb. tables included
// [table.go:92-103].
func (s *Session) tables() ([]string, error) {
	// style: doc comment + numbered overview + step comments
	// because: the listing is a query whose one-column answer is checked
	// and copied out, and the two reasons it has today move here with it
	//
	// The list is the native query `show tables`, because the C API has
	// no enumeration call and that query is the server's own notion of a
	// table, answered in one round trip [prefix.h:70-74, table.go:93-100]
	//   - rejected: a prefix scan over the empty prefix with a metadata
	//     call per alias, N+1 round trips over every entry type
	//     [Rationale: listing]
	// The $qdb. tables are listed as the server lists them, because
	// hiding them needs a prefix rule this project would own
	//   [table.go:100-103]
	//
	// 1. fetch the result on the held session [cluster.go:287]
	//    rec, err := s.fetch("show tables")
	// 2. a nil batch (no result set) is no table [cluster.go:445-446]
	// 3. the result is one string column named $table (qdbd 3.15.0.dev0),
	//    read by position; another shape is an error naming it, because a
	//    server change would otherwise read as an empty cluster
	//    [table.go:109-117]
	// 4. each value is cloned, because a Go string of an Arrow array
	//    aliases the batch's buffer and the batch is released here
	//    [table.go:117-123]
}
```

```go
// Tables lists the names of the cluster's tables as user u, in the order
// the cluster reports them, qdbd's own $qdb. tables included. The
// listing is retried once on a retryable failure, as a read is
// [table.go:89-91].
func (c *Cluster) Tables(ctx context.Context, u User) ([]string, error) {
	// style: doc comment alone
	// because: one Call around Session.tables, as CreateTable wraps its
	// session method [table.go:74-80]
	//
	// at WithReadRetry: a listing is a read [table.go:105, cluster.go:324-329]
}
```

```go
// schemaOfMetadata is the schema TableSchemas answers for a table whose
// metadata is md: $timestamp, non-nullable, then the columns in order,
// nullable, in the binding's Arrow types, a symbol column's field
// carrying MetaSymtable, and the schema carrying MetaShardSize and
// MetaTTL in milliseconds.
func schemaOfMetadata(md qdbapi.TableMetadata) *arrow.Schema {
	// style: doc comment + numbered overview + step comments
	// because: the shape is a domain rule the owner fixed, and each part
	// of it has its own reason
	//
	// The shape is the table as it was created and not the reader's or a
	// query's result set, because the metadata call is the canonical
	// description of a table and a result set has a schema of its own
	// [Rationale: shape]
	//   - rejected: the reader's shape with $table in front, because
	//     $table is a result-set artefact no table has [Rationale: shape]
	//   - rejected: the columns alone, because $timestamp is a column of
	//     every table that every query returns [Rationale: shape]
	//
	// 1. $timestamp first, the reader's special field, non-nullable
	//    [read.go:46-50]
	// 2. each column nullable in TsColumnType.ArrowType, as dataFields
	//    builds it, because the server declares no type map of its own
	//    [read.go:55-65, internal/AGENTS.md, Code]
	// 3. a symbol column's field carries MetaSymtable, because the
	//    symtable is the one column attribute the Arrow type loses
	//    (a symbol reads as a string) [Rationale: keys, read.go:62]
	// 4. the schema carries MetaShardSize and MetaTTL as integer
	//    milliseconds, the C API's unit and the unit the create body
	//    takes, with 0 meaning disabled as qdb_ttl_disabled does
	//    [docs/brief.md:597-599, ts.h:318]
	//   - rejected: Go duration strings, a second spelling of a duration
	//     next to the create body's milliseconds [Rationale: keys]
	//   - rejected: ARROW:FLIGHT:SQL:TYPE_NAME and the other reserved
	//     keys, because nothing asks for them yet and they can be added
	//     later [Rationale: keys]
}
```

```go
// tableSchema answers the schema of the table called name through one
// metadata call on the held session. The binding's error for an unknown
// table passes through unchanged, so IsTableNotFound classifies it.
func (s *Session) tableSchema(name string) (*arrow.Schema, error) {
	// style: doc comment alone
	// because: one binding call and the pure builder, as schemaOf is
	// [ingest.go:86-97]
	//
	// at Metadata: the fetching call and not the cached one, because a
	// listing describes the cluster as it is at the call [ts.h:845-863,
	// Rationale: binding]
}
```

```go
// TableSchemas lists the cluster's tables as user u with the schema of
// each, in the order the cluster reports them, qdbd's own $qdb. tables
// included, through one leased session. Any failed lookup fails the
// listing. The listing is retried once on a retryable failure, as a read
// is.
func (c *Cluster) TableSchemas(ctx context.Context, u User) ([]TableSchema, error) {
	// style: doc comment + numbered overview + step comments
	// because: one lease spans a query and N metadata calls, and the
	// failure policy is the owner's
	//
	// One Call holds the session across the listing and every lookup,
	// because one checkout per request is what the pool and the budget
	// are built around, and Query would hand the session back before the
	// names are read [Rationale: lease, cluster.go:431-438]
	//   - rejected: Tables, then one Call per name: N+1 checkouts per
	//     request, each counted by the budget [Rationale: lease]
	//
	// 1. the names on the held session [Session.tables]
	// 2. one metadata call per name on the same session, in the names'
	//    order [Session.tableSchema]
	// 3. any failure, a not-found included, fails the listing, because a
	//    table that vanishes between the two calls makes the request
	//    inconsistent and the case is extreme [Rationale: vanished]
	//   - rejected: skipping the vanished row, an errors.Is per lookup for
	//     a case that does not happen in practice [Rationale: vanished]
	// at WithReadRetry: a listing is a read, and a retry re-runs the
	//    whole closure on a fresh session after a bad one
	//    [cluster.go:356-360]
}
```

### `internal/qdb/cluster_test.go`

```go
// TestTableSchemas: a table created as the user with one column of each
// type appears in TableSchemas on both clusters with $timestamp then its
// columns in the binding's Arrow types, the symtable on its symbol
// column, and the shard size and TTL it was created with, and every
// listed schema starts with $timestamp.
func TestTableSchemas(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: a handler-like sequence of create, list, find and compare
	//
	// 1. one table with every type word, a symtable on the symbol column,
	//    a one-hour shard, removed on cleanup, named qdbtest_ for the
	//    sweep [cluster_test.go:395-405]
	// 2. TableSchemas, then the entry with the table's name
	// 3. the fields equal the expected schema, built in the test from the
	//    columns, through Schema.Equal, which compares field metadata and
	//    so the symtable key [datatype_nested.go:965-975]
	// 4. MetaShardSize is the hour in milliseconds and MetaTTL is 0, read
	//    with GetValue because Schema.Equal ignores schema metadata
	//    [schema.go:243-245]
	// 5. every entry's first field is $timestamp, so the $qdb. tables are
	//    described too [Verified facts: $qdb.firehose]
}
```

### `internal/flightsql/catalog.go`

`errIncludeSchema` is removed.

```go
// GetFlightInfoTables answers the info of the table listing, four columns,
// or five with each table's schema when cmd asks for it
// [schema_ref/reference_schemas.go:30-42].
func (s *Server) GetFlightInfoTables(_ context.Context, cmd flightsql.GetTables, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment alone
	// because: one conditional over infoFor, as the other catalog infos
	// are [catalog.go:112-115]
	//
	// at the schema choice: the shape the router's GetSchema promises for
	// the flag [server.go:850-854]
}
```

```go
// DoGetTables answers one row per table name the cluster reports as the
// caller's user, under catalog qdb, schema main and type TABLE, filtered
// by cmd's table name pattern, and empty when cmd's catalog, schema
// pattern or table types exclude the cluster. With include_schema each
// row carries the table's schema as IPC bytes in table_schema.
func (s *Server) DoGetTables(ctx context.Context, cmd flightsql.GetTables) (*arrow.Schema, <-chan flight.StreamChunk, error) {
	// style: doc comment + numbered overview + step comments
	// because: the existing narrative gains one branch and one column
	//
	// The listing is the cluster's, filtered here, because `show tables`
	// has no filtered form [catalog.go:170-172]
	//
	// 1. the empty result when the request excludes the cluster, before
	//    the cluster is called [catalog.go:181-189]
	// 2. the user from the claims [catalog.go:191-193]
	// 3. without the flag Cluster.Tables, with it Cluster.TableSchemas,
	//    because the schemas cost one metadata call per table and a plain
	//    listing should not pay it [Rationale: two paths]
	// 4. a failure is INTERNAL through statusOf, because nothing in the
	//    request reaches the engine as text [catalog.go:194-198]
	// 5. one row per name the pattern admits, the schema serialized with
	//    flight.SerializeSchema into table_schema when asked, because that
	//    is the IPC form every Flight implementation reads
	//    [record_batch_writer.go:104-112]
	//   - rejected: ignoring the flag and answering four columns, because
	//     the driver would read a result that differs from the schema
	//     GetSchema promised [catalog.go:149-151]
}
```

### `internal/flightsql/flightsql_test.go`

```go
// binaryColumn answers the binary column named name of rec as a slice,
// the sibling of column for the table_schema column.
func binaryColumn(t *testing.T, rec arrow.RecordBatch, name string) [][]byte {
	// style: doc comment alone
	// because: one accessor like column [flightsql_test.go:542-554]
}
```

`TestCatalog`'s doc comment and overview drop "include_schema is
UNIMPLEMENTED on GetFlightInfo and on DoGet" and gain the read-back.
Step 7 becomes:

```go
	// 7. include_schema: GetTables with the flag over a generated table
	//    answers five columns, and the created table's table_schema
	//    deserializes to the fixture's schema with its keys
	//   - rapid draws a table (GenerateSchema, CreateAs), so symbol
	//     columns and their symtables are covered [table.go:185-205]
	//   - the ticket is read through catalogRows, the row found by
	//     table_name, the bytes deserialized with flight.DeserializeSchema
	//     [record_batch_reader.go:177-189]
	//   - the fields equal table.SchemaWithMeta(tbl), Schema.Equal
	//     comparing the symtable key; qdb:shard_size is one day in
	//     milliseconds and qdb:ttl is 0, read with GetValue
	//     [table.go:495-497, schema.go:243-245]
	//   - the hand-built ticket with the flag (the old UNIMPLEMENTED case)
	//     now answers the same five columns, because a ticket is bytes a
	//     client can mint without GetFlightInfo [catalog.go:174-175]
```

### `internal/qdbtest/table/table.go`

```go
// SchemaWithMeta is the schema the Flight catalog answers for tbl under
// include_schema: Schema's fields, a symbol column's field carrying
// qdb.MetaSymtable, and the shard size and TTL as qdb.MetaShardSize and
// qdb.MetaTTL in milliseconds [Rationale: shape, keys].
func SchemaWithMeta(tbl Table, shard, ttl time.Duration) *arrow.Schema {
	// style: doc comment alone
	// because: Schema's loop with two metadata lines; the fixture states
	// the expected shape by construction, as internal/AGENTS.md, Tests,
	// asks of every comparison
}
```

### Documents

`internal/flightsql/AGENTS.md`, Catalog, the GetTables bullet:

```
- GetTables answers Cluster.Tables under the request's table name
  pattern, applied in Go because the server has no filtered form.
  qdbd's own $qdb. tables are listed (owner, 2026-10-22). With
  include_schema it answers Cluster.TableSchemas, each table's schema
  serialized into table_schema as arrow-go's SerializeSchema writes it;
  `DoGetTables` says why the plain listing does not pay for the
  schemas (owner, 2026-10-23).
```

`internal/AGENTS.md`, Code, the table list bullet:

```
- The table list is Cluster.Tables, the native `show tables` query on a
  held session under the read retry, answering names in the cluster's
  order with qdbd's own $qdb. tables included. The C API has no
  enumeration call, and Session.tables says why the query is the core
  and what was rejected. Cluster.TableSchemas is the list with each
  table's schema from the native metadata call, $timestamp then the
  columns, the symtable on a symbol field and the shard size and TTL on
  the schema (the Meta* keys of table.go), through one leased session,
  any failed lookup failing the listing; TableSchemas says why. The
  Flight catalog reads both, and GET /api/v2/tables and GET
  /api/v2/tables/{name} wrap them (ADR-0018, Decision 11).
```

`docs/adr/0018-flight-sql.md`, Decision 11, edited in place (a decision
within the ADR's subject, `docs/AGENTS.md`, ADRs):

```
11. **Catalog commands are in this milestone.** `GetTables` answers the
    cluster's table list, and with `include_schema` each table's schema
    from the C API's table metadata call: `$timestamp` then the
    columns in the binding's Arrow types, `qdb:symtable` on a symbol
    column's field, and `qdb:shard_size` and `qdb:ttl` on the schema
    as integer milliseconds, `0` meaning no TTL (owner, 2026-10-23).
    `GetCatalogs`, `GetDbSchemas` and `GetTableTypes` answer the fixed
    shape of one cluster. The table list core in `internal/qdb` is
    written here and the exploration milestone reuses it.
```

`docs/log.md`, Current state: the include_schema item leaves Next; the
`$qdb.` sentence under In flight gains "with each table's schema"; the
M6/M7 handoff bullet on `Cluster.Tables` becomes:

```
- `Cluster.Tables` and `Cluster.TableSchemas` (`internal/AGENTS.md`,
  Code) are the cores `GET /api/v2/tables` and
  `GET /api/v2/tables/{name}` wrap.
```

## Rationale

| decision                                                                                                             | why                                                                                                                                                        | rejected, and why                                                                                                             | gained                                                  | given up                                                                      | settled by                                               |
| -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ----------------------------------------------------------------------------- | -------------------------------------------------------- |
| shape: `$timestamp` then the columns, from the metadata call                                                         | the metadata call is the canonical way to describe a table; a query answers a result set, which has a schema of its own                                    | the reader's shape with `$table`, a result-set artefact no table has; the columns alone, hiding the index every query returns | one schema per table that matches what `CREATE` took    | the Flight schema differs from a native result stream's                       | owner, 2026-10-23                                        |
| listing: `show tables` stays the name source                                                                         | the C API has no table enumeration; the query answers names in one round trip                                                                              | prefix scan plus metadata per entry: every entry type under a limit, N+1 calls                                                | no change to the listing that landed 2026-10-22         | the names come from a query while the schemas come from the metadata call     | owner, 2026-10-23                                        |
| lease: one `Call` for the names and every lookup                                                                     | one checkout per request is what the pool and the budget are built around                                                                                  | `Tables` then one `Call` per name: N+1 checkouts per request, each counted by the budget                                      | one session per listing                                 | the listing moves onto `Session.tables`, a small refactor of `Cluster.Tables` | owner, 2026-10-23                                        |
| vanished: any failed lookup fails the listing as INTERNAL                                                            | the request was inconsistent, and the case is extreme                                                                                                      | skipping the row: an `errors.Is` per lookup for a case that does not happen in practice                                       | one error path                                          | a drop racing a listing fails that listing once                               | owner, 2026-10-23                                        |
| keys: `qdb:symtable` on symbol fields, `qdb:shard_size` and `qdb:ttl` on the schema, integer milliseconds, `0` = off | the symtable is the one attribute the Arrow type loses; shard size and TTL are table metadata Arrow can carry; milliseconds are the create body's unit     | the reserved `ARROW:FLIGHT:SQL:*` keys, nothing asks for them yet; Go duration strings, a second duration spelling            | a driver sees the symtable and the table's retention    | three keys this server owns and must keep                                     | owner, 2026-10-23                                        |
| binding: upstream `Metadata()` first, re-vendored at the PR head then at master                                      | the binding is vendored and changed upstream first; the schema bytes are a wire shape and change once                                                      | columns and symtable now, shard and TTL later: the bytes would change when the two keys arrive                                | all three keys from the first commit                    | the first build commit waits on the sibling PR; two vendor commits            | owner, 2026-10-23                                        |
| binding shape: `TimeseriesEntry.Metadata() (TableMetadata, error)`, fetching call, `ColumnsInfo` unchanged           | the consumer needs the three fields from one call; the ingest and the reader keep calling `ColumnsInfo`                                                    | a cached variant for the listing, because a listing describes the cluster as it is                                            | no change in `ingest.go` or `read.go`                   | the exact name follows the sibling repository's decision                      | PR #128 as landed at its head, 2026-10-23; merge pending |
| two paths: `Tables` without the flag, `TableSchemas` with it                                                         | the schemas cost one metadata call per table, and a plain listing should not pay it                                                                        | one path always calling `TableSchemas`: N metadata calls for a request that asked for names                                   | the plain listing stays one round trip                  | one branch in `DoGetTables`                                                   | proposal                                                 |
| the builder and the keys live in `internal/qdb`; `flightsql` only serializes                                         | the shape is the table's and M7's HTTP endpoint answers the same table; the Flight layer owns the bytes, not the shape                                     | a builder in `flightsql`: the HTTP endpoint would copy it                                                                     | one shape for both planes                               | `internal/qdb` exports three key constants                                    | proposal                                                 |
| tests: a fixed table in `package qdb`, a generated one over Flight, expected schema by construction in the fixture   | `package qdb` cannot import the fixture; the Flight test draws symbol columns and their symtables; the fixture states the expected shape, as its rules ask | a unit test of `schemaOfMetadata` alone: a low-value test of a pure builder the two live tests cover                          | the keys are checked on a live cluster on both clusters | one more fixture function, `SchemaWithMeta`                                   | proposal                                                 |

## Knowledge

- Commit 2 (vendor at PR head): no prose; the commit subject names the
  PR.
- Commit 3 (`Session.tables`, `Cluster.Tables`): the listing's two
  reasons (no enumeration call, the `$qdb.` tables) move from
  `Cluster.Tables`'s overview into `Session.tables`'s; `Tables` keeps
  the doc comment and the retry note. `internal/AGENTS.md`, Code, the
  table list bullet names `Session.tables`.
- Commit 4 (`Meta*`, `TableSchema`, `schemaOfMetadata`,
  `Session.tableSchema`, `Cluster.TableSchemas`): Rationale rows shape,
  keys, lease, vanished and binding land in the comments the Design
  sketches; the key constants' comment carries the reserved-prefix fact;
  `internal/AGENTS.md`, Code, the table list bullet gains the
  `TableSchemas` sentences; ADR-0018 Decision 11 gains the include_schema
  sentence.
- Commit 5 (`TestTableSchemas`): the test's overview carries why the
  fields and the keys are compared separately (`Schema.Equal` ignores
  schema metadata) and why the `$timestamp` check runs over every entry
  (the `$qdb.` tables).
- Commit 6 (`GetFlightInfoTables`): the inline note on the schema choice.
- Commit 7 (`DoGetTables`): the two-paths row lands in step 3; the
  rejected "ignore the flag" stays at step 5, moved from the removed
  refusal comment. `internal/flightsql/AGENTS.md`, Catalog, the GetTables
  bullet.
- Commit 8 (`SchemaWithMeta`, `TestCatalog`, `binaryColumn`): the
  fixture's doc comment names the shape; the test step carries the
  deserialization fact and the hand-built ticket's reason.
- Commit 9 (documents): `internal/flightsql/AGENTS.md` Tests bullet
  (`TestCatalog` reads include_schema back over a generated table),
  `internal/AGENTS.md` Tests (the fixture's `SchemaWithMeta` beside
  `Schema`), `docs/log.md` Current state.
- Commit 10 (vendor at master): no prose.
- The plan's deletion commit appends the log entry and moves nothing.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit:

- Commit 2 waits on the sibling pull request. The executor pins the PR
  head's pseudo-version with `go get github.com/bureau14/qdb-api-go/v3@<sha>`,
  `go mod tidy`, `go mod vendor`, and reads the method as it landed. If
  the head moved and its name or fields differ from the Verified facts,
  the Design's `Session.tableSchema` follows the landed shape and the
  Rationale's binding-shape row is updated in the same commit, with no
  question needed; if the method is missing a field (the TTL, the shard
  size), the executor stops and asks the owner.
- Commit 5 is the first run of the metadata call over the `$qdb.` tables.
  If it fails on one of them, the vanished row's rule makes every
  include_schema listing fail; the executor stops and asks the owner
  whether the `$qdb.` tables leave the schema listing, rather than
  choosing.
- Commit 10 waits on the PR's merge; the executor pins master's head the
  same way. The Buildkite build runs on commit 10's head.

## Commits

1. `docs(plan): flight-tables-schema-plan.md, GetTables include_schema from the native metadata through one leased session`
2. `build(vendor): qdb-api-go at the head of PR #128, TimeseriesEntry.Metadata answers columns, shard size and ttl`
3. `refactor(qdb): Session.tables lists the names on the held session, and Cluster.Tables runs it under the read retry`
4. `feat(qdb): Cluster.TableSchemas answers every table's schema through one leased session, $timestamp then the columns with the symtable, shard size and ttl keys`
5. `test(qdb): a created table answers its schema and keys in TableSchemas on both clusters`
6. `feat(flightsql): GetFlightInfoTables answers the five-column info under include_schema`
7. `feat(flightsql): DoGetTables fills table_schema with the serialized schema through Cluster.TableSchemas`
8. `test(flightsql): TestCatalog reads include_schema back, a generated table's schema deserializing to the fixture's with its keys`
9. `docs(flightsql): the Catalog and Tests bullets, the table list line and the log carry the schema listing`
10. `/doc-discipline all internal/qdb internal/flightsql internal/qdbtest/table docs/flight-tables-schema-plan.md`, its fixes as further small commits
11. `build(vendor): qdb-api-go at master, the squash commit of PR #128`
12. `/doc-discipline check docs/flight-tables-schema-plan.md`, its findings fixed before the next commit
13. `docs(log): flight-tables-schema-plan.md deleted, facts moved to ADR-0018, internal/AGENTS.md, internal/flightsql/AGENTS.md and the comments of internal/qdb and internal/flightsql`
14. Verify: push `sc-19567/rr-flight-tables-schema`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. The `$qdb.` tables under the metadata call: `$qdb.firehose` is a
   timeseries entry, so the call should answer. Recommendation: keep the
   rule that any failed lookup fails the listing, and if commit 5 shows
   a `$qdb.` table refusing the call, ask before excluding them.
2. PR #128 is open and not merged; its head may move before commit 2
   pins it. Recommendation: commit 2 pins the head at the time of the
   build and commit 11 pins master's squash commit, as the How the
   knowledge lands section says.
