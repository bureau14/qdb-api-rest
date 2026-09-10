# Arrow Query Path -- Plan

Status: approved. The working plan for one M1 unit: the query core hands
every consumer the Arrow record batch the binding produces directly from
the C API, and the columnar result set leaves this repository. Verified
facts land here dated; progress is in `docs/log.md`; the document is
deleted when the unit lands (`docs/AGENTS.md`, Plans). The wire contract
is ADR-0009, rewritten in place (decision log below).

## Purpose

`qdb-api-go` on upstream `master` (`cabebb6`, "QDB-19753 - Arrow query
API") adds `Query.FetchArrow()`: the query runs through `qdb_query_arrow`,
which builds Arrow columns inside the C API without a row-major
`qdb_query_result_t`, and the binding moves each column into a Go
`arrow.RecordBatch` through the Arrow C data interface, zero-copy.

Outcome of the unit: `Cluster.Query` returns an `arrow.RecordBatch`, the
`Encoder` seam takes one, the Arrow encoder is the batch loop and nothing
else, and no code in this repository calls `Fetch` or names a
`QueryResultSet`. The JSON, NDJSON and CSV encoders that follow
(`docs/log.md`, Next) are written over the batch.

## What the binding delivers (verified 2026-09-10)

Against the live fixture (`internal/qdbtest`), through upstream
`cabebb6`, C API 3.15.0:

- Types on the batch: `int64`, `float64`, `timestamp[ns]`, `utf8`,
  `binary`. A count aggregate is an `int64` column. Every field is
  nullable.
- Timestamps are naive, `timestamp[ns]` with no zone. The values are
  nanoseconds since the Unix epoch in UTC whatever the process zone
  (checked under `TZ=Asia/Bangkok` against the columnar path's values).
- An all-null column keeps its table type with every slot null; the
  batch never carries an Arrow `Null` field. A result with zero rows is a
  zero-row batch with every column typed.
- `utf8` and `binary` fields carry `max_width` field metadata written by
  the C API (`"0"` when the column holds no bytes).
- A statement without a result set (DDL) yields a nil batch and no error.
- A batch may come together with an error on partial failure; the batch
  must be released either way.
- The batch holds no reference to the handle and outlives it; the
  session returns to its pool before the caller reads a row.
- The C API's column struct comment lists DATE64 as a possible type
  (`qdb/include/qdb/ts.h`, `qdb_arrow_column_t`); no query in the fixture
  produces one. The struct is marked as still under development.
- The bump brings arrow-go v18.7.0 to v18.8.0 and vendors `arrow/cdata`.

## Interfaces

Ownership rule: whoever receives a batch from `Cluster.Query` owns it and
calls `Release` exactly once; an encoder never releases what it is given.
A nil batch is a statement without a result set.

```go
// internal/qdb
func (s *Session) fetch(q string) (arrow.RecordBatch, error)
// FetchArrow. An error means no result: a batch handed back with an
// error is released here and nil is returned.

func (c *Cluster) Query(ctx context.Context, u User, q string, opts ...CallOption) (arrow.RecordBatch, error)
// The batch outlives the session, which is back in its pool on return.

func (c *Cluster) Probe(ctx context.Context) error
// Unchanged signature; the readiness batch is released at once.

// internal/encoding
type Encoder interface {
    ContentType() string
    Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error
}

func (Arrow) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error
func writeArrow(ctx context.Context, w io.Writer, rec arrow.RecordBatch, batchRows int64) error
// Schema, slices of batchRows rows, end-of-stream marker; a nil rec is
// a schema with no fields and no batches.

// internal/encoding, test helpers
func run(t failer, c *qdb.Cluster, q string) arrow.RecordBatch
// Released by the test at the end of the rapid iteration.
```

Deleted with the unit: `timestampNanosUTC`, `validity`, `fixedWidth`,
`variableWidth`, `arrowColumn` and `Record` in `internal/encoding/arrow.go`.

## Approach

### `internal/qdb`

`Session.fetch` calls `FetchArrow`. `Cluster.Query` returns the batch and,
on any error, nil; a partial batch is released before the error is
returned. `Probe` runs the readiness query the same way. The `Session`
comment states what a caller outside the package may hold: one batch,
whose buffers are C-allocated and freed by its release callback.

### `internal/encoding`

`Encoder.Encode` takes the batch. The Arrow encoder is `writeArrow` over
it: schema, slices of 65536 rows, the end-of-stream marker. Nothing in
this unit interprets a column type: the Arrow encoder is a pass-through,
and an unlisted type reaches the wire as whatever the binding says. The
row-rendering encoders written later are the first code that must know a
type to render a cell.

### Tests

`arrow_test.go` keeps its shape: a generated table through
`qdbtest/table`, a batch size small enough that rows span batches, the
IPC reader, cell-by-cell comparison with the table that was written. The
expected schema is the batch's own; an all-null column is compared by
validity, since it carries the table type; the timestamp check expects
`timestamp[ns]` without a zone.

### Documents

- ADR-0009, rewritten in place: the wire schema is the batch the binding
  delivers; timestamps pass through naive; an all-null column keeps its
  table type; `max_width` passes through; "zero-copy" means the C API's
  buffers moved into Go, never copied by this repository.
- `internal/AGENTS.md`: the result rule names the batch and its release;
  the encoder seam names the batch.
- `docs/brief.md`: the materialization paragraph names `qdb_query_arrow`
  as the path in use; the one-shot constraint (no cursor) stays.
- `docs/log.md`: Next item 1 reads "over the batch".

## Commits

Each commit builds, passes lint and its tests on its own. The seam and
the query change are separable because the old `Record` builds a batch
from a result set until the query returns one itself.

1. `docs(adr): ADR-0009 names the batch the binding delivers as the wire schema`
2. `build(vendor): bump qdb-api-go to master with the Arrow query API`
3. `refactor(encoding): the Encoder seam and the Arrow writer take a record batch`
   (`Record(rs)` feeds the test in between)
4. `refactor(qdb): Query and Probe fetch the binding's Arrow record batch`
   (`cluster.go`, `cluster_test.go`; the round trip reads the batch and
   expects its semantics)
5. `refactor(encoding): the column map over the result set leaves`
6. `docs(internal): the result rule names the batch and its release`
7. `docs(brief): the unwrapped Arrow path is the query path`
8. `docs(log): the remaining encoders sit over the batch`
9. `docs(log): arrow-query-plan.md deleted; facts moved to ADR-0009 and internal/AGENTS.md`

## Decision log (2026-09-10)

| Decision                                    | Why                                                                                      | Rejected                                                                    |
| ------------------------------------------- | ---------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| ADR-0009 rewritten in place, not superseded | owner decision; the ADR describes one standing contract, and the contract moved          | ADR-0010 superseding 0009                                                   |
| Timestamps pass through naive               | owner decision; the values are UTC nanoseconds regardless; no relabel layer in this repo | relabel to `Timestamp(ns, "UTC")` in `internal/encoding`; wait for upstream |
| An all-null column keeps its table type     | owner decision; the binding's behaviour; a schema that does not change with the data     | mapping to Arrow `Null` as before                                           |
| `max_width` metadata passes through         | owner decision; non-concern for now                                                      | stripping it with a schema rewrite                                          |
| No column-type interpretation in this unit  | owner decision; the Arrow encoder is a pass-through; rendering encoders decide later     | rejecting types outside the six with an error                               |
| The `QueryResultSet` path leaves entirely   | owner decision; one way to query                                                         | keeping `Fetch` for the probe                                               |
| Any error means no result                   | owner decision; a partial batch is released and dropped                                  | surfacing partial rows                                                      |
