# End-to-End Test Harness -- Plan

Status: approved. This document specifies the permanent e2e harness
described in the brief's Testing doctrine (items 2 and 3): golden e2e
and stress against a live qdbd. It is a working
document: verified facts and dates are recorded here, not in the brief;
progress is recorded in `docs/log.md`, not here. Decisions are in the
dated decision logs at the end.

## Purpose

Prove, for the life of the product, that the built binary, driven over
HTTP like a client:

1. returns exactly the audited response, on the v2 surface and on the
   v1 surface (the goldens);
2. behaves honestly under stress: fast, explicit failure under overload,
   in-flight streams survive graceful shutdown.

Every assertion is pass or fail. The harness measures nothing: time to
first byte, memory and throughput are the bench's (ADR-0013;
`docs/bench-plan.md`), which lives separately in `tests/e2e/bench/` and
consumes this harness's services and dataset.

The harness is Make + shell + curl + awk (the qdb-nats-connector ADR-007
lineage) and contains no Python. It runs identically on developer
machines and in Buildkite on every platform (see "In Buildkite").

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
  `TZ=UTC`: the v1 JSON renders timestamps in the server's local
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
expected/<suite>/<case>/ expected responses too large for git (see Goldens)
```

Hosting follows qdb-nats-connector: the public builddeps S3 bucket, prefix
`datasets/qdb-api-rest/`, archive name `reproduce-<date>-5613032.tar.gz`,
addressed by an in-repo `tests/e2e/datasets.json` (`base_url` +
`archives[]` of `{name, date, rows, sha256}`; the sha256 is of the archive
and is verified after download), fetched with plain `curl -fL`, no auth.
Developer and CI agent obtain the data identically;
`make load DATASETS_LOCAL_DIR=<dir>` takes the archive from a local
directory instead, for a freshly packaged archive that is not uploaded
yet.

The archive is produced by `tests/e2e/tools/package-dataset.sh` (`make
package-dataset SRC=<db.tar.zst> OUT=<dir>`): it starts a throwaway qdbd on
the extracted data directory (port 2846, never the shared service), reads
the shard size from `SHOW TABLE`, exports with `qdb_export`, injects
`shard_size` into the import config, writes `metadata.json`, and prints
the `datasets.json` entry and the `aws s3 cp` command (upload is a manual
operator step). The export runs one shard-sized range at a time
(`common.sh::export_table_csv` says why).

Loading is a test-owned, idempotent step: `make load` downloads, verifies
the sha256, and runs `qdb_import -f reproduce.csv --config
reproduce.import.json -j <n>` against the running qdbd -- skipped when
`SELECT COUNT(*) FROM "reproduce"` already reports 5,613,032. Query
workloads are read-only, so the loaded table persists for the qdbd
lifetime.

Round-trip fidelity (nulls, nanosecond timestamps, quoting) is verified
by `make verify-dataset`: the loaded table is exported again and compared
byte-for-byte with the CSV it was imported from (verified identical
2026-08-19). The bench's
`native@qdbd` fingerprint against the original data directory remains the
belt-and-braces check. A mismatch is a `qdb_export`/`qdb_import` bug
worth surfacing, not a harness problem.

CI loads the same archive developers do; there is one dataset. Cases
bound their own result size with `IN RANGE` or `LIMIT`, and no case
selects the whole table: a full-table response is the bench's business.
To verify with the first CI run (2026-09-17: unmeasured): the wall-clock
time of `make load` on the slowest agent. The fallback, if it is
unacceptable, is a second, smaller archive of whole shards loaded under
its own table name, with the two `reproduce` v1 goldens recaptured
against it.

## Goldens

A golden is an audited expected response: a run somebody looked at,
judged correct, and committed; every later run is compared with it byte
for byte (ADR-0013). No canonicalization and no tolerance: the text
encoders are deterministic on every platform, QuasarDB returns rows in
a stable order, and an unexpected byte is a bug worth seeing. Capture
never runs in CI; a golden changes only in a commit whose diff the owner
reviews.

A case is a directory `tests/e2e/golden/<suite>/<NN-slug>/`: a
hand-written `request.json` (method, path, pre-encoded query string,
headers, body, auth mode, compare mode) next to the expected `status`,
`headers` (only the headers that belong to a contract, lowercased,
sorted; absence is recorded as absence) and `body`. One driver,
`golden.sh`, captures and replays both suites. Compare modes: `bytes`;
`gunzip` (the decompressed bytes); `login-shape`, because a login
answers a token that differs per call (`{"token": <non-empty string>}`
on v1, RFC 6749's fields on v2).

An expected body small enough for review lives in git next to its
`request.json`; a body too large for git lives in the dataset archive
under `expected/<suite>/<case>/`, qdb-nats-connector's layout, so a
failure still has a body to diff against.

Cross-format equivalence (JSON, NDJSON, CSV, Arrow IPC, Flight SQL) is
covered by the Go generative property tests, never by golden files.

An endpoint lands with its goldens, so the suites grow with the
milestones (`docs/brief.md`, Milestones): the auth and session cases,
the exploration cases and the `/api/v2/sql` cases join `v2` with their
endpoints; ingestion is qdb-nats-connector's flow exactly -- ingest
through the API, `qdb_export`, byte-compare with the golden CSV. Flight
SQL has no shell client; whether it gets a small Go tool in this
harness or stays with the property tests is decided at that milestone's
entry.

### The v2 suite

Captured from the server under test (`make capture-v2`, an operator
step) and audited before it is committed: a data case against qdbsh or
`qdb_export` output for the same range, a shape or error case against
the ADR that owns the wire contract.

A v2 query answers the same data in every format and under every
content coding, so a query case is one request run as a matrix, never a
directory per format. Its `request.json` lists the `formats` (`json`,
`ndjson`, `csv`, `arrow`) and the `encodings` (`identity`, `gzip`) it
runs under; the driver issues one request per pair, setting `Accept`
and `Accept-Encoding` itself:

```
golden/v2/10-query-seed-types/
  request.json   query, auth, formats, encodings
  status         one, shared by every run of the matrix
  body.json
  body.ndjson
  body.csv       also what the decoded Arrow stream must equal
  schema.arrow   field names, types, timestamp unit
