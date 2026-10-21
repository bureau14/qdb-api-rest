# End-to-End Test Harness -- Specification

Status: approved. This document specifies the permanent e2e harness
described in the brief's Testing doctrine (items 2 and 3): the v2 round
trip, the Flight SQL smoke tests and the stress against a live
qdbd. It is a permanent
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
2. answers the stock Flight SQL clients: Arrow's ADBC and JDBC drivers
   each log in and query both engines (the Flight SQL smoke tests,
   ADR-0018);
3. behaves honestly under stress: fast, explicit failure under overload,
   in-flight streams survive graceful shutdown.

Every assertion is pass or fail. The harness measures nothing
(ADR-0015). The v1 golden suite of the drop-in milestone joins this
harness when that milestone starts (`docs/brief.md`, Milestones).

The harness is Make + shell + curl + awk (the qdb-nats-connector ADR-007
lineage) plus one Go tool and one Java source file for the JDBC smoke
test. The Go tool also drives the ADBC smoke test. It runs
identically on developer machines and in Buildkite on every platform
(see "In Buildkite").

## Service model

qdbd is a persistent background service, never started by a test:

- `scripts/tests/setup/start-services.sh` (the shared qdb-test-setup used
  by qdb-nats-connector, qdb-api-python, qdb-api-go, as a git submodule of
  `bureau14/qdb-test-setup` pinned by SHA, never edited locally) starts
  qdbd insecure on `127.0.0.1:2836` and secure on `2838`, with qdbd flags,
  license and cluster keys owned by that one script. It force-restarts
  qdbd and wipes the data directories.
- Tests assume the service is up and fail fast with a clear message if
  the port does not answer. Service failures are infrastructure issues,
  not test concerns.
- The REST server under test is the only process the harness starts and
  stops, through pidfile helpers copied from nats-connector's
  `common.sh`. Every server the harness starts runs under `TZ=UTC`, so
  a response that carries a local-time timestamp is portable across
  machines.

## The v2 round trip

There is one round trip and there are no captured files (ADR-0014). It
is the sequence of `TestRoundtrip` (`internal/httpapi/roundtrip_test.go`)
driven over HTTP by curl against the built binary. The expected value
of every step is known by construction, so nothing is audited and
nothing is captured. The round trip runs once
per cluster, `insecure` (`qdb://127.0.0.1:2836`) and `secure` (`:2838`),
each against its own server under test. The secure login body is the
user security file `start-services.sh` writes at the repo root
(`user_private.key`, the file `internal/qdbtest` reads too), and the
insecure login is anonymous. Every assertion is pass or fail. Nothing
is timed.

1. `POST /api/v2/auth/login`: 200 and the RFC 6749 shape, with
   `access_token` and `refresh_token` non-empty strings, `token_type`
   `Bearer` and `expires_in` a positive integer (ADR-0011, ADR-0016).
   `POST /api/v2/auth/refresh` with the refresh token: 200 and the same
   shape. `GET /api/v2/session` under the refreshed access token: 200
   and `username` equal to the login's. The refreshed access token
   authenticates every later step. Logout has no step, because it
   changes nothing a later step could observe.
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
6. Each table is read through `POST /api/v2/query` with
   `#sql SELECT '<name>' AS "$table", * FROM qdb.<name> ORDER BY "$timestamp"`
   in every format under both codings, with the assertions of step 5.
   qdb-duck's scan answers no `$table` column, and the tool routes rows
   by it, so the select names the table as a constant.

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
repository, the cgo binding included (ADR-0015). It
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
  The JSON it reads is the array of results every route answers or the
  one result `Encode` writes.

### The driver

`tests/e2e/round-trip.sh run --insecure-url <url> --secure-url <url>
--tool <e2etool> [--rows N] [--seed S]` is a small option loop. The
Makefile is the only source of URLs, ports and paths.
`make test-round-trip` runs it. A cluster whose URL is empty gets a
server started from `QDB_REST_BIN` (`40090` on the insecure node,
`40091` on the secure node with `--cluster-public-key-file` and
`--cluster-user-security-file`), with the HTTPS listener on `40092`
and `40093` and the Flight listener on `40094` and `40095`, because
neither can be turned off and the two servers must not collide, and
`TZ=UTC`. Working files go to `actual/round-trip/<cluster>/`, one
`.body` and one `.headers` file per request. The driver is one file:
generic request and status helpers, one function per
step kind (login, create, ingest, read back, query back, sql back) that
takes the table, the format and the coding as arguments, one
`check_response` that the read, the query and the sql read share for the header assertions, the
decoding and the comparison, and a matrix of body formats, response
formats and codings that `run_cluster` iterates. A format is one list
entry, and a new step is one function called inside the loop. The
round trip stays in shell and curl, because the question it answers is
whether the built binary works for a client over HTTP, and curl is
that client; a Go client would repeat `TestRoundtrip` in
`internal/httpapi`.

