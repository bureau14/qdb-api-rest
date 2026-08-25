# Assessment Benchmark -- Plan

Status: approved. This document specifies the temporary local benchmark
described in the brief's Testing doctrine (item 4). It is a working
document: dates and verified facts are recorded here, not in the brief;
progress is recorded in `docs/log.md`, not here. On retirement, any
mechanics still worth keeping move to `docs/e2e-plan.md` or the relevant
`AGENTS.md`, this status becomes `retired`, and `docs/log.md` gets a
one-line entry.

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

| protocol    | client                                                                                                                         |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------ |
| `native`    | `quasardb` Python package over `qdb://`, streaming via `stream_query` (sc-19522); one-shot `qdb_query` mode dropped 2026-08-24 |
| `legacy`    | `POST /api/login` + `POST /api/query`, JSON, client-side parse and wart normalization                                          |
| `flightsql` | `pyarrow.flight` / `adbc_driver_flightsql`, Arrow record batches                                                               |

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
  - `native`: return of the first batch from `stream_query`.
- `client_peak_rss_bytes`: sampled from outside the measurement child.
- `server_peak_rss_bytes`: REST-server process (absent for `native@qdbd`).
- `server_cpu_seconds`: REST-server CPU time delta (informational only).
- `response_bytes` where the protocol exposes it.
- **Data-volume metrics** (the gateway-thesis evidence; see "Two volumes"
  below):
  - `qdbd_out_bytes`, `qdbd_request_count`: bytes and requests qdbd sent
    to whichever process held the C API handle for this run (the Python
    client for `native`, the REST server otherwise). Delta of the
    node-wide cumulative counters `$qdb.statistics.requests.out_bytes` /
    `.total_count`, read by the parent before the child starts and after
    it exits.
  - `client_bytes`: bytes the measured client process received, per
    protocol: `native` = `qdbd_out_bytes` by definition (the client is
    the reducer); `legacy` = HTTP body bytes read (gzip controlled
    explicitly, recorded); `flightsql` = sum of Arrow IPC record-batch
    sizes (approximate; gRPC may compress on the wire).
  - `client_cpu_seconds`: user+sys CPU of the measurement child
    (`resource.getrusage`), the "low-CPU client machine" proxy.
  - `report` derives `reduction = qdbd_out_bytes / client_bytes`.
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
baselines reset. Per query: 3 discarded warmup repetitions, then 5
measured repetitions, summarized by the **median** (robust to a single
straggler on a developer machine). Warmups run through the identical
measurement path -- fresh child, fresh REST server, counters -- so only
one code path exists; what they warm is qdbd, which needs 2-3 executions
of a query to reach steady state (verified 2026-08-24, cause of the
warmth bias in the first cross-run comparison). Warmup reps are persisted
in the result file flagged `warmup: true` and count for the fingerprint
check, but never for the medians; cold-start walls therefore stay
inspectable. Counts are the `WARMUP` / `REPS` Makefile variables.

## Two volumes: qdbd -> reducer, reducer -> client

QuasarDB is map/reduce-shaped: every shard touched is a mapped entry and
the reduce phase runs in the process that holds the C API handle. For
`native@qdbd` that process is the customer's client; for every REST run
it is the REST server, co-located with qdbd. The benchmark therefore
measures two volumes per query and keeps them apart:

1. **qdbd -> reducer**: what qdbd pushes to the C client. Identical for
   every run of the same query (same query, same qdbd, same C API); the
   difference between runs is _where_ those bytes land -- on the customer
   WAN link (`native`) or on the datacenter loopback (any REST run).
2. **reducer -> client**: what the final consumer actually receives. For
   `native` it equals volume 1; for REST runs it is the encoded response.

Query shapes behave very differently on these two axes; the reduce-shape
query family below exists to cover each class:

