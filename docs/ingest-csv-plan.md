# The CSV ingest -- Plan

Status: approved. Scaffolding for one unit of work: `POST /api/v2/rows`
taking a `text/csv` body, the writer's door as
`GET /api/v2/tables/{name}/rows` is the reader's. Deleted when the
ingest lands; the rules move to the `AGENTS.md` of `internal` and
`internal/httpapi` first (`docs/AGENTS.md`, Plans). What the owner
fixed is in `docs/log.md`, Current state, Next 1, amended by the
decisions of 2026-09-23 below; this plan turns it into code. The
NDJSON and Arrow IPC bodies and the request codings are Next 2, not
this unit.

## What the ingest is

One request is one batch push: the body's rows, routed to their tables
by a `$table` column, go to the cluster through the binding's writer in
one `qdb_exp_batch_push_with_options` call (`docs/brief.md`, endpoint
sketch). Every table in one body carries the same column list: the
binding refuses a writer whose tables differ
(`vendor/github.com/bureau14/qdb-api-go/v3/writer.go`, `SetTable`,
"schema mismatch"), and one kind of table per batch is how ingestion
is done (owner, 2026-09-23). A body whose tables differ is refused
whole; nothing is pushed.

The session is held for the whole request (owner, 2026-09-23): one
`Call` leases a session of the caller's pool, the parser looks each
table's columns up through it the first time a row names the table,
the writer pushes through it, and the lease ends with the answer. A
slow client therefore occupies one session for as long as its body
takes to arrive; the session budget bounds that like every other call.
The push is never retried (`internal/qdb/cluster.go`, `WithReadRetry`).

Verified against qdb-api-go `abadf4e` (2026-09-23):

- `Writer.SetTable` refuses a table whose `WriterColumn` list (name and
  type) differs from the first table's; `Push` refuses a writer with
  no tables.
- Deduplication is written per table (`writer_table.go`, `toNative`:
  `deduplication_mode`, `where_duplicate`); the push mode is written
  into the options (`writer_options.go`, `setNative`); the push flags
  (write-through, async client push) are not written and are not used
  here.
- `upsert` without columns is refused by the binding too; `drop`
  without columns would deduplicate on every column, a case the
  handler never reaches because it requires columns for either mode.
- `WriterTable.SetIndex` refuses `NullTime`; the null of every other
  type is the type's sentinel (`MinInt64`, `NaN`, the empty string,
  the nil blob, `NullTime`), so a null cell is written as its sentinel
  and the empty string cannot be written (`docs/log.md`, Handoff to
  M2).
- The C API's async push mode returns before the rows are readable, so
  a test reads back only after `transactional` and `fast`.

## Design

### `internal/qdb`, `ingest.go`

One file: the push options, the column lookup, the writer building and
the CSV reading. A later body format adds functions to this file, not a
file of its own: the CSV part is not specific to QuasarDB, and the
package's story is the writer (owner, 2026-09-23).

```go
type PushOptions struct {
    Mode                 string   // transactional | fast | async; "" is fast
    DeduplicationMode    string   // "" (none) | drop | upsert
    DeduplicationColumns []string // required by either mode, refused without one
}

var ErrInvalidPushOptions = errors.New("qdb: invalid push options")
var ErrInvalidRows = errors.New("qdb: invalid rows")

type IngestResult struct {
    Rows, Tables int
    Parse, Push  time.Duration
}

func (c *Cluster) IngestCSV(ctx context.Context, u User, body io.Reader, o PushOptions) (IngestResult, error)
```

`PushOptions.writerOptions()` maps the words onto the binding's
`WriterOptions`, `ErrInvalidPushOptions` for a word outside the
vocabulary or a deduplication mode without columns (or columns without
a mode); it runs before the lease.

`IngestCSV` runs inside `Call`, never retried. The steps, each a
function of the file:

1. `encoding/csv` reads the header: `$table` and `$timestamp` are
   required, every other name is a data column; a name missing is
   `ErrInvalidRows`. The reader's field count is fixed by the header,
   so a short or long record is the standard reader's error, wrapped
   in `ErrInvalidRows` with the row number.
