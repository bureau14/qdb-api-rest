# internal/encoding -- Agent Instructions

Scope: the wire encoders and decoders. Package-wide Go rules, logging
and the test fixtures: `internal/AGENTS.md`.

## The seam

- Every encoder implements `Encoder` over the Arrow record batches the
  core returns (`internal/AGENTS.md`, Code, the result rule) and knows
  only its media type and its bytes: it never flushes, never logs,
  never negotiates, and never releases a batch. `Encode` takes the
  query's one batch; `EncodeStream` takes a table read's sequence, each
  batch encoded before the next is pulled, every batch sharing the
  first's schema unchecked, an error step ending the encoding with its
  error. A buffered writer inside either is flushed once at the end; the
  HTTP flush is the handler's, and an error before the first flush
  leaves nothing on the wire.
- A format that is ingested also implements `Decoder`, the encoder
  inverted, in the same file: a body reads into one `model.TableBatch`
  per table, `$timestamp` first and then the data columns the body
  carried, in first-seen order, a table with no rows absent. `$table`
  routes a row and is never carried in a batch. Each table is typed the
  first time a row names it through the `model.SchemaOf` it is given,
  the reader's whole-table schema, so the batch carries the reader's
  types and the package declares no field of its own; the tables of one
  body must agree in every field's type, the decoder's check, since the
  Arrow writer checks one table at a time. The receiver owns the
  batches and releases each once; on error there are none. A body's
  fault is `ErrInvalidRows`, naming the row and the column, with the
  reader's cause kept in the chain; the lookup's error passes as is. The
  empty field is null in every type: the text wires cannot carry the
  empty string.
- Every encoder looks at the ctx once per `chunkRows` rows, one shared
  constant: the record batch size on the Arrow wire, the stride between
  ctx checks on the text wires.
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
  complete stream. A stream of batches opens on the first batch's
  schema and writes every batch in `chunkRows` slices.

## The text formats

- JSON, NDJSON and CSV are the only code that must know a type to
  write a cell. Each format writes every type the way its own readers
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
  `$table`), a nil batch `{"columns":[]}`, no trailing newline. A stream
  of batches is a top-level array of such results, one per batch, comma
  separated: column-oriented within a batch, bounded memory across them,
  and a cut stream is invalid JSON, so a truncated read is never taken
  for a complete one.
- NDJSON (`application/x-ndjson`) is one object per row, keys in column
  order, LF-terminated lines; no rows is an empty body. A stream of
  batches is the batches' lines appended.
- CSV (`text/csv`) is `encoding/csv`'s RFC 4180: a header row, LF, a
  field quoted only by the standard writer's rule. `int64` and
  `float64` (`strconv`, shortest round trip) as plain text; `utf8` as
  its own bytes; `binary` as standard base64; the empty field for null,
  for NaN and the infinities, and for the empty string alike. A nil
  batch is an empty body, no rows the header alone. A stream of batches
  is the first batch's header, then every batch's rows. Byte identity
  with `qdb_export`'s CSV is a non-concern.

## Tests

- The encoders' tests are in `encode_test.go`. Each wire family has one
  round trip against the live fixture: the test generates a table,
  queries it once, encodes the result, parses the output with the
  standard library and compares every cell with the batch it encoded.
  The fixture's `Check` has proven that batch to be the table written.
  Values the fixture does not generate are pinned byte for byte on one
  hand-built batch, and the stream path is pinned on that batch twice,
  as the two one-shot bodies joined; neither needs a cluster. The
  decoders' tests are in `decode_test.go`: one generative round trip
  over every codec draws tables through the fixture, encodes them as
  one body over `table.Body` and decodes them back to the batches it
  drew, without a cluster; the faults of a body are one table of cases.
  A test helper that parses a body is named `read*`, because `decode`
  is the package's word for its `Decoder`. `encoding_test.go` holds the
  helpers both files share.
