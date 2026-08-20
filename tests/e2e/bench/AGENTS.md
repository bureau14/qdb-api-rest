# tests/e2e/bench -- Agent Instructions

Scope: the temporary assessment bench. Specification and verified facts
live in `docs/bench-plan.md`; progress in `docs/log.md`; usage in
`README.md`.

- Temporary tool: no abstractions beyond what the measurement needs;
  deletable with one `rm -rf tests/e2e/bench`. Anything with a longer
  lifetime belongs in `tests/e2e` proper.
- The `Makefile` is the only configuration source; `bench.py` takes every
  path and port as a required flag and has no defaults.
- qdbd and the dataset are owned by the e2e harness; the bench starts and
  stops only the REST server of its run, one (protocol, server) pair per
  invocation.
- The tests are the `selftest` subcommand (fingerprint invariants) and the
  cross-protocol fingerprint match in `report`; do not add a unit-test
  suite.
- `results/` is never committed; measured numbers live in result files and
  `docs/log.md` lessons, never in the plan.
- Python style: functional, small composable functions, book order
  (definitions before use), ASCII only. This is the only Python in
  `tests/e2e`.