| class                                            | qdbd -> reducer    | reducer -> client | what the comparison shows                                          |
| ------------------------------------------------ | ------------------ | ----------------- | ------------------------------------------------------------------ |
| `LIMIT n` on a raw select, `COUNT(*)`            | tiny (pushed down) | tiny              | the gateway's extra-hop cost (brief, Open risk 3); no win expected |
| coarse `GROUP BY` (bucket >= shard, low-card)    | small              | small             | already reduced inside qdbd; control case                          |
| fine `GROUP BY` / high-cardinality key, no LIMIT | large              | large             | reduce cost moves to the gateway, encoding + streaming decide      |
| same + `ORDER BY agg DESC LIMIT k`               | **large**          | **tiny**          | the gateway thesis: WAN bytes and client CPU collapse to ~k rows   |
| full raw select                                  | large              | large             | the existing headline materialization KPI                          |

Patterns learned while probing the dataset (mechanics, not numbers; the
numbers are measured by the bench and debated there):

- `LIMIT` without `ORDER BY` is pushed down: qdbd ships roughly the
  limited rows. `ORDER BY <aggregate> ... LIMIT k` is **not**: qdbd ships
  the complete aggregate to the reducer, which sorts and discards. So the
  top-k variant of an aggregate costs exactly as much on volume 1 as the
  unlimited variant and almost nothing on volume 2. This gap is the
  property being measured.
- Volume 1 scales with _groups x shards touched_, not rows scanned. On
  this dataset (96 shards of 15 min, one day) low-cardinality keys such
  as `accountId` / `orderStreamId` with hourly buckets are already
  reduced almost entirely server-side; `GROUP BY id` (~1.5M distinct)
  or second-granularity buckets are needed to make the reducer work.
- Where qdbd itself is the bottleneck (very fine time buckets), wall
  clock barely moves between protocols on localhost; choose at least one
  reduce query whose qdbd time is short relative to its transfer + reduce
  so differences between runs are visible locally. Bytes and client CPU
  remain meaningful even when seconds do not.
- Note for `report`: the reduce-next-to-the-data advantage is a property
  of the architecture, and the **old** server has it too (it runs
  `qdb_query` server-side). The bench presents `native@qdbd` vs any REST
  run as the architectural delta, and old vs new REST as the
  implementation delta (encoding, streaming, gateway overhead).

Measurement mechanics, verified 2026-08-19:

- qdbd exposes node-wide cumulative counters
  `$qdb.statistics.requests.{in_bytes,out_bytes,total_count,...}` as
  integer entries readable with `direct_int_get` (qdbsh, or
  `quasardb.stats` in Python). They refresh periodically
  (`statistics_refresh_interval`, 500 ms in the shared test-setup config,
  5 s qdbd default): read them after a settle of at least 2x the interval
  or the delta is zero. They are node-global with no per-connection
  attribution in insecure mode -- acceptable because the bench runs one
  (protocol, server) pair at a time; the read itself costs a few
  requests, measured once per run as a no-op baseline and subtracted.
- OS-level socket accounting (`nettop`) reports zero for loopback traffic
  on macOS; the qdbd counters are the only portable source for volume 1.
- The counter is pre-compression (verified 2026-08-24: byte-identical
  `out_bytes` across balanced and uncompressed runs of the same query), so
  volume-1 numbers are comparable regardless of `qdb_compression_t`. The
  binding defaults are NOT the same (also verified 2026-08-24, contrary to
  what this bullet previously claimed): qdb-api-python's `Cluster` sets
  `qdb_comp_balanced` explicitly, while the old server's bare
  `qdb.NewHandle()` leaves the C API default, which is `qdb_comp_none`
  ("balanced ... not enabled by default", `qdb/option.h`). Over loopback,
  balanced is a pure CPU tax: ~13% wall on `agg_topk` (~4.3 s vs ~3.8 s).
  The bench therefore pins the mode on every run via the
  `CAPI_COMPRESSION` Makefile variable (default `none`, the only value
  `old-rest` can honor -- a `balanced` run against it fails fast) and
  records the effective per-run value in the result file's environment
  block. The new server must expose an explicit client-compression knob at
  M1 so `legacy@new-rest` runs under the same pinned mode.
- No WAN emulation (dummynet/netem) in the first version: bytes stand in
  for bandwidth, client CPU seconds for client compute. A throttled-link
  mode converting bytes into seconds is an opt-in later addition if the
  byte numbers alone do not settle the debate.

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

Materialization family (the original KPI):

