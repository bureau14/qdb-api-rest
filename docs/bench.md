# Assessment Benchmark -- Specification

Status: approved. This document specifies the temporary local benchmark
described in the brief's Testing doctrine (item 4). It is a permanent
specification for as long as the bench exists (`docs/AGENTS.md`,
Specifications). Dates and verified facts are recorded here and not in
the brief, and progress is recorded in `docs/log.md` and not here. When
the bench is removed, any mechanics still worth keeping move to
`docs/e2e.md` or the relevant `AGENTS.md`, this document is deleted with
it, and `docs/log.md` gets a one-line entry.

**This tool is a one-time thing.** It exists to prove that the rewrite
beats the old REST API on client wall clock, and is retired once that is
demonstrated. No abstractions are built for it beyond what the measurement
needs, and it is deletable with one `rm -rf tests/e2e/bench`. Everything
with a longer lifetime (qdbd as a service, the dataset, budgets, stress)
lives in the permanent e2e harness (`docs/e2e.md`), which this tool
consumes and never owns.

## Purpose

The bench measures, on a developer machine, the KPI that matters for the
gateway thesis: **wall-clock time until a Python client holds a fully
materialized pandas DataFrame** for a large query result. The harness
serves two concerns with one piece of code:

1. **Drop-in compatibility**: the _same_ v1-protocol client code
   (login, `POST /api/query`, JSON parse, wart normalization) runs
   unchanged against the old server and the new server and must produce
   the same data. This is the "a customer's Python script keeps working"
   claim, checked semantically (normalized DataFrame fingerprints).
   Byte-shape compatibility of the v1 endpoints is the permanent e2e
   harness's job (`docs/e2e.md`) rather than this tool's.
2. **Performance**: the new REST API beats the old REST API on client wall
   clock, both for the unchanged v1 protocol (what a customer gets by
   swapping the binary) and for Arrow Flight SQL (what they get by moving
   to the gateway protocol). Increased server-side compute is explicitly
   acceptable, because the gateway trades co-located CPU for client
   latency. Server CPU time is reported as an informational column and
   never as a pass/fail criterion.

Both concerns need identical mechanics (run one client/server pair in
isolation, persist normalized fingerprints and timings, compare
afterwards), so they share the harness. Only `report` reads the result
files differently for each.

The bench is the home of every measured number in the project: wall
clock, time to first byte, RSS, throughput and byte volumes. The e2e
harness and CI assert behaviour and measure nothing (ADR-0013), so no
milestone criterion or CI gate is a number from here, and a person reads
`report`. The semantic compatibility check costs nothing extra, because
it is a by-product of the `v1@new-rest` run that measures the same pair.

This is a local developer tool, deliberately **not** wired into Buildkite.
It builds `qdb-api-python` from a local checkout and the old server from
a `master` worktree, and skips cross-platform ceremony.

## Dependencies on the e2e harness

- **qdbd** is up on `127.0.0.1:2836`, started by
  `scripts/tests/setup/start-services.sh`. The bench never starts or stops
  qdbd.
- **The dataset** (table `reproduce`, 5,613,032 rows) is loaded by
  `tests/e2e`'s `make load` (CSV + `qdb_import`, S3-hosted and
  sha256-pinned, see `docs/e2e.md`). The bench never fetches or loads
  data.
- `bench.py run` asserts both (port answers, `COUNT(*)` matches) and fails
  fast with the make target to run when they do not.

## Protocols, servers, runs

A run is a **(protocol, server) pair**. The two axes are orthogonal:

- **protocol** is the client-side code, and it owns
  `fetch(query, record_ttfb) -> DataFrame`.
- **server** is the process that answers, and it owns `server_cmd()`,
  the port and the pidfile (qdbd is the shared service and has none).

| protocol     | client                                                                                                                     |
| ------------ | -------------------------------------------------------------------------------------------------------------------------- |
| `native`     | `quasardb` Python package over `qdb://`, streaming via `stream_query`, the native reference the gateway is chasing         |
| `v1`         | `POST /api/login` + `POST /api/query`, JSON, client-side parse and wart normalization                                      |
| `flightsql`  | `pyarrow.flight` / `adbc_driver_flightsql`, Arrow record batches                                                           |
| `http-arrow` | `POST /api/v2/auth/login` + `POST /api/v2/query` with `Accept: application/vnd.apache.arrow.stream`, read by `pyarrow.ipc` |

