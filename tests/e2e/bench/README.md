# tests/e2e/bench -- assessment bench

The bench measures wall-clock time until a Python client holds a fully
materialized pandas DataFrame, per (protocol, server) pair, on the
5.6M-row `reproduce` dataset. The specification, metric definitions,
lifetime and retirement condition are in `docs/bench.md`. It runs on
local developer machines only, never in CI.

## Prerequisites

- qdbd running: `bash scripts/tests/setup/start-services.sh`
- dataset loaded: `make -C tests/e2e load`
- a `~/git/qdb-api-python` checkout whose `qdb/` tree carries the same
  C API as this repo's (`install-qdb` fans one artifact tree into both,
  and `make check` verifies it)

## Usage

```
make check venv old-server        # parity check, bench venv, old binary
make bench-native@qdbd            # -> results/native@qdbd.json
make bench-v1@old-rest            # -> results/v1@old-rest.json
make bench-v1@new-rest            # not enabled (docs/bench.md)
make bench-flightsql@new-rest     # not enabled (docs/bench.md)
make report                       # compare all results/*.json
```

Each query runs `WARMUP=3` discarded warmups followed by `REPS=5` measured
repetitions, and the report shows medians over the measured reps. The
warmups are persisted in the result file flagged `warmup: true`, so
cold-start numbers stay inspectable. `WARMUP=0 REPS=1` gives a quick
smoke. `QUERIES=a,b` restricts the query set, and
`CAPI_COMPRESSION=none|balanced` sets the qdbd <-> C API compression for
the run's C-API holder. All of these belong on the `make` command line.
Keep `CAPI_COMPRESSION` the same across the runs you compare, and
`docs/bench.md`, "Two volumes", says why the default is `none`. Each
`bench-*` invocation rewrites its run's result file whole, so the final
comparison wants one invocation per run with the full query set.

The first `make venv` builds the `quasardb` wheel from the qdb-api-python
checkout (a slow C++ build). The wheel is cached on (checkout sha, C API
hash) and only rebuilt when either changes.
