# internal/encoding -- Agent Instructions

Scope: the wire encoders and decoders. The package-wide Go rules, the
logging rules and the test fixtures are in `internal/AGENTS.md`.

## The seam

- Every encoder implements `Encoder` over the Arrow record batches the
  core returns (`internal/AGENTS.md`, Code, the result rule). An encoder
  knows only its media type and its bytes. It does not flush, log or
  negotiate, and it does not release a batch. `EncodeStream` is the one
  entry point; the e2e tool and the round trip's oracle write a
  generated body as a sequence of one batch. It encodes each batch of
  the sequence before it pulls the next. It assumes every batch shares
  the first batch's schema and does not check it, and it assumes the
  sequence is never empty, because every producer stands in an empty
  batch of its schema for a result of no rows. An error element ends
  the encoding with that error. A buffered writer inside it is flushed
  once at the end. The HTTP flush belongs to the handler, so an error
  before the first flush leaves nothing on the wire.
- A format that is ingested also implements `Decoder`, the inverse of
  its encoder, in the same file. The decoder reads a body into one
  `model.TableBatch` per table, with `$timestamp` first and then the
  data columns the body carried, in first-seen order. A table with no
  rows gets no batch. `$table` routes a row and is not carried in a
  batch, and a row whose `$table` is null or empty is `ErrInvalidRows`.
  The decoder types each table the first time a row names it,
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
- The NDJSON decoder reads one object per row through the standard
  library, into a map of raw values, so the library parses and the
  decoder only routes. The first object's keys fix the column list,
  `$table` and `$timestamp` among them, and the batch carries those
  data columns in the table's order, because a JSON object's members
  carry no order of their own, and the JSON decoder takes the same
  order. A later object's absent key is null, and
  a key outside the list is `ErrInvalidRows`. A number in an `int64`
  column must be an integer literal.
- The JSON decoder reads one bare result or the array of results (what
  every route answers). It reads one result at a time,
  whole, through the standard library, and then routes it row by row,
  because `$table` is a column at any position. The first result fixes
  the column list, `$table` and `$timestamp` among them, and a later
  result names the same columns. Within a result a
  column name appears once and every column has the same number of
  cells. A member a result does not declare and a value after the body
  are `ErrInvalidRows`. The `type` member is not read, because the
  reader's schema types the batch. A cell reads as an NDJSON cell,
  through the `jsonTable` both decoders share.
- The Arrow decoder reads an IPC stream of any batch count. The body's
  columns are picked by name, and a body column's type must equal the
  reader's type for that name. There is no cast, because the binding
  refuses what a cast would take. A dictionary-encoded or null `$table`
  is `ErrInvalidRows`.
- Every encoder checks the ctx once per batch, and a batch is at most
  `model.BatchRows` rows, the binary's batch size. The decoders check it
  once per `model.BatchRows` rows of the body they read, because a body
  arrives in no batches.
- The query and the table reader answer the binding's five types:
  `int64` (a count included), `float64`, `timestamp[ns]` without a zone,
  and `utf8` and `binary` carrying `max_width` field metadata. The sql
  endpoint answers DuckDB's types. The text encoders write those five
  and DuckDB's scalars: every signed and unsigned integer width,
  `float32`, `decimal128`, `bool`, `date32` and `time64`, and a
  timestamp of any unit. They write only the types a producer answers,
  so the other Arrow scalars (`float16`, `decimal256`, `date64`, a
  duration) have no case. Every field is nullable, and an all-null
  column keeps its type. Any other type is an encode error that names
  the column (`ErrUnsupportedType`), and the encoder does not panic on
  it.

## Arrow

- The Arrow encoder transmits the batch's schema as it is, field
  metadata included, and interprets no column type. The wire is the IPC
  streaming format in the record batches the producer hands it, which
  `Cluster.Query` and the bulk reader bound at `model.BatchRows` rows and
  DuckDB at one vector, without in-format buffer compression (HTTP
  `Accept-Encoding` compression is independent of it). A batch of no
  rows is not written, so a result of no rows is the schema and the
  marker alone, which is a complete stream. A stream of batches opens on
  the first batch's schema.

## The text formats

- JSON, NDJSON and CSV are the only code that must know a type to write
  a cell. Each format writes every type the way its own readers expect,
  and the code for a format lives only in that format's file (`json.go`,
  `csv.go`). The two files share nothing but the package's
  `ErrUnsupportedType`, the number constraints (`signed`, `unsigned`,
  `floating`, `floatBits`) and three texts. The timestamp text is RFC
  3339 in UTC with nine fixed fractional digits
  (`2026-06-11T00:00:00.000683000Z`). The date text is the ISO 8601
  calendar date (`2026-06-11`). The time-of-day text has nine fixed
  fractional digits, and its hour runs to 24 (`24:00:00.000000000`).
  These texts are facts about the wire, not about one format.
