# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-08-19

| Milestone             | State       | Note                                   |
| --------------------- | ----------- | -------------------------------------- |
| M0 -- Foundation      | not started | first target; see brief Milestones     |
| M1 -- Drop-in compat  | not started | e2e legacy goldens + red bar in place  |
| M2 -- v2 data plane   | not started |                                        |
| M3 -- Flight SQL      | not started | gated on M0 driver-compat spike        |
| M4 -- Embedded DuckDB | not started | gated on M0 go-duckdb + qdb-duck spike |
| M5 -- Release         | not started |                                        |

In flight:

- Nothing.

Next:

1. Upload the dataset archive to S3 (`aws s3 cp`, command printed by
   `make -C tests/e2e package-dataset`; entry already in
   `tests/e2e/datasets.json`, sha256 `175bdb58...`); until then
   `make load DATASETS_LOCAL_DIR=~/datasets`.
2. M0 entry criteria: define, record here. Remove old-checkout clutter
   (`apps/`, `etc/`, `.buildkite/tools/__pycache__`, `db/`) by hand.
3. Bench Phase 1 (`docs/bench-plan.md`, Implementation order, steps 2-4;
   includes the reduce-shape query family and the two-volume metrics);
   its `make old-server` delegates to `tests/e2e`.
4. M1: make `make -C tests/e2e test-legacy QDB_REST_BIN=...` green.

Blocked on:

- Nothing.

## Entries

## 2026-08-19 -- Bench: two-volume measurement and reduce-shape queries

- Investigated how to make the benchmark show the map/reduce thesis
  (reduce runs in the C-API holder; the gateway moves it next to qdbd).
  Outcome recorded in `docs/bench-plan.md`, "Two volumes" + the
  reduce-shape query family + decision log 2026-08-19. No hard numbers
  recorded on purpose; the bench will produce them.
- Lessons:
  - qdbd's `$qdb.statistics.requests.out_bytes` / `total_count` give the
    qdbd -> client volume; refresh is periodic (500 ms in test-setup),
    so settle before reading. `nettop` sees nothing on macOS loopback.
  - `LIMIT` is pushed down; `ORDER BY <agg> LIMIT k` is not -- the full
    aggregate reaches the reducer. That gap is the thing to measure.
  - qdbd -> client volume scales with groups x shards, not rows; on this
    dataset (96 x 15-min shards, low-cardinality string keys) only
    high-cardinality keys (`id`) or fine buckets load the reducer.
    Second-granularity buckets are qdbd-bound and hide protocol
    differences on localhost.
  - Probing tools that work today: `qdbsh --output-format csv` as a C
    client; `direct_set_node` + `direct_int_get` via qdbsh stdin (the
    `-c` flag cannot be repeated). Single-shot `SELECT *` of the full
    table overflows qdbsh's input buffer (125 MiB), same as `qdb_export`.

## 2026-08-19 -- e2e harness: dataset, legacy goldens, red bar for M1

- First commit on the branch (docs skeleton, `.gitignore`); qdb-test-setup
  added as a git submodule at `scripts/tests/setup` (SHA `685fd72b`, same
  as qdb-nats-connector).
- `tests/e2e/` built per `docs/e2e-plan.md`: `Makefile`, `common.sh`,
  `datasets.json`, `legacy.sh`, `seed.sql`, `tools/package-dataset.sh`,
  21 legacy golden cases under `golden/legacy/`, `README.md`, `AGENTS.md`.
  Verified end to end: `make load` (idempotent), `make verify-dataset`
  (byte-identical round trip), `make seed` (idempotent), `make old-server`,
  `make capture-golden`, `make test-legacy-selfcheck` (21/21), and
  `make test-legacy` failing fast with "no server under test" -- the red
  bar M1 turns green.
- Dataset archive `reproduce-2026-08-19-5613032.tar.gz` produced from the
  sc-19522 `db.tar.zst`; local copy in `~/datasets`; S3 upload pending
  (see Next).
- Lessons (details in `docs/e2e-plan.md`, Dataset and "The CSV is the
  expected output"):
  - Single-shot `qdb_export` of the full table overflows the client input
    buffer; export per shard-sized range and concatenate (byte-identical).
  - The legacy JSON renders timestamps in the server's local zone
    (`time.Unix`); the harness pins `TZ=UTC` everywhere.
  - Against the 3.15 C API the `"(void)"`/`"(undefined)"` sentinels of the
    old server are unreachable: nulls are typed `none` and serialize as
    JSON `null`, column type = last non-null row, `"none"` if all null.
    Goldens pin that. Worth a note in the brief's wart list when M1
    starts; the rewrite can keep the sentinel mapping for typed undefined
    values at no cost.
  - go-swagger auth errors are `{"code":401,"message":...}` without a
    trailing newline; handler errors are `{"message":...}` with one;
    status probes answer 200 with an empty body and no `Content-Type`;
    gzip is applied on any `Accept-Encoding` containing `gzip`, no `Vary`.
    All pinned by goldens 03/04, 14/19, 20/21, 16.
  - Old-server facts not to copy: readiness opens a new qdb handle per
    request and never closes it; `--port` is overridden by `--local`.
  - `make capture-golden` against the old server works with `master`'s
    binary because its legacy wire code equals `v3.14.2` (diff-verified).

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
