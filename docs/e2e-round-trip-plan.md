# The v2 e2e round trip -- Plan

Status: draft. This plan is the unit that defines what the v2 e2e tests
and makes the harness test it. It is unit 3 of `docs/e2e-v2-flow-plan.md`,
"Units under this plan", and it is deleted when the unit lands
(`docs/AGENTS.md`, Plans).

## Outcome

When the unit lands:

- The v2 e2e is called the round trip in every document, the harness and
  the tool: `tests/e2e/round-trip.sh`, `make test-round-trip`,
  `actual/round-trip/<cluster>/`, `roundTripSchema`, and the prose of
  the brief, ADR-0014, `docs/e2e.md`, the log's Current state and the
  live plan, which becomes `docs/e2e-v2-round-trip-plan.md`.
- `docs/e2e.md`, "The v2 round trip" is the definition of what the e2e
  tests, and the harness tests that and nothing more: on each cluster,
  one login, four tables, one per body format, the generated rows
  ingested into each as its format, and every table read through the
  table reader and queried through `SELECT *`, in every format under
  identity and gzip, every response decoded to CSV and compared byte for
  byte with the generated CSV.
- `e2etool gen` writes `rows.csv`, `rows.ndjson`, `rows.arrow` and
  `rows.json` through the package's encoders, and `e2etool tocsv` turns
  a JSON, NDJSON or Arrow response into CSV through the package's
  decoders and its CSV encoder.
- The round trip passes locally on both clusters and in Buildkite, built
  through the API on this branch.

It leaves for later units:

- `scripts/cicd/40.test-e2e.sh`, the CI step that runs
  `make test-round-trip` in every platform's build step and closes M2
  (`docs/log.md`, Next 2).
- The empty read, zstd on the response wire, request-body codings, push
  and deduplication modes, and a body carrying several tables, which
  stay Go tests (Rationale).

## Verified facts

- A bare `SELECT *` answers `$timestamp`, `$table`, then the data
  columns in creation order, in the binding's types (probe through
  `Cluster.Query` on the insecure qdbd, session of 2026-10-12).
- The decoders put `$timestamp` first and the data columns after it,
  drop `$table`, and look every name up in the schema `SchemaOf` answers
  (`internal/encoding/AGENTS.md:19-24`, `internal/encoding/csv.go:208-216`,
  `internal/encoding/json.go:380-392`). The JSON decoder orders the data
  columns by the schema, not the body (`internal/encoding/AGENTS.md`, The
  seam, the NDJSON and JSON bullets).
- The JSON decoder reads the one result a query answers or the array of
  results the table reader answers (`internal/encoding/json.go:786-790`).
- The decoders' tests answer `SchemaOf` with the schema of
  `table.WithTable`, `$table` first (`internal/encoding/decode_test.go:45-47`,
  `:77`), and `table.WithTable` puts a `$table` column of the table's name
  in front of `tbl.Batch`, or of an empty batch of the schema when the
  batch is nil (`internal/qdbtest/table/table.go:247-262`, `:322-336`).
- A table with no rows gets no batch from a decoder
  (`internal/encoding/AGENTS.md:22-23`); the unit never decodes an empty
  body, because the empty read is dropped.
- The query route takes the query text as the body under
  `Content-Type: text/plain` or `application/sql`
  (`internal/httpapi/query.go:98-99`) and is compressed and
  bearer-authenticated like the read (`internal/httpapi/query.go:144`).
- `Accept-Encoding` is read in the client's order, the first of `gzip`,
  `zstd` and `identity` wins, and the status is held until the first body
  byte, so a result under gzip carries `Content-Encoding: gzip`
  (`internal/httpapi/AGENTS.md:102-108`). The in-process round trip
  compares `Content-Type` with the encoder's media type exactly, with no
  charset (`internal/httpapi/roundtrip_test.go:141-143`).
- The in-process round trip ingests Arrow bodies built like the tool's,
  the fixture batch with `$table` in front (`roundtrip_test.go:225-230`,
  `drawnIngest`), so the body's types equal the reader's by construction
  (`internal/encoding/AGENTS.md`, The seam, the Arrow bullet).
