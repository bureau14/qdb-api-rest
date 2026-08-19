# Assessment Benchmark -- Plan

Status: approved. This document specifies the temporary local benchmark
described in the brief's Testing doctrine (item 4). It is a working
document: dates and verified facts are recorded here, not in the brief;
progress is recorded in `docs/log.md`, not here. On retirement, lessons
move to `docs/log.md` and this status becomes `retired`.

**This tool is a one-time thing.** It exists to prove that the rewrite
beats the old REST API on client wall clock, and is retired once that is
demonstrated. No abstractions are built for it beyond what the measurement
needs; it is deletable with one `rm -rf tests/e2e/bench`. Everything with
a longer lifetime -- qdbd as a service, the dataset, budgets, stress --
lives in the permanent e2e harness (`docs/e2e-plan.md`), which this tool
consumes and never owns.

## Purpose

Measure, on a developer machine, the one KPI that matters for the gateway
thesis: **wall-clock time until a Python client holds a fully materialized
pandas DataFrame** for a large query result. The harness serves two
concerns with one piece of code:

1. **Drop-in compatibility**: the _same_ legacy-protocol client code
   (login, `POST /api/query`, JSON parse, wart normalization) runs
   unchanged against the old server and the new server and must produce
   the same data. This is the "a customer's Python script keeps working"
   claim, checked semantically (normalized DataFrame fingerprints).
   Byte-shape compatibility of the legacy endpoints is the permanent e2e
   harness's job (`docs/e2e-plan.md`), not this tool's.
2. **Performance**: the new REST API beats the old REST API on client wall
   clock -- both for the unchanged legacy protocol (what a customer gets by
   swapping the binary) and for Arrow Flight SQL (what they get by moving
   to the gateway protocol). Increased server-side compute is explicitly
   acceptable -- the gateway trades co-located CPU for client latency.
   Server CPU time is reported as an informational column, never a
   pass/fail criterion.

Both concerns need identical mechanics -- run one client/server pair in
isolation, persist normalized fingerprints and timings, compare
afterwards -- so they share the harness; only `report` reads the result
files differently for each.

This is a local developer tool, deliberately **not** wired into Buildkite.
That buys freedom: we may build `qdb-api-python` from a local checkout,
build the old server from a `master` worktree, and skip cross-platform
ceremony.

## Dependencies on the e2e harness

- **qdbd** is up on `127.0.0.1:2836`, started by
  `scripts/tests/setup/start-services.sh`. The bench never starts or stops
  qdbd.
- **The dataset** (table `reproduce`, 5,613,032 rows) is loaded by
  `tests/e2e`'s `make load` (CSV + `qdb_import`, S3-hosted, sha256-pinned;
  see `docs/e2e-plan.md`). The bench never fetches or loads data.
- `bench.py run` asserts both (port answers, `COUNT(*)` matches) and fails
  fast with the make target to run when they do not.

## Protocols, servers, runs

A run is a **(protocol, server) pair**. The two axes are orthogonal:

- **protocol** = the client-side code; owns
  `fetch(query, record_ttfb) -> DataFrame`.
- **server** = the process that answers; owns `server_cmd()`, port,
  pidfile (qdbd is the shared service and has none).

| protocol    | client                                                                                                        |
| ----------- | ------------------------------------------------------------------------------------------------------------- |
| `native`    | `quasardb` Python package over `qdb://`; sub-modes `query` (one-shot) and `stream` (`stream_query`, sc-19522) |
| `legacy`    | `POST /api/login` + `POST /api/query`, JSON, client-side parse and wart normalization                         |
| `flightsql` | `pyarrow.flight` / `adbc_driver_flightsql`, Arrow record batches                                              |

| server     | what                                          | ports         |
| ---------- | --------------------------------------------- | ------------- |
| `qdbd`     | the shared service (not managed by the bench) | 2836          |
| `old-rest` | `master` worktree build                       | 40080         |
| `new-rest` | this branch                                   | 40090 / 40493 |

Valid runs (the registry is this table, nothing else):

| run                  | answers                                                                         | available |
| -------------------- | ------------------------------------------------------------------------------- | --------- |
| `native@qdbd`        | the reference the gateway is chasing; validates dataset and qdbd health         | Phase 1   |
| `legacy@old-rest`    | today's baseline                                                                | Phase 1   |
| `legacy@new-rest`    | drop-in compatibility (same client code, same fingerprint?) and drop-in speedup | with M1   |
| `flightsql@new-rest` | the gateway thesis                                                              | with M3   |

