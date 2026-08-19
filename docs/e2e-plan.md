# End-to-End Test Harness -- Plan

Status: approved. This document specifies the permanent e2e harness
described in the brief's Testing doctrine (items 2 and 3): golden-data
equivalence, budgets, and stress against a live qdbd. It is a working
document: verified facts and dates are recorded here, not in the brief;
progress is recorded in `docs/log.md`, not here. Decisions were taken
2026-08-16 (see Decision log at the end).

## Purpose

Prove, on every commit and for the life of the product, that the REST
server:

1. returns correct results at scale, across all wire formats;
2. stays inside its performance budgets (time to first byte, bounded
   memory, throughput);
3. behaves honestly under stress: fast, explicit failure under overload,
   in-flight streams survive graceful shutdown.

The harness is Make + shell + curl + awk (the qdb-nats-connector ADR-007
lineage), runs in Buildkite, and contains no Python. The temporary
multi-target performance comparison lives separately in
`tests/e2e/bench/` (see `docs/bench-plan.md`) and consumes this harness's
services and dataset.

## Service model

qdbd is a persistent background service, never started by a test:

- `scripts/tests/setup/start-services.sh` (the shared qdb-test-setup used
  by qdb-nats-connector, qdb-api-python, qdb-api-go, as a git submodule of
  `bureau14/qdb-test-setup` pinned by SHA, never edited locally) starts
  qdbd insecure on `127.0.0.1:2836` and secure on `2838`, with qdbd flags,
  license and cluster keys owned by that one script. It force-restarts
  qdbd and wipes the data directories, so `make load` follows it.
- Tests assume the service is up and fail fast with a clear message if
  the port does not answer. Service failures are infrastructure issues,
  not test concerns.
- The REST server under test is the only process the harness starts and
  stops, via pidfile helpers copied from nats-connector's `common.sh`.
  Every server the harness starts, and every golden capture, runs under
  `TZ=UTC`: the legacy JSON renders timestamps in the server's local
  time zone, so goldens are portable only with the zone pinned.

## Dataset

Canonical dataset: table `reproduce`, **5,613,032 rows**, 14 columns
(strings, int64, double, timestamps, real null distribution), ~834 MB as
legacy JSON. Origin: the sc-19522 customer memory-optimization case.

Distribution format: **CSV plus `qdb_import` config**, produced once by
`qdb_export --ts reproduce -f reproduce.csv --config reproduce.import.json`
from the original qdbd data directory (`shard_size` added to the config so
`qdb_import` creates the table). Archive contents:

```
reproduce.csv            data, no header (qdb_export convention)
reproduce.import.json    qdb_import parser/column config, with shard_size
metadata.json            row count, sha256 of the csv, generation date
```

Hosting follows qdb-nats-connector: the public builddeps S3 bucket, prefix
`datasets/qdb-api-rest/`, archive name `reproduce-<date>-5613032.tar.gz`,
addressed by an in-repo `tests/e2e/datasets.json` (`base_url` +
`archives[]` of `{name, date, rows, sha256}`; the sha256 is of the archive
and is verified after download), fetched with plain `curl -fL`, no auth.
Developer and CI agent obtain the data identically. Until the upload
happens, `make load DATASETS_LOCAL_DIR=<dir>` copies the archive from a
local directory instead of downloading.

The archive is produced by `tests/e2e/tools/package-dataset.sh` (`make
package-dataset SRC=<db.tar.zst> OUT=<dir>`): it starts a throwaway qdbd on
the extracted data directory (port 2846, never the shared service), reads
the shard size from `SHOW TABLE`, exports with `qdb_export`, injects
`shard_size` into the import config, writes `metadata.json`, and prints
the `datasets.json` entry and the `aws s3 cp` command (upload is a manual
operator step). Verified 2026-08-19: `qdb_export` reads through the bulk
reader and a single-shot export of the full table overflows the client
network input buffer (125 MiB, no flag to raise it); the harness exports
one shard-sized half-open range at a time and concatenates, which is
byte-identical to a single export (`common.sh::export_table_csv`).

Loading is a test-owned, idempotent step: `make load` downloads, verifies
the sha256, and runs `qdb_import -f reproduce.csv --config
reproduce.import.json -j <n>` against the running qdbd -- skipped when
`SELECT COUNT(*) FROM "reproduce"` already reports 5,613,032. Query
workloads are read-only, so the loaded table persists for the qdbd
lifetime.

