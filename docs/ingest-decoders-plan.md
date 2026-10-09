# The NDJSON and Arrow IPC decoders and the request codings -- Plan

Status: draft. This plan is scaffolding for one unit on the branch
`sc-19567/rr-ingest-decoders`. It is deleted when the unit lands
(`docs/AGENTS.md`, Plans). It is written for an executor who has none
of the conversation that shaped it.

## Outcome

When the unit lands:

- `POST /api/v2/rows` reads a CSV, an NDJSON or an Arrow IPC body,
  picked by `Content-Type`, and pushes it in one batch per table the way
  the CSV ingest does today.
- The body may arrive under `Content-Encoding: gzip` or `zstd`. The
  handler decompresses it before the decoder reads it. Any other coding
  is 415.
- `encoding.NDJSON` and `encoding.Arrow` implement `Decoder`, each in
  its encoder's file, and each reads back what its encoder wrote. The
  decode round trip (`internal/encoding/decode_test.go`) proves this
  over every codec without a cluster, and each decoder has one fault
  table.
- `TestRoundtrip` (`internal/httpapi/roundtrip_test.go`) ingests under
  a drawn body format and a drawn request coding, and `TestErrorRows`
  carries the two coding rows.
- `internal/encoding/AGENTS.md` and `internal/httpapi/AGENTS.md` state
  the rules this plan records, and `docs/log.md` strikes Next item 1.

Left for later units: the e2e tool and driver (`docs/log.md`, Next 2,
`docs/e2e-v2-flow-plan.md`), and casting a body column into the
reader's type (rejected here, Rationale).

## Verified facts

Each fact was checked on 2026-10-09 against the tree at `c507129`.

1. The `Decoder` seam: `Decode(ctx, r, schemaOf) ([]model.TableBatch, error)`,
   no batches on error (`internal/encoding/encoding.go:32-38`).
2. The CSV decoder is the shape: one pass, per-table builders, a table
   typed by `schemaOf` on first sight, the first table's types checked
   against every later table, `ErrInvalidRows` naming row and column
   with the reader's cause in the chain, one batch per table in
   first-seen order (`internal/encoding/csv.go:229-282,362-430`).
3. `model.TableBatch` holds one batch per table and `model.SchemaOf`
   answers the reader's whole-table schema, `$table` and `$timestamp`
   first (`internal/model/model.go:10-22`).
4. `Session.ingest` releases every decoded batch after the push, stages
   each through `ArrowWriter.SetTable`, and skips the push when no rows
   came (`internal/qdb/ingest.go:117-153`).
5. The Arrow writer accepts int64, float64, timestamp in any unit,
   date64, utf8 and binary, refuses a `$table` field, refuses a
   dictionary or large type, and accepts zero-row batches
   (`vendor/github.com/bureau14/qdb-api-go/v3/writer_arrow.go:250-273`).
6. `ipc.NewReader` reads the schema eagerly and returns an error for a
   body that is not a stream; `Reader.Next` releases the previous batch,
   and `RecordBatch` is valid until the next `Next`; dictionary batches
   are memoised inside the reader (`vendor/github.com/apache/arrow-go/v18/arrow/ipc/reader.go:97-131,202-303`).
7. `RecordBatch.NewSlice(i, j)` and `array.NewSlice` share the parent's
   buffers; `array.Concatenate` copies into one array
   (`vendor/github.com/apache/arrow-go/v18/arrow/array/record.go:268`,
   `array/array.go:133`, `array/concat.go:41`).
8. `arrow.TypeEqual` compares data types without field metadata
   (`vendor/github.com/apache/arrow-go/v18/arrow/compare.go:42`), so
   the reader's `max_width` metadata on a string field does not sink an
   equal body type.
9. `jsontext.Decoder` reads a stream of top-level values, NDJSON named
   as the case, through `ReadToken`, `PeekKind`, `SkipValue` and
   `Token.Int`, `Token.Float`, `Token.String`
   (`/opt/local/lib/go-1.27/src/encoding/json/jsontext/decode.go:50,122,314,413,469,1167-1169`,
   `token.go:278,386,452`). The JSON encoder already appends through
   `jsontext` (`internal/encoding/json.go:44,54`).
