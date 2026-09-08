# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-09-08

| Milestone             | State       | Note                                            |
| --------------------- | ----------- | ----------------------------------------------- |
| M0 -- Foundation      | done        | exit signed off 2026-08-25                      |
| M1 -- v2 query        | in progress | auth core, pool core, Arrow encoder landed      |
| M2 -- v2 auth         | not started |                                                 |
| M3 -- Drop-in compat  | not started | red bar exists: `make -C tests/e2e test-legacy` |
| M4 -- Resilience      | not started | entry decides whether e2e returns to CI         |
| M5 -- Flight SQL      | not started |                                                 |
| M6 -- Exploration     | not started |                                                 |
| M7 -- Ingestion       | not started |                                                 |
| M8 -- Embedded DuckDB | not started |                                                 |
| M9 -- Release         | not started |                                                 |

M1 criteria. Entry (met): M0 signed off; `qdb-api-go` vendored at the
upstream that links `libqdb_api.a` statically on Linux. Exit:
`make -C tests/e2e` full-table `text/csv` equivalence green against
`bin/qdb_rest`; the format-equivalence property test (JSON, NDJSON,
CSV, Arrow IPC) green on all eight platforms; `POST /api/v2/auth/login`
mints an access token the query endpoint accepts; gzip negotiated via
`Accept-Encoding`; time-to-first-byte and server RSS for the 5.6M-row
query recorded in the e2e results.

M3 criteria. Entry: v2 auth and query are landed (M1 and M2 exits);
the 18 legacy goldens replay against a server under test (already
true). Exit: every legacy golden green against `bin/qdb_rest` at both
spellings; `bench-legacy@new-rest` fingerprints equal `legacy@old-rest`
on every query under `CAPI_COMPRESSION=none` (enable
`("legacy", "new-rest")` in `tests/e2e/bench/bench.py`).

In flight:

- Nothing.

Next:

1. The rest of M1, each unit with its own plan before it starts: the
   JSON, NDJSON and CSV encoders over the same seam as the Arrow one
   (`internal/encoding`, ADR-0009); `POST /api/v2/query` with `Accept` negotiation and the
   flushing writer; the bearer middleware, the minimal login and gzip;
   the `tests/e2e` target that runs the full-table `text/csv`
   equivalence against `/api/v2/query` (`test-legacy` is the only
   replay target today). The handler unit must raise
   `cluster.max_in_buffer_size` for the bench's full-table query: the C
   API default (256 MiB) cannot return the 5.6M-row `SELECT *`; the old
   server's e2e flags use 8 GiB (`tests/e2e/Makefile`). An oversized
   reply (`ErrNetworkInbufTooSmall`) is fatal in the binding, so it
   costs no reconnect; the v2 engine maps it to a client error.
2. File upstream against `qdb-api-go`: `HandleType.APIVersion` and
   `APIBuild` release the static string from `qdb_version()` /
   `qdb_build()` through `qdb_release` with a nil handle, which
   `client.h` documents as API-managed and not to be freed. No local
   patch (`docs/brief.md`, Vendoring).
3. At M4's entry: decide whether the e2e harness returns to CI or the
   budgets run locally (`.buildkite/AGENTS.md` holds the decision and
   the recipe).

Handoff to M3 (the legacy wrappers):

- The legacy byte-shape facts -- key order, 401 bodies, error-message
  concatenation, find and gzip warts -- are recorded in
  `docs/e2e-plan.md`, "The CSV is the expected output".
- Every legacy route is a wrapper over its v2 counterpart and lives in
  `internal/httpapi/legacy`, a package that does not exist yet and
  carries its own `AGENTS.md` (ADR-0007; rules in `internal/AGENTS.md`).
- The `find` wart (goldens 12 and 13) has no v2 endpoint until M6; its
  v2 core is a tag-find function in `internal/qdb`, written in M3 and
  reused by M6's tags endpoint (`docs/brief.md`, Milestones).
