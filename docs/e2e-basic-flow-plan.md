# The basic v2 e2e flow -- Plan

Status: draft. This plan is scaffolding for unit 2 of
`docs/e2e-v2-flow-plan.md`, "Units under this plan": the basic test
the owner decided on 2026-10-11. It is deleted when the unit lands.
Anything permanent moves to `docs/e2e.md`, `tests/e2e/AGENTS.md`,
`tests/e2e/README.md` or an ADR first (`docs/AGENTS.md`, Plans).

## Outcome

When the unit lands, `make -C tests/e2e test-flow QDB_REST_BIN=<bin>`
builds `tests/e2e/tools/e2etool`, and for each cluster (insecure on
`qdb://127.0.0.1:2836`, secure on `:2838`) starts one REST server from
the binary, logs in, drops and creates one table `e2e_csv` with one
column of every type, ingests `rows.csv` through `POST /api/v2/rows`,
reads the table back through `GET /api/v2/tables/e2e_csv/rows` as CSV
and compares the two files with `cmp`. `rows.csv` is ten thousand drawn
rows plus the edge rows, written by `encoding.CSV` over one record
batch the table fixture draws under a seed the driver prints. The
driver is `tests/e2e/flow.sh`. The README and the `AGENTS.md` of
`tests/e2e` describe what exists, ADR-0014's Consequences no longer say
the CSV path is proven against a source the encoders did not touch, and
the log's Next item 1 is struck.

It leaves for later units, each named in `docs/log.md`, Next: the
empty read, the NDJSON, Arrow IPC and columnar JSON bodies, the JSON,
NDJSON and Arrow read formats, `e2etool tocsv`, the gzip coding, the
rewrite of `docs/e2e.md`, "The v2 flow" (the surface definition) and
`scripts/cicd/40.test-e2e.sh`.

## Verified facts

- The four routes the driver calls are registered: the login
  (`internal/httpapi/login.go:96`), the create and the delete
  (`internal/httpapi/tables.go:130-131`), the ingest
  (`internal/httpapi/rows.go:165`) and the table reader
  (`internal/httpapi/read.go:103`). Verified 2026-10-12.
- The login body is `{"username","secret_key"}`
  (`internal/httpapi/login.go:17-18`), the shape of the user security
  file `start-services.sh` writes at the repo root, which
  `internal/qdbtest/qdbtest.go:47-50` reads as `user_private.key`
  next to `cluster_public.key`. The answer is `access_token`,
  `token_type`, `expires_in` (`login.go:38-40`).
- The create body is `name`, `shard_size` in milliseconds with no
  default, and `columns` of `name`, `type`, `symtable`
  (`internal/httpapi/tables.go:23-37`). The vocabulary is `blob`,
  `double`, `int64`, `string`, `symbol`, `timestamp`, and a symtable
  belongs to a symbol column and to no other
  (`internal/qdb/table.go:19-46`). The create answers 201 with no body
  and 409 on a taken name; the delete answers 204 and 404
  (`tables.go:97-125`).
- The ingest answers `{"rows","tables","parse_ms","push_ms"}`
  (`internal/httpapi/rows.go:68-71`), under a 64 MiB cap on the wire
  (`rows.go:25`), with `fast` as the default push mode, which the bulk
  reader sees at once (`internal/AGENTS.md`, Code, the ingest rule).
- The table reader answers `$table` and `$timestamp` first, then the
  data columns, rows ascending by `$timestamp`, as CSV under
  `Accept: text/csv` (`internal/httpapi/AGENTS.md`, Handlers). The
  CSV encoder writes a stream of batches as the first batch's header
  and then every batch's rows (`internal/encoding/csv.go:143`), and
  the empty field is null (`internal/encoding/AGENTS.md`, The text
  formats).
- The fixture's draw already keeps every bound the generated rows need
  (`internal/qdbtest/table/table.go:66-105`): no `MinInt64`, no NaN,
  no empty string or blob, no NUL, alphanumeric symbols, and a
  strictly ascending index from `indexStart` (2020-01-01) with a drawn
  step (`table.go:120-129`). `generateRows` draws the row count (0 to 40) and the null density itself (`table.go:144-158`).
- `rapid.Custom(fn).Example(seed)` draws one value of `fn` under a seed
  outside a test (`vendor/pgregory.net/rapid/combinators.go:20`,
  `generator.go:81`). The `*rapid.T` it passes implements the fixture's
  `T` (`table.go:33-38`).
