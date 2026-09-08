# M1 -- v2 Query: the Query Core and the Encoders -- Plan

Status: draft. This document plans the query half of milestone M1 as the
brief defines it (`docs/brief.md`, Milestones): `POST /api/v2/query`
streamed through the four content-negotiated encoders. It is a working
document: verified facts and dates are recorded here, not in the brief;
progress is recorded in `docs/log.md`, not here. The first unit of work
under it is the Arrow IPC encoder, specified in full below; the later
units of M1 (the sibling encoders, the handler, bearer authentication,
the minimal login, gzip) extend this document when they start. The
plan is deleted when M1 exits, its surviving facts moved to
`internal/AGENTS.md`, the `AGENTS.md` of `internal/encoding`, and ADRs
(`docs/AGENTS.md`, Plans).

## Purpose

Turn one query result into bytes on the wire in any of four formats,
from one execution core, with response memory bounded by this binary and
the first byte leaving as soon as the result exists. The formats and
their media types are fixed by the brief (Data plane):

| Accept                                | Format                           |
| ------------------------------------- | -------------------------------- |
| `application/json` (default)          | columnar tables/columns, v2's    |
| `application/x-ndjson`                | one JSON object per row          |
| `text/csv`                            | RFC 4180                         |
| `application/vnd.apache.arrow.stream` | Arrow IPC stream, record batches |

The Arrow encoder comes first because it is the one whose in-memory
shape the other three are checked against (the format-equivalence
property test decodes all four and compares), because Flight SQL (M5)
reuses its record-batch half unchanged, and because it fixes the binding
surface the other three consume.

## Verified facts (2026-09-08)

- The C API materializes a query result in full before returning
  (`qdb_query`, one shot); no cursor exists (`docs/brief.md`, Data
  plane). Streaming in M1 therefore means overlapping serialization and
  transmission with iteration over a materialized result.
- `qdb-api-go` PR #121 (`sc-19711/more-efficient-query-results`, commit
  `b3ccb61`) adds `QueryResultSet`: a Go-owned, columnar copy of a
  result with no C pointers and no Close obligation. Per column a sealed
  `QueryColumn` (`QueryColumnInt64`, `QueryColumnDouble`,
  `QueryColumnTimestamp`, `QueryColumnString`, `QueryColumnBlob`,
  `QueryColumnNull`), each a dense `Values` slice plus a validity `Mask`.
  `Query.Fetch()` executes, converts and releases the C result in one
  call.
- The result set is Arrow-shaped in memory: `Mask.Bytes()` is an Arrow
  validity bitmap (LSB-first, 1 = valid, `(n+7)/8` bytes, padding bits
  clear); `Values` of the fixed-width columns are Arrow values buffers;
  string and blob columns expose `Bytes()` (every cell back to back in
  row order, nulls contributing nothing) and `Offsets()` (`n+1` int32
  entries, `offsets[0] = 0`), which are exactly Arrow's data and offsets
  buffers for `Utf8` and `Binary`. A column past `math.MaxInt32` bytes
  is `ErrOutOfBounds` at conversion.
- `count(...)` cells fold into `QueryColumnInt64` upstream; the C API's
  `qdb_query_result_count` tag does not survive conversion. Timestamps
  are int64 nanoseconds since the Unix epoch, UTC; a cell outside the
  years 1678 to 2262 is `ErrOutOfBounds` at conversion. Symbols arrive
  as strings. Array-typed cells are `ErrNotImplemented` at conversion.
- The legacy golden `07-query-aggregate-reproduce` pins
  `"type":"count"` for `COUNT(id)` next to `"type":"int64"` for
  `SUM(firmLpId)` (`tests/e2e/golden/legacy/07-query-aggregate-reproduce/body`).
- `arrow-go` v18.7.0 is the latest stable release; neither it nor any
  Arrow package is vendored today. `go mod vendor` vendors only the
  packages the tree imports, not the module's whole dependency tree.
- Every supported platform is little-endian, so a `[]int64`,
  `[]float64` or mask byte slice is an Arrow buffer as it is.

## The query core

`internal/qdb` hands the rest of the server a `*qdbapi.QueryResultSet`
and nothing else: the binding's `QueryResult` (a view over C memory
with a Close obligation) never leaves the vendored package.

- `Cluster.Query(ctx, u, q, opts...) (*qdbapi.QueryResultSet, error)`
  replaces the callback form. The call completes, and the session
  returns to its pool, before a single response byte exists, so the
  session is held for the query's duration only, never for the
  response's; the read-retry option keeps its meaning.
- `Session.query` becomes a one-line `Fetch`; `Probe` runs the readiness
  query and discards the set.
- A statement that yields no result set (DDL) is a nil set; every
  encoder treats nil as zero columns and zero rows.