| server     | what                                          | ports         |
| ---------- | --------------------------------------------- | ------------- |
| `qdbd`     | the shared service (not managed by the bench) | 2836          |
| `old-rest` | `master` worktree build                       | 40080         |
| `new-rest` | this branch                                   | 40090 / 40493 |

The valid runs are the table below. The registry in `bench.py` is this
table and nothing more, and the `http-arrow@new-rest` row joins it with
its protocol module.

| run                   | answers                                                                         | needs                  |
| --------------------- | ------------------------------------------------------------------------------- | ---------------------- |
| `native@qdbd`         | the reference the gateway is chasing, and it validates dataset and qdbd health  | qdbd and the dataset   |
| `v1@old-rest`         | the production server's baseline                                                | the old server         |
| `v1@new-rest`         | drop-in compatibility (same client code, same fingerprint?) and drop-in speedup | the v1 wrappers        |
| `flightsql@new-rest`  | the gateway thesis                                                              | Flight SQL             |
| `http-arrow@new-rest` | the first number for the rewrite: the v2 query path, Arrow over plain HTTP      | the v2 login and query |

There is **one run per invocation** and never a simultaneous run. This
keeps the code focused and makes RSS attribution unambiguous, because
only one REST server process exists during a run. Cross-run comparison
happens afterwards, over persisted result files. A run whose server side
does not exist raises "not implemented". A future `qdb-api-python` Flight
SQL transport is a new protocol (`flightsql-qdbpy@new-rest`) that
measures integration overhead separately, as one module with no harness
change.

## Metrics

Headline, per (run, query, repetition):

- `wall_to_dataframe_seconds`: from just before issuing the query to the
  moment the DataFrame is fully constructed in the client process.

Supporting:

- `ttfb_seconds` has a per-protocol definition, printed with the number:
  - `v1`: first response body byte.
  - `flightsql`, `http-arrow`: arrival of the first Arrow record batch.
  - `native`: return of the first batch from `stream_query`.
- `client_peak_rss_bytes`: sampled from outside the measurement child.
- `server_peak_rss_bytes`: REST-server process (absent for `native@qdbd`).
- `server_cpu_seconds`: REST-server CPU time delta (informational only).
- `response_bytes` where the protocol exposes it.
- **Data-volume metrics** (the gateway-thesis evidence, see "Two volumes"
  below):
  - `qdbd_out_bytes`, `qdbd_request_count`: bytes and requests qdbd sent
    to whichever process held the C API handle for this run (the Python
    client for `native`, the REST server otherwise). Delta of the
    node-wide cumulative counters `$qdb.statistics.requests.out_bytes` /
    `.total_count`, read by the parent before the child starts and after
    it exits.
  - `client_bytes`: bytes the measured client process received, per
    protocol. For `native` it is `qdbd_out_bytes` by definition, because
    the client is the reducer. For `v1` it is the HTTP body bytes read,
    with gzip controlled explicitly and recorded. For `flightsql` it is
    the sum of Arrow IPC record-batch sizes, which is approximate because
    gRPC may compress on the wire.
  - `client_cpu_seconds`: user+sys CPU of the measurement child
    (`resource.getrusage`), the "low-CPU client machine" proxy.
  - `report` derives `reduction = qdbd_out_bytes / client_bytes`.
- `wart_count`: occurrences of `"(void)"` / `"(undefined)"` seen by
  the v1 parser before normalization. It is informational, and it makes
  a silent wart drop visible in `report` even though fingerprints are
  compared post-normalization.
- qdbd peak RSS is not a metric, because the same qdbd runs under every
  run and its memory behavior is not what this harness assesses.

The methodology is the one `reproduce.py` from the sc-19522 work proved.
Every measurement runs in a **fresh child process**. The parent samples
the RSS of child and server at a fixed interval (`ps -o rss=`, which is
portable to macOS, where `/proc` does not exist) and collects a JSON
result line from the child's stdout. The REST server is **restarted
between runs** so RSS baselines reset. Per query, the bench runs 3
discarded warmup repetitions, then 5 measured repetitions, summarized by
the **median**, which is robust to a single straggler on a developer
machine. Warmups run through the identical measurement path (fresh
child, fresh REST server, counters), so only one code path exists. What
they warm is qdbd, which needs 2-3 executions of a query to reach steady
state (verified 2026-08-24). Warmup reps are persisted in the result
file flagged `warmup: true` and count for the fingerprint check, but
never for the medians, so cold-start walls stay inspectable. The counts
are the `WARMUP` / `REPS` Makefile variables.

