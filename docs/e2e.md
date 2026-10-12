# End-to-End Test Harness -- Specification

Status: approved. This document specifies the permanent e2e harness
described in the brief's Testing doctrine (items 2 and 3): the v2 round trip,
the v1 goldens and stress against a live qdbd. It is a permanent
specification (`docs/AGENTS.md`, Specifications). Verified facts and
dates are recorded here, not in the brief. Progress is recorded in
`docs/log.md`, not here. Decisions are in the dated decision logs at
the end.

## Purpose

The harness proves, for the life of the product, that the built binary,
driven over HTTP like a client:

1. works as a client expects on the v2 surface: login, create tables,
   ingest rows in every body format, read and query them back in every
   format and content coding (the v2 round trip, ADR-0014);
2. returns exactly what the old server returned on the v1 surface (the
   v1 goldens, ADR-0013);
3. behaves honestly under stress: fast, explicit failure under overload,
   in-flight streams survive graceful shutdown.

Every assertion is pass or fail. The harness measures nothing. Time to
first byte, memory and throughput belong to the bench (ADR-0013,
`docs/bench.md`), which lives separately in `tests/e2e/bench/` and
consumes this harness's services and dataset.

The harness is Make + shell + curl + awk (the qdb-nats-connector ADR-007
lineage) plus one Go tool, and contains no Python. It runs
identically on developer machines and in Buildkite on every platform
(see "In Buildkite").

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
  stops, through pidfile helpers copied from nats-connector's
  `common.sh`. Every server the harness starts, and every golden
  capture, runs under `TZ=UTC`. The v1 JSON encodes timestamps in the
  server's local time zone, so goldens are portable only with the zone
  pinned.

## Dataset

The canonical dataset is the table `reproduce`, **5,613,032 rows**, 14
columns (strings, int64, double, timestamps, real null distribution),
~834 MB as v1 JSON. It originates from the sc-19522 customer
memory-optimization case.

The distribution format is **CSV plus a `qdb_import` config**, produced once by
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
`archives[]` of `{name, date, rows, sha256}`, where the sha256 is of the
archive and is verified after download), fetched with plain `curl -fL`
and no auth. The developer and the CI agent obtain the data identically.
`make load DATASETS_LOCAL_DIR=<dir>` takes the archive from a local
directory instead, for a freshly packaged archive that is not uploaded
yet.

The archive is produced by `tests/e2e/tools/package-dataset.sh` (`make
package-dataset SRC=<db.tar.zst> OUT=<dir>`). It starts a throwaway qdbd on
the extracted data directory (port 2846, never the shared service), reads
the shard size from `SHOW TABLE`, exports with `qdb_export`, injects
`shard_size` into the import config, writes `metadata.json`, and prints
the `datasets.json` entry and the `aws s3 cp` command (upload is a manual
operator step). The export runs one shard-sized range at a time
(`common.sh::export_table_csv` says why).

Loading is a test-owned, idempotent step. `make load` downloads the
archive, verifies the sha256, and runs `qdb_import -f reproduce.csv
--config reproduce.import.json -j <n>` against the running qdbd. It is
skipped when `SELECT COUNT(*) FROM "reproduce"` already reports
5,613,032. Query
workloads are read-only, so the loaded table persists for the qdbd
lifetime.

Round-trip fidelity (nulls, nanosecond timestamps, quoting) is verified
by `make verify-dataset`. The loaded table is exported again and compared
byte for byte with the CSV it was imported from (verified identical
2026-08-19). The bench's
`native@qdbd` fingerprint against the original data directory remains the
belt-and-braces check. A mismatch is a `qdb_export` or `qdb_import` bug
worth surfacing, not a harness problem.

The dataset has two readers: the v1 suite (goldens 06 and 16 select
`reproduce LIMIT 10`) and the bench. The v2 round trip reads neither the
dataset nor `seed.sql`. It makes its own rows. CI loads the same
archive developers do, once the v1 suite enters the build step. Cases
bound their own result size with `IN RANGE` or `LIMIT`, and no case
selects the whole table, because a full-table response is the bench's
business. That first CI run verifies the wall-clock time of `make load`
on the slowest agent. The fallback, if it is unacceptable, is a second,
smaller archive of whole shards loaded under its own table name, with
the two `reproduce` v1 goldens recaptured against it.

## The v2 round trip