golden/v2/30-query-no-bearer/
  request.json  status  headers  body      no formats: one run
```

- The `status` is the same for every run of the matrix.
- `Content-Type` is asserted from the format and `Content-Encoding`
  from the encoding; neither is stored per run. A case without
  `formats` (a login, an error) stores its `headers` like a v1 case.
- A text body is compared byte for byte with `body.<format>`.
- A compressed run is decompressed and compared with the same
  `body.<format>`, so a content coding adds no golden.
- Arrow IPC is compared as decoded content (ADR-0013): the format
  leaves null slots and padding undefined, so an Arrow body has no
  stable bytes, and decoding normalizes both. The body is piped through
  `tools/arrowcsv`, a pure-Go tool (`arrow-go`'s IPC reader and the CSV
  encoder of `internal/encoding`; no cgo, built by the Makefile with
  the same toolchain as the server) that writes the schema to one file
  and the batches as one CSV to another. The schema is compared with
  `schema.arrow`, the CSV byte for byte with `body.csv`, so Arrow adds
  no large golden.

Cases: the login (anonymous, and the secure cluster's user on the
secure cluster); the query over the seeded fixture (every type, nulls,
the awkward string) and over a `reproduce` range of roughly 200k rows,
so every text encoder crosses its 65536-row chunk boundary several
times and the Arrow stream is multi-batch, over real null timestamps
the Go table fixture cannot write; and the rows of the ADR-0010 and
ADR-0011 error tables a client can provoke from outside (no bearer,
malformed bearer, unsupported media type, oversized body, invalid
query, refused credentials). zstd is covered by the Go round-trip test;
it joins `encodings` only if the `zstd` CLI is present on every agent.

### The v1 suite

Byte-shape equivalence of the v1 endpoints (`/api/login`, `/api/query`,
status probes) uses small golden request/response pairs captured from
the old server under `tests/e2e/golden/v1/<NN-slug>/`: a hand-written
`request.json` (method, path, pre-encoded query string, headers, JSON
body, auth mode `none|bearer|urlparam`, compare mode
`bytes|gunzip|login-shape`) next to the captured `status`, `headers`
(only `content-type` and `content-encoding`, lowercased, sorted; absence
is recorded as absence) and `body` (raw bytes; decompressed for
`gunzip`). `login-shape` checks `{"token": <non-empty string>}` because
tokens vary per call. The driver's `capture|replay` modes drive both
sides; `make capture-v1` is an operator step, `make test-v1`
replays against the server under test, `make test-v1-selfcheck`
replays against the old server to prove the goldens are deterministic.
Both replays compare with the same files. Every login and query golden
also replays at its `/api/v1/<path>` spelling against the server under
test; the probe goldens replay at the unversioned path only (ADR-0008).
Full-table golden responses are deliberately not captured (834 MB of
JSON is not a fixture).

Goldens are captured from the old server **built from `master`** in a
worktree (`make old-server`), linked against this repo's `qdb/` tree --
the same C API the server under test uses, so any difference is the REST
layer's. `master`'s wire-shape code is byte-identical to the released
`v3.14.2` (verified 2026-08-19), so the goldens stand for the deployed
server.

Fixture for the goldens (`make seed`, `tests/e2e/seed.sql`, idempotent):
the nine tagged tables from old master's rest-setup (`foo/bar/baz_01..03`,
tags `tag_01..03` on `$qdb.tagroot`), `seed_types` (blob, int64,
double, string, symbol, timestamp; one full, one all-null, one mixed row
with a nanosecond timestamp and `"`, `,`, `<&>` in a string) and
`seed_allnull` (pins `"type":"none"`). `reproduce` supplies `LIMIT 10`.