- `encoding.CSV{}.Encode(ctx, w, rec)` writes one batch as a header row
  and rows (`internal/encoding/csv.go:123`). The hand-built edge batch
  of `internal/encoding/encode_test.go:395` (`TestEdgeCells`) is the
  shape the tool's edge rows take.
- `common.sh` holds `start_server`, `stop_server`, `require_qdbd`,
  `check_service_running` and exports `TZ=UTC` and `EXE_EXT`
  (`tests/e2e/common.sh:30-34,68,136-162`). `require_qdbd` probes the
  port of `QDB_URI` alone (`common.sh:96-99`).
- The Makefile's server-under-test block is `REST_PORT` `40090` and
  `REST_ARGS` with `--listen-https=` (`tests/e2e/Makefile:49-56`). The
  secure cluster's flags are `--cluster-public-key-file` and
  `--cluster-user-security-file`, derived from
  `cluster.public_key_file` and `cluster.user_security_file`
  (`internal/config/config.go:66-67,226`), and `--cluster` is the URI
  alias (`cmd/qdb_rest/AGENTS.md`).
- The root Makefile builds Go by sourcing `.envrc` first and passing
  `-mod=vendor` (`Makefile:30`); the CI build adds `-buildvcs=false`
  for rhel7 (`scripts/cicd/20.build.sh`).
- `tests/e2e/actual/`, `*.log` and `*.pid` under `tests/e2e` are
  gitignored (`.gitignore`).
- `docs/e2e.md:492-494` and ADR-0014's Consequences both carry the
  sentence the owner reversed on 2026-10-11 (`docs/e2e-v2-flow-plan.md`,
  "Owner decisions" 4).
- `docs/log.md` Next 1 names this unit; items 2 to 4 depend on it.

## Design

### `internal/qdbtest/table/table.go`

`generateRows` splits so the tool can fix the row count. The draw of
the count and the density stays in `generateRows`.

```go
// GenerateRows draws rows rows into tbl's batch at one drawn null
// density, so a run ranges from no nulls to all-null columns. [table.go:144-146]
// The batch is released on rt's cleanup. [table.go:155]
func GenerateRows(rt *rapid.T, tbl Table, rows int) Table {
	// style: doc comment alone
	// because: it is generateRows with the row count as a parameter, one
	// loop over the columns, and the body says the rest
}

// generateRows draws tbl's rows: a row count of 0 to 40 and then
// GenerateRows. [table.go:144-146]
func generateRows(rt *rapid.T, tbl Table) Table {
	// style: doc comment alone
	// because: it forwards in one step (rule 1)
}
```

### `tests/e2e/tools/e2etool/main.go` (new)

```go
// Command e2etool is the Go tool of the v2 e2e flow (docs/e2e.md,
// "The tool"). gen writes the rows the flow ingests. It may import any
// package of this repository, the cgo binding included. [ADR-0014 4]
package main

// flowSchema is the table the flow creates: one column of every type,
// named after its type, with the symbol's symtable named after the
// table and the column as the fixture names them. [table.go:170-177]
func flowSchema(name string) table.Table {
	// style: doc comment alone
	// because: it assigns a fixed list
}

// edgeRows is the batch of the values a draw reaches rarely, timestamped
// before the fixture's index start so a concatenation ahead of the
// drawn rows keeps $timestamp ascending: MaxInt64 and MinInt64+1, the
// awkward string with a quote, a comma, <&> and an LF, one all-null
// row, and a nanosecond timestamp. [TestEdgeCells, table.go:66-105,120]
// The caller releases it.
func edgeRows(schema *arrow.Schema) arrow.RecordBatch {
	// style: doc comment alone + inline note at the MinInt64+1 row
	// because: it builds one batch through the builders, and only the
	// bounds need saying
	//
	// at MinInt64+1: MinInt64 is the server's null for int64 [table.go:84]
	// at the all-null row: the empty field stands for null, so a null in
	// every type proves the ingest reads it back as null [encoding AGENTS, The text formats]
}

// generate draws rows rows of the flow's table under seed, with the
// edge rows ahead of them, and returns the batch as an ingest body
// carries it, $table first. [e2e-v2-flow-plan.md, "The Go tool"]
func generate(name string, rows int, seed int) arrow.RecordBatch {
	// style: doc comment + numbered overview + step comments
	// because: three blocks meet (the fixture's draw, the edge batch and
	// the body shape), and the seed's role is not evident from the code
	//
	// The draw is the fixture's, so the vocabulary and the bounds are the
	// property tests' [Rationale, "the fixture draws"]
	//
	// 1. draw the table's rows through rapid.Custom(...).Example(seed),
	//    which is rapid's one seeded entry outside a test [generator.go:81]
	//    - rejected: a tool-local draw with math/rand, because the bounds
	//      would be copied [Rationale]
	// 2. build the edge batch and concatenate it ahead of the drawn rows,
	//    column by column, because the flow asks for the edge rows first
	//    and a draw reaches them rarely [Rationale, "edge rows"]
	// 3. put the $table column in front through table.WithTable, so the
	//    CSV header equals the reader's [httpapi AGENTS, Handlers]
}

// writeCSV writes rec as rows.csv in dir through the package's CSV
// encoder, so the file is the encoder's dialect and the tool formats
// no cell of its own. [Owner decisions 4]
func writeCSV(dir string, rec arrow.RecordBatch) error {
	// style: doc comment alone
	// because: open, encode, close
}

// gen is the gen subcommand: --rows, --seed, --out, then generate and
// writeCSV.
func gen(args []string) error {
	// style: doc comment alone
	// because: a flag set and two calls
}

func main() {
	// style: bare
	// because: a subcommand switch over os.Args
}
```

