# Project Log

Status: living document. Top block is rewritten in place; entries below are
append-only, newest first. Conventions: `docs/AGENTS.md`.

## Current state

Last updated: 2026-08-25

| Milestone             | State       | Note                                                                |
| --------------------- | ----------- | ------------------------------------------------------------------- |
| M0 -- Foundation      | in progress | CI green on all 8 platforms (build 43); exit pending owner sign-off |
| M1 -- Drop-in compat  | not started | e2e legacy goldens + red bar in place                               |
| M2 -- v2 data plane   | not started |                                                                     |
| M3 -- Flight SQL      | not started |                                                                     |
| M4 -- Embedded DuckDB | not started |                                                                     |
| M5 -- Release         | not started |                                                                     |

In flight:

- M0. Landed: go module, `cmd/qdb_rest` (status probes, graceful
  shutdown, dual HTTP/HTTPS listeners), `internal/config`,
  `internal/observe` (context-carried logger, ADR-0002),
  `internal/tlsconf` (ADR-0001), request middleware with `X-Request-Id`
  and access lines, root `Makefile`;
  dataset archive on S3 (harness download verified); goldens 20/21
  green under the harness (see 2026-08-20 and 2026-08-21 entries);
  `VERSION` file + `-ldflags` build metadata + `-version` flag; Buildkite
  pipeline skeleton (lint + 8-platform build/unit-test; see 2026-08-24
  CI entry) verified green end-to-end on Buildkite, `-race` on all
  platforms including Windows (build 43; see 2026-08-25 entry).

Next:

1. M0 exit sign-off (owner): everything in the amended M0 scope is
   landed and CI-verified; the static `libqdb_api.a` is asserted present
   on Linux but actually linked only when M1 vendors qdb-api-go.
2. M1: make `make -C tests/e2e test-legacy QDB_REST_BIN=...` green, then
   `make -C tests/e2e/bench bench-legacy@new-rest` (the bench is built and
   waiting; enable the registry row by clearing its gate in `bench.py`).
3. Circle back (no date): the e2e harness is excluded from CI until
   further notice (owner decision, 2026-08-24 CI entry); what re-adding
   takes is noted in `.buildkite/AGENTS.md`.

Blocked on:

- Nothing.

## Entries

## 2026-08-25 -- First Buildkite runs green; Windows -race wiring

- Builds 41-43 on `sc-19567/rest-rewrite` (created via API with
  `ignore_pipeline_branch_filters: true`; no web-side changes). Build 43
  is the keeper: 10/10 passed -- lint, all 8 platform build+unit-test
  steps with `-race`, aggregate report. The artifact dance held on every
  platform (c-api resolved from quasardb-build `master` via the plugin's
  ref fallback), the GO127/gcc15 agent vars exist everywhere including
  macOS, and the `-version` smoke printed injected metadata on all 8.
- Build 41 failed both Windows legs: `go: -race requires cgo`. Wrong
  first fix (dropping `-race` on Windows) was reverted on owner
  instruction; the real cause is PATH: gcc.exe lives in native
  `C:\mingw64\bin` (`/c/mingw64/bin` under MSYS) and the MSYS-internal
  `/mingw64/bin` on the job PATH does not resolve there, so go silently
  disables cgo. Fix mirrors qdb-nats-connector: `cicd_setup_c_toolchain`
  (the prepend, `00.common.sh`) plus `windows-go-test-exec.sh` as the
  `go test -exec` wrapper (PATH converted to Windows form so test
  binaries resolve MinGW runtime DLLs under the service context).
- Lessons:
  - The org-wide `branch_configuration: "master 3.14.x"` gates only
    automatic webhook builds. Manual/API builds on any branch are
    normal and pass `ignore_pipeline_branch_filters: true`; the `bk`
    CLI (3.46) does not set it, so its 422 on a feature branch is not
    "builds blocked".
  - Buildkite build creation wants a full 40-char SHA; resolve it with
    `git rev-parse`, never expand an abbreviation by hand -- a wrong
    SHA fails checkout as GitHub's `upload-pack: not our ref`, which
    reads like a replication problem and is not.