Exactly **one run per invocation**. No simultaneous runs: this keeps the
code focused and makes RSS attribution unambiguous (only one REST server
process exists during a run). Cross-run comparison happens afterwards,
over persisted result files. Runs not yet available raise "not
implemented". A future `qdb-api-python` Flight SQL transport is a new
protocol (`flightsql-qdbpy@new-rest`) to measure integration overhead
separately -- one module, no harness change.

## Metrics

Headline, per (run, query, repetition):

- `wall_to_dataframe_seconds`: from just before issuing the query to the
  moment the DataFrame is fully constructed in the client process.

Supporting:

- `ttfb_seconds` -- per-protocol definition, printed with the number:
  - `legacy`: first response body byte.
  - `flightsql`: arrival of the first Arrow record batch.
  - `native` stream mode: return of the first batch from `stream_query`.
  - `native` one-shot mode: equals wall time by construction (the C API
    delivers nothing incrementally); reported as such, not hidden.
- `client_peak_rss_bytes`: sampled from outside the measurement child.
- `server_peak_rss_bytes`: REST-server process (absent for `native@qdbd`).
- `server_cpu_seconds`: REST-server CPU time delta (informational only).
- `response_bytes` where the protocol exposes it.
- `legacy_wart_count`: occurrences of `"(void)"` / `"(undefined)"` seen by
  the legacy parser before normalization (informational; makes a silent
  wart drop visible in `report` even though fingerprints are compared
  post-normalization).
- qdbd peak RSS is NOT a metric (identical qdbd under every run; its
  memory behavior is not what this harness assesses).

Methodology (proven by `reproduce.py` from the sc-19522 work): every
measurement runs in a **fresh child process**; the parent samples RSS of
child and server at a fixed interval (`ps -o rss=` -- portable to macOS,
where `/proc` does not exist) and collects a JSON result line from the
child's stdout. The REST server is **restarted between runs** so RSS
baselines reset. Default 3 runs per query; mean and per-run values are
both persisted.

## Version purity rules

The point of the exercise is old-REST vs new-REST with everything
underneath held identical:

- Artifact distribution is `install-qdb`
  (`~/playground/install-qdb`, `uv tool install`-able): one invocation
  (`install-qdb --source buildkite --branch master --build release
--yes`) downloads the quasardb-build artifacts and fans the **same**
  extracted tree into every `~/git/qdb-*/qdb` checkout -- including this
  repo's `qdb/` (old and new server link it, qdbd runs from it) and
  `qdb-api-python/qdb` (the Python extension builds against it). The
  harness does not manage artifacts; it **verifies** parity: `make check`
  hashes `libqdb_api` in both trees, fails fast on mismatch, and the hash
  plus `qdbd --version` are recorded in every result file.
- The old server is built from `master` **from source** (the checked-in
  binary is stale and does not link current `libqdb_api`).
- Old REST API = latest `master` of this repo; new REST API = this
  branch. Never the released 3.14.2-based binary -- that would smuggle a
  second qdb version into the comparison.
- The `quasardb` Python package is built from the local
  `~/git/qdb-api-python` checkout via that repo's canonical build,
  `scripts/cicd/10.build.sh` (clean `.env/` venv, `python -m build -w`,
  `QDB_TESTS_ENABLED=OFF`, wheel in `dist/`) -- a pip-installed wheel
  would pin 3.14.2 and contaminate the comparison. The C++ build is
  slow, so `make venv` re-runs it only when the cache key (qdb-api-python
  git sha, C API hash) changes, then installs the wheel into the bench
  venv. The bench venv must use the same Python the wheel was built with
  (`PYTHON_CMD`, one Makefile variable); `CMAKE_GENERATOR=Ninja` is
  honored for faster rebuilds.
- The Go binding version is part of each server's implementation and is
  allowed to differ; the C API and qdbd underneath are byte-identical,
  which is the layer the purity requirement is about.

## Queries

A small fixed set, identical across runs:

| id      | query                                   | purpose                       |
| ------- | --------------------------------------- | ----------------------------- |
| `count` | `SELECT COUNT(*) FROM "reproduce"`      | sanity + tiny-result latency  |
| `head`  | `SELECT * FROM "reproduce" LIMIT 65536` | mid-size, TTFB shape          |
| `full`  | `SELECT * FROM "reproduce"`             | the headline 5.6M-row KPI run |

The set is data, not code (a table in `bench.py`); adding an aggregation
query later is a one-line change.

## Layout