### `tests/e2e/flow.sh` (new)

The driver is shell, so its comments are the file header and one line
per function. The header states: the usage line, that the flow
captures nothing because its expected value is its input (ADR-0014 2),
that the Makefile is the only source of URLs and paths, and that
`common.sh` supplies the helpers and `TZ=UTC`.

```bash
# flow.sh run --insecure-url <url> --secure-url <url> [--rows N] [--seed S]
# The v2 e2e flow (docs/e2e.md, "The v2 flow"). It proves the built
# binary over HTTP like a client: login, create, ingest, read back. The
# expected value of every step is the generated input, so nothing is
# captured (ADR-0014). Working files go to actual/flow/<cluster>/.

# parse_args: the small option loop; every option is named because two
# URLs on a command line need names. [e2e-v2-flow-plan decision log 2026-09-18]

# login <cluster>: POST /api/v2/auth/login, the anonymous body on the
# insecure cluster and user_private.key verbatim on the secure one; the
# token is checked by shape, a non-empty access_token. [ADR-0014 3, login.go]

# create_table <url> <token>: DELETE then POST /api/v2/tables with the
# e2e_csv body, dropped first so the flow is idempotent; 404 on the
# delete is tolerated, 201 on the create is required. [tables.go, flow plan "The flow" 2]

# ingest <url> <token>: POST /api/v2/rows as text/csv with rows.csv;
# 200 and .rows == the row count written. [rows.go:68]

# read_back <url> <token>: GET /api/v2/tables/e2e_csv/rows under
# Accept: text/csv, with no Accept-Encoding, into actual.csv; then
# cmp rows.csv actual.csv. [flow plan decision "The plain run sends no Accept-Encoding"]

# run_cluster <name> <url>: the four steps against one server, logged
# with the seed so a failure reproduces. [Owner decisions 6]
```

The row count the ingest must answer is the drawn rows plus the edge
rows, so `e2etool gen` prints the row count it wrote and the driver
reads it, rather than the driver assuming `ROWS`.

### `tests/e2e/Makefile`

```make
# ---- the v2 flow (docs/e2e.md, "The v2 flow")
# ROWS and SEED select the generated data; the driver prints both. [Owner decisions 6]
ROWS ?= 10000
SEED ?= $(shell date +%s)
E2ETOOL := $(E2E_DIR)/tools/e2etool/e2etool$(EXE_EXT)
# The secure server: 40091, the key files start-services.sh writes at
# the repo root, TLS off like the insecure one. [config.go:66-67, qdbtest.go:47-50]
REST_PORT_SECURE ?= 40091
REST_ARGS_SECURE ?= --cluster qdb://127.0.0.1:2838 --cluster-public-key-file $(REPO_ROOT)/cluster_public.key \
                    --cluster-user-security-file $(REPO_ROOT)/user_private.key \
                    --listen-http 127.0.0.1:$(REST_PORT_SECURE) --listen-https=

# e2etool: built with the server's toolchain and environment, so the
# tool links the same C API. [ADR-0013 Consequences, internal/AGENTS.md Building]
e2etool:
	. $(REPO_ROOT)/.envrc && cd $(REPO_ROOT) && go build -mod=vendor -buildvcs=false -o $(E2ETOOL) ./tests/e2e/tools/e2etool

# test-flow: both clusters must answer (2836 and 2838), one server per
# cluster is started from QDB_REST_BIN unless the URL is given, and the
# servers are stopped on exit. The flow loads nothing. [log.md Handoff to M2]
test-flow: services-check e2etool
```

