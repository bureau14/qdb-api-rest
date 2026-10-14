# third_party/ -- Agent Instructions

Scope: code that stands in for a dependency's module. Nothing here is
vendored (that is `vendor/`), and nothing here is this project's own
logic.

## duckdb-nolib

`duckdb-nolib/<os>-<arch>/` are five one-file modules that `go.mod`
puts in place of the five prebuilt DuckDB library modules of
`duckdb-go-bindings` (`lib/linux-amd64`, `lib/linux-arm64`,
`lib/darwin-amd64`, `lib/darwin-arm64` and `lib/windows-amd64`), one
directory per module because a replacement directory serves one module
path.

- The binding imports a prebuilt module only when neither
  `duckdb_use_static_lib` nor `duckdb_use_lib` is set, and the root
  `.envrc` sets one of the two on every platform through `GOFLAGS`, so
  the build never compiles them. `go mod vendor` selects packages under
  every build tag regardless, and the five modules hold about half a
  gigabyte of static archives, so without the replacement a re-vendor
  would commit them (owner, 2026-10-14). The alternatives were vendoring
  them whole, and pruning them after every `go mod vendor` through a
  make target, which a bare `go mod vendor` would undo.
- A stub carries no cgo directive and no symbol. A build without the
  tags compiles against it and fails at link time for want of
  libduckdb, which is the signal that `.envrc` was not sourced.
- Bumping the binding bumps the version in `go.mod` only. The stubs
  carry no version and need no change.
