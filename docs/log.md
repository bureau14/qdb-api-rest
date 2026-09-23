# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-09-22

| Milestone                       | State       | Note                                                                   |
| ------------------------------- | ----------- | ---------------------------------------------------------------------- |
| M0 -- Foundation                | done        | exit signed off 2026-08-25                                             |
| M1 -- v2 query                  | done        | closed 2026-09-21                                                      |
| M2 -- Tables, reader and ingest | in progress | the v2 e2e flow is its exit (ADR-0014)                                 |
| M3 -- v2 auth                   | not started |                                                                        |
| M4 -- Drop-in compat            | not started | local red bar exists: `make -C tests/e2e test-v1`; joins CI when green |
| M5 -- Resilience                | not started |                                                                        |
| M6 -- Flight SQL                | not started |                                                                        |
| M7 -- Exploration               | not started |                                                                        |
| M8 -- Embedded DuckDB           | not started |                                                                        |
| M9 -- Release                   | not started |                                                                        |

M2 criteria. Exit: `POST /api/v2/tables`, `DELETE /api/v2/tables/{name}`,
`GET /api/v2/tables/{name}/rows` (every format) and `POST /api/v2/rows`
(CSV, NDJSON, Arrow IPC bodies) landed with their ingest/read roundtrip
property tests per input format; the v2 e2e flow (`docs/e2e.md`, "The
v2 flow") green in Buildkite on all eight platforms through
`scripts/cicd/40.test-e2e.sh`.

M4 criteria. Entry: v2 auth and query are landed (M1 and M3 exits);
the 14 v1 goldens replay against a server under test. Exit: every
v1 golden green against `bin/qdb_rest` at both
spellings, in Buildkite on all eight platforms; `v1@new-rest` fingerprints equal `v1@old-rest`
on every query under `CAPI_COMPRESSION=none` (enable
`("v1", "new-rest")` in `tests/e2e/bench/bench.py`).

In flight:

- Nothing.

Next:

1. The table reader: `GET /api/v2/tables/{name}/rows` through the bulk
   reader's Arrow sequence (`Reader.Arrow`, vendored), one record
   batch per fetch encoded before the next is fetched, in every
   format; `?start=&end=` both or neither, `?columns=a,b`. Owner-fixed:
   JSON stays column-oriented as a top-level array of the query's
   `{"columns":[..]}` objects, one per batch, so a cut stream is
   invalid JSON; an empty table answers its schema (one batch with
   empty `data`, the header alone, a schema with no batches), read
   through `ColumnsInfo` since the reader yields nothing for it.
2. The ingest slice: `POST /api/v2/rows` with the CSV parser and its
   roundtrip property test through the table reader. Owner-fixed: multi-table,
   a required `$table` column routing each row and a required
   `$timestamp`, the header the union of the tables' columns, an empty
   field null; the parser streams record by record into the writer's
   columns, one push per request; a 64 MiB body cap (1 MiB stays for
   every other body); strictly the CSV encoder's dialect;
   `?push-mode=transactional|fast|async`, default `fast`;
   `?deduplication-mode=drop|upsert`, absent meaning none, with
   `?deduplication-columns=a,b` required by either mode (the columns
   say what a duplicate is, the mode what happens to one); the answer
   200 with `{"rows", "tables", "parse_ms", "push_ms"}`, `async`
   included since the push call returned.
3. The NDJSON and Arrow IPC parsers with their property tests;
   `Content-Encoding: gzip|zstd` on the ingest.
4. `tests/e2e/tools/e2etool` (`gen`, `tocsv`), then `flow.sh` and
   `make test-flow` driving one server per cluster
   (`docs/e2e-v2-flow-plan.md`).
5. `scripts/cicd/40.test-e2e.sh` in the build step; the first
   Buildkite run of the flow is M2's exit.
6. The bench unit: the `http-arrow@new-rest` run, the first wall clock,
   time to first byte and RSS for the 5.6M-row query
   (`docs/bench.md`, "Protocols, servers, runs").
7. File upstream against `qdb-api-go`, no local patch (`docs/brief.md`,
   Vendoring): `HandleType.APIVersion` and `APIBuild` release the static
   string from `qdb_version()` / `qdb_build()` through `qdb_release` with
   a nil handle, which `client.h` documents as API-managed and not to be
   freed.

Handoff to M2 (tables, reader and ingest):

- The flow drops its tables through `DELETE /api/v2/tables/{name}`,
  which leaves symtables; a create over an existing symtable is
  accepted (`internal/httpapi/tables_test.go`).
- The flow's generated rows carry no empty string (ADR-0014,
  Consequences); the ingest parser reads an empty CSV field as null.