- The goldens and the bench client exercise only the unversioned
  aliases (the old server knows no other spelling); the exit criterion
  additionally proves `/api/v1/<path>` answers identically, by replaying
  every login and query golden at both spellings. The probe goldens
  have one spelling (ADR-0008).
- Client-side C API compression is an explicit config knob, default
  `none`, so `legacy@new-rest` runs under the bench's pinned mode
  (`docs/bench-plan.md`, "Two volumes").
- Golden 07 pins `"type":"count"`; v1 answers `int64` there
  (`docs/brief.md`, v1 query). The replay normalizes `count` to
  `int64` on the golden side, or the golden is re-captured with the
  deviation applied.

Deferred to M9 (release), tracked nowhere else:

- Windows service mode: Event Log for lifecycle events, `log.file` with
  rotation (`docs/brief.md`, "Observability and logging").
- `qdb-release` version registration for the `VERSION` file
  (`docs/brief.md`, "Versioning and release").

Blocked on:

- Nothing.

## Entries

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

## 2026-09-02 -- ADR-0007 accepted: legacy compatibility layer

- v2 first; every v1 route wraps its v2 counterpart; legacy code in
  `internal/httpapi/legacy` only. The direct legacy login leaves the
  tree and returns as a wrapper in M2.

## 2026-09-02 -- ADR-0008 records the /api/v1 spelling decision

- The 2026-09-01 owner decision (canonical `/api/v1/<path>`, unversioned
  aliases, never a redirect) moves from the brief into ADR-0008.

## 2026-09-02 -- milestones reordered: v2 data plane before drop-in compat

- Owner decision: v1 routes wrap v2, so v2 is built first and the
  legacy endpoints follow as thin wrappers in their own package
  (`docs/brief.md`, Milestones; ADR-0007). The direct legacy-query
  implementation and its plan were discarded; the verified wire facts
  moved to `docs/e2e-plan.md`.

## 2026-09-02 -- legacy /api/v1/tags dropped from scope

- Owner decision: unused; removed from the compat surface, the goldens
  and the code. `docs/brief.md`, Compatibility contract.

## 2026-09-01 -- canonical spelling is /api/v1

- Owner decision: internal references always spell legacy endpoints
  `/api/v1/<path>`; the unversioned aliases are compatibility-only.
  ADR-0008.

## 2026-09-01 -- ADR-0005 accepted: token cryptography

- Hand-rolled dir+A256GCM compact JWE, argon2id + HKDF derivation,
  go-jose as the test-side cross-check. `internal/auth` and legacy
  `/api/v1/login` land with it.

## 2026-09-01 -- ADR-0006 accepted: clock injection

- The clock is a `now func() time.Time` constructor argument, never
  context state; synctest deferred to future pure-Go timer units.

## 2026-08-31 -- v1 aliases minted; v1 routes wrap v2

- Owner decision: every legacy endpoint also served at `/api/v1/<path>`,
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
  reps, median reported. `docs/bench-plan.md`, decision log 2026-08-24.

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

- `native@qdbd` and `legacy@old-rest` fingerprints agree on every query;
  the tool is ready for `legacy@new-rest`. Contract decisions:
  `docs/bench-plan.md`, decision log 2026-08-20.

## 2026-08-19 -- Bench measures two data volumes

- qdbd -> reducer and reducer -> client, with a reduce-shape query family.
  `docs/bench-plan.md`, "Two volumes" and decision log 2026-08-19.

## 2026-08-19 -- e2e harness in place; M1 red bar exists

- Dataset loaded and round-trip verified, legacy goldens captured from
  `master`, `make test-legacy` fails fast without a server under test.
  `docs/e2e-plan.md`, decision log 2026-08-19.

## 2026-08-16 -- Planning frozen

- e2e harness and bench plans approved. Decision logs 2026-08-16 in
  `docs/e2e-plan.md` and `docs/bench-plan.md`.

## 2026-08-14 -- Old-server baseline measured

- `docs/bench-plan.md`, "Verified baseline".
