# Rendering Encoders -- Plan

Status: draft. The working plan for one M1 unit: the JSON, NDJSON and
CSV encoders over the Arrow record batch, through the seam the Arrow
encoder already implements, pinned by one format-equivalence property
test. Verified facts land here dated; progress is in `docs/log.md`; the
document is deleted when the unit lands (`docs/AGENTS.md`, Plans). The
conventions below are not architecture: when the unit lands they live as
the comments of the code that implements them and as the encoder rule in
`internal/AGENTS.md`, never as an ADR.

## Purpose

`internal/encoding` holds the `Encoder` seam and the Arrow encoder, a
pass-through that interprets no column type. The three rendering formats
the brief's data plane lists (`docs/brief.md`, "Data plane") are the
first code that must know a type to render a cell. Outcome of the unit:
three more `Encoder` implementations over the same batch, one shared
cell vocabulary, and a property test that decodes all four formats of
one generated table and compares every cell with what was written.

Out of scope here, each a later M1 unit (`docs/log.md`, Next): the
`POST /api/v2/query` handler and its `Accept` negotiation, the flushing
writer, gzip, and the e2e `text/csv` full-table target. That target
compares against `reproduce.csv`, a `qdb_export` file: no header,
every string quoted, naive timestamps. Byte identity with `qdb_export`
is a non-concern; the e2e comparison normalizes (skips the header,
strips the quotes and the `Z`) before `compare_csv`.

## What the encoders receive

The batch is the binding's (`internal/AGENTS.md`, Code): every field
nullable; the types `int64`, `float64`, naive `timestamp[ns]`, `utf8`
and `binary`; an all-null column keeping its table type; a nil batch for
a statement without a result set. The encoders own nothing: they never
release the batch, never flush, never log. A type outside the five is an
encode error naming the column, never a panic: the binding's vocabulary
can grow (array-typed results, `docs/brief.md`, Risks) and the wire must
refuse loudly rather than guess.

Timestamps are UTC: the binding delivers the C API's timespec without a
zone and QuasarDB stores every timestamp in UTC.

## Cell conventions

One cell renders the same way in every format wherever the format can
carry it; where it cannot, the most widely parsed representation wins.

| wire type         | JSON / NDJSON                                  | CSV                                                   |
| ----------------- | ---------------------------------------------- | ----------------------------------------------------- |
| `int64`           | bare number                                    | bare number                                           |
| `float64`         | bare number, `jsontext.AppendFloat`            | same text                                             |
| `float64` NaN/Inf | `null`                                         | empty field                                           |
| `timestamp[ns]`   | `"2026-06-11T00:00:00.000683000Z"`             | same text, unquoted                                   |
| `utf8`            | JSON string (`jsontext.AppendQuote`)           | RFC 4180 quoting; the empty string is the empty field |
| `binary`          | base64 standard with padding, as a JSON string | same text                                             |
| null              | `null`                                         | empty field                                           |

Why each:

- `int64` as a bare number is the pass-through; every non-JavaScript
  parser reads it exactly. Values above 2^53 lose precision in browsers;
  a quoting knob is a later addition if a consumer asks.
- `float64` is written by the standard library's `jsontext.AppendFloat`:
  shortest round trip, plain notation for exponents in [-6, 21),
  exponent notation outside, byte-identical to `encoding/json`. NaN and
  the infinities are guarded before the appender, which would write
  them as the strings `"NaN"` and `"Infinity"`: they are not JSON, and
  NaN is QuasarDB's own null for doubles at the writer, so null is the
  honest reading.
- The timestamp is RFC 3339 in UTC with a fixed nine fractional digits:
  lossless to the nanosecond, universally parsed, and fixed width is
  cheaper to write than a trimmed one. Epoch nanoseconds would be the
  pure pass-through but exceed 2^53 and read as noise.
- `utf8` is escaped by the standard library's `jsontext.AppendQuote`,
  the minimal RFC 8785 escaping. A column the binding types `utf8` is
  not validated by the C API; invalid UTF-8 is replaced by U+FFFD, the
  error the escaper reports is dropped, and the response stays valid.
- `binary` is base64 as `encoding/json` renders bytes; what nearly every
  client library decodes without configuration.

