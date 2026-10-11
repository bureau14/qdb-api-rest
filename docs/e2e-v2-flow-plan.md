# The v2 e2e flow -- Plan

Status: approved. This plan is scaffolding for the v2 e2e flow. The
code the flow needs (the endpoints, one Go tool, one driver) lands in
units under this plan. The plan is deleted when the flow lands.
Anything permanent moves to `docs/e2e.md`, `tests/e2e/AGENTS.md`,
`tests/e2e/README.md` or an ADR first (`docs/AGENTS.md`, Plans).

## The decision (owner, 2026-09-18)

The v2 e2e proves that the basic flow works, on the built binary,
driven over HTTP like a client: authenticate, create a table, query it,
ingest rows, query them back in every format and content coding. It
does not pin error rows, because the ADR-0010 and ADR-0011 tables are
Go tests (`internal/httpapi/*_test.go`). It captures nothing. The rows
it ingests are generated, so the expected response is known by
construction and there is no audit step. The v1 suite is untouched. The
old server is its specification and its goldens stay (ADR-0013 3-4).

Consequences for the milestones: the flow needs `POST /api/v2/tables`,
`GET /api/v2/tables/{name}/rows` and `POST /api/v2/rows`. They live in
the milestone directly after M1, **M2 -- Tables, reader and ingest**. M1
closes on its Go tests. M2 closes on the flow green in Buildkite.

## Owner decisions (2026-10-11)

The flow lands in steps, and the first step is deliberately small. The
owner's answers of this date, recorded here so that the unit that
executes them needs no conversation:

1. **The first e2e unit is one basic test.** It logs in, creates one
   table, ingests the generated rows as CSV, reads the table back as
   CSV and compares the two files byte for byte, on each cluster. The
   empty read, the other body formats, the other read formats and the
   content codings are not part of it.
2. **The surface is defined together.** After the basic test runs, the
   owner and the agent define what the flow tests, and "The v2 flow" in
   `docs/e2e.md` is rewritten to that definition before any further
   step lands. Until then that section is the direction, not a
   commitment per step.
3. **The ingest takes columnar JSON.** `POST /api/v2/rows` under
   `Content-Type: application/json` reads the shape the query and the
   table reader answer, so that every format the read answers is a
   format the ingest takes. This lands as its own unit before the basic
   test (`docs/log.md`, Next). The brief's open question 2 keeps only
   the v1 tables/columns shape open.
4. **Lean: no duplicated encoding code.** The tool writes its bodies
   through the encoders of `internal/encoding` and decodes responses
   through its decoders, which decision 3 completes for every read
   format. The tool carries no cell formatting or parsing of its own.
   This reverses one sentence of ADR-0014's Consequences, "the CSV path
   is proven against a source the encoders did not touch": the CSV
   encoder's bytes are pinned on a hand-built batch in
   `internal/encoding/encode_test.go` instead, and the ingest decoder
   that reads the generated file is a separate path proven against the
   encoder in `decode_test.go`. The unit that lands the tool amends
   that sentence.
5. **The tool stays Go.** Python was considered for the client and
   rejected: ADR-0013 already names no pyarrow wheels on FreeBSD, a
   venv on every agent, Python in the permanent path and pandas' int64
   coercion, and the flow must run on all eight platforms. Speed plays
   no part, because the flow measures nothing. Flight SQL, when it
   comes, is a subcommand of the same tool over arrow-go's stock client,
   which the module already carries. The QuasarDB Python API's gateway
   transport against this server is tested in that API's repository,
   because this server's e2e depending on the Python API while the
   Python API depends on this server is a cycle.
6. **Rows default to ten thousand.** `ROWS` is a Makefile variable with
   that default. `SEED` defaults to the epoch second, and the driver
   prints both.
7. The branch `sc-19567/rr-ci-qdbd-logs` is a temporary debug branch of
   the Windows qdbd investigation and plays no part in the flow.

Recommendations the owner has not ruled on, settled by the basic test
unit's plan:

- The basic test runs on both clusters, because the secure one costs
  one Makefile block and is the only step that proves the key-file
  flags.
- The tool's Arrow schema takes its types from the binding's
  `TsColumnType.ArrowType`, so a later Arrow body carries the reader's
  types by construction (`internal/AGENTS.md`, the read).

## The flow

The flow runs once per cluster, `insecure` (`qdb://127.0.0.1:2836`) and
`secure` (`:2838`), each against its own server under test. The secure
login body is the user security file `start-services.sh` writes at the
repo root (`user_private.key`, the file `internal/qdbtest` reads too),
and the insecure login is anonymous.

