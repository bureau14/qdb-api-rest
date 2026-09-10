# Generated Table Fixture -- Plan

Status: draft. This document specifies the Go table fixture that every
test which needs rows in a live qdbd uses: the encoder round trips and the
format-equivalence property test of M1, the ingest/query roundtrip of M7
(`docs/brief.md`, Testing doctrine). It is a working document: decisions
are in the dated decision log at the end; progress is in `docs/log.md`.

## Purpose

One fixture, in one package, that turns a generated schema and generated
rows into a table in the test cluster and hands the test back exactly
what it wrote, so a test compares what came over the wire with what went
in. Today the Arrow round trip carries its own loader
(`internal/encoding/arrow_test.go`): a fixed six-column schema, rows
inserted one query-language `INSERT` at a time, values rendered as query
literals so the generator has to know the language's quoting rules. That
loader is private to one test, slow per row, and cannot be shared with
the three encoders that come next.

## Shape

The fixture is two functions, one pure and one that does I/O, so a test
reads as: draw a table, create it, query it, compare.

```go
package table // internal/qdbtest/table

// Table is a generated table: its schema, its index and its rows.
type Table struct {
    Name    string
    Columns []Column     // one to five, types drawn independently
    Index   []time.Time  // $timestamp, strictly ascending, never null
}

// Column is one generated column: its name, its type, and its cells as
// the writer's own ColumnData plus the validity mask the writer's
// sentinels encode.
type Column struct {
    Name     string
    Type     qdbapi.TsColumnType
    Symtable string        // symbol columns only: <table>_<column>
    Data     qdbapi.ColumnData
    Valid    []bool        // false marks a null cell
}

// Generate draws a table: one to five columns of independently drawn
// types, a row count, a null density, and the rows.
func Generate(rt *rapid.T) Table

// Create creates tbl in the cluster as the anonymous user, pushes its
// rows through the batch writer (fast push, no deduplication), and
// removes the table and its symtables on t's cleanup.
func Create(t T, c *qdb.Cluster, tbl Table)
```

`T` is the slice of `testing.TB` the fixture needs (`Helper`, `Fatalf`,
`Cleanup`), so `*testing.T` and `*rapid.T` both fit; `rapid.T` has all
three.

Types are an input the generator draws, one per column, from the six
column types; a test that wants one type in every column filters the
draw with `rapid.Filter` or uses `rapid.SampledFrom` on its own list and
passes the result to a `GenerateOf(rt, types...)` variant if that reads
better than filtering. The plan starts with `Generate` only; the variant
is added when the first test needs it.

## Cells and nulls

The writer takes dense value slices and encodes a null as the type's
sentinel: `math.MinInt64` for int64, `NaN` for double, `""` for string
and symbol, `nil` for blob
(`vendor/github.com/bureau14/qdb-api-go/v3/query_result.go`, MaskedArray).
The fixture's generators draw a value or a null per cell at the table's
null density, store the sentinel in `Data` and the truth in `Valid`, and
never draw a sentinel as a value: `MinInt64` and `NaN` are excluded from
the value ranges, and an empty string or blob is a value that the mask
alone tells from a null.

A timestamp data column cannot hold a null through the public writer: the
null timespec is `qdb_min_time` in both fields and the writer's
`ColumnDataTimestamp` exposes no way to set it from outside the binding
(`vendor/github.com/bureau14/qdb-api-go/v3/column_data.go`,
`NewColumnDataTimestamp`). Timestamp data columns are therefore dense in
the fixture. A null-aware constructor is an upstream request
(`internal/AGENTS.md`, Building: no local patch); until it lands, the
null-timestamp cell has no coverage in the Go tests.

The index is the `$timestamp` column: a fixed start, a drawn step, strictly
ascending so every row survives, never null. This mirrors the Python
fixture, whose index is a `date_range` with a step and whose data columns
carry the nulls (`~/git/qdb-api-python/tests/conftest.py`,
`_array_with_index_and_table`).

Value ranges: int64 over the full range minus the sentinel; double over
finite binary64 minus `NaN`; string and symbol share one generator over
`[a-zA-Z0-9]{1,16}`, since the binding carries both as string values and
symbol values become symtable entries the server rejects arbitrary bytes
in; blob over any bytes; timestamp over the nanosecond range the result
set accepts.

## Names and cleanup

