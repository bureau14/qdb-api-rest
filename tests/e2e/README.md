# tests/e2e -- end-to-end harness

Golden-data equivalence, budgets and stress for the QuasarDB REST API, run
against a live qdbd. Specification: `docs/e2e-plan.md`. Make + shell + curl

- jq + awk; no Python (the temporary Python bench lives in `bench/`).

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

Until the dataset archive is uploaded to S3, pass
`DATASETS_LOCAL_DIR=<dir holding reproduce-<date>-5613032.tar.gz>` to
`make load`. The archive is produced by
`make package-dataset SRC=<db.tar.zst> OUT=<dir>`, which also prints the
`datasets.json` entry and the upload command.

## Legacy goldens

`golden/legacy/<NN-slug>/request.json` is hand-written; `status`, `headers`
and `body` next to it are captured by `legacy.sh capture` and committed,
never edited by hand. See the header of `legacy.sh` for the request and
compare modes. Both capture and replay run under `TZ=UTC` (set in
`common.sh`): the legacy JSON renders timestamps in the server's local zone.

Goldens come from the old server built from `master`, whose legacy wire
code is byte-identical to the released `v3.14.2` (verified by diff; only a
version constant differs), linked against the same `qdb/` C API as the
server under test.