Two places where the goldens and the contract (`docs/brief.md`,
Compatibility contract) meet:

- Null cells are JSON `null` in every golden; the old server's sentinel
  strings never appear because the C API types every null cell
  `qdb_query_result_none` (verified 2026-08-19 over raw selects,
  `IN RANGE`, `GROUP BY`, aggregates and arithmetic on nulls).
- No golden exercises a deliberate deviation (ADR-0013; the list is
  `docs/brief.md`, Compatibility contract, "Deliberate deviations"). A
  `COUNT(...)` column is the one a query could reach -- the old server
  types it `count`, v1 `int64` -- so no golden query selects a `COUNT`.

The byte-shape facts the goldens pin (verified 2026-09-02 from the old
server's models and producers on `master`; the v1 wrappers in
`internal/httpapi/v1` reproduce them, ADR-0007):

- Key order and omission follow the old models' struct order: column
  objects serialize `data`, `name`, `type` (`name` and `type`
  omitempty); table objects `columns` then `name` (`name` omitempty,
  `columns` never omitted). A query-result table carries no name, a
  tag-find table carries `"columns":null`, and an empty result is
  `{"tables":[]}`, never `null`.
- 200/400/500 bodies end with a newline and leave HTML unescaped
  (`SetEscapeHTML(false)`; the seeded `<&>` string pins it); 401 bodies
  have no trailing newline and use `{"code":401,"message":"..."}` in
  that key order.
- The 401 messages: no token -> `unauthenticated for invalid
credentials`; unverifiable token -> `Invalid authentication token`;
  expired token -> `Token has expired. Please login again`.
- Query and find execution errors are 400 `{"message":...}`; the
  message is the binding's error, a space, and the query result's
  `ErrorMessage()` (golden: `query_execute (operation=query_execute,
query=SELECT FROM): The provided query is invalid. expected FROM`).
  The binding's `wrapError` rendering is identical between the old
  server's vendored binding and ours.
- The find wart executes the raw find expression through the binding's
  `Find().ExecuteString` (`qdb_query_find`); `qdb_get_tagged` plays no
  part in it.
- Auth extraction: the `Authorization` header's `Bearer ` prefix is
  optional (a bare token authenticates), and the header wins over the
  `?token=` parameter.
- gzip: a request whose `Accept-Encoding` contains the substring `gzip`
  gets a gzipped body plus `content-encoding: gzip`, on every route;
  everything else is identity.

