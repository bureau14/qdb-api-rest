# duckdb-scalars -- the qdb-duck pin goes, and the text encoders write every DuckDB scalar

Status: draft

Branch: `sc-19567/rr-duckdb-scalars`, off `sc-19567/rest-rewrite` at
`dcd4a4d`. This plan is the unit's only memory. An executor writes
every commit, comment and document row from it alone.

## 1. Outcome

When the unit lands:

- `.buildkite/pipeline.py` names no qdb-duck ref. Every step downloads
  qdb-duck under this repository's own ref, and the qdb-artifacts plugin
  falls back to qdb-duck's master, FreeBSD included.
- The JSON, NDJSON and CSV encoders write every scalar type DuckDB
  answers: `int8`, `int16`, `int32`, `uint8`, `uint16`, `uint32`,
  `uint64`, `float32`, `decimal128`, `bool`, `date32` and `time64`, in
  addition to the five they write today. `SELECT 1` (`int32`),
  `SELECT NULL` (`int32`) and `sum()` (`decimal(38,0)`) answer 200 on
  `POST /api/v2/sql`.
- The `type` word of the columnar JSON result is the Arrow type's own
  name (`DataType.Name()`) for every column. The five existing words
  become `int64`, `float64`, `utf8`, `binary` and `timestamp`.
- A column of any other type (an interval, a list, a struct, a map, a
  dictionary-encoded enum) is still `ErrUnsupportedType`, which names
  the column, and the sql endpoint answers 500 before the first byte.
- `docs/log.md` Next no longer lists the pin or the encoder widening.

Left for later units:

- Nested and other non-scalar types (intervals, lists, structs, maps,
  enums). No document calls for them yet.
- The remaining Next items of `docs/log.md`: the data-path tests on the
  secure cluster, M5, and the qdb-duck `qdb://` story.
- The v1 query wire (M8) keeps QuasarDB's words in its own wart
  encoders (`docs/brief.md`, POST /api/v1/query, and ADR-0007). This
  unit does not touch v1, because no v1 code exists yet.

## 2. Verified facts

| fact                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        | evidence                                                                                                                                          |
| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| bureau14/qdb-duck#17 merged on 2026-10-15 as `5b80617`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `gh pr view 17 -R bureau14/qdb-duck`, session 2026-10-17                                                                                          |
| qdb-duck master build 90 passed at `5b80617`, with both FreeBSD jobs passed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | `bk api pipelines/qdb-duck/builds?branch=master`, session 2026-10-17                                                                              |
| The pipeline library treats a missing `by_project` key as empty                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | `.buildkite/tools/qdb_pipeline/artifacts.py:21-22`                                                                                                |
| The plugin walks this repository's ref, then master, then main                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `.buildkite/AGENTS.md`, Facts                                                                                                                     |
| DuckDB 1.5.4's Arrow export answers these types: TINYINT `int8`, SMALLINT `int16`, INTEGER `int32`, BIGINT `int64`, HUGEINT and UHUGEINT `decimal(38, 0)`, UTINYINT to UBIGINT `uint8` to `uint64`, FLOAT `float32`, DOUBLE and `avg()` `float64`, `sum()` over integers `decimal(38, 0)`, DECIMAL(p,s) `decimal(p, s)`, `count(*)` `int64`, BOOLEAN `bool`, DATE `date32`, TIME and TIMETZ `time64[us]`, TIMESTAMP `timestamp[us]`, TIMESTAMPTZ `timestamp[us, tz=<host zone>]`, TIMESTAMP_NS/MS/S `timestamp[ns/ms/s]`, INTERVAL `month_day_nano_interval`, VARCHAR, UUID and JSON `utf8`, BLOB, BIT and VARINT `binary`, lists `list<..>`, structs `struct<..>`, MAP `map<..>`, ENUM `dictionary<values=utf8, indices=uint8>`, and a bare `NULL` `int32` | scratch program over the vendored `duckdb-go` v2.10504.0 (`NewArrowFromConn`), run through `go run -overlay` outside the tree, session 2026-10-17 |
| DuckDB answers `TIME '24:00:00'` as `time64[us]` value 86400000000                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          | same probe, session 2026-10-17                                                                                                                    |
| DuckDB answers dates before year 1 (`DATE '0001-01-01' - 1` is `0000-12-31`)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | same probe, session 2026-10-17                                                                                                                    |
| The timestamp types already encode: `*array.Timestamp` covers every unit through `nanosReader`, and the zone is not read                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | `internal/encoding/encoding.go:72-77`, `json.go:105-113`, `csv.go:50-57`                                                                          |
| The Arrow encoder already transmits any type                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | `internal/encoding/AGENTS.md`, Arrow                                                                                                              |
| arrow-go names: `Int8Type` `int8` ... `Uint64Type` `uint64`, `Float32Type` `float32`, `Float64Type` `float64`, `Decimal128Type` `decimal`, `BooleanType` `bool`, `Date32Type` `date32`, `Time64Type` `time64`, `StringType` `utf8`, `BinaryType` `binary`, `TimestampType` `timestamp`                                                                                                                                                                                                                                                                                                                                                                                                                                                                      | `vendor/github.com/apache/arrow-go/v18/arrow/datatype_fixedwidth.go:36,450,574,744`, `datatype_binary.go:70`, `datatype_numeric.gen.go:141,154`   |
| `decimal128.Num.ToString(scale)` writes the exact plain text, keeping the scale's trailing zeros (`1.50` for scale 2)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | `vendor/github.com/apache/arrow-go/v18/arrow/decimal128/decimal128.go:529-560`                                                                    |
| `decimal128.FromString(text, prec, scale)` parses that text back                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | `decimal128.go:235`                                                                                                                               |
| Each arrow-go numeric array is its own struct with `Value(i) T` and `IsNull(i)`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | `vendor/github.com/apache/arrow-go/v18/arrow/array/numeric_generic.go:32-49,346-478`                                                              |
| The JSON decoder does not read the `type` member                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | `internal/encoding/AGENTS.md`, The JSON decoder                                                                                                   |
| No ADR fixes the JSON type words. They came from the text encoders' plan                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | `docs/log.md`, 2026-09-11 entry; `grep -n double docs/adr/*.md` prints nothing                                                                    |
| `SELECT 1` is today's sql unsupported-type row                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              | `internal/httpapi/errors_test.go:82`                                                                                                              |
| The table endpoints speak QuasarDB's column types                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | `internal/qdb/table.go:19-30`, `docs/brief.md:531-535`                                                                                            |

