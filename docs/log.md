# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-08-23

| Milestone             | State       | Note                                  |
| --------------------- | ----------- | ------------------------------------- |
| M0 -- Foundation      | in progress | skeleton serves status probes         |
| M1 -- Drop-in compat  | not started | e2e legacy goldens + red bar in place |
| M2 -- v2 data plane   | not started |                                       |
| M3 -- Flight SQL      | not started |                                       |
| M4 -- Embedded DuckDB | not started |                                       |
| M5 -- Release         | not started |                                       |

In flight:

- M0. Landed: go module, `cmd/qdb_rest` (status probes, graceful
  shutdown, dual HTTP/HTTPS listeners), `internal/config`,
  `internal/observe` (context-carried logger, ADR-0002),
  `internal/tlsconf` (ADR-0001), request middleware with `X-Request-Id`
  and access lines, root `Makefile`;
  dataset archive on S3 (harness download verified); goldens 20/21
  green under the harness (see 2026-08-20 and 2026-08-21 entries).

Next:

1. M0 remaining, roughly in order: Buildkite CI skeleton (includes the
   `VERSION` file and `-ldflags` build metadata per the brief's
   Versioning section; `make build` has neither yet, and the
   golangci-lint v2 + gofumpt pin does not exist yet either), packaging +
   Docker.
2. M1: make `make -C tests/e2e test-legacy QDB_REST_BIN=...` green, then
   `make -C tests/e2e/bench bench-legacy@new-rest` (the bench is built and
   waiting; enable the registry row by clearing its gate in `bench.py`).

Blocked on:

- Nothing.

## Entries

## 2026-08-23 -- Context-carried logging and request middleware

- Landed: `observe.WithLogger` / `Logger` / `WithAttrs` plus the shared
  attribute vocabulary (`KeyRequestID`, `KeyError`, `Err`); `cmd/qdb_rest`
  threads the logger through `http.Server.BaseContext` and routes
  net/http's `ErrorLog` through the same handler, no `slog.SetDefault`;
  `httpapi` request middleware (inbound `X-Request-Id` honored when
  well-formed, minted as a v7 UUID from the stdlib `uuid` package
  otherwise, always echoed; one access line per request). Decision:
  ADR-0002. House rules: `internal/AGENTS.md`.
- Verified: `go test ./...` (attr scoping, id echo/mint, Flush through the
  recorder); smoke run shows the access line carrying `request_id`;
  status goldens 20/21 replay green (no header or body change).
- Lesson: a wrapping `http.ResponseWriter` must implement `Unwrap()` or
  `http.ResponseController` loses Flush and deadlines -- fatal for a
  streamed response path, and invisible until the first stream.

## 2026-08-21 -- De-risk spikes removed from the roadmap

- Owner decision (Leon): the two M0 de-risk spikes -- Flight SQL driver
  compatibility and go-duckdb + qdb-duck embedding -- are removed from
  the roadmap entirely; neither topic is to be considered a risk. The
  brief's M0 scope, its Flight SQL and Embedded DuckDB sections, and
  Risks items 1-2 are updated accordingly (remaining risks renumbered;
  `docs/bench-plan.md`'s "Open risk 5" reference is now "Open risk 3").
- M3 and M4 are therefore not gated on anything from M0; they start on
  their dependency order alone. The 2026-08-20 M0-start entry's exit
  criteria listed "both de-risk spikes"; that entry stays as written
  (append-only), this one supersedes its spike clause.

## 2026-08-21 -- Toolchain: go 1.27

- `go.mod` bumped to `go 1.27` / `toolchain go1.27.0` (installed at
  `/opt/local/lib/go-1.27`); build, vet and all tests green, no code
  changes needed.
- The brief's Development standards now bless the 1.27 additions worth
  knowing about: generic methods, the stdlib `uuid` package, and
  `encoding/json/v2` (relevant to M2's `internal/encoding`; adoption
  there is an ADR decision). "Template functions" in release chatter =
  generic methods.

## 2026-08-21 -- M0: config, logging, TLS listener; dataset on S3

- Landed: `internal/config` (YAML + `${VAR}` env interpolation + env
  overrides + flags; precedence flags > env > file > defaults; unknown
  YAML keys and unset `${VAR}` are errors), `internal/observe` (slog
  construction: json | console, level vocabulary), `internal/tlsconf`
  (PEM pair from config, or an ephemeral self-signed ECDSA P-256
  certificate generated at startup with a logged sha256 fingerprint;
  decision in ADR-0001), `cmd/qdb_rest` reworked to serve HTTP `:40080`
  and HTTPS `:40443` from config with one graceful drain across both.
  First vendored dependency: `gopkg.in/yaml.v3`.
- Verified: unit tests (config precedence/interpolation, TLS handshake
  with the generated certificate); smoke run -- both probes answer on
  both listeners, ephemeral warning logged, SIGTERM drain clean; status
  goldens 20/21 replay green with the harness's new default
  `REST_ARGS` (`-listen-tls=` so a test server never binds 40443).
- Dataset archive uploaded to S3 by the operator; verified through the
  harness's own `download-golden` path (fresh download + sha256), so
  `DATASETS_LOCAL_DIR` is no longer needed anywhere.
- Deferred by ADR-0001: ACME/Let's Encrypt (inapplicable inside customer
  networks), ACM (keys not exportable; LB-in-front already works),
  certificate hot reload (additive later).

