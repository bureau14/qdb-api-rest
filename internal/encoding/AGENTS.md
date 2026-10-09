# internal/encoding -- Agent Instructions

Scope: the wire encoders and decoders. The package-wide Go rules, the
logging rules and the test fixtures are in `internal/AGENTS.md`.

## The seam

- Every encoder implements `Encoder` over the Arrow record batches the
  core returns (`internal/AGENTS.md`, Code, the result rule). An encoder
  knows only its media type and its bytes. It does not flush, log or
  negotiate, and it does not release a batch. `Encode` takes the one
  batch of a query. `EncodeStream` takes the sequence of batches of a
  table read and encodes each batch before it pulls the next. It
  assumes every batch shares the first batch's schema and does not
  check it. An error step ends the encoding with that error. A buffered
  writer inside either method is flushed once at the end. The HTTP
  flush belongs to the handler, so an error before the first flush
  leaves nothing on the wire.
- A format that is ingested also implements `Decoder`, the inverse of
  its encoder, in the same file. The decoder reads a body into one
  `model.TableBatch` per table, with `$timestamp` first and then the
  data columns the body carried, in first-seen order. A table with no
  rows gets no batch. `$table` routes a row and is not carried in a
  batch. The decoder types each table the first time a row names it,
  through the `model.SchemaOf` it is given, which answers the reader's
  whole-table schema. The batch therefore carries the reader's types,
  and the package declares no field of its own. The tables of one body
  must agree in every field's type. The decoder checks this, because
  the Arrow writer checks one table at a time. The receiver owns the
  batches and releases each once. On error there are no batches. A
  fault in the body is `ErrInvalidRows`, which names the row and the
  column and keeps the reader's cause in the chain. The error of the
  schema lookup passes through as is. The empty field is null in every
  type, because the text wires cannot carry the empty string. A cell's
  text is the same on the CSV and NDJSON wires, so one `textAppender`
  in `encoding.go` parses it for both decoders.
- The NDJSON decoder reads one object per row. The first object's keys
  fix the column list, `$table` and `$timestamp` among them. A later
  object's absent key is null, and a key outside the list is
  `ErrInvalidRows`. A number token in an `int64` column must be an
  integer literal.
- The Arrow decoder reads an IPC stream of any batch count. The body's
  columns are picked by name, and a body column's type must equal the
  reader's type for that name. There is no cast, because the binding
  refuses what a cast would take. A dictionary-encoded or null `$table`
  is `ErrInvalidRows`.
- Every encoder checks the ctx once per `chunkRows` rows. The constant
  is shared: it is the record batch size on the Arrow wire and the
  stride between ctx checks on the text wires.
- The wire types are the binding's types: `int64` (a count included),
  `float64`, `timestamp[ns]` without a zone, and `utf8` and `binary`
  carrying `max_width` field metadata. Every field is nullable, and an
  all-null column keeps its table type. An Arrow type outside these five
  is an encode error that names the column (`ErrUnsupportedType`), not
  a panic.

## Arrow

- The Arrow encoder transmits the batch's schema as it is, field
  metadata included, and interprets no column type. The wire is the IPC
  streaming format in record batches of `chunkRows` rows, without
  in-format buffer compression (HTTP `Accept-Encoding` compression is
  independent of it). A nil batch encodes as a schema with no fields
  and no batches, which is a complete stream. A stream of batches opens
  on the first batch's schema and writes every batch in `chunkRows`
  slices.

## The text formats

- JSON, NDJSON and CSV are the only code that must know a type to write
  a cell. Each format writes every type the way its own readers expect,
  and the code for a format lives only in that format's file
  (`json.go`, `csv.go`). The two files share nothing but the package's
  `ErrUnsupportedType` and the timestamp text, which is RFC 3339 in UTC
  with nine fixed fractional digits (`2026-06-11T00:00:00.000683000Z`).
  The timestamp text is a fact about the wire, not about one format.
- JSON and NDJSON write `int64` as a bare number. They write `float64`
  through `jsontext.AppendFloat`, the bytes `encoding/json` writes, and
  NaN and the infinities as `null`. They write `utf8` through
  `jsontext.AppendQuote`, which replaces invalid UTF-8 by U+FFFD. They
  write `binary` as a string of standard base64 with padding, and the
  timestamp as a string. The wire type names are QuasarDB's words:
  `int64`, `double`, `string`, `blob`, `timestamp`. A symbol answers as
  a `string` and a count as an `int64`.
- JSON (`application/json`) is
  `{"columns":[{"name":..,"type":..,"data":[..]},..]}`, with the keys in
  that order and no `tables` wrapper. The table a row came from is a
  column, `$table`. A nil batch encodes as `{"columns":[]}`. There is no
  trailing newline. A stream of batches is a top-level array of such
  results, one per batch, comma separated. The body is column-oriented
  within a batch and memory stays bounded across batches. A cut stream
  is invalid JSON, so a truncated read cannot pass for a complete one.
- NDJSON (`application/x-ndjson`) is one object per row, with the keys
  in column order and LF-terminated lines. No rows encode as an empty
  body. A stream of batches is the lines of every batch appended.
- CSV (`text/csv`) is the RFC 4180 of `encoding/csv`: a header row, LF
  line endings, and a field quoted only when the standard writer's rule
  says so. `int64` and `float64` are written as plain text through
  `strconv`, with the shortest round trip. `utf8` is written as its own
  bytes and `binary` as standard base64. The empty field stands for
  null, for NaN and the infinities, and for the empty string alike. A
  nil batch encodes as an empty body, and no rows as the header alone.
  A stream of batches is the first batch's header, then the rows of
  every batch. Byte identity with the CSV of `qdb_export` is not a
  goal.

## Tests

- The encoders' tests are in `encode_test.go`. Each wire family has one
  round trip against the live fixture. The test generates a table,
  queries it once, encodes the result, parses the output with the
  standard library and compares every cell with the batch it encoded.
  The fixture's `Check` has proven that batch to be the table written.
  Values the fixture does not generate are pinned byte for byte on one
  hand-built batch, and the stream path is pinned on that batch twice,
  as the two one-shot bodies joined. Neither pin needs a cluster.
- The decoders' tests are in `decode_test.go`. One generative round
  trip over every codec draws tables through the fixture, encodes them
  as one body over `table.Body` and decodes them back to the batches it
  drew, without a cluster. The faults of a body are one table of cases
  per decoder.
- A test helper that parses a body is named `read*`, because `decode`
  is the package's word for its `Decoder`. `encoding_test.go` holds the
  helpers both files share.