## 3. Design

File order follows each file's existing order. Only added or changed
entries are sketched.

### `.buildkite/pipeline.py`

The comment block above `_DUCK_GIT_REF` and the constant are deleted.
`_download_options` loses its `by_project` entry and its last docstring
sentence.

```python
def _download_options(variant: str, git_ref: str) -> dict:
    # style: doc comment alone
    # because: one dict literal, nothing to narrate
    #
    # - every download project receives this repository's ref and the
    #   step's variant [pipeline.py:182-190]
    # - the plugin resolves a ref without artifacts to the project's
    #   master, so no ref is spelled out for quasardb-build or qdb-duck
    #   [.buildkite/AGENTS.md, Facts]
    # - (deleted) the sentence about the by_project pin
```

### `internal/encoding/encoding.go`

Two layouts join `timestampLayout`, below it, and one helper joins
`nanosReader`.

```go
// dateLayout is how every text format writes a date32: the ISO 8601
// calendar date. [owner, 2026-10-17, Rationale row 5]
// A date has no zone and no instant, so it carries no time and no Z.
// [owner, 2026-10-17]
const dateLayout = "2006-01-02"
	// style: doc comment alone
	// because: a constant whose comment says where its value came from
```

```go
// appendTimeOfDay appends nanos since midnight as hh:mm:ss with nine
// fixed fractional digits, the width of timestampLayout's fraction.
// [owner, 2026-10-17, Rationale row 5]
// The hour runs to 24, because DuckDB answers TIME '24:00:00'.
// [probe, session 2026-10-17]
func appendTimeOfDay(dst []byte, nanos int64) []byte {
	// style: doc comment alone + inline note at the arithmetic
	// because: one arithmetic split into fields, the reason for it is
	//          one line
	//
	// at the split: time.Format was rejected, because it wraps
	//   86400 seconds to 00:00:00 [probe; Rationale row 6]
	// h, m, s, ns := nanos/3600e9, nanos/60e9%60, nanos/1e9%60, nanos%1e9
}
```

```go
// timeNanosReader returns the reader of a's values as nanoseconds since
// midnight, whatever unit the field declares.
func timeNanosReader(a *array.Time64) func(i int) int64 {
	// style: doc comment alone
	// because: the twin of nanosReader for time64
}
```