## 2026-08-24 -- VERSION, build metadata, Buildkite CI skeleton

- Landed: `VERSION` file (`3.15.0.dev0`, the single version-string
  location; the qdb-release registration itself stays in M5); build
  metadata injected via `-ldflags` per qdb-nats-connector ADR-011
  (version, commit, build time, build mode, GOAMD64 level as
  `main`-package vars; a `-version` flag surfaced through
  `config.ErrVersionRequested` so the metadata stays in `main`; one
  "starting" log line with version/commit/build_mode); root `Makefile`
  wiring. `.buildkite/` (dynamic `pipeline.py`, step templates,
  `qdb-cicd-tools` as the `.buildkite/tools` submodule) and
  `scripts/cicd/` (`00.common.sh`, `10.lint.sh`, `20.build.sh`,
  `30.test-unit.sh`). Graph: 1 lint + 8 per-platform build+unit-test
  steps (same matrix as qdb-nats-connector, mirroring quasardb name for
  name) + 1 aggregate test report.
- Owner decision (Leon): the e2e harness is **excluded from CI until
  further notice** -- no qdbd, no dataset, no `test-legacy` in the
  pipeline. Tracked as a circle-back item in Current state; the
  re-adding recipe (server/utils archives, start-services, pre-exit
  hook) is in `.buildkite/AGENTS.md`.
- Decisions worth keeping:
  - Steps download only the c-api archive. Nothing links it yet, but
    `cicd_assert_qdb_tree` asserts `qdb/lib` + `qdb/include` (plus
    `libqdb_api.a` on Linux) on every platform, so the artifact dance
    is proven now, while builds are trivial and failures cheap.
  - `10.lint.sh` delegates to `make lint` (the lint step is Linux-only,
    GNU make available): the golangci-lint pin keeps exactly one
    writer, the root Makefile. The per-platform scripts call `${GO}`
    directly instead (FreeBSD ships BSD make, Windows agents none).
  - No `upload`/`promote` plugin blocks anywhere: the pipeline
    publishes no artifacts (packaging is out of the plan), and an
    artifact-less promote would poison `LATEST_SUCCESSFUL`.
- Facts verified while building:
  - The qdb-artifacts plugin walks [requested ref, master, main] when
    resolving downloads, so feature-branch builds of this repo resolve
    quasardb-build's master artifacts without any special-casing.
  - errcheck flags unchecked `fmt.Fprintf` to an `io.Writer`;
    `versionText()` renders into a `strings.Builder` and `fmt.Print`s
    once instead.
- Verified locally: `make build` + `bin/qdb_rest -version` shows the
  injected metadata; `make lint` 0 issues; `go test ./...` green;
  `pipeline.py check` valid (10 steps) and the generated YAML inspected
  (keys, queues, depends_on, `$$`-escaped agent vars). Not verified: a
  real Buildkite run (see Next). The web pipeline needs no change:
  `master` already drives its `.buildkite/pipeline.py` through the same
  `generate|check` entrypoint and requirements.txt, so this branch's
  generator is a drop-in for the existing upload step.

## 2026-08-24 -- Packaging and Docker removed from the plan

- Owner decision (Leon): deb/rpm packaging and the Docker image are a
  complete non-concern for now and are removed from milestones and task
  planning entirely -- dropped from M0's scope and from M5's
  packaging-finalization clause; the brief's Goals item 7 no longer
  promises a Docker image and a new Non-goals bullet records the
  deferral (including the `%config(noreplace)` RPM note, preserved
  there). They return only as a deliberate re-add when deployment
  becomes a concern.
