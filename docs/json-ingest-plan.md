# Columnar JSON ingestion -- Plan

Status: draft. This plan is scaffolding for one unit: `POST /api/v2/rows`
reads the columnar JSON the query and the table reader answer. It is
deleted when the unit lands, and what must outlive it moves first to
`internal/encoding/AGENTS.md` and to the comments in `json.go`
(`docs/AGENTS.md`, Plans).

## 1. Outcome

When the unit lands:

- `encoding.JSON` implements `Decoder` in `internal/encoding/json.go`.
  It reads either shape the JSON encoder writes: the one result
  `{"columns":[...]}` of `Encode` (the query) and the top-level array of
  such results of `EncodeStream` (the table reader).
- The NDJSON and JSON decoders share one per-table accumulator,
  `jsonTable`, with one cell path, `appendCell`, and one `$table` reader,
  `jsonTableName`.
- JSON is a member of `codecs` in `decode_test.go`, so it runs the
  decode round trip, the fault property and the interleaved property.
  `TestDecodeFormatFaults` carries the JSON faults a batch cannot
  express.
- `decoders` in `internal/httpapi/rows.go` maps `application/json` to
  `encoding.JSON{}`, so `TestRoundtrip` draws JSON as an ingest format
  with no test code of its own.
- `internal/encoding/AGENTS.md`, The seam, states the JSON decoder's
  rules, and `docs/log.md`, Next, starts at the basic e2e test.

Left for later units, each named in `docs/log.md`, Next: the basic e2e
test (`e2etool gen`, `flow.sh`, `make test-flow`), the surface
definition, `40.test-e2e.sh`, the bench unit and the upstream
`qdb-api-go` report. The v1 tables/columns JSON shape stays an open
question of the brief (`docs/brief.md`, Open questions 2).

## 2. Verified facts

- The JSON encoder writes one result as
  `{"columns":[{"name":..,"type":..,"data":[..]},..]}` and a stream as a
  top-level array of results with a comma between them; an empty stream
  is `[]` and a nil batch is `{"columns":[]}`
  [`internal/encoding/json.go`, the `JSON` doc comment and `JSON.EncodeStream`].
- The table reader answers through `EncodeStream`, the query through
  `Encode` [`internal/httpapi/AGENTS.md`, Handlers; `json.go`, `JSON.Encode` and `JSON.EncodeStream`].
- The JSON cell text equals the NDJSON cell text: a number literal, a
  string, base64 for a blob, the RFC 3339 timestamp string, `null` for
  null and for NaN and the infinities [`json.go`, the shared
  `jsonCell`].
- `ndjsonText` plus `textAppender` already turn a JSON value into a
  builder append, with the kind check first [`json.go`, `ndjsonText` and
  `ndjsonTable.appendRow`; `encoding.go`, `textAppender`].
- `ndjsonTable` and `newNDJSONTable` type a table by a set of names, in
  the table's order, with the check against the first table's types
  [`json.go`, `ndjsonTable` and `newNDJSONTable`]. Nothing in them is NDJSON-specific except the
  name.
- `json.UnmarshalDecode` with `json.RejectUnknownMembers(true)` refuses
  a member the struct does not declare, with the member's name in the
  message. A `jsontext.Decoder` reads several top-level values in a row
  without complaint, so a trailing value has to be checked by the
  decoder: `PeekKind` reports it and `ReadToken` returns `io.EOF` only
  at the end [scratch module, go1.27.0, 2026-10-11].
- `jsontext` rejects a duplicate member name in one object by default
  (`AllowDuplicateNames` defaults to false) [`$GOROOT/src/encoding/json/jsontext/options.go:46-60`].
  A duplicate column name is a duplicate `name` value across objects of
  an array, which this does not catch.
- golangci-lint, under the repository's `.golangci.yml`, reports nothing
  for a struct field that only `json` unmarshalling writes and no code
  reads [scratch module, 2026-10-11].
- `TestRoundtrip` draws the ingest content type from the keys of
  `decoders` [`internal/httpapi/roundtrip_test.go:75`], and the request
  body is the encoder of that type over the tables
  [`roundtrip_test.go:76`].
- `TestErrorRows` has the row `"ingest: json body"`, which posts `[]` as
  `application/json` and expects 415 [`internal/httpapi/errors_test.go:79`].
  Under this unit that body is a valid empty stream.