An endpoint lands with its step in the round trip, so the round trip
grows with the milestones (`docs/brief.md`, Milestones). Refresh and
session join the login step, and the exploration endpoints add steps.
Flight SQL has no step in the round trip. Its tests are the Go property
tests, driven by arrow-go's Flight SQL client, and the two smoke tests
below (ADR-0018).

## The Flight SQL smoke tests

Arrow publishes two official Flight SQL drivers, and customers are
likely to reach the Flight listener through both. DataFrame frameworks
such as pandas and polars connect through ADBC, and Java programs
through JDBC. The two drivers share no client code. Each sends its own
handshake, auth metadata and connect-time metadata calls, so each gets
its own smoke test against the built binary. ADBC is tested in Go and
JDBC in Java. The smoke tests do not repeat the format-equivalence
property. Through its driver, each test:

1. connects to `listen.flight`, with a username and secret on the
   secure cluster, which exercises Handshake and the bearer, and as the
   anonymous user on the insecure cluster;
2. runs one native query and reads its rows;
3. runs one `#sql` query and reads its rows, with at least one column of
   a DuckDB type that QuasarDB does not have.

The ADBC test is an `e2etool` subcommand that drives arrow-adbc's Go
Flight SQL driver. The Python and C packages of that driver bind the
same Go code (the `adbc_driver_flightsql` README, 2026-10-19), so the
Go test runs the driver logic that pandas and polars load. arrow-adbc's
Go module is pinned in `go.mod` and vendored. It and the server use
one arrow-go release (v18.8.0 on both sides, 2026-10-19), and one is
bumped only together with the other. The test connects with
certificate verification disabled.

The JDBC test is one Java source file that `java` runs directly, with
no build tool, because JDBC is a Java API and a Java program uses the
driver as Java customers do. The Makefile downloads a pinned version of
the driver jar (`org.apache.arrow:flight-sql-jdbc-driver`) from Maven
Central on first use, checks its SHA-256 and caches it outside the
tree. The server serves the Flight listener on its ephemeral
self-signed certificate, and the test connects with
`disableCertificateVerification=true`.

Both tests run on both clusters and on all eight platforms. The ADBC
test is Go and runs wherever the server builds. Every agent already
builds the QuasarDB Java API, which uses JNI, so every agent has a JVM.
FreeBSD may drop out of the JDBC test if the driver does not run there,
which is unverified.

## In Buildkite

`scripts/cicd/40.test-e2e.sh` runs after the Go tests in every
platform's build step, against the qdbd `start-services.sh` started and
the binary `20.build.sh` built. It runs `make test-round-trip`, which
loads nothing. The stress joins with the resilience milestone, and the
v1 goldens with the drop-in milestone. A suite enters the step when it
is green. The script follows qdb-nats-connector's
`50.test-e2e.sh`: GNU make discovery (`gmake` on FreeBSD,
`mingw32-make` on Windows), the Windows DLL co-location into `bin/`,
where `qdb_rest` and `e2etool` are both built, and a dump of the two
REST servers' and both qdbd clusters' logs on failure. `curl`, `jq` and
`gunzip` are what that pipeline already requires on the same agents.

## Stress definition

The stress is shell and curl, asserted as behaviour and never as a
timing or a measured number (ADR-0015). N parallel clients (`xargs -P`
and curl) read a large table the stress ingests itself through
`e2etool gen` and `POST /api/v2/rows`, the way the round trip does. The
assertions are that the session budget bounds load (a request past the
budget waits for a session or times out at its deadline, and every
admitted request completes with the expected body), and that in-flight
streams complete across a graceful shutdown drain. Its definition is
completed when the resilience milestone starts.

## Layout