```
tests/e2e/bench/
  Makefile               check | venv | old-server | new-server | bench-<protocol>@<server> | report | clean
  bench.py               run + report subcommands (see CLI)
  protocols/             native.py, legacy.py, flightsql.py   (fetch)
  servers/               old_rest.py, new_rest.py             (server_cmd)
  results/               <protocol>@<server>.json (gitignored), consumed by report
  README.md              usage; links back to this plan
```

There are no start/stop scripts and no `env.sh`. The Makefile is the
single source of truth for paths and ports and passes them to `bench.py`
as explicit flags; qdbd and the dataset are the e2e harness's business.

Ports: qdbd `2836` (shared setup); old REST `40080`; new REST `40090`
(HTTP) and `40493` (Flight SQL gRPC). All distinct so a stray server
never collides, even though only one run's server exists during a
measurement.

## Server lifecycle: bench.py owns it

`bench.py run` starts, samples, and stops the REST server of its run
-- and restarts it between runs. It must own the process anyway (it needs
the pid for RSS sampling and the restart-per-run rule is a measurement
rule), so no separate start script exists. Each server module exposes
`server_cmd(cfg) -> list[str]`; the harness runs it as a child, polls the
port, measures, terminates. `qdbd` has no server module.

Server binaries are built by `make old-server` (delegates to
`tests/e2e`'s target: git worktree of `master` in `tests/e2e/.old-master`,
`go build` against the repo's `qdb/`) and `make new-server` (this branch);
`bench.py` receives the binary paths as flags. Old-server launch flags that matter (verified):
`--local -c qdb://127.0.0.1:2836 --pool-size 4 --parallelism-count 4
--max-in-buffer-size 8589934592 --log-file <path>`. The deployed binary
has no HTTP timeouts (server flag group never parsed), so no timeout
equalization is needed.

## CLI and flow

```
scripts/tests/setup/start-services.sh     # once: qdbd
make -C tests/e2e load                    # once: dataset into qdbd (idempotent)

cd tests/e2e/bench
make check venv old-server                # parity check, bench venv, old binary
make bench-native@qdbd                    # -> results/native@qdbd.json
make bench-legacy@old-rest                # -> results/legacy@old-rest.json
make bench-legacy@new-rest                # once M1 lands
make bench-flightsql@new-rest             # once M3 lands
make report                               # merges results/*.json
```

`bench.py run --protocol legacy --server new-rest` writes
`results/legacy@new-rest.json`: per-repetition metrics, the mean,
environment (git shas of this repo/master/qdb-api-python, C API hash,
machine, timestamp), and the result fingerprint. `bench.py report` reads
whatever result files exist and prints two sections:

1. **Compatibility**: a fingerprint matrix per query across all runs,
   with the sentence that matters called out explicitly --
   `legacy@old-rest == legacy@new-rest` -- plus the wart counts.
2. **Performance**: wall-clock / TTFB / RSS table, with the two headline
   deltas: `legacy@new-rest` vs `legacy@old-rest` (drop-in speedup) and
   `flightsql@new-rest` vs `legacy@old-rest` (gateway thesis), and
   `native@qdbd` as the floor.

## Module contract

Protocol modules (`protocols/<name>.py`) expose:

```python
def fetch(query: str, record_ttfb: Callable[[], None]) -> pandas.DataFrame
```

Server modules (`servers/<name>.py`) expose:

```python
def server_cmd(cfg) -> list[str]
```

- `record_ttfb` is called once by the protocol at its first-data moment
  (definitions above).
- A protocol module knows nothing about which server answers; it gets a
  base URL / URI from the harness. That is what makes `legacy@old-rest`
  and `legacy@new-rest` run byte-for-byte the same client code.
- The harness owns everything else: child forking, timing, RSS sampling,
  server lifecycle, fingerprinting, persistence. Adding `flightsql` later
  means writing `protocols/flightsql.py` (~30 lines) and enabling the
  registry row -- no harness changes.
- `legacy.py` internals: anonymous login (`{"username":"","secret_key":""}`
  -> Bearer token), `POST /api/query`, then columnar JSON to DataFrame:
  `pd.DataFrame({col.name: col.data})` plus legacy-wart normalization
  (`"(void)"` -> NaT, `"(undefined)"` -> NA, ISO timestamps parsed),
  counting warts as it goes. Plain `json.loads` on purpose: that parse
  cost is the honest price a real customer pays on this path and belongs
  in the measurement.

## Functional equivalence across runs

Because runs are separate invocations, equivalence is checked over
**persisted fingerprints**, not live DataFrames. The fingerprint of a
result, computed after per-protocol normalization (timestamps to UTC ns,
legacy sentinels to proper nulls, column order sorted):

- shape and column names;
- per-column null counts;
- numeric columns: sum/min/max, compared with relative tolerance 1e-9
  (awk-tolerance philosophy from qdb-nats-connector ADR-007);
- string/timestamp columns: a stable order-independent hash;
- first and last 5 rows, normalized, verbatim (human-debuggable diffs).

`bench.py report` compares fingerprints pairwise across runs for each
query id and reports pass/fail per column. "Somewhat the same" is thus
concrete: identical after documented normalization, floats within
tolerance.

Caveat, stated once: because normalization runs before fingerprinting,
`legacy@old-rest == legacy@new-rest` proves data equivalence through a
real client, not byte-shape identity of the legacy JSON (a dropped wart
would still pass). Byte-shape is the permanent e2e golden pairs' job;
the informational `legacy_wart_count` keeps a silent drop visible here.

The same fingerprint (via `native@qdbd`) is the one-time check that
the CSV export/import round trip of the dataset is faithful: run once
against a qdbd serving the original data directory, once against the
imported table, compare.

## Verified baseline (2026-08-14, Apple M-series, 48 GB, localhost)

Old REST API, built from `master` against qdbd 3.15.0.dev0, `full` query,
curl to /dev/null (pre-harness spike; harness numbers will include the
client-side DataFrame parse on top):

| metric             | value                               |
| ------------------ | ----------------------------------- |
| response size      | 833,774,502 bytes                   |
| TTFB               | 29.28 s                             |
| total wall         | 29.41 s (transmission only ~0.13 s) |
| server peak RSS    | ~8.4 GB                             |
| qdb-side QueryData | 20.3 s (from server log)            |

Reference for `native@qdbd` (sc-19522 measurements, same dataset):
`stream_query` holds client RSS ~2.2 GB flat vs ~7.7 GB peak inside
`libqdb_api` for one-shot `qdb_query`.

## Implementation order

1. e2e prerequisites (owned by `docs/e2e-plan.md`): shared
   `scripts/tests/setup/`, dataset conversion + upload, `make load`.
2. `Makefile`: `check`, `venv` (wheel build via qdb-api-python's
   `scripts/cicd/10.build.sh`), `old-server` (worktree build recipe,
   proven).
3. `bench.py` core: child runner, server lifecycle, RSS sampler,
   fingerprinting, `results/` persistence, `report`.
4. `protocols/native.py` (query + stream sub-modes),
   `protocols/legacy.py`, `servers/old_rest.py`; run `native@qdbd` and
   `legacy@old-rest` and validate equivalence between them -- this
   cross-checks the legacy parser against the native client before the
   rewrite ever enters the picture.
5. `servers/new_rest.py` + `protocols/flightsql.py` stubs; registry rows
   `legacy@new-rest` (enabled with M1) and `flightsql@new-rest` (enabled
   with M3) raise "not implemented" until then.

Steps 1-4 are Phase 1 and make the tool immediately useful: native vs
legacy@old-rest numbers quantify today's REST tax, and the equivalence
check hardens the harness itself. The rewrite drops in at step 5's
seams: `legacy@new-rest` the moment M1 serves the legacy endpoints (the
first real drop-in compatibility signal), `flightsql@new-rest` with M3.
When new-rest wins on both, the tool has done its job and is removed.

## Decision log (2026-08-16)

| Decision                                 | Why                                                                                                    | Rejected                                                                     |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------- |
| One-time tool, no abstraction investment | comparison is done once new-rest beats old-rest                                                        | permanent-grade infra in the bench; shared abstractions up front             |
| qdbd + dataset come from the e2e harness | service model (ADR-007); loading is a test resource with a longer lifetime than the bench              | `start-qdbd.sh`, `stop-all.sh`, `fetch-dataset.sh`, per-run fresh extraction |
| `bench.py` owns REST-server lifecycle    | it needs the pid and restarts per run anyway; resolves the old plan's contradiction                    | `start-old-rest.sh` / `start-new-rest.sh` as operator steps                  |
| Run = (protocol, server) pair            | one legacy client module runs unchanged against old and new server: compat and perf from the same code | three opaque "targets" (conflates client code with the server it hits)       |
| Makefile as the only config source       | one place for paths/ports, passed as explicit flags                                                    | `env.sh` sourced by many scripts                                             |
| Python only here, never in CI            | qdb-api-python build + master worktree are heavyweight and temporary                                   | bench harness as the CI performance-budget mechanism                         |
