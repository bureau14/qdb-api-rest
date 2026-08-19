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

- `scripts/tests/setup/start-services.sh` (a copy of the shared
  qdb-test-setup used by qdb-nats-connector, qdb-api-python, qdb-api-go)
  starts qdbd insecure on `127.0.0.1:2836` and secure on `2838`, with
  qdbd flags, license and cluster keys owned by that one script.
- Tests assume the service is up and fail fast with a clear message if
  the port does not answer. Service failures are infrastructure issues,
  not test concerns.
- The REST server under test is the only process the harness starts and
  stops, via pidfile helpers copied from nats-connector's `common.sh`.

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
`archives[]`), fetched with plain `curl -fL`, no auth. Developer and CI
agent obtain the data identically. Until the upload happens,
`~/Downloads` is the fallback location.

Loading is a test-owned, idempotent step: `make load` downloads, verifies
the sha256, and runs `qdb_import -f reproduce.csv --config
reproduce.import.json -j <n>` against the running qdbd -- skipped when
`SELECT COUNT(*) FROM "reproduce"` already reports 5,613,032. Query
workloads are read-only, so the loaded table persists for the qdbd
lifetime.

Round-trip fidelity (nulls, nanosecond timestamps, symbol columns) is
verified once at conversion time by fingerprinting the native client's
result against the original data directory and against the imported
table. A mismatch is a `qdb_export`/`qdb_import` bug worth surfacing, not
a harness problem.

## The CSV is the expected output

For the large-table equivalence check, no second expected artifact
exists: `POST /api/v2/query` with `Accept: text/csv` on
`SELECT * FROM "reproduce"` is compared against `reproduce.csv` with the
awk `compare_csv` tolerance comparator (numeric fields within tolerance,
everything else exact; QuasarDB's deterministic ordering makes row order
comparable). Cross-format equivalence (JSON, NDJSON, Arrow IPC, Flight
SQL) is covered by the Go generative property tests, not by golden files.

Legacy byte-shape equivalence (`/api/login`, `/api/query`, `/api/tags`)
uses small golden request/response pairs captured from the old server
(count, head, aggregates) under `tests/e2e/golden/`. Full-table golden
responses are deliberately not captured (834 MB of JSON is not a fixture).

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
scripts/tests/setup/      shared qdb-test-setup (start-services.sh, stop-services.sh,
                          config.sh, utils.sh, cleanup.sh); qdbd up
tests/e2e/
  datasets.json           base_url + archives[]; same schema as qdb-nats-connector
  Makefile                download-golden | extract | load | test-legacy | test-formats |
                          test-budgets | test-stress | clean
  common.sh               copied helpers: count_qdb_rows, wait_for_qdb_rows,
                          export_table_csv, compare_csv, pidfile helpers, log_*
  budgets.env             versioned budget numbers
  golden/                 legacy request/response pairs
  bench/                  temporary multi-target comparison (docs/bench-plan.md)
```

## What is reused from qdb-nats-connector, and what is not

Reused (copied, then adapted): `scripts/tests/setup/`, `datasets.json` and
the `download-golden`/`extract` Makefile recipes, `common.sh` helpers.

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