- Conversion errors (`ErrOutOfBounds`, `ErrNotImplemented`) are the
  binding's and surface through `Query`'s error like any other; the
  handler maps them to a client error (later unit).

## The encoder seam

Package `internal/encoding` (`docs/brief.md`, Project structure), one
file per format, definitions before use. The seam every format
implements:

```go
// Encoder writes one result set to w in one wire format.
type Encoder interface {
    // ContentType is the media type the handler answers with.
    ContentType() string
    // Encode writes rs to w. rs may be nil. It returns the first write
    // error; ctx ending between batches ends the encoding.
    Encode(ctx context.Context, w io.Writer, rs *qdbapi.QueryResultSet) error
}
```

Rules that hold for every encoder:

- An encoder holds no state across calls and logs nothing: a write
  error is the caller's to log, tagged by the request middleware.
- An encoder writes to the `io.Writer` it is given and never flushes:
  when to flush is the handler's decision (a size-threshold flushing
  writer around the response, later unit).
- Content negotiation, status codes, gzip and the `Content-Type` header
  are the handler's; an encoder knows its media type and its bytes.

## The Arrow IPC encoder

The Arrow IPC streaming format is a byte format, not a transport: a
Schema message, then record batches, then an end-of-stream marker, no
footer. The encoder produces those bytes on an `io.Writer`; HTTP carries
them. Two halves, split so Flight SQL reuses the first:

1. **Result set to record.** `Record(rs) arrow.Record` builds one
   record over the whole result set, zero-copy: every buffer is a Go
   slice the result set owns, wrapped with `memory.NewBufferBytes`. No
   builder, no per-cell loop.
2. **Record to stream.** `Encode` writes the schema, then the record in
   slices of `batchRows` rows through `ipc.NewWriter`, then the
   end-of-stream marker on `Close`.

### Type map

| `QueryColumn`          | Arrow type                     | validity       | buffers                |
| ---------------------- | ------------------------------ | -------------- | ---------------------- |
| `QueryColumnInt64`     | `Int64`                        | `Mask.Bytes()` | `Values`               |
| `QueryColumnDouble`    | `Float64`                      | `Mask.Bytes()` | `Values`               |
| `QueryColumnTimestamp` | `Timestamp(Nanosecond, "UTC")` | `Mask.Bytes()` | `Values`               |
| `QueryColumnString`    | `Utf8`                         | `Mask.Bytes()` | `Offsets()`, `Bytes()` |
| `QueryColumnBlob`      | `Binary`                       | `Mask.Bytes()` | `Offsets()`, `Bytes()` |
| `QueryColumnNull`      | `Null`                         | none           | none, length only      |

- Every field is nullable, whatever the data holds, so a query's schema
  does not change with its rows.
- Field names are the result's column names verbatim, duplicates kept;
  Arrow allows duplicate field names and the result set keeps every
  column in order.
- Null count is `Mask.NullCount()`; the validity buffer is always
  passed, even when no slot is null, so there is one path.
- Sentinels the result set stores in null slots (`MinInt64`, `NaN`,
  empty) are never on the wire's meaning: a reader ignores the bytes of
  a slot whose validity bit is clear. A valid `NaN` double is a value.
- `Utf8` and `Binary`, not the Large variants: 32-bit offsets are what
  every consumer reads natively (pyarrow, ADBC, JDBC, DuckDB, Arrow
  JS), and the result set already caps a column at `math.MaxInt32`
  bytes, so the offsets cannot overflow.
- No dictionary encoding for symbols in M1.

### Batches

- `batchRows` is a constant of the encoder, 65536, not configuration.
  The C API materializes the full result before the first byte, so the
  batch size bounds nothing on the server; it is the consumer's
  granularity. The edge it leaves open: a batch of wide blob rows is as
  large as those rows.
- A slice of the whole record is zero-copy for fixed-width columns; the
  IPC writer rebases the offsets of a sliced string or blob array, a
  copy of `batchRows` int32 offsets per batch, never of the data.
- `ctx.Err()` is checked between batches; a client that left ends the
  encoding at the next batch boundary.
- An empty result set is the schema and the end-of-stream marker, a
  valid stream with zero batches; a nil set is a schema with no fields.

### Memory

- Wrapped buffers are garbage-collected Go memory; `Retain`/`Release`
  are no-ops on them. The encoder still releases the record and its
  slices in `defer`, the arrow-go convention, so a later allocator
  change needs no call-site change.
- The IPC writer allocates its own scratch (message framing, padding)
  from `memory.DefaultAllocator`. No custom allocator in M1.
- In-format record-batch buffer compression (lz4, zstd) is deferred past
  M1: the brief calls it request-negotiable and names no mechanism, gzip
  at the HTTP layer is M1's compression, and the two are independent.

### Errors