1. `POST /api/v2/auth/login`: 200, `login-shape` (`access_token` a
   non-empty string, `token_type` `Bearer`, `expires_in` a positive
   integer, per ADR-0011 3). The token authenticates every later step.
2. `POST /api/v2/tables`: one table per input format, `e2e_csv`,
   `e2e_ndjson`, `e2e_arrow`, `e2e_json`, every column type (`blob`,
   `double`, `int64`, `string`, `symbol`, `timestamp`), dropped first so
   the flow is idempotent.
3. Read each empty table in every format. JSON keeps the columns with
   empty `data`, NDJSON is an empty body, CSV is the header alone, and
   Arrow is a schema with no batches (`internal/encoding/AGENTS.md`, The
   text formats). This proves the schema path before any row exists.
   The table reader is the route that answers the schema of an empty
   table in every format (`internal/httpapi/AGENTS.md`, Handlers).
4. `POST /api/v2/rows`: the generated rows, whose `$table` column
   names the table, as CSV for `e2e_csv`, as NDJSON for `e2e_ndjson`,
   as Arrow IPC for `e2e_arrow` and as columnar JSON for `e2e_json`,
   each answered with a 200 whose body carries the row count.
5. Read each table (`GET /api/v2/tables/{name}/rows`) in every format
   under `identity` and `gzip`. Every response is decoded to CSV and
   compared byte for byte with the generated CSV. `content-type` is
   asserted from the format and `content-encoding` from the coding. A
   gzip run is decompressed first. Nothing else in the headers is read.

Every assertion is pass or fail. Nothing is timed (ADR-0013 2). The
basic test unit lands step 1, step 2 for `e2e_csv` alone, step 4 for
CSV alone and step 5 for CSV under `identity` alone. The surface
definition (Owner decisions 2) decides the rest.

### The comparator

One expected value, the generated CSV, written by the CSV encoder over
the generated batch (`encoding/csv` RFC 4180, header row, LF). The CSV
response is compared raw. JSON, NDJSON and Arrow IPC responses are
decoded by the harness's Go tool through the package's decoders and
encoded through its CSV encoder, then compared with the same file. That
is the rule ADR-0013 5 already sets for Arrow, applied to every format.

One fact of the tree bounds the generated data (verified 2026-09-18).
CSV writes null and the empty string as the same empty field
(`internal/encoding/AGENTS.md`, The text formats), so the generator
emits no empty string, and the ingest decoder reads an empty CSV field
as null. The generator keeps every bound of the table fixture's draw
(`internal/AGENTS.md`, Tests): no `MinInt64`, no NaN, no empty string
or blob, no NUL, and alphanumeric symbols.

### The Go tool

`tests/e2e/tools/e2etool`, built by the Makefile with the server's
toolchain and environment (ADR-0013, Consequences):

- `e2etool gen --rows N --seed S`: draws one record batch, `$table`
  first, then `$timestamp` strictly ascending, then one column of every
  type, with the edge rows (extreme integers, the awkward string with
  `"`, `,` and `<&>`, nulls, nanosecond timestamps) ahead of the drawn
  ones, and writes it as `rows.csv`, `rows.ndjson`, `rows.arrow` and
  `rows.json` through the package's encoders. NaN is excluded, because
  it encodes as null and would not round-trip. The basic test unit
  writes `rows.csv` alone.
- `e2etool tocsv --format json|ndjson|arrow`: stdin to CSV on stdout,
  through the package's decoder of that format and its CSV encoder. It
  lands with the surface definition.

It may import any package of this repository, the cgo binding
included.

### The driver

`tests/e2e/flow.sh run --insecure-url <url> --secure-url <url>
[--rows N] [--seed S]` is a small option loop. The Makefile stays the
only source of URLs, ports and paths. `golden.sh` keeps the v1 suite
and loses nothing. `make test-flow` runs the driver. A cluster whose
URL is empty gets a server started from `QDB_REST_BIN` (`40090`
insecure, `40091` secure with `--cluster-public-key-file` and
`--cluster-user-security-file`, with the TLS listener off and
`TZ=UTC`). `test-v1` takes its server from `--insecure-url` or
`REST_URL_INSECURE`, and the bare `REST_URL` leaves. Working files go
to `actual/flow/<cluster>/`.

`seed.sql` and `make load` stay for the v1 suite (goldens 06 and 16
read `reproduce`) and for the bench. The flow reads neither. The
dataset archive carries no `expected/` directory, because nothing in
v2 is stored, so nothing in v2 is too large for git.

### The endpoints (M2)