A table is named `qdbtest_<16 lowercase letters>`, drawn from the test's
own `rapid.T` so a failing case replays with the same name. A symbol
column's symtable is `<table>_<column>`. `Create` registers one cleanup
that removes the table and every symtable, through the same cluster, and
reports a removal error through `t.Errorf` so a leak is visible and never
fatal. Inside `rapid.Check` the cleanup runs at the end of each iteration,
so the cluster holds one fixture table per running test at any time.

## Where it lives and what it needs from the cluster

`internal/qdbtest` is the qdbd fixture's one home (`internal/AGENTS.md`,
Tests). The table fixture takes a `*qdb.Cluster`, so it imports
`internal/qdb`; `internal/qdb`'s own white-box tests import `qdbtest`
for the URIs and `Require`, and Go rejects that cycle in test. The table
fixture is therefore the subpackage `internal/qdbtest/table`, and
`internal/qdbtest` stays cgo-free and importable from everywhere.

The cluster's `Session` is the only door to the binding's handle
(`internal/qdb/cluster.go`, Session) and today runs one operation,
`fetch`. The fixture needs three more, each one C API operation, each
part of the surface the server needs anyway:

- `CreateTable(name string, shard time.Duration, cols ...qdbapi.TsColumnInfo) error`
  (M6's create endpoint);
- `RemoveTable(name string) error` (M6);
- `Push(w *qdbapi.Writer) error` (M7's ingest).

They land in `internal/qdb` as production code with the fixture as their
first caller, so the fixture never dials around the cluster and every
session it uses is budgeted and pooled like any other. Removing a
symtable is `RemoveTable` on the symtable's alias: both are entries.

## What the tests then look like

The Arrow round trip draws a table, creates it, queries `SELECT *`,
encodes, decodes, and compares the decoded columns with `Table.Columns`:
type by the type map, validity by `Valid`, values by `Data`. The result
set stops being the oracle; it is the encoder's input, and the wire is
compared with what was written. The format-equivalence test draws one
table and decodes every format against the same `Table`. The
ingest/query roundtrip (M7) pushes a `Table` through the endpoint instead
of the writer and compares the same way.

## Verified facts

- 2026-09-10: `rapid.T` implements `Cleanup`, `Helper` and `Fatalf`
  (`vendor/pgregory.net/rapid/engine.go`).
- 2026-09-10: `Writer.SetTable` requires every table in one writer to
  share a schema, and truncate pushes are unimplemented
  (`vendor/github.com/bureau14/qdb-api-go/v3/writer.go`,
  `writer_table.go`); the fixture uses one writer per table and never
  truncates.
- 2026-09-10: the binding's own generators (`genWriterData*`,
  `genPopulatedTablesOfType`) are unexported
  (`vendor/github.com/bureau14/qdb-api-go/v3/test_utils.go`); they are
  the pattern, not code to reuse.

## Open questions

None.

## Decision log (2026-09-10)

| Decision                                                 | Why                                                                                                   | Rejected                                                                        |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Plan before code                                         | owner: every remaining M1 unit gets a plan                                                            | straight to code                                                                |
| The fixture takes a `*qdb.Cluster`                       | owner: budgeted, pooled sessions like every other caller                                              | the fixture dials its own binding session                                       |
| Random names, `t.Cleanup` removal, per iteration         | owner; the Go API's `newAllColumnsSchema` idiom; no `purge_all` here                                  | no cleanup (Python idiom); fixed names dropped and recreated (Arrow loader)     |
| Column types are a generated input, one to five columns  | owner: "the data type is a generated input parameter"; multiple types if it stays simple              | strictly one column of one type per table                                       |
| `$timestamp` is a dense ascending index, never generated | owner: the index is special, required non-null, generated with a step (Python)                        | drawing the index like a data column                                            |
| The fixture hands back the generated data                | owner: easier to validate; the wire is compared with what was written                                 | decoupled generation and creation with no returned data                         |
| Batch writer, fast push, no deduplication                | owner; one push per table; fast push is read-consistent for the query that follows                    | `INSERT` per row; async push (rows lag the query)                               |
| Subpackage `internal/qdbtest/table`                      | `internal/qdb` tests import `qdbtest`; a `qdbtest` that imports `internal/qdb` is a test import cycle | one package; moving the cluster tests out of package `qdb` (they are white-box) |
| `Session` gains `CreateTable`, `RemoveTable`, `Push`     | one C API operation each; M6 and M7 need them; the fixture is their first caller                      | a test-only handle accessor on `Session`                                        |