| id      | query                                   | purpose                       |
| ------- | --------------------------------------- | ----------------------------- |
| `count` | `SELECT COUNT(*) FROM "reproduce"`      | sanity + tiny-result latency  |
| `head`  | `SELECT * FROM "reproduce" LIMIT 65536` | mid-size, TTFB shape          |
| `full`  | `SELECT * FROM "reproduce"`             | the headline 5.6M-row KPI run |

Reduce-shape family (the gateway thesis; see "Two volumes"), one query
per class (cardinalities verified 2026-08-20: `accountId` 85 groups,
`orderStreamId` 105, `id` ~1.5M):

| id           | SQL                                                                                            | class                    |
| ------------ | ---------------------------------------------------------------------------------------------- | ------------------------ |
| `limit10`    | `SELECT * FROM "reproduce" LIMIT 10`                                                           | pushed-down, extra hop   |
| `agg_coarse` | `SELECT $timestamp, accountId, COUNT(id), SUM(amount) FROM "reproduce" GROUP BY 1h, accountId` | reduced in qdbd, control |
| `agg_wide`   | `SELECT id, COUNT(id), SUM(amount), MIN(rate), MAX(rate) FROM "reproduce" GROUP BY id`         | heavy both ways          |
| `agg_topk`   | `agg_wide` text + `ORDER BY SUM(amount) DESC, id ASC LIMIT 10`                                 | heavy in, tiny out       |

`COUNT(id)`, never `COUNT(*)`: the wire expands `COUNT(*)` into one count
per column. Row order of `GROUP BY` results is deterministic across
protocols (verified by the cross-protocol fingerprints, edge rows
included), so `agg_wide` needs no ORDER BY of its own.

Rules for the reduce family: every `ORDER BY` carries a full tiebreaker
(ties at the top-k boundary are real on this dataset and would make the
fingerprint nondeterministic); `agg_topk` is the `agg_wide` text plus the
`ORDER BY ... LIMIT` clause, nothing else, so their volume-1 numbers are
directly comparable; double aggregates (`SUM`, `AVG`) go through the
existing float tolerance.

The set is data, not code (a table in `bench.py`); adding a query is a
one-line change.

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
3. **Gateway leverage**: per query, `qdbd_out_bytes` vs `client_bytes`
   vs `client_cpu_seconds` across all runs, with `reduction` derived. The
   sentence that matters is per reduce-family query: what `native@qdbd`
   must download and compute to end up with k rows versus what
   `flightsql@new-rest` delivers for the same k rows.

## Module contract

Protocol modules (`protocols/<name>.py`) expose:

```python
def fetch(cfg, query, record_ttfb, telemetry: dict) -> Iterator[pandas.DataFrame]
```

An iterator, so streaming protocols yield batches without a forced concat
(which would destroy their RSS story); one-shot protocols yield once. The
harness fingerprints batches through a streaming accumulator whose result
is invariant to batch boundaries (`selftest` pins that). `telemetry` is
the protocol's channel for wire-level metrics the harness cannot see:
`response_bytes`, `body_bytes_decoded`, `gzip`, `wart_count`.

Server modules (`servers/<name>.py`) expose:

```python
def server_cmd(cfg) -> list[str]
```

- `record_ttfb` is called once by the protocol at its first-data moment
  (definitions above). The clock and the client CPU meter cover only the
  fetch-iterator pulls; fingerprint accumulation between pulls is bench
  overhead and stays outside both.
- A protocol module knows nothing about which server answers; it gets a
  base URL / URI from the harness. That is what makes `legacy@old-rest`
  and `legacy@new-rest` run byte-for-byte the same client code.
- The harness owns everything else: child forking, timing, RSS sampling,
  server lifecycle, fingerprinting, persistence. Adding `flightsql` later
  means writing `protocols/flightsql.py` (~30 lines) and enabling the
  registry row -- no harness changes.
- `legacy.py` internals: anonymous login (`{"username":"","secret_key":""}`
  -> Bearer token), `POST /api/query` over stdlib `http.client` (exact
  first-body-byte TTFB, explicit `Accept-Encoding`), then columnar JSON to
  DataFrame:
  `pd.DataFrame({col.name: col.data})` plus legacy-wart normalization
  (`"(void)"` -> NaT, `"(undefined)"` -> NA, ISO timestamps parsed),
  counting warts as it goes. Plain `json.loads` on purpose: that parse
  cost is the honest price a real customer pays on this path and belongs
  in the measurement. HTTP gzip is on by default and the only mode in the
  standard runs (real clients send `Accept-Encoding: gzip`; the server
  compresses on any such header); `--no-gzip` exists for one-off probes,
  and every result records the setting plus both byte counts.

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
4. `protocols/native.py` (`stream_query`),
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

