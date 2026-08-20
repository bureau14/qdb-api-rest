# tests/e2e -- Agent Instructions

Scope: the permanent e2e harness. Specification and verified facts live in
`docs/e2e-plan.md`; progress in `docs/log.md`. Usage in `README.md`.

- `Makefile` is the only entry point and the single source of truth for
  paths, ports and flags; scripts receive them as arguments or exported
  variables. Add targets there, not new top-level scripts.
- `common.sh` holds every shared helper (logging, pidfiles, qdbsh wrapper,
  chunked `qdb_export`, awk CSV compare). Source it; do not duplicate.
  Always call `qdbsh` through the wrapper (it redirects qdbsh's log files
  out of the tree).
- qdbd is a service (`scripts/tests/setup/start-services.sh`, a git
  submodule: never edit it here). Tests fail fast if it is down; they never
  start or stop it. The harness starts and stops only REST servers, via
  pidfiles.
- Goldens under `golden/legacy/`: `request.json` is written by hand, the
  captured `status`/`headers`/`body` are written only by
  `make capture-golden` and committed as-is. To add a case, add a directory
  with a `request.json`, run `make capture-golden CASES=<case>`, eyeball the
  body, commit. Recapturing everything is an operator decision; diff the
  result before committing.
- `TZ=UTC` is exported by `common.sh` for every server the harness starts
  and every capture; keep it that way (legacy timestamps are local-time).
- The dataset table `reproduce` is read-only for tests; fixture tables
  (`seed.sql`) are dropped and recreated freely.
- Shell style: `set -euo pipefail`, small named functions, definitions
  before use, ASCII only. No Python in this directory (the bench in
  `bench/` is the one exception and is temporary; its conventions live in
  `bench/AGENTS.md`).