## Two volumes: qdbd -> reducer, reducer -> client

QuasarDB is map/reduce-shaped. Every shard touched is a mapped entry,
and the reduce phase runs in the process that holds the C API handle.
For `native@qdbd` that process is the customer's client, and for every
REST run it is the REST server, co-located with qdbd. The benchmark
therefore measures two volumes per query and keeps them apart:

1. **qdbd -> reducer**: what qdbd pushes to the C client. It is identical
   for every run of the same query (same query, same qdbd, same C API).
   The difference between runs is _where_ those bytes land, on the
   customer WAN link (`native`) or on the datacenter loopback (any REST
   run).
2. **reducer -> client**: what the final consumer actually receives. For
   `native` it equals volume 1, and for REST runs it is the encoded
   response.

Query shapes behave very differently on these two axes, and the
reduce-shape query family below exists to cover each class:

| class                                            | qdbd -> reducer    | reducer -> client | what the comparison shows                                               |
| ------------------------------------------------ | ------------------ | ----------------- | ----------------------------------------------------------------------- |
| `LIMIT n` on a raw select, `COUNT(*)`            | tiny (pushed down) | tiny              | the gateway's extra-hop cost (brief, Open risk 3), with no win expected |
| coarse `GROUP BY` (bucket >= shard, low-card)    | small              | small             | already reduced inside qdbd, the control case                           |
| fine `GROUP BY` / high-cardinality key, no LIMIT | large              | large             | reduce cost moves to the gateway, encoding + streaming decide           |
| same + `ORDER BY agg DESC LIMIT k`               | **large**          | **tiny**          | the gateway thesis: WAN bytes and client CPU collapse to ~k rows        |
| full raw select                                  | large              | large             | the existing headline materialization KPI                               |

This is how the dataset behaves on the two axes. These are mechanics,
and the numbers are the bench's to measure.

- `LIMIT` without `ORDER BY` is pushed down, so qdbd ships roughly the
  limited rows. `ORDER BY <aggregate> ... LIMIT k` is **not** pushed
  down, so qdbd ships the complete aggregate to the reducer, which sorts
  and discards. So the top-k variant of an aggregate costs as much on
  volume 1 as the unlimited variant and almost nothing on volume 2. This
  gap is the property being measured.
- Volume 1 scales with _groups x shards touched_ rather than rows
  scanned. On this dataset (96 shards of 15 min, one day) low-cardinality
  keys such as `accountId` / `orderStreamId` with hourly buckets are
  already reduced almost entirely server-side. `GROUP BY id` (~1.5M
  distinct) or second-granularity buckets are needed to make the reducer
  work.
- Where qdbd itself is the bottleneck (very fine time buckets), wall
  clock barely moves between protocols on localhost. Choose at least one
  reduce query whose qdbd time is short relative to its transfer + reduce
  so differences between runs are visible locally. Bytes and client CPU
  remain meaningful even when seconds do not.
- A note for `report`: the reduce-next-to-the-data advantage is a
  property of the architecture, and the **old** server has it too,
  because it runs `qdb_query` server-side. The bench presents
  `native@qdbd` vs any REST run as the architectural delta, and old vs
  new REST as the implementation delta (encoding, streaming, gateway
  overhead).

Measurement mechanics, verified 2026-08-19:

- qdbd exposes node-wide cumulative counters
  `$qdb.statistics.requests.{in_bytes,out_bytes,total_count,...}` as
  integer entries readable with `direct_int_get` (qdbsh, or
  `quasardb.stats` in Python). They refresh periodically
  (`statistics_refresh_interval`, 500 ms in the shared test-setup config,
  5 s qdbd default), so read them after a settle of at least 2x the
  interval, or the delta is zero. They are node-global with no
  per-connection attribution in insecure mode, which is acceptable
  because the bench runs one (protocol, server) pair at a time. The read
  itself costs a few requests, measured once per run as a no-op baseline
  and subtracted.
- OS-level socket accounting (`nettop`) reports zero for loopback traffic
  on macOS, so the qdbd counters are the only portable source for volume 1.
