# tests/e2e -- Agent Instructions

Scope: the permanent e2e harness. The specification and the verified
facts live in `docs/e2e.md`, progress in `docs/log.md`, and usage in
`README.md`.

- `Makefile` is the only entry point and the single source of truth for
  paths, ports and flags. Scripts receive them as arguments or exported
  variables. Add targets there rather than new top-level scripts.
- `common.sh` holds every shared helper (logging, pidfiles,
  `wait_for_exit`, the request, login, create, ingest and compare
  helpers both drivers share, the qdbsh wrapper). Source it and do not
  duplicate it. Always call `qdbsh` through the wrapper, which redirects
  qdbsh's log files out of the tree.
- qdbd is a service (`scripts/tests/setup/start-services.sh`, a git
  submodule that is never edited here). Tests fail fast if it is down,
  and they never start or stop it. The harness starts and stops only
  REST servers, via pidfiles.
- Nothing in this directory asserts a timing or a measured number
  (ADR-0015). Every assertion is pass or fail, compared with `cmp`.
- Nothing in this directory loads a dataset (ADR-0015). Every row a
  suite needs is drawn by `tools/e2etool gen`, and the suite creates
  its own `e2e_*` tables on both clusters and drops them first.
- The v2 round trip (`round-trip.sh`, `make test-round-trip`, ADR-0014)
  captures nothing. The rows it ingests come from `tools/e2etool gen`,
  which draws them through the table fixture (`internal/qdbtest/table`)
  and writes every body file through the server's encoders, and every
  response that is not the reader's CSV is decoded through
  `tools/e2etool tocsv` and the server's decoders, so the tool formats
  and parses no cell of its own. The mechanics are `docs/e2e.md`, "The
  v2 round trip". A wire change that breaks the round trip is fixed in
  the tool or the driver in the same commit, never by storing a
  response. Error rows are Go tests in `internal/httpapi`, never round
  trip steps. The generated rows carry no empty string. The tool is
  built by `make e2etool` with the server's toolchain and the root
  `.envrc` into the repository's `bin/`, next to `qdb_rest`, so the CI
  step co-locates the Windows DLLs once, and it may import any package
  of this repository. The create body in `common.sh` and
  `roundTripSchema` in the tool spell the same column names, and a
  change to one changes the other. A new response format is one entry
  in the driver's matrix and one decoder in the tool. The driver asserts
  the startup warning of a secure cluster behind plain HTTP by grepping
  each server's log: present in the secure server's, absent from the
  insecure server's. The message is spelled in `round-trip.sh`
  (`PLAINTEXT_WARNING`) and in `cmd/qdb_rest/main.go`, and a change to
  one changes the other. A server given as a URL has no log, and the
  step is skipped for it.
- The driver scrapes `/metrics` on each server after the format loop
  and greps each name of `METRIC_SERIES` at the start of a line. A
  unit that adds a series adds its name there in the same commit, and
  nothing asserts a value.
- The stress (`stress.sh`, `make test-stress`, `docs/e2e.md`, Stress
  definition) runs by hand and never in CI. One call is one cluster,
  and the step ends the server it was given: it sends the one SIGTERM
  and waits for the exit with `wait_for_exit`. Never send a draining
  server a second SIGTERM, and never `stop_server` it: the binary
  releases its signal handler once the serve loop ends, and the second
  signal kills it mid-drain. The parameters are the Makefile's.
- The ADBC and JDBC steps. The ADBC steps run `tools/e2etool adbc`,
  which drives arrow-adbc's Flight SQL driver and writes the result
  through a server encoder: Arrow IPC, which the driver sends through
  `tocsv`, or CSV for a result the round trip's schema does not type.
  The tool writes no text of its own for either. The JDBC steps run the
  jar `make jdbc` builds from `tools/jdbc`, which compares column names
  and a row count and never a cell.
- `common.sh` exports `TZ=UTC` for every server the harness starts.
  Keep it that way, so a response that carries a local-time timestamp
  compares equal on every machine.
- For ad-hoc probing, `qdbsh --output-format csv` is a plain C client.
  Its `-c` flag cannot be repeated, so a multi-statement session (for
  example `direct_set_node` followed by `direct_int_get`) goes in via
  stdin.
- Shell style: `set -euo pipefail`, small named functions, definitions
  before use, ASCII only.
