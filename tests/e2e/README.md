# tests/e2e -- end-to-end harness

The v2 flow, the v1 goldens and stress for the QuasarDB REST API, run
against a live qdbd. Specification: `docs/e2e.md`; conventions:
`AGENTS.md`. The bench in `bench/` has its own README and plan
(`docs/bench.md`).

## Prerequisites

- The QuasarDB distribution extracted into `<repo>/qdb` (`qdb/bin/qdbd`,
  `qdbsh`, `qdb_export`, `qdb_import`, `qdb/lib`).
- qdbd running: `bash scripts/tests/setup/start-services.sh` (insecure
  `127.0.0.1:2836`, secure `:2838`; the script force-restarts and wipes data
  dirs, so re-run `make load` afterwards).
- `jq`, `curl`, GNU make, Go (for `make old-server` and the flow's
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
make test-flow QDB_REST_BIN=<new server binary>  # the v2 flow, one server per cluster (arrives with M2)
```

All capture/replay targets accept `CASES='<case> ...'` to run a subset of
the golden cases (default: all).

The dataset archive is produced by
`make package-dataset SRC=<db.tar.zst> OUT=<dir>`, which also prints the
`datasets.json` entry and the upload command (operator step;
`DATASETS_LOCAL_DIR=<dir>` makes `make load` take the archive from a local
directory instead of S3).

## The v2 flow

`make test-flow` logs in, creates a table per input format, queries them
empty, ingests generated rows and reads them back in every format under
`identity` and `gzip`, on the insecure and the secure cluster; nothing
is captured (ADR-0014). `ROWS=<n>` and `SEED=<s>` select the generated
data; the seed is printed so a failure reproduces. Specification:
`docs/e2e.md`, "The v2 flow".

## v1 goldens

`golden/v1/<NN-slug>/request.json` is hand-written; `status`, `headers`
and `body` next to it are captured from the old server and committed.
Request and compare modes: the header of `golden.sh`. Editing rules:
`AGENTS.md`; provenance and verified facts: `docs/e2e.md`.