The shared integer appenders also live here, because both formats
write an integer's text the same way and the cell closures differ only
in the null token:

```go
// signed is the part of an arrow-go signed integer array a cell writer
// reads. Each width is its own struct, so a type switch case per width
// passes its array here. [numeric_generic.go:346-478]
type signed[T int8 | int16 | int32 | int64] interface {
	IsNull(i int) bool
	Value(i int) T
}

// unsigned is signed for the unsigned widths.
type unsigned[T uint8 | uint16 | uint32 | uint64] interface {
	IsNull(i int) bool
	Value(i int) T
}
	// style: doc comment alone
	// because: two constraint types, the reason is one sentence
	//
	// rejected: one case body per width, because seven copies of the
	//   same closure differ only in the type [Rationale row 7]
```

`int64` keeps its existing case and does not move to the generic path.
The executor may fold it in if the case body becomes identical. The
commit says so if it does.

### `internal/encoding/json.go`

```go
// jsonColumn is one column with its JSON encoding: its name, its type
// as the Arrow type's name, and the function that appends cell i as a
// JSON value, null included. [owner, 2026-10-17, Rationale row 3]
type jsonColumn struct {
	name string
	kind string
	cell func(dst []byte, i int) []byte
}
	// style: doc comment alone
```

```go
// jsonSigned returns the appender of a signed integer column: a bare
// number, null for null. [AGENTS.md, JSON and NDJSON]
func jsonSigned[T int8 | int16 | int32 | int64](a signed[T]) func(dst []byte, i int) []byte

// jsonUnsigned returns the appender of an unsigned integer column. A
// uint64 above 2^53 is still a bare number, and a client that parses
// numbers as doubles loses digits. [owner, 2026-10-17, Rationale row 8]
func jsonUnsigned[T uint8 | uint16 | uint32 | uint64](a unsigned[T]) func(dst []byte, i int) []byte
	// style: doc comment alone
	// because: one closure each
```

```go
// jsonCell returns the type word and the appender of column a. The type
// switch runs once per column, so appending a cell is one call.
// [json.go:81-85]
// The word is the Arrow type's name: a symbol column arrives as utf8
// and reports utf8, and a count arrives as int64. [owner, 2026-10-17]
// A type without a case is ErrUnsupportedType, which names the column.
// [error.go:10-18]
func jsonCell(f arrow.Field, a arrow.Array) (jsonColumn, error) {
	// style: doc comment alone + inline note at each new case
	// because: one type switch, the shape of csvCell
	//
	// c := jsonColumn{name: f.Name, kind: f.Type.Name()}
	//   - the per-case `c.kind = "..."` assignments are deleted
	//     [Rationale row 3]
	// at *array.Int8, Int16, Int32: jsonSigned [commit 4]
	// at *array.Uint8 .. Uint64: jsonUnsigned [commit 4]
	// at *array.Float32: null for null, NaN and the infinities, as for
	//   float64, and otherwise jsontext.AppendFloat at bitSize 32, so
	//   the text is float32's own shortest round trip [Rationale row 9]
	// at *array.Boolean: true or false [commit 6]
	// at *array.Decimal128: the exact text of Value(i).ToString(scale)
	//   as a bare number, so the wire is lossless [owner, 2026-10-17,
	//   Rationale row 4]
	// at *array.Date32: a string in dateLayout [owner, 2026-10-17]
	// at *array.Time64: a string through appendTimeOfDay [owner,
	//   2026-10-17]
}
```

### `internal/encoding/csv.go`

```go
// csvCell returns the function that writes cell i of column a as CSV
// text, with the empty field for null. Each type is written the way CSV
// readers expect: an integer of any width and a shortest round-trip
// float as plain text, a decimal as its exact text, a bool as true or
// false, a timestamp in timestampLayout, a date in dateLayout, a time
// of day with nine fractional digits, a string as its own bytes, a blob
// as standard base64. [csv.go:24-33; owner, 2026-10-17]
// NaN and the infinities of float32 and float64 become the empty field.
// [csv.go:43-49; Rationale row 9]
// The rest of the existing doc comment stays.
func csvCell(f arrow.Field, a arrow.Array) (func(i int) string, error) {
	// style: doc comment alone
	// because: one type switch, the shape csvCell already has
	//
	// at *array.Int8 .. Int32: csvSigned, strconv.FormatInt
	// at *array.Uint8 .. Uint64: csvUnsigned, strconv.FormatUint
	// at *array.Float32: strconv.FormatFloat(.., 'g', -1, 32)
	// at *array.Boolean: strconv.FormatBool
	// at *array.Decimal128: Value(i).ToString(scale)
	// at *array.Date32: dateLayout
	// at *array.Time64: appendTimeOfDay into a string
}
```