2. A `columns` lookup, `map[string][]qdbapi.WriterColumn`, filled
   through the held session the first time a record names a table:
   `Table(name).ColumnsInfo()` typed onto the header's data columns.
   A header name the table does not have is `ErrInvalidRows`; a table
   whose typed header differs from the first table's is
   `ErrInvalidRows` naming both tables; a table the cluster does not
   know is the binding's `ErrAliasNotFound`, which `IsTableNotFound`
   classifies. Each is the whole request's failure, found before the
   push.
3. Cells: per data column one appender chosen once from the column's
   type, the CSV encoder's rendering inverted (`internal/encoding/csv.go`,
   `csvCell`): `int64` through `strconv.ParseInt`, `double` through
   `strconv.ParseFloat`, `timestamp` through `time.RFC3339Nano`,
   `string` and `symbol` as their own bytes, `blob` as standard base64.
   The empty field is the type's null sentinel; an empty `$timestamp`
   is `ErrInvalidRows` (the index cannot be null); a field that does
   not parse is `ErrInvalidRows` with the row and column. Records
   stream into per-table column slices; nothing is held twice.
4. At EOF, zero rows answers `IngestResult{}` without a push (the
   writer refuses an empty push). Otherwise one `WriterTable` per table
   over the header's data columns, `SetIndex`, `SetData`, one `Writer`
   under the push options, `SetTable` each, `Push` through the
   session. `Parse` is the time from the first read to EOF, `Push` the
   push call; both `time.Since`, data in flight (ADR-0006,
   Consequences).

The rules of `internal/AGENTS.md`, Code, widen by one paragraph: an
ingest is one push per request through one held session; the tables
of one body share one column list; a body's failure is the whole
request's, found before the push; `internal/qdb` classifies, the
handler names no binding error.

### `internal/httpapi`, `rows.go`

`POST /api/v2/rows`, `requireBearer` and `withCompression` like every
route. The handler:

1. `Content-Type` must be `text/csv` (`mime.ParseMediaType`, a charset
   parameter neither honored nor checked); anything else is 415, with
   the two formats of Next 2 named in the detail once they exist.
2. The push parameters from the query string: `push-mode`,
   `deduplication-mode`, `deduplication-columns` (comma-separated),
   passed as words; `qdb` judges them.
3. The body is not read whole: `http.MaxBytesReader` at 64 MiB wraps
   `r.Body` and `IngestCSV` streams from it, so server memory is the
   parsed columns, never a second copy of the body. Every other body
   keeps `readBody`'s 1 MiB. A `*http.MaxBytesError` surfacing from
   the parse is 413.
4. The answer: 200, `application/json`,
   `{"rows": N, "tables": T, "parse_ms": P, "push_ms": Q}`, `push_ms`
   under `async` being the time until the push call returned.

The bearer rows, the 503s and the caller-gone row are ADR-0010's,
through `requireBearer` and `writeClusterError` unchanged; the rows
this route adds:

| Case                                                                             | Answer                                 |
| -------------------------------------------------------------------------------- | -------------------------------------- |
| the rows pushed                                                                  | 200, the four-field body               |
| a header-only body                                                               | 200, zero rows, zero tables, no push   |
| `Content-Type` not `text/csv`                                                    | 415                                    |
| body over 64 MiB                                                                 | 413                                    |
| a push mode or deduplication mode outside the vocabulary; a mode without columns | 400 (`qdb.ErrInvalidPushOptions`)      |
| `$table` or `$timestamp` missing from the header; a header name the table lacks  | 400 (`qdb.ErrInvalidRows`)             |
| a field that does not parse; an empty `$timestamp`; a short or long record       | 400 (`qdb.ErrInvalidRows`)             |
| two tables of the body with different typed columns                              | 400 (`qdb.ErrInvalidRows`)             |
| a `$table` the cluster does not know (`qdb.IsTableNotFound`)                     | 404, the whole request, nothing pushed |
| the cluster refused the push                                                     | 400, the binding's message             |

The rules of `internal/httpapi/AGENTS.md`, Handlers, gain the route's
contract in the shape of the table reader's paragraph.