Two compatibility layers, deliberately: this harness checks **byte-shape**
(golden pairs, permanent, Buildkite; replay needs the committed goldens
and never the old server); the temporary, local bench checks **semantic**
compatibility through a real client -- the same legacy-protocol Python
code run against the old and the new server, compared by normalized
DataFrame fingerprint (`v1@old-rest == v1@new-rest` in
`docs/bench-plan.md`).

## In Buildkite

`scripts/cicd/40.test-e2e.sh` runs after the Go tests in every
platform's build step, against the qdbd `start-services.sh` started and
the binary `20.build.sh` built: `make load seed`, then the suites. A
suite enters the step when it is green: `test-v2` first, `test-v1`
when the wrappers land, the stress with the resilience milestone. The
script follows qdb-nats-connector's `50.test-e2e.sh`: GNU make discovery
(`gmake` on FreeBSD, `mingw32-make` on Windows), the Windows DLL
co-location next to the binary, and a dump of the REST server's and
qdbd's logs on failure. `curl`, `jq` and `gunzip` are what that pipeline
already requires on the same agents.

## Stress definition

Shell + curl, asserted as behaviour, never as a timing or a measured
number (ADR-0013): N parallel clients (`xargs -P` + curl) against a
large `reproduce` range; assert that the session budget bounds load (a
request past the budget waits for a session or times out at its
deadline, every admitted request completes with the golden body), and
that in-flight streams complete across a graceful shutdown drain.

## Layout

```
scripts/tests/setup/      qdb-test-setup git submodule (start-services.sh, ...); qdbd up
tests/e2e/
  datasets.json           base_url + archives[{name,date,rows,sha256}]
  Makefile                services-check | download-golden | extract | load | verify-dataset |
                          seed | package-dataset | old-server | capture-v1 |
                          test-v1 | test-v1-selfcheck | clean | distclean
                          capture-v2 | test-v2 (arrive with the v2 suite)
                          test-stress (arrives with the resilience milestone)
  common.sh               helpers: log_*, pidfile/start_server/stop_server, qdbsh wrapper,
                          count_qdb_rows, export_table_csv (chunked), sha256_file
  golden.sh               golden capture/replay driver (drives both suites once the
                          v2 suite arrives)
  seed.sql                golden fixture (qdbsh statements)
  tools/package-dataset.sh  db.tar.zst -> dataset archive (operator)
  tools/arrowcsv/         Go: Arrow IPC stream -> schema + CSV (arrives with the v2 suite)
  golden/v1/              v1 request/response pairs, captured from the old server
  golden/v2/              v2 cases, one per request, formats inside (arrives with the v2 suite)
  .old-master/            git worktree of master for the old server (gitignored)
  bench/                  temporary multi-target comparison (docs/bench-plan.md)
  AGENTS.md, README.md    conventions, usage
```

The bench's `make old-server` delegates to this Makefile's target; there is
one recipe for building the old server.

## Lineage

`scripts/tests/setup/` is qdb-nats-connector's test setup as a
submodule; `datasets.json`, the `download-golden`/`extract` recipes and
the `common.sh` helpers are copies from that repository, adapted. Its
synthetic-message generator and NATS loader are not used: the loader
here is `qdb_import` (and `/api/v2/ingest` as a self-test once it
exists), because the customer-derived table -- real nulls, real string
cardinality, real skew -- exercises encoders in ways synthetic data
hides. Schema variety (multi-table ingest, symbols, blobs, tags) comes
from the Go `rapid` property tests, generated in-process.

## Decision log (2026-08-16)

