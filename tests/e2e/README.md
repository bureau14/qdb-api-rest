# tests/e2e -- end-to-end harness

The v2 round trip, the v1 goldens and stress for the QuasarDB REST API, run
against a live qdbd. The specification is `docs/e2e.md`, and the
conventions are in `AGENTS.md`. The bench in `bench/` has its own README
and plan (`docs/bench.md`).

## Prerequisites

- The QuasarDB distribution extracted into `<repo>/qdb` (`qdb/bin/qdbd`,
  `qdbsh`, `qdb_export`, `qdb_import`, `qdb/lib`).
- qdbd running: `bash scripts/tests/setup/start-services.sh` (insecure
  `127.0.0.1:2836`, secure `:2838`). The script force-restarts and wipes
  the data dirs, so re-run `make load` afterwards.
- `jq`, `curl`, GNU make, Go (for `make old-server` and the round trip's
  `tools/e2etool`).

## Usage

```
make load                    # dataset into qdbd (download, sha256, import; idempotent)
make verify-dataset          # export the loaded table and byte-compare with the CSV
make seed                    # small fixture tables + tags (idempotent)
make old-server              # build the old REST server from master (worktree in .old-master/)
make capture-v1              # operator: (re)capture goldens from the old server
make test-v1-selfcheck       # replay goldens against the old server (harness determinism)
make test-v1 QDB_REST_BIN=<new server binary> [REST_ARGS=...]
make test-v1 REST_URL=http://127.0.0.1:40090     # against an already running server
make e2etool                 # build the Go tool of the v2 round trip
make test-round-trip QDB_REST_BIN=<new server binary>  # the v2 round trip, one server per cluster
make test-round-trip REST_URL_INSECURE=http://127.0.0.1:40090 REST_URL_SECURE=http://127.0.0.1:40091
```

All capture/replay targets accept `CASES='<case> ...'` to run a subset of
the golden cases (default: all).

`make package-dataset SRC=<db.tar.zst> OUT=<dir>` produces the dataset
archive and also prints the `datasets.json` entry and the upload command.
This is an operator step. `DATASETS_LOCAL_DIR=<dir>` makes `make load`
take the archive from a local directory instead of S3.

## The v2 round trip

`make test-round-trip` logs in, creates the tables `e2e_csv`,
`e2e_ndjson`, `e2e_arrow` and `e2e_json` with one column of every type,
ingests the generated rows into each as its body format, and reads and
queries (`SELECT *`) every table back as CSV, NDJSON, Arrow IPC and
JSON under identity and gzip, each response decoded to CSV and compared
byte for byte with the generated file, on the insecure and the secure
cluster. Nothing is captured (ADR-0014). `ROWS=<n>` and `SEED=<s>`
select the generated data, and the round trip prints both so a failure
reproduces. `tools/e2etool gen` draws the rows and writes `rows.csv`,
`rows.ndjson`, `rows.arrow` and `rows.json` through the server's own
encoders, and `tools/e2etool tocsv` turns a response into CSV through
the server's own decoders. The definition is `docs/e2e.md`, "The v2
round trip". Working files are under `actual/round-trip/<cluster>/`,
one `.body` and one `.headers` per request.

## v1 goldens

`golden/v1/<NN-slug>/request.json` is hand-written. The `status`,
`headers` and `body` next to it are captured from the old server and
committed. The header of `golden.sh` describes the request and compare
modes. The editing rules are in `AGENTS.md`, and the provenance and
verified facts are in `docs/e2e.md`.