## Tests

- `internal/qdbtest/table`: `CSV(tbl) []byte`, the table's rows in the
  CSV encoder's dialect (`encoding/csv`, header, LF): `$table`,
  `$timestamp`, then the columns, a null cell the empty field, a
  timestamp in the encoders' nine-digit text, a blob in base64. The
  one renderer the property tests and, later, `e2etool gen` share.
- `internal/qdbtest/table`: `CreateEmpty(t, c, tbl)`, the create half
  of `Create` (create, `RemoveOnCleanup`, no push), so a test that
  pushes through its own door does not copy it; `Create` stacks on
  it. `GenerateLike(rt, tbl)`, a table with a fresh name, `tbl`'s
  columns and its own drawn rows, so a body can carry several tables
  of one column list.
- `internal/qdb`, `ingest_test.go`, `package qdb_test` (the fixture
  imports `internal/qdb`, as `read_test.go`): one property. One to
  three tables of one column list (`Generate`, then `GenerateLike`),
  each `CreateEmpty`, their `CSV` bodies concatenated under one
  header, `IngestCSV` under a drawn push mode of `transactional` or
  `fast`, the result's `Rows` and `Tables` equal to what was written;
  every table read back through `Cluster.Read`, its batches
  concatenated per column, `table.Check` against the rows written. A
  drawn row count of zero exercises the no-push answer. One case in
  the same test pushes a body twice under `drop` on `$timestamp` and
  reads the rows back once.
- `internal/httpapi`, `rows_test.go`: one property, the reader test's
  shape: the same bodies over HTTP answer 200 and the four-field
  body with the row and table counts; read back through the reader
  endpoint equal to `EncodeStream` run directly, as `read_test.go`
  does. Plus the error rows of the table above, one request each,
  and one `async` push answered 200 without a read-back.

Every test bounds its sessions; `go test -p 1`.

## Commits

1. `feat(qdb): PushOptions carry the push mode and the deduplication mode with its columns; invalid ones fail before a lease`
2. `feat(qdb): IngestCSV streams records into per-table writer columns through one held session and pushes once`
3. `test(qdbtest): table.CSV renders a table's rows in the CSV encoder's dialect`
4. `test(qdbtest): CreateEmpty and GenerateLike, the blocks an ingest test stacks on`
5. `test(qdb): generated tables pushed as CSV read back as written`
6. `feat(httpapi): POST /api/v2/rows ingests a CSV body under push-mode and deduplication parameters`
7. `test(httpapi): a generated multi-table CSV body over HTTP reads back as written; the error rows`
8. `docs(agents): internal and httpapi AGENTS.md name the ingest rules`
9. `docs(log): the CSV ingest landed; ingest-csv-plan.md deleted`
10. Verify: push `sc-19567/rr-ingest-csv`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again.

Every commit builds and passes `make lint`; the Go tests run against
the live pair from `scripts/tests/setup/start-services.sh`.

## Open questions and recommendations

None; the owner settled the six of the seed report on 2026-09-23.

## Decision log (2026-09-23)

| Decision                                         | Why                                                                          | Rejected                                                          |
| ------------------------------------------------ | ---------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| One column list per body                         | the binding's writer rule; one kind of table per batch is how ingestion goes | one writer per column set, several pushes; an upstream change     |
| One `ingest.go` in `internal/qdb`                | the format is not QuasarDB's; the package's story is the writer              | `ingest_csv.go` per format; a decoder side in `internal/encoding` |
| One session for the whole request                | the lookup and the push through one lease; the budget bounds it              | a lease per column lookup and one for the push                    |
| An unknown `$table` is 404 and fails the request | the resource named does not exist; nothing partial is written                | 400 with the binding's detail; pushing the known tables           |
| A header-only body is 200 with zero rows         | nothing to write is not an error                                             | 400                                                               |
| The body streams from `MaxBytesReader`           | server memory is the parsed columns, not a copy of the body                  | `readBody` with a second cap                                      |
| The header's columns are the columns written     | a column the header lacks is null in every row without a second mechanism    | requiring every column of the table in the header                 |