| Decision                                             | Why                                                                                | Rejected                                                                  |
| ---------------------------------------------------- | ---------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Permanent e2e separate from temporary bench          | different lifetimes; the bench dies once new-rest beats old-rest                   | one combined assessment framework; shared abstractions up front           |
| qdbd via shared `start-services.sh`                  | ADR-007 service model; one owner for qdbd flags/license                            | bench-private `start-qdbd.sh` / `stop-all.sh`                             |
| Dataset as CSV + import config, loaded by qdb_import | shared qdbd means the front door is the only way in; CSV doubles as ingest fixture | qdbd data-directory tarball (`db.tar.zst`); fresh extract per launch      |
| S3, nats-connector style, `datasets.json` + curl     | developer and CI fetch identically; proven                                         | `.buildkite/tools/artifacts.py` (per-build ephemeral layout); env-var URI |
| Idempotent `make load` behind a COUNT(*) check       | data is a test resource, not an operator chore                                     | manual pre-loading; per-run loading                                       |
| Stress in shell/curl/awk                             | keeps the permanent path free of Python and heavyweight builds                     | Python or the bench harness in CI                                         |
| Copy nats helpers, not its generator/loader          | helpers fit; generator/loader have the wrong shape and sink                        | nats YAML generator as fixture source                                     |

## Decision log (2026-08-19)

| Decision                                        | Why                                                                                                                                          | Rejected                                              |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| qdb-test-setup as a git submodule, not a copy   | one owner for qdbd flags/license; same as nats-connector and old master; updates by SHA                                                      | copying the scripts (drift, duplicated owner)         |
| Goldens from the old server built from `master` | wire code == v3.14.2 by diff; same C API as the server under test; `3.14.x` needs an old qdb-api-go checkout and would pair different C APIs | released 3.14.2 binary; `3.14.x` source build         |
| `TZ=UTC` pinned by the harness                  | legacy timestamps are local-time; goldens must be machine-portable                                                                           | capture in host zone                                  |
| Seeded fixture plus `reproduce`                 | controllable types/nulls/tags; real data for count/head/aggregate                                                                            | `reproduce` only (no blob, no tags, no null control)  |
| `sha256` per archive in `datasets.json`         | brief says sha256-pinned; nats relies on dated names only                                                                                    | dated filename alone                                  |
| Chunked `qdb_export` in the harness             | client input buffer caps single-shot export; chunks are byte-identical                                                                       | raising the buffer (no flag); a different export tool |

## Decision log (2026-08-20)

| Decision                                     | Why                                                                                                 | Rejected                                                 |
| -------------------------------------------- | --------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| Lazy login + `CASES=` selection in golden.sh | auth-free cases (status probes) replay against a server without `/api/login`; single-case debugging | eager login (couples every replay to the login endpoint) |

## Decision log (2026-09-12)

| Decision                                    | Why                                                              | Rejected                                          |
| ------------------------------------------- | ---------------------------------------------------------------- | ------------------------------------------------- |
| No full-table `text/csv` equivalence target | not a target; cross-format correctness is the Go property test's | the awk tolerance comparator over `reproduce.csv` |

## Decision log (2026-09-17)

| Decision                                          | Why                                                                                     | Rejected                                                                        |
| ------------------------------------------------- | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| The harness measures nothing; budgets leave       | ADR-0013: a number is the bench's, CI gates are yes/no                                  | `test-budgets`, `budgets.env`, TTFB and RSS gates                               |
| Arrow compared decoded, against the CSV golden    | ADR-0013; no stable bytes, and the audited CSV golden already says what the data is     | status and `content-type` only; a separate Arrow golden; pyarrow in the harness |
| One driver for both suites                        | capture, replay and compare are the same mechanics; only the login and the paths differ | a second script per suite                                                       |
| Expected bodies in git, large ones in the archive | small bodies are reviewable diffs; a large one still gives a failure something to diff  | everything in git; a sha256 in place of the body                                |
| One dataset in CI, cases bound their own size     | no new packaging tooling, no recapture of the `reproduce` goldens                       | a CI slice as the first choice (kept as the fallback)                           |
| No v1 golden exercises a deliberate deviation     | ADR-0013; both replays compare with the same captured files, the comparator stays `cmp` | a comparator rule per deviation; a golden that selects a `COUNT`                |
| One v2 case per request, formats inside           | the same query answers the same data in every format and coding; one status, one CSV    | a directory per (query, format); a twin naming another case's body              |
| The suites are `v1` and `v2`                      | the names of the API versions they pin; `legacy` named one of them after its history    | `legacy` as a suite, target, driver, fixture or package name                    |