Round-trip fidelity (nulls, nanosecond timestamps, quoting) is verified
by `make verify-dataset`: the loaded table is exported again and compared
byte-for-byte with the CSV it was imported from (verified identical
2026-08-19, 5,613,032 rows, ~3 s import with `-j 8`). The bench's
`native@qdbd` fingerprint against the original data directory remains the
belt-and-braces check. A mismatch is a `qdb_export`/`qdb_import` bug
worth surfacing, not a harness problem.

## The CSV is the expected output

For the large-table equivalence check, no second expected artifact
exists: `POST /api/v2/query` with `Accept: text/csv` on
`SELECT * FROM "reproduce"` is compared against `reproduce.csv` with the
awk `compare_csv` tolerance comparator (numeric fields within tolerance,
everything else exact; QuasarDB's deterministic ordering makes row order
comparable). Cross-format equivalence (JSON, NDJSON, Arrow IPC, Flight
SQL) is covered by the Go generative property tests, not by golden files.

Legacy byte-shape equivalence (`/api/login`, `/api/query`, `/api/tags`,
status probes) uses small golden request/response pairs captured from the
old server under `tests/e2e/golden/legacy/<NN-slug>/`: a hand-written
`request.json` (method, path, pre-encoded query string, headers, JSON
body, auth mode `none|bearer|urlparam`, compare mode
`bytes|gunzip|login-shape`) next to the captured `status`, `headers`
(only `content-type` and `content-encoding`, lowercased, sorted; absence
is recorded as absence) and `body` (raw bytes; decompressed for
`gunzip`). `login-shape` checks `{"token": <non-empty string>}` because
tokens vary per call. `tests/e2e/legacy.sh capture|replay` drives both
sides; `make capture-golden` is an operator step, `make test-legacy`
replays against the server under test, `make test-legacy-selfcheck`
replays against the old server to prove the goldens are deterministic.
Full-table golden responses are deliberately not captured (834 MB of
JSON is not a fixture).

Goldens are captured from the old server **built from `master`** in a
worktree (`make old-server`), linked against this repo's `qdb/` tree --
the same C API the server under test uses, so any difference is the REST
layer's. Verified 2026-08-19: the legacy wire-shape code
(`qdbinterface/query.go`, `models/`, `restapi/configure_qdb_api_rest.go`)
is byte-identical between tag `v3.14.2`, branch `3.14.x` and `master`
(only `const version = "3.14.2"` differs); `3.14.x` itself has no
`vendor/` and needs a `../qdb-api-go` checkout at v3.13.5.

Fixture for the goldens (`make seed`, `tests/e2e/seed.sql`, idempotent):
the nine tagged tables from old master's rest-setup (`foo/bar/baz_01..03`,
tags `tag_01..03` on `$qdb.tagroot`), `legacy_types` (blob, int64,
double, string, symbol, timestamp; one full, one all-null, one mixed row
with a nanosecond timestamp and `"`, `,`, `<&>` in a string) and
`legacy_allnull` (pins `"type":"none"`). `reproduce` supplies count,
`LIMIT 10` and a `GROUP BY side` aggregate.

Verified 2026-08-19, relevant to the brief's wart list: against the 3.15
C API, null cells of every type reach the REST layer typed
`qdb_query_result_none` and serialize as JSON `null`, with the column
type taken from the last non-null row (`"none"` if all null). The
`"(void)"` / `"(undefined)"` branches in the old `qdbinterface/query.go`
(typed int64/timestamp carrying the undefined sentinel) were not
reachable with any probed query (raw selects, `IN RANGE`, `GROUP BY`,
`MIN/MAX/SUM/FIRST/LAST`, arithmetic on nulls). The goldens therefore pin
`null`; a rewrite that keeps the same sentinel mapping for typed
undefined values is faithful either way, but the sentinels cannot be
pinned by a golden under this C API.

Two compatibility layers, deliberately: this harness checks **byte-shape**
(golden pairs, permanent, CI); the temporary bench checks **semantic**
compatibility through a real client -- the same legacy-protocol Python
code run against the old and the new server, compared by normalized
DataFrame fingerprint (`legacy@old-rest == legacy@new-rest` in
`docs/bench-plan.md`).

## Stress definition

All against the 5,613,032-row table, all shell + curl + awk:

1. **Correctness at scale**: full-table equivalence per format (above).
2. **Budgets as CI gates** (brief item 3): time to first byte under a
   fixed bound (`curl -w '%{time_starttransfer}'`), server RSS delta
   bounded and independent of result size (`ps -o rss=` sampled during
   the request), sustained throughput floor per format. Budget numbers
   are versioned in the repo and revised deliberately, never silently.
3. **Concurrency**: N parallel clients (`xargs -P` + curl) against the
   full query; assert admission control fails fast with 429/503 +
   `Retry-After` instead of goodput collapse, and that in-flight streams
   complete across a graceful shutdown drain.

