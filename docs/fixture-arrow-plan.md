# The table fixture holds one record batch -- Plan

Status: draft. Scaffolding for one unit of work: the generated table
fixture (`internal/qdbtest/table`) stores its rows as one Arrow record
batch in the reader's types, pushes it through the Go API's Arrow
writer, and is compared with what a read answers by array equality;
the sentinel row writer leaves the repository, and the decode round
trip draws its tables through the fixture. Deleted when the unit
lands; the rules move to `internal/AGENTS.md` and
`internal/encoding/AGENTS.md` first (`docs/AGENTS.md`, Plans). The
owner set the direction on 2026-10-05 and 2026-10-06 (the seed
report, its answers and the Go API session's answers, folded into the
Rationale below); this plan turns it into code.

## Outcome

When the unit lands:

- `table.Table` is a name, a schema (`Column`: name, QuasarDB column
  type, symtable) and `Batch`, one `arrow.RecordBatch` of `$timestamp`
  and the data columns in the types `TsColumnType.ArrowType` answers;
  a nil batch is a table of no rows. `Generate`, `GenerateSchema` and
  `GenerateLike` draw into Arrow builders; `Create` pushes through
  `ArrowWriter`; `Check` and `CheckColumn` compare a read's columns by
  name with `array.Equal`; `WithTable` and `Body` hand a test the batch
  an ingest body carries. The CSV renderer, the writer column data, the
  validity masks and the typed comparers are gone.
- `internal/qdb/read.go` builds the reader's schema from the binding's
  `ArrowType`; the server declares no column-type map of its own.
- `Session.Push` and every use of `qdbapi.Writer` are gone.
- The rendered and Arrow round trips compare the wire with the batch
  they encoded, after `table.Check` proves that batch; the decode round
  trip draws its tables through the fixture; the httpapi round trip
  renders its ingest body with the CSV encoder over `table.Body`.
- The vendored Go API is PR 126's head; `go.mod` is re-pointed at its
  squash commit before the fast-forward merge.

Left for later units: the NDJSON and Arrow IPC decoders (`docs/log.md`,
Next 2) and the e2e tool, which generates through this fixture
(`docs/e2e-v2-flow-plan.md`, The Go tool).

## Verified facts

- `TsColumnType.ArrowType` answers int64, float64, `timestamp[ns]` with
  no zone (one package-level pointer, deliberately not
  `arrow.FixedWidthTypes.Timestamp_ns`, whose zone is "UTC"), binary,
  and plain utf8 for string and symbol alike; an invalid type answers
  nil. Nullability and the reader's `max_width` metadata are not part
  of it (`vendor/github.com/bureau14/qdb-api-go/v3/entry_timeseries_common.go:176-195`,
  Go API session, 2026-10-06). This is field for field what
  `internal/qdb/read.go:46-66` declares today.
- `ColumnTypeOfArrow` never infers a symbol; the Arrow writer's
  validation goes through it (`entry_timeseries_common.go:217-239`,
  `writer_arrow.go:67-78`). The fixture therefore keeps QuasarDB's
  column type as its vocabulary (owner, 2026-10-06).
- The whole Go API is one cgo package, 35 of 40 files with
  `import "C"`; no importer compiles without cgo (`vendor/.../v3/*.go`,
  checked 2026-10-06). The mapping is vendored at PR 126's head,
  `14cfccc8f0e3cb77ce1212427686245a42d0b4b3`
  (`go.mod`: `v3.9.1-0.20261006010600-14cfccc8f0e3`), open, Buildkite
  build 249 green; its squash commit is read from
  `gh pr view 126 --json mergeCommit` (Go API session, 2026-10-06).
- `ArrowWriter.SetTable` needs a `$timestamp` field of a timestamp type
  with no null slot, refuses a `$table` field, accepts zero-row batches;
  `Push` skips tables with zero rows and makes no C call when none
  remain; the caller still owns every batch after `Push`
  (`writer_arrow.go:52-78`, `writer_arrow.go:247-291`).
- The server stores a zero-length string or blob as null and reads a
  stored `MinInt64` or NaN as null, whichever writer sent it
  (`git show 13e03b6:docs/arrow-ingest-plan.md`, Verified facts, against
  `qdb/timeseries/none_value.hpp`, 2026-10-05). A generator keeps
  avoiding them even though the validity bitmap now carries the nulls.