- M0's remaining scope is therefore the Buildkite CI skeleton (with the
  `VERSION` file and `-ldflags` build metadata). The 2026-08-20 M0-start
  entry's exit criteria listed packaging and the Docker image; that
  entry stays as written (append-only), this one supersedes that clause.

## 2026-08-24 -- Bench: warmup reps and median aggregation

- Owner decision (Leon): every query now runs 3 discarded warmups followed
  by 5 measured repetitions, and the report shows the median of the 5
  (previously: mean of 3, no warmup -- the warmth bias diagnosed earlier
  today). `WARMUP` / `REPS` Makefile variables; `WARMUP=0 REPS=1` for
  smokes. Warmups use the identical measurement path, are persisted
  flagged `warmup: true` (cold-start walls stay inspectable) and count for
  the fingerprint check but never for the medians. Result-file schema:
  `means` renamed to `medians`.
- Cost: a full `legacy@old-rest` run grows from ~5 to ~12 minutes,
  dominated by the `full` query. Accepted for a deliberate local tool.

## 2026-08-24 -- Bench: compression pinned to none; aggregation gap explained

- Investigated why `legacy@old-rest` beat `native@qdbd` on the aggregation
  queries (`agg_topk` 3.76 s vs 4.88 s). Two causes, fully decomposed, no
  architectural component:
  1. **Compression default mismatch (~0.5 s on `agg_topk`)**:
     qdb-api-python's `Cluster` defaults to `qdb_comp_balanced`; the old
     server's bare `qdb.NewHandle()` never sets compression and the C API
     default is `qdb_comp_none` (`qdb/option.h` says so explicitly). Over
     loopback, balanced is pure CPU tax. Python with compression disabled
     hits 3.77-3.80 s -- identical to the old server.
  2. **Run-ordering warmth bias (~0.6 s)**: native ran first against a
     colder qdbd; warm native `agg_coarse` (0.27-0.33 s) matches legacy's
     0.32 s exactly.
- Ruled out along the way: client parallelism (flat 1..16), buffer sizes
  (equalized), HTTP/JSON overhead (bare qdb-api-go probe == old server),
  Python wrapper overhead (binding metrics: wall == raw `qdb_query` time).
- Owner decision (Leon): every run uses **no compression**; the bench pins
  it via `CAPI_COMPRESSION` (Makefile, default `none`, passed as a required
  `--capi-compression` flag), `legacy@old-rest` fails fast on any other
  value, and the effective per-run mode is recorded in the result file.
  Facts and decision row in `docs/bench-plan.md` (Two volumes mechanics +
  decision log 2026-08-24).
- Also verified: the `$qdb.statistics.requests.out_bytes` counter is
  pre-compression (byte-identical across balanced/uncompressed runs), so
  volume-1 comparability was never affected.
- M1 note: the rewrite must set client compression explicitly (config
  knob, default none) so `legacy@new-rest` runs under the same pinned mode
  -- and because compression moves reduce-heavy walls by ~13%, silently
  inheriting a different default would fake a regression or a win.

## 2026-08-24 -- Bench: native@qdbd is stream_query only

- Owner decision (Leon): the native reference measures `stream_query()`
  only; the one-shot `qdb_query` sub-mode is removed from the bench
  (`docs/bench-plan.md`, decision log 2026-08-24). The mode machinery in
  `bench.py` (per-mode child config, per-mode means, the `("query",
"stream")` loop) is deleted with it. No result-file compatibility:
  stale files are regenerated, never parsed (owner rule, same day).
- The 2026-08-20 acceptance gate's operative fact stands: native stream
  and legacy@old-rest fingerprints agree on all 7 queries.

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
- `observe.Logger` panics on a context without a logger (fail fast is
  the norm, production included); `make lint` (golangci-lint v2.13.1,
  pinned and installed by the target, `.golangci.yml`) rejects the
  `slog`/`log` globals via forbidigo and non-`*Context` calls via
  sloglint, with gofumpt + goimports as formatters.
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
