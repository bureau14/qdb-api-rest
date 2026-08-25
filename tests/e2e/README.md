# tests/e2e -- end-to-end harness

Golden-data equivalence, budgets and stress for the QuasarDB REST API, run
against a live qdbd. Specification: `docs/e2e-plan.md`. Make + shell +
curl + jq + awk; no Python (the bench in `bench/`, `docs/bench-plan.md`, is
the one exception).

## Prerequisites

- The QuasarDB distribution extracted into `<repo>/qdb` (`qdb/bin/qdbd`,
  `qdbsh`, `qdb_export`, `qdb_import`, `qdb/lib`).
- qdbd running: `bash scripts/tests/setup/start-services.sh` (insecure
  `127.0.0.1:2836`, secure `:2838`; the script force-restarts and wipes data
  dirs, so re-run `make load` afterwards).
- `jq`, `curl`, GNU make, Go (only for `make old-server`).

## Usage

```
make load                    # dataset into qdbd (download, sha256, import; idempotent)
make verify-dataset          # export the loaded table and byte-compare with the CSV
make seed                    # small legacy fixture tables + tags (idempotent)
make old-server              # build the old REST server from master (worktree in .old-master/)
make capture-golden          # operator: (re)capture goldens from the old server
make test-legacy-selfcheck   # replay goldens against the old server (harness determinism)
make test-legacy QDB_REST_BIN=<new server binary> [REST_ARGS=...]
make test-legacy REST_URL=http://127.0.0.1:40090     # against an already running server
```

All capture/replay targets accept `CASES='<case> ...'` to run a subset of
the golden cases (default: all).

The dataset archive is produced by
`make package-dataset SRC=<db.tar.zst> OUT=<dir>`, which also prints the
`datasets.json` entry and the upload command (operator step;
`DATASETS_LOCAL_DIR=<dir>` makes `make load` take the archive from a local
directory instead of S3).

## Legacy goldens

`golden/legacy/<NN-slug>/request.json` is hand-written; `status`, `headers`
and `body` next to it are captured from the old server and committed.
Request and compare modes: the header of `legacy.sh`. Editing rules:
`AGENTS.md`; provenance and verified facts: `docs/e2e-plan.md`.
