# The v2 e2e flow -- Plan

Status: draft. Scaffolding for one unit of work: the documentation
re-cut that replaces the v2 golden suite with the v2 e2e flow and
inserts the milestone that carries it. The code the flow needs (two
endpoints, one Go tool, one driver) is a later unit under this same
plan; this unit writes no code. Deleted when the flow lands; anything
permanent moves to `docs/e2e.md`, `tests/e2e/AGENTS.md`,
`tests/e2e/README.md` or an ADR first (`docs/AGENTS.md`, Plans).

## The decision (owner, 2026-09-18)

The v2 e2e proves that the basic flow works, on the built binary,
driven over HTTP like a client: authenticate, create a table, query it,
ingest rows, query them back in every format and content coding. It
does not pin error rows -- the ADR-0010 and ADR-0011 tables are Go
tests (`internal/httpapi/*_test.go`) -- and it captures nothing: the
rows it ingests are generated, so the expected response is known by
construction and there is no audit step. The v1 suite is untouched: the
old server is its specification and its goldens stay (ADR-0013 3-4).

Consequences for the milestones: the flow needs `POST /api/v2/tables`
and `POST /api/v2/tables/{name}/rows`, which the brief placed in M6 and
M7. They move into a new milestone directly after M1, **M2 -- Tables
and ingest**, and every later milestone renumbers by one. M1 closes on
its Go tests; M2 closes on the flow green in Buildkite.

## The flow

Runs once per cluster, `insecure` (`qdb://127.0.0.1:2836`) and `secure`
(`:2838`), each against its own server under test; the secure login
body is the user security file `start-services.sh` writes at the repo
root (`user_private.key`, the file `internal/qdbtest` reads too), the
insecure login is anonymous.

1. `POST /api/v2/auth/login`: 200, `login-shape` (`access_token` a
   non-empty string, `token_type` `Bearer`, `expires_in` a positive
   integer; ADR-0011 3). The token authenticates every later step.
2. `POST /api/v2/tables`: one table per input format, `e2e_csv`,
   `e2e_ndjson`, `e2e_arrow`, every column type (`blob`, `double`,
   `int64`, `string`, `symbol`, `timestamp`), dropped first so the flow
   is idempotent.
3. Query each empty table in every format: JSON keeps the columns with
   empty `data`, NDJSON is an empty body, CSV the header alone, Arrow a
   schema with no batches (`internal/encoding/AGENTS.md`, Rendering).
   Proves the schema path before any row exists.
4. `POST /api/v2/tables/{name}/rows`: the generated rows, as CSV into
   `e2e_csv`, as NDJSON into `e2e_ndjson`, as Arrow IPC into
   `e2e_arrow`; 200 (or the status ADR-0015 fixes), then a row count
   through the query endpoint.
5. Query each table in every format under `identity` and `gzip`; every
   response decoded to CSV and compared byte for byte with the
   generated CSV. `content-type` is asserted from the format,
   `content-encoding` from the coding; a gzip run is decompressed
   first; nothing else in the headers is read.

Every assertion is pass or fail; nothing is timed (ADR-0013 2).

### The comparator

One expected value, the generated CSV in the CSV encoder's dialect
(`encoding/csv` RFC 4180, header row, LF). The CSV response is
compared raw. JSON, NDJSON and Arrow IPC responses are decoded by the
harness's Go tool and rendered through `internal/encoding`'s CSV
encoder, then compared with the same file: the rule ADR-0013 5 already
sets for Arrow, applied to every rendered format. The CSV path is thus
proven against a source the encoders never touched, and the other
three are proven equal to it.

Two facts of the tree bound the generated data (verified 2026-09-18):

- CSV renders null and the empty string as the same empty field
  (`internal/encoding/AGENTS.md`, Rendering), so the generator never
  emits an empty string, and the ingest parser reads an empty CSV field
  as null.
- The batch writer has no null-aware timestamp column constructor until
  the `qdb-api-go` upstream fix (`docs/log.md`, Next), so the generated
  data has null cells in every type except `timestamp`; the flow gains
  null timestamps when the fix lands.

### The Go tool

`tests/e2e/tools/e2etool`, pure Go, no cgo, built by the Makefile with
the server's toolchain (ADR-0013, Consequences):

- `e2etool gen --rows N --seed S`: writes the rows as `rows.csv`,
  `rows.ndjson` and `rows.arrow` (one batch per `chunkRows`, the IPC
  streaming format), every column type, nulls, the awkward string
  (`"`, `,`, `<&>`), nanosecond timestamps, negative and extreme
  integers, NaN excluded (it renders as null and would not round-trip).
  The seed is printed by the driver so a failure reproduces.
- `e2etool tocsv --format json|ndjson|arrow`: stdin to CSV on stdout,
  through the package's own CSV encoder.

It imports `internal/encoding` and `arrow-go` only. It replaces the
`tools/arrowcsv` of `docs/e2e.md`.

### The driver

`tests/e2e/flow.sh run --insecure-url <url> --secure-url <url>
[--rows N] [--seed S]`, a small option loop; the Makefile is still the
only source of URLs, ports and paths. `golden.sh` keeps the v1 suite
and loses nothing. `make test-v2` (and `capture-v2`, gone) become
`make test-flow`: a cluster whose URL is empty gets a server started
from `QDB_REST_BIN` (`40090` insecure, `40091` secure with
`--cluster-public-key-file` and `--cluster-user-security-file`, TLS
listener off, `TZ=UTC`); `test-v1` takes its server from
`--insecure-url` / `REST_URL_INSECURE`; the bare `REST_URL` leaves.
Working files go to `actual/flow/<cluster>/`.

