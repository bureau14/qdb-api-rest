# Project Log

Status: living document. The top block is rewritten in place. The entries
below are append-only, newest first. The conventions are in `docs/AGENTS.md`.

## Current state

Last updated: 2026-10-08

| Milestone                       | State       | Note                                                                             |
| ------------------------------- | ----------- | -------------------------------------------------------------------------------- |
| M0 -- Foundation                | done        | exit signed off 2026-08-25                                                       |
| M1 -- v2 query                  | done        | closed 2026-09-21                                                                |
| M2 -- Tables, reader and ingest | in progress | the v2 e2e flow is its exit (ADR-0014)                                           |
| M3 -- v2 auth                   | not started |                                                                                  |
| M4 -- Drop-in compat            | not started | a local red bar exists (`make -C tests/e2e test-v1`), and it joins CI when green |
| M5 -- Resilience                | not started |                                                                                  |
| M6 -- Flight SQL                | not started |                                                                                  |
| M7 -- Exploration               | not started |                                                                                  |
| M8 -- Embedded DuckDB           | not started |                                                                                  |
| M9 -- Release                   | not started |                                                                                  |

M2 criteria. Exit: `POST /api/v2/tables`, `DELETE /api/v2/tables/{name}`,
`GET /api/v2/tables/{name}/rows` (every format) and `POST /api/v2/rows`
(CSV, NDJSON, Arrow IPC bodies) landed with their ingest/read roundtrip
property tests per input format, and the v2 e2e flow (`docs/e2e.md`,
"The v2 flow") green in Buildkite on all eight platforms through
`scripts/cicd/40.test-e2e.sh`.

M4 criteria. Entry: v2 auth and query are landed (M1 and M3 exits), and
the 14 v1 goldens replay against a server under test. Exit: every v1
golden green against `bin/qdb_rest` at both spellings, in Buildkite on
all eight platforms, and `v1@new-rest` fingerprints equal to
`v1@old-rest` on every query under `CAPI_COMPRESSION=none` (enable
`("v1", "new-rest")` in `tests/e2e/bench/bench.py`).

In flight:

- The Windows build steps are flaky: qdbd dies under `TestRoundtrip`
  in about one run in seven, a bug in the daemon and not in this
  project. A separate Claude Code session investigates it on the
  branch `sc-19567/rr-ci-qdbd-logs`, checked out as a git worktree at
  `~/git/qdb-api-rest-ci-qdbd-logs`; that branch's
  `docs/ci-qdbd-logs-plan.md` holds everything about it, and the
  branch also carries the CI telemetry that uploads qdbd's logs with
  every test report. Development on this branch continues as usual; a
  red Windows job whose qdbd log ends mid-flush is that bug, and a
  re-run of the job is the answer.

Next:

1. The prose audit (root `AGENTS.md`, Prose) of every other comment and
   document in the project, one package or document per unit.
   `tests/e2e/bench/bench.py` is left out: the bench retires once the
   rewrite beats the old server (`docs/brief.md`, Testing doctrine).
2. The NDJSON and Arrow IPC decoders, each a `Decoder` in its format's
   file joining `codecs` and the round trip's draw of body formats
   (`internal/encoding/AGENTS.md`, The seam, and `internal/httpapi/AGENTS.md`,
   Tests), and `Content-Encoding: gzip|zstd` on the ingest.
3. `tests/e2e/tools/e2etool` (`gen`, `tocsv`), then `flow.sh` and
   `make test-flow` driving one server per cluster
   (`docs/e2e-v2-flow-plan.md`).
4. `scripts/cicd/40.test-e2e.sh` in the build step. The first
   Buildkite run of the flow is M2's exit.
5. The bench unit: the `http-arrow@new-rest` run, the first wall clock,
   time to first byte and RSS for the 5.6M-row query
   (`docs/bench.md`, "Protocols, servers, runs").
6. File upstream against `qdb-api-go`, with no local patch
   (`docs/brief.md`, Vendoring). `HandleType.APIVersion` and `APIBuild`
   release the static string from `qdb_version()` and `qdb_build()`
   through `qdb_release` with a nil handle, which `client.h` documents
   as API-managed and not to be freed.

Handoff to M2 (tables, reader and ingest):

- The flow drops its tables through `DELETE /api/v2/tables/{name}`,
  which leaves symtables. A create over an existing symtable is
  accepted (`internal/httpapi/tables_test.go`).
- The v1 suite keeps `seed.sql` and `make load`. The flow reads
  neither, and `make test-flow` loads nothing (`docs/e2e.md`, Dataset
  and "In Buildkite").