`csvSigned` and `csvUnsigned` mirror `jsonSigned` and `jsonUnsigned`,
in `csv.go`, above `csvCell`, with a doc comment alone.

### `internal/encoding/encode_test.go`

`wordOf` is deleted. `checkCells` compares `got.kind` with
`f.Type.Name()`.

```go
// wantValid reports whether cell i of a is a value on the wire. A null
// is not, and neither is a NaN or an infinity of a float column, which
// every text format writes as null. [json.go:97-104, csv.go:43-49]
func wantValid(a arrow.Array, i int) bool
	// style: doc comment alone
	// because: one switch on the two float types
	//
	// - rejected: drawing only finite floats, because the float32 null
	//   rule would then be pinned nowhere [Rationale row 10]
```

```go
// checkCells compares one parsed column with the column that was
// encoded: the name, the wire type when the format carries one, the
// validity of every cell, and every value parsed back from its text.
func checkCells(t failer, f arrow.Field, want arrow.Array, got wireColumn) {
	// style: doc comment alone + inline note at the new cases
	// because: one type switch, a case per type
	//
	// validity uses wantValid in place of IsValid [Rationale row 10]
	// at the narrow signed widths: strconv.ParseInt, compared as int64
	// at the unsigned widths: strconv.ParseUint, compared as uint64
	// at *array.Float32: strconv.ParseFloat(.., 32), compared as float32
	// at *array.Boolean: strconv.ParseBool
	// at *array.Decimal128: decimal128.FromString with the column's
	//   precision and scale, compared as Num [decimal128.go:235]
	// at *array.Date32: time.Parse(dateLayout), days since the epoch
	// at *array.Time64: parseTimeOfDay, compared with timeNanosReader
}
```

```go
// parseTimeOfDay parses hh:mm:ss.nnnnnnnnn and checks its width: nine
// fixed fractional digits. The hour may be 24. [appendTimeOfDay]
func parseTimeOfDay(s string) (int64, error)
	// style: doc comment alone
	// because: the inverse of appendTimeOfDay
	//
	// - rejected: time.Parse, because it refuses hour 24 [Rationale row 6]
```

```go
// drawScalar draws a column of type dt and n rows, about one in eight
// of them null.
// Dates run from 0001-01-01 to 9999-12-31, because the four-digit year
// is the form dateLayout writes. [Rationale row 11]
// Times run from 00:00:00 to 24:00:00 inclusive. [probe]
// Floats include NaN and the infinities. [Rationale row 10]
func drawScalar(t *rapid.T, dt arrow.DataType, n int) arrow.Array
	// style: doc comment alone + inline note at the decimal draw
	// because: a type switch of generators
	//
	// at the decimal: the unscaled value is a drawn digit string of at
	//   most the precision's length, through big.Int, so it always fits
	//   [decimal128.go:79]
```

```go
// drawScalarBatch draws one batch of n rows with one column per
// DuckDB scalar the live round trip cannot generate: int8, int16,
// int32, uint8, uint16, uint32, uint64, float32, one decimal128 of
// drawn precision and scale, bool, date32, time64[us] and time64[ns].
// [Verified facts, probe]
func drawScalarBatch(t *rapid.T) arrow.RecordBatch
	// style: doc comment alone
```

```go
// TestScalarsTextRoundTrip checks that the three text encoders write
// every DuckDB scalar so that the standard library parses it back to
// the value encoded, nulls included, without a cluster.
func TestScalarsTextRoundTrip(t *testing.T) {
	// style: doc comment alone + step comments where the purpose is not
	//        evident
	// because: the shape of TestTextRoundTrip [encode_test.go:278-303]
	//
	// - the batch is drawn in memory, because these types reach the
	//   encoders only through DuckDB, and the table fixture writes
	//   QuasarDB tables [Rationale row 12]
	// - checkText(JSON), checkText(NDJSON), checkText(CSV), as
	//   TestTextRoundTrip does
}
```

`TestEdgeCells` keeps its values. Its JSON words become `float64`,
`utf8` and `binary`. The file's header comment gains one sentence:
the scalar property draws the DuckDB types in memory.

### `internal/encoding/decode_test.go`

