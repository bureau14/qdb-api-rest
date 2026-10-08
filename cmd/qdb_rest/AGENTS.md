# cmd/qdb_rest -- Agent Instructions

Scope: the binary's entry point. Packages it wires together live under
`internal/` (see its `AGENTS.md`).

- The command line takes GNU long options only, the spelling of every
  QuasarDB binary. The parser is the stdlib `flag` package, which parses
  one or two dashes alike. There are no short aliases and no `pflag`.
  Every configuration key is a flag named after its path with dots and
  underscores as hyphens (`pool.max_sessions` is `--pool-max-sessions`),
  a `QDB_REST_<KEY_PATH>` environment variable and a file key, all
  derived from the one struct in `internal/config`, and the operator
  picks the layer. `--cluster` and `--user-security-file` are the
  `qdbsh` aliases. `--config FILE`, `--version` and `--help` are the
  meta flags, and `examples/qdb_rest.yaml` is the reference.
- Build metadata (version, commit, build time, build mode, arch level)
  is injected via `-ldflags`, and no version constants live in source.
  `scripts/cicd/AGENTS.md` owns the composition rule.
- `--version` prints the linked C API version, which makes it CI's link
  smoke test on every platform (`scripts/cicd/20.build.sh`).