The result set is complete in memory before `Encode` runs, so the only
failures during encoding are write failures and ctx ending: the status
line has left, nothing can be reported in-band, and the handler logs the
error with the request id. Nothing the encoder does can fail before the
first write except the type switch meeting an unknown `QueryColumn`,
which is a programming error against the sealed set and panics.

## Tests

- A property test (`pgregory.net/rapid`) against the live qdbd fixture
  (`internal/qdbtest`): generate a table with every column type and a
  null density, write rows through the binding's batch writer, query it,
  encode the result set through the Arrow encoder with a small
  `batchRows` so rows span batches, decode with `ipc.NewReader`, and
  compare the decoded record with the result set: schema, every value,
  every null, the row count, over batch boundaries. One cluster, one
  user, sessions bounded as `internal/AGENTS.md` requires. `batchRows`
  is a parameter of the writing function so the test can lower it; the
  handler passes the constant.
- The same generator and decoder become the format-equivalence test once
  the sibling encoders exist: decode all four, compare to the result
  set. That test is the M1 exit criterion (`docs/log.md`).
- No test of the C API, no mocks, no test of glue (`internal/AGENTS.md`).

## Vendoring

Both changes are mechanical, never by hand under `vendor/`:

```
go get github.com/bureau14/qdb-api-go/v3@b3ccb61
go get github.com/apache/arrow-go/v18@v18.7.0
go mod tidy
go mod vendor
```

- `b3ccb61` is on the PR branch; once PR #121 is on `master`, re-pin to
  the master commit the same way before this branch fast-forwards into
  the base, so the base never points at a branch commit for longer than
  the work takes.
- `go mod vendor` brings in only the arrow-go packages the encoder
  imports (`arrow`, `array`, `memory`, `ipc`) and what they import
  (flatbuffers, compression libraries for the IPC reader, and a few
  more); the module's own test and tooling dependencies stay out.

## Compatibility deviation: `count` is `int64`

Owner decision, 2026-09-08: clients have always had to treat `count`
and `int64` identically, and a count is an int64 number, so v1 answering
`"type":"int64"` where the old server answered `"type":"count"` is an
accepted deviation. Consequences:

- The brief's Compatibility contract gains the deviation next to the
  dropped sentinels (edit to be approved with this plan).
- The legacy byte-shape compare (M3, `tests/e2e/legacy.sh`) must accept
  `int64` for a golden column typed `count`, or golden 07 is re-captured
  with the deviation applied; decided at M3.

## Implementation order (the Arrow unit)

1. Vendor `qdb-api-go` at `b3ccb61` and `arrow-go` v18.7.0.
2. `Cluster.Query` returns a `QueryResultSet`; `Session.query` is a
   `Fetch`; `Probe` discards the set.
3. `internal/encoding`: the `Encoder` seam and the result-set-to-record
   half.
4. The IPC stream half: `Encode` over batches.
5. The round-trip property test against the live qdbd.
6. `docs/log.md` Current state: in flight, and the brief's deviation.

## Open questions

1. The brief's Compatibility contract text for the `count` deviation:
   written with this plan, or at M3 with the wrapper.
2. Whether `Record(rs)` and the batching iterator are exported now for
   M5 or unexported until Flight SQL needs them.

## Decision log (2026-09-08)

| Decision                                                   | Why                                                                                                | Rejected                                                                             |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| `QueryResultSet` is the only result type outside `vendor/` | Go-owned memory, no Close obligation, session returns to the pool before the response starts       | encoding inside the `QueryResult` callback (session held, C lifetime in the encoder) |
| Zero-copy buffers from the result set's accessors          | the columns are already Arrow-shaped; builders would copy every string and blob byte a second time | arrow-go builders                                                                    |
| `Utf8` / `Binary`, int32 offsets                           | native for every consumer; result sizes are bounded and the column cap makes overflow impossible   | `LargeUtf8` / `LargeBinary`                                                          |
| `Timestamp(ns, "UTC")`                                     | the result set's representation, no conversion, full nanosecond precision                          | microseconds; a naive (zone-less) timestamp                                          |
| Every field nullable                                       | a stable schema per query, independent of the rows                                                 | nullability from the null count                                                      |
| `batchRows` a constant (65536)                             | bounds nothing on the server; the plain option first                                               | a config knob                                                                        |
| No IPC buffer compression in M1                            | independent of HTTP gzip, which M1 carries; negotiation mechanism unspecified                      | lz4/zstd record batches now                                                          |
| Encoder never flushes, never logs                          | flushing and logging are the handler's, tagged by the middleware                                   | a flushing encoder                                                                   |
| `count` answers as `int64` in v1                           | owner decision; clients never distinguished them; the tag does not survive the binding             | a `QueryColumnCount` upstream; reading `QueryResult` in the wrapper                  |
| Vendor the PR commit now, re-pin to master before merging  | the accessors are needed today; the base never keeps a branch pin                                  | waiting for the merge; builders meanwhile                                            |
