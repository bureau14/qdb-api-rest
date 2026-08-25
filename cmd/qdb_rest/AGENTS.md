# cmd/qdb_rest -- Agent Instructions

Scope: the binary's entry point. Packages it wires together live under
`internal/` (see its `AGENTS.md`).

- Command line: GNU long options only (`--version`, `--config FILE`),
  the spelling of every QuasarDB binary. Stdlib `flag`, which parses one
  or two dashes alike; no short aliases, no `pflag`. A flag exists for
  what a systemd unit or a shell passes on the line; everything else is
  the config file plus `QDB_REST_*` environment variables
  (`examples/qdb_rest.yaml` is the reference).
- Build metadata (version, commit, build time, build mode, arch level)
  is injected via `-ldflags`; no version constants in source. The
  composition rule is owned by `scripts/cicd/AGENTS.md`.
- `--version` prints the linked C API version, which makes it CI's link
  smoke test on every platform (`scripts/cicd/20.build.sh`).
