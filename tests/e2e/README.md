# tests/e2e -- end-to-end harness

The v2 round trip and the stress for the QuasarDB REST API, run against
a live qdbd. The specification is `docs/e2e.md`, and the conventions
are in `AGENTS.md`.

## Prerequisites

- The QuasarDB distribution extracted into `<repo>/qdb` (`qdb/bin/qdbd`,
  `qdbsh`, `qdb/lib`).
- qdbd running: `bash scripts/tests/setup/start-services.sh` (insecure
  `127.0.0.1:2836`, secure `:2838`). The script force-restarts and wipes
  the data dirs.
- `curl`, `gunzip`, GNU make, Go (for the round trip's `tools/e2etool`),
  and `java` and `mvn` (for its `tools/jdbc` jar, which the round trip
  needs on every platform).

## Usage

```
make e2etool                 # build the Go tool of the v2 round trip into <repo>/bin/
make jdbc                    # build the JDBC smoke jar into tools/jdbc/target/
make test-round-trip QDB_REST_BIN=<new server binary>  # the v2 round trip, one server per cluster
make test-round-trip REST_URL_INSECURE=http://127.0.0.1:40090 REST_URL_SECURE=http://127.0.0.1:40091 \
    FLIGHT_URL_INSECURE=grpc+tls://127.0.0.1:40094 FLIGHT_URL_SECURE=grpc+tls://127.0.0.1:40095
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
secure cluster. Nothing is captured (ADR-0014). `ROWS=<n>` and `SEED=<s>`
select the generated data, and the round trip prints both so a failure
reproduces. `tools/e2etool gen` draws the rows and writes `rows.csv`,
`rows.ndjson`, `rows.arrow` and `rows.json` through the server's own
encoders, and `tools/e2etool tocsv` turns a response into CSV through
the server's own decoders. The definition is `docs/e2e.md`, "The v2
round trip". Working files are under `actual/round-trip/<cluster>/`,
one `.body` and one `.headers` per request.