- The `make load` wall clock on the slowest agent is measured by the
  first CI run of the v1 suite, not of the flow (`docs/e2e.md`,
  Dataset).

Handoff to M4 (the v1 wrappers):

- The v1 byte-shape facts (key order, 401 bodies, error-message
  concatenation, find and gzip warts) are recorded in
  `docs/e2e.md`, "The v1 goldens".
- Every v1 route is a wrapper over its v2 counterpart and lives in
  `internal/httpapi/v1`, created with the first wrapper together
  with its own `AGENTS.md` (ADR-0007, with the rules in `internal/AGENTS.md`).
- The `find` wart (goldens 12 and 13) has no v2 endpoint until M7. Its
  v2 core is a tag-find function in `internal/qdb`, written in M4 and
  reused by M7's tags endpoint (`docs/brief.md`, Milestones).
- The goldens and the bench client exercise only the unversioned
  aliases (the old server knows no other spelling). The exit criterion
  additionally proves `/api/v1/<path>` answers identically, by replaying
  every golden at both spellings (ADR-0008).
- `v1@new-rest` runs under the bench's pinned C API compression
  through `cluster.compression` (`docs/bench.md`, "Two volumes").
- A `COUNT(...)` column is answered as `int64`, a deliberate deviation
  no golden exercises (`docs/brief.md`, "Deliberate deviations",
  and ADR-0013).

Blocked on:

- Nothing.

## Entries

## 2026-10-08 -- internal/qdb reads as plain sentences; prose-audit-qdb-plan.md deleted

- No fact moved, because the plan carried none. The remaining audit
  units stay under Current state, Next, item 1.

## 2026-10-07 -- the files the encode-test unit touched read as plain sentences; prose-audit-touched-plan.md deleted

- Owner decisions: accepted ADRs and dated entries are reworded with
  their decisions unchanged, and the bench is outside the audit. No fact
  moved, because the plan carried none.

## 2026-10-06 -- the encoders' tests are encode_test.go; encode-test-plan.md deleted

- Owner decisions: the project says "encode", never "render", for the
  encoding concept, and JSON, NDJSON and CSV are the text formats
  (`internal/encoding/AGENTS.md`), and comments and documents read as
  plain sentences (root `AGENTS.md`, Prose).

## 2026-10-06 -- the table fixture holds one record batch; fixture-arrow-plan.md deleted

- The fixture pushes through the Arrow writer and is checked by array
  equality. The column-type map is the binding's
  (`TsColumnType.ArrowType`, qdb-api-go PR 126). Owner decisions:
  QuasarDB's column types stay the fixture's vocabulary, and
  `internal/model` imports no binding. The rules went to
  `internal/AGENTS.md` (Tests, the read) and `internal/encoding/AGENTS.md`
  (Tests).

## 2026-10-05 -- the ingest decodes to Arrow; arrow-ingest-plan.md deleted

- `POST /api/v2/rows` decodes its body in `internal/encoding` into one
  record batch per table and pushes through the Go API's Arrow writer.
  The neutral types live in the new `internal/model`. Owner decisions:
  Arrow is the one representation between the layers, and one body has
  one column list, types included, which the decoder checks. The rules
  went to `internal/AGENTS.md` (the ingest), `internal/encoding/AGENTS.md`
  (the decoder) and `internal/model/AGENTS.md`.

## 2026-09-23 -- the CSV ingest landed; ingest-csv-plan.md deleted

- `POST /api/v2/rows` landed with a CSV body through the writer, one
  push per request. Owner decisions: the tables of one body share one
  column list, and the httpapi tests are one round trip and one error
  table. The rules went to `internal/AGENTS.md` (the ingest) and
  `internal/httpapi/AGENTS.md` (the route, the round trip).

## 2026-09-23 -- the table reader landed; table-reader-plan.md deleted

