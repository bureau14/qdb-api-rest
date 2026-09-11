# ADR-0009: Arrow wire types for query results

Status: accepted
Date: 2026-09-08

## Context

`POST /api/v2/query` answers `Accept: application/vnd.apache.arrow.stream`
with an Arrow IPC stream (`docs/brief.md`, Data plane), and Flight SQL
(M5) ships the same record batches. The query core hands every consumer
the `arrow.RecordBatch` that `qdb-api-go` builds through
`qdb_query_arrow`: the C API lays the result out as Arrow columns without
a row-major intermediate, and the binding moves each column into Go
through the Arrow C data interface. The batch's schema fixes what every
consumer sees and what Flight SQL advertises; changing it later changes
every client.

## Decision

The wire schema is the batch the binding delivers, transmitted as-is.

| result column    | Arrow type                       |
| ---------------- | -------------------------------- |
| `int64`, a count | `Int64`                          |
| `double`         | `Float64`                        |
| `timestamp`      | `Timestamp(Nanosecond)`, no zone |
| `string`, symbol | `Utf8`                           |
| `blob`           | `Binary`                         |

1. **Every field is nullable**, whatever the rows hold, so a query's
   schema does not depend on its data. An all-null column keeps its
   table type with every slot null; the batch never carries an Arrow
   `Null` field.
2. **The schema passes through unread.** Field names, types,
   nullability and every metadata entry are the binding's to define and
   go on the wire unchanged: `Utf8` and `Binary` fields carry
   `max_width`. Nothing on the Arrow path interprets a column type; a
   type outside the table reaches the wire as whatever the binding
   says.
3. **Timestamps are naive**: `Timestamp(Nanosecond)` with no zone, the
   values nanoseconds since the Unix epoch.
4. **Zero-copy.** The C API's buffers are moved into Go, never copied by
   this repository; the batch owns them and its release frees them.
5. **`Utf8` and `Binary`, not the Large variants**: the C API exports
   int32 offsets.
6. **Column names verbatim, duplicates kept**, which Arrow allows.
7. **The IPC streaming format**, in record batches of a constant 65536
   rows, no in-format buffer compression. HTTP `Accept-Encoding`
   compression is the response's compression and is independent.
8. **A statement without a result set** encodes as a schema with no
   fields and no batches, a complete stream.

## Consequences

- Whoever receives a batch from the query core owns it and releases it
  exactly once; an encoder never releases what it is given.
- The IPC stream encoder is a loop over the batch's slices: the schema,
  the batches, the end-of-stream marker. Flight SQL ships the same
  batch.
- The row-rendering encoders (JSON, NDJSON, CSV) are the only code that
  must know a type to render a cell.
- The batch size bounds nothing on the server, which holds the whole
  result before the first byte (the C API is one-shot); it is the
  consumer's granularity, so it is a constant, not configuration.
- In-format compression (lz4, zstd) is an addition behind a request
  parameter, not a change to these types.

## Alternatives rejected

| Alternative                                             | Why not                                                                                                 |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| Building the batch here over the binding's columnar set | a second layout of what the C API already lays out as Arrow; one way to query                           |
| Relabel timestamps to `Timestamp(ns, "UTC")` here       | a relabel layer over a schema that is the binding's to define                                           |
| An all-null column as Arrow `Null`                      | schema changes with the data; Flight SQL clients cache schemas                                          |
| Stripping or rewriting field metadata                   | whatever the C API or the binding attaches is part of the contract they define                          |
| Rejecting a type outside the table with an error        | the Arrow path is a pass-through; the rendering encoders decide what they cannot render                 |
| `LargeUtf8` / `LargeBinary`                             | 64-bit offsets buy only arrays past 2 GiB; some JDBC and JS readers lack them                           |
| A distinct type for `count`                             | the C API answers an int64; clients never told a count from an int64 (`docs/brief.md`, v1 query)        |
| Dictionary-encoded symbols                              | the C API exports plain `Utf8`; an extra pass per query for a gain no consumer asked for                |
| Batch size as configuration                             | bounds nothing on the server; a knob nobody can set well                                                |
| In-format buffer compression in the first cut           | independent of HTTP compression, which the response already has; the negotiation mechanism is undecided |