There is one round trip and there are no captured files (ADR-0014). It
is the sequence of `TestRoundtrip` (`internal/httpapi/roundtrip_test.go`)
driven over HTTP by curl against the built binary. The expected value
of every step is known by construction, so nothing is audited and
nothing lives under `tests/e2e/golden/v2/`. The round trip runs once
per cluster, `insecure` (`qdb://127.0.0.1:2836`) and `secure` (`:2838`),
each against its own server under test. The secure login body is the
user security file `start-services.sh` writes at the repo root
(`user_private.key`, the file `internal/qdbtest` reads too), and the
insecure login is anonymous. Every assertion is pass or fail. Nothing
is timed.

1. `POST /api/v2/auth/login`: 200 and the RFC 6749 shape, with
   `access_token` a non-empty string, `token_type` `Bearer` and
   `expires_in` a positive integer (ADR-0011). The token authenticates
   every later step.
2. `POST /api/v2/tables`: one table per body format, `e2e_csv`,
   `e2e_ndjson`, `e2e_arrow` and `e2e_json`, with one column of every
   type (`blob`, `double`, `int64`, `string`, `symbol`, `timestamp`),
   dropped first so the round trip is idempotent.
3. `POST /api/v2/rows`: the generated rows, whose `$table` column names
   the table, as CSV into `e2e_csv`, as NDJSON into `e2e_ndjson`, as
   Arrow IPC into `e2e_arrow` and as columnar JSON into `e2e_json`,
   each answered 200 with the row count. The tool writes every body
   from the same batch, `$table` included, so the four bodies differ in
   format only. A body carrying several tables is the Go property
   test's case, not the round trip's.
4. Each table is read through `GET /api/v2/tables/{name}/rows` in every
   format (`csv`, `ndjson`, `arrow`, `json`) under `identity` and
   `gzip`. The CSV response is compared byte for byte with the
   generated CSV. A JSON, NDJSON or Arrow response is decoded to CSV by
   the tool and compared with the same file. A gzip response is
   decompressed first. `content-type` is asserted from the format and
   `content-encoding` from the coding (absent for `identity`). Nothing
   else in the headers is read. The plain run sends no
   `Accept-Encoding`, and the gzip run sends `Accept-Encoding: gzip`.
   The table reader answers `$table` and `$timestamp` first, then the
   data columns, and the generated CSV has the same header, so the
   comparison is byte for byte with no reordering.
5. Each table is queried through `POST /api/v2/query` with
   `SELECT * FROM <name>` in every format under both codings, with the
   assertions of step 4, except that every response, the CSV one
   included, is decoded to CSV by the tool. A bare `SELECT *` answers
   `$timestamp`, then `$table`, then the columns in creation order,
   and the rows ascending by `$timestamp`, so the query's CSV is not
   the generated bytes. The decoders put `$timestamp` first and drop
   `$table`, which the tool puts back in front, and that one step is
   the whole reorder.

The round trip tests nothing else. Each of the following has a Go test
and no step here: the empty read of a table in every format
(`TestRoundtrip`, step 3), zstd on the response wire
(`internal/httpapi/compress_test.go`, because the agents carry `gunzip`
and no zstd binary, see "In Buildkite"), request-body codings and push
modes (`TestRoundtrip`, step 4), deduplication
(`TestRoundtripDeduplicated`), several tables in one body
(`internal/encoding/decode_test.go`), and the error rows of ADR-0010
and ADR-0011 (`internal/httpapi`). Cross-format equivalence over
generated schemas stays with the Go property tests. The round trip
proves the binary over HTTP with one schema.

The generated rows are the CSV encoder's own bytes (`encoding/csv` RFC
4180, header row, LF), pinned without a cluster in
`internal/encoding/encode_test.go`, and the other three formats are
proven equal to them. One fact of the tree bounds the data (verified
2026-09-18). CSV writes null and the empty string alike, so the
generator emits no empty string and the ingest decoder reads an empty
CSV field as null.

### The tool

`tests/e2e/tools/e2etool`, built by the Makefile with the server's
toolchain and environment into the repository's `bin/`, next to
`qdb_rest`. It may import any package of this
repository, the cgo binding included (ADR-0013, Consequences). It
formats and parses no cell of its own: every file it writes comes out
of an encoder of `internal/encoding`, and every response it reads goes
through a decoder of the same package.