10. `gzip.NewReader` reads the header at construction and `Close`
    returns an error; `zstd.NewReader(r, zstd.WithDecoderConcurrency(1))`
    decodes a stream synchronously with no goroutine, and `Close` returns
    nothing (`/opt/local/lib/go-1.27/src/compress/gzip/gunzip.go:92,290`,
    `vendor/github.com/klauspost/compress/zstd/decoder.go:84,581`,
    `decoder_options.go:61-63`).
11. The response side: `coding`, `negotiateCoding`, `newCompressor` at
    the fastest level with zstd concurrency one, `withCompression`
    (`internal/httpapi/compress.go:14-60,113-133`). The ingest handler
    picks the decoder from `decoders` by media type, caps the body with
    `MaxBytesReader`, and classifies the error (`internal/httpapi/rows.go:26-115`).
12. The fixture: `table.Body` joins tables into one batch with `$table`
    in front, in the binding's Arrow types, and `table.WithTable(rt,
first).Schema()` is the reader's schema of the shared column list
    (`internal/qdbtest/table/table.go:315-374`,
    `internal/encoding/decode_test.go:64-69`). The fixture draws no
    empty string, no NaN, no NUL (`table.go:75-87,95-120`).
13. The round trip's ingest body is the CSV encoder over `table.Body`,
    and `newCompressor` is reachable from the httpapi tests, which are
    white-box (`internal/httpapi/roundtrip_test.go:29-46`,
    `internal/AGENTS.md`, Tests).
14. `encode_test.go` names its body parsers `read*`; `decode` is the
    package's word for `Decoder` (`internal/encoding/AGENTS.md`, Tests).

## Design

The entries are in the file order they will have. Each is a sketch:
the shape and the claims, not the wording.

### `internal/encoding/encoding.go`, after `timestampLayout`

```go
// textAppender returns the function that parses one cell of text into
// the builder b, the inverse of the text the encoders write: an integer
// through strconv, a float through strconv, a timestamp in RFC 3339, a
// string as its own bytes, a blob through standard base64. The text is
// the same on the CSV and NDJSON wires, so both decoders call it.
// [csv.go:178-211 before this unit, Rationale row 11]
func textAppender(f arrow.Field, b array.Builder) (func(string) error, error) {
	// style: doc comment alone + inline note at the switch
	// because: one type switch like csvAppender, the shape arrow-go's own
	// CSV reader takes to reach a typed Append from a dynamic schema
	//
	// at the switch: the Builder interface carries no typed Append, so the
	// switch runs once per column and the cell path is one typed call
	// [arrow/array/builder.go:59-72, arrow/csv/reader.go:431-441]
	// at the switch: arrow-go's AppendValueFromString was rejected, because
	// its grammar refuses the Z our timestamps carry, accepts a bare
	// integer as a timestamp and reads "(null)" as null [Rationale row 11]
}
```

`csvAppender` leaves; `newCSVTable` calls `textAppender`.

### `internal/encoding/json.go`, after the `JSON` encoder

```go
// ndjsonTable accumulates the rows of one table: the batch's schema,
// one builder per field with its appender, and the rows appended so far.
// [fact 2]
type ndjsonTable struct {
	name      string
	schema    *arrow.Schema
	builders  []array.Builder
	appenders []func(string) error
	rows      int
}
```

```go
// newNDJSONTable types the body's columns by the table's schema:
// $timestamp first, then the first object's names in their order. A
// name the table lacks is ErrInvalidRows. A table after the first must
// agree with the first on every field's type, or ErrInvalidRows names
// both tables. The error of schemaOf passes through as is. [fact 2]
func newNDJSONTable(name string, names []string, schemaOf model.SchemaOf, first *ndjsonTable) (*ndjsonTable, error) {
	// style: doc comment + numbered overview + step comments
	// because: the same four steps as newCSVTable
	//
	// 1. look the table up through schemaOf and pass its error as is [fact 2]
	// 2. pick $timestamp and the names from the schema by name [csv.go:247-257]
	// 3. a table after the first has the same type in every field
	//    [csv.go:259-266, internal/AGENTS.md, the ingest]
	// 4. one builder and one textAppender per field [fact 2]
}
```

```go
// appendRow appends one object, as the map of its raw members, to the
// builders of t: a column the object lacks or names null is null, and
// a null or absent $timestamp is an error, because the index cannot be
// null. A member outside the column list is an error naming it.
// [Rationale row 1, csv.go:284-301]
func (t *ndjsonTable) appendRow(row map[string]jsontext.Value) error {
	// style: doc comment + numbered overview + step comments
	// because: two passes over the row, and three things fail on the way
	//
	// 1. every member must be a column of the list, $table included,
	//    so a typo is a fault and not a silent drop [Rationale row 1]
	// 2. walk the column list: absent or null appends null, except the
	//    index; a string-kind value is unquoted once and every value's
	//    text goes through textAppender, because the number text and
	//    the string content are the CSV cell's text [Rationale row 11]
}
```

```go
// Decode implements Decoder. The body is one JSON object per row, in
// this encoder's dialect: the first object's keys fix the column list,
// $table and $timestamp among them. [Rationale rows 1, 2]
func (NDJSON) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// style: doc comment + numbered overview + step comments
	// because: the same three steps as CSV.Decode, over objects
	//
	// each row is read as one map of raw values through json.UnmarshalDecode,
	// so the standard library parses and this decoder only routes
	// [Rationale row 2]
	//
	// 1. the first object fixes the column list: its keys, $table and
	//    $timestamp among them, in its order [Rationale row 1]
	// 2. each object goes to the builders of its table, named by its
	//    $table member, which must be a string; a table seen for the
	//    first time is typed through schemaOf [csv.go:388-421]
	//    - the ctx is checked once per chunkRows rows [encoding.go:40-52]
	//    - a read error keeps the decoder's cause in the chain [csv.go:400-402]
	// 3. at the end every table becomes one batch, in first-seen order [fact 2]
}
```

### `internal/encoding/arrow.go`, after the `Arrow` encoder

```go
// arrowTable accumulates the slices of one table across the batches of
// a stream: the batch's schema in the reader's fields, the column index
// of each field in the body, and the slices collected so far. [fact 7]
type arrowTable struct {
	name   string
	schema *arrow.Schema
	fields []int               // the body column of each schema field
	slices []arrow.RecordBatch // retained slices of the stream's batches
}
```

```go
// newArrowTable types the body's columns by the table's schema:
// $timestamp first, then the body's data columns in their order. A
// name the table lacks, and a column whose type differs from the
// reader's, are ErrInvalidRows. A table after the first must agree with
// the first, which the equality with the reader's types already gives,
// because every table of the body is checked against the same body
// columns. The error of schemaOf passes through as is. [fact 2, Rationale row 3]
func newArrowTable(name string, body *arrow.Schema, schemaOf model.SchemaOf) (*arrowTable, error) {
	// style: doc comment + numbered overview + step comments
	// because: the same steps as newCSVTable, with the type check turned
	// on the body instead of on the previous table
	//
	// 1. look the table up through schemaOf and pass its error as is [fact 2]
	// 2. pick $timestamp and the body's names from the reader's schema;
	//    a name the table lacks is the body's fault [csv.go:247-257]
	// 3. the body column's type equals the reader's, by arrow.TypeEqual,
	//    which ignores the reader's max_width metadata [fact 8]
	//    - rejected: casting a body column into the reader's type, because
	//      the binding refuses what the cast would take and a conversion
	//      layer is more code than the rule [Rationale row 3]
	//    - a $table column that is not utf8 (a dictionary, for one) is
	//      refused with the same error [Rationale row 4]
	// 4. the batch's schema is the reader's fields in that order, so the
	//    batch carries the reader's types as the seam says [AGENTS.md, The seam]
}
```

```go
// add keeps the rows [i, j) of rec for this table, as a slice that
// shares rec's buffers and is retained past the reader's next batch.
// [facts 6, 7]
func (t *arrowTable) add(rec arrow.RecordBatch, i, j int64)
```

```go
// batch concatenates the slices into one record batch under the
// reader's fields and releases the slices. [fact 7]
func (t *arrowTable) batch() (model.TableBatch, error) {
	// style: doc comment alone + inline note at the concatenate
	// because: one loop over the columns, and the one reason is the copy
	//
	// at the concatenate: the copy is accepted, because the seam hands
	// the push one batch per table and the binding's several-batches
	// form is not carried by model.TableBatch [Rationale row 5]
}
func (t *arrowTable) release()
```

```go
// splitRuns calls f once per run of equal $table values in rec, with
// the table name and the row range. A null $table is ErrInvalidRows
// naming the row. [Rationale row 4]
func splitRuns(rec arrow.RecordBatch, table int, f func(name string, i, j int64) error) error {
	// style: doc comment alone + inline note at the run boundary
	// because: one scan over the column
	//
	// at the boundary: rows of one table are usually contiguous, so a
	// run is one slice and the common body costs one slice per table
	// per batch; an interleaved body costs one slice per row and
	// decodes all the same [Rationale row 5]
}
```

```go
// Decode implements Decoder. The body is an Arrow IPC stream in this
// encoder's dialect: one schema with $table, $timestamp and the data
// columns in any order, in the reader's types, and any number of
// record batches. [facts 5, 6, Rationale row 3]
func (Arrow) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// style: doc comment + numbered overview + step comments
	// because: the same three steps as CSV.Decode, over batches
	//
	// the stream is read batch by batch and each batch is sliced into
	// runs of one table, so a body of any batch count decodes with one
	// copy per table [facts 6, 7]
	//
	// 1. open the reader; a body that is not a stream, or whose schema
	//    names no $table or $timestamp, is ErrInvalidRows [fact 6, csv.go:350-355]
	//    - the reader's error keeps the cause in the chain, because the
	//      HTTP cap surfaces through it [csv.go:400-402]
	// 2. each batch is split into runs; a table seen for the first time
	//    is typed through schemaOf against the body's schema; every run
	//    is kept as a retained slice [facts 6, 7]
	//    - the ctx is checked once per batch, as the encoder checks it
	//      [arrow.go:33-36]
	// 3. at the end every table becomes one batch, in first-seen order,
	//    and the reader is released [fact 2]
}
```

### `internal/encoding/decode_test.go`

```go
// codecs gains NDJSON and Arrow, so the one round trip covers them
var codecs = ...{{CSV{}, CSV{}}, {NDJSON{}, NDJSON{}}, {Arrow{}, Arrow{}}}
```

The round trip itself is unchanged: `table.Body` in each format decodes
to the batches drawn (fact 12).

```go
// TestDecodeFaults: one property over every codec. It draws tables
// through the fixture, applies one drawn mutation to the body batch
// before encoding, and checks that the decoder answers ErrInvalidRows.
// The mutations are of the batch, so one list serves every format:
// drop $table, drop $timestamp, rename a data column, retype a data
// column, null one $timestamp slot, null one $table slot. A table
// schemaOf refuses surfaces its error unwrapped, drawn as a seventh
// case. [Rationale row 12, fact 12]
```

```go
// TestDecodeInterleaved: the rows of two or three tables shuffled into
// one body decode to one batch per table, in first-seen order, with
// each table's rows in body order, for every codec. The Arrow body is
// written in several record batches. [Rationale row 5]
```

```go
// TestDecodeFormatFaults: what the batch cannot express, one short
// table per codec: bytes that are not the format, a fraction in an
// int64 column (CSV, NDJSON), a top-level value that is not an object
// (NDJSON), a dictionary-encoded $table (Arrow). [Rationale rows 2, 4]
```

```go
// TestNDJSONSparseRows: a later object may omit a column, which reads
// as null, and may name its members in any order. The body is written
// by hand, because no encoder writes a sparse object. [Rationale row 1]
```

`TestCSVDecodeFaults`, `TestNDJSONDecodeFaults`, `TestArrowDecodeFaults`,
`TestArrowInterleavedBatches` and `arrowBody` leave.

### `internal/httpapi/compress.go`, after `withCompression`

```go
// requestCoding reads the Content-Encoding of a request as one coding.
// An absent header and identity mean none. The second value is false
// for a coding this server does not read, which includes a list of
// several. [Rationale row 6]
func requestCoding(contentEncoding string) (coding, bool) {
	// style: doc comment alone + inline note at the list check
	// because: one normalisation and one switch
	//
	// at the list check: a coding list is refused rather than unwound,
	// because no client stacks codings on a request and unwinding is
	// code for nobody [Rationale row 6]
}
```

```go
// newDecompressor opens a reader of coding c over r, the inverse of
// newCompressor. The gzip reader reads the header now, so a corrupt
// body can fail here, and the zstd decoder runs at concurrency one, so
// a request spawns no goroutines. [fact 10, Rationale row 7]
func newDecompressor(c coding, r io.Reader) (io.ReadCloser, error) {
	// style: doc comment alone
	// because: one switch like newCompressor
	//
	// at the zstd arm: IOReadCloser adapts the decoder, whose Close
	// returns nothing [fact 10]
}
```

### `internal/httpapi/rows.go`

```go
// decoders gains NDJSON and Arrow
var decoders = map[string]encoding.Decoder{
	encoding.CSVContentType:    encoding.CSV{},
	encoding.NDJSONContentType: encoding.NDJSON{},
	encoding.ArrowContentType:  encoding.Arrow{},
}
```

```go
// handleIngestRows: the overview gains two steps and the error switch
// one arm
func handleIngestRows(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + numbered overview + step comments (existing)
	//
	// 1. a body of a type no decoder reads is 415 (existing)
	// 2. a Content-Encoding this server does not read is 415 naming
	//    gzip and zstd, the mirror of the type check [Rationale row 6]
	// 3. the body under the ingest cap, which bounds the bytes on the
	//    wire; the decoded size of a hostile body is not bounded, an
	//    accepted cost inside a customer network [Rationale row 8]
	// 4. the decompressor over the capped body; its construction error
	//    is the body's fault, 400 [fact 10, Rationale row 9]
	//    - closed when the handler returns, after the decode
	// 5. one call under the held session's schema lookup (existing)
	// 6. the status by who failed (existing); a corrupt coding surfaces
	//    from the decode as ErrInvalidRows, so no new arm is needed
	//    [csv.go:400-402, Rationale row 9]
}
```

### `internal/httpapi/roundtrip_test.go`

```go
// ingestBodyOf takes the encoder; the body is that encoder over
// table.Body [fact 13]
func ingestBodyOf(t table.T, e encoding.Encoder, tables []table.Table) []byte

