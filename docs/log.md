# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-08-27

| Milestone             | State       | Note                                     |
| --------------------- | ----------- | ---------------------------------------- |
| M0 -- Foundation      | done        | exit signed off 2026-08-25               |
| M1 -- Drop-in compat  | in progress | red bar: `make -C tests/e2e test-legacy` |
| M2 -- v2 data plane   | not started |                                          |
| M3 -- Flight SQL      | not started |                                          |
| M4 -- Embedded DuckDB | not started |                                          |
| M5 -- Release         | not started |                                          |

M1 criteria. Entry (met): M0 signed off; `qdb-api-go` vendored at the
upstream that links `libqdb_api.a` statically on Linux; the 21 legacy
goldens replay against a server under test; the bench's
`legacy@new-rest` row awaits enabling. Exit: every legacy golden green
against `bin/qdb_rest`; `bench-legacy@new-rest` fingerprints equal
`legacy@old-rest` on every query under `CAPI_COMPRESSION=none`; the
readiness probe dials the cluster as the service user and answers
`200`/`503`; the auth ADR (JWE library, AEAD, key derivation) accepted;
property tests for auth and the pool green on all eight platforms.

In flight:

- M1, cluster config + handle pool + readiness: planned in
  `docs/pool-plan.md` (owner decisions taken 2026-08-26 and 2026-08-27)
  with ADR-0003 (pool) and ADR-0004 (probes) proposed. No code yet.

Next:

1. `cluster:`, `pool:` and `status:` config blocks, the `internal/qdb`
   handle pool, and the readiness probe on its own dial
   (`docs/pool-plan.md`, Implementation order).
2. Auth ADR, then `internal/auth`.
3. `/api/login`, `/api/query` with the legacy JSON encoder, `/api/tags`,
   golden by golden until `make -C tests/e2e test-legacy QDB_REST_BIN=<bin>`
   is green.
4. Add `("legacy", "new-rest")` to `ENABLED` in `tests/e2e/bench/bench.py`
   and run `make -C tests/e2e/bench bench-legacy@new-rest`.
5. File upstream against `qdb-api-go`: `HandleType.APIVersion` and
   `APIBuild` release the static string from `qdb_version()` /
   `qdb_build()` through `qdb_release` with a nil handle, which
   `client.h` documents as API-managed and not to be freed. No local
   patch (`docs/brief.md`, Vendoring).
6. Circle back, no date: return the e2e harness to CI
   (`.buildkite/AGENTS.md` holds the decision and the recipe).

Handoff to M1:

- Client-side C API compression is an explicit config knob, default
  `none`, so `legacy@new-rest` runs under the bench's pinned mode
  (`docs/bench-plan.md`, "Two volumes").
- The readiness probe dials its own handle as the service user, outside
  the pool, and fails with `503` (ADR-0004; `docs/brief.md`,
  Compatibility contract).
- `cluster.max_in_buffer_size` must be raised for the bench's full-table
  query, as the old server's e2e flags do (`docs/pool-plan.md`, Handoff
  notes).
- Legacy goldens pin JSON `null` for null cells; the `"(void)"` /
  `"(undefined)"` sentinels are unreachable under the 3.15 C API, so
  keeping the brief's sentinel mapping for typed undefined values is
  compatible either way (`docs/e2e-plan.md`, "The CSV is the expected
  output").

Deferred to M5, tracked nowhere else:

- Windows service mode: Event Log for lifecycle events, `log.file` with
  rotation (`docs/brief.md`, "Observability and logging").
- `qdb-release` version registration for the `VERSION` file
  (`docs/brief.md`, "Versioning and release").

Blocked on:

- Nothing.

## Entries

## 2026-08-27 -- Pool plan review decisions

- Owner decisions: pools keyed per user, not per session (brief amended,
  "Resilience and connection management"); C calls reach code only
  through a narrow `Handle` wrapper; probes get their own ADR-0004
  (proposed). `docs/pool-plan.md`, Owner decisions; ADR-0003.

## 2026-08-26 -- Pool plan decisions

- Owner decisions: readiness dials its own handle and fails with `503`
  (brief contract amended); `IsRetryable` classifies errors; nothing
  dials at startup. `docs/pool-plan.md`, Owner decisions; ADR-0003.

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