- The counter is pre-compression (verified 2026-08-24), so volume-1
  numbers are comparable regardless of `qdb_compression_t`. Wall clock
  is not comparable, because over loopback `balanced` is a pure CPU tax,
  and the C API holders default differently (qdb-api-python's `Cluster`
  sets `qdb_comp_balanced`, and the old server's bare `qdb.NewHandle()`
  leaves the C API default, `qdb_comp_none`). The bench therefore pins
  the mode on every run via the `CAPI_COMPRESSION` Makefile variable and
  records the effective per-run value in the result file's environment
  block. The default is `none`, the only value `old-rest` can honor, so
  a `balanced` run against it fails fast, and `new-rest` takes it
  through `cluster.compression`.
- There is no WAN emulation (dummynet/netem) in the first version. Bytes
  stand in for bandwidth, and client CPU seconds stand in for client
  compute. A throttled-link mode converting bytes into seconds is an
  opt-in later addition if the byte numbers alone do not settle the
  debate.

## Version purity rules

The point of the exercise is old-REST vs new-REST with everything
underneath held identical:

- Artifact distribution is `install-qdb`
  (`~/playground/install-qdb`, `uv tool install`-able). One invocation
  (`install-qdb --source buildkite --branch master --build release
--yes`) downloads the quasardb-build artifacts and fans the **same**
  extracted tree into every `~/git/qdb-*/qdb` checkout, including this
  repo's `qdb/` (old and new server link it, and qdbd runs from it) and
  `qdb-api-python/qdb` (the Python extension builds against it). The
  harness does not manage artifacts. It **verifies** parity: `make check`
  hashes `libqdb_api` in both trees, fails fast on mismatch, and the hash
  plus `qdbd --version` are recorded in every result file.
- The old server is built from `master` **from source**, because the
  checked-in binary is stale and does not link the current `libqdb_api`.
- The old REST API is the latest `master` of this repo, and the new REST
  API is this branch. Neither is the released 3.14.2-based binary, which
  would smuggle a second qdb version into the comparison.
- The `quasardb` Python package is built from the local
  `~/git/qdb-api-python` checkout via that repo's canonical build,
  `scripts/cicd/10.build.sh` (clean `.env/` venv, `python -m build -w`,
  `QDB_TESTS_ENABLED=OFF`, wheel in `dist/`), because a pip-installed
  wheel would pin 3.14.2 and contaminate the comparison. The C++ build
  is slow, so `make venv` re-runs it only when the cache key
  (qdb-api-python git sha, C API hash) changes, then installs the wheel
  into the bench venv. The bench venv must use the same Python the wheel
  was built with (`PYTHON_CMD`, one Makefile variable), and
  `CMAKE_GENERATOR=Ninja` is honored for faster rebuilds.
- The Go binding version is part of each server's implementation and is
  allowed to differ. The C API and qdbd underneath are byte-identical,
  which is the layer the purity requirement is about.

## Queries

A small fixed set, identical across runs:

Materialization family (the original KPI):

| id      | query                                   | purpose                       |
| ------- | --------------------------------------- | ----------------------------- |
| `count` | `SELECT COUNT(id) FROM "reproduce"`     | sanity + tiny-result latency  |
| `head`  | `SELECT * FROM "reproduce" LIMIT 65536` | mid-size, TTFB shape          |
| `full`  | `SELECT * FROM "reproduce"`             | the headline 5.6M-row KPI run |

Reduce-shape family (the gateway thesis, see "Two volumes"), one query
per class (cardinalities verified 2026-08-20: `accountId` 85 groups,
`orderStreamId` 105, `id` ~1.5M):

| id           | SQL                                                                                            | class                    |
| ------------ | ---------------------------------------------------------------------------------------------- | ------------------------ |
| `limit10`    | `SELECT * FROM "reproduce" LIMIT 10`                                                           | pushed-down, extra hop   |
| `agg_coarse` | `SELECT $timestamp, accountId, COUNT(id), SUM(amount) FROM "reproduce" GROUP BY 1h, accountId` | reduced in qdbd, control |
| `agg_wide`   | `SELECT id, COUNT(id), SUM(amount), MIN(rate), MAX(rate) FROM "reproduce" GROUP BY id`         | heavy both ways          |
| `agg_topk`   | `agg_wide` text + `ORDER BY SUM(amount) DESC, id ASC LIMIT 10`                                 | heavy in, tiny out       |

The queries say `COUNT(id)` and never `COUNT(*)`, because the wire
expands `COUNT(*)` into one count per column. Row order of `GROUP BY`
results is deterministic across protocols (verified by the
cross-protocol fingerprints, edge rows included), so `agg_wide` needs no
ORDER BY of its own.