- The bulk reader's Arrow schema differs from `qdb_query_arrow`'s in
  two details a table reader client may notice: `$table` and `$timestamp` are
  non-nullable and no field carries `max_width` metadata; the encoders
  read neither. Two C API defects surface through the table reader and are
  filed upstream, not worked around: the Arrow path drops one trailing
  NUL byte from a string cell, and two symbol-bearing tables in one
  reader fail with invalid argument (the table reader reads one table).
- The v1 suite keeps `seed.sql` and `make load`; the flow reads
  neither, and `make test-flow` loads nothing (`docs/e2e.md`, Dataset
  and "In Buildkite").
- The `make load` wall clock on the slowest agent is measured by the
  first CI run of the v1 suite, not of the flow (`docs/e2e.md`,
  Dataset).

Handoff to M4 (the v1 wrappers):

- The v1 byte-shape facts -- key order, 401 bodies, error-message
  concatenation, find and gzip warts -- are recorded in
  `docs/e2e.md`, "The v1 goldens".
- Every v1 route is a wrapper over its v2 counterpart and lives in
  `internal/httpapi/v1`, created with the first wrapper together
  with its own `AGENTS.md` (ADR-0007; rules in `internal/AGENTS.md`).
- The `find` wart (goldens 12 and 13) has no v2 endpoint until M7; its
  v2 core is a tag-find function in `internal/qdb`, written in M4 and
  reused by M7's tags endpoint (`docs/brief.md`, Milestones).
- The goldens and the bench client exercise only the unversioned
  aliases (the old server knows no other spelling); the exit criterion
  additionally proves `/api/v1/<path>` answers identically, by replaying
  every golden at both spellings (ADR-0008).
- `v1@new-rest` runs under the bench's pinned C API compression
  through `cluster.compression` (`docs/bench.md`, "Two volumes").
- A `COUNT(...)` column is answered as `int64`, a deliberate deviation
  no golden exercises (`docs/brief.md`, "Deliberate deviations";
  ADR-0013).

Blocked on:

- Nothing.

## Entries

## 2026-09-22 -- M2 gains the table reader; the ingest is multi-table

- Owner decisions: whole tables are read through the bulk reader
  (`GET /api/v2/tables/{name}/rows`), never materialized by a query;
  one ingest endpoint, `POST /api/v2/rows`, routed by `$table`, so the
  ingestion milestone folds into M2 and the later milestones renumber
  (`docs/brief.md`, Milestones); the contracts are the handlers'.

## 2026-09-21 -- tables-plan.md deleted with table create and delete landed

- The wire contract to `docs/brief.md`, "Tables: create and delete";
  the handler rules to `internal/httpapi/AGENTS.md`; the owner's ingest
  parameters to Current state, Next.

## 2026-09-21 -- M1 closed; M2 started with table create and delete

- Owner decisions: table creation and ingest come before any CI run of
  the e2e; the table endpoints need no ADR, their contract is the
  brief's (`docs/brief.md`, "Tables: create and delete").

## 2026-09-18 -- ADR-0014 accepted: the v2 e2e is a generated flow; M2 -- tables and ingest inserted

- Owner decisions: the v2 e2e proves login, create, query, ingest and
  query back with generated rows, no goldens, error rows as Go tests;
  the two endpoints move into a new M2 and the later milestones
  renumber (`docs/brief.md`, Milestones); `docs/e2e.md` and
  `docs/bench.md` are permanent specifications (`docs/AGENTS.md`).

## 2026-09-17 -- the status probes are outside the compatibility contract

