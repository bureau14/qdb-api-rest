# scripts/cicd/ -- Conventions

Scope: the Buildkite step scripts. The pipeline that invokes them lives
in `.buildkite/` (see its `AGENTS.md`).

## Contract

- Every step script sources `00.common.sh` first; `00.common.sh` runs
  nothing on its own (the leading `00.` means "loaded first, executes
  nothing").
- The Go toolchain arrives as `GOROOT`/`GOPATH` env vars injected by
  `pipeline.py`; `cicd_setup_go_toolchain` derives `${GO}` from them.
  Never invoke a bare `go` from a step script -- except through the root
  Makefile, whose PATH `cicd_setup_go_toolchain` has already prepended.
- `10.lint.sh` delegates to `make lint` (Linux-only step, GNU make
  available): the golangci-lint version pin has exactly one writer, the
  root Makefile. The per-platform scripts call `${GO}` directly instead
  of make because FreeBSD ships BSD make and the Windows agents none.
- `20.build.sh` composes the same `-ldflags` as the root Makefile's
  build target (VERSION file, git SHA, build time, build mode, GOAMD64);
  when one changes, change the other.
- Builds and tests run `-mod=vendor` and `-buildvcs=false`; the reasons
  are commented where they are used.
- No CGO env helper exists yet; when M1 vendors qdb-api-go, the
  canonical CGO environment lands in `00.common.sh` (qdb-nats-connector
  `.envrc` + `cicd_setup_qdb_env` is the reference).