## Decision log (2026-08-19)

| Decision                                                      | Why                                                                                                         | Rejected                                                             |
| ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Measure two volumes (qdbd -> reducer, reducer -> client)      | the gateway thesis is about where the reduce runs; one byte count cannot show it                            | response size only                                                   |
| Volume 1 from qdbd `$qdb.statistics.requests.*` counters      | only portable source; macOS loopback is invisible to OS accounting; one run at a time makes it attributable | `nettop`/`/proc/net`, per-connection instrumentation in the bindings |
| Reduce-shape query family, one query per class                | classes behave differently on the two axes; a single aggregate would show only one of them                  | one "aggregation query"                                              |
| High-cardinality / fine-bucket keys for `agg_wide`/`agg_topk` | coarse buckets over low-cardinality keys are already reduced in qdbd and prove nothing                      | hourly x `accountId`-style queries as the headline                   |
| Bytes + client CPU, no WAN emulation in v1                    | honest, portable, enough to settle the architectural question; seconds can be derived later                 | dummynet/netem throttling as a prerequisite                          |
| Hard measured numbers stay out of this plan                   | they are subject to debate; the bench produces them, results files hold them                                | recording probe numbers as verified facts here                       |

## Decision log (2026-08-20)

| Decision                                                         | Why                                                                                                  | Rejected                                                           |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| Reduce-family SQL as pinned in "Queries"                         | `GROUP BY id` (~1.5M groups) is the only key that loads the reducer; 1h x `accountId` is the control | 1s buckets (qdbd-bound, hides protocol deltas on localhost)        |
| `fetch` returns an iterator of DataFrames                        | stream mode must not concat; one fingerprint accumulator serves one-shot and streaming alike         | `-> DataFrame` with a stream special case in the harness           |
| `telemetry` dict parameter on `fetch`                            | wire-level metrics (`response_bytes`, `wart_count`, `gzip`) have no other home                       | parsing them out of protocol return values                         |
| Legacy HTTP gzip on by default, only mode in standard runs       | customer-realistic (`requests` sends `Accept-Encoding: gzip`); recorded per result, `--no-gzip` flag | gzip off (raw-wire baseline), measuring both (doubles legacy reps) |
| Native client input buffer = old server's `--max-in-buffer-size` | every run must accept the same result sizes; the binding default (256 MiB) fails `agg_wide`/`full`   | binding defaults per client                                        |
| Counters read key-by-key on a direct node connection             | `stats.by_node` scans every stat key (~3k requests/read) and drowns small queries                    | `quasardb.stats.by_node` full scan                                 |
| All-null columns fingerprint type-free                           | no observable wire type: legacy JSON types them `none`, the native client picks a dtype              | per-protocol dtype exceptions in the comparison                    |

## Decision log (2026-08-24)

| Decision                                                                                       | Why                                                                                                                                                                                     | Rejected                                                                                                                     |
| ---------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `native@qdbd` measures `stream_query()` only                                                   | owner decision (Leon); the streaming path is the native reference the gateway is chasing, and one mode halves every native run                                                          | keeping the one-shot `qdb_query` sub-mode                                                                                    |
| C API compression pinned per run via `CAPI_COMPRESSION`, default `none` (owner decision, Leon) | binding defaults diverge (python balanced, old server none) and polluted the aggregation comparison; the old server is not configurable, so `none` is the only mode every run can share | per-binding defaults (proven inconsistent); balanced everywhere (old-rest cannot honor it)                                   |
| 3 warmups + 5 measured reps per query, median reported (owner decision, Leon)                  | qdbd needs 2-3 executions to reach steady state and run ordering leaked into the old means; the median shrugs off one straggler                                                         | mean of 3 with no warmup (the polluted status quo); cheap warmup outside the measurement path (second code path to mistrust) |
