# cmd/qdb_rest -- Agent Instructions

Scope: the binary's entry point. Packages it wires together live under
`internal/` (see its `AGENTS.md`).

- Command line: GNU long options only, the spelling of every QuasarDB
  binary. Stdlib `flag`, which parses one or two dashes alike; no short
  aliases, no `pflag`. Every configuration key is a flag named after its
  path with dots and underscores as hyphens (`pool.max_sessions` is
  `--pool-max-sessions`), a `QDB_REST_<KEY_PATH>` environment variable
  and a file key, all derived from the one struct in `internal/config`;
  the operator picks the layer. `--cluster` and `--user-security-file`
  are the `qdbsh` aliases; `--config FILE`, `--version` and `--help` are
  the meta flags (`examples/qdb_rest.yaml` is the reference).
- Build metadata (version, commit, build time, build mode, arch level)
  is injected via `-ldflags`; no version constants in source. The
  composition rule is owned by `scripts/cicd/AGENTS.md`.
- `--version` prints the linked C API version, which makes it CI's link
  smoke test on every platform (`scripts/cicd/20.build.sh`).