## 2026-08-20 -- M0 started: go skeleton + status probes

- Criteria. Entry (met): e2e harness with legacy goldens and the bench in
  place, dataset loaded, red bar for M1 exists. Exit: the brief's M0 scope
  -- config, logging, TLS, probes, CI on all platforms with the static
  `libqdb_api.a` dance, packaging, Docker image, both de-risk spikes.
- Landed: `go.mod` (`github.com/bureau14/qdb-api-rest`, go 1.26, zero
  third-party deps), `internal/httpapi` (probe handlers at the legacy
  paths and `/api/v2` mirrors; 200, empty body, no Content-Type),
  `cmd/qdb_rest` (`-listen` flag, slog JSON to stdout, SIGTERM drain
  bounded below the harness's 10 s kill window), root `Makefile`
  (`make build`). Old-checkout clutter removed (`apps/`, `etc/`,
  `.buildkite/`, `db/`).
- Harness: `legacy.sh` fetches its token lazily (first case with
  `auth != none`) and tolerates non-login responses; `CASES=` selects a
  golden subset on all capture/replay targets; `REST_ARGS` defaults to the
  rewrite's `-listen` flag. Verified: selfcheck 21/21;
  `make -C tests/e2e test-legacy QDB_REST_BIN=bin/qdb_rest
CASES='20-status-liveness 21-status-readiness'` passes 2/2 with the
  harness owning start/stop; the full replay fails fast at login -- the
  M1 red bar, now reachable per case.
- Lesson: the eager login in `legacy.sh` coupled every replay to
  `/api/login`, making even unauthenticated goldens unreachable against a
  partial server; lazy token fetch plus case selection is the general fix
  and doubles as single-case debugging during M1.

## 2026-08-20 -- Bench Phase 1 built; native == legacy@old-rest

- `tests/e2e/bench/` built per `docs/bench-plan.md` (Implementation order
  steps 2-5): Makefile (`check`/`venv`/`old-server`/`bench-*`/`report`),
  `bench.py` (run/report/selftest + `_child`), `protocols/{native,legacy}`,
  `servers/old_rest`, stubs for `new_rest`/`flightsql`. Chosen reduce-family
  SQL and contract decisions recorded in the plan (decision log 2026-08-20).
- Acceptance gate green: all 7 queries fingerprint-equal across
  native@qdbd query, native@qdbd stream and legacy@old-rest, 3 reps each;
  `wart_count` 0 everywhere (sentinels unreachable under the 3.15 C API,
  as the goldens pin). Numbers live in `tests/e2e/bench/results/`; the
  legacy `full` TTFB reproduces the 2026-08-14 pre-harness baseline, and
  `agg_topk` ships 284 MiB to the reducer against 346 B to the client --
  the gateway thesis is now a measured, repeatable number.
- Lessons:
  - `quasardb.stats.by_node` reads every stat key (~3k requests per call)
    and drowns small queries; the bench reads the two request counters
    key-by-key over a direct node connection (~5 requests).
  - All-null columns have no observable wire type (legacy JSON types them
    `none`, the native client picks a dtype); fingerprints treat them
    type-free or the protocols disagree on `matchableIdList`.
  - `GROUP BY` row order is deterministic across protocols on this
    dataset (edge-row fingerprints match), so `agg_wide` needs no ORDER BY.
  - The Python binding's default client input buffer (256 MiB) fails
    `agg_wide`/`full`; the bench pins it to the old server's 8 GiB flag.
  - The wheel's package version (3.14.3.dev0) is qdb-api-python's own;
    `quasardb.version()` reports the C API (3.15.0.dev0). Parity is the
    C API hash, not the package version.
  - Legacy `full` decoded body is ~28 MB smaller than the 2026-08-14 curl
    baseline: JSON `null` replaces the longer `"(undefined)"` rendering.

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