- The ingest body is capped on the wire by `maxIngestBytes` [`internal/httpapi/rows.go`, `maxIngestBytes`].

## 3. Design

All of it is in `internal/encoding/json.go`, below the NDJSON decoder's
section where it already sits, except the test and handler entries at
the end. File order is the order below.

### 3.1 The shared accumulator (commit 2)

`ndjsonTable` becomes `jsonTable` and `newNDJSONTable` becomes
`newJSONTable`. The bodies keep their numbered steps. `ndjsonText` becomes
`jsonText`. Only the doc comments change where they name NDJSON.

```go
// jsonTable accumulates the rows of one table for either JSON decoder.
// [json.go, `ndjsonTable`; this unit]
// It holds the batch's schema, one builder per field with its text
// appender, and whether each field reads a JSON number or a JSON string.
// [`ndjsonTable`]
type jsonTable struct {
	// style: doc comment alone
	// because: a struct whose fields are named by the doc comment
}
```

```go
// newJSONTable types the body's columns by the table's schema: $timestamp
// first, then the named data columns in the table's order. [`newNDJSONTable`]
// A name the table lacks is ErrInvalidRows. [`newNDJSONTable` step 2]
// A table after the first must agree with the first on every field's
// type, or ErrInvalidRows names both tables. [`newNDJSONTable` step 3]
// The error of schemaOf passes through as is. [`newNDJSONTable` step 1]
func newJSONTable(name string, names map[string]bool, schemaOf model.SchemaOf, first *jsonTable) (*jsonTable, error) {
	// style: doc comment + numbered overview + step comments
	// because: the body is the four steps newNDJSONTable already narrates,
	//   unchanged except for the name
	//
	// The table's order is used because a JSON object's members carry no
	//   order of their own, and the columnar decoder takes the same order so
	//   both decoders give one batch shape. [`newNDJSONTable`; this unit]
	// 1.-4. as today [`newNDJSONTable`]
}
```

```go
// jsonText returns the cell text of a JSON value for the text appender:
// a number's literal as it stands, or a string's content unquoted.
// A value of the other kind is an error. [`ndjsonText`]
func jsonText(v jsontext.Value, numeric bool) (string, error) {
	// style: doc comment alone
	// because: one switch on the value's kind
}
```

```go
// appendCell appends the JSON value v to field i of t. An absent (nil)
// or null v appends null, except in field 0, $timestamp, which cannot be
// null. [`ndjsonTable.appendRow` step 2]
// The error names the column. [`ndjsonTable.appendRow` step 2]
func (t *jsonTable) appendCell(i int, v jsontext.Value) error {
	// style: doc comment + inline note at the null branch and at the kind check
	// because: three outcomes on one value, each with a reason that is local
	//   to its line
	//
	// at the null branch: an absent member and a null member read alike,
	//   because the encoder writes null as a member and a sparse NDJSON body
	//   leaves it out [`ndjsonTable.appendRow` step 2]
	// at i == 0: $timestamp cannot be null, because the writer would refuse
	//   the row at the push [`ndjsonTable.appendRow` step 2]
	// at the kind check: the kind check comes first, so a string in a
	//   number column is named as such and not as a parse error on its
	//   quotes [`ndjsonTable.appendRow` step 2]
}
```

```go
// appendRow appends one object, as the map of its raw members, to the
// builders of t. A column the object lacks or names null is null, and a
// null or absent $timestamp is an error. A member outside the column
// list is an error naming it. [`ndjsonTable.appendRow`]
func (t *jsonTable) appendRow(row map[string]jsontext.Value) error {
	// style: doc comment + numbered overview + step comments
	// because: it keeps the two steps it narrates today
	//
	// 1. no member outside the list, so a typo is a fault and not a silent
	//    drop [`ndjsonTable.appendRow` step 1]
	// 2. each field of the batch in order, through appendCell, so a column
	//    the object does not name is reached as well [`ndjsonTable.appendRow` step 2]
	//    for i, f := range t.schema.Fields() { t.appendCell(i, row[f.Name]) }
}
```