- `GET /api/v2/tables/{name}/rows` landed through the bulk reader, in
  every format. The rules went to `internal/AGENTS.md` (the read, the
  lent batches, the two C API defects), `internal/encoding/AGENTS.md`
  (`EncodeStream`, the JSON array) and `internal/httpapi/AGENTS.md`
  (the route's contract).

## 2026-09-22 -- M2 gains the table reader; the ingest is multi-table

- Owner decisions: whole tables are read through the bulk reader
  (`GET /api/v2/tables/{name}/rows`), never materialized by a query.
  There is one ingest endpoint, `POST /api/v2/rows`, routed by
  `$table`, so the ingestion milestone folds into M2 and the later
  milestones renumber (`docs/brief.md`, Milestones). The contracts are
  the handlers'.

## 2026-09-21 -- tables-plan.md deleted with table create and delete landed

- The wire contract went to `docs/brief.md`, "Tables: create and
  delete", the handler rules to `internal/httpapi/AGENTS.md`, and the
  owner's ingest parameters to Current state, Next.

## 2026-09-21 -- M1 closed; M2 started with table create and delete

- Owner decisions: table creation and ingest come before any CI run of
  the e2e, and the table endpoints need no ADR, because their contract
  is the brief's (`docs/brief.md`, "Tables: create and delete").

## 2026-09-18 -- ADR-0014 accepted: the v2 e2e is a generated flow; M2 -- tables and ingest inserted

- Owner decisions: the v2 e2e proves login, create, query, ingest and
  query back with generated rows, with no goldens and with error rows
  as Go tests. The two endpoints move into a new M2 and the later
  milestones renumber (`docs/brief.md`, Milestones). `docs/e2e.md` and
  `docs/bench.md` are permanent specifications (`docs/AGENTS.md`).

## 2026-09-17 -- the status probes are outside the compatibility contract

- Owner decision: v1 is the login and the query. The probes keep both
  paths with no old-server promise and no golden (`docs/brief.md`,
  Compatibility contract and Observability and logging).

## 2026-09-17 -- the suites are `v1` and `v2`; no v1 golden selects a count

- Owner decisions: `legacy` leaves every suite, target, driver, fixture,
  package and bench-run name. No v1 golden exercises a deliberate
  deviation (ADR-0013). A v2 case is one request with its formats
  inside (`docs/e2e.md`, "The v2 suite").

## 2026-09-17 -- test-strategy-plan.md deleted with the documentation re-cut landed

- The decisions went to ADR-0013, the layers, the deviations and the
  milestones to `docs/brief.md`, and the mechanics to `docs/e2e.md`
  and `docs/bench.md`.

## 2026-09-17 -- ADR-0013 accepted: e2e goldens run in Buildkite

- Owner decisions: e2e returns to CI from M1. A golden is an audited
  response. Budgets leave the CI gates, and every number is the
  bench's (`docs/brief.md`, Testing doctrine).

## 2026-09-16 -- compression-plan.md deleted with response compression landed

- The wire rule went to ADR-0012, and the middleware rules to
  `internal/httpapi/AGENTS.md`.

## 2026-09-16 -- ADR-0012 accepted: v2 response compression

- Owner decisions: client order decides and there is no `q`. The level
  is the fastest, with one compressor per response. zstd lands with
  gzip, since `arrow-go` already vendors and links it (in the brief,
  zstd leaves M4 and open question 3 is closed).

## 2026-09-15 -- login-plan.md deleted with the login endpoint landed

- The wire contract and the credential check went to ADR-0011, and the
  handler rules to `internal/httpapi/AGENTS.md`.

## 2026-09-15 -- ADR-0011 accepted: v2 login

- Owner decisions: credentials are proven by one direct dial outside
  the pools, breaker-gated and unbudgeted. The token response is RFC
  6749's. `auth.access_ttl` lands now, with a default of 15m.

## 2026-09-15 -- query-plan.md deleted with the query endpoint landed

- The wire contract went to ADR-0010, and the handler rules and the
  error mapping to `internal/httpapi/AGENTS.md`.

## 2026-09-15 -- ADR-0010 accepted: v2 query request, negotiation and errors

- The query is the body, `Accept` picks the encoder, errors are RFC 9457
  problems with the status saying who failed, and only bearer access
  tokens are accepted.

## 2026-09-12 -- the full-table text/csv equivalence leaves the harness

- Owner decision: it is not a target, and cross-format correctness is
  the Go property test's (`docs/brief.md`, Testing doctrine). The awk
  comparator and the section that specified it leave `docs/e2e.md`.

## 2026-09-11 -- encoders-plan.md deleted with the text encoders landed

- The text-format rules went to `internal/encoding/AGENTS.md`, which now
  holds every encoder rule, and the columns-only JSON shape and the
  `jsontext` appenders to `docs/brief.md`.

## 2026-09-11 -- arrow-query-plan.md deleted; ADR-0009 leaves the tree

- The query core hands out the batch `qdb_query_arrow` builds, so the
  wire schema is the binding's, not a REST decision. The ownership rule
  went to `internal/AGENTS.md`, Code, and the encoder rules to
  `internal/encoding/AGENTS.md`.

## 2026-09-10 -- table-fixture-plan.md deleted with the fixture landed

- The fixture rules and the writer's null contract went to
  `internal/AGENTS.md`, Tests, and the upstream request to Current
  state, Next.

## 2026-09-08 -- m1-plan.md deleted with the Arrow unit landed

- The wire types went to ADR-0009, the `count` deviation to
  `docs/brief.md`, Compatibility contract, and the result-set and
  vendoring rules to `internal/AGENTS.md`. Later M1 units bring their
  own plan.

## 2026-09-08 -- ADR-0009 accepted: Arrow wire types for query results

- Timestamp(ns, UTC), Utf8 and Binary, every field nullable, zero-copy
  from the binding's result set.

## 2026-09-08 -- count answers as int64 in v1

- Owner decision: a `count` column is an int64 and clients never told
  them apart, so v1 answers `"type":"int64"` where the old server said
  `"count"`. `docs/brief.md`, Compatibility contract.

## 2026-09-04 -- Session fate is the binding's; ADR-0003 removed

- Owner decision: `qdb-api-go` decides a session's fate (`IsBadSession`,
  through `Lease.Done`). This layer keeps the breaker (fed by
  `IsClusterUnavailable`), the budget, the per-user map and the opt-in
  read retry. Session health leaves `docs/brief.md`, Resilience.