- `e2etool gen --rows N --seed S --out DIR --table NAME`: draws one
  batch, `$table` naming the table first, then `$timestamp` strictly
  ascending, then one column of every type, with the edge rows (the
  extreme integers, the awkward string with `"`, `,`, `<&>` and an LF,
  nulls, nanosecond timestamps) ahead of the drawn ones, and writes it
  as `rows.csv`, `rows.ndjson`, `rows.arrow` and `rows.json` through
  the package's encoders. NaN is excluded, since it encodes as null and
  would not round-trip. It prints the row count, edge rows included,
  which the driver asserts the ingest's answer against. The driver
  prints the seed so a failure reproduces.
- `e2etool tocsv --format csv|json|ndjson|arrow --table NAME`: stdin to
  CSV on stdout. The body is decoded through the package's decoder of that
  format, typed through the tool's own schema of the table with
  `$table` in front, the same shape the decoders' tests answer. The
  decoder drops `$table`, so the tool puts a `$table` column of the
  name back in front, and encodes through the package's CSV encoder.
  The JSON it reads is the one result the query answers or the array
  of results the table reader answers.

### The driver

`tests/e2e/round-trip.sh run --insecure-url <url> --secure-url <url>
--tool <e2etool> [--rows N] [--seed S]` is a small option loop. The
Makefile is the only source of URLs, ports and paths.
`make test-round-trip` runs it. A cluster whose URL is empty gets a
server started from `QDB_REST_BIN` (`40090` on the insecure node,
`40091` on the secure node with `--cluster-public-key-file` and
`--cluster-user-security-file`), with the TLS listener off and
`TZ=UTC`. Working files go to `actual/round-trip/<cluster>/`, one
`.body` and one `.headers` file per request. The driver is one file,
like `golden.sh`: generic request and status helpers, one function per
step kind (login, create, ingest, read back, query back) that takes the
table, the format and the coding as arguments, one `check_response`
that the read and the query share for the header assertions, the
decoding and the comparison, and a matrix of body formats, response
formats and codings that `run_cluster` iterates. A format is one list
entry, and a new step is one function called inside the loop. The
round trip stays in shell and curl, because the question it answers is
whether the built binary works for a client over HTTP, and curl is
that client; a Go client would repeat `TestRoundtrip` in
`internal/httpapi`.

An endpoint lands with its step in the round trip, so the round trip
grows with the milestones (`docs/brief.md`, Milestones). Refresh and
session join the login step, and the exploration endpoints and
`/api/v2/sql` add steps. Flight SQL has no shell client. Whether it
gets a subcommand of the tool or stays with the property tests is
decided at that milestone's entry.

## The v1 goldens

A golden is an audited expected response: a run somebody looked at,
judged correct, and committed. Every later run is compared with it byte
for byte (ADR-0013). There is no canonicalization and no tolerance. The
old server's text output is deterministic on every platform, QuasarDB
returns rows in a stable order, and an unexpected byte is a bug worth
seeing. Capture never runs in CI. A golden changes only in a commit
whose diff the owner reviews.

A case is a directory `tests/e2e/golden/v1/<NN-slug>/` with a
hand-written `request.json` (method, path, pre-encoded query string,
headers, body, auth mode, compare mode) next to the expected `status`,
`headers` (only the headers that belong to a contract, lowercased and
sorted, with absence recorded as absence) and `body`. The driver,
`golden.sh`, captures and replays the suite. The compare modes are
`bytes`, `gunzip` (the decompressed bytes) and `login-shape`, which
exists because a login answers a token that differs per call
(`{"token": <non-empty string>}`).

### The suite

Byte-shape equivalence of the v1 endpoints (`/api/login`, `/api/query`)
uses small golden request/response pairs captured from
the old server under `tests/e2e/golden/v1/<NN-slug>/`. Each pair is a
hand-written `request.json` (method, path, pre-encoded query string,
headers, JSON body, auth mode `none|bearer|urlparam`, compare mode
`bytes|gunzip|login-shape`) next to the captured `status`, `headers`
(only `content-type` and `content-encoding`, lowercased and sorted, with
absence recorded as absence) and `body` (raw bytes, decompressed for
`gunzip`). `login-shape` checks `{"token": <non-empty string>}` because
tokens vary per call. The driver's `capture|replay` modes drive both
sides. `make capture-v1` is an operator step, `make test-v1` replays
against the server under test, and `make test-v1-selfcheck` replays
against the old server to prove the goldens are deterministic.
Both replays compare with the same files. The drop-in milestone's exit
additionally replays every login and query golden at its
`/api/v1/<path>` spelling against the server under test (ADR-0008).
No golden covers a status probe, because the probes are outside the
compatibility contract (`docs/brief.md`, Observability and logging).
Full-table golden responses are deliberately not captured (834 MB of
JSON is not a fixture).