- JSON and NDJSON write `int64` and every other integer width as a bare
  number, and a `decimal128` as its exact text as a bare number. A
  client that parses JSON numbers as doubles loses digits past 2^53 in
  an `int64`, a `uint64` or a decimal, and a client that needs every
  digit uses the Arrow format. They write `float32` and `float64`
  through `jsontext.AppendFloat` at the column's own width, the bytes
  `encoding/json` writes, and NaN and the infinities as `null`. They
  write a `bool` as `true` or `false`. They write `utf8` through
  `jsontext.AppendQuote`, which replaces invalid UTF-8 by U+FFFD. They
  write `binary` as a string of standard base64 with padding, and the
  timestamp, the date and the time of day as strings in their texts. The
  type word is the Arrow type's name (`DataType.Name()`): `int8` to
  `int64`, `uint8` to `uint64`, `float32`, `float64`, `decimal`, `bool`,
  `date32`, `time64`, `utf8`, `binary`, `timestamp`. A symbol answers as
  `utf8` and a count as `int64`. The word is the name the Arrow wire
  reports, so a result column's word differs from the table schema's
  word for the same column (`float64` against `double`). QuasarDB's
  words were rejected, because they would give the narrow and unsigned
  integers a width they lack. DuckDB's SQL names were rejected, because
  the encoders do not know DuckDB and the query endpoint never answers
  those names.
- JSON (`application/json`) is
  `{"columns":[{"name":..,"type":..,"data":[..]},..]}`, with the keys in
  that order and no `tables` wrapper. The table a row came from is a
  column, `$table`. There is no trailing newline. A stream of batches is
  a top-level array of such results, one per batch, comma separated, so
  a native result larger than one batch is several results. The body is column-oriented
  within a batch and memory stays bounded across batches. A cut stream
  is invalid JSON, so a truncated read cannot pass for a complete one.
- NDJSON (`application/x-ndjson`) is one object per row, with the keys
  in column order and LF-terminated lines. No rows encode as an empty
  body. A stream of batches is the lines of every batch appended.
- CSV (`text/csv`) is the RFC 4180 of `encoding/csv`: a header row, LF
  line endings, and a field quoted only when the standard writer's rule
  says so. Every integer width, `float32` and `float64` are written as
  plain text through `strconv`, a float with its own width's shortest
  round trip. A decimal is written as its exact text. A `bool` is
  written as `true` or `false`. The date and the time of day are written
  in their texts. `utf8` is written as its own bytes and `binary` as
  standard base64. The empty field stands for null, for NaN and the
  infinities, and for the empty string alike. No rows encode as the
  header alone. A stream of batches is
  the first batch's header, then the rows of every batch. Byte identity
  with the CSV of `qdb_export` is not a goal.

## Tests

- The encoders' tests are in `encode_test.go`. One round trip,
  `TestRoundTrip`, runs against the live fixture. It generates a table,
  queries it once, and appends drawn columns of the DuckDB scalars,
  because those types reach the encoders only through DuckDB and the
  table fixture writes QuasarDB tables. It encodes the batch in every
  format, parses the output with the standard library and compares every
  cell with the batch it encoded. The fixture's `Check` has proven the
  queried part to be the table written. Values the fixture does not
  generate are pinned byte for byte on one hand-built batch, and so is
  the empty batch with no columns in every format. One test,
  `TestStream`, pins the stream path on that batch: two batches stream
  as the two one-batch bodies joined, and an error element ends the
  stream with its error. Neither pin
  needs a cluster.
- The decoders' tests are in `decode_test.go`. One generative round
  trip over every codec draws tables through the fixture, encodes them
  as one body over `table.Body` and decodes them back to the batches it
  drew, without a cluster. The faults of a body are one property over
  every codec, which draws one mutation of the batch before encoding
  it, so the fault logic is written once. What a batch cannot express,
  bytes that are not the format, a value of the wrong kind and a JSON
  body of the wrong shape, is one short table per format. A second property shuffles the tables' rows
  through the stream encoder.
- A test helper that parses a body is named `read*`, because `decode`
  is the package's word for its `Decoder`. `encoding_test.go` holds the
  helpers both files share.
