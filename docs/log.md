# Project Log

Status: living document. The top block is rewritten in place. The entries
below are append-only, newest first. The conventions are in `docs/AGENTS.md`.

## Current state

Last updated: 2026-10-13

| Milestone                       | State       | Note                                                  |
| ------------------------------- | ----------- | ----------------------------------------------------- |
| M0 -- Foundation                | done        | exit signed off 2026-08-25                            |
| M1 -- v2 query                  | done        | closed 2026-09-21                                     |
| M2 -- Tables, reader and ingest | done        | closed 2026-10-12                                     |
| M3 -- v2 auth                   | in progress | ADR-0016 fixes the shapes, criteria below             |
| M4 -- Embedded DuckDB           | not started | ADR-0017 fixes the design, criteria below             |
| M5 -- Flight SQL                | not started | one endpoint over both query layers, so it follows M4 |
| M6 -- Resilience                | not started |                                                       |
| M7 -- Exploration               | not started |                                                       |
| M8 -- Drop-in compat            | not started | the v1 goldens are captured when it starts (ADR-0015) |
| M9 -- Release                   | not started |                                                       |

M3 criteria. Entry: ADR-0016 accepted (met 2026-10-13). Exit:
`auth.refresh_ttl` in the config, login answering the pair,
`POST /api/v2/auth/refresh`, `POST /api/v2/auth/logout` and
`GET /api/v2/session` landed with their property tests (pair roundtrip,
sliding refresh expiry, rotation continuity through a refresh, each
`typ` refused by the other's route), and the round trip's login step
calling refresh and session, green in Buildkite on all eight platforms.

In flight:

- Nothing.

Next:

1. M3 unit one: `auth.refresh_ttl` and `Tokens.MintPair`, with login
   answering `refresh_token` (ADR-0016, Lifetimes and Refresh).
2. M3 unit two: the refresh, logout and session handlers and their
   round-trip step (ADR-0016; `docs/e2e.md`, "The v2 round trip").
3. File upstream against `qdb-api-go`, with no local patch
   (`docs/brief.md`, Vendoring). `HandleType.APIVersion` and `APIBuild`
   release the static string from `qdb_version()` and `qdb_build()`
   through `qdb_release` with a nil handle, which `client.h` documents
   as API-managed and not to be freed.

M4 criteria. Entry: ADR-0017 accepted (met 2026-10-13). Exit: the
`duckdb` config block, `POST /api/v2/sql` over one DuckDB instance per
user held in the user pool state, linked from qdb-duck's package with
the paired go-duckdb tag, the native-versus-DuckDB equivalence property
test and the round trip's sql step, green in Buildkite on all eight
platforms with FreeBSD answering `501`.

Handoff to M5 (Flight SQL):

- Whether Flight SQL gets a subcommand of `e2etool` or stays with the
  property tests is decided at M5's entry (`docs/e2e.md`, "The
  driver").
- The Flight SQL endpoint serves both the regular query and the DuckDB
  layer, and `internal/olap` exposes the DuckDB query as the batch
  iterator the encoders take (ADR-0017).

Blocked on:

- Nothing.

## Entries

## 2026-10-13 -- M4 designed: ADR-0017 accepted, one DuckDB instance per user, FreeBSD without DuckDB

- Owner decisions; ADR-0017, and `docs/brief.md`, Embedded DuckDB,
  carries the result. Open question 2 is closed.

## 2026-10-13 -- M3 started: ADR-0016 accepted, token lifetimes and the refresh, logout and session shapes

- Owner decisions; ADR-0016, and `docs/brief.md`, Authentication,
  carries the result. Open question 1 is closed.

## 2026-10-12 -- early-preview scope: milestones reordered, bench, dataset and v1 goldens removed

- Owner decision; ADR-0015. The brief's Milestones and Testing doctrine
  and `docs/e2e.md` carry the result, and ADR-0013 is superseded.

## 2026-10-12 -- M2 closed: the round trip runs in every build step; three e2e plans deleted

- `scripts/cicd/40.test-e2e.sh` runs `make test-round-trip` after the Go
  tests on every platform. Owner decisions: the tool builds into `bin/`,
  the harness Makefile's trap is the one stop of the REST servers, and M3
  starts with the next unit. `ci-e2e-plan.md`, `e2e-round-trip-plan.md`
  and `e2e-v2-round-trip-plan.md` deleted; facts moved to `docs/e2e.md`,
  decision logs of 2026-10-11 and 2026-10-12.

## 2026-10-12 -- the v2 e2e is the round trip, and its surface is defined

- Owner decisions: the name is "round trip", the sequence of
  `TestRoundtrip` over HTTP; the round trip reads and queries
  (`SELECT *`) four tables in every format under identity and gzip,
  and the empty read, zstd, request codings and push modes stay Go
  tests (`docs/e2e.md`, "The v2 round trip").

## 2026-10-12 -- the basic v2 e2e flow landed; e2e-basic-flow-plan.md deleted

- `make -C tests/e2e test-flow` runs on both clusters. Facts moved to
  `tests/e2e/AGENTS.md` and ADR-0014's Consequences; the rest is in the
  tool's and the driver's comments.

## 2026-10-12 -- three qdbd defects filed with R&D

- sc-19942, sc-19944 and sc-19939 are R&D's to fix, and the test
  failures they cause are in `internal/AGENTS.md`, Tests.

## 2026-10-11 -- the columnar JSON decoder landed; json-ingest-plan.md deleted

- Facts moved to `internal/encoding/AGENTS.md`, The seam, and the
  comments of `internal/encoding/json.go`.

## 2026-10-11 -- the ingest takes columnar JSON; the e2e flow starts with one basic test

- Owner decisions: `POST /api/v2/rows` accepts the columnar JSON the
  read answers (`docs/brief.md`, endpoint sketch and M2), the next unit
  implements it, the first e2e unit is one basic test and the surface
  is defined with the owner afterwards, the tool stays Go
  (`docs/e2e-v2-flow-plan.md`, "Owner decisions (2026-10-11)").

## 2026-10-09 -- the NDJSON and Arrow IPC decoders and the request codings landed; ingest-decoders-plan.md deleted

- `POST /api/v2/rows` reads CSV, NDJSON and Arrow IPC bodies under
  `Content-Encoding: gzip|zstd`. Owner decisions: the ingest cap bounds
  the bytes on the wire, a sparse NDJSON object reads absent as null, a
  body's Arrow types must equal the reader's, the text wires share one
  cell parser and the standard library parses NDJSON, and the decoders'
  faults are one property. The rules went to `internal/encoding/AGENTS.md`
  (The seam, Tests) and `internal/httpapi/AGENTS.md` (Handlers,
  Middleware, Tests).

## 2026-10-08 -- the prose audit is complete

- Every comment and document outside `tests/e2e/bench/bench.py` reads as
  plain sentences. Owner decisions: the remainder landed as one unit
  without a plan document, and `internal/model/AGENTS.md`,
  `tests/e2e/tools/package-dataset.sh` and the ADR template joined it.
  No fact moved.

## 2026-10-08 -- the internal/httpapi sources and AGENTS.md read as plain sentences; prose-audit-httpapi-plan.md deleted

- No fact moved, because the plan carried none. Owner decision: the
  audit lands in slices of one review each, and Current state, Next,
  item 1 names what remains.

## 2026-10-08 -- internal/qdb reads as plain sentences; prose-audit-qdb-plan.md deleted

- No fact moved, because the plan carried none. Owner decision: the
  rest of the audit is one unit, listed under Current state, Next,
  item 1.

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
