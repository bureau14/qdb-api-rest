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
- A statistics snapshot is named after what it describes, `FooStats`
  (`ClusterStats`, the binding's `SessionPoolStats`), never a bare `Stats`; a bare
  `Stats` exists only as the type that composes every `FooStats` of its
  package.

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
  `slog.Default()` or `slog.Info` (`make lint` rejects them): the
  logger is state (it carries attributes) and is passed along by value,
  in the ctx wherever there is one. A component that has no ctx at call
  time (the `qdb-api-go` logger adapter) holds the logger it was given.
  `Logger` panics on a ctx without a logger: pass the ctx you were
  given, never `context.Background()`.
- The context is where request-scoped and process-scoped values travel:
  the logger (`observe`) and the cluster (`qdb.WithCluster` /
  `qdb.ClusterFrom`, which panics without one, like `Logger`). Handlers
  read them from the request context; nothing is injected through
  constructors that the context already carries, and `NewHandler` takes
  no arguments.
- Scope attributes with `observe.WithAttrs(ctx, ...)` and pass the child
  ctx down; the caller's ctx stays untagged.
- Keys come from `observe.Key*`; errors go through `observe.Err(err)`.
  Add a key to `observe` before using it in a second package.
- Edges enrich, handlers do not: HTTP middleware and gRPC interceptors
  tag the ctx (request id, user, session); code below only logs.
- Any type that holds a secret implements `slog.LogValuer` so it can
  never print one.

## Tests

- Pin genuine logic only; no tests for glue. White-box, same package,
  small helpers declared before use, `t.Helper()`.
- Data-shaped behaviour gets property tests (`pgregory.net/rapid`);
  wire-shaped behaviour gets the e2e harness (`tests/e2e/`).
- The tests are not in the business of testing the C API, which offers
  no way to mock an error or a stall. A mechanism that could only be
  exercised by stalling or faking a C call (the abandon-on-deadline
  failsafe) is pinned on its pure part (`guard`) and otherwise left
  untested; never add a listener that stalls `connect`, and never mock
  the binding.
- `internal/qdb` and `internal/httpapi` tests dial a live qdbd (the pair
  from `scripts/tests/setup/start-services.sh`, insecure `2836` / secure
  `2838`), so a bare `go test ./...` needs those services up. The fixture
  has one home, `internal/qdbtest`: the URIs, the key files, and
  `Require`, which fails fast with the start hint when a port does not
  answer. Nothing is skipped under `-short`.
