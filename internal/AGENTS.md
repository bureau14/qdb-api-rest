# internal/ -- Agent Instructions

Scope: every Go package under `internal/`. The package layout and what
each package owns are in `docs/brief.md`, "Project structure". Hard
decisions are in `docs/adr/`.

## Code

- Book order: definitions come before use, so a reader does not scroll
  up.
- No package-level mutable state. `context.Context` is the first
  parameter of anything that does I/O, logs, or can be cancelled.
- Small composable functions with descriptive names, and explicit over
  implicit. Comments follow the root `AGENTS.md`, "Code comments": the
  Go doc comment is the contract, and the narrative lives in the body.
  A negation belongs in a comment only where it records a rejected
  alternative or an invariant. A comment does not correct what an
  earlier version claimed.
- Validation has one home, and a rule is not duplicated at a second
  layer. `internal/config` owns the shape of every value, which is
  parsing, layer folding and the vocabulary of enumerated keys. It also
  owns the simple bounds a value must meet for the server to serve,
  such as a count of at least one or a size that is not negative, so
  that startup refuses a configuration that cannot serve and names the
  key (`validatePool`, `validateDuckDB`).
- A check that needs I/O or another component's knowledge stays with
  the component that does the work, and no eager copy of it runs at
  startup. `tlsconf` owns the certificate-pair rule, the C API judges
  the dial options at dial, `internal/auth` judges its passphrases and
  argon2id costs, and DuckDB judges a spill directory when an instance
  opens.
- The v1 compatibility code lives in `internal/httpapi/v1` and nowhere
  else (ADR-0007). That is the v1 wire surface: the wrapper handlers,
  the wart encoders and the v1 token extraction. The package imports
  `internal/httpapi` for the v2 core, and the binary's entry point
  composes the two, so `internal/httpapi` does not import it. A v1
  route wraps its v2 counterpart and does not reimplement it. Inside
  the package the names say v1 too (`writeV1JSON`, not a bare
  `writeJSON`). Outside the package, no code knows a wart exists.
- A query result outside `internal/qdb` is the Arrow record batch the
  binding builds through `qdb_query_arrow`, and nothing else.
  `Cluster.Query` hands it to a sink as a sequence of that one batch
  (`qdb.Batches`), as `Cluster.Read` and `Cluster.Duck` do, so every
  result in the binary is read one way. The session is back in its pool
  before the sink runs, and the batch outlives the session. The sink
  borrows the batch, and `Query` releases it after the sink returns, so
  a sink that never ranges over the sequence leaks nothing and a sink
  that keeps the batch retains it. Its buffers are C-allocated and that
  release frees them. On any error the sink does not run. A partial
  batch is released inside `internal/qdb`. A statement without a result
  set is one empty batch with no columns. The binding's row-major and
  columnar result paths are not called.