The reduce family follows three rules. Every `ORDER BY` carries a full
tiebreaker, because ties at the top-k boundary are real on this dataset
and would make the fingerprint nondeterministic. `agg_topk` is the
`agg_wide` text plus the `ORDER BY ... LIMIT` clause and nothing more,
so their volume-1 numbers are directly comparable. Double aggregates
(`SUM`, `AVG`) go through the existing float tolerance.

The set is data rather than code (a table in `bench.py`), so adding a
query is a one-line change.

## Layout

```
tests/e2e/bench/
  Makefile               help | check | venv | old-server | bench-<protocol>@<server> | report |
                         selftest | clean | distclean
  bench.py               run + report subcommands (see CLI)
  protocols/             native.py, v1.py, flightsql.py   (fetch); http_arrow.py arrives with its run
  servers/               old_rest.py, new_rest.py             (server_cmd)
  results/               <protocol>@<server>.json (gitignored), consumed by report
  README.md              usage; links back to this plan
```

There are no start/stop scripts and no `env.sh`. The Makefile is the
single source of truth for paths and ports and passes them to `bench.py`
as explicit flags, and qdbd and the dataset are the e2e harness's
business.

The ports are qdbd `2836` (shared setup), old REST `40080`, and new REST
`40090` (HTTP) and `40493` (Flight SQL gRPC). All are distinct so a
stray server never collides, even though only one run's server exists
during a measurement.

## Server lifecycle: bench.py owns it

`bench.py run` starts, samples, and stops the REST server of its run,
and restarts it between runs. It must own the process anyway, because
it needs the pid for RSS sampling and the restart-per-run rule is a
measurement rule, so no separate start script exists. Each server module
exposes `server_cmd(cfg) -> list[str]`, and the harness runs it as a
child, polls the port, measures, and terminates it. `qdbd` has no server
module.

`make old-server` builds the old server. It delegates to `tests/e2e`'s
target, which makes a git worktree of `master` in `tests/e2e/.old-master`
and runs `go build` against the repo's `qdb/`. The new server is
whatever binary `NEW_REST_BIN` names, built by the root Makefile.
`bench.py` receives the binary paths as flags. The old-server launch
flags that matter (verified) are
`--local -c qdb://127.0.0.1:2836 --pool-size 4 --parallelism-count 4
--max-in-buffer-size 8589934592 --log-file <path>`. `--local` overrides
`--port`, so the old server always answers on 40080. The deployed binary
has no HTTP timeouts, because the server flag group is never parsed, so
no timeout equalization is needed. The new-server launch flags that
matter are `--cluster-max-in-buffer-size` at the old server's value,
`--cluster-compression` from `CAPI_COMPRESSION`, and the HTTPS listener
off. The buffer size matters because the C API default cannot return
the full table, and an oversized reply is `ErrNetworkInbufTooSmall`,
which is fatal in the binding, so there is no reconnect.

## CLI and flow

```
scripts/tests/setup/start-services.sh     # once: qdbd
make -C tests/e2e load                    # once: dataset into qdbd (idempotent)

cd tests/e2e/bench
make check venv old-server                # parity check, bench venv, old binary
make bench-native@qdbd                    # -> results/native@qdbd.json
make bench-v1@old-rest                    # -> results/v1@old-rest.json
make bench-http-arrow@new-rest            # needs the v2 login and query
make bench-v1@new-rest                    # needs the v1 wrappers
make bench-flightsql@new-rest             # needs Flight SQL
make report                               # merges results/*.json
```

`bench.py run --run v1@new-rest` writes
`results/v1@new-rest.json`: per-repetition metrics, the mean,
environment (git shas of this repo/master/qdb-api-python, C API hash,
machine, timestamp), and the result fingerprint. `bench.py report` reads
whatever result files exist and prints two sections:

1. **Compatibility**: a fingerprint matrix per query across all runs,
   with the sentence that matters, `v1@old-rest == v1@new-rest`, called
   out explicitly, plus the wart counts.
2. **Performance**: wall-clock / TTFB / RSS table, with the two headline
   deltas: `v1@new-rest` vs `v1@old-rest` (drop-in speedup) and
   `flightsql@new-rest` vs `v1@old-rest` (gateway thesis), and
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