## Layout

```
scripts/tests/setup/      qdb-test-setup git submodule (start-services.sh, ...); qdbd up
tests/e2e/
  datasets.json           base_url + archives[{name,date,rows,sha256}]
  Makefile                services-check | download-golden | extract | load | verify-dataset |
                          seed | package-dataset | old-server | capture-golden |
                          test-legacy | test-legacy-selfcheck | clean | distclean
                          (later: test-formats | test-budgets | test-stress)
  common.sh               helpers: log_*, pidfile/start_server/stop_server, qdbsh wrapper,
                          count_qdb_rows, export_table_csv (chunked), compare_csv, sha256_file
  legacy.sh               golden capture/replay driver
  seed.sql                legacy fixture (qdbsh statements)
  tools/package-dataset.sh  db.tar.zst -> dataset archive (operator)
  budgets.env             versioned budget numbers (later)
  golden/legacy/          legacy request/response pairs
  .old-master/            git worktree of master for the old server (gitignored)
  bench/                  temporary multi-target comparison (docs/bench-plan.md)
  AGENTS.md, README.md    conventions, usage
```

The bench's `make old-server` delegates to this Makefile's target; there is
one recipe for building the old server.

## What is reused from qdb-nats-connector, and what is not

Reused: `scripts/tests/setup/` (as a submodule), `datasets.json` and the
`download-golden`/`extract` Makefile recipes (copied, plus sha256
verification), `common.sh` helpers (copied, then adapted).

Not reused: `tools/generator` (synthetic messages: weighted choices,
random floats, fixed-interval timestamps) and `qdb-data-loader` (publishes
to NATS). Wrong shape and wrong sink: our loader is `qdb_import` (later
`/api/v2/ingest`, as a self-test), and the customer-derived table -- real
nulls, real string cardinality, real skew -- exercises encoders in ways
synthetic data hides. Schema variety (multi-table ingest, symbols, blobs,
tags) comes from the Go `rapid` property tests, generated in-process.

## Decision log (2026-08-16)

| Decision                                              | Why                                                                                | Rejected                                                                  |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Permanent e2e separate from temporary bench           | different lifetimes; the bench dies once new-rest beats old-rest                   | one combined assessment framework; shared abstractions up front           |
| qdbd via shared `start-services.sh`                   | ADR-007 service model; one owner for qdbd flags/license                            | bench-private `start-qdbd.sh` / `stop-all.sh`                             |
| Dataset as CSV + import config, loaded by qdb_import  | shared qdbd means the front door is the only way in; CSV doubles as ingest fixture | qdbd data-directory tarball (`db.tar.zst`); fresh extract per launch      |
| S3, nats-connector style, `datasets.json` + curl      | developer and CI fetch identically; proven                                         | `.buildkite/tools/artifacts.py` (per-build ephemeral layout); env-var URI |
| Idempotent `make load` behind a COUNT(*) check        | data is a test resource, not an operator chore                                     | manual pre-loading; per-run loading                                       |
| CSV is the expected output for full-table equivalence | no second 834 MB artifact; ordering is deterministic                               | full-table golden responses per format                                    |
| Stress in shell/curl/awk                              | keeps the permanent path free of Python and heavyweight builds                     | Python or the bench harness in CI                                         |
| Copy nats helpers, not its generator/loader           | helpers fit; generator/loader have the wrong shape and sink                        | nats YAML generator as fixture source                                     |

## Decision log (2026-08-19)

| Decision                                        | Why                                                                                                                                          | Rejected                                              |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| qdb-test-setup as a git submodule, not a copy   | one owner for qdbd flags/license; same as nats-connector and old master; updates by SHA                                                      | copying the scripts (drift, duplicated owner)         |
| Goldens from the old server built from `master` | wire code == v3.14.2 by diff; same C API as the server under test; `3.14.x` needs an old qdb-api-go checkout and would pair different C APIs | released 3.14.2 binary; `3.14.x` source build         |
| `TZ=UTC` pinned by the harness                  | legacy timestamps are local-time; goldens must be machine-portable                                                                           | capture in host zone                                  |
| Seeded fixture plus `reproduce`                 | controllable types/nulls/tags; real data for count/head/aggregate                                                                            | `reproduce` only (no blob, no tags, no null control)  |
| `sha256` per archive in `datasets.json`         | brief says sha256-pinned; nats relies on dated names only                                                                                    | dated filename alone                                  |
| Chunked `qdb_export` in the harness             | client input buffer caps single-shot export; chunks are byte-identical                                                                       | raising the buffer (no flag); a different export tool |