- Owner decision: v1 is the login and the query; the probes keep both
  paths with no old-server promise and no golden (`docs/brief.md`,
  Compatibility contract and Observability and logging).

## 2026-09-17 -- the suites are `v1` and `v2`; no v1 golden selects a count

- Owner decisions: `legacy` leaves every suite, target, driver, fixture,
  package and bench-run name; no v1 golden exercises a deliberate
  deviation (ADR-0013); a v2 case is one request with its formats
  inside (`docs/e2e.md`, "The v2 suite").

## 2026-09-17 -- test-strategy-plan.md deleted with the documentation re-cut landed

- The decisions to ADR-0013; the layers, the deviations and the
  milestones to `docs/brief.md`; the mechanics to `docs/e2e.md`
  and `docs/bench.md`.

## 2026-09-17 -- ADR-0013 accepted: e2e goldens run in Buildkite

- Owner decisions: e2e returns to CI from M1; a golden is an audited
  response; budgets leave the CI gates and every number is the
  bench's (`docs/brief.md`, Testing doctrine).

## 2026-09-16 -- compression-plan.md deleted with response compression landed

- The wire rule to ADR-0012; the middleware rules to
  `internal/httpapi/AGENTS.md`.

## 2026-09-16 -- ADR-0012 accepted: v2 response compression

- Owner decisions: client order decides, no `q`; fastest level, one
  compressor per response; zstd lands with gzip since `arrow-go` already
  vendors and links it (brief: zstd leaves M4, open question 3 closed).

## 2026-09-15 -- login-plan.md deleted with the login endpoint landed

- The wire contract and the credential check to ADR-0011; the handler
  rules to `internal/httpapi/AGENTS.md`.

## 2026-09-15 -- ADR-0011 accepted: v2 login

- Owner decisions: credentials proven by one direct dial outside the
  pools, breaker-gated and unbudgeted; RFC 6749 token response;
  `auth.access_ttl` now, default 15m.

## 2026-09-15 -- query-plan.md deleted with the query endpoint landed

- The wire contract to ADR-0010; the handler rules and the error mapping
  to `internal/httpapi/AGENTS.md`.

## 2026-09-15 -- ADR-0010 accepted: v2 query request, negotiation and errors

- The query is the body, `Accept` picks the encoder, errors are RFC 9457
  problems with the status saying who failed, bearer access tokens only.

## 2026-09-12 -- the full-table text/csv equivalence leaves the harness

- Owner decision: not a target; cross-format correctness is the Go
  property test's (`docs/brief.md`, Testing doctrine). The awk
  comparator and the section that specified it leave `docs/e2e.md`.

## 2026-09-11 -- encoders-plan.md deleted with the rendering encoders landed

- The rendering rules to `internal/encoding/AGENTS.md`, which now holds
  every encoder rule; the columns-only JSON shape and the `jsontext`
  appenders to `docs/brief.md`.

## 2026-09-11 -- arrow-query-plan.md deleted; ADR-0009 leaves the tree

- The query core hands out the batch `qdb_query_arrow` builds, so the
  wire schema is the binding's, not a REST decision; the ownership rule
  to `internal/AGENTS.md`, Code, the encoder rules to
  `internal/encoding/AGENTS.md`.

## 2026-09-10 -- table-fixture-plan.md deleted with the fixture landed

- The fixture rules and the writer's null contract to `internal/AGENTS.md`,
  Tests; the upstream request to Current state, Next.

## 2026-09-08 -- m1-plan.md deleted with the Arrow unit landed

- Wire types to ADR-0009; the `count` deviation to `docs/brief.md`,
  Compatibility contract; result-set and vendoring rules to
  `internal/AGENTS.md`. Later M1 units bring their own plan.

## 2026-09-08 -- ADR-0009 accepted: Arrow wire types for query results

- Timestamp(ns, UTC), Utf8 and Binary, every field nullable, zero-copy
  from the binding's result set.

## 2026-09-08 -- count answers as int64 in v1

- Owner decision: a `count` column is an int64 and clients never told
  them apart, so v1 answers `"type":"int64"` where the old server said
  `"count"`. `docs/brief.md`, Compatibility contract.

## 2026-09-04 -- Session fate is the binding's; ADR-0003 removed

