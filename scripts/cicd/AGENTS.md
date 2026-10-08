# scripts/cicd/ -- Conventions

Scope: the Buildkite step scripts. The pipeline that invokes them lives
in `.buildkite/` (see its `AGENTS.md`).

## Contract

- Every step script sources `00.common.sh` first. `00.common.sh` runs
  nothing on its own, and the leading `00.` means "loaded first,
  executes nothing".
- The Go toolchain arrives as `GOROOT`/`GOPATH` env vars injected by
  `pipeline.py`, and `cicd_setup_go_toolchain` derives `${GO}` from
  them. A step script never invokes a bare `go`, except through the
  root Makefile, whose PATH `cicd_setup_go_toolchain` has already
  prepended.
- `10.lint.sh` delegates to `make lint`, a Linux-only step with GNU make
  available, so the golangci-lint version pin has one writer, the root
  Makefile. The per-platform scripts call `${GO}` directly instead of
  make because FreeBSD ships BSD make and the Windows agents none.
- `20.build.sh` composes the same `-ldflags` as the root Makefile's
  build target (VERSION file, git SHA, build time, build mode, GOAMD64).
  When one changes, change the other.
- Builds and tests run `-mod=vendor` and `-buildvcs=false`, and the
  reasons are commented where they are used.
- The CGO environment has one writer, the root `.envrc` (direnv on
  developer machines, sourced by the root Makefile and by
  `cicd_setup_qdb_env` in CI). Every step that compiles Go calls
  `cicd_assert_qdb_tree` then `cicd_setup_qdb_env`, lint included,
  because golangci-lint's typecheck compiles the cgo package. A step
  script never exports `CGO_*`.
