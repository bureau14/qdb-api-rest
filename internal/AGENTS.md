# internal/ -- Agent Instructions

Scope: every Go package under `internal/`. Package layout and what each
package owns: `docs/brief.md`, "Project structure". Hard decisions:
`docs/adr/`.

## Code

- Book order: definitions before use; a reader never scrolls up.
- No package-level mutable state. `context.Context` is the first
  parameter of anything that does I/O, logs, or can be cancelled.
- Small composable functions with descriptive names; explicit over
  implicit. Comments state why, as facts; never history. A negation
  belongs in a comment only where it records a rejected alternative or
  an invariant, never to correct what an earlier version claimed.
- Dense code -- crypto, parsers, encoders -- additionally gets compact
  inline walk-through comments: one short sentence per step, the why as
  it happens (`internal/auth/token.go` is the reference). Glue gets
  none, and a walk-through never pads into narration.
- Validation has one home: the component that consumes a value owns its
  checks, made once, where a violation cannot get past them (`tlsconf`
  owns the certificate-pair rule; the C API judges the dial options at
  dial; `internal/auth` judges its passphrases and argon2id costs).
  `internal/config` owns only shape -- parsing, layer folding, the
  vocabulary of enumerated keys -- never a value's semantic bounds.
  Never duplicate a rule at a second layer, and add no eager check for
  what the consumer already rejects loudly.
- Legacy compatibility code -- the v1 wire surface: wrapper handlers,
  wart encoders, legacy token extraction -- lives in
  `internal/httpapi/legacy` and nowhere else (ADR-0007). That package
  imports `internal/httpapi` for the v2 core and the binary's entry
  point composes the two, so `internal/httpapi` never imports it; a
  legacy route wraps its v2 counterpart, never reimplements it. Inside
  the package names say legacy too (`writeLegacyJSON`, never a bare
  `writeJSON`); outside it, no code knows a wart exists.
- A query result outside `internal/qdb` is the Arrow record batch the
  binding builds through `qdb_query_arrow` and nothing else:
  `Cluster.Query` returns one, the session is back in its pool before
  the caller reads a row, and the batch outlives it. Whoever receives
  the batch owns it and calls `Release` exactly once; its buffers are
  C-allocated and freed by that release. On any error there is no batch
  (a partial one is released inside `internal/qdb`); a statement without
  a result set is a nil batch. The binding's row-major and columnar
  result paths are never called.
- Encoders (`internal/encoding`) share one seam over that batch and know
  only their media type and their bytes: they never flush, never log,
  never negotiate, and never release the batch. The Arrow encoder
  transmits the batch's schema as-is, field metadata included, and
  interprets no column type; the rendering encoders (JSON, NDJSON, CSV)
  are the only code that must know a type to render a cell. Wire types:
  ADR-0009.
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
  then bump the version in `go.mod` (`go get ...@<commit>`, a commit on
  upstream `master`; a branch commit lives only as long as the work
  that needs it) and re-run `go mod tidy` and `go mod vendor`; nothing
  under `vendor/` is written by hand.

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
  constructors that the context already carries. Composing the legacy
  routes (ADR-0007) is the entry point's job: composition, not state.
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
  small helpers declared before use, `t.Helper()`. Test bodies carry the
  same compact walk-through comments as dense code wherever a step's
  purpose is not evident from the assertion.
- Data-shaped behaviour gets property tests (`pgregory.net/rapid`);
  wire-shaped behaviour gets the e2e harness (`tests/e2e/`).
- The tests are not in the business of testing the C API, which offers
  no way to mock an error or a stall: never add a listener that stalls
  `connect`, and never mock the binding.
- `internal/qdb` and `internal/httpapi` tests dial a live qdbd (the pair
  from `scripts/tests/setup/start-services.sh`, insecure `2836` / secure
  `2838`), so a bare `go test ./...` needs those services up. The fixture
  has one home, `internal/qdbtest`: the URIs, the key files, and
  `Require`, which fails fast with the start hint when a port does not
  answer. Nothing is skipped under `-short`.
- A test that needs rows draws a table with `internal/qdbtest/table`
  (`Generate`, then `Create` on a `*qdb.Cluster`) and compares what came
  back with the `Table` it holds; it never writes its own loader. The
  fixture pushes through the batch writer, whose null is the type's
  sentinel (`MinInt64`, `NaN`, the empty string, the nil blob), so a
  generated value is never a sentinel and a timestamp data column is
  dense: the null timespec is not settable through the writer, and a
  null-aware constructor is an upstream request. The fixture is a
  subpackage because `internal/qdb`'s own tests import `qdbtest`, and a
  `qdbtest` that imported `internal/qdb` would be a test import cycle.
  Every fixture table is removed on the test's cleanup, per
  `rapid.Check` iteration too; run `qdbsh` for `qdbtest_*` entries when a
  run was killed mid-way.
- qdbd's session pool is finite and exhaustion is punished: past its
  limit the test cluster logs `out of free sessions` and refuses new
  ones for fifteen minutes. Run the packages
  serially (`go test -p 1 ./...`), and bound the sessions every test
  holds open at every step -- closes in flight count, because a handle
  holds its qdbd sessions until `qdb_close` returns.
