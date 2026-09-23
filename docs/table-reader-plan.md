# The table reader -- Plan

Status: approved. Scaffolding for one unit of work:
`GET /api/v2/tables/{name}/rows`, the whole table read through the
bulk reader and streamed batch by batch in every format. Deleted when
the reader lands; the rules move to the `AGENTS.md` of `internal`,
`internal/qdb`, `internal/encoding` and `internal/httpapi` first
(`docs/AGENTS.md`, Plans). What the owner fixed is in `docs/log.md`,
Current state, Next 1; this plan turns it into code. The vocabulary is
QuasarDB's: writer, reader, query. This endpoint is the reader's door,
as `POST /api/v2/rows` is the writer's.

## What the table reader is

A query buffers: `qdb_query_arrow` materializes the whole result before
the first byte, whatever its size (`docs/brief.md`, Data plane). The
bulk reader is a cursor: `Reader.Arrow()` (qdb-api-go `abadf4e`,
vendored) yields one caller-owned `arrow.RecordBatch` per fetch of at
most `batchSize` rows, valid after the next fetch, after `Close` and
after the handle is closed. The endpoint encodes each batch and sends
it before the next is fetched, so REST-server memory is one batch and
time to first byte is one fetch, whatever the table's size. That is
why a whole table is read here and never through `SELECT *`.

Verified against qdb-api-go `abadf4e` (2026-09-22):

- Schema: `$table` utf8 non-nullable, `$timestamp` timestamp[ns] naive
  non-nullable, then the data columns, nullable, in the query path's
  types (int64, float64, utf8 for string and symbol, binary,
  timestamp[ns]); no `max_width` metadata. With `WithColumns` the
  requested order exactly, `$table` and `$timestamp` only when named.
- An empty table yields nothing: no batch, no schema. `ColumnsInfo`
  is the schema's source then.
- A missing table fails `NewReader` with `ErrAliasNotFound`, before
  any byte is written; the handler answers 404 (status table below).
- One sequence per reader; breaking out is safe; `Close` after that
  is safe.

Two defects of the C API surface through this endpoint. Both are
filed against quasardb (sc-19829 the NUL byte, sc-19830 the symbol
columns; 2026-09-23); neither is worked around, because neither can
be:

- **A trailing NUL byte is dropped from a string cell**, only on the
  bulk reader's Arrow path (`qdb_bulk_reader_get_data_arrow`). The
  chunk path of the same reader and `qdb_query_arrow` return the byte.
  Arrow strings are length-delimited (an offsets buffer, never a
  terminator), so a string stored as `"a\x00"` arrives as `"a"` with
  length 1, and nothing downstream -- the binding, the encoders, the
  IPC writer, a client -- can tell it was longer; every hop reports
  exactly what `libqdb_api` produced. Only strings whose last byte is
  NUL are affected.