```go
// jsonTableName returns the table a $table value names. It must be a
// string and not empty. [`ndjsonTableOf`]
func jsonTableName(v jsontext.Value) (string, error) {
	// style: doc comment alone
	// because: two checks, each named in the contract
}
```

`ndjsonTableOf` keeps its absent-member check and calls `jsonTableName`
for the rest [`ndjsonTableOf`]. `NDJSON.Decode` changes only in the
type names.

### 3.2 The result as the body carries it (commit 3)

```go
// jsonResultColumn is one column object of a columnar result as the
// body carries it. [json.go, the `JSON` doc comment]
// The type member is read and not checked, because the reader's schema
// types the batch, as it does for CSV and NDJSON. [Rationale "type member"]
type jsonResultColumn struct {
	// style: doc comment alone
	// because: a struct the standard library fills
	//
	// fields: Name `json:"name"`, Type `json:"type"`, Data []jsontext.Value `json:"data"`
	// Data holds each cell raw, so the cell path is NDJSON's. [3.1 appendCell]
}

// jsonResult is one columnar result as the body carries it. [json.go, the `JSON` doc comment]
type jsonResult struct {
	// style: doc comment alone
	// because: a struct the standard library fills
	//
	// field: Columns []jsonResultColumn `json:"columns"`
}
```

### 3.3 The results of a body (commit 3, extended in commit 4)

```go
// jsonResults yields the results of a body: the one result the query
// answers, or each result of the array the table reader answers.
// [Rationale "both shapes"; json.go, `JSON.Encode` and `JSON.EncodeStream`]
// An empty array yields nothing. [json.go, `JSON.EncodeStream`]
// A body that is neither, a result the standard library cannot read, a
// member a result does not declare, and a value after the body are
// ErrInvalidRows, which names the result, counted from 1, and keeps the
// reader's cause in the chain. [this unit; `NDJSON.Decode`]
// The sequence ends after its first error.
func jsonResults(dec *jsontext.Decoder) iter.Seq2[jsonResult, error] {
	// style: doc comment + numbered overview + step comments
	// because: the shape of the body decides the path, and three things
	//   fail on the way
	//
	// The results are read one at a time, each whole, so the decoder holds
	//   one result in memory and never the body's other results.
	//   [Rationale "one result whole"]
	//
	// 1. peek the first token: an object is one result, an array is a
	//    stream of results; anything else is the body's fault.
	//    [json.go, `JSON.Encode` and `JSON.EncodeStream`]
	//    (commit 3 has the object arm only; commit 4 adds the array arm)
	// 2. read each result through json.UnmarshalDecode with
	//    RejectUnknownMembers, so a misspelled "columns" or "data" is a
	//    fault and not an empty result. [Rationale "unknown members";
	//    scratch module 2026-10-11]
	//    - rejected: the default, which ignores an unknown member, because
	//      a misspelled key would then ingest nothing and answer 200
	//      [Rationale "unknown members"]
	// 3. in the array arm, read '[' and then results until PeekKind says
	//    ']', and read the ']'. [jsontext API]
	// 4. after the body, ReadToken must answer io.EOF, because jsontext
	//    reads several top-level values without complaint and a second
	//    value would otherwise be dropped silently. [scratch module
	//    2026-10-11]
	// Both error chains stay reachable: the sentinel decides the status,
	//   and the reader's cause reveals a body-size cap the HTTP layer set.
	//   [`NDJSON.Decode`; rows.go, `handleIngestRows` step 6]
}
```

```go
// readJSONHeader returns the data columns of a result by name and its
// row count. [this unit]
// $table and $timestamp are required, a name appears once, and every
// column has the same number of cells, or the result is ErrInvalidRows.
// [Rationale "column list"; `readNDJSONHeader`]
func readJSONHeader(res jsonResult) (map[string]bool, int, error) {
	// style: doc comment alone
	// because: one loop over the columns, whose three checks the contract
	//   names
	//
	// at the duplicate check: jsontext refuses a duplicate member inside
	//   one object, and a duplicate column is a repeated "name" across
	//   the array's objects, which it does not see. [Verified facts,
	//   AllowDuplicateNames]
	// at the end: {"columns":[]}, the nil batch's result, names no $table
	//   and is refused like a CSV header without $table.
	//   [Rationale "nil result"; csv.go, `readCSVHeader`]
}
```

