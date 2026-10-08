# tests/e2e/bench -- Agent Instructions

Scope: the assessment bench. The specification, verified facts, lifetime
and retirement condition live in `docs/bench.md`, progress in
`docs/log.md`, and usage in `README.md`.

- No abstractions beyond what the measurement needs. Anything with a
  longer lifetime than the bench belongs in `tests/e2e` proper.
- The `Makefile` is the only configuration source. `bench.py` takes
  every path and port as a required flag and has no defaults.
- The e2e harness owns qdbd and the dataset. The bench starts and stops
  only the REST server of its run, one (protocol, server) pair per
  invocation.
- The tests are the `selftest` subcommand (fingerprint invariants) and
  the cross-protocol fingerprint match in `report`. Do not add a
  unit-test suite.
- `results/` is never committed. Measured numbers live in result files
  only, never in the plan or the log.
- Python style: functional, small composable functions, book order
  (definitions before use), ASCII only. This is the only Python in
  `tests/e2e`.