`services-check` stays as it is. `test-flow` probes the secure port
through `check_service_running` in its recipe, because the v1 targets
need the insecure cluster alone and the one-port `require_qdbd` is
theirs.

### `tests/e2e/README.md`, `tests/e2e/AGENTS.md`

```
README, "The v2 flow": make test-flow QDB_REST_BIN=<bin> logs in, creates
  e2e_csv, ingests the generated rows as CSV and reads them back as CSV,
  compared byte for byte, on each cluster; ROWS and SEED; the seed is
  printed. The other formats and codings arrive with the surface
  definition (docs/log.md, Next).
AGENTS.md, the flow bullet: the rows come from tools/e2etool gen, written
  through the package's CSV encoder; the flow compares them with cmp; a
  wire change is fixed in the tool or the driver, never by storing a
  response; tocsv is not there yet; the generated rows carry no empty
  string.
```

### `docs/adr/0014-v2-e2e-generated-flow.md`

```
Consequences, the second bullet: the expected CSV is the CSV encoder's
  own bytes over the generated batch, which encode_test.go pins on a
  hand-built batch without a cluster; the ingest decoder that reads the
  file is a separate path proven against the encoder in decode_test.go.
  A CSV encoder bug would have to be mirrored by the decoder to hide,
  and the two are tested independently. [Owner decisions 4]
Dated amendment line under the bullet: amended 2026-10-12, owner
  decision of 2026-10-11 in docs/e2e-v2-flow-plan.md (the plan is
  deleted later, so the sentence carries the decision itself).
```

### `docs/e2e.md`

```
"The v2 flow", the paragraph at lines 491-494: the sentence "so the CSV
  path is proven against a source the encoders did not touch" becomes
  "the CSV encoder's bytes are pinned without a cluster in
  encode_test.go". Nothing else in the section changes, because the
  surface definition rewrites it (docs/log.md, Next 2).
```

### `docs/log.md`

```
Current state: Next item 1 struck; items renumbered; In flight stays
  "Nothing". One entry: "2026-10-12 -- the basic v2 e2e flow landed;
  e2e-basic-flow-plan.md deleted; facts moved to tests/e2e/AGENTS.md and
  ADR-0014".
```

## Rationale

| decision                                                            | why                                                                                                        | rejected, and why                                                                                                                      | gained                                        | given up                                                       | settled by                                                                           |
| ------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| The tool draws through the fixture, extended by `GenerateRows`      | rapid-style generation shares the vocabulary and bounds of the property tests                              | a tool-local generator, because the bounds (no empty string, no NUL, no MinInt64) would be copied and drift                            | one set of bounds                             | the tool links cgo through `internal/qdb`                      | `docs/e2e-v2-flow-plan.md`, decision log 2026-09-18; the fixture block is a proposal |
| Seeded draw through `rapid.Custom(...).Example(seed)`               | it is rapid's one seeded entry point outside a test, and the tool is a binary                              | `rapid.Check` under a fake `testing.T`, because it is a test harness and shrinks on failure                                            | a printed seed reproduces a run               | `Example` draws one value, so the whole table is one `Custom`  | proposal                                                                             |
| The edge rows are a hand-built batch in the tool, ahead of the draw | the flow plan asks for them first, and ten thousand drawn rows reach `MaxInt64` or the all-null row rarely | edge rows in the fixture, because the fixture draws values and the property tests cover the edges through their own hand-built batches | the extremes are in every run                 | one batch build in the tool                                    | `docs/e2e-v2-flow-plan.md`, "The Go tool"; the home is a proposal                    |
| The expected file is the CSV encoder's bytes                        | no duplicated cell formatting, and the encoder is pinned byte for byte without a cluster                   | a tool-local CSV writer, because it duplicates `csv.go`                                                                                | one dialect                                   | the CSV path is no longer proven against an independent source | owner, 2026-10-11                                                                    |
| The flow runs on both clusters from this unit on                    | the secure one costs one Makefile block and is the only step that proves the key-file flags                | insecure only                                                                                                                          | the key-file flags are exercised on every run | one more server start per run                                  | `docs/e2e-v2-flow-plan.md`, recommendation; owner has not ruled (Open questions 1)   |
| The tool prints the row count it wrote and the driver asserts on it | the edge rows add to `ROWS`, and one writer of the number keeps the driver from recomputing it             | the driver adding a constant, because the edge count would live twice                                                                  | one source of the count                       | the driver parses one line of the tool's output                | proposal                                                                             |
| `e2e.md` changes one sentence only                                  | the surface definition rewrites the section after this unit, and that is the owner's step                  | the whole section now, because it would pre-empt the definition                                                                        | no conflict with Next 2                       | the section still lists three tables and `tocsv` until then    | `docs/e2e-v2-flow-plan.md`, "Owner decisions" 2; Open questions 2                    |
| A red flow run caused by sc-19942 is re-run once                    | the qdbd crash is random and filed with R&D, and the tests carry no workaround                             | a retry in the driver, because that hides a real failure                                                                               | an honest driver                              | one manual re-run on a crash                                   | `internal/AGENTS.md`, Tests                                                          |

