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
  is injected via `-ldflags` into `internal/build`, which `main` reads
  for `--version`, and no version constants live in source.
  `scripts/cicd/AGENTS.md` owns the composition rule.
- `--version` prints the linked C API version and the linked DuckDB
  release as the library reports it, which makes it CI's link smoke test
  for both (`scripts/cicd/20.build.sh`).
- The binary serves three listeners: HTTP, HTTPS and Flight SQL. HTTP
  is off unless `--listen-http` is set, and the other two cannot be off
  (ADR-0001).
- Every HTTP request inherits the process values through a context
  without the process context's cancellation (`newListeners`), so
  SIGINT or SIGTERM cuts no stream in flight and `listen.shutdown_grace`
  is what ends one. The drain closes every listener's port first, so a
  new connection is refused while streams finish, and readiness is not
  consulted (ADR-0004). Flight takes the process context itself, because
  its interceptors splice the values onto each call's own context.
- The binary speaks systemd's notify protocol through `internal/notify`:
  `READY=1` once `newListeners` has returned, which is once every port
  is bound, `STOPPING=1` right after the process context's end and
  before the drain, and `WATCHDOG=1` on the process context between the
  two. Nothing is sent when `NOTIFY_SOCKET` is unset, and a failed send
  is one warning. The HTTP servers bind in `newHTTPServer`, before
  serving, so that `READY=1` is true when sent.