- Owner decision: `qdb-api-go` decides a session's fate (`IsBadSession`,
  through `Lease.Done`); this layer keeps the breaker (fed by
  `IsClusterUnavailable`), the budget, the per-user map and the opt-in
  read retry. Session health leaves `docs/brief.md`, Resilience.

## 2026-09-02 -- v2 auth precedes the drop-in; the find core lands with the wrappers

- Owner decision: M2 is v2 auth, M3 the drop-in, so the first shippable
  binary exposes no half-built v2 endpoint. The `find` wart's v2 core
  is written in M3. `docs/brief.md`, Milestones.

## 2026-09-02 -- admission control and the sentinels leave the scope

- Owner decision: the session budget is the whole overload mechanism;
  no admission layer, no 429. The unreachable `"(void)"` /
  `"(undefined)"` sentinels are not reproduced. `docs/brief.md`,
  Resilience and Compatibility contract.

## 2026-09-02 -- milestones re-cut into nine smaller ones

- Owner decision: M1 narrows to the v2 query path plus a minimal login;
  the remaining v2 surface, resilience, Flight SQL, ingestion and DuckDB
  become M3..M8 with one green bar each. `docs/brief.md`, Milestones.

## 2026-09-02 -- ADR-0007 accepted: v1 compatibility layer

- v2 first; every v1 route wraps its v2 counterpart; v1 code in
  `internal/httpapi/v1` only. The direct v1 login leaves the
  tree and returns as a wrapper in M2.

## 2026-09-02 -- ADR-0008 records the /api/v1 spelling decision

- The 2026-09-01 owner decision (canonical `/api/v1/<path>`, unversioned
  aliases, never a redirect) moves from the brief into ADR-0008.

## 2026-09-02 -- milestones reordered: v2 data plane before drop-in compat

- Owner decision: v1 routes wrap v2, so v2 is built first and the
  v1 endpoints follow as thin wrappers in their own package
  (`docs/brief.md`, Milestones; ADR-0007). The direct v1-query
  implementation and its plan were discarded; the verified wire facts
  moved to `docs/e2e.md`.

## 2026-09-02 -- /api/v1/tags dropped from scope

- Owner decision: unused; removed from the compat surface, the goldens
  and the code. `docs/brief.md`, Compatibility contract.

## 2026-09-01 -- canonical spelling is /api/v1

- Owner decision: internal references always spell v1 endpoints
  `/api/v1/<path>`; the unversioned aliases are compatibility-only.
  ADR-0008.

## 2026-09-01 -- ADR-0005 accepted: token cryptography

- Hand-rolled dir+A256GCM compact JWE, argon2id + HKDF derivation,
  go-jose as the test-side cross-check. `internal/auth` and v1
  `/api/v1/login` land with it.

## 2026-09-01 -- ADR-0006 accepted: clock injection

- The clock is a `now func() time.Time` constructor argument, never
  context state; synctest deferred to future pure-Go timer units.

## 2026-08-31 -- v1 aliases minted; v1 routes wrap v2

- Owner decision: every v1 endpoint also served at `/api/v1/<path>`,
  unversioned paths assume v1, new endpoints under `/api/v2/*` only; v1
  routes wrap v2 implementations, no parallel code. `docs/brief.md`,
  Compatibility contract.

## 2026-08-31 -- Per-call deadlines are a binding concern, not the gateway's

- Owner decision: the abandon-on-deadline failsafe, session poisoning,
  the wedged counter and `pool.call_timeout` leave `internal/qdb`; calls
  are bounded by the C socket timeout. If wanted, the capability belongs
  upstream (`qdb-api-go` or the C API). ADR-0003.

## 2026-08-31 -- Pool unit landed; ADR-0003 and ADR-0004 accepted

- `pool-plan.md` deleted; facts moved to ADR-0003 (C API and qdbd
  constraints), `internal/AGENTS.md` (session-exhaustion test rule) and
  the Handoff block (bench buffer size, login handoff).

## 2026-08-30 -- Pool core taken from qdb-api-go

- Owner decision: the REST API integrates the binding's `SessionPool`
  and `SessionFactory`; the local core is dropped and eager option
  validation is out. ADR-0003.

