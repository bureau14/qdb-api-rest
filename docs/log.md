# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-08-25

| Milestone             | State       | Note                                              |
| --------------------- | ----------- | ------------------------------------------------- |
| M0 -- Foundation      | in progress | scope complete; owner exit sign-off pending       |
| M1 -- Drop-in compat  | not started | red bar in place: `make -C tests/e2e test-legacy` |
| M2 -- v2 data plane   | not started |                                                   |
| M3 -- Flight SQL      | not started |                                                   |
| M4 -- Embedded DuckDB | not started |                                                   |
| M5 -- Release         | not started |                                                   |

M0 criteria. Entry (met): e2e harness with legacy goldens, the bench,
the dataset loaded, a red bar for M1. Exit: the brief's M0 scope --
config, logging, TLS, status probes, Buildkite CI on all supported
platforms with the C-API artifact dance, e2e harness and bench
scaffolding against a live qdbd -- landed and verified in CI, then owner
sign-off.

In flight:

- M0: awaiting owner exit sign-off.

Next:

1. M0 exit sign-off (owner).
2. M1: make `make -C tests/e2e test-legacy QDB_REST_BIN=<bin>` green,
   then run `make -C tests/e2e/bench bench-legacy@new-rest` (the
   registry row is gated in `bench.py`; clear the gate).
3. Circle back, no date: return the e2e harness to CI
   (`.buildkite/AGENTS.md` holds the decision and the recipe).

Handoff to M1:

- Vendoring qdb-api-go is what turns the Linux static `libqdb_api.a`
  from asserted-present into linked; the canonical CGO environment lands
  in `scripts/cicd/00.common.sh` at the same time (`scripts/cicd/AGENTS.md`,
  `.buildkite/AGENTS.md`).
- Client-side C API compression is an explicit config knob, default
  `none`, so `legacy@new-rest` runs under the bench's pinned mode
  (`docs/bench-plan.md`, "Two volumes").
- The readiness probe uses a pooled handle under the service user; the
  old server opened a fresh handle per probe and never closed it
  (`docs/brief.md`, "Resilience and connection management").
- Legacy goldens pin JSON `null` for null cells because the
  `"(void)"` / `"(undefined)"` sentinels are unreachable under the 3.15
  C API; keeping the brief's sentinel mapping for typed undefined values
  is compatible with the goldens either way (`docs/e2e-plan.md`, "The
  CSV is the expected output").

Blocked on:

- Nothing.

## Entries

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
