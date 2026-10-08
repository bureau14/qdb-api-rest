# tests/e2e -- Agent Instructions

Scope: the permanent e2e harness. The specification and the verified
facts live in `docs/e2e.md`, progress in `docs/log.md`, and usage in
`README.md`.

- `Makefile` is the only entry point and the single source of truth for
  paths, ports and flags. Scripts receive them as arguments or exported
  variables. Add targets there rather than new top-level scripts.
- `common.sh` holds every shared helper (logging, pidfiles, the qdbsh
  wrapper, chunked `qdb_export`). Source it and do not duplicate it.
  Always call `qdbsh` through the wrapper, which redirects qdbsh's log
  files out of the tree.
- qdbd is a service (`scripts/tests/setup/start-services.sh`, a git
  submodule that is never edited here). Tests fail fast if it is down,
  and they never start or stop it. The harness starts and stops only
  REST servers, via pidfiles.
- Nothing in this directory asserts a timing or a measured number
  (ADR-0013). Every assertion is pass or fail, compared with `cmp`.
- The v2 flow (`flow.sh`, `make test-flow`, ADR-0014) captures nothing.
  The rows it ingests come from `tools/e2etool gen`, `tools/e2etool
tocsv` decodes the responses to CSV, and the flow compares them with
  the generated CSV. A wire change that breaks the flow is fixed in the
  tool or the driver in the same commit, never by storing a response.
  Error rows are Go tests in `internal/httpapi`, never flow steps. The
  generated rows carry no empty string (`docs/e2e.md`, "The v2 flow").
- A v1 golden is an audited expected response, compared byte for byte,
  with no canonicalization and no tolerance. Capture is an operator
  step and never runs in CI. Under `golden/v1/`, `request.json` is
  written by hand, and only `make capture-v1` writes the captured
  `status`, `headers` and `body`, which are committed as they are. A
  captured file is never edited, and the comparator never grows a
  special case. No case exercises a deliberate deviation of v1 from the
  old server (`docs/brief.md`, "Deliberate deviations"), so no golden
  query selects a `COUNT`. To add a case, add a directory with a
  `request.json`, run `make capture-v1 CASES=<case>`, eyeball the body,
  and commit. Recapturing everything is an operator decision, and the
  result is diffed before it is committed.
- `common.sh` exports `TZ=UTC` for every server the harness starts and
  every capture. Keep it that way, because v1 timestamps are local
  time.
- The dataset table `reproduce` is read-only for tests, and the fixture
  tables (`seed.sql`) are dropped and recreated freely. Both serve the
  v1 suite and the bench only. The flow creates its own `e2e_*` tables
  on both clusters and drops them first.
- For ad-hoc probing, `qdbsh --output-format csv` is a plain C client.
  Its `-c` flag cannot be repeated, so a multi-statement session (for
  example `direct_set_node` followed by `direct_int_get`) goes in via
  stdin. A single-shot `SELECT *` of `reproduce` overflows qdbsh's
  client input buffer (125 MiB), as it does for `qdb_export`.
- Shell style: `set -euo pipefail`, small named functions, definitions
  before use, ASCII only. There is no Python in this directory, except
  the bench in `bench/` (`docs/bench.md`), whose conventions live in
  `bench/AGENTS.md`.