// ingest takes the headers whole; the round trip draws a codec from
// decoders' keys and a coding from {identity, gzip, zstd}, compresses
// the body through newCompressor for a coding other than identity,
// and sets Content-Type and Content-Encoding [facts 11, 13]
```

The step-4 comment of `TestRoundtrip` says the format and the coding
are drawn, so every iteration ingests one of nine combinations.
`TestRoundtripDeduplicated` keeps CSV.

### `internal/httpapi/errors_test.go`

Two rows join the table:

```go
"ingest: unknown content-encoding": Content-Encoding: br, 415
"ingest: corrupt gzip":             Content-Encoding: gzip over the CSV header bytes uncompressed, 400
```

### `internal/encoding/AGENTS.md`

Under "The seam", the decoder bullet gains:

```
- The NDJSON decoder reads one object per row. The first object's keys
  fix the column list, $table and $timestamp among them. A later
  object's absent key is null, and a key outside the list is
  ErrInvalidRows. A number token into an int64 column must be an
  integer literal.
- The Arrow decoder reads an IPC stream of any batch count. The body's
  columns are picked by name, and a body column's type must equal the
  reader's type for that name. There is no cast, because the binding
  refuses what a cast would take. A dictionary-encoded or null $table
  is ErrInvalidRows. Rows are sliced by runs of one $table value and
  concatenated once per table, so the common body costs one copy.