`tableCol` at line 441 says `"type":"utf8"`. The decoder does not read
the member, so this keeps the hand-written bodies in the wire's
vocabulary and proves nothing new.

### `internal/httpapi/errors_test.go`

```go
		// at the row: a list is a type no text encoder writes
		// [internal/encoding/AGENTS.md]
		"sql: unsupported type": {http.MethodPost, "/api/v2/sql", "SELECT [1]", bearer, http.StatusInternalServerError, ""},
```

### `internal/httpapi/sql_test.go` (new)

```go
// TestSQLScalars posts one row of every DuckDB scalar to /api/v2/sql
// and checks that the JSON result names each column by its Arrow type.
// [owner, 2026-10-17, Rationale row 12]
// It proves that DuckDB answers the types the encoders write, in the
// words the encoders write them. [probe]
func TestSQLScalars(t *testing.T) {
	// style: doc comment alone + step comments
	// because: one request and one comparison
	//
	// 1. the query casts one literal to each type, and NULL stands
	//    alone, because a bare NULL is int32 [probe]
	//    SELECT 1::TINYINT, 1::SMALLINT, 1, 1::UTINYINT, 1::USMALLINT,
	//    1::UINTEGER, 1::UBIGINT, 1.5::FLOAT, sum(1), 1.50::DECIMAL(5,2),
	//    true, DATE '2026-06-11', TIME '24:00:00', NULL
	// 2. the answer is 200 in application/json
	// 3. the column words are, in order: int8, int16, int32, uint8,
	//    uint16, uint32, uint64, float32, decimal, decimal, bool,
	//    date32, time64, int32
	// 4. the cells are the expected text: 1.5, 1, 1.50, "2026-06-11",
	//    "24:00:00.000000000", null for the last column
}
```

Package `httpapi`, white-box, through `newServer` (`query_test.go:42`),
after the file order of the other `_test.go` files.

### Document rows

`internal/encoding/AGENTS.md`, The seam, the wire types bullet (line
66), rewritten:

```
- the query and the table reader answer the binding's five types:
  int64, float64, timestamp[ns] without a zone, utf8 and binary with
  max_width metadata [existing bullet]
- the sql endpoint answers DuckDB's types [Verified facts, probe]
- the text encoders write those five and DuckDB's scalars: every
  signed and unsigned integer width, float32, decimal128, bool, date32
  and time64, and a timestamp of any unit [commits 4-8]
- every field is nullable, and an all-null column keeps its type
  [existing bullet]
- any other type is ErrUnsupportedType, which names the column, and
  never a panic [existing bullet]
```

The text formats, the shared-text bullet (line 89): the two files
share `ErrUnsupportedType`, the timestamp text, the date text, the
time-of-day text and the integer constraint types (`signed`,
`unsigned`). These texts are facts about the wire. [Rationale row 13]

The text formats, the JSON and NDJSON bullet (line 98), rewritten:

```
- int64 and every other integer width are a bare number, and a decimal
  is its exact text as a bare number [owner, 2026-10-17]
- a client that parses JSON numbers as doubles loses digits past 2^53
  in an int64, a uint64 or a decimal, and a client that needs every
  digit uses the Arrow format [owner, 2026-10-17]
- float32 and float64 go through jsontext.AppendFloat at their own
  width, and NaN and the infinities are null [existing; Rationale row 9]
- a bool is true or false [commit 6]
- a date and a time of day are strings in their layouts [owner,
  2026-10-17]
- the type word is the Arrow type's name (DataType.Name()): int64,
  float64, utf8, binary, timestamp, int32, uint64, float32, decimal,
  bool, date32, time64 [owner, 2026-10-17]
- a symbol answers as utf8 and a count as int64 [existing, renamed]
```

The CSV bullet gains: every integer width as plain text, float32 at its
own shortest round trip, a decimal as its exact text, a bool as true
or false, a date and a time of day in their layouts.

The Tests section gains: the DuckDB scalars have one property,
`TestScalarsTextRoundTrip`, that draws a batch in memory, because those
types reach the encoders only through DuckDB.

`internal/httpapi/AGENTS.md:90-92`, the sql bullet's last sentences:

```
- a column type no text encoder writes (a list, for one) is 500 before
  the first byte, and internal/encoding/AGENTS.md lists the types the
  encoders write [commit 4]
- a caller casts in SQL to one of those types or asks for Arrow
  [existing, adjusted]
```

`internal/olap/AGENTS.md:23`, the last bullet, rewritten:

