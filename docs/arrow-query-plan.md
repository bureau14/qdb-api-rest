# Arrow Query Path -- Plan

Status: approved. This document is the working plan for one M1 unit:
the query core hands every consumer the Arrow record batch the binding
now produces directly from the C API, and the columnar result set leaves
this repository. It is scaffolding: verified facts land here dated,
progress is in `docs/log.md`, and the document is deleted when the unit
lands (`docs/AGENTS.md`, Plans). The wire contract it changes is
ADR-0009, rewritten in place under the owner's decision below.

## Purpose

`qdb-api-go` on upstream `master` (`cabebb6`, "QDB-19753 - Arrow query
API") adds `Query.FetchArrow()`: the query runs through `qdb_query_arrow`,
which builds Arrow columns inside the C API without the row-major
`qdb_query_result_t`, and the binding moves each column into a Go
`arrow.RecordBatch` through the Arrow C data interface, zero-copy. The
REST server's Arrow encoder already builds exactly such a batch by hand
from the columnar `QueryResultSet`; with the binding producing the batch,
that hand-built mapping is dead weight and the seam every encoder shares
is the batch itself.

Outcome of the unit: `Cluster.Query` returns an `arrow.RecordBatch`, the
`Encoder` seam takes one, the Arrow encoder is the batch loop and nothing
else, and no code in this repository calls `Fetch` or names a
`QueryResultSet`. The JSON, NDJSON and CSV encoders that follow
(`docs/log.md`, Next) are written over the batch from the start.

## What the binding delivers (verified 2026-09-10)

Against the live fixture (`internal/qdbtest`), through upstream
`cabebb6`, C API 3.15.0:

- Types on the batch: `int64`, `float64`, `timestamp[ns]`, `utf8`,
  `binary`. A count aggregate is an `int64` column. Every field is
  nullable.
- Timestamps are naive: `timestamp[ns]` with no zone. The values are
  nanoseconds since the Unix epoch, UTC, identical to the columnar
  path's values under `TZ=Asia/Bangkok`; the C API's own comment about
  shifting into the handle's zone is not observable on a default handle.
- An all-null column keeps its table type with every slot null; the
  batch never carries an Arrow `Null` field. A result with zero rows is a
  zero-row batch with every column typed.
- `utf8` and `binary` fields carry `max_width` field metadata written by
  the C API (`"0"` when the column holds no bytes).
- A statement without a result set (DDL) yields a nil batch and no error.
- A batch may come together with an error on partial failure; the batch
  must be released either way.
- The batch holds no reference to the handle and outlives it: the
  session returns to its pool before the caller reads a row, as today.
- The C API's column struct comment lists DATE64 as a possible type
  (`qdb/include/qdb/ts.h`, `qdb_arrow_column_t`); no query in the fixture
  produces one. The struct is marked as still under development.
- The bump brings arrow-go v18.7.0 to v18.8.0 and vendors `arrow/cdata`.

## Approach

### `internal/qdb`

`Session.fetch` becomes `FetchArrow`; `Cluster.Query` returns the batch;
any error means no result, and a partial batch is released before the
error is returned. `Probe` runs the readiness query the same way and
releases the batch at once. The `Session` comment says what is true now:
the batch's buffers are C-allocated and freed by its release callback,
so a caller outside this package owns exactly one thing with a `Release`
obligation, the batch.

### `internal/encoding`

`Encoder.Encode` takes `arrow.RecordBatch`; nil is a statement without a
result set and encodes as no fields and no rows. The Arrow encoder
becomes `writeArrow` over the batch: schema, slices of 65536 rows, the
end-of-stream marker. The column map, the buffer wrappers and `Record`
are deleted. Nothing in this unit interprets a column type: the Arrow
encoder is a pass-through and an unlisted type reaches the wire as
whatever the binding says. The row-rendering encoders written later are
the first code that must know a type to render a cell.

### Tests

`arrow_test.go` keeps its shape: a generated table through
`qdbtest/table`, a batch size small enough that rows span batches, the
IPC reader, cell-by-cell comparison with the table that was written. The
expected schema is the batch's own; an all-null column is compared by
validity, since it now carries the table type; the timestamp check
expects `timestamp[ns]` without a zone.

### Documents

- ADR-0009 is rewritten in place: the wire schema is the batch the
  binding delivers; timestamps pass through naive; an all-null column
  keeps its table type; `max_width` passes through; "zero-copy" means
  the C API's buffers moved into Go, never copied by this repository.
- `internal/AGENTS.md`: the result rule names the batch and its release,
  the encoder seam names the batch.
- `docs/brief.md`: the materialization paragraph names `qdb_query_arrow`
  as the path in use; the one-shot constraint (no cursor) stays.
- `docs/log.md`: Next item 1 reads "over the batch"; the upstream asks
  gain the zone question if the owner wants it filed.

## Decision log (2026-09-10)

| Decision                                           | Why                                                                                      | Rejected                                                                    |
| -------------------------------------------------- | ---------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| ADR-0009 rewritten in place, not superseded        | owner decision; the ADR describes one standing contract, and the contract moved          | ADR-0010 superseding 0009                                                   |
| Timestamps pass through naive                      | owner decision; the values are UTC nanoseconds regardless; no relabel layer in this repo | relabel to `Timestamp(ns, "UTC")` in `internal/encoding`; wait for upstream |
| An all-null column keeps its table type            | owner decision; the binding's behaviour; a schema that does not change with the data     | mapping to Arrow `Null` as before                                           |
| `max_width` metadata passes through                | owner decision; non-concern for now                                                      | stripping it with a schema rewrite                                          |
| No column-type interpretation in this unit         | owner decision; the Arrow encoder is a pass-through; rendering encoders decide later     | rejecting types outside the six with an error                               |
| The `QueryResultSet` path leaves entirely          | owner decision; one way to query                                                         | keeping `Fetch` for the probe                                               |
| Any error means no result                          | owner decision; a partial batch is released and dropped                                  | surfacing partial rows                                                      |
| The seam and the qdb change may land as one commit | the seam type cannot compile against the old `Query` in between                          | a shim commit                                                               |