```

Under "Tests": "The decoders' tests" gains "one fault table per
decoder".

### `internal/httpapi/AGENTS.md`

The ingest bullet under Handlers gains:

```
- `Content-Encoding: gzip|zstd` is decompressed before the decoder
  reads the body, and any other coding, a list included, is 415
  naming the two. The ingest cap (`maxIngestBytes`) bounds the bytes on the wire, so
  the decoded size of a hostile body is not bounded; the brief
  excludes public-internet hardening. A corrupt compressed body is
  400 through ErrInvalidRows.
```

The Middleware compression bullet gains one sentence: the request side
is `newDecompressor`, the inverse of `newCompressor`, at concurrency
one for zstd.

Under "Tests", the round trip bullet says the ingest draws a body
format and a request coding.

### `docs/log.md`

Next item 1 is struck, the later items renumber, and one dated entry
records the unit: `ingest-decoders-plan.md deleted; facts moved to
internal/encoding/AGENTS.md and internal/httpapi/AGENTS.md`. The
deletion itself is the merge stage's work, after the plan's work lands.

## Rationale

| decision                                                                                                             | why                                                                                                                                                               | rejected, and why                                                                                                                                                                               | gained                                | given up                                                         | settled by                                                                                                                          |
| -------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------- | ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| 1. The first NDJSON object fixes the column list; an absent key in a later object is null, an unknown key is a fault | one body is one column list, as the CSV header fixes it, and sparse NDJSON is ordinary                                                                            | refusing any object whose keys differ: rejects ordinary bodies for no gain                                                                                                                      | sparse bodies decode                  | a typo in a later object's key is a fault, not a new column      | owner, 2026-10-09                                                                                                                   |
| 2. NDJSON reads each row through `json.UnmarshalDecode` into a map of raw values                                     | the text formats are for convenience and Arrow is the fast path, so the standard library parses and the decoder only routes; raw values keep an int64 exact       | walking `jsontext` tokens by hand: a parser in the package for a cost nobody measured, reversed in review                                                                                       | the decoder is routing alone          | a map and its keys per row                                       | owner, 2026-10-09                                                                                                                   |
| 11. One `textAppender` in `encoding.go` parses a cell's text for CSV and NDJSON                                      | the cell text is the same on both wires, and the type switch is the shape arrow-go's own CSV reader takes to reach a typed `Append`                               | one switch per format: the same five conversions twice; arrow-go's `AppendValueFromString`: refuses the `Z` our timestamps carry, accepts a bare integer as a timestamp, reads `(null)` as null | one switch in the package             | NDJSON unquotes a string-kind value before the shared parser     | owner, 2026-10-09; `arrow/array/builder.go:59-72`, `arrow/csv/reader.go:431-441`, `array/timestamp.go:365-380`, `array/array.go:34` |
| 12. The decoders' faults are one generative property over mutations of the batch                                     | one list of mutations serves every format, so the fault logic is written once                                                                                     | one hand-written table per decoder: the same cases three times, reversed in review                                                                                                              | one property for every codec          | faults a batch cannot express stay in one short per-format table | owner, 2026-10-09                                                                                                                   |
| 3. A body column's Arrow type must equal the reader's type                                                           | the binding refuses a large or dictionary type on its side anyway, and a conversion layer is more code than the rule                                              | casting (`timestamp[us]` into `[ns]`, `large_utf8` into `utf8`): a layer the binding then re-checks                                                                                             | one rule, one check                   | a pyarrow client must write `timestamp[ns]` and plain `utf8`     | owner, 2026-10-09                                                                                                                   |
| 4. A dictionary-encoded or null `$table` is a fault                                                                  | no client of ours sends one, and decoding the dictionary is code for nobody                                                                                       | decoding the dictionary                                                                                                                                                                         | less code                             | pyarrow's default for a categorical column is refused            | proposal                                                                                                                            |
| 5. Rows are sliced by runs of one `$table` value and concatenated once per table                                     | the seam hands the push one batch per table, and the common body is one table per run                                                                             | handing the binding several batches per table: `SetTable` accepts them, `model.TableBatch` does not carry them; per-row builders: a copy per cell                                               | one copy per table, zero-copy slicing | an interleaved body costs one slice per row                      | `internal/model/model.go:13-16`, `internal/encoding/AGENTS.md` The seam                                                             |
| 6. `Content-Encoding` names one coding; a list or anything but gzip, zstd, identity is 415                           | the mirror of the type check, and no client stacks codings on a request                                                                                           | unwinding a list: code for nobody                                                                                                                                                               | one token read                        | a stacked request is refused                                     | proposal                                                                                                                            |
| 7. The zstd request decoder runs at concurrency one                                                                  | a request spawns no goroutines, as the response encoder spawns none                                                                                               | the library default: goroutines per request                                                                                                                                                     | one reader per request, no pool       | a slower decode on a large body, unmeasured                      | ADR-0012 Decision 4 by symmetry; proposal for the request side                                                                      |
| 8. The ingest cap bounds the bytes on the wire                                                                       | that is the request body the cap names, and the brief excludes public-internet hardening                                                                          | capping the decoded bytes: a second limit and a second 413 path                                                                                                                                 | the cap stays one line                | the decoded size of a hostile body is unbounded                  | owner, 2026-10-09                                                                                                                   |
| 9. A corrupt compressed body is 400                                                                                  | the reader's error surfaces from the decode wrapped in `ErrInvalidRows`, the path the cap already uses; a gzip header error at construction takes the same status | a status of its own: nothing distinguishes a corrupt coding from a corrupt body to the caller                                                                                                   | no new arm in the handler             | the detail is the library's message                              | proposal                                                                                                                            |
| 10. Request decompression lives in `internal/httpapi/compress.go`                                                    | compression sits below the encoders, which know nothing of HTTP                                                                                                   | a decoder in `internal/encoding`: the encoders would know HTTP                                                                                                                                  | one file for both directions          | none                                                             | ADR-0012 Decision 2                                                                                                                 |

## Knowledge

Per commit, where each why lands.

- Commits 10 to 12 (the review's redirect): `textAppender` carries row 11
  at its switch; `NDJSON.Decode` carries row 2 in its overview;
  `appendRow` carries rows 1 and 11; `TestDecodeFaults` carries row 12.
  `internal/encoding/AGENTS.md`, The seam, says the text wires share
  one cell parser (row 11), and Tests says the faults are one property
  (row 12).
- Commit 2 (NDJSON decoder): the narrated functions `newNDJSONTable`,
  `appendObject`, `readNDJSONHeader` and `NDJSON.Decode` carry rows 1
  and 2 at the steps named in Design; `ndjsonAppender`'s int64 arm
  carries the integer-literal rule. `internal/encoding/AGENTS.md`, The
  seam, gains the NDJSON bullet (rows 1, 2).
- Commit 3 (NDJSON tests): `TestNDJSONDecodeFaults` pins rows 1 and 2;
  `internal/encoding/AGENTS.md`, Tests, says one fault table per
  decoder.
- Commit 4 (Arrow decoder): `newArrowTable` carries rows 3 and 4 at
  step 3, `arrowTable.batch` carries row 5 at the concatenate,
  `splitRuns` carries row 5 at the boundary, and `Arrow.Decode`
  carries fact 6 in its overview. `internal/encoding/AGENTS.md`, The
  seam, gains the Arrow bullet (rows 3, 4, 5).
- Commit 5 (Arrow tests): `TestArrowDecodeFaults` pins rows 3 and 4.
- Commit 6 (the ingest reads the two bodies): `decoders` gains two
  entries, no new why.
- Commit 7 (the request codings): `requestCoding` carries row 6,
  `newDecompressor` carries row 7 and fact 10, `handleIngestRows` steps
  2 to 4 and 6 carry rows 6, 8 and 9. `internal/httpapi/AGENTS.md`,
  Handlers, gains the coding bullet (rows 6, 8, 9) and the Middleware
  bullet names the request side (rows 7, 10).
- Commit 8 (httpapi tests): the round trip's step 4 says the format and
  the coding are drawn; the two error rows pin rows 6 and 9.
  `internal/httpapi/AGENTS.md`, Tests, says the round trip draws both.
- Commit 9 (log): Next item 1 struck; nothing else moves, because every
  fact above has its home in an `AGENTS.md` or a comment.

Every Rationale row lands at least once: 1, 2 in commits 2, 3 and 10 to 12; 11, 12 in commits 10 to 12; 3,
4, 5 in commits 4 and 5; 6, 7, 8, 9, 10 in commits 7 and 8.

## How the knowledge lands

The executor:

1. Runs `/doc-discipline read` before the first code commit.
2. Writes every commit's comments and document rows from their
   specifications in Design and Knowledge, in the same commit as the
   code, in the shape the root `AGENTS.md`, "Code comments", prescribes
   and in the prose of root `AGENTS.md`, Prose. Every listed claim
   appears once, at the place the specification names. No claim is
   added without evidence. A claim the code contradicts is not written
   and is reported in the build-stage message.
3. Writes a why that arises while building and is not in the plan where
   it is decided, with its evidence, or asks the owner through the
   question tool before the commit that needs it.
4. After commit 9, runs `/doc-discipline all internal/encoding
internal/httpapi docs/log.md` and lands one small commit per finding.
5. Before the build-stage message, runs `/doc-discipline check
internal/encoding internal/httpapi docs/ingest-decoders-plan.md`,
   which reconciles the code against this plan, and quotes its report
   or says it was clean.
6. Runs `make lint` and `go test -p 1 ./internal/encoding/
./internal/httpapi/` before every commit, with the fixture services
   up (`internal/AGENTS.md`, Tests).

## Commits

1. `docs(plan): ingest-decoders-plan.md, the NDJSON and Arrow IPC decoders and the request codings` (this document)
2. `feat(encoding): the NDJSON decoder reads one object per row into one batch per table`
3. `test(encoding): NDJSON joins the decode round trip; the NDJSON body's faults`
4. `feat(encoding): the Arrow decoder splits an IPC stream by $table into one batch per table`
5. `test(encoding): Arrow joins the decode round trip; the Arrow body's faults`
6. `feat(httpapi): the ingest reads NDJSON and Arrow IPC bodies`
7. `feat(httpapi): the ingest reads a gzip or zstd body under Content-Encoding`
8. `test(httpapi): the round trip ingests under a drawn format and coding; the coding error rows`
9. `docs(log): the decoders and the request codings landed; Next item 1 struck`
10. `/doc-discipline all internal/encoding internal/httpapi docs/log.md`, one small commit per finding.
11. `/doc-discipline check internal/encoding internal/httpapi docs/ingest-decoders-plan.md`, one small commit per unlanded reason.
12. `docs(plan): ingest-decoders-plan.md, the review's redirect: one text parser, the standard library parses NDJSON, one fault property`
13. `refactor(encoding): textAppender parses a cell's text for both text wires`
14. `refactor(encoding): the NDJSON decoder reads each row through json.UnmarshalDecode`
15. `test(encoding): the decoders' faults are one property over mutations of the batch`
16. `/doc-discipline check internal/encoding docs/ingest-decoders-plan.md`, one small commit per unlanded reason.
17. Verify: push `sc-19567/rr-ingest-decoders`, build its head in
    Buildkite through the API with `ignore_pipeline_branch_filters`
    and the full 40-character SHA (`.buildkite/AGENTS.md`), reprioritize
    its scheduled jobs, wait for the result. Green: report the build
    number. Red: fix with further small commits on this branch, push,
    build again.

## Open questions and recommendations

1. Row 4, a dictionary-encoded `$table` is refused: recommended, because
   no client sends one yet and the rule is one line to lift later.
2. Row 6, a coding list is 415: recommended, the mirror of the
   `Content-Type` rule.
3. Row 7, zstd request decoding at concurrency one: recommended, by
   symmetry with ADR-0012.
4. Row 9, a corrupt compressed body is 400 with the library's message
   as the detail: recommended, because it is the path the cap already
   takes.