- `golden.sh` dumps headers with `curl -D`, decompresses with
  `gunzip -c`, and requires `gunzip` (`tests/e2e/golden.sh:36`, `:92-97`).
  The agents carry `curl`, `jq` and `gunzip` (`docs/e2e.md:338`).
- The generated rows carry no empty string and no NaN, and the edge rows
  sit ahead of the drawn rows with `$timestamp` ascending
  (`tests/e2e/tools/e2etool/main.go:41-56`, `:123-170`). A query answers
  rows ascending by `$timestamp` (`internal/qdbtest/table/table.go:225-227`),
  so the query's rows come back in the generated order.
- The bulk reader after an async push answers none of the rows or some
  twice (`internal/AGENTS.md:86`); the round trip keeps the default push
  mode, `fast`.
- Every mention of the word "flow" for the v2 e2e, with its file
  (`grep -rni flow` of 2026-10-12, excluding Flight SQL, "flows through"
  and the bench's "CLI and flow" heading): `docs/brief.md:784`, `:855`,
  `:887`, `:889`; `docs/log.md:14`, `:26-27`, `:44-47`, `:59-66`;
  `docs/e2e.md:4`, `:18`, `:108`, `:118-208`, `:331`, `:360-369`,
  `:439-450`; `docs/e2e-v2-flow-plan.md` whole; `docs/AGENTS.md:79`;
  `docs/adr/0014-v2-e2e-generated-flow.md:1`, `:28-31`, `:53-54`, `:63`,
  `:80`, `:94`; `docs/adr/README.md:19`; `tests/e2e/Makefile:12-13`,
  `:58`, `:65`, `:85`, `:94`, `:207-241`; `tests/e2e/README.md:3`,
  `:15`, `:29-31`, `:42-52`; `tests/e2e/flow.sh` whole;
  `tests/e2e/AGENTS.md:20-31`, `:50`; `tests/e2e/tools/e2etool/main.go:1-2`,
  `:27-31`, `:51`, `:123`, `:138`, `:144`; `internal/httpapi/AGENTS.md:127`.
  Dated log entries keep their wording, because they are append-only.

## Design

In the order the files have. The sketches carry the shape and the
claims; the build stage writes the prose with the code in view.

### `tests/e2e/tools/e2etool/main.go`

```go
// Command e2etool is the Go tool of the v2 e2e round trip (docs/e2e.md,
// "The tool"). gen writes the rows the round trip ingests, in every body
// format, and tocsv turns a response into CSV. [docs/e2e.md, The tool]
// The tool may import any package of this repository, the cgo binding
// included. [ADR-0014 4]
//
//	e2etool gen --rows N --seed S --out DIR [--table NAME]   write DIR/rows.{csv,ndjson,arrow,json}, print the row count
//	e2etool tocsv --format json|ndjson|arrow [--table NAME]  stdin to CSV on stdout
package main
```

```go
// roundTripSchema is the table the round trip creates: one column of
// every type, named v_<type>, the symtable named after the table and the
// column. The driver's create body spells the same names. [round-trip.sh create_body]
func roundTripSchema(name string) table.Table {
	// style: doc comment alone
	// because: it assigns a literal; flowSchema renamed, body unchanged
}
```

```go
// bodyFiles is the file each body format's encoder writes, keyed by the
// format name the driver's matrix uses. [round-trip.sh BODY_FORMATS]
var bodyFiles = []struct {
	name string
	enc  encoding.Encoder
}{{"csv", encoding.CSV{}}, {"ndjson", encoding.NDJSON{}}, {"arrow", encoding.Arrow{}}, {"json", encoding.JSON{}}}
```

```go
// writeBodies writes rec as rows.<format> in dir for every body format,
// through the package's encoders, so each file is its encoder's dialect
// and the tool formats no cell of its own. [flow plan, Owner decisions 4]
func writeBodies(dir string, rec arrow.RecordBatch) error {
	// style: doc comment alone
	// because: one loop over bodyFiles calling writeBody; nothing to tell
	//
	// at the loop: the four files carry the same batch, $table included,
	//   so the bodies differ in format only [docs/e2e.md, The v2 round trip, step 3]
}
```

```go
// writeBody writes rec to dir/<file> through enc, and closes the file
// on every path.
func writeBody(dir, file string, enc encoding.Encoder, rec arrow.RecordBatch) error {
	// style: doc comment alone
	// because: writeCSV generalized over the encoder; one create, one
	//   encode, one close
}
```

```go
// gen is the gen subcommand. It prints the row count it wrote, edge rows
// included, so the driver asserts the ingest's answer against the one
// writer of that number. [round-trip.sh ingest]
func gen(args []string) error {
	// style: doc comment + numbered overview + step comments
	// because: four steps whose order matters, and a count on stdout
	//   means every file is complete [main.go:184-189 today]
	//
	// Every failure ends the command before anything is printed [main.go:185]
	//
	// 1. parse the flags; the defaults are the Makefile's [Makefile ROWS, e2e_csv]
	// 2. draw the batch, alive until after the writes [generate]
	// 3. write every body file [writeBodies]
	// 4. print the row count, the one line the driver reads [round-trip.sh run_cluster]
}
```

```go
// decoders is the decoder of each response format tocsv reads, keyed by
// the format name the driver's matrix uses. CSV is absent, because the
// driver compares a CSV response raw. [docs/e2e.md, The v2 round trip, step 4]
var decoders = map[string]encoding.Decoder{"json": encoding.JSON{}, "ndjson": encoding.NDJSON{}, "arrow": encoding.Arrow{}}
```

```go
// toCSV decodes one response body of the round trip's table named name
// from r through dec and writes it as CSV to w, in the generated file's
// column order, $table first. [docs/e2e.md, The v2 round trip, The comparator]
func toCSV(w io.Writer, r io.Reader, dec encoding.Decoder, name string) error {
	// style: doc comment + numbered overview + step comments
	// because: the decoder drops $table and the comparison needs it back,
	//   and the schema the decoder types against is the tool's own
	//
	// The response is the table reader's or the query's answer for one
	// table, so the decoder answers one batch and the tool restores what
	// the decoder strips [encoding/AGENTS.md:19-24]
	//
	// 1. the schema the decoder types against is the round trip schema
	//    with $table in front, the shape the decoders' tests answer
	//    [decode_test.go:77, table.go:322-336]; no request to the server,
	//    because the tool holds the schema already [Rationale: schema]
	// 2. decode; one batch is expected, because a response names one table
	//    and the generated rows are never empty [main.go edgeRows]; any
	//    other count is an error naming it
	// 3. put $table back in front through table.WithTable, because the
	//    decoder drops it and the generated CSV carries it first
	//    [encoding/AGENTS.md:23, Rationale: $table]; the column order is
	//    the schema's for every format, so no reorder is needed
	//    [Verified facts, SELECT * and the JSON decoder]
	// 4. encode through the CSV encoder, so the bytes are the same dialect
	//    as rows.csv [ADR-0014 2]
}
```

```go
// tocsv is the tocsv subcommand: stdin to CSV on stdout.
func tocsv(args []string) error {
	// style: doc comment alone
	// because: flags, a lookup in decoders, one call to toCSV
	//
	// at the lookup: an unknown --format is an error naming the three
}
```

```go
func main() {
	// style: doc comment alone (none; a dispatch on os.Args[1])
	// because: two subcommands, one usage line
}
```

### `tests/e2e/round-trip.sh`

```bash
# The v2 e2e round trip (docs/e2e.md, "The v2 round trip"). It proves the
# built binary over HTTP like a client: login, create a table, ingest the
# generated rows, read and query them back in every format and coding.
# The expected value of every step is the generated input, so nothing is
# captured and nothing is audited (ADR-0014). Error rows are Go tests,
# never steps here. [header; flow.sh:1-13 today]
#
#   round-trip.sh run --insecure-url <url> --secure-url <url> --tool <e2etool> [--rows N] [--seed S]
#
# Working files go to actual/round-trip/<cluster>/. The script needs
# bash, curl, jq, cmp and gunzip. [require_command lines]
```

```bash
# request <out_dir> <name> <curl args...>: one request, its body in
# <out_dir>/<name>.body, its headers in <out_dir>/<name>.headers, its
# status on stdout. [flow.sh:48-56 today, plus -D]
request() {
	# style: doc comment alone
	# because: one curl line
	#
	# at -D: the header dump is what read_back asserts content-type and
	#   content-encoding from, the way golden.sh captures headers
	#   [golden.sh:92]
}
```

```bash
# expect_header <out_dir> <name> <header> <want>: the response header
# equals want, or is absent when want is empty. [docs/e2e.md step 4]
expect_header() {
	# style: doc comment alone
	# because: one grep and one compare
	#
	# at the grep: case-insensitive, because HTTP header names are
	#   [RFC 9110 5.1]; the value is trimmed of its CR
	# at the empty want: identity carries no content-encoding
	#   [httpapi/AGENTS.md:105-106]
}
```

```bash
# compare_csv <out_dir> <name> <format> <table> <csv>: the body of
# <name>, decoded to CSV through e2etool tocsv unless it is CSV already,
# compared byte for byte with the generated CSV. [ADR-0014 2]
compare_csv() {
	# style: doc comment + numbered overview + step comments
	# because: two paths and a diff on failure
	#
	# 1. a CSV body is compared raw; any other format goes through tocsv
	#    --format <format> --table <table> into <name>.csv [docs/e2e.md step 4]
	# 2. cmp with the generated file; on a mismatch the first bytes of
	#    both are diffed, because the seed reproduces the rest [flow.sh:134-137 today]
}
```

```bash
# read_back <out_dir> <url> <token> <table> <format> <media_type> <coding> <csv>:
# GET /api/v2/tables/<table>/rows under Accept of that type and that
# coding, headers asserted, body compared with the generated CSV.
read_back() {
	# style: doc comment + numbered overview + step comments
	# because: coding, request, header assertions and the comparison
	#
	# 1. the coding's request header: identity sends no Accept-Encoding,
	#    because that is what a plain client sends [flow plan, decision log
	#    2026-09-18]; gzip sends Accept-Encoding: gzip alone, because the
	#    first listed coding wins [httpapi/AGENTS.md:103]
	# 2. the request, status 200
	# 3. content-type equals the format's media type exactly, and
	#    content-encoding equals the coding, absent for identity
	#    [roundtrip_test.go:141, httpapi/AGENTS.md:105]; nothing else in
	#    the headers is read [docs/e2e.md step 4]
	# 4. a gzip body is decompressed into <name>.body through gunzip -c
	#    first [golden.sh:97]
	# 5. compare_csv
}
```

```bash
# query_back <out_dir> <url> <token> <table> <format> <media_type> <coding> <csv>:
# POST /api/v2/query with SELECT * FROM <table> as text/plain, then the
# same assertions as read_back. [owner, 2026-10-12]
query_back() {
	# style: doc comment alone + inline note at the query text
	# because: the steps are read_back's with another request; the
	#   shared part is one helper, check_response, that read_back and
	#   query_back both call after their request
	#
	# at SELECT *: a bare SELECT * answers $table next to $timestamp and
	#   the rows ascending by $timestamp, so the decoded CSV is the
	#   generated file with no post-processing [Verified facts]
	# at text/plain: the query text is the body [query.go:98-99]
}
```

The build stage shapes `read_back` and `query_back` around one
`check_response` helper holding steps 3 to 5 above, so the assertions
are written once; the sketch above lists the claims, and the helper
carries the ones about headers and decoding.

```bash
# The matrix. One table per body format, named after it, and every
# table is read and queried in every response format under every
# coding. A format is one entry here. [docs/e2e.md, The driver]
BODY_FORMATS=("csv:text/csv" "ndjson:application/x-ndjson" "arrow:application/vnd.apache.arrow.stream" "json:application/json")
READ_FORMATS=("csv:text/csv" "ndjson:application/x-ndjson" "arrow:application/vnd.apache.arrow.stream" "json:application/json")
CODINGS=("identity" "gzip")
```

```bash
# run_cluster <name> <url> <login_body_file>: the round trip against one
# server: login once, then per body format create the table, ingest the
# generated rows in that format, and read and query them back in every
# format and coding. The seed is in every line. [flow.sh:150-172 today]
run_cluster() {
	# style: doc comment + numbered overview + step comments
	# because: the loop nest is the definition's step list
	#
	# 1. login once; the token serves every step [docs/e2e.md step 1]
	# 2. generate once per cluster into <out>/rows.*, because the four
	#    tables ingest the same rows and the tool writes every body in one
	#    run [gen]; the $table column names the body's table, so gen runs
	#    per table [main.go generate step 3]
	# 3. per body format: create, ingest rows.<format>, then every read
	#    format under every coding, read and query [docs/e2e.md steps 2-5]
}
```

### `tests/e2e/Makefile`

The variables and the target renamed: `ROUND_TRIP := $(E2E_DIR)/round-trip.sh`,
`test-round-trip`, the help lines, and the comments that say "flow". No
new variable, because the driver takes everything it needs already.

### `docs/e2e.md`, "The v2 round trip"

```
- the heading and the section's prose say round trip; one sentence says
  the round trip is TestRoundtrip's sequence driven over HTTP by curl
  against the built binary [httpapi/AGENTS.md:126-127, owner 2026-10-12]
- the steps, numbered, each named with what it asserts:
  1. login, RFC 6749 shape [unchanged]
  2. four tables, e2e_csv, e2e_ndjson, e2e_arrow, e2e_json, every
     column type, dropped first [flow plan, The flow, step 2]
  3. ingest the generated rows into each as its format, 200 with the
     row count; the four bodies differ in format only; several tables
     in one body is the Go property test's case [unchanged, plus json]
  4. read each table in every format under identity and gzip; the CSV
     response compared raw, the others through tocsv; content-type from
     the format, content-encoding from the coding; nothing else in the
     headers [unchanged]
  5. query each table with SELECT * in every format under identity and
     gzip, the same comparison; a bare SELECT * answers $table next to
     $timestamp and rows ascending by $timestamp, which the decoder and
     the tool fold into the generated order [Verified facts]
- what the round trip does not test, each with its Go test: the empty
  read (TestRoundtrip step 3), zstd on the response wire
  (compress_test.go), request-body codings and push modes
  (TestRoundtrip step 4), several tables per body (decode_test.go),
  error rows (ADR-0010 and ADR-0011 tables) [Rationale]
- The comparator paragraph: unchanged in substance, "four formats"
- The tool: gen writes the four files; tocsv reads json, ndjson or
  arrow, types it through the round trip schema, restores $table,
  writes CSV; the chunkRows clause is gone [owner, 2026-10-12, question 6]
- The driver: round-trip.sh, actual/round-trip, make test-round-trip,
  the matrix lists, one function per step kind, check_response shared
  by the read and the query
- In Buildkite and Layout: make test-round-trip, round-trip.sh
```

### `tests/e2e/README.md`, "The v2 round trip"

```
- make test-round-trip does what the definition says, in one paragraph;
  ROWS and SEED; the tool's two subcommands; the pointer to docs/e2e.md
- the usage block: make test-round-trip lines
```

### `tests/e2e/AGENTS.md`

```
- the v2 bullet says round trip, round-trip.sh, make test-round-trip,
  tocsv next to gen, roundTripSchema, and that the read and the query
  share check_response
- the dataset bullet: "the round trip creates its own e2e_* tables"
```

### `docs/log.md`, Current state

```
- M2 row and criteria: "the v2 e2e round trip"; the criteria name the
  four body formats (CSV, NDJSON, Arrow IPC, columnar JSON)
- Next: item 1 struck; item 1 becomes the CI step, "make test-round-trip"
- Handoff to M2: "The round trip drops its tables", "make
  test-round-trip loads nothing", "not of the round trip"
- one entry: the surface is defined, the round trip tests it, the name
```

## Rationale

| decision                                                                                  | why                                                                                                                                                    | rejected, and why                                                                                                                             | gained                                                                        | given up                                                                        | settled by                                                                       |
| ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| The v2 e2e is called the round trip                                                       | the Go tests already call this sequence a round trip (`TestRoundtrip`, `TestDecodeRoundTrip`), and the e2e is that round trip over HTTP                | "flow", because it named nothing the project uses elsewhere                                                                                   | one word for one concept across tests and e2e                                 | a rename across documents, the harness and the tool in this unit                | owner, 2026-10-12                                                                |
| ADR-0014 and the dated log entries keep their file names and the entries their wording    | ADR file names are stable identifiers linked from `docs/adr/README.md`, and entries are append-only; ADR prose is reworded with its decision unchanged | renaming the ADR file, because every link would move for a word                                                                               | a small diff                                                                  | the ADR's file name says "flow"                                                 | owner decision of 2026-10-07 (`docs/log.md`, entry), proposal for the file names |
| The live plan file is renamed to `e2e-v2-round-trip-plan.md`                              | it is ephemeral and its name is cited as an example in `docs/AGENTS.md:79`                                                                             | keeping the name, because a plan named flow would outlive the word                                                                            | consistency                                                                   | one `git mv` and two pointers                                                   | proposal                                                                         |
| The query endpoint is in the round trip, as `SELECT *`                                    | the owner wants the query path driven over HTTP too                                                                                                    | the table reader alone, because `TestRoundtrip` queries in process                                                                            | the query handler, its negotiation and compression proven on the built binary | 32 more requests per cluster                                                    | owner, 2026-10-12                                                                |
| The empty read is dropped                                                                 | the owner dropped it; `TestRoundtrip` step 3 reads the first table empty in every format                                                               | the empty read against the header line, because it costs a second expected value and a tocsv path for zero batches                            | one expected file, `rows.csv`                                                 | the schema path of an empty table is proven in process only                     | owner, 2026-10-12                                                                |
| Codings are identity and gzip on the response wire                                        | the agents carry `gunzip` and no zstd binary, and zstd is `compress_test.go`'s                                                                         | zstd, because it needs a binary on eight agents                                                                                               | no new agent dependency                                                       | zstd unproven from outside                                                      | owner, 2026-10-12 (`docs/e2e.md:338`)                                            |
| Request-body codings, push and deduplication modes, several tables per body stay Go tests | the owner confirmed; `TestRoundtrip` draws them and `TestRoundtripDeduplicated` covers deduplication                                                   | steps in the round trip, because each would repeat a drawn case with no new question                                                          | a round trip that is one matrix                                               | nothing driven from outside for them                                            | owner, 2026-10-12                                                                |
| tocsv types the response through the round trip schema it holds                           | the decoders need a `SchemaOf`, and the tool has the schema already, with the binding's types the reader answers                                       | a `GET /api/v2/tables/{name}` request, because the endpoint is M7's, and asking the server for the schema it must prove would be circular     | no second request, no dependency on an endpoint that does not exist           | a table name on the tocsv command line                                          | proposal                                                                         |
| tocsv restores `$table` through `table.WithTable`                                         | the decoders drop `$table`, and the generated CSV and every response carry it first                                                                    | stripping `$table` from the expected file, because the expected value stays one file; a tool-local column prepend, because the fixture has it | the comparison stays one `cmp` with `rows.csv`                                | a dependency of the tool on the fixture's `T` stand-in, which `gen` has already | proposal                                                                         |
| Headers are asserted from a `curl -D` dump                                                | `golden.sh` captures headers the same way, and `-w` formats cannot say a header is absent                                                              | `curl -w '%{content_type}'`, because it cannot assert an absent `content-encoding`                                                            | one mechanism in the harness                                                  | one more file per response                                                      | proposal                                                                         |
| gen writes the four body files in one run                                                 | the bodies are one batch through four encoders, and one run keeps one draw per table                                                                   | one file per run under `--format`, because four draws of the same seed are four times the work for the same batch                             | one command per table                                                         | four files on disk when one is read                                             | proposal                                                                         |
| gen runs once per table                                                                   | `$table` is a column of every body and names the table the row goes to                                                                                 | one run with a `$table` rewritten per table, because the tool would then format a cell                                                        | the bodies are the encoders' bytes, untouched                                 | four draws per cluster, each a few thousand rows                                | `main.go` generate step 3, flow plan Owner decisions 4                           |

## Knowledge

Per commit, where each why lands.

- Commit 2 (the word in the documents): the Rationale rows on the name,
  the ADR file names and the plan file land as the reworded prose; the
  name's reason lands in `docs/e2e.md`, "The v2 round trip", first
  paragraph (the round trip is `TestRoundtrip` over HTTP), and in
  `internal/httpapi/AGENTS.md:127` ("the in-process twin of the e2e
  round trip").
- Commit 3 (the harness rename): `round-trip.sh` header, `Makefile`
  comments, `README.md`, `tests/e2e/AGENTS.md`, `main.go` header and
  `roundTripSchema`; no new reason.
- Commit 4 (the definition): every row of "The v2 round trip" above; the
  Rationale rows on the query, the empty read, the codings and the Go
  tests land as the section's "what it does not test" list; the tool and
  driver paragraphs describe what commits 5 to 11 build, and the section
  is the definition the owner approved, written before the code.
- Commit 5 (`gen` writes four files): `bodyFiles`, `writeBodies`,
  `writeBody`, `gen` as sketched; the Rationale rows on one run and one
  run per table land in `writeBodies` and in `generate` step 3.
- Commit 6 (`tocsv`): `decoders`, `toCSV`, `tocsv`, `main` as sketched;
  the Rationale rows on the schema and on `$table` land in `toCSV` steps
  1 and 3; the Verified fact on `SELECT *` lands in `toCSV` step 3.
- Commit 7 (`request` dumps headers): the `-D` note, with the Rationale
  row on `curl -D`.
- Commit 8 (header assertions): `expect_header`, the header claims of
  `check_response`.
- Commit 9 (decode and gunzip): `compare_csv`, the gunzip claim.
- Commit 10 (`query_back`): the `SELECT *` note and the `text/plain`
  note.
- Commit 11 (the matrix): the three lists and `run_cluster` as sketched.
- Commit 12 (README and AGENTS.md): the rows sketched above.
- Commit 13 (the log): Current state as sketched, one entry.

Every Rationale row lands in commit 2, 4, 5, 6 or 7 as listed.

## How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write every commit's comments and document rows from the
   specifications in Design and Knowledge, in the same commit as the
   code, so that every listed claim appears once, at the place the
   specification names, no claim is added without evidence, and a claim
   the code contradicts is reported in the build-stage message and not
   written.
3. A why that arises while building and is not in this plan is written
   where it is decided, with its evidence, or asked of the owner through
   the question tool before the commit that needs it.
4. After the last code commit, run `/doc-discipline all tests/e2e docs/e2e.md`
   and land one small commit per finding.
5. Before the build-stage message, run
   `/doc-discipline check tests/e2e docs/e2e.md docs/e2e-round-trip-plan.md`,
   which reconciles the code against this plan and reports every reason
   the plan promised that the code and documents do not state.

## Commits

1. `docs(plan): e2e-round-trip-plan.md, the v2 e2e is the round trip and the harness tests the definition` (this document, committed)
2. `docs: the v2 e2e is the round trip in the brief, ADR-0014, e2e.md, the log's current state and the live plan`
3. `refactor(e2e): flow.sh becomes round-trip.sh, make test-round-trip runs it, and the tool's schema is roundTripSchema`
4. `docs(e2e): "The v2 round trip" is the definition: four tables, the read and the query in every format under identity and gzip`
5. `feat(e2e): e2etool gen writes rows.csv, rows.ndjson, rows.arrow and rows.json through the encoders`
6. `feat(e2e): e2etool tocsv decodes a json, ndjson or arrow body to CSV with $table in front`
7. `feat(e2e): request dumps the response headers next to the body`
8. `feat(e2e): check_response asserts content-type from the format and content-encoding from the coding`
9. `feat(e2e): check_response decodes json, ndjson and arrow through tocsv and gunzips a gzip body`
10. `feat(e2e): query_back selects the table whole and checks it like a read`
11. `feat(e2e): the matrix ingests four tables and reads and queries every format under identity and gzip`
12. `docs(e2e): README and AGENTS.md describe gen's four files, tocsv and the matrix`
13. `docs(log): the round trip is defined and the harness tests it; Next item 1 struck`
14. `/doc-discipline all tests/e2e docs/e2e.md`, one small commit per finding, then `/doc-discipline check tests/e2e docs/e2e.md docs/e2e-round-trip-plan.md`.
15. Verify: push `sc-19567/rr-e2e-round-trip`, build its head in Buildkite (`.buildkite/AGENTS.md`, Facts: API-created, branch filters bypassed, the full 40-character SHA), wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. ADR-0014 keeps its file name and its title is reworded to "the v2 e2e is a generated round trip, not a golden suite". Recommendation: yes, as the Rationale says.
2. The live plan is renamed to `docs/e2e-v2-round-trip-plan.md`. Recommendation: yes.
3. `tocsv` takes `--table` for the schema and the `$table` value, defaulting to `e2e_csv` like `gen`. Recommendation: yes, one flag the driver always passes.