Goldens are captured from the old server **built from `master`** in a
worktree (`make old-server`), linked against this repo's `qdb/` tree,
the same C API the server under test uses, so any difference is the REST
layer's. `master`'s wire-shape code is byte-identical to the released
`v3.14.2` (verified 2026-08-19), so the goldens stand for the deployed
server.

The fixture for the goldens (`make seed`, `tests/e2e/seed.sql`,
idempotent) holds the nine tagged tables from old master's rest-setup (`foo/bar/baz_01..03`,
tags `tag_01..03` on `$qdb.tagroot`), `seed_types` (blob, int64,
double, string, symbol, timestamp, with one full, one all-null and one mixed row
with a nanosecond timestamp and `"`, `,`, `<&>` in a string) and
`seed_allnull` (which pins `"type":"none"`). `reproduce` supplies
`LIMIT 10`.

The goldens and the contract (`docs/brief.md`, Compatibility contract)
meet in two places:

- Null cells are JSON `null` in every golden. The old server's sentinel
  strings do not appear, because the C API types every null cell
  `qdb_query_result_none` (verified 2026-08-19 over raw selects,
  `IN RANGE`, `GROUP BY`, aggregates and arithmetic on nulls).
- No golden exercises a deliberate deviation (ADR-0013, and the list is
  `docs/brief.md`, Compatibility contract, "Deliberate deviations"). A
  `COUNT(...)` column is the one a query could reach, since the old
  server types it `count` and v1 types it `int64`, so no golden query
  selects a `COUNT`.