```
- a query answers DuckDB's Arrow types, and the sql endpoint answers
  them as DuckDB converts them [owner, 2026-10-17, Rationale row 14]
- a TIMESTAMPTZ arrives as a UTC instant tagged with the instance's
  TimeZone, which defaults to the host's zone, and the text encoders
  write it in UTC [probe]
- a TIMETZ arrives as a plain time64 without its offset [probe]
- internal/encoding/AGENTS.md lists the types the text encoders write
  [commit 4]
```

`docs/log.md`, Current state: Next loses its first two items, and
`Last updated` is the day of the commit. The one entry is the plan's
deletion (`docs/AGENTS.md`, The log), in the same commit.

## 4. Rationale

| #   | decision                                                                                          | why                                                                                    | rejected, and why                                                                                                                                                                        | gained                                                                                                                   | given up                                                                                                                                                       | settled by                                               |
| --- | ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| 1   | Remove the qdb-duck pin and name no ref                                                           | The pin's own comment says to delete it once #17 merges, and #17 merged                | Pinning `refs/heads/master` explicitly: the plugin already falls back to master, and `.buildkite/AGENTS.md` keeps `by_project` for temporary branch pins only                            | one less moving part in the pipeline                                                                                     | none                                                                                                                                                           | `.buildkite/pipeline.py:176-178`, `.buildkite/AGENTS.md` |
| 2   | The text encoders learn all DuckDB scalars                                                        | The owner chose the scalar set                                                         | int32 and decimal only: TINYINT, uint64 and booleans would keep failing. Scalars plus nested: recursive cell writers, about twice the unit                                               | `SELECT 1`, `SELECT NULL`, `sum()`, booleans, dates and times answer                                                     | nested types still answer 500                                                                                                                                  | owner, 2026-10-17                                        |
| 3   | The JSON type word is `DataType.Name()` for every column, the old five renamed                    | The owner asked for one vocabulary, and chose Arrow's names                            | QuasarDB words extended: uint64 and the narrow integers would report a width they lack. DuckDB SQL names: the encoders do not know DuckDB, and the query endpoint never uses those words | the word equals what the Arrow wire and Flight SQL report, and the word table goes away                                  | a result's word differs from the table schema's word for the same column (`float64` against `double`), and every JSON client that reads the word sees a rename | owner, 2026-10-17                                        |
| 4   | A decimal is its exact text, a bare number in JSON                                                | The owner chose the lossless wire                                                      | A quoted string: a decimal would read differently from every other number. Conversion to double: precision lost on the server                                                            | no digit is lost on the wire                                                                                             | a naive JSON client loses digits, accepted, and Arrow is the exact path                                                                                        | owner, 2026-10-17                                        |
| 5   | A date is `2006-01-02`, a time of day is `15:04:05.000000000`                                     | The owner chose ISO with a fixed width                                                 | A date as a midnight timestamp: the wire would claim an instant and a zone. Trimmed digits: a variable width, unlike the timestamp text                                                  | both parse with any ISO 8601 reader, and the width is fixed like the timestamp's                                         | none                                                                                                                                                           | owner, 2026-10-17                                        |
| 6   | The time of day is formatted arithmetically, and the test parses it the same way                  | DuckDB answers `TIME '24:00:00'`                                                       | `time.Format` and `time.Parse`: the first wraps 24:00:00 to 00:00:00, and the second refuses hour 24                                                                                     | 24:00:00 survives the wire                                                                                               | a few lines of arithmetic in place of a library call                                                                                                           | probe, session 2026-10-17                                |
| 7   | One generic cell writer per sign over a `signed` or `unsigned` constraint                         | arrow-go gives each width its own struct, so the switch needs a case per width         | one closure body per width: seven copies differing only in the type                                                                                                                      | each case is one line                                                                                                    | two small constraint types in `encoding.go`                                                                                                                    | proposal                                                 |
| 8   | uint64 is a bare JSON number                                                                      | It is the same trade as decimal                                                        | a quoted string, for the same reason as row 4                                                                                                                                            | one rule for every integer                                                                                               | a naive client loses digits past 2^53                                                                                                                          | owner, 2026-10-17 (the precision answer)                 |
| 9   | float32 is written at bitSize 32, and NaN and the infinities are null                             | 0.1 then writes as `0.1`, not `0.10000000149011612`, and the null rule matches float64 | widening to float64 first: the text would carry digits the value never had                                                                                                               | the text is float32's own shortest round trip                                                                            | none                                                                                                                                                           | proposal                                                 |
| 10  | The property draws NaN and the infinities, and the test's validity check counts them as null      | the float32 null rule is pinned in the property                                        | drawing finite floats only: the float32 null rule would be pinned nowhere                                                                                                                | one property covers the null rule                                                                                        | `checkCells` gains `wantValid`, also used by the live round trip, which never draws NaN                                                                        | proposal                                                 |
| 11  | The property draws dates in years 1 to 9999                                                       | `dateLayout` is the four-digit form                                                    | DuckDB's full range: outside years 1 to 9999 the text is Go's formatter's output (year 0, a minus sign, five digits), which the test's `time.Parse` refuses                              | the property pins the form every reader parses                                                                           | dates outside years 1 to 9999 are written but not pinned                                                                                                       | proposal                                                 |
| 12  | The tests are a drawn in-memory property plus one sql check                                       | The owner chose them                                                                   | byte pins only: the extremes would be as covered as a hand list                                                                                                                          | value coverage across every type without a cluster, and the sql check proves DuckDB answers the types the encoders write | none                                                                                                                                                           | owner, 2026-10-17                                        |
| 13  | The date layout, the time-of-day appender and the integer constraints are shared in `encoding.go` | The date and time text are facts about the wire, as the timestamp text is              | a copy per format: two homes for one wire fact                                                                                                                                           | one home for each text                                                                                                   | the shared-text rule of `internal/encoding/AGENTS.md` grows                                                                                                    | proposal                                                 |
| 14  | The sql endpoint answers DuckDB's types as DuckDB converts them                                   | When a client queries through DuckDB, DuckDB's standards are what the endpoint answers | normalizing DuckDB's conversion (setting the instance's TimeZone, carrying the TIMETZ offset)                                                                                            | the server adds no rules of its own on the DuckDB path                                                                   | a TIMETZ loses its offset, and a TIMESTAMPTZ carries the host zone in its Arrow type                                                                           | owner, 2026-10-17                                        |
| 15  | Only the types DuckDB answers are added                                                           | No other producer of batches exists                                                    | every Arrow scalar (float16, decimal32/64/256, date64, time32, duration, large and view strings): code nothing produces                                                                  | each new case is reachable                                                                                               | a future producer of those types adds its cases                                                                                                                | proposal                                                 |

