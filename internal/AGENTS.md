# internal/ -- Agent Instructions

Scope: every Go package under `internal/`. Package layout and what each
package owns: `docs/brief.md`, "Project structure". Hard decisions:
`docs/adr/`.

## Code

- Book order: definitions before use; a reader never scrolls up.
- No package-level mutable state. `context.Context` is the first
  parameter of anything that does I/O, logs, or can be cancelled.
- Small composable functions with descriptive names; explicit over
  implicit. Comments follow the root `AGENTS.md`, "Code comments": the
  Go doc comment is the contract, the narrative lives in the body. A
  negation belongs in a comment only where it records a rejected
  alternative or an invariant, never to correct what an earlier version
  claimed.
- Validation has one home: the component that consumes a value owns its
  checks, made once, where a violation cannot get past them (`tlsconf`
  owns the certificate-pair rule; the C API judges the dial options at
  dial; `internal/auth` judges its passphrases and argon2id costs).
  `internal/config` owns only shape -- parsing, layer folding, the
  vocabulary of enumerated keys -- never a value's semantic bounds.
  Never duplicate a rule at a second layer, and add no eager check for
  what the consumer already rejects loudly.
- v1 compatibility code -- the v1 wire surface: wrapper handlers,
  wart encoders, v1 token extraction -- lives in
  `internal/httpapi/v1` and nowhere else (ADR-0007). That package
  imports `internal/httpapi` for the v2 core and the binary's entry
  point composes the two, so `internal/httpapi` never imports it; a
  v1 route wraps its v2 counterpart, never reimplements it. Inside
  the package names say v1 too (`writeV1JSON`, never a bare
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
- A table read is a sequence of such batches (`qdb.Batches`), one per
  fetch of the bulk reader, handed to a sink while the session is held:
  `Cluster.Read` leases one session of the caller's pool for as long as
  the sink runs and is never retried. The sink borrows each batch for
  its step; the sequence releases it when the step returns, so a value
  that outlives the step is copied (a `Value` of an Arrow array aliases
  the batch's buffers). A missing table, a bad range and an unknown
  column fail before the sink runs; an empty table is one batch of
  schema alone, built from `ColumnsInfo`, so there is always a step.
  Two C API defects surface here unworked-around, filed as sc-19829
  (the Arrow path drops one trailing NUL byte from a string cell; a
  canary in `internal/qdb/read_test.go` fails when it is fixed) and
  sc-19830 (two symbol-bearing tables in one reader fail; a read is one
  table).
- An ingest is one batch push per request through one held session:
  `Cluster.IngestCSV` leases one session of the caller's pool for the
  whole body, looks each table's columns up through it the first time a
  row names the table, and pushes through it; it is never retried. The
  tables of one body share one column list (the header's data columns,
  typed by each table; the binding's writer refuses a second table whose
  typed columns differ); a body's failure -- a header without `$table`
  or `$timestamp`, a name a table lacks, a field that does not parse, an
  empty `$timestamp`, tables of differing types (`ErrInvalidRows`), an
  unknown table (`IsTableNotFound`) -- is the whole request's, found
  before the push, and nothing is written. The header's data columns
  are the columns written; one the header lacks is null in every row.
  The empty field is the type's null sentinel, so the empty string
  cannot be ingested. Push and deduplication options are judged before
  the lease (`ErrInvalidPushOptions`); either deduplication mode needs
  its columns. The parsers of every body format live in `ingest.go`:
  no format is QuasarDB's, the package's story is the writer.
- Encoders live in `internal/encoding`; open `internal/encoding/AGENTS.md`
  before touching an encoder, a wire shape or a cell rendering.
- The v2 handlers, the problem body and the bearer middleware live in
  `internal/httpapi`; open `internal/httpapi/AGENTS.md` before touching
  a handler, an error mapping or a route.
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
  constructors that the context already carries. Composing the v1
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
  small helpers declared before use, `t.Helper()`. The one exception
  is `internal/qdb/read_test.go`, `package qdb_test`: it needs the
  table fixture, which imports `internal/qdb`. Test bodies carry step
  comments (root `AGENTS.md`, "Code comments") wherever a step's purpose
  is not evident from the assertion.
- Data-shaped behaviour gets property tests (`pgregory.net/rapid`);
  wire-shaped behaviour gets the e2e harness (`tests/e2e/`).
- The tests are not in the business of testing the C API, which offers
  no way to mock an error or a stall: never add a listener that stalls
  `connect`, and never mock the binding.
- `internal/qdb` and `internal/httpapi` tests dial a live qdbd (the pair
  from `scripts/tests/setup/start-services.sh`, insecure `2836` / secure
  `2838`), so a bare `go test ./...` needs those services up. The fixture
  has one home, `internal/qdbtest`: the URIs, the key files, the
  secure cluster's test user (`SecureUser`), and `Require`, which fails fast
  with the start hint when a port does not answer. Nothing is skipped
  under `-short`.
- A test that needs rows draws a table with `internal/qdbtest/table`
  (`Generate`, then `Create` on a `*qdb.Cluster`) and compares what came
  back with the `Table` it holds; it never writes its own loader. A
  test that creates or pushes through its own door (an HTTP route)
  takes the blocks `Create` stacks on, `GenerateSchema`,
  `GenerateLike` (another table of the same columns) and
  `RemoveOnCleanup`, never a copy of them; a body of rows comes from
  `table.CSV`, the one renderer of a `Table` in the CSV encoder's
  dialect. What came back is compared
  with `table.Check` (a record batch, column by column by name, `$table`
  and `$timestamp` included) or `table.CheckColumn`; no test carries a
  comparer of its own. The
  fixture pushes through the batch writer, whose null is the type's
  sentinel (`MinInt64`, `NaN`, the empty string, the nil blob,
  `NullTime`), so a generated value is never a sentinel. The table
  fixture and
  the cluster fixture (`internal/qdbtest/cluster`, `NewInsecure` and
  `NewSecure`) are subpackages because `internal/qdb`'s own tests import `qdbtest`, and
  a `qdbtest` that imported `internal/qdb` would be a test import cycle.
  Every fixture table is removed on the test's cleanup, per
  `rapid.Check` iteration too; run `qdbsh` for `qdbtest_*` entries when a
  run was killed mid-way.
- qdbd's session pool is finite and exhaustion is punished: past its
  limit the test cluster logs `out of free sessions` and refuses new
  ones for fifteen minutes. Run the packages
  serially (`go test -p 1 ./...`), and bound the sessions every test
  holds open at every step -- closes in flight count, because a handle
  holds its qdbd sessions until `qdb_close` returns.