It is an iterator, so streaming protocols yield batches without a
forced concat, which would destroy their RSS story, and one-shot
protocols yield once. The harness fingerprints batches through a
streaming accumulator whose result is invariant to batch boundaries,
which `selftest` pins. `telemetry` is the protocol's channel for
wire-level metrics the harness cannot see: `response_bytes`,
`body_bytes_decoded`, `gzip`, `wart_count`.

Server modules (`servers/<name>.py`) expose:

```python
def server_cmd(cfg) -> list[str]
```

- The protocol calls `record_ttfb` once at its first-data moment
  (definitions above). The clock and the client CPU meter cover only the
  fetch-iterator pulls. Fingerprint accumulation between pulls is bench
  overhead and stays outside both.
- A protocol module knows nothing about which server answers, and it
  gets a base URL / URI from the harness. That is what makes
  `v1@old-rest` and `v1@new-rest` run byte-for-byte the same client
  code.
- The harness owns everything else: child forking, timing, RSS sampling,
  server lifecycle, fingerprinting, persistence. Adding `flightsql` later
  means writing `protocols/flightsql.py` (~30 lines) and enabling the
  registry row, with no harness changes.
- `v1.py` is a customer's script. It does an anonymous login, a
  `POST /api/query` over stdlib `http.client` (exact first-body-byte
  TTFB), a plain `json.loads` (that parse cost is the honest price of
  this path), and then columnar JSON to DataFrame with the sentinel
  strings normalized and counted. HTTP gzip is on in the standard runs,
  as real clients send `Accept-Encoding: gzip`. `--no-gzip` exists for
  probes, and every result records the setting plus both byte counts.

## Functional equivalence across runs