Type names on the wire are QuasarDB's words: `int64`, `double`,
`string`, `blob`, `timestamp`. A `symbol` column is a `string` in a
query result (`docs/brief.md`, "/api/v2 endpoint sketch"), and a `count`
column is an `int64` (`docs/brief.md`, Compatibility contract). The
mapping from Arrow type to word is one function; an unknown type is the
encode error above.

## Body shapes

**JSON** (`application/json`) is one result, columnar:

```
{"columns":[{"name":"$timestamp","type":"timestamp","data":[...]}, ...]}
```

One object per column, keys in that order so a streaming reader knows
the type before the data. No `tables` wrapper: one query is one result,
and the table a row came from is a column like any other (`$table`).
Column-major streams naturally over the batch, every Arrow array walked
once. A nil batch is `{"columns":[]}`. No trailing newline.

**NDJSON** (`application/x-ndjson`) is one object per row, keys in
column order, one line per row, LF-terminated:

```
{"$timestamp":"2026-06-11T00:00:00.000683000Z","accountId":"LQEF226Z","id":128674892828}
```

A nil batch, or a batch with no rows, is an empty body. Keys are the
column names escaped once and reused for every row.

**CSV** (`text/csv`) is RFC 4180 with a header row, LF line endings and
minimal quoting through the standard library's `encoding/csv`:

```
$timestamp,accountId,flag,id
2026-06-11T00:00:00.000683000Z,LQEF226Z,B,128674892828
```

- The header row carries the column names; every consumer that matters
  (pandas, DuckDB, spreadsheets) expects one, and RFC 4180 allows it.
- LF, not CRLF: what Go, pandas and DuckDB write, what every reader
  accepts, and smaller. `encoding/csv`'s default.
- A field is quoted only when it holds a comma, a quote, a CR or an LF,
  `encoding/csv`'s rule. A null is the empty field, and so is the empty
  string: `encoding/csv` never quotes an empty field, and every reader
  that matters folds the two anyway (pandas to NaN, DuckDB to NULL).
- A nil batch is an empty body: no columns means no header line. A
  batch with columns and no rows is the header line alone.

## Approach

### `internal/encoding`

- `cell.go`: the shared vocabulary. The Arrow-type-to-word function; the
  timestamp and base64 renderers; the float guard in front of
  `jsontext.AppendFloat`. Dense code, so it carries the walk-through
  comments (`internal/AGENTS.md`, Code).
- `json.go`: `JSON` and `NDJSON`. Both write bytes through one
  `bufio.Writer` from `strconv.AppendInt`, `jsontext.AppendFloat` and
  `jsontext.AppendQuote` into a scratch buffer reused per row; no
  encoder object, no per-cell allocation (the Arrow string, binary and
  timestamp accessors are views, not copies). The type switch runs once
  per column and yields a per-column cell appender, so the hot loop is
  a call per cell. The `bufio.Writer` is flushed once at the end of
  `Encode`: internal buffering, not the HTTP flush, which stays the
  handler's.
- The ctx is checked every `chunkRows` rows, the one constant the Arrow
  encoder slices its record batches by (today `arrowBatchRows`, renamed
  and shared): a chunk is a record batch on the Arrow wire and a run of
  rows between two ctx checks on the rendered wires.
- `csv.go`: `CSV` over `encoding/csv`'s `Writer`, standard library
  first. One record slice of `len(columns)` strings is reused per row;
  a cell becomes its string through the shared vocabulary. The per-cell
  string is the cost of the standard writer's interface, accepted; the
  e2e budgets (M4) measure it.
- Content types: `application/json`, `application/x-ndjson`,
  `text/csv`, next to `ArrowContentType`. The `charset` parameter and
  the `header=present` parameter are the handler's business (a later
  unit), not the encoder's.

### Tests

- `encoding_test.go`: the fixture the Arrow test already holds
  (`newCluster`, `run`, `checkColumn`, `checkIndex`, `typed`,
  `checkValues`), moved so every format test shares it.
- `arrow_test.go` keeps only the Arrow-specific decode and its two
  tests.