The byte-shape facts the goldens pin (verified 2026-09-02 from the old
server's models and producers on `master`) are listed below. The v1
wrappers in `internal/httpapi/v1` reproduce them (ADR-0007).

- Key order and omission follow the old models' struct order. Column
  objects serialize `data`, `name`, `type` (`name` and `type`
  omitempty), and table objects serialize `columns` then `name` (`name`
  omitempty, `columns` never omitted). A query-result table carries no
  name, a tag-find table carries `"columns":null`, and an empty result
  is `{"tables":[]}`, never `null`.
- 200/400/500 bodies end with a newline and leave HTML unescaped
  (`SetEscapeHTML(false)`, which the seeded `<&>` string pins). 401
  bodies have no trailing newline and use `{"code":401,"message":"..."}`
  in that key order.
- The 401 messages are `unauthenticated for invalid credentials` for no
  token, `Invalid authentication token` for an unverifiable token, and
  `Token has expired. Please login again` for an expired token.
- Query and find execution errors are 400 `{"message":...}`. The
  message is the binding's error, a space, and the query result's
  `ErrorMessage()` (golden: `query_execute (operation=query_execute,
query=SELECT FROM): The provided query is invalid. expected FROM`).
  The binding's `wrapError` message is identical between the old
  server's vendored binding and ours.
- The find wart executes the raw find expression through the binding's
  `Find().ExecuteString` (`qdb_query_find`). `qdb_get_tagged` plays no
  part in it.
- Auth extraction: the `Authorization` header's `Bearer ` prefix is
  optional (a bare token authenticates), and the header wins over the
  `?token=` parameter.
- gzip: a request whose `Accept-Encoding` contains the substring `gzip`
  gets a gzipped body plus `content-encoding: gzip`, on every route.
  Everything else is identity.

There are two compatibility layers, deliberately. This harness checks
**byte-shape** (golden pairs, permanent, in Buildkite, where a replay
needs the committed goldens and not the old server). The temporary,
local bench checks **semantic** compatibility through a real client:
the same v1-protocol Python code is run against the old and the new
server and compared by normalized DataFrame fingerprint
(`v1@old-rest == v1@new-rest` in `docs/bench.md`).

## In Buildkite

`scripts/cicd/40.test-e2e.sh` runs after the Go tests in every
platform's build step, against the qdbd `start-services.sh` started and
the binary `20.build.sh` built. It runs `make test-round-trip` first,
which loads nothing. `make load seed test-v1` joins when the wrappers
land, and the stress joins with the resilience milestone. A suite enters
the step when it is green. The script follows qdb-nats-connector's
`50.test-e2e.sh`: GNU make discovery (`gmake` on FreeBSD,
`mingw32-make` on Windows), the Windows DLL co-location into `bin/`,
where `qdb_rest` and `e2etool` are both built, and a dump of the two
REST servers' and both qdbd clusters' logs on failure. The REST servers
are stopped by the harness Makefile's exit trap, not by the Buildkite
hook. `curl`, `jq` and `gunzip` are what that pipeline already requires
on the same agents.

## Stress definition

The stress is shell and curl, asserted as behaviour and never as a
timing or a measured number (ADR-0013). N parallel clients (`xargs -P`
and curl) run against a large `reproduce` range. The assertions are
that the session budget bounds load (a request past the budget waits
for a session or times out at its deadline, and every admitted request
completes with the golden body), and that in-flight streams complete
across a graceful shutdown drain.

## Layout

```
scripts/tests/setup/      qdb-test-setup git submodule (start-services.sh, ...); qdbd up
tests/e2e/
  datasets.json           base_url + archives[{name,date,rows,sha256}]
  Makefile                services-check | download-golden | extract | load | verify-dataset |
                          seed | package-dataset | old-server | capture-v1 |
                          test-v1 | test-v1-selfcheck | clean | distclean
                          e2etool | test-round-trip
                          test-stress (arrives with the resilience milestone)
  common.sh               helpers: log_*, pidfile/start_server/stop_server, qdbsh wrapper,
                          count_qdb_rows, export_table_csv (chunked), sha256_file
  golden.sh               v1 golden capture/replay driver
  round-trip.sh                 the v2 round trip driver
  seed.sql                v1 golden fixture (qdbsh statements)
  tools/package-dataset.sh  db.tar.zst -> dataset archive (operator)
  tools/e2etool/          Go: gen and tocsv;
                          gen writes the four body files, tocsv decodes a response to CSV
  golden/v1/              v1 request/response pairs, captured from the old server
  .old-master/            git worktree of master for the old server (gitignored)
  bench/                  temporary multi-target comparison (docs/bench.md)
  AGENTS.md, README.md    conventions, usage
```

The bench's `make old-server` delegates to this Makefile's target. There
is one recipe for building the old server.

## Lineage

`scripts/tests/setup/` is qdb-nats-connector's test setup as a
submodule. `datasets.json`, the `download-golden` and `extract` recipes
and the `common.sh` helpers are copies from that repository, adapted.
Its synthetic-message generator and NATS loader are not used. The
loader here is `qdb_import` (and `POST /api/v2/rows` as a self-test once
it exists), because the customer-derived table, with real nulls, real
string cardinality and real skew, exercises encoders in ways synthetic
data hides. Schema variety (multi-table ingest, symbols, blobs, tags) comes
from the Go `rapid` property tests, generated in-process.

## Decision log (2026-08-16)

| Decision                                             | Why                                                                                             | Rejected                                                                     |
| ---------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Permanent e2e separate from temporary bench          | different lifetimes, and the bench dies once new-rest beats old-rest                            | one combined assessment framework, shared abstractions up front              |
| qdbd via shared `start-services.sh`                  | ADR-007 service model, one owner for qdbd flags and license                                     | bench-private `start-qdbd.sh` / `stop-all.sh`                                |
| Dataset as CSV + import config, loaded by qdb_import | a shared qdbd means the front door is the only way in, and the CSV doubles as an ingest fixture | qdbd data-directory tarball (`db.tar.zst`), with a fresh extract per launch  |
| S3, nats-connector style, `datasets.json` + curl     | developer and CI fetch identically, and it is proven                                            | `.buildkite/tools/artifacts.py` (per-build ephemeral layout), an env-var URI |
| Idempotent `make load` behind a COUNT(*) check       | data is a test resource, not an operator chore                                                  | manual pre-loading, per-run loading                                          |
| Stress in shell/curl/awk                             | keeps the permanent path free of Python and heavyweight builds                                  | Python or the bench harness in CI                                            |
| Copy nats helpers, not its generator/loader          | the helpers fit, and the generator and loader have the wrong shape and sink                     | nats YAML generator as fixture source                                        |

## Decision log (2026-08-19)

| Decision                                        | Why                                                                                                                                                                    | Rejected                                                       |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| qdb-test-setup as a git submodule, not a copy   | one owner for qdbd flags and license, the same as nats-connector and old master, updates by SHA                                                                        | copying the scripts (drift, duplicated owner)                  |
| Goldens from the old server built from `master` | the wire code equals v3.14.2 by diff and uses the same C API as the server under test, while `3.14.x` needs an old qdb-api-go checkout and would pair different C APIs | the released 3.14.2 binary, a `3.14.x` source build            |
| `TZ=UTC` pinned by the harness                  | v1 timestamps are local time, and goldens must be machine-portable                                                                                                     | capture in host zone                                           |
| Seeded fixture plus `reproduce`                 | controllable types, nulls and tags, plus real data for the head of a real table                                                                                        | `reproduce` only (no blob, no tags, no null control)           |
| `sha256` per archive in `datasets.json`         | the brief says sha256-pinned, while nats relies on dated names only                                                                                                    | dated filename alone                                           |
| Chunked `qdb_export` in the harness             | the client input buffer caps a single-shot export, and chunks are byte-identical                                                                                       | raising the buffer (there is no flag), a different export tool |

## Decision log (2026-08-20)

| Decision                                     | Why                                                                                     | Rejected                                                 |
| -------------------------------------------- | --------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| Lazy login + `CASES=` selection in golden.sh | auth-free cases replay against a server without `/api/login`, and single-case debugging | eager login (couples every replay to the login endpoint) |

## Decision log (2026-09-12)

| Decision                                    | Why                                                                  | Rejected                                          |
| ------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------- |
| No full-table `text/csv` equivalence target | not a target, and cross-format correctness is the Go property test's | the awk tolerance comparator over `reproduce.csv` |

## Decision log (2026-09-17)

| Decision                                               | Why                                                                                                | Rejected                                                                        |
| ------------------------------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| The harness measures nothing and budgets leave         |
| ADR-0013: a number is the bench's, CI gates are yes/no | `test-budgets`, `budgets.env`, TTFB and RSS gates                                                  |
| Arrow compared decoded, against the CSV golden         | ADR-0013, since there are no stable bytes and the audited CSV golden already says what the data is | status and `content-type` only, a separate Arrow golden, pyarrow in the harness |
| One dataset in CI, cases bound their own size          | no new packaging tooling, no recapture of the `reproduce` goldens                                  | a CI slice as the first choice (kept as the fallback)                           |
| No v1 golden exercises a deliberate deviation          | ADR-0013, both replays compare with the same captured files, and the comparator stays `cmp`        | a comparator rule per deviation, a golden that selects a `COUNT`                |
| The suites are `v1` and `v2`                           | the names of the API versions they pin, while `legacy` named one of them after its history         | `legacy` as a suite, target, driver, fixture or package name                    |

The v2 golden suite this table also shaped (one driver for both suites,
expected bodies in the archive, one case per request) was replaced by
the round trip on 2026-09-18 (ADR-0014). Those rows are gone with it.

## Decision log (2026-09-18)

| Decision                                                 | Why                                                                                    | Rejected                                                  |
| -------------------------------------------------------- | -------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| The v2 e2e is one round trip, generated data, no goldens | proves the basic round trip with little code and no audit liability (ADR-0014)         | ten captured cases and an operator capture cycle          |
| Error rows are Go tests only                             | the e2e proves the round trip, not the surface, and the tables are pinned in `httpapi` | 401/413/415/400 goldens                                   |
| Ingest bodies: CSV, NDJSON and Arrow IPC                 | the round trip ingests the same rows three ways, and the property tests come with them | CSV only, the rest in the ingestion milestone             |
| One Go tool decodes every format to CSV                  | ADR-0013 5's Arrow rule for every text format, one expected file                       | an encoder per format in shell, row-count checks for JSON |
| The Go tool generates the rows                           | rapid-style generation shares the vocabulary of the property tests                     | an awk generator, a checked-in fixture CSV                |
| The round trip runs on both clusters                     | the same round trip on two parallel environments, and only the login differs           | insecure only                                             |
| A driver with named options                              | two URLs on a command line need names                                                  | positional URLs, environment variables                    |
| The plain run sends no `Accept-Encoding`                 | what a plain client sends, and the explicit `identity` rule is a Go test               | `Accept-Encoding: identity`                               |

## Decision log (2026-10-11)

| Decision                               | Why                                                                                      | Rejected                                                                  |
| -------------------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| The ingest takes columnar JSON         | every read format is an ingest format, and the decoder set covers every response         | columnar JSON as output only, a tool-local JSON reader                    |
| The first e2e unit is one basic test   | one small step on the built binary before the surface is defined                         | the whole round trip in one unit                                          |
| The surface is defined with the owner  | what is useful to test is the owner's call, not derivable from the documents             | the agent cutting the remaining steps alone                               |
| The tool reuses the package's encoders | no duplicated cell formatting, and the encoders are pinned byte for byte without cluster | a tool-local CSV writer so the expected file is untouched by the encoders |
| The tool stays Go                      | ADR-0013's platform reasons stand, and Flight SQL comes with arrow-go's client           | a Python client, the Python API as the e2e client                         |
| Ten thousand rows by default           | owner's preference                                                                       | one thousand                                                              |

## Decision log (2026-10-12)

| Decision                                                                                  | Why                                                                                                                                     | Rejected                                                                                                                                                                             |
| ----------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| The v2 e2e is called the round trip                                                       | the Go tests call this sequence a round trip (`TestRoundtrip`, `TestDecodeRoundTrip`), and the e2e is that round trip over HTTP (owner) | "flow", because it named nothing the project uses elsewhere                                                                                                                          |
| ADR-0014 keeps its file name under a reworded title                                       | ADR file names are stable identifiers linked from `docs/adr/README.md`, and dated log entries are append-only (owner)                   | renaming the ADR file, because every link would move for a word                                                                                                                      |
| The query endpoint is in the round trip, as `SELECT *`                                    | the owner wants the query path driven over HTTP too                                                                                     | the table reader alone, because `TestRoundtrip` queries in process                                                                                                                   |
| The empty read is dropped                                                                 | `TestRoundtrip` step 3 reads the first table empty in every format (owner)                                                              | an empty read against the header line, because it costs a second expected value and a tocsv path for zero batches                                                                    |
| Codings are identity and gzip on the response wire                                        | the agents carry `gunzip` and no zstd binary, and zstd is `compress_test.go`'s (owner)                                                  | zstd, because it needs a binary on eight agents                                                                                                                                      |
| Request-body codings, push and deduplication modes, several tables per body stay Go tests | `TestRoundtrip` draws them and `TestRoundtripDeduplicated` covers deduplication (owner)                                                 | steps in the round trip, because each would repeat a drawn case with no new question                                                                                                 |
| tocsv types the response through the round trip schema it holds, named by `--table`       | the decoders need a `SchemaOf`, and the tool has the schema already, with the binding's types the reader answers (owner)                | a `GET /api/v2/tables/{name}` request, because the endpoint is M7's and asking the server for the schema it must prove would be circular                                             |
| tocsv restores `$table` through `table.WithTable`                                         | the decoders drop `$table`, and the generated CSV and every response carry it first                                                     | stripping `$table` from the expected file, because the expected value stays one file; a tool-local column prepend, because the fixture has it                                        |
| Headers are asserted from a `curl -D` dump                                                | `golden.sh` captures headers the same way, and `-w` formats cannot say a header is absent                                               | `curl -w '%{content_type}'`, because it cannot assert an absent `content-encoding`                                                                                                   |
| gen writes the four body files in one run                                                 | the bodies are one batch through four encoders, and one run keeps one draw per table                                                    | one file per run under `--format`, because four draws of the same seed are four times the work for the same batch                                                                    |
| gen runs once per table                                                                   | `$table` is a column of every body and names the table the row goes to                                                                  | one run with a `$table` rewritten per table, because the tool would then format a cell                                                                                               |
| e2etool is built into the repository's `bin/`                                             | one directory for both cgo binaries makes the CI step's Windows DLL co-location one copy (owner)                                        | overriding `E2ETOOL` on the CI command line, because it hides the layout in the step; DLLs copied into the tool's source directory, because they would sit untracked in the checkout |
| `40.test-e2e.sh` calls the harness Makefile through GNU make                              | the Makefile is the harness's only entry point and owns the server lifecycle                                                            | driving `round-trip.sh` from the step, because it duplicates the Makefile's start, trap and stop                                                                                     |
| The Makefile's exit trap is the one stop of the REST servers                              | the lifecycle is small and known, and start and stop stay together (owner)                                                              | stopping by pidfile from `hooks/pre-exit`, because it splits the lifecycle across two files for the case of a step killed by its timeout                                             |
| The round trip runs inside the build step, after the Go tests                             | ADR-0013 6 places it there, and a separate step would rebuild and restart qdbd                                                          | a separate pipeline step                                                                                                                                                             |