## 5. Knowledge

| commit | where the whys land                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2      | `_download_options` docstring loses the pin sentence. Row 1 needs no comment, because the code no longer pins anything, and `.buildkite/AGENTS.md` already says when a pin is used                                                                                                                                                                                                                                                                          |
| 3      | `jsonColumn` and `jsonCell` doc comments (row 3). `internal/encoding/AGENTS.md`, the type-word sentence (row 3, the word list and the symbol rename). `TestEdgeCells` and `decode_test.go` words                                                                                                                                                                                                                                                            |
| 4      | `signed`, `unsigned` doc comments with the rejected alternative (row 7). `jsonSigned`, `jsonUnsigned` (row 8), `csvSigned`, `csvUnsigned`. `jsonCell` and `csvCell` cases. `internal/encoding/AGENTS.md`: the wire types bullet rewritten (rows 2, 15), the integer and precision sentences (rows 4, 8). `internal/httpapi/AGENTS.md` sql bullet and `internal/olap/AGENTS.md` last bullet point at the encoding list. `errors_test.go` row moves to a list |
| 5      | `jsonCell` and `csvCell` float32 cases (row 9). `internal/encoding/AGENTS.md`, the float sentences                                                                                                                                                                                                                                                                                                                                                          |
| 6      | the bool cases. `internal/encoding/AGENTS.md`, the bool sentences                                                                                                                                                                                                                                                                                                                                                                                           |
| 7      | the decimal cases (row 4). `internal/encoding/AGENTS.md`, the decimal sentences and the precision trade-off with Arrow as the exact path (row 4)                                                                                                                                                                                                                                                                                                            |
| 8      | `dateLayout`, `appendTimeOfDay` with the rejected `time.Format` (rows 5, 6), `timeNanosReader`. The date and time cases. `internal/encoding/AGENTS.md`, the shared-text bullet (row 13) and the date and time sentences                                                                                                                                                                                                                                     |
| 9      | `wantValid` (row 10), `parseTimeOfDay` (row 6), `drawScalar` (rows 10, 11), `drawScalarBatch`, `TestScalarsTextRoundTrip` (row 12). The test file's header. `internal/encoding/AGENTS.md`, Tests                                                                                                                                                                                                                                                            |
| 10     | `TestSQLScalars` doc comment and step comments (row 12)                                                                                                                                                                                                                                                                                                                                                                                                     |
| 11     | `internal/olap/AGENTS.md`, the DuckDB-conversion sentences (row 14)                                                                                                                                                                                                                                                                                                                                                                                         |
| 14     | `docs/log.md` Current state, and the entry naming where this plan's facts went                                                                                                                                                                                                                                                                                                                                                                              |