- `equivalence_test.go`: the format-equivalence property test the brief
  names (`docs/brief.md`, Testing doctrine, item 1). One `rapid.Check`
  per run: a generated table through `qdbtest/table`, one batch through
  `Cluster.Query`, encoded four ways; each decoded with the standard
  library (`encoding/json`, `encoding/csv`, the IPC reader) back to a
  per-column, per-cell view; every cell compared with the table that was
  written, validity included. Each decoder is a test helper that knows
  its format's null and its cell text, nothing else.
- Nil-batch and no-row cases pinned per format inside the same file.
- `go test -p 1 ./internal/encoding/...` against the live fixture; one
  cluster per test function, `rapid.Check` inside it.

### Documents

- `internal/AGENTS.md`, Code, the encoders bullet: gains the cell
  conventions and the body shapes as rules once the plan is deleted.
- `docs/log.md`: In flight while the unit runs; Next 1 loses the
  encoders; one entry when the plan is deleted.

## Commits

Each commit builds, passes lint and its tests on its own.

1. `docs(encoders-plan): the JSON, NDJSON and CSV encoders over the batch`
2. `docs(log): the rendering encoders are in flight`
3. `feat(encoding): a cell renders per wire type, null aware`
4. `feat(encoding): the CSV encoder`
5. `test(encoding): the Arrow round-trip helpers become the encoders' shared fixture`
6. `feat(encoding): the NDJSON encoder`
7. `feat(encoding): the JSON encoder`
8. `test(encoding): the four formats of one batch decode to the table that was written`
9. `docs(internal): the rendering rules join the encoder rule`
10. `docs(log): encoders-plan.md deleted; facts moved to internal/AGENTS.md`

## Decision log (2026-09-11)

| Decision                                                 | Why                                                                                                                                                          | Rejected                                                                                                                                                 |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| One unit: three encoders plus the equivalence test       | owner decision; the shared cell vocabulary is most of the work; the test needs all four formats                                                              | CSV alone first, for its external oracle                                                                                                                 |
| JSON shape and cell conventions live in code comments    | owner decision; conventions, not architecture                                                                                                                | an ADR for the v2 JSON payload                                                                                                                           |
| Streaming JSON through the `jsontext` appenders          | owner decision; no per-cell allocation on the hot loop; the standard library's escaper and number formatter, no ADR (`docs/brief.md`, Development standards) | `json/v2` `jsontext.Encoder` (token state per cell); hand-rolled escaping; the exponent rule hand-written over `strconv`; an ADR for adopting `jsontext` |
| CSV through `encoding/csv`                               | owner decision; the standard library first, the per-cell string accepted                                                                                     | hand-rolled RFC 4180 writer into the buffered writer                                                                                                     |
| No alignment with `qdb_export`'s CSV                     | owner decision; the tool exports raw tables, not queries; user-friendliness and speed decide; the e2e full-table comparison normalizes                       | always-quoted strings, no header, nine-digit naive timestamps                                                                                            |
| Header row, LF, minimal quoting                          | what every reader accepts                                                                                                                                    | CRLF; no header; always-quoting                                                                                                                          |
| The empty string is the empty field, like null           | owner decision; `encoding/csv` never quotes an empty field; readers fold the two anyway                                                                      | `""` for the empty string (needs a hand-rolled writer)                                                                                                   |
| No `tables` wrapper around the JSON result               | owner decision; one query is one result; the table is a column (`$table`)                                                                                    | the legacy tables/columns shape                                                                                                                          |
| NaN and the infinities render as null                    | not JSON; NaN is the writer's double null                                                                                                                    | `"NaN"` strings; an encode error                                                                                                                         |
| Timestamps as RFC 3339 UTC, nine fixed fractional digits | lossless, universal, fixed width writes fastest                                                                                                              | epoch nanoseconds (above 2^53, unreadable); trimmed fractions                                                                                            |
| An unlisted Arrow type is an encode error                | the vocabulary can grow upstream; refuse loudly, never guess                                                                                                 | a panic; rendering through `fmt`                                                                                                                         |
| One shared `chunkRows` constant                          | owner decision; one number for the Arrow batch and the ctx stride                                                                                            | a second constant per encoder                                                                                                                            |