Because runs are separate invocations, equivalence is checked over
**persisted fingerprints** rather than live DataFrames. The fingerprint
of a result is computed after per-protocol normalization (timestamps to
UTC ns, the old server's sentinels to proper nulls, column order sorted)
and consists of:

- shape and column names;
- per-column null counts;
- numeric columns: sum/min/max, compared with relative tolerance 1e-9
  (awk-tolerance philosophy from qdb-nats-connector ADR-007);
- string/timestamp columns: a stable order-independent hash;
- first and last 5 rows, normalized, verbatim (human-debuggable diffs).

`bench.py report` compares fingerprints pairwise across runs for each
query id and reports pass/fail per column. "Somewhat the same" is thus
concrete: identical after documented normalization, with floats within
tolerance.

One caveat, stated once. Because normalization runs before
fingerprinting, `v1@old-rest == v1@new-rest` proves data equivalence
through a real client rather than byte-shape identity of the v1 JSON,
so a dropped wart would still pass. Byte-shape is the permanent e2e
golden pairs' job, and the informational `wart_count` keeps a silent
drop visible here.

The same fingerprint (via `native@qdbd`) is the one-time check that
the CSV export/import round trip of the dataset is faithful. Run it once
against a qdbd serving the original data directory, once against the
imported table, and compare.

## Where the rewrite drops in

`native@qdbd` and `v1@old-rest` agree on every fingerprint, which
cross-checks the v1 parser against the native client before the
rewrite enters the picture. The remaining rows are enabled
in `bench.py` when their server side exists: `http-arrow@new-rest` with
the v2 login and query (the first performance signal),
`v1@new-rest` with the v1 wrappers (the first drop-in
compatibility signal), `flightsql@new-rest` with Flight SQL.

## Decision log (2026-08-16)

| Decision                                 | Why                                                                                                | Rejected                                                                     |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| One-time tool, no abstraction investment | comparison is done once new-rest beats old-rest                                                    | permanent-grade infra in the bench, and shared abstractions up front         |
| qdbd + dataset come from the e2e harness | service model (ADR-007), and loading is a test resource with a longer lifetime than the bench      | `start-qdbd.sh`, `stop-all.sh`, `fetch-dataset.sh`, per-run fresh extraction |
| `bench.py` owns REST-server lifecycle    | it needs the pid and restarts per run anyway, and this resolves the old plan's contradiction       | `start-old-rest.sh` / `start-new-rest.sh` as operator steps                  |
| Run = (protocol, server) pair            | one v1 client module runs unchanged against old and new server: compat and perf from the same code | three opaque "targets" (conflates client code with the server it hits)       |
| Makefile as the only config source       | one place for paths/ports, passed as explicit flags                                                | `env.sh` sourced by many scripts                                             |
| Python only here, never in CI            | qdb-api-python build + master worktree are heavyweight and temporary                               | bench harness as the CI performance-budget mechanism                         |

## Decision log (2026-08-19)

| Decision                                                      | Why                                                                                                                         | Rejected                                                             |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| Measure two volumes (qdbd -> reducer, reducer -> client)      | the gateway thesis is about where the reduce runs, and one byte count cannot show it                                        | response size only                                                   |
| Volume 1 from qdbd `$qdb.statistics.requests.*` counters      | the only portable source, because macOS loopback is invisible to OS accounting, and one run at a time makes it attributable | `nettop`/`/proc/net`, per-connection instrumentation in the bindings |
| Reduce-shape query family, one query per class                | classes behave differently on the two axes, and a single aggregate would show only one of them                              | one "aggregation query"                                              |
| High-cardinality / fine-bucket keys for `agg_wide`/`agg_topk` | coarse buckets over low-cardinality keys are already reduced in qdbd and prove nothing                                      | hourly x `accountId`-style queries as the headline                   |
| Bytes + client CPU, no WAN emulation in v1                    | honest, portable and enough to settle the architectural question, and seconds can be derived later                          | dummynet/netem throttling as a prerequisite                          |
| Hard measured numbers stay out of this plan                   | they are subject to debate, the bench produces them, and results files hold them                                            | recording probe numbers as verified facts here                       |

## Decision log (2026-08-20)

| Decision                                                         | Why                                                                                                         | Rejected                                                       |
| ---------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| Reduce-family SQL as pinned in "Queries"                         | `GROUP BY id` (~1.5M groups) is the only key that loads the reducer, and 1h x `accountId` is the control    | 1s buckets (qdbd-bound, hides protocol deltas on localhost)    |
| `fetch` returns an iterator of DataFrames                        | stream mode must not concat, and one fingerprint accumulator serves one-shot and streaming alike            | `-> DataFrame` with a stream special case in the harness       |
| `telemetry` dict parameter on `fetch`                            | wire-level metrics (`response_bytes`, `wart_count`, `gzip`) have no other home                              | parsing them out of protocol return values                     |
| v1 HTTP gzip on by default, only mode in standard runs           | customer-realistic (`requests` sends `Accept-Encoding: gzip`), recorded per result, with a `--no-gzip` flag | gzip off (raw-wire baseline), measuring both (doubles v1 reps) |
| Native client input buffer = old server's `--max-in-buffer-size` | every run must accept the same result sizes, and the binding default (256 MiB) fails `agg_wide`/`full`      | binding defaults per client                                    |
| Counters read key-by-key on a direct node connection             | `stats.by_node` scans every stat key (~3k requests/read) and drowns small queries                           | `quasardb.stats.by_node` full scan                             |
| All-null columns fingerprint type-free                           | no observable wire type: v1 JSON types them `none`, the native client picks a dtype                         | per-protocol dtype exceptions in the comparison                |

## Decision log (2026-08-24)

| Decision                                                                                       | Why                                                                                                                                                                                         | Rejected                                                                                                                             |
| ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `native@qdbd` measures `stream_query()` only                                                   | owner decision (Leon), because the streaming path is the native reference the gateway is chasing, and one mode halves every native run                                                      | keeping the one-shot `qdb_query` sub-mode                                                                                            |
| C API compression pinned per run via `CAPI_COMPRESSION`, default `none` (owner decision, Leon) | binding defaults diverge (python balanced, old server none) and polluted the aggregation comparison, and the old server is not configurable, so `none` is the only mode every run can share | per-binding defaults (proven inconsistent), and balanced everywhere (old-rest cannot honor it)                                       |
| 3 warmups + 5 measured reps per query, median reported (owner decision, Leon)                  | qdbd needs 2-3 executions to reach steady state and run ordering leaked into the old means, and the median shrugs off one straggler                                                         | mean of 3 with no warmup (the polluted status quo), and a cheap warmup outside the measurement path (a second code path to mistrust) |

## Decision log (2026-09-17)

| Decision                                      | Why                                                                                             | Rejected                                          |
| --------------------------------------------- | ----------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| The bench is the one home of measured numbers | ADR-0013: TTFB and RSS are numbers a person reads, not gates                                    | TTFB and RSS recorded or gated by the e2e harness |
| `http-arrow@new-rest` as a run                | needs only the v2 login and query, so the rewrite's first number does not wait for the wrappers | waiting for `v1@new-rest` or `flightsql@new-rest` |