```
scripts/tests/setup/      qdb-test-setup git submodule (start-services.sh, ...); qdbd up
tests/e2e/
  Makefile                services-check | e2etool | test-round-trip | clean
                          test-stress (arrives with the resilience milestone)
  common.sh               helpers: log_*, pidfile/start_server/stop_server, qdbsh wrapper
  round-trip.sh           the v2 round trip driver
  tools/e2etool/          Go: gen and tocsv;
                          gen writes the four body files, tocsv decodes a response to CSV
  AGENTS.md, README.md    conventions, usage
```

## Lineage

`scripts/tests/setup/` is qdb-nats-connector's test setup as a
submodule, and the `common.sh` helpers are copies from that repository,
adapted. Its synthetic-message generator, its NATS loader and its
golden datasets are not used: every row the harness needs is drawn by
`e2etool gen` through the table fixture of the Go property tests, so
schema variety (every column type, nulls, awkward strings) comes from
the same generator in-process and over HTTP.

## Decision log (2026-08-16)

| Decision                            | Why                                                            | Rejected                                      |
| ----------------------------------- | -------------------------------------------------------------- | --------------------------------------------- |
| qdbd via shared `start-services.sh` | ADR-007 service model, one owner for qdbd flags and license    | bench-private `start-qdbd.sh` / `stop-all.sh` |
| Stress in shell/curl/awk            | keeps the permanent path free of Python and heavyweight builds | Python or the bench harness in CI             |

## Decision log (2026-08-19)

| Decision                                      | Why                                                                                             | Rejected                                      |
| --------------------------------------------- | ----------------------------------------------------------------------------------------------- | --------------------------------------------- |
| qdb-test-setup as a git submodule, not a copy | one owner for qdbd flags and license, the same as nats-connector and old master, updates by SHA | copying the scripts (drift, duplicated owner) |

## Decision log (2026-08-20)

| Decision | Why | Rejected |
| -------- | --- | -------- |

## Decision log (2026-09-12)

| Decision | Why | Rejected |
| -------- | --- | -------- |

## Decision log (2026-09-17)

| Decision                                       | Why                                                                                        | Rejected                                                                        |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------- |
| The harness measures nothing and budgets leave | a CI gate is yes or no (ADR-0015)                                                          | `test-budgets`, `budgets.env`, TTFB and RSS gates                               |
| Arrow compared decoded, against the CSV        | ADR-0015, since there are no stable bytes and the CSV already says what the data is        | status and `content-type` only, a separate Arrow golden, pyarrow in the harness |
| The suites are `v1` and `v2`                   | the names of the API versions they pin, while `legacy` named one of them after its history | `legacy` as a suite, target, driver, fixture or package name                    |

The v2 golden suite this table also shaped (one driver for both suites,
expected bodies in the archive, one case per request) was replaced by
the round trip on 2026-09-18 (ADR-0014). Those rows are gone with it.

## Decision log (2026-09-18)

| Decision                                                 | Why                                                                                    | Rejected                                                  |
| -------------------------------------------------------- | -------------------------------------------------------------------------------------- | --------------------------------------------------------- |
| The v2 e2e is one round trip, generated data, no goldens | proves the basic round trip with little code and no audit liability (ADR-0014)         | ten captured cases and an operator capture cycle          |
| Error rows are Go tests only                             | the e2e proves the round trip, not the surface, and the tables are pinned in `httpapi` | 401/413/415/400 goldens                                   |
| Ingest bodies: CSV, NDJSON and Arrow IPC                 | the round trip ingests the same rows three ways, and the property tests come with them | CSV only, the rest in the ingestion milestone             |
| One Go tool decodes every format to CSV                  | the Arrow rule (ADR-0015) applied to every text format, one expected file              | an encoder per format in shell, row-count checks for JSON |
| The Go tool generates the rows                           | rapid-style generation shares the vocabulary of the property tests                     | an awk generator, a checked-in fixture CSV                |
| The round trip runs on both clusters                     | the same round trip on two parallel environments, and only the login differs           | insecure only                                             |
| A driver with named options                              | two URLs on a command line need names                                                  | positional URLs, environment variables                    |
| The plain run sends no `Accept-Encoding`                 | what a plain client sends, and the explicit `identity` rule is a Go test               | `Accept-Encoding: identity`                               |

## Decision log (2026-10-11)

