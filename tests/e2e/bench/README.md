# tests/e2e/bench -- assessment bench

Measures wall-clock time until a Python client holds a fully materialized
pandas DataFrame, per (protocol, server) pair, on the 5.6M-row `reproduce`
dataset. Specification, metric definitions, lifetime and retirement condition:
`docs/bench-plan.md`. Local developer machines only, never CI.

## Prerequisites

- qdbd running: `bash scripts/tests/setup/start-services.sh`
- dataset loaded: `make -C tests/e2e load`
- a `~/git/qdb-api-python` checkout whose `qdb/` tree carries the same
  C API as this repo's (`install-qdb` fans one artifact tree into both;
  `make check` verifies)

## Usage

```
make check venv old-server        # parity check, bench venv, old binary
make bench-native@qdbd            # -> results/native@qdbd.json
make bench-legacy@old-rest        # -> results/legacy@old-rest.json
make bench-legacy@new-rest        # not enabled (docs/bench-plan.md)
make bench-flightsql@new-rest     # not enabled (docs/bench-plan.md)
make report                       # compare all results/*.json
```

Each query runs `WARMUP=3` discarded warmups followed by `REPS=5` measured
repetitions; the report shows medians over the measured reps (warmups are
persisted in the result file flagged `warmup: true`, so cold-start numbers
stay inspectable). `WARMUP=0 REPS=1` gives a quick smoke. `QUERIES=a,b`
restricts the query set, and `CAPI_COMPRESSION=none|balanced` sets the
qdbd <-> C API compression for the run's C-API holder (all of these belong
on the `make` command line). The default is `none`: it is the only value the old server
can honor (its handle setup hardcodes the C API default), and mixing modes
across runs pollutes the comparison -- `balanced` costs ~13% wall on the
reduce-heavy queries over loopback. Each `bench-*` invocation rewrites its
run's result file whole; the final comparison wants one invocation per run
with the full query set.

The first `make venv` builds the `quasardb` wheel from the qdb-api-python
checkout (slow C++ build); it is cached on (checkout sha, C API hash) and
only rebuilt when either changes.