## 2026-08-28 -- Pool review decisions

- Owner decisions: QuasarDB's vocabulary (user, session); every key in
  every layer; option checks left to the binding; the failsafe untested
  against the C API; a session factory upstream. `docs/brief.md`,
  Vocabulary; `cmd/qdb_rest/AGENTS.md`; ADR-0003.

## 2026-08-27 -- Pool plan review decisions

- Owner decisions: pools keyed per user, not per session (brief amended,
  "Resilience and connection management"); C calls reach code only
  through a narrow `Handle` wrapper; probes get their own ADR-0004
  (accepted 2026-08-31). ADR-0003.

## 2026-08-26 -- Pool plan decisions

- Owner decisions: readiness dials its own handle and fails with `503`
  (brief contract amended); `IsRetryable` classifies errors; nothing
  dials at startup. ADR-0003; ADR-0004.

## 2026-08-25 -- M0 complete, M1 started

- Owner exit sign-off for M0; M1 criteria in Current state; scope:
  `docs/brief.md`, Milestones.

## 2026-08-25 -- M0 scope complete

- Every M0 deliverable is landed and green on Buildkite for all eight
  platforms; owner exit sign-off outstanding (Current state).

## 2026-08-24 -- e2e harness excluded from CI

- Owner decision, until further notice. Record and re-adding recipe:
  `.buildkite/AGENTS.md`.

## 2026-08-24 -- Packaging and Docker removed from the plan

- Owner decision: deb/rpm packaging and the Docker image leave the
  milestones entirely. `docs/brief.md`, Non-goals.

## 2026-08-24 -- Bench methodology decisions

- Owner decisions: `native@qdbd` measures `stream_query()` only; C API
  compression pinned per run, default `none`; 3 warmups + 5 measured
  reps, median reported. `docs/bench.md`, decision log 2026-08-24.

## 2026-08-23 -- ADR-0002 accepted: context-carried logging

- The logger travels in `context.Context`; HTTP middleware tags request
  ids and writes the access line. House rules: `internal/AGENTS.md`.

## 2026-08-21 -- De-risk spikes removed from the roadmap

- Owner decision: the Flight SQL driver-compatibility and go-duckdb
  embedding spikes are dropped and neither topic is a risk; M3 and M4
  start on dependency order alone. `docs/brief.md`, Milestones and Risks.

## 2026-08-21 -- Go 1.27 adopted

- Toolchain bumped; the 1.27 features blessed for this project are in
  `docs/brief.md`, Development standards.

## 2026-08-21 -- ADR-0001 accepted: TLS certificates

- PEM pair from config, ephemeral self-signed default; ACME, ACM and hot
  reload deferred.

## 2026-08-21 -- Dataset archive published on S3

- `make -C tests/e2e load` fetches `reproduce-2026-08-19-5613032.tar.gz`
  through `datasets.json`; no local copy needed anywhere.

## 2026-08-20 -- M0 started

- Entry criteria met. Scope: `docs/brief.md`, Milestones; criteria:
  Current state.

## 2026-08-20 -- Bench Phase 1 accepted

- `native@qdbd` and `v1@old-rest` fingerprints agree on every query;
  the tool is ready for `v1@new-rest`. Contract decisions:
  `docs/bench.md`, decision log 2026-08-20.

## 2026-08-19 -- Bench measures two data volumes

- qdbd -> reducer and reducer -> client, with a reduce-shape query family.
  `docs/bench.md`, "Two volumes" and decision log 2026-08-19.

## 2026-08-19 -- e2e harness in place; M1 red bar exists

- Dataset loaded and round-trip verified, v1 goldens captured from
  `master`, `make test-v1` fails fast without a server under test.
  `docs/e2e.md`, decision log 2026-08-19.

## 2026-08-16 -- Planning frozen

- e2e harness and bench plans approved. Decision logs 2026-08-16 in
  `docs/e2e.md` and `docs/bench.md`.

## 2026-08-14 -- Old-server baseline measured

- A pre-harness spike; the bench's `v1@old-rest` result files supersede
  its numbers (`docs/bench.md`).