### 3.4 The decoder (commit 3, unchanged by commit 4)

```go
// Decode implements Decoder. The body is the columnar JSON the query
// answers, one result, or the array of results the table reader
// answers. [Rationale "both shapes"]
// The first result fixes the column list, $table and $timestamp among
// them, and every later result names the same columns in any order.
// [Rationale "column list"]
// The type member of a column is not read. [Rationale "type member"]
func (JSON) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// style: doc comment + numbered overview + step comments
	// because: a result is read, checked and then routed row by row into
	//   per-table builders that span the results, and four things fail on
	//   the way
	//
	// A result is columnar and $table is one of its columns, at any
	//   position, so a cell cannot be routed before the $table column of its
	//   result has been read. The decoder therefore reads one result whole
	//   and then routes it row by row. [Rationale "one result whole"]
	//   - rejected: a decoder that appends each cell as it reads the
	//     tokens, because the $table column may come last, and it would
	//     parse JSON by hand where the standard library parses for NDJSON.
	//     [Rationale "one result whole"]
	//
	// 1. each result of the body comes from jsonResults [3.3]
	//    for res, err := range jsonResults(jsontext.NewDecoder(r))
	// 2. every result passes readJSONHeader; the first result's names fix
	//    the column list, and a later result that names other columns is
	//    ErrInvalidRows naming the result, because one body is one column
	//    list. [Rationale "column list"; internal/encoding/AGENTS.md, The seam]
	//    names, rows, err := readJSONHeader(res); maps.Equal(first, names)
	// 3. index the result's columns by name once, so each row looks its
	//    cells up without a scan. [this unit]
	// 4. route each row: its $table cell names the table through
	//    jsonTableName; a table seen for the first time is typed through
	//    newJSONTable and checked against the first table, and the lookup's
	//    own error passes through unwrapped so the handler can answer 404.
	//    [`NDJSON.Decode` step 2; rows.go, `handleIngestRows` step 6]
	// 5. append the row's cell of every field of the table through
	//    appendCell, in the batch's order. Every field has a column,
	//    because newJSONTable picked the fields from the column list.
	//    [3.1]
	//    The ctx is checked once per chunkRows rows, counted across the
	//    results, like the other decoders. [encoding.go, `checkChunk`]
	//    Errors name the row counted from 1 across the body, like NDJSON's.
	//    [`NDJSON.Decode`]
	// 6. at the end every table becomes one batch, in first-seen order; on
	//    any error every builder is released and no batch is returned.
	//    [internal/encoding/AGENTS.md, The seam]
}
```

### 3.5 Tests (commits 5 and 6)

`decode_test.go`:

```go
// codecs pairs every format that decodes with its encoder. A new decoder
// joins the slice and thereby the round trip. [decode_test.go, `codecs`]
var codecs = /* CSV, NDJSON, Arrow, JSON */
	// style: doc comment alone
	// because: the existing comment covers the new member
	//
	// JSON's Encode writes one result and EncodeStream an array, so the
	//   round trip and the fault property exercise the object arm and the
	//   interleaved property the array arm. [`TestDecodeRoundTrip`, `TestDecodeFaults`, `TestDecodeInterleaved`]
```

`TestDecodeFormatFaults` gains these rows, each with a one-line step
comment saying which rule it pins, under the existing `ints` schema
[`TestDecodeFormatFaults`]:

```go
// "json: not json":            `{"columns":`                -> the standard library's parse error is the body's fault
// "json: not a result":        `1`                          -> the body is neither a result nor an array of results
// "json: unknown member":      a result plus `"x":1`         -> RejectUnknownMembers
// "json: trailing value":      a result, then `{}`           -> the EOF check after the body
// "json: ragged columns":      $table with 1 cell, i with 2  -> one row count per result
// "json: duplicate column":    two columns named "i"         -> a name appears once
// "json: columns differ":      `[r1, r2]`, r2 without "i"    -> the first result fixes the list
// "json: nil result":          `{"columns":[]}`              -> names no $table
```

The empty array `[]` decoding to no tables is not pinned by a test of
its own [Rationale "nil result", given up].

`internal/httpapi/errors_test.go`: the row `"ingest: json body"` becomes
`"ingest: text body"`, posting `csvHeader` as `text/plain` and expecting 415.
No comment changes. The round trip draws JSON with no test change
[`roundtrip_test.go:75`].

### 3.6 The handler (commit 7)

```go
// decoders maps each media type the ingest accepts to its decoder.
// [rows.go, `decoders`]
var decoders = /* + encoding.JSONContentType: encoding.JSON{} */
	// style: doc comment alone
	// because: a map literal the existing comment covers
```

### 3.7 Document rows

`internal/encoding/AGENTS.md`, The seam, a new bullet after the NDJSON
bullet (commit 3, the array sentence in commit 4):

```
- The JSON decoder reads the one result the query answers or the array
  of results the table reader answers, one result at a time through the
  standard library. [Rationale "both shapes", "one result whole"]
- The first result fixes the column list, $table and $timestamp among
  them, and a later result names the same columns. [Rationale "column list"]
- Within a result a column name appears once and every column has the
  same number of cells. [3.3]
- A member a result does not declare and a value after the body are
  ErrInvalidRows. [Rationale "unknown members"; 3.3 step 4]
- The type member is not read, because the reader's schema types the
  batch. [Rationale "type member"]
- A cell reads as an NDJSON cell, through the jsonTable both decoders
  share. [3.1]
```

The existing NDJSON bullet's clause "the batch carries those data
columns in the table's order, because a JSON object's members carry no
order of their own" stays and gains "and the JSON decoder takes the
same order" (commit 2).

`docs/log.md`, Current state (commit 8): Next item 1 is struck and the
items renumber; `Last updated` stays 2026-10-11 or moves to the day.
The plan's deletion (commit 11) appends one entry:
`json-ingest-plan.md deleted; facts moved to internal/encoding/AGENTS.md and json.go`.

## 4. Rationale

| decision           | why                                                                                                                                                         | rejected, and why                                                                                         | gained                                                  | given up                                                                                                              | settled by                                                     |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| both shapes        | the owner asked for "the shape the query and the table reader answer", and the reader answers the array                                                     | the object only, which refuses the reader's own output                                                    | every JSON response the server writes is an ingest body | the decoder has two arms                                                                                              | `docs/log.md` Next 1; owner, 2026-10-11                        |
| one result whole   | `$table` may sit at any column position, so a cell cannot be routed before its result's `$table` column is read; the standard library parses, as for NDJSON | a token-level decoder that appends cells as it reads, which needs `$table` first and hand-written parsing | the standard library parses; the code routes only       | one result is held in memory as raw cells, one allocation per cell, bounded by the ingest body cap (`maxIngestBytes`) | proposal, approved with this plan                              |
| type member        | the reader's schema types the batch, as for CSV and NDJSON, and a kind mismatch already fails at the cell                                                   | checking the word against the encoder's word for the reader's type, which needs a word map in the decoder | no second type vocabulary in the decoder                | a wrong type word with matching cells is accepted                                                                     | proposal, approved with this plan                              |
| column list        | one body is one column list, as one Arrow stream has one schema and NDJSON's first object fixes the list                                                    | a per-result column list, which lets two results of one table disagree                                    | the same rule in every decoder                          | a later result cannot add or drop a column                                                                            | proposal, approved with this plan                              |
| unknown members    | a misspelled `columns` or `data` would otherwise decode as an empty result and answer 200                                                                   | the standard library's default, which ignores unknown members                                             | a typo is a 400                                         | a client cannot add members of its own                                                                                | proposal, approved with this plan                              |
| nil result         | `{"columns":[]}` names no `$table`, like a CSV header without one; `[]` names no table, like an empty NDJSON body                                           | answering zero tables for `{"columns":[]}`                                                                | the header rule of the other decoders holds             | `[]` decoding to nothing is not pinned by a test                                                                      | proposal, approved with this plan                              |
| shared accumulator | the JSON cell text equals the NDJSON cell text, so one table type and one cell path serve both, as one `textAppender` serves CSV and NDJSON                 | a second table type for JSON, a copy of NDJSON's                                                          | one cell path for the JSON family, inside `json.go`     | the NDJSON type and constructor are renamed                                                                           | `internal/encoding/AGENTS.md`, The seam; proposal for the name |
| error row change   | `[]` as `application/json` becomes a valid empty body, so the 415 row needs another type                                                                    | dropping the row, which loses the 415 on the ingest                                                       | the 415 row stays                                       | none                                                                                                                  | this unit                                                      |

## 5. Knowledge

- Commit 2: `jsonTable` and `newJSONTable` doc comments name both
  decoders (Rationale "shared accumulator"). `appendCell` carries the
  null and kind notes moved from `appendRow` step 2. The NDJSON bullet of
  `internal/encoding/AGENTS.md` gains the JSON decoder's order clause.
- Commit 3: `jsonResultColumn` carries Rationale "type member".
  `jsonResults` carries "unknown members" at step 2 and the EOF reason
  at step 4. `readJSONHeader` carries "nil result" and the duplicate
  note. `JSON.Decode` carries "both shapes" in its doc comment, "one
  result whole" in its overview with the rejected alternative, and
  "column list" at step 2. `internal/encoding/AGENTS.md` gains the JSON
  decoder bullet without the array sentence.
- Commit 4: `jsonResults` gains the array arm (step 1 and 3) and the
  AGENTS bullet gains the array sentence.
- Commit 5: the `codecs` comment states which property exercises which
  arm.
- Commit 6: each new row carries the rule it pins.
- Commit 7: none beyond the code.
- Commit 8: `docs/log.md`, Current state.
- Commit 11: the log entry for the plan's deletion. Every Rationale row
  has landed in a comment or the AGENTS bullet by then; the "error row
  change" row lands in commit 7's subject and needs no comment, because
  the row's name states it.

## 6. How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write every commit's comments and document rows from sections 3 and
   5, in the same commit as the code, in the shape of the root
   `AGENTS.md`, "Code comments", and its Prose rules. Every listed claim
   appears once, at the place named. No claim is added without evidence.
   A claim the code contradicts is reported in the build-stage message
   and not written.
3. A reason that arises while building and is not in this plan is
   written where it is decided, with its evidence, or asked of the owner
   through the question tool before the commit.
4. After the last code commit, run
   `/doc-discipline all internal/encoding internal/httpapi/rows.go internal/httpapi/errors_test.go`
   and make one small commit per finding.
5. Before the build-stage message, run
   `/doc-discipline check internal/encoding internal/httpapi/rows.go internal/httpapi/errors_test.go docs/json-ingest-plan.md`.
   Fix every "Unlanded from the plan" finding with a small commit, or
   report it with the reason it was dropped. Quote the report in the
   build-stage message.
6. Run `make lint` and `go test -p 1 ./internal/encoding/... ./internal/httpapi/...`
   (qdbd up) before each commit that touches Go code.
7. Run `npx prettier --write` on every Markdown file touched.

## 7. Commits

1. `docs(plan): json-ingest-plan.md, the columnar JSON decoder and its ingest`
2. `refactor(encoding): ndjsonTable becomes jsonTable, with appendCell and jsonTableName shared by both JSON decoders`
3. `feat(encoding): the JSON decoder reads one columnar result into one batch per table`
4. `feat(encoding): the JSON decoder reads the table reader's array of results`
5. `test(encoding): JSON joins the decode round trip, the fault property and the interleaved property`
6. `test(encoding): the JSON body's format faults`
7. `feat(httpapi): the ingest reads a columnar JSON body`
8. `docs(log): columnar JSON ingestion landed; Next item 1 struck`
9. `/doc-discipline all` over the paths in section 6, one commit per
   finding.
10. `/doc-discipline check` against this plan, one commit per unlanded
    finding.
11. `docs(log): json-ingest-plan.md deleted; facts moved to internal/encoding/AGENTS.md and json.go`
12. Verify: push `sc-19567/rr-json-ingest`, build its head in Buildkite,
    wait for the result. Green: report the build number. Red: fix with
    further small commits on this branch, push, build again. The
    mechanics are in `.buildkite/AGENTS.md`.

## 8. Open questions and recommendations

1. The base `sc-19567/rest-rewrite` is three commits ahead of origin,
   and the Verify step pushes the feature branch only. Recommendation:
   push the base together with the merge, in the merge stage.
2. Rationale rows "one result whole", "type member", "column list",
   "unknown members" and "nil result" are proposals. Recommendation: as
   written; the plan's approval settles them.