Every Rationale row lands: 1 in commit 2, 2 and 15 in commit 4, 3 in
commit 3, 4 in commits 4 and 7, 5 and 6 in commit 8, 6 again in commit
9, 7 and 8 in commit 4, 9 in commit 5, 10, 11 and 12 in commit 9, 12
again in commit 10, 13 in commit 8, and 14 in commit 11.

## 6. How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write every commit's comments and document rows from their
   specifications in sections 3 and 5, in the same commit as the code.
   Every listed claim appears once, at the place the specification
   names, in the project's prose (root `AGENTS.md`, Prose). The
   evidence brackets stay in this plan. No claim is added without
   evidence. A claim the code contradicts is reported in the
   build-stage message and not written.
3. A why that arises while building and is not in this plan is written
   where it is decided, with its evidence, or asked of the owner
   through the question tool before the commit.
4. Each commit builds and passes `make lint` and the tests of the
   packages it touches (`go test -p 1` with the qdbd pair up, per
   `internal/AGENTS.md`, Tests). Run `python3 .buildkite/pipeline.py
check` with `BUILDKITE_BRANCH` set after commit 2.
5. Run `npx prettier --write` on every Markdown file a commit touches.
6. After the last code commit, run `/doc-discipline all
.buildkite/pipeline.py internal/encoding internal/httpapi
internal/olap/AGENTS.md`, and make one small commit per finding.
7. Then run `/doc-discipline check` on the same paths with
   `docs/duckdb-scalars-plan.md`. Fix every "Unlanded from the plan"
   finding with a further small commit, or report it with the reason it
   was dropped.
8. The last commit deletes this plan, after its facts have landed, and
   appends the one log entry `docs/AGENTS.md`, Plans, asks for:
   `duckdb-scalars-plan.md deleted; facts moved to <where>`.

## 7. Commits

1. `docs(plan): duckdb-scalars-plan.md, the qdb-duck pin goes and the text encoders write every duckdb scalar`
   (this document, already committed)
2. `ci(buildkite): qdb-duck downloads from its master, the freebsd branch pin is removed`
3. `refactor(encoding): the json type word is the arrow type's name for every column`
4. `feat(encoding): the text encoders write every signed and unsigned integer width`
5. `feat(encoding): the text encoders write float32 at its own shortest round trip`
6. `feat(encoding): the text encoders write bool`
7. `feat(encoding): the text encoders write a decimal as its exact text`
8. `feat(encoding): the text encoders write date32 as an iso date and time64 with nine digits`
9. `test(encoding): a property encodes drawn batches of the duckdb scalars and reads every cell back`
10. `test(httpapi): /api/v2/sql answers one row of every duckdb scalar in its arrow words`
11. `docs(olap): the sql endpoint answers duckdb's types as duckdb converts them`
12. `/doc-discipline all` on the paths of section 6, one commit per
    finding.
13. `/doc-discipline check` on the same paths with this plan, one commit
    per unlanded finding.
14. `docs(log): next drops the qdb-duck pin and the encoder widening; duckdb-scalars-plan.md deleted`
15. Verify: push `sc-19567/rr-duckdb-scalars`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again. The mechanics (API-created build,
    `ignore_pipeline_branch_filters`, the full 40-character SHA, then
    reprioritizing the scheduled jobs) are in `.buildkite/AGENTS.md`.

## 8. Open questions and recommendations

1. Should `int64` move onto the generic signed path for symmetry?
   Recommendation: yes, if its case body becomes identical, and the
   commit says so.
2. Should dates outside years 1 to 9999 get a pin of the text Go
   writes? Recommendation: no, because DuckDB users rarely store them,
   and Arrow carries them exactly.
3. Should the stale memory note on the qdb-duck FreeBSD pin be deleted
   after the merge? Recommendation: yes (owner, 2026-10-17).
