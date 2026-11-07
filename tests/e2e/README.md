# tests/e2e -- end-to-end harness

The v2 round trip, the stress and the v1 comparison for the QuasarDB
REST API, run against a live qdbd. The specification is `docs/e2e.md`,
and the conventions are in `AGENTS.md`.

## Prerequisites

- The QuasarDB distribution extracted into `<repo>/qdb` (`qdb/bin/qdbd`,
  `qdbsh`, `qdb/lib`).
- qdbd running: `bash scripts/tests/setup/start-services.sh` (insecure
  `127.0.0.1:2836`, secure `:2838`). The script force-restarts and wipes
  the data dirs.
- `curl`, `gunzip`, `jq`, GNU make, Go (for the round trip's
  `tools/e2etool`), `java` and `mvn` (for its `tools/jdbc` jar, which
  the round trip needs on every platform), and `openssl` for the v1
  comparison.

## Usage

```
make e2etool                 # build the Go tool of the v2 round trip into <repo>/bin/
make jdbc                    # build the JDBC smoke jar into tools/jdbc/target/
make test-round-trip QDB_REST_BIN=<new server binary>  # the v2 round trip, one server per cluster
make test-round-trip REST_URL_INSECURE=http://127.0.0.1:40090 REST_URL_SECURE=http://127.0.0.1:40091 \
    FLIGHT_URL_INSECURE=grpc+tls://127.0.0.1:40094 FLIGHT_URL_SECURE=grpc+tls://127.0.0.1:40095
make test-stress QDB_REST_BIN=<new server binary>      # the stress, by hand: the drain step on both clusters
make old-server                                        # export master and build the old server into <repo>/bin/
make test-v1-compare QDB_REST_BIN=<new server binary>  # the v1 comparison, by hand, after a round trip
```

## The v2 round trip

`make test-round-trip` logs in, creates the tables `e2e_csv`,
`e2e_ndjson`, `e2e_arrow` and `e2e_json` with one column of every type,
ingests the generated rows into each as its body format, and reads and
queries (`SELECT *`) every table back as CSV, NDJSON, Arrow IPC and
JSON under identity and gzip, each response decoded to CSV and compared
byte for byte with the generated file, then reads every table back
through arrow-adbc's Flight SQL driver and runs a `#sql` query of
DuckDB-only types through it, then does both through Arrow's JDBC
driver, comparing column names and row counts, on the insecure and the
secure cluster, and scrapes `/metrics` on each server for the series
names, presence only. Nothing is captured (ADR-0014). `ROWS=<n>` and `SEED=<s>`
select the generated data, and the round trip prints both so a failure
reproduces. `tools/e2etool gen` draws the rows and writes `rows.csv`,
`rows.ndjson`, `rows.arrow` and `rows.json` through the server's own
encoders, and `tools/e2etool tocsv` turns a response into CSV through
the server's own decoders. The definition is `docs/e2e.md`, "The v2
round trip". Working files are under `actual/round-trip/<cluster>/`,
one `.body` and one `.headers` per request.

## The stress

`make test-stress` starts one server per cluster under a long shutdown
grace and a budget of two sessions, ingests generated rows into
`e2e_stress` as CSV, starts one slow read of the table, sends the
server SIGTERM once the first byte has arrived, and asserts that a new
connection is refused while the read is open, that the body compares
byte for byte with the generated rows, and that the server exits
cleanly on its own. It runs by hand and never in CI. `STRESS_ROWS`,
`STRESS_SEED`, `STRESS_LIMIT_RATE`, `STRESS_GRACE` and
`STRESS_EXIT_WINDOW` are its parameters, and the Makefile says why
each has its value. The definition is `docs/e2e.md`, "Stress
definition". Working files are under `actual/stress/<cluster>/`.

## The v1 comparison

`make test-v1-compare` runs after `make test-round-trip` has seeded the
clusters. It builds the old server from `master` (`make old-server`)
and runs `compare-v1.sh`, which starts it next to the new one on the
secure cluster and then on the insecure one, logs in to both, tags `e2e_csv` with `e2e_tag` through
the new server, and sends each query of `compare-v1.sh` to both: the
old server at `/api/<path>`, the new one at `/api/v1/<path>` and
`/api/<path>`. A 200 compares byte for byte, anything else by status
and the shape of the body, and the login by shape. It runs by hand and
never in CI, and `COMPARE_TABLE` selects the table. The definition is
`docs/e2e.md`, "The v1 comparison". Working files are under
`actual/compare/<cluster>/`, and the export under `old-server/`.