- `rapid.Float64()` is `Float64Range(-MaxFloat64, MaxFloat64)`: no NaN,
  no infinity (`vendor/pgregory.net/rapid/floats.go:58-61`).
- The bulk reader drops one trailing NUL from a string cell (sc-19829,
  `internal/qdb/read_test.go:120-124`); the draw keeps NUL out.
- `array.Equal` compares type and values with nulls by validity;
  `array.RecordEqual` compares column counts, row counts and columns
  through it, not schema metadata or nullability
  (`vendor/github.com/apache/arrow-go/v18/arrow/array/compare.go:31,202`).
- `internal/encoding`'s tests are white-box (`package encoding`:
  `render_test.go:8`, `arrow_test.go:6`, `decode_test.go:4`) and import
  the fixture, so the fixture importing `internal/encoding` is a test
  import cycle Go refuses. The fixture hands a test the batch; the test
  runs the encoder.
- `encoding/csv` quotes a field with a leading space, a comma, a quote,
  a CR or an LF and reads it back (`internal/encoding/csv.go:108-114`);
  the decode round trip already draws `,`, `"` and LF into strings
  (`internal/encoding/decode_test.go:72`).
- Callers that compile unchanged after the fixture changes: the
  schema-only literals of `internal/httpapi/errors_test.go:32-33`,
  `createBodyOf` (`tables_test.go:53-60`), `TestQueryCompressed`,
  `TestReadTableRange` and `TestReadAnswersRowsWritten`, which use
  `Generate`, `Create`, `Select`, `Columns` by name and `Check` only.

## Design

Every function, type and variable the unit adds or changes, in file
order; the doc comment is the contract, the numbered overview is what
the body states. Everything not named is unchanged.

### `internal/qdb/read.go` (commit 3)

`timestampType` and `arrowTypes` leave. `specialFields` and
`dataFields` take their types from the binding:

```go
// specialFields are the two columns every table has and the reader
// answers non-nullable: the table a row came from and its index.
var specialFields = map[string]arrow.Field{
	"$table":     {Name: "$table", Type: qdbapi.TsColumnString.ArrowType()},
	"$timestamp": {Name: "$timestamp", Type: qdbapi.TsColumnTimestamp.ArrowType()},
}

// dataFields is the table's own columns as the reader answers them: the
// binding's Arrow type of each column type, nullable, in the table's
// order, keyed by name for the requested subset.
func dataFields(cols []qdbapi.TsColumnInfo) ([]arrow.Field, map[string]arrow.Field)
```

Both bare (rule 1). The symbol-reads-as-string fact moves from the
map's comment to the binding's doc comment, where it now lives.

### `internal/encoding/render_test.go` (commit 4)

The file header's last clause becomes "every cell compared with the
batch encoded, which the fixture's Check has proven to be the table
written". `wordOf`, `nanosOf`, `checkValues`, `checkCells`,
`checkRendered` change; the decoders and parsers stay.

```go
// wordOf is the wire type word of a column's Arrow type.
func wordOf(dt arrow.DataType) string

// checkValues compares every valid slot of a rendered column with the
// value encoded.
func checkValues[V any](t failer, name string, want arrow.Array, value func(int) V, got func(int) V, equal func(V, V) bool)

// checkCells compares one rendered column with the column encoded: name,
// wire type where carried, every validity bit, and every value parsed
// back from its text.
func checkCells(t failer, f arrow.Field, want arrow.Array, got wireColumn)

// checkRendered compares a rendered body with the batch encoded, column
// by column in order.
func checkRendered(t failer, rec arrow.RecordBatch, got []wireColumn)
```

`checkCells` switches on the array's concrete type (`*array.Int64`,
`*array.Float64`, `*array.Timestamp` through `nanosReader`,
`*array.String`, `*array.Binary`); the `qdbapi` import leaves.
`TestRenderedRoundTrip` gains one step after the query:
`table.Check(rt, tbl, rec)`, with the step comment "the batch is the
table written; the wire is then checked against the batch, so one
comparer, the fixture's, decides what was written". The names for the
NDJSON decoder come from `rec.Schema()`. All bare beyond that (rule 2).

### `internal/encoding/arrow_test.go` (commit 5)