- A table read is a sequence of such batches (`qdb.Batches`), one per
  fetch of the bulk reader, handed to a sink while the session is held.
  `Cluster.Read` leases one session of the caller's pool for as long as
  the sink runs, and it is not retried. The sink borrows each batch
  until yield returns, and the sequence releases it then, so a value
  that outlives the loop body is copied (a `Value` of an Arrow array
  aliases the batch's buffers). A missing table, a bad range and
  an unknown column fail before the sink runs. An empty table is one
  empty batch of its schema, built from `ColumnsInfo`, so the sequence
  is never empty. The Arrow type of a column type is the binding's
  `TsColumnType.ArrowType` (a symbol reads as a string). The server
  declares no map of its own. Two C API defects surface here without a
  workaround, filed as sc-19829 and sc-19830. The first: the Arrow path
  drops one trailing NUL byte from a string cell, and a canary in
  `internal/qdb/read_test.go` fails when it is fixed. The second: two
  symbol-bearing tables in one reader fail, so a read is one table.
- An ingest is one push per request through one held session.
  `Cluster.Ingest` leases one session of the caller's pool for the whole
  body. It lends the decoder a schema lookup bound to that session
  (`Session.schemaOf`, which answers the reader's whole-table schema),
  and it pushes the decoded batches through the session in one
  `ArrowWriter` call. The push is not retried. The body decodes in
  `internal/encoding` into one `model.TableBatch` per table, typed the
  first time a row names the table. `internal/qdb` releases every batch
  once the push has returned. The tables of one body share one column
  list, names and types: the header's data columns, typed by each
  table. The decoder refuses a table whose types differ from the
  first's, because the Arrow writer checks one table at a time. A
  body's failure is the whole request's failure, found before the push,
  and nothing is written. The failures are a header without `$table` or
  `$timestamp`, a name a table lacks, a field that does not parse, an
  empty `$timestamp` or `$table`, tables of differing types
  (`encoding.ErrInvalidRows`), and an unknown table (`IsTableNotFound`).
  The header's data columns are the columns written. A column the
  header lacks is null in every row. The empty field is null, and the
  server stores a zero-length string or blob as null whichever writer
  sent it, so the empty string cannot be ingested. Push and
  deduplication options are judged before the lease
  (`ErrInvalidPushOptions`). Either deduplication mode needs its
  columns. After an `async` push a query sees the rows at once, and the
  bulk reader sees them only once the server has flushed. Before that
  flush the reader answers none of them, or some twice while the flush
  runs (qdbd 3.15.0.dev0, the Arrow and the sentinel writer alike).
  `fast` and `transactional` are visible on both paths when the push
  returns. No format is QuasarDB's. The package's story is the lease,
  the type map and the push.
- A DuckDB call runs DuckDB SQL on the caller's own DuckDB instance
  (`Cluster.Duck`, ADR-0017). The instance lives on the user's pool
  state, opens on the user's first DuckDB call and counts against
  `duckdb.max_instances` from before its open until its close returns.
  Past the cap the call is `InstanceLimitError`, whose `RetryAfter` is
  the eviction tick. A DuckDB call is use of the pool for as long as it
  runs, and an idle instance does not keep a pool, so the instance
  closes with its pool after `pool.idle_timeout`. A DuckDB call is never
  retried, does not feed the breaker, and takes no unit of the budget,
  because qdb-duck pools its own qdbd sessions. On an insecure cluster
  every instance attaches anonymously. The result is the lent
  `Batches`, as a table read's is.
- A query string reaches an engine through `query.Run` (`internal/query`),
  which alone recognizes the `#sql` prefix (ADR-0018). The HTTP query
  route calls it, and the Flight server will. It calls `Cluster.Query`
  with `WithReadRetry` or `Cluster.Duck`, which take the same sink.
- On a secure cluster, a dial that fails other than by an unreachable
  cluster is `qdb.ErrCredentialsRefused`, whether a user pool, the login
  or the classification of a failed DuckDB attach dialed. The
  sentinel's comment says why the dial alone classifies. The refusal is not retried, and it is an answer to the breaker. The
  data routes answer it 401 with the `invalid_token` challenge
  (ADR-0010), and the login answers it 401 without a challenge
  (ADR-0011).
- The neutral table types (`TableBatch`, `SchemaOf`) live in
  `internal/model`. `internal/encoding` and `internal/qdb` both import
  it, and it imports neither. Open `internal/model/AGENTS.md` before
  adding a type there.
- Encoders live in `internal/encoding`. Open `internal/encoding/AGENTS.md`
  before touching an encoder, a wire shape or the text of a cell.
- The embedded DuckDB engine lives in `internal/olap`. Open
  `internal/olap/AGENTS.md` before touching an instance, its attach or
  its query.
- The v2 handlers, the problem body and the bearer middleware live in
  `internal/httpapi`. Open `internal/httpapi/AGENTS.md` before touching
  a handler, an error mapping or a route.
- A statistics snapshot is named after what it describes, `FooStats`
  (`ClusterStats`, the binding's `SessionPoolStats`), and not a bare
  `Stats`. A bare `Stats` exists only as the type that composes every
  `FooStats` of its package.

## Building

- The binary compiles through cgo against two extracted packages that a
  developer machine unpacks by hand: the C API in `qdb/`, which the
  vendored `qdb-api-go` behind `internal/qdb` needs, and qdb-duck's
  package in `duck/`, which the vendored `duckdb-go` behind
  `internal/olap` needs. Run `source .envrc` (or direnv) before a bare
  `go build` or `go test`. The root Makefile does it for you. `.envrc`
  tells cgo where both packages are and exports the DuckDB build tags
  through `GOFLAGS`. Without it the compile fails on missing qdb
  headers, and the binding compiles against the stubs of `third_party/`
  and fails to link. `qdb-api-go` is never patched in `vendor/`. Fix it
  upstream, then bump the version in `go.mod` (`go get ...@<commit>`, a
  commit on upstream `master`; a branch commit lives only as long as
  the work that needs it), and re-run `go mod tidy` and `go mod
vendor`. Nothing under `vendor/` is written by hand. A branch commit
  of a PR that is squash-merged does not reach `master`, so the work
  that vendored it re-points `go.mod` at the squash commit before it
  merges.

## Logging (ADR-0002)

- Log through the context, with the `*Context` methods only:
  `observe.Logger(ctx).InfoContext(ctx, msg, attrs...)`. Never
  `slog.Default()` or `slog.Info` (`make lint` rejects them). The
  logger is state, because it carries attributes, and it is passed
  along by value, in the ctx wherever there is one. A component that
  has no ctx at call time (the `qdb-api-go` logger adapter) holds the
  logger it was given. `Logger` panics on a ctx without a logger, so
  pass the ctx you were given, never `context.Background()`.
- The context is where request-scoped and process-scoped values travel:
  the logger (`observe`) and the cluster (`qdb.WithCluster` and
  `qdb.ClusterFrom`, which panics without one, like `Logger`). Handlers
  read them from the request context. Nothing is injected through a
  constructor that the context already carries. Composing the v1
  routes (ADR-0007) is the entry point's job. That is composition, not
  state.
- Scope attributes with `observe.WithAttrs(ctx, ...)` and pass the child
  ctx down. The caller's ctx stays untagged.
- Keys come from `observe.Key*`, and errors go through
  `observe.Err(err)`. Add a key to `observe` before using it in a second
  package.
- Edges enrich and handlers do not: HTTP middleware and gRPC
  interceptors tag the ctx (request id, user, session), and the code
  below them only logs.
- Any type that holds a secret implements `slog.LogValuer`, so it can
  never print one.

## Tests

- Pin genuine logic only, and write no test for glue. Tests are
  white-box, in the same package, with small helpers declared before
  use and `t.Helper()`. The exceptions are
  `internal/qdb/read_test.go` (`package qdb_test`) and
  `internal/olap/olap_test.go` (`package olap_test`). Both need the
  table fixture, which imports `internal/qdb`, and `internal/qdb`
  imports `internal/olap`. Test bodies carry step
  comments (root `AGENTS.md`, "Code comments") wherever a step's
  purpose is not evident from the assertion.
- Data-shaped behaviour gets property tests (`pgregory.net/rapid`).
  Wire-shaped behaviour gets the e2e harness (`tests/e2e/`).
- The tests are not in the business of testing the C API, which offers
  no way to mock an error or a stall. Never add a listener that stalls
  `connect`, and never mock the binding.
- The tests that dial a live qdbd use the pair from
  `scripts/tests/setup/start-services.sh`, insecure `2836` and secure
  `2838`, so a bare `go test ./...` needs those services up. The fixture
  has one home, `internal/qdbtest`. `qdbtest.Cluster` describes each
  cluster (its URI, its key files and the user a test acts as),
  `Clusters` lists both, `Config` binds a configuration to one, and
  `Require` fails fast with the start hint when a port does not answer.
  Nothing is skipped under `-short`.
- Every test that dials qdbd runs once per cluster, as subtests named
  `insecure` and `secure`, through `cluster.Each`, or over
  `qdbtest.Clusters` inside `package qdb`, which cannot import the
  cluster fixture. A test acts as the anonymous user on the insecure
  cluster and as the test user on the secure one. `TestLoginVerdicts`
  runs on the secure cluster alone, because the insecure cluster
  accepts any secret, so its refusal has no insecure twin. A copy of
  the cluster pair in each package was rejected, because the fixture
  has one home. One CI run per cluster through an environment variable
  was rejected, because it needs a second CI step and runs the tests
  that dial no qdbd twice.
- A test that needs rows draws a table with `internal/qdbtest/table`
  (`Generate`, then `Create` on a `*qdb.Cluster`). The table holds its
  rows as one record batch in the binding's Arrow types and pushes it
  through the Arrow writer. The test compares what came back with the
  `Table` it holds and writes no loader of its own. `Create` and
  `RemoveOnCleanup` run as the anonymous user, and `CreateAs` and
  `RemoveOnCleanupAs` take the user for a cluster that refuses the
  anonymous user. A test that creates
  or pushes through its own door (an HTTP route) takes the blocks
  `Create` stacks on, which are `GenerateSchema`, `GenerateLike`
  (another table of the same columns) and `RemoveOnCleanup`. It does
  not copy them. A body of rows is the format's encoder run over
  `table.Body` (several tables as one batch, `$table` per row) or
  `table.WithTable` (one table). The fixture cannot import
  `internal/encoding`, because the encoding tests are white-box and
  import the fixture, which would be a test import cycle. What came
  back is compared with `table.Check` (a record batch, column by column
  by name, `$table` and `$timestamp` included) or `table.CheckColumn`,
  by array equality. No test carries a comparer of its own. A test
  that reads a generated table passes the table's span (`table.Range`)
  as the read's range, because a bulk reader opened on a table with no
  rows under no range probes every shard of a forever range, and the
  owner chose the bound over a qdbd story. The draw
  does not produce `MinInt64`, `NaN`, the empty string or the empty
  blob, which the server reads back as null whichever writer sent them.
  It does not produce a NUL either, which the bulk reader drops from the
  end of a string (sc-19829). Strings draw the characters the text wires
  quote, and symbols draw only what a symtable takes. The table fixture
  and the cluster fixture (`internal/qdbtest/cluster`, `Bind`, `Each`
  and `EachWith`) are subpackages, because `internal/qdb`'s own tests
  import `qdbtest`, and a `qdbtest` that imported `internal/qdb` would
  be a test import cycle. Every fixture table is removed on the test's
  cleanup, per `rapid.Check` iteration too. Run `qdbsh` for `qdbtest_*`
  entries when a run was killed mid-way.
- qdbd's session pool is finite and exhaustion is punished. Past its
  limit the test cluster logs `out of free sessions` and refuses new
  ones for fifteen minutes. Run the packages serially
  (`go test -p 1 ./...`), as the CI test step does, because every
  package that dials qdbd shares the two clusters, and one test's
  effect on a cluster can fail a test in another package. Parallel
  packages would be safe only against a qdbd per package, which brings
  problems of its own. Running the tests that dial no qdbd in parallel
  was rejected, because orchestrating the split is hard and not worth it
  (owner, 2026-10-19). Bound
  the sessions every test holds open at every step. A close in flight counts, because a handle holds its
  qdbd sessions until `qdb_close` returns.
- Three qdbd defects are filed with the QuasarDB R&D team. This project
  does not fix them, and the tests carry no workaround for them. They
  explain test failures that a re-run of the job clears.
  - sc-19942: qdbd crashes at random in its streamvbyte decoder. A test
    that is talking to qdbd at that moment fails, and the qdbd log ends
    mid-flush. The Windows build steps hit it under `TestRoundtrip`.
  - sc-19944: once the `int64` values of one column in one bucket sum to
    `MinInt64` under two's complement wrapping, qdbd cannot load that
    bucket, and every later push into it fails with "Invalid reply from
    the remote host". `TestRoundtripDeduplicated` pushes its rows twice
    and draws such a bucket at random.
  - sc-19939: a debug assertion in qdbd. It causes no failure in this
    project.