| Decision                               | Why                                                                                                 | Rejected                                                                  |
| -------------------------------------- | --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| The ingest takes columnar JSON         | every read format is an ingest format, and the decoder set covers every response                    | columnar JSON as output only, a tool-local JSON reader                    |
| The first e2e unit is one basic test   | one small step on the built binary before the surface is defined                                    | the whole round trip in one unit                                          |
| The surface is defined with the owner  | what is useful to test is the owner's call, not derivable from the documents                        | the agent cutting the remaining steps alone                               |
| The tool reuses the package's encoders | no duplicated cell formatting, and the encoders are pinned byte for byte without cluster            | a tool-local CSV writer so the expected file is untouched by the encoders |
| The tool stays Go                      | no pyarrow wheels on FreeBSD and no venv on the agents, and Flight SQL comes with arrow-go's client | a Python client, the Python API as the e2e client                         |
| Ten thousand rows by default           | owner's preference                                                                                  | one thousand                                                              |

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
| Headers are asserted from a `curl -D` dump                                                | `-w` formats cannot say a header is absent                                                                                              | `curl -w '%{content_type}'`, because it cannot assert an absent `content-encoding`                                                                                                   |
| gen writes the four body files in one run                                                 | the bodies are one batch through four encoders, and one run keeps one draw per table                                                    | one file per run under `--format`, because four draws of the same seed are four times the work for the same batch                                                                    |
| gen runs once per table                                                                   | `$table` is a column of every body and names the table the row goes to                                                                  | one run with a `$table` rewritten per table, because the tool would then format a cell                                                                                               |
| e2etool is built into the repository's `bin/`                                             | one directory for both cgo binaries makes the CI step's Windows DLL co-location one copy (owner)                                        | overriding `E2ETOOL` on the CI command line, because it hides the layout in the step; DLLs copied into the tool's source directory, because they would sit untracked in the checkout |
| `40.test-e2e.sh` calls the harness Makefile through GNU make                              | the Makefile is the harness's only entry point and owns the server lifecycle                                                            | driving `round-trip.sh` from the step, because it duplicates the Makefile's start, trap and stop                                                                                     |
| The Makefile's exit trap is the one stop of the REST servers                              | the lifecycle is small and known, and start and stop stay together (owner)                                                              | stopping by pidfile from `hooks/pre-exit`, because it splits the lifecycle across two files for the case of a step killed by its timeout                                             |
| The round trip runs inside the build step, after the Go tests                             | the harness runs in the build step (ADR-0015), and a separate step would rebuild and restart qdbd                                       | a separate pipeline step                                                                                                                                                             |
| The dataset, the v1 goldens, the old-server build and the bench leave the harness         | the early preview needs none of them, and each cost time the preview does not have (owner, ADR-0015)                                    | keeping the twelve goldens that read no dataset; a smaller dataset archive for CI                                                                                                    |
| The stress generates its own rows                                                         | the round trip already proves generated rows over HTTP, and no customer-derived data is in scope (ADR-0015)                             | a large `reproduce` range                                                                                                                                                            |

## Decision log (2026-10-15)

| Decision                                        | Why                                                                                         | Rejected                                                                     |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| The sql step selects the table name as `$table` | qdb-duck's scan answers no `$table` column, and tocsv routes rows by it                     | teaching tocsv a missing `$table`, because the tool would then shape a table |
| The sql step runs on both clusters              | the secure attach through the DuckDB secret is proven over HTTP on the built binary (owner) | the insecure cluster alone                                                   |

## Decision log (2026-10-19)

| Decision                                                     | Why                                                                                                                                                 | Rejected                                                                                  |
| ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Smoke tests through Arrow's Flight SQL ADBC and JDBC drivers | customers are likely to connect through both official drivers, the two share no client code, and the property tests drive arrow-go's client (owner) | Flight covered by the Go property tests alone                                             |
| ADBC from Go, through an `e2etool` subcommand                | the ADBC Flight SQL driver is Go code that its Python and C packages bind (owner)                                                                   | Python through the driver's Python package, which needs a Python toolchain on every agent |
| JDBC from one Java source file                               | JDBC is a Java API, and `java` runs one source file without a build tool (owner)                                                                    | Python through JayDeBeApi                                                                 |
| The smoke tests run on both clusters                         | the secure cluster proves Handshake and the bearer, and the insecure cluster proves the anonymous user (owner)                                      | the secure cluster alone                                                                  |
| The driver jar comes from Maven Central, pinned and hashed   | no large binary in git and nothing extra on the agent images (owner)                                                                                | the jar committed to the repository, the jar installed by agent provisioning              |