The endpoints are decided with their code slices, not here. The
contract of the table routes is the brief's (`docs/brief.md`, "Tables:
create and delete"). The contracts of the table reader and the ingest
are their handlers'. The plan records only what the flow needs of them.
`POST /api/v2/tables` takes a JSON body naming the table, its shard
size and its columns in the brief's schema vocabulary.
`GET /api/v2/tables/{name}/rows` answers the whole table in the
`Accept`ed format, `$table` and `$timestamp` first, streamed batch by
batch. `POST /api/v2/rows` takes the rows as the body, with
`Content-Type` selecting the decoder (`text/csv`,
`application/x-ndjson`, `application/vnd.apache.arrow.stream`,
`application/json`), a `$table` column routing each row, and a
`Content-Encoding` of `gzip` or `zstd` accepted. It makes one push per
request through the Arrow writer and answers 200 with `{"rows": N,
...}`. Errors are RFC 9457 problems (ADR-0010 6).

## Units under this plan

1. Columnar JSON ingestion (`docs/log.md`, Next 1), its own unit with
   its own plan.
2. The basic test (Owner decisions 1): `e2etool gen` writing `rows.csv`
   through the CSV encoder, the Makefile target that builds the tool,
   `flow.sh` with the four steps, `make test-flow` with one server per
   cluster, the README and `tests/e2e/AGENTS.md` describing what
   exists, and the amendment of ADR-0014's sentence (Owner decisions 4).
3. The surface definition (Owner decisions 2), then the remaining steps
   as the definition cuts them.
4. `scripts/cicd/40.test-e2e.sh`, which puts the flow in CI and closes
   M2.

## Open questions and recommendations

1. Where the flow checks the query endpoint, if anywhere. The round trip
   in `internal/httpapi/roundtrip_test.go` queries every table in every
   format, and the flow as written reads through the table reader only.
   The surface definition decides.
2. Both clusters from the basic test on (recommended above).

## Decision log (2026-09-18)

| Decision                                           | Why                                                                              | Rejected                                                  |
| -------------------------------------------------- | -------------------------------------------------------------------------------- | --------------------------------------------------------- |
| The v2 e2e is one flow, generated data, no goldens | proves the basic flow with little code and no audit liability                    | ten captured cases and an operator capture cycle          |
| Error rows are Go tests only                       | the e2e proves the flow, not the surface, and the tables are pinned in `httpapi` | 401/413/415/400 goldens                                   |
| M2 -- Tables and ingest, later milestones renumber | the flow needs the two endpoints, and they are small and unblock later work      | growing M1, the suite as an M7 exit                       |
| Ingest bodies: CSV, NDJSON and Arrow IPC now       | the flow ingests the same rows three ways, and the property tests come with them | CSV only, the rest in the ingestion milestone             |
| One Go tool decodes every format to CSV            | ADR-0013 5's Arrow rule for every text format, one expected file                 | an encoder per format in shell, row-count checks for JSON |
| The Go tool generates the rows                     | rapid-style generation shares the vocabulary of the property tests               | an awk generator, a checked-in fixture CSV                |
| `e2e-plan.md` and `bench-plan.md` become permanent | they outgrew scaffolding, and they are specifications with one home each         | folding them into `AGENTS.md`s, keeping them as plans     |
| The flow runs on both clusters                     | the same flow on two parallel environments, and only the login differs           | insecure only                                             |
| A driver with named options                        | two URLs on a command line need names                                            | positional URLs, environment variables                    |
| The plain run sends no `Accept-Encoding`           | what a plain client sends, and the explicit `identity` rule is a Go test         | `Accept-Encoding: identity`                               |

## Decision log (2026-10-11)

| Decision                               | Why                                                                                      | Rejected                                                                  |
| -------------------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| The ingest takes columnar JSON         | every read format is an ingest format, and the decoder set covers every response         | columnar JSON as output only, a tool-local JSON reader                    |
| The first e2e unit is one basic test   | one small step on the built binary before the surface is defined                         | the whole flow in one unit                                                |
| The surface is defined with the owner  | what is useful to test is the owner's call, not derivable from the documents             | the agent cutting the remaining steps alone                               |
| The tool reuses the package's encoders | no duplicated cell formatting, and the encoders are pinned byte for byte without cluster | a tool-local CSV writer so the expected file is untouched by the encoders |
| The tool stays Go                      | ADR-0013's platform reasons stand, and Flight SQL comes with arrow-go's client           | a Python client, the Python API as the e2e client                         |
| Ten thousand rows by default           | owner's preference                                                                       | one thousand                                                              |