## Knowledge

- Commit 2 (`GenerateRows`): the doc comments above; no `AGENTS.md`
  row, because `internal/AGENTS.md`, Tests, already says a test takes
  the fixture's blocks and copies none.
- Commit 3 (`e2etool gen`): `generate`'s overview carries the fixture
  draw, the seeded entry and the edge rows (Rationale rows 1 to 3);
  `edgeRows` carries the bounds; `writeCSV` carries the encoder rule
  (Rationale row 4).
- Commit 4 (`e2etool` target): the Makefile comment carries the
  toolchain rule.
- Commit 5 (`flow.sh`): the header carries ADR-0014 2 and the function
  comments carry the step rules; `read_back` carries the no
  `Accept-Encoding` rule; the ingest step carries the printed row count
  (Rationale row 6).
- Commit 6 (`test-flow`): the Makefile comment carries the two-port
  check, the two servers and the key files (Rationale row 5).
- Commit 7 (README, `AGENTS.md`): what exists, and that `tocsv` and the
  rest arrive with the surface definition.
- Commit 8 (ADR-0014, `docs/e2e.md`): Rationale row 4's "given up"
  lands in the ADR's Consequences; row 7 is served by changing one
  sentence.
- Commit 9 (log): Next 1 struck.
- Rationale row 8 has its home already (`internal/AGENTS.md`, Tests)
  and lands nowhere new.

## How the knowledge lands

Run `/doc-discipline read` before the first code commit. Write every
commit's comments and document rows from the Design and Knowledge
sections, in the same commit as the code, so that every listed claim
appears once at the named place, no claim is added without evidence,
and a claim the code contradicts is reported in the build-stage message
and not written. A why that arises while building and is not here is
written where it is decided with its evidence, or asked of the owner
through the question tool before the commit. After the last code
commit, run `/doc-discipline all internal/qdbtest/table tests/e2e docs/adr/0014-v2-e2e-generated-flow.md`
and make one small commit per finding. Before the build-stage message,
run `/doc-discipline check internal/qdbtest/table tests/e2e docs/e2e-basic-flow-plan.md`.
Run `npx prettier --write` on every Markdown file touched.

## Commits

1. `docs(plan): e2e-basic-flow-plan.md, the basic v2 e2e flow: login, one table, CSV in and CSV out on each cluster` (this document)
2. `test(qdbtest): GenerateRows draws a table's rows at a given count`
3. `feat(e2e): e2etool gen draws the rows and writes rows.csv through the CSV encoder`
4. `build(e2e): the Makefile builds e2etool with the server's toolchain`
5. `feat(e2e): flow.sh logs in, creates e2e_csv, ingests rows.csv and reads it back byte for byte`
6. `build(e2e): make test-flow starts one server per cluster and runs the driver`
7. `docs(e2e): README and AGENTS.md describe the tool, the driver and test-flow as they exist`
8. `docs(adr): ADR-0014's expected CSV is the encoder's, pinned in encode_test.go`
9. `docs(log): the basic v2 e2e flow landed; Next item 1 struck`
10. `/doc-discipline all` over the touched paths, one small commit per finding, then `/doc-discipline check` against this plan.
11. Verify: push `sc-19567/rr-e2e-basic-flow`, build its head in Buildkite (API-created build, `ignore_pipeline_branch_filters`, the full 40-character SHA, `.buildkite/AGENTS.md`), wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Both clusters from this unit on. Recommendation: yes, as the flow
   plan recommends.
2. `docs/e2e.md` changes one sentence now and the rest at the surface
   definition. Recommendation: yes.
3. `GenerateRows` on the fixture rather than a tool-local draw.
   Recommendation: the fixture, since no test copies its blocks.
4. The edge rows live in the tool. Recommendation: the tool, in the
   shape of `TestEdgeCells`.
5. `tests/e2e/README.md` and `AGENTS.md` describe what exists after this
   unit rather than the direction. Recommendation: what exists, so a
   reader is not sent to a `tocsv` that is not there.
