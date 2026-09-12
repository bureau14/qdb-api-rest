# internal/encoding -- Agent Instructions

Scope: the wire encoders. Package-wide Go rules, logging and the test
fixtures: `internal/AGENTS.md`.

## The seam

- Every encoder implements `Encoder` over the Arrow record batch the
  query core returns (`internal/AGENTS.md`, Code, the result rule) and
  knows only its media type and its bytes: it never flushes, never logs,
  never negotiates, and never releases the batch. A buffered writer
  inside `Encode` is flushed once at the end; the HTTP flush is the
  handler's.
- Every encoder looks at the ctx once per `chunkRows` rows, one shared
  constant: the record batch size on the Arrow wire, the stride between
  ctx checks on the rendered wires.
- The wire types are the binding's: `int64` (a count included),
  `float64`, naive `timestamp[ns]`, `utf8` and `binary` carrying
  `max_width` field metadata, every field nullable, an all-null column
  keeping its table type. An Arrow type outside the five is an encode
  error naming the column (`ErrUnsupportedType`), never a panic.

## Arrow

- The Arrow encoder transmits the batch's schema as-is, field metadata
  included, and interprets no column type. On the wire: the IPC
  streaming format in record batches of `chunkRows` rows, no in-format
  buffer compression (HTTP `Accept-Encoding` compression is independent
  of it); a nil batch is a schema with no fields and no batches, a
  complete stream.

## Rendering

- JSON, NDJSON and CSV are the only code that must know a type to
  render a cell. Each format renders every type the way its own readers
  expect, and the code for a format lives in that format's file only
  (`json.go`, `csv.go`); the two share nothing but the package's
  `ErrUnsupportedType` and the timestamp text, RFC 3339 in UTC with
  nine fixed fractional digits (`2026-06-11T00:00:00.000683000Z`),
  which is a fact about the wire, not about a format.
- JSON and NDJSON: `int64` a bare number; `float64` through
  `jsontext.AppendFloat`, the bytes `encoding/json` writes, NaN and the
  infinities `null`; `utf8` through `jsontext.AppendQuote`, invalid
  UTF-8 replaced by U+FFFD; `binary` a string of standard base64 with
  padding; the timestamp a string. Wire type names are QuasarDB's
  words: `int64`, `double`, `string`, `blob`, `timestamp`; a symbol
  answers as a `string` and a count as an `int64`.
- JSON (`application/json`) is
  `{"columns":[{"name":..,"type":..,"data":[..]},..]}`, keys in that
  order, no `tables` wrapper (the table a row came from is a column,
  `$table`), a nil batch `{"columns":[]}`, no trailing newline.
- NDJSON (`application/x-ndjson`) is one object per row, keys in column
  order, LF-terminated lines; no rows is an empty body.
- CSV (`text/csv`) is `encoding/csv`'s RFC 4180: a header row, LF, a
  field quoted only by the standard writer's rule. `int64` and
  `float64` (`strconv`, shortest round trip) as plain text; `utf8` as
  its own bytes; `binary` as standard base64; the empty field for null,
  for NaN and the infinities, and for the empty string alike. A nil
  batch is an empty body, no rows the header alone. Byte identity with
  `qdb_export`'s CSV is a non-concern.

## Tests

- One round trip per wire family against the live fixture
  (`arrow_test.go`, `render_test.go`): a generated table, queried once,
  encoded, decoded with the standard library, compared cell by cell
  with what was written. What the table fixture cannot write is pinned
  byte for byte on one hand-built batch in `render_test.go`.
