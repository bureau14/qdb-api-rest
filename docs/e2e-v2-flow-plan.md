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
   `e2e_ndjson`, `e2e_arrow`, every column type (`blob`, `double`,
   `int64`, `string`, `symbol`, `timestamp`), dropped first so the flow
   is idempotent.
3. Query each empty table in every format. JSON keeps the columns with
   empty `data`, NDJSON is an empty body, CSV is the header alone, and
   Arrow is a schema with no batches (`internal/encoding/AGENTS.md`, The
   text formats). This proves the schema path before any row exists.
4. `POST /api/v2/rows`: the generated rows, whose `$table` column
   names the table, as CSV for `e2e_csv`, as NDJSON for `e2e_ndjson`,
   and as Arrow IPC for `e2e_arrow`, each answered with a 200 whose body
   carries the row count.
5. Read each table (`GET /api/v2/tables/{name}/rows`) in every format
   under `identity` and `gzip`. Every response is decoded to CSV and
   compared byte for byte with the generated CSV. `content-type` is
   asserted from the format and `content-encoding` from the coding. A
   gzip run is decompressed first. Nothing else in the headers is read.

Every assertion is pass or fail. Nothing is timed (ADR-0013 2).

### The comparator

One expected value, the generated CSV in the CSV encoder's dialect
(`encoding/csv` RFC 4180, header row, LF). The CSV response is
compared raw. JSON, NDJSON and Arrow IPC responses are decoded by the
harness's Go tool and encoded through `internal/encoding`'s CSV
encoder, then compared with the same file. That is the rule ADR-0013 5
already sets for Arrow, applied to every text format. The CSV path is
thus proven against a source the encoders did not touch, and the other
three formats are proven equal to it.

One fact of the tree bounds the generated data (verified 2026-09-18).
CSV writes null and the empty string as the same empty field
(`internal/encoding/AGENTS.md`, The text formats), so the generator
emits no empty string, and the ingest decoder reads an empty CSV field
as null.

### The Go tool

`tests/e2e/tools/e2etool`, built by the Makefile with the server's
toolchain and environment (ADR-0013, Consequences):

- `e2etool gen --rows N --seed S`: writes the rows as `rows.csv`,
  `rows.ndjson` and `rows.arrow` (one batch per `chunkRows`, the IPC
  streaming format), every column type, nulls, the awkward string
  (`"`, `,`, `<&>`), nanosecond timestamps, negative and extreme
  integers. NaN is excluded, because it encodes as null and would not
  round-trip. The driver prints the seed so a failure reproduces.
- `e2etool tocsv --format json|ndjson|arrow`: stdin to CSV on stdout,
  through the package's own CSV encoder.

It may import any package of this repository, the cgo binding
included. It replaces the `tools/arrowcsv` of `docs/e2e.md`.

### The driver

`tests/e2e/flow.sh run --insecure-url <url> --secure-url <url>
[--rows N] [--seed S]` is a small option loop. The Makefile stays the
only source of URLs, ports and paths. `golden.sh` keeps the v1 suite
and loses nothing. `make test-v2` (and `capture-v2`, which is gone)
become `make test-flow`. A cluster whose URL is empty gets a server
started from `QDB_REST_BIN` (`40090` insecure, `40091` secure with
`--cluster-public-key-file` and `--cluster-user-security-file`, with
the TLS listener off and `TZ=UTC`). `test-v1` takes its server from
`--insecure-url` or `REST_URL_INSECURE`, and the bare `REST_URL`
leaves. Working files go to `actual/flow/<cluster>/`.

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
`Content-Type` selecting the parser (`text/csv`,
`application/x-ndjson`, `application/vnd.apache.arrow.stream`), a
`$table` column routing each row, and a `Content-Encoding` of `gzip` or
`zstd` accepted. It makes one push per request through the Arrow
writer and answers 200 with `{"rows": N, ...}`. Errors are RFC 9457
problems (ADR-0010 6).

## This unit: the documentation re-cut

| Document                                 | Change                                                                                                                                                                                                                                                               |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/brief.md`, Milestones              | M2 -- Tables and ingest is inserted, M1's exit loses the golden suite, M2..M9 renumber to M3..M10, the exploration milestone loses "create", the ingestion milestone keeps the multi-table `/api/v2/ingest` only, and the ordering rationale names why M2 sits there |
| `docs/brief.md`, Testing doctrine 2      | v2 is the generated roundtrip flow, v1 keeps goldens, and the dataset sentence is v1's and the bench's                                                                                                                                                               |
| `docs/brief.md`, endpoint sketch         | the two M2 endpoints marked M2                                                                                                                                                                                                                                       |
| `docs/adr/0014-v2-e2e-generated-flow.md` | the decision above, which supersedes ADR-0013 3 and 5 for v2 only, and one Go tool that generates and decodes                                                                                                                                                        |
| `docs/adr/0010`, `0011`, `0012`          | the five milestone numbers renumber mechanically (M2 -> M3, M3 -> M4, M4 -> M5), and no decision changes                                                                                                                                                             |
| `docs/e2e-plan.md` -> `docs/e2e.md`      | restored and renamed as a permanent specification, "The v2 suite" rewritten as the flow, the `expected/` archive layout, the operator capture cycle and the v2 golden case layout leave, and the v1 section is unchanged                                             |
| `docs/bench-plan.md` -> `docs/bench.md`  | restored and renamed as a permanent specification, with the pointers repointed                                                                                                                                                                                       |
| `docs/AGENTS.md`                         | a row for subsystem specifications (`docs/<subsystem>.md`, permanent, edited in place, the home of verified mechanics too large for an `AGENTS.md`), so that Plans are not the only home of verified facts                                                           |
| `docs/log.md`                            | the milestone table and criteria rewritten, Next 1 is the M2 unit, the handoff renamed to M4, and one dated entry                                                                                                                                                    |
| `tests/e2e/AGENTS.md`, `README.md`       | the flow, its targets and the tool, with the golden rules scoped to v1                                                                                                                                                                                               |

## Commits

1. `docs(plan): the v2 e2e flow replaces the golden suite; this unit re-cuts the documentation`
2. `docs(brief): M2 -- tables and ingest; the later milestones renumber`
3. `docs(brief): the v2 e2e is a generated roundtrip flow; v1 keeps goldens`
4. `docs(adr): ADR-0014, the v2 e2e is a generated flow, no goldens`
5. `docs(adr): milestone numbers in ADR-0010, 0011 and 0012 follow the brief`
6. `docs: e2e-plan.md and bench-plan.md are the permanent e2e.md and bench.md`
7. `docs(e2e): the v2 section is the flow; the archive carries no expected bodies`
8. `docs(log): M2 inserted; the flow is next; the handoff is M4's`
9. `docs(e2e): AGENTS.md and README name the flow, the tool and the targets`

Every commit passes `npx prettier --check` on the files it touches.
No commit touches code, tests or the Makefile.

## Open questions and recommendations

1. The tool's name: `tests/e2e/tools/e2etool`, one binary, `gen` and
   `tocsv` subcommands, one build rule. This is recommended. The
   alternative is two binaries.
2. The flow runs on both clusters (decision 2026-09-18 below, kept).
   The secure node proves the key-file flags and the credential check,
   and the same rows run on both.

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
| The plain run sends no `Accept-Encoding`           | what a plain client sends, and the explicit `identity` rule is a Go test         |

         | `Accept-Encoding: identity`                               |
