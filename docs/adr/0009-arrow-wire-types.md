# ADR-0009: Arrow wire types for query results

Status: accepted
Date: 2026-09-08

## Context

`POST /api/v2/query` answers `Accept: application/vnd.apache.arrow.stream`
with an Arrow IPC stream (`docs/brief.md`, Data plane), and Flight SQL
(M5) ships the same record batches. The query core hands the encoder a
Go-owned `QueryResultSet` from `qdb-api-go`, whose columns are already
laid out as Arrow buffers: a validity mask (LSB-first, 1 = valid), dense
values for the fixed-width types, and one cell buffer plus `n+1` int32
offsets for strings and blobs. The Arrow types chosen for the wire fix
what every consumer sees and what Flight SQL advertises; changing them
later changes every client.

## Decision

| result column          | Arrow type                     |
| ---------------------- | ------------------------------ |
| `QueryColumnInt64`     | `Int64`                        |
| `QueryColumnDouble`    | `Float64`                      |
| `QueryColumnTimestamp` | `Timestamp(Nanosecond, "UTC")` |
| `QueryColumnString`    | `Utf8`                         |
| `QueryColumnBlob`      | `Binary`                       |
| `QueryColumnNull`      | `Null`                         |

1. **Every field is nullable**, whatever the rows hold, so a query's
   schema does not depend on its data.
2. **Zero-copy from the result set.** Every Arrow buffer is memory the
   result set owns: its mask bytes, values, offsets and cell bytes are
   wrapped, never copied. The null sentinels the binding stores in null
   slots stay in the values buffer; a reader ignores the bytes of a slot
   whose validity bit is clear.
3. **`Utf8` and `Binary`, not the Large variants.** The binding caps a
   column at `math.MaxInt32` bytes, so int32 offsets cannot overflow.
4. **A count is an `Int64`.** The binding folds `count(...)` cells into
   `QueryColumnInt64`; the C API's count tag is not on the wire.
5. **Symbols are `Utf8`**, no dictionary encoding.
6. **Column names verbatim, duplicates kept**, which Arrow allows.
7. **The IPC streaming format**, in record batches of a constant 65536
   rows, no in-format buffer compression. HTTP `Accept-Encoding`
   compression is the response's compression and is independent.
8. **A statement without a result set** encodes as a schema with no
   fields and no batches, a complete stream.

## Consequences

- One `Record(rs)` builds the record Flight SQL reuses; the IPC stream
  encoder is a thin loop over its slices.
- The batch size bounds nothing on the server, which holds the whole
  result before the first byte (the C API is one-shot); it is the
  consumer's granularity, so it is a constant, not configuration.
- A result whose string or blob column exceeds 2 GiB is rejected by the
  binding before encoding starts, never truncated on the wire.
- In-format compression (lz4, zstd) is an addition behind a request
  parameter, not a change to these types.

## Alternatives rejected

| Alternative                                   | Why not                                                                                                    |
| --------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `LargeUtf8` / `LargeBinary`                   | 64-bit offsets buy only arrays past 2 GiB, which the binding refuses; some JDBC and JS readers lack them   |
| Microsecond or zone-less timestamps           | loses the nanoseconds the database stores; the result set is already nanoseconds UTC                       |
| Nullability from the null count               | schema changes with the data; Flight SQL clients cache schemas                                             |
| Arrow builders over the column values         | a second copy of every string and blob byte per query                                                      |
| A distinct type for `count`                   | the tag does not survive the binding; clients never told a count from an int64 (`docs/brief.md`, v1 query) |
| Dictionary-encoded symbols                    | the result set has no dictionary; an extra pass per query for a gain no consumer asked for                 |
| Batch size as configuration                   | bounds nothing on the server; a knob nobody can set well                                                   |
| In-format buffer compression in the first cut | independent of HTTP compression, which the response already has; the negotiation mechanism is undecided    |
