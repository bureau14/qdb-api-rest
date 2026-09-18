# tests/e2e -- Agent Instructions

Scope: the permanent e2e harness. Specification and verified facts live in
`docs/e2e.md`; progress in `docs/log.md`. Usage in `README.md`.

- `Makefile` is the only entry point and the single source of truth for
  paths, ports and flags; scripts receive them as arguments or exported
  variables. Add targets there, not new top-level scripts.
- `common.sh` holds every shared helper (logging, pidfiles, qdbsh wrapper,
  chunked `qdb_export`). Source it; do not duplicate.
  Always call `qdbsh` through the wrapper (it redirects qdbsh's log files
  out of the tree).
- qdbd is a service (`scripts/tests/setup/start-services.sh`, a git
  submodule: never edit it here). Tests fail fast if it is down; they never
  start or stop it. The harness starts and stops only REST servers, via
  pidfiles.
- Nothing in this directory asserts a timing or a measured number
  (ADR-0013); every assertion is pass or fail, compared with `cmp`.
- The v2 flow (`flow.sh`, `make test-flow`; ADR-0014) captures nothing:
  the rows it ingests come from `tools/e2etool gen`, the responses are
  decoded to CSV by `tools/e2etool tocsv` and compared with the
  generated CSV. A wire change that breaks the flow is fixed in the
  tool or the driver in the same commit, never by storing a response.
  Error rows are Go tests in `internal/httpapi`, never flow steps. The
  generated rows carry no empty string (`docs/e2e.md`, "The v2
  flow").
- A v1 golden is an audited expected response, compared byte for byte;
  no canonicalization, no tolerance. Capture is an operator step and
  never runs in CI. Goldens under `golden/v1/`: `request.json` is written by hand, the
  captured `status`/`headers`/`body` are written only by
  `make capture-v1` and committed as-is; a captured file is never
  edited and the comparator never grows a special case. No case
  exercises a deliberate deviation of v1 from the old server
  (`docs/brief.md`, "Deliberate deviations"): no golden query selects a
  `COUNT`. To add a case, add a directory
  with a `request.json`, run `make capture-v1 CASES=<case>`, eyeball the
  body, commit. Recapturing everything is an operator decision; diff the
  result before committing.
- `TZ=UTC` is exported by `common.sh` for every server the harness starts
  and every capture; keep it that way (v1 timestamps are local-time).
- The dataset table `reproduce` is read-only for tests; fixture tables
  (`seed.sql`) are dropped and recreated freely. Both serve the v1
  suite and the bench only; the flow creates its own `e2e_*` tables
  on both clusters and drops them first.
- Ad-hoc probing: `qdbsh --output-format csv` is a plain C client. Its
  `-c` flag cannot be repeated, so a multi-statement session (e.g.
  `direct_set_node` followed by `direct_int_get`) goes in via stdin. A
  single-shot `SELECT *` of `reproduce` overflows qdbsh's client input
  buffer (125 MiB), exactly as it does for `qdb_export`.
- Shell style: `set -euo pipefail`, small named functions, definitions
  before use, ASCII only. No Python in this directory (the bench in
  `bench/`, `docs/bench.md`, is the one exception; its conventions
  live in `bench/AGENTS.md`).
