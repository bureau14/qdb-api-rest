# internal/ -- Agent Instructions

Scope: every Go package under `internal/`. Package layout and what each
package owns: `docs/brief.md`, "Project structure". Hard decisions:
`docs/adr/`.

## Code

- Book order: definitions before use; a reader never scrolls up.
- No package-level mutable state. `context.Context` is the first
  parameter of anything that does I/O, logs, or can be cancelled.
- Small composable functions with descriptive names; explicit over
  implicit. Comments state why, as facts; never history.

## Building

- `internal/qdb` imports the vendored `qdb-api-go`, so anything that
  imports it (the binary does) compiles through cgo against `qdb/`.
  `source .envrc` (or direnv) before a bare `go build` / `go test`; the
  root Makefile does it for you. Without it the compile fails on missing
  qdb headers. `qdb-api-go` is never patched in `vendor/`: fix upstream,
  then bump the version in `go.mod` and re-run `go mod vendor`.

## Logging (ADR-0002)

- Log through the context, `*Context` methods only:
  `observe.Logger(ctx).InfoContext(ctx, msg, attrs...)`. Never
  `slog.Default()`, `slog.Info`, or a stored logger (`make lint` rejects
  them). `Logger` panics on a ctx without a logger: pass the ctx you were
  given, never `context.Background()`.
- Scope attributes with `observe.WithAttrs(ctx, ...)` and pass the child
  ctx down; the caller's ctx stays untagged.
- Keys come from `observe.Key*`; errors go through `observe.Err(err)`.
  Add a key to `observe` before using it in a second package.
- Edges enrich, handlers do not: HTTP middleware and gRPC interceptors
  tag the ctx (request id, principal, session); code below only logs.
- Any type that holds a secret implements `slog.LogValuer` so it can
  never print one.

## Tests

- Pin genuine logic only; no tests for glue. White-box, same package,
  small helpers declared before use, `t.Helper()`.
- Data-shaped behaviour gets property tests (`pgregory.net/rapid`);
  wire-shaped behaviour gets the e2e harness (`tests/e2e/`).