## 2026-09-02 -- v2 auth precedes the drop-in; the find core lands with the wrappers

- Owner decision: M2 is v2 auth, M3 the drop-in, so the first shippable
  binary exposes no half-built v2 endpoint. The `find` wart's v2 core
  is written in M3. `docs/brief.md`, Milestones.

## 2026-09-02 -- admission control and the sentinels leave the scope

- Owner decision: the session budget is the whole overload mechanism.
  There is no admission layer and no 429. The unreachable `"(void)"` /
  `"(undefined)"` sentinels are not reproduced. `docs/brief.md`,
  Resilience and Compatibility contract.

## 2026-09-02 -- milestones re-cut into nine smaller ones

- Owner decision: M1 narrows to the v2 query path plus a minimal login.
  The remaining v2 surface, resilience, Flight SQL, ingestion and DuckDB
  become M3..M8 with one green bar each. `docs/brief.md`, Milestones.

## 2026-09-02 -- ADR-0007 accepted: v1 compatibility layer

- v2 comes first, every v1 route wraps its v2 counterpart, and v1 code
  lives in `internal/httpapi/v1` only. The direct v1 login leaves the
  tree and returns as a wrapper in M2.

## 2026-09-02 -- ADR-0008 records the /api/v1 spelling decision

- The 2026-09-01 owner decision (canonical `/api/v1/<path>`, unversioned
  aliases, never a redirect) moves from the brief into ADR-0008.

## 2026-09-02 -- milestones reordered: v2 data plane before drop-in compat

- Owner decision: v1 routes wrap v2, so v2 is built first and the
  v1 endpoints follow as thin wrappers in their own package
  (`docs/brief.md`, Milestones, and ADR-0007). The direct v1-query
  implementation and its plan were discarded. The verified wire facts
  moved to `docs/e2e.md`.

## 2026-09-02 -- /api/v1/tags dropped from scope

- Owner decision: it is unused, so it is removed from the compat
  surface, the goldens and the code. `docs/brief.md`, Compatibility
  contract.

## 2026-09-01 -- canonical spelling is /api/v1

- Owner decision: internal references always spell v1 endpoints
  `/api/v1/<path>`. The unversioned aliases are compatibility-only.
  ADR-0008.

## 2026-09-01 -- ADR-0005 accepted: token cryptography

- Hand-rolled dir+A256GCM compact JWE, argon2id + HKDF derivation,
  go-jose as the test-side cross-check. `internal/auth` and v1
  `/api/v1/login` land with it.

## 2026-09-01 -- ADR-0006 accepted: clock injection

- The clock is a `now func() time.Time` constructor argument, never
  context state. synctest is deferred to future pure-Go timer units.

## 2026-08-31 -- v1 aliases minted; v1 routes wrap v2

- Owner decision: every v1 endpoint is also served at `/api/v1/<path>`,
  unversioned paths assume v1, and new endpoints go under `/api/v2/*`
  only. v1 routes wrap v2 implementations, with no parallel code. `docs/brief.md`,
  Compatibility contract.

## 2026-08-31 -- Per-call deadlines are a binding concern, not the gateway's

- Owner decision: the abandon-on-deadline failsafe, session poisoning,
  the wedged counter and `pool.call_timeout` leave `internal/qdb`. Calls
  are bounded by the C socket timeout. If wanted, the capability belongs
  upstream (`qdb-api-go` or the C API). ADR-0003.

## 2026-08-31 -- Pool unit landed; ADR-0003 and ADR-0004 accepted