The file header's last clause becomes "compared column by column with
the batch encoded, which the fixture's Check has proven". After
`decode`, `TestArrowRoundTrip` runs `table.Check(rt, tbl, rec)` once
and then, per column, `array.Equal(rec.Column(i), cols[i])`, the
failure naming the field; the `table.Columns` loop leaves. Bare.

### `internal/qdbtest/table/table.go` (commits 6 and 7)

Package doc: "Package table is the generated table fixture, in building
blocks that stack: GenerateSchema draws a table without rows, Generate
draws rows into one as a record batch in the reader's types, GenerateLike
draws another table of the same columns, RemoveOnCleanup removes
whatever a table leaves behind, and Create creates the table, pushes
its batch through the Arrow writer and removes it on the test's
cleanup. Check and CheckColumn compare what a read answers with the
batch written, by array equality. WithTable and Body are the batch as
an ingest body carries it, for a test that pushes through its own door.
Rules: internal/AGENTS.md, Tests."

```go
// Column is one column of a table's schema: its name, its QuasarDB
// column type and, for a symbol, its symtable.
type Column struct {
	Name     string
	Type     qdbapi.TsColumnType
	Symtable string // symbol columns only
}

// Table is a generated table: its name, its schema and its rows as one
// record batch, $timestamp first and then the columns in order, each in
// the Arrow type the binding's ArrowType answers for its column type. A
// nil Batch is a table of no rows. The fixture releases the batch on
// the test's cleanup; a test never releases it.
type Table struct {
	Name    string
	Columns []Column
	Batch   arrow.RecordBatch
}

// columnTypes is what a column's type is drawn from.
var columnTypes = []qdbapi.TsColumnType{...}   // unchanged

// indexStart is the first index value; every index begins here.
var indexStart = ...                            // unchanged

// drawSymbol draws a symbol value: a symtable entry, which the server
// rejects arbitrary bytes in.
func drawSymbol(rt *rapid.T) string            // `[a-zA-Z0-9]{1,16}`

// drawString draws a string value: the characters the text wires must
// quote or escape (a space, a comma, a quote, <&>, an LF) among plain
// ones, never empty and never a NUL. The server stores the empty string
// as null, and the bulk reader drops a trailing NUL (sc-19829).
func drawString(rt *rapid.T) string            // `[a-zA-Z0-9 ,"<&>\n]{1,16}`

// drawTime draws a timestamp value in the range the result set accepts.
func drawTime(rt *rapid.T) time.Time           // unchanged

// appendValue draws one value of kind into b. The server reads a stored
// MinInt64 or NaN as null and a zero-length string or blob as null, so
// none of them is drawn: a value read back must be the value written.
func appendValue(rt *rapid.T, kind qdbapi.TsColumnType, b array.Builder)

// generateArray draws n cells of kind, each null at nullPct percent.
func generateArray(rt *rapid.T, kind qdbapi.TsColumnType, n, nullPct int) arrow.Array

// generateIndex draws a strictly ascending $timestamp column of n rows: a
// drawn step from a fixed start, so no two rows collide.
func generateIndex(rt *rapid.T, n int) arrow.Array

// Schema is the schema of tbl's batch: $timestamp, non-nullable, then
// the columns in order, nullable, in the binding's Arrow types.
func Schema(tbl Table) *arrow.Schema

// generateRows draws tbl's rows into its batch: a row count and one
// null density for the whole table, so runs range from no nulls to
// all-null columns. The batch is released on rt's cleanup.
func generateRows(rt *rapid.T, tbl Table) Table

// generateColumn, GenerateSchema: unchanged.

// Generate draws a table: a schema and its rows.
func Generate(rt *rapid.T) Table

// GenerateLike draws a table of tbl's columns under a fresh name, with
// its own rows: the same names and types, a symbol column's symtable
// named after the new table, so several tables share one column list.
func GenerateLike(rt *rapid.T, tbl Table) Table

// Select: unchanged.

// Rows is tbl's row count; a nil batch has none.
func (tbl Table) Rows() int

// batchOf is tbl's batch, retained, or an empty batch of its schema when
// tbl has none; the caller releases it.
func batchOf(tbl Table) arrow.RecordBatch

// expected is the column a read of tbl answers under name, which the
// caller releases: $table is the name in every row, anything else the
// batch's column of that name; false when tbl has none.
func expected(tbl Table, name string) (arrow.Array, bool)

// CheckColumn compares one column read back under name with the column
// written: the type, every validity bit, every value. Null slots are
// compared by validity only.
func CheckColumn(t T, tbl Table, name string, got arrow.Array)

// Check compares a record batch read back with tbl, column by column by
// name, so a read that answers $table, $timestamp and the columns and one
// that answers a requested subset are checked alike.
func Check(t T, tbl Table, rec arrow.RecordBatch)

// WithTable is tbl's batch as an ingest body carries it: a $table column
// of the name in front. Released on t's cleanup.
func WithTable(t T, tbl Table) arrow.RecordBatch

// Body is the tables as one ingest body carries them: their WithTable
// batches concatenated, which needs one column list. Released on t's
// cleanup. An encoder run over it is a body of that format.
func Body(t T, tables ...Table) arrow.RecordBatch

// columnInfos, entries, call, RemoveOnCleanup: unchanged.

// Create creates tbl in the cluster, pushes its batch through the Arrow
// writer, and removes it on t's cleanup. The cleanup is registered as
// soon as the table exists, so a failed push leaves nothing behind.
func Create(t T, c *qdb.Cluster, tbl Table)
```

Narrated under rule 3:

- `appendValue`: a domain rule. Its doc comment carries the rule; each
  case states its draw in one line (the int64 range from `MinInt64+1`,
  `rapid.Float64()` which draws no NaN, symbol or string by kind, a
  blob of 1 to 64 bytes, a timestamp through `drawTime`).
- `Create`: overview "1. create the table; 2. register the removal
  before any push, so a failed push leaves nothing behind; 3. no rows:
  return, so no session is leased for a push the writer would skip
  anyway; 4. stage the batch in one fast-push writer and push through
  one session".
- `Body`: overview "1. one WithTable batch per table, which share a
  schema because one body is one column list; 2. concatenate per
  column; 3. one batch over the first's schema, released on cleanup".

Everything else is bare (rules 1 and 2). Leaving the file: `Index`,
`Column.Data`, `Column.Valid`, `cells`, `generateMask`, `generateData`,
`drawText`, `allValid`, `Columns`, `ColumnOf`, `timestampLayout`,
`csvCell`, `CSV`, `typed`, `checkValues`, `same`, `nanos`, `writerOf`.

Commit 6 lands everything above except `drawString` and `Body`, with
`drawSymbol` serving both text types under its old name `drawText`;
commit 7 adds `Body`, splits `drawString` out and renames `drawText`.

### `internal/qdb/read_test.go` (commit 6)

`TestReadDropsTrailingNUL` builds its one-row batch over
`table.Schema(tbl)` with a timestamp builder and a string builder, in a
local `oneRow` helper ("oneRow is tbl's batch of one row at the start
of time holding s in its one string column"); the `ColumnData` literal
leaves. Bare.

### `internal/httpapi/roundtrip_test.go` (commits 6 and 9)

Commit 6: `ingestBodyOf(t table.T, tables []table.Table) string` runs
`encoding.CSV{}.Encode` over `table.WithTable` per table, keeping the
header-stripping join; `len(tbl.Index)` becomes `tbl.Rows()`; the
empty read sets `empty.Batch = nil`. Commit 9: `ingestBodyOf` becomes
the encoder over `table.Body(t, tables...)` and the join leaves; its
doc comment: "ingestBodyOf is the one CSV body that carries every
table: the CSV encoder over the tables' one batch". Bare.

### `internal/encoding/decode_test.go` (commit 8)

`genSchema`, `genCell`, `genBatch`, `withTable`, `bodyOf` and
`wireTypes` leave; `specials` stays for `TestCSVDecodeFaults`'s
`typed`. The file header: "for every codec, tables drawn through the
fixture go through the encoder as one body and come back out of the
decoder as the batches drawn". `TestDecodeRoundTrip` per iteration:
"1. one to three tables of one column list through the fixture, zero
rows allowed; 2. one body: the codec's encoder over `table.Body`; 3. decoded under a `SchemaOf` answering the reader's whole-table
schema, `table.WithTable(first).Schema()`, for every name; 4. the
batches with rows come back in first-seen order, each `RecordEqual` to
the table's batch, and every batch is released". Bare beyond the step
comments.

### `internal/qdb/cluster.go` (commit 10)

`Session.Push` leaves. `PushArrow` unchanged.

### Documents (commit 11)

- `internal/AGENTS.md`, Code, the read paragraph: after "built from
  `ColumnsInfo`", one sentence: "the Arrow type of a column type is the
  binding's `TsColumnType.ArrowType` (a symbol reads as a string); the
  server declares no map of its own". Tests, the fixture paragraph,
  rewritten: a test draws a table with `internal/qdbtest/table`
  (`Generate`, then `Create`), which holds its rows as one record batch
  in the binding's Arrow types and pushes it through the Arrow writer;
  a test that pushes through its own door takes `GenerateSchema`,
  `GenerateLike`, `RemoveOnCleanup`, and a body is the format's encoder
  run over `table.Body` (or `WithTable` for one table), since the
  fixture cannot import `internal/encoding` (the encoding tests are
  white-box and import the fixture); what came back is compared with
  `table.Check` or `table.CheckColumn`, by array equality by name, no
  test carrying a comparer of its own; the draw avoids `MinInt64`, NaN,
  the empty string, the empty blob and NUL because the server reads the
  first four back as null and the bulk reader drops a trailing NUL;
  strings draw the characters the text wires quote, symbols only what
  a symtable takes. The sentence on the batch writer's sentinels leaves.
- `internal/encoding/AGENTS.md`, Tests: "The decoders are one
  generative round trip over every codec (`decode_test.go`): tables
  drawn through the fixture, encoded as one body over `table.Body` and
  decoded back to the batches drawn, no cluster; the faults of a body
  are one table."

### `docs/log.md` (commit 12)

Current state: Next 1 leaves, the rest renumber; `Last updated`
2026-10-06. One entry: "2026-10-06 -- the table fixture holds one
record batch; fixture-arrow-plan.md deleted. The fixture pushes
through the Arrow writer and is checked by array equality; the
column-type map is the binding's (`TsColumnType.ArrowType`, qdb-api-go
PR 126). Owner decisions: QuasarDB's column types stay the fixture's
vocabulary; `internal/model` imports no binding. The rules to
`internal/AGENTS.md` (Tests, the read) and `internal/encoding/AGENTS.md`
(Tests)."

## Rationale

| decision                                                                                | why                                                                                                                                                         | rejected, and why                                                                                                                     | gained                                                          | given up                                                                                   | settled by                                                                           |
| --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| The fixture holds one record batch in the reader's types                                | one representation: what the Arrow writer takes and the reader answers                                                                                      | keeping writer column data next to a batch: two copies of every row                                                                   | one generator for cluster tests, decoders and the e2e tool      | the sentinel null vocabulary                                                               | `docs/log.md`, Next 1                                                                |
| `Create` pushes through `ArrowWriter`; no rows, no push                                 | the writer would skip the table anyway; skipping first leases no session                                                                                    | always pushing: a lease for nothing                                                                                                   | one fewer session per empty table                               | nothing                                                                                    | `writer_arrow.go:283-291`; owner, 2026-10-05                                         |
| `Check` compares per column by name with `array.Equal`                                  | the failure names the column; array equality ignores nullability and `max_width`, which the reader adds and the fixture does not                            | `RecordEqual` over a rebuilt record: trips on the reader's metadata and names no column                                               | one comparer for query results, whole reads and subsets         | nothing                                                                                    | owner, 2026-10-06 (A)                                                                |
| The column-type map is the binding's `ArrowType`; `read.go` drops its own               | one home for the fact, upstream, next to the reader and writer that define it; the two agreed field for field                                               | an export from `internal/qdb` for the fixture: a second home; a map in `internal/model`: makes the neutral package import the binding | no map in this repository                                       | a vendored branch commit until PR 126 merges                                               | owner, 2026-10-06 (B); Go API session, 2026-10-06                                    |
| `internal/model` stays free of the binding                                              | the no-cgo rule is layering, not build: the binary is cgo anyway and the encoding test binary already is; the neutral package knowing no C API is the value | `model` importing qdb-api-go for the type vocabulary                                                                                  | the layering rule of `internal/model/AGENTS.md` kept            | nothing this unit needs                                                                    | owner, 2026-10-06                                                                    |
| QuasarDB's column types stay the fixture's vocabulary                                   | create needs them and `ColumnTypeOfArrow` never infers a symbol                                                                                             | the fixture drawing Arrow fields and deriving the column type                                                                         | `createBodyOf` and the literals unchanged                       | nothing                                                                                    | owner, 2026-10-06 (13)                                                               |
| The encoder tests compare the wire with the batch encoded, after `Check`                | the fixture's check is the one comparer; the tests need no typed accessors on it                                                                            | typed accessors on the fixture's batch for the tests                                                                                  | the fixture shrinks; wire checks are independent of its storage | nothing                                                                                    | owner, 2026-10-06 (C)                                                                |
| The CSV renderer leaves the fixture; a test runs the encoder over `WithTable` or `Body` | the encoding tests are white-box and import the fixture, so the fixture cannot import `internal/encoding`                                                   | the fixture rendering through the encoder; a renderer of its own, a second CSV dialect                                                | one dialect, the encoder's                                      | a test writes two lines to get a body                                                      | the Go toolchain (import cycle in test); `docs/log.md`, Next 1                       |
| `Body` concatenates the tables into one batch                                           | format-agnostic: NDJSON has no header to strip and Arrow IPC no lines; it is what a client sends                                                            | the header-stripping join, CSV-only                                                                                                   | the decoders' round trip works for every format                 | nothing                                                                                    | proposal, settled by this plan's approval                                            |
| The decode round trip draws through the fixture                                         | one generator; the owner put the fold in scope                                                                                                              | the test's own `genSchema`/`genBatch`: a second draw of the same tables                                                               | the hand-built generators leave                                 | the draw of arbitrary nanosecond timestamps and `MinInt64`, now within the server's bounds | owner, 2026-10-06 (12)                                                               |
| Generators stay functions of `rt` that stack; no option structs                         | the three consumers differ only in bounds and one set serves all; stacking is the dependency injection                                                      | `table.Gen(opts...) *rapid.Generator[Table]`, pytest-style parametrization                                                            | no scaffolding                                                  | per-consumer bounds, until a consumer needs them                                           | owner, 2026-10-06 (12), the recommendation accepted by the plan's approval           |
| Strings draw the awkward characters; symbols stay plain                                 | the decoders need what the text wires quote; the server takes any bytes in a string, a symtable not                                                         | one plain draw for both                                                                                                               | the cluster tests cover quoting too                             | nothing                                                                                    | `decode_test.go:72`; `docs/e2e-v2-flow-plan.md`, The Go tool                         |
| The draw avoids the server's null sentinels and NUL                                     | the server reads a stored `MinInt64`, NaN, empty string or empty blob as null; the reader drops a trailing NUL                                              | drawing them and excluding null slots from the comparison                                                                             | a value read back is the value written                          | four values and one byte never generated                                                   | `git show 13e03b6:docs/arrow-ingest-plan.md`, Verified facts; `read_test.go:120-124` |
| The vendor commit landed before the plan                                                | the owner's order                                                                                                                                           | the plan first                                                                                                                        | none                                                            | none                                                                                       | owner, 2026-10-06                                                                    |

## Knowledge

Per commit, where each why lands.

3. `refactor(qdb)`: `specialFields` and `dataFields` are bare; the
   symbol-as-string fact lives in the binding's doc comment
   (`entry_timeseries_common.go:163-169`); `internal/AGENTS.md`, the
   read paragraph, says the map is the binding's (commit 11).
4. `test(encoding)`, render: the step comment after the query says the
   fixture's check decides what was written (Rationale: encoder tests
   compare with the batch; evidence `internal/AGENTS.md:165-168`). The
   file header carries the same claim.
5. `test(encoding)`, Arrow: the file header and the step comment, as
   above.
6. `test(qdbtest)`: `Table`'s doc comment carries the batch's layout,
   the nil rule and the release ownership; `appendValue`'s doc comment
   carries the sentinel rule with the server as the reason (Rationale:
   the draw avoids the sentinels); `drawString` names the empty string
   and sc-19829; `Create`'s overview says why no rows means no push
   (`writer_arrow.go:283-291`) and why the removal is registered first
   (the existing comment); `CheckColumn`'s doc comment says null slots
   compare by validity only; `WithTable`'s doc comment says why `$table`
   goes in front (an ingest body carries it). `oneRow` in `read_test.go`
   is bare.
7. `test(qdbtest)`, `Body`: the overview says the batches share a
   schema because one body is one column list
   (`internal/AGENTS.md:64-67`); the doc comment says an encoder over it
   is a body of that format (Rationale: `Body` concatenates).
   `drawString`'s doc comment names the characters and why.
8. `test(encoding)`, decode: the step comments above; the file header
   says the tables come from the fixture.
9. `test(httpapi)`: `ingestBodyOf`'s doc comment.
10. `refactor(qdb)`: deletion; `cluster.go`'s remaining comments read
    back against the file (rule 6).
11. `docs(agents)`: the two `AGENTS.md` passages of Design, Documents.
    Homes given here and nowhere else: the import-cycle reason for the
    fixture not rendering; the map being the binding's; the sentinel
    reason being the server's, not the writer's; strings versus symbols.
12. `docs(log)`: the entry; Next 1 leaves; this plan deleted.

Facts with no home until commit 11: the import-cycle reason and the
server-side sentinel reason; both land in `internal/AGENTS.md`, Tests.
The `internal/model` layering decision lands in the log entry; its rule
already stands in `internal/model/AGENTS.md`.

## How the knowledge lands

The executor runs `/doc-discipline read` before commit 3. Every
commit's comments and document rows are written from Design and
Knowledge above, in the same commit as the code, in the shape of the
root `AGENTS.md`, "Code comments". A why that arises while building
and is not in this plan is written where it is decided, with its
evidence, or asked of the owner through the question tool before the
commit that needs it; never guessed, never dropped silently. After
commit 10 and before commit 11:
`/doc-discipline all internal/qdbtest internal/qdb internal/encoding internal/httpapi`,
one small commit per finding. Before the build-stage message:
`/doc-discipline check internal/qdbtest internal/qdb internal/encoding internal/httpapi internal/AGENTS.md docs/fixture-arrow-plan.md`,
its report quoted in the message. After every Markdown edit
`npx prettier --write`; before commit 11 the four greps of
`docs/AGENTS.md`, Checks, over the touched documents. Every commit
builds and passes `make lint`; the Go tests run `-p 1` against the live
pair from `scripts/tests/setup/start-services.sh`.

## Commits

1. `build(deps): vendor qdb-api-go at the column-type to Arrow mapping`
   (landed, `e07ea3e`; the owner ordered it before the plan)
2. `docs(plan): fixture-arrow-plan.md, the table fixture holds one record batch and pushes through the Arrow writer`
   (this document)
3. `refactor(qdb): the reader's Arrow types are the binding's ArrowType`
4. `test(encoding): the rendered round trip compares the wire with the batch encoded; Check proves the batch`
5. `test(encoding): the Arrow round trip compares decoded columns with the batch encoded by array equality`
6. `test(qdbtest): the table holds its rows as one record batch, pushed through the Arrow writer and checked by array equality`
7. `test(qdbtest): Body concatenates tables into one ingest batch; strings draw the awkward characters`
8. `test(encoding): the decode round trip draws its tables through the fixture; its own generators leave`
9. `test(httpapi): the ingest body is the CSV encoder over table.Body`
10. `refactor(qdb): Session.Push and the sentinel writer leave`
11. `docs(agents): the fixture holds a record batch; a body is the encoder over Body; the sentinels are the server's; the type map is the binding's`
12. `docs(log): the fixture pushes through the Arrow writer; fixture-arrow-plan.md deleted`
13. Verify: push `sc-19567/rr-fixture-arrow`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again.

Between 10 and 11: `/doc-discipline all`, a finding one more small
commit. Before the build-stage message: `/doc-discipline check` with
this plan. Before the fast-forward merge, in the merge stage: re-point
`go.mod` at PR 126's squash commit (`gh pr view 126 --json mergeCommit`),
`go mod tidy`, `go mod vendor`, one `build(deps)` commit, a green
build; if the PR is not merged by then, the owner decides whether the
merge waits.

## Open questions and recommendations

1. `Body` as one concatenated batch, replacing the header-stripping
   join in both tests: recommended; the alternative keeps a CSV-only
   trick the NDJSON decoder cannot reuse.
2. Generators without option structs: recommended; `table.Gen(opts...)`
   when a consumer needs other bounds.