- **Two tables with symbol columns in one reader fail with invalid
  argument** (`ts.h` says the reader supports one table "at the
  moment"). This endpoint opens one reader over one table, so it is
  not hit; a multi-table read waits for the fix.

## Design

### `internal/qdb`

```go
type ReadOptions struct {
    Columns    []string  // nil: every column, $table and $timestamp first
    Start, End time.Time // both set or both zero; the binding judges the pair
    BatchRows  int       // rows per fetch; 0: readBatchRows (65536, the encoders' chunk)
}

func (c *Cluster) Read(ctx context.Context, u User, name string, o ReadOptions,
    sink func(iter.Seq2[arrow.RecordBatch, error]) error) error
```

`Read` runs inside `Call`, never retried: the session is held for the
whole stream, because every fetch needs the live handle, so a read
occupies one session of `u`'s pool for as long as the client reads.
The budget bounds concurrent reads like any other call. Inside `Call`:
`NewReader` (its error is returned before `sink` runs, so
`IsTableNotFound` and the binding's "invalid time range" reach the
handler before any byte); then `sink` over a sequence that yields the
reader's batches, and, when the reader yielded none, exactly one
schema-only batch built from `ColumnsInfo` in the reader's layout
(`$table`, `$timestamp`, then the columns, or the requested subset),
so every format answers the schema of an empty table. The sink owns
each batch and releases it; a batch it did not release is released
when the sequence ends. `Reader.Close` runs after `sink` returns.

The result rule of `internal/AGENTS.md` widens by one sentence: a read
is a sequence of such batches, handed to a sink while the session is
held.

### `internal/encoding`

`Encoder` gains one method:

```go
EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error
```

Every batch shares the first batch's schema, which the reader
guarantees; the encoders do not check it. An error step ends the
stream with that error; the handler decides what the client sees.
`Encode` stays as it is for the query. Per format:

- Arrow: the IPC writer opens on the first batch's schema, writes every
  batch (re-sliced to `chunkRows` as today), closes the stream.
- CSV: the header from the first batch, then every batch's rows.
- NDJSON: every batch's rows.
- JSON: `[`, one `{"columns":[..]}` object per batch in the query's
  exact shape, comma-separated, `]`. Column-oriented per batch, bounded
  memory, and a stream cut mid-way is invalid JSON, so a client cannot
  mistake a truncated read for a complete one. A sequence with zero
  batches would be `[]`; the endpoint never sends one (above).

### `internal/httpapi`

`GET /api/v2/tables/{name}/rows`, `requireBearer` and `withCompression`
like the query. The handler reads `columns` (comma-separated, exactly
the fields answered, the reader's rule) and `start`/`end`
(`time.RFC3339Nano`, which accepts the encoders' timestamp text),
negotiates `Accept` as the query does, and calls `Read`. Inside the
sink the handler sets `Content-Type`, wraps the writer in
`countingWriter` and runs `EncodeStream`. The status is decided before
the first byte. The bearer rows, the 503s and the caller-gone row are
ADR-0010's, through `requireBearer` and `writeClusterError` unchanged;
the rows this route adds:

| Case                                                    | Answer                                       |
| ------------------------------------------------------- | -------------------------------------------- |
| the table read                                          | 200, the negotiated `Content-Type`, streamed |
| the cluster knows no such table (`qdb.IsTableNotFound`) | 404, before any byte                         |
| `start` or `end` does not parse                         | 400                                          |
| one of `start`/`end` given, or `end` not after `start`  | 400, the binding's message                   |
| the stream failed with zero bytes out                   | 500                                          |
| the stream failed after the first byte                  | the stream cut, one warning line             |

Response compression through `Accept-Encoding` as on every route
(ADR-0012).

## Tests

- `internal/qdbtest/table`: the comparer `Check(t, tbl, rec)`, moved
  out of `internal/encoding`'s test file where it exists today as
  `checkColumn`: every column of `rec` against the column written, by
  name -- `$timestamp` against the index, `$table` equal to `tbl.Name`
  in every row, a data column by type, every validity bit, every
  value. The encoding tests switch to it; a shared test helper lives
  in a named fixture package, never in another package's test file.
- `internal/qdb`: one property, in `package qdb_test` because the
  fixture imports `internal/qdb` (the one black-box test of the
  package; `Cluster.Read` is exported, nothing is lost). A fixture
  table (`table.Generate`, `table.Create`), read with a drawn
  `BatchRows` of 1 to 16, its batches concatenated per column
  (`array.Concatenate`), passes `table.Check` against the rows written,
  no batch over `BatchRows`. A drawn row count of zero exercises the
  schema-only batch. The comparison is exact: the fixture draws
  `[a-zA-Z0-9]`, so no drawn string ends in NUL.
- `internal/qdb`: one canary pinning the NUL defect, the inverse of an
  expected failure. One row with `"x\x00"` pushed through the writer;
  the query batch answers `"x\x00"`, the reader answers `"x"`, and the
  test asserts that inequality. Its comment cites sc-19829 and says:
  when this test fails, the C API has been fixed; delete the test and
  let `drawText` draw NUL. So the fix is noticed by `go test`, not by
  someone re-reading a comment.
- `internal/encoding`: one test over hand-built batches, no cluster:
  for each encoder, `EncodeStream` over two batches equals the
  expected joining of the per-batch renderings (the IPC stream with
  two batches, the CSV with one header, the NDJSON lines appended, the
  JSON array); an error step surfaces the error.
- `internal/httpapi`: one property, the query test's shape: a fixture
  table read over HTTP in every media type answers 200, the encoder's
  `Content-Type`, and the bytes `EncodeStream` writes when run
  directly over `Cluster.Read`; plus the error rows of the table
  above.

Every test bounds its sessions; `go test -p 1`.

## Commits

1. `feat(qdb): Cluster.Read streams a table's batches through the bulk reader as the caller`
2. `feat(qdb): an empty table reads as one schema-only batch from ColumnsInfo`
3. `test(qdbtest): table.Check compares a record batch with the table written`
4. `test(qdb): a table read answers the rows written, batch by batch`
5. `test(qdb): the bulk reader drops a trailing NUL byte, pinned until sc-19829 lands`
6. `feat(encoding): EncodeStream renders a sequence of batches; Arrow and CSV`
7. `feat(encoding): NDJSON and JSON stream a sequence; JSON is an array of batches`
8. `test(encoding): a streamed sequence renders as its batches joined`
9. `feat(httpapi): GET /api/v2/tables/{name}/rows reads the table in the negotiated format`
10. `feat(httpapi): the table read takes start, end and columns`
11. `test(httpapi): a generated table read over HTTP equals the stream encoder run directly`
12. `test(httpapi): the table read's error rows`
13. `docs(agents): internal, encoding and httpapi AGENTS.md name the table reader rules`
14. `docs(log): the table reader landed; table-reader-plan.md deleted`
15. Verify: push `sc-19567/rr-table-reader`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again.

Every commit builds and passes `make lint`; the Go tests run against
the live pair from `scripts/tests/setup/start-services.sh`.

## Open questions and recommendations

None; the owner settled the four below on 2026-09-23.

## Decision log (2026-09-22)

| Decision                                         | Why                                                                   | Rejected                                        |
| ------------------------------------------------ | --------------------------------------------------------------------- | ----------------------------------------------- |
| One batch per fetch, encoded before the next     | bounded memory and early first byte, the reader's reason to exist     | `FetchAll`; a query over the table              |
| JSON is a top-level array of batch objects       | column-oriented, streams, a cut is invalid JSON                       | one object (cannot stream); a `batches` wrapper |
| The schema-only batch comes from `ColumnsInfo`   | the reader yields nothing for an empty table                          | answering `[]` / an empty body with no schema   |
| `EncodeStream` next to `Encode`                  | the query's one-shot path stays untouched                             | replacing `Encode` with a one-element sequence  |
| Range errors are the binding's                   | one home for the both-or-neither rule; the message reaches the caller | a second check in the handler                   |
| The NUL defect is pinned by a canary, not hidden | exact assertions everywhere; `go test` notices the upstream fix       | a tolerant comparator; a comment alone          |
| The vocabulary is reader, never dump             | QuasarDB's words: writer, reader, query                               | "dump" (2026-09-23, owner)                      |