- `pool-plan.md` was deleted. Its facts moved to ADR-0003 (C API and qdbd
  constraints), `internal/AGENTS.md` (session-exhaustion test rule) and
  the Handoff block (bench buffer size, login handoff).

## 2026-08-30 -- Pool core taken from qdb-api-go

- Owner decision: the REST API integrates the binding's `SessionPool`
  and `SessionFactory`. The local core is dropped and eager option
  validation is out. ADR-0003.

## 2026-08-28 -- Pool review decisions

- Owner decisions: QuasarDB's vocabulary (user, session), every key in
  every layer, option checks left to the binding, the failsafe untested
  against the C API, and a session factory upstream. Recorded in
  `docs/brief.md`, Vocabulary, `cmd/qdb_rest/AGENTS.md` and ADR-0003.

## 2026-08-27 -- Pool plan review decisions

- Owner decisions: pools are keyed per user, not per session (brief
  amended, "Resilience and connection management"), C calls reach code
  only through a narrow `Handle` wrapper, and probes get their own
  ADR-0004 (accepted 2026-08-31). ADR-0003.

## 2026-08-26 -- Pool plan decisions

- Owner decisions: readiness dials its own handle and fails with `503`
  (brief contract amended), `IsRetryable` classifies errors, and nothing
  dials at startup. ADR-0003 and ADR-0004.

## 2026-08-25 -- M0 complete, M1 started

- Owner exit sign-off for M0. The M1 criteria are in Current state, and
  the scope is in `docs/brief.md`, Milestones.

## 2026-08-25 -- M0 scope complete

- Every M0 deliverable is landed and green on Buildkite for all eight
  platforms. The owner's exit sign-off is outstanding (Current state).

## 2026-08-24 -- e2e harness excluded from CI

- Owner decision, until further notice. The record and the re-adding
  recipe are in `.buildkite/AGENTS.md`.

## 2026-08-24 -- Packaging and Docker removed from the plan

- Owner decision: deb/rpm packaging and the Docker image leave the
  milestones entirely. `docs/brief.md`, Non-goals.

## 2026-08-24 -- Bench methodology decisions

- Owner decisions: `native@qdbd` measures `stream_query()` only, C API
  compression is pinned per run with the default `none`, and a run is 3
  warmups plus 5 measured reps with the median reported.
  `docs/bench.md`, decision log 2026-08-24.

## 2026-08-23 -- ADR-0002 accepted: context-carried logging

- The logger travels in `context.Context`. HTTP middleware tags request
  ids and writes the access line. The house rules are in
  `internal/AGENTS.md`.

## 2026-08-21 -- De-risk spikes removed from the roadmap

- Owner decision: the Flight SQL driver-compatibility and go-duckdb
  embedding spikes are dropped and neither topic is a risk. M3 and M4
  start on dependency order alone. `docs/brief.md`, Milestones and Risks.

## 2026-08-21 -- Go 1.27 adopted

- The toolchain is bumped. The 1.27 features blessed for this project are in
  `docs/brief.md`, Development standards.

## 2026-08-21 -- ADR-0001 accepted: TLS certificates

- A PEM pair from config, with an ephemeral self-signed default. ACME,
  ACM and hot reload are deferred.

## 2026-08-21 -- Dataset archive published on S3

- `make -C tests/e2e load` fetches `reproduce-2026-08-19-5613032.tar.gz`
  through `datasets.json`. No local copy is needed anywhere.

## 2026-08-20 -- M0 started

- The entry criteria are met. The scope is in `docs/brief.md`,
  Milestones, and the criteria are in Current state.

## 2026-08-20 -- Bench Phase 1 accepted

- `native@qdbd` and `v1@old-rest` fingerprints agree on every query.
  The tool is ready for `v1@new-rest`. The contract decisions are in
  `docs/bench.md`, decision log 2026-08-20.

## 2026-08-19 -- Bench measures two data volumes

- qdbd -> reducer and reducer -> client, with a reduce-shape query family.
  `docs/bench.md`, "Two volumes" and decision log 2026-08-19.

## 2026-08-19 -- e2e harness in place; M1 red bar exists

- The dataset is loaded and round-trip verified, the v1 goldens are
  captured from `master`, and `make test-v1` fails fast without a
  server under test.
  `docs/e2e.md`, decision log 2026-08-19.

## 2026-08-16 -- Planning frozen

- The e2e harness and bench plans are approved. The decision logs of
  2026-08-16 are in `docs/e2e.md` and `docs/bench.md`.

## 2026-08-14 -- Old-server baseline measured

- A pre-harness spike. The bench's `v1@old-rest` result files supersede

  its numbers (`docs/bench.md`).