`seed.sql` and `make load` stay for the v1 suite (goldens 06 and 16
read `reproduce`) and for the bench; the flow reads neither. The
dataset archive carries no `expected/` directory: nothing in v2 is
too large for git because nothing in v2 is stored.

### The endpoints (M2, their own ADR)

Decided in ADR-0015 with the code unit, not here; the plan records
only what the flow needs of them: `POST /api/v2/tables` takes a JSON
body naming the table, its shard size and its columns in the brief's
schema vocabulary (`docs/brief.md`, "/api/v2 endpoint sketch");
`POST /api/v2/tables/{name}/rows` takes the rows as the body,
`Content-Type` selecting the parser (`text/csv`,
`application/x-ndjson`, `application/vnd.apache.arrow.stream`), a
`Content-Encoding` of `gzip` or `zstd` accepted, one push per request
through the batch writer. Errors are RFC 9457 problems (ADR-0010 6).

## This unit: the documentation re-cut

| Document                                 | Change                                                                                                                                                                                                                                                        |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/brief.md`, Milestones              | M2 -- Tables and ingest inserted; M1's exit loses the golden suite; M2..M9 renumber to M3..M10; the exploration milestone loses "create", the ingestion milestone keeps the multi-table `/api/v2/ingest` only; the ordering rationale names why M2 sits there |
| `docs/brief.md`, Testing doctrine 2      | v2 is the generated roundtrip flow; v1 keeps goldens; the dataset sentence is v1's and the bench's                                                                                                                                                            |
| `docs/brief.md`, endpoint sketch         | the two M2 endpoints marked as decided by ADR-0015                                                                                                                                                                                                            |
| `docs/adr/0014-v2-e2e-generated-flow.md` | the decision above; supersedes ADR-0013 3 and 5 for v2 only; one Go tool generates and decodes                                                                                                                                                                |
| `docs/adr/0010`, `0011`, `0012`          | the five milestone numbers renumber mechanically (M2 -> M3, M3 -> M4, M4 -> M5); no decision changes                                                                                                                                                          |
| `docs/e2e-plan.md` -> `docs/e2e.md`      | restored and renamed, a permanent specification; "The v2 suite" rewritten as the flow; the `expected/` archive layout, the operator capture cycle and the v2 golden case layout leave; the v1 section unchanged                                               |
| `docs/bench-plan.md` -> `docs/bench.md`  | restored and renamed, a permanent specification; pointers repointed                                                                                                                                                                                           |
| `docs/AGENTS.md`                         | a row for subsystem specifications (`docs/<subsystem>.md`: permanent, edited in place, the home of verified mechanics too large for an `AGENTS.md`); Plans no longer the only home of verified facts                                                          |
| `docs/log.md`                            | milestone table and criteria rewritten; Next 1 is the M2 unit; the handoff renamed to M4; one dated entry                                                                                                                                                     |
| `tests/e2e/AGENTS.md`, `README.md`       | the flow, its targets and the tool; the golden rules scoped to v1                                                                                                                                                                                             |

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

Every commit passes `npx prettier --check` on the files it touches;
no commit touches code, tests or the Makefile.

## Open questions and recommendations

1. The tool's name: `tests/e2e/tools/e2etool`, one binary, `gen` and
   `tocsv` subcommands, one build rule. Recommended; an alternative is
   two binaries.
2. The flow runs on both clusters (decision 2026-09-18 below, kept):
   the secure node proves the key-file flags and the credential check;
   the same rows on both.
3. The ingest status: 200 with a small JSON body (`rows` written) or 201. Recommended 200 with `{"rows": N}`; ADR-0015 decides.

## Decision log (2026-09-18)

| Decision                                           | Why                                                                          | Rejected                                                  |
| -------------------------------------------------- | ---------------------------------------------------------------------------- | --------------------------------------------------------- |
| The v2 e2e is one flow, generated data, no goldens | proves the basic flow with little code and no audit liability                | ten captured cases and an operator capture cycle          |
| Error rows are Go tests only                       | the e2e proves the flow, not the surface; the tables are pinned in `httpapi` | 401/413/415/400 goldens                                   |
| M2 -- Tables and ingest, later milestones renumber | the flow needs the two endpoints; they are small and unblock later work      | growing M1; the suite as an M7 exit                       |
| Ingest bodies: CSV, NDJSON and Arrow IPC now       | the flow ingests the same rows three ways; the property tests come with them | CSV only, the rest in the ingestion milestone             |
| One Go tool decodes every format to CSV            | ADR-0013 5's Arrow rule for every rendered format; one expected file         | a renderer per format in shell; row-count checks for JSON |
| The Go tool generates the rows                     | rapid-style generation shares the vocabulary of the property tests           | an awk generator; a checked-in fixture CSV                |
| `e2e-plan.md` and `bench-plan.md` become permanent | they outgrew scaffolding; specifications with one home each                  | folding them into `AGENTS.md`s; keeping them as plans     |
| The flow runs on both clusters                     | the same flow on two parallel environments; only the login differs           | insecure only                                             |
| A driver with named options                        | two URLs on a command line need names                                        | positional URLs; environment variables                    |
| The plain run sends no `Accept-Encoding`           | what a plain client sends; the explicit `identity` rule is a Go test         | `Accept-Encoding: identity`                               |
