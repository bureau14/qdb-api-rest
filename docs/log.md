# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-08-19

| Milestone             | State       | Note                                   |
| --------------------- | ----------- | -------------------------------------- |
| M0 -- Foundation      | not started | first target; see brief Milestones     |
| M1 -- Drop-in compat  | not started |                                        |
| M2 -- v2 data plane   | not started |                                        |
| M3 -- Flight SQL      | not started | gated on M0 driver-compat spike        |
| M4 -- Embedded DuckDB | not started | gated on M0 go-duckdb + qdb-duck spike |
| M5 -- Release         | not started |                                        |

In flight:

- Planning documents complete (`brief.md`, `e2e-plan.md`, `bench-plan.md`);
  no code on this branch yet (orphan branch, no commits).

Next:

1. Commit the docs skeleton.
2. M0 entry criteria: define, record here.
3. e2e prerequisites (`docs/e2e-plan.md`, Layout): shared
   `scripts/tests/setup/`, dataset conversion + S3 upload, `make load`.
4. Bench Phase 1 (`docs/bench-plan.md`, Implementation order, steps 2-4).

Blocked on:

- Nothing.

## Entries

## 2026-08-19 -- Documentation scaffolding

- Added root `AGENTS.md` (hierarchical instruction convention, folder
  pointers), `docs/AGENTS.md` (docs conventions, reading order,
  one-writer-per-fact rule), this log, and `docs/adr/` with a template.
  Root `CLAUDE.md` is only `@AGENTS.md`.
- Normalized the `Status:` lines of the brief and plans to the lifecycle
  vocabulary; progress now lives only in this file.

## 2026-08-16 -- Planning decisions

- e2e harness and bench decisions taken; see the decision-log tables in
  `docs/e2e-plan.md` and `docs/bench-plan.md`.

## 2026-08-14 -- Old-server baseline measured

- Old REST API `full` query: 834 MB JSON, 29.3 s TTFB, ~8.4 GB RSS. Details
  in `docs/bench-plan.md`, "Verified baseline".
- Lesson: the deployed old binary never parses the go-swagger server flag
  group, so documented HTTP timeouts are never applied. Do not equalize
  timeouts when comparing.
