# internal/httpapi -- Agent Instructions

Scope: the v2 HTTP handlers, the middleware and the probes. The
package-wide Go rules, the logging rules and the test fixtures are in
`internal/AGENTS.md`. ADR-0010 holds the wire contract of the query
endpoint and the v2 error shape. ADR-0011 holds the login's, and
ADR-0016 the refresh's, the logout's and the session's. ADR-0017
holds DuckDB's, which the query endpoint reaches behind `#sql`. The brief
holds the table routes' (`docs/brief.md`, "Tables: create and delete").
This file, Handlers, holds the table reader's.

## Handlers

- A handler reads the logger, the cluster, the keychain and the claims
  from the request context, and nothing is injected. `auth.ClaimsFrom`
  panics on a route registered without `requireBearer`.
- Every v2 error goes through `writeProblem`, which writes one RFC
  9457 body. The handler sets a header that belongs to the status
  (`WWW-Authenticate`, `Retry-After`) before the call. No handler
  answers 200 with an error body.
- The status is decided before the first byte. `query.Run` calls
  `Cluster.Query` with `WithReadRetry` because the query completes
  before the sink runs and nothing has been sent yet. A handler that has
  started streaming never retries.
- The error mapping is by who failed, which is ADR-0010's table, and it
  lives in one place, `writeClusterError`. A `*qdb.BreakerOpenError`, a
  `*qdb.InstanceLimitError` or `qdb.IsClusterUnavailable` is 503, with
  the breaker's or the cap's `RetryAfter` on the header and no header
  otherwise. Any other cluster error is the
  caller's, at the status the handler passes (400 for a query, 401 for
  a login) with the binding's message as the detail. The caller's own
  context ending gets nothing on the wire and one debug line. 500 is
  reserved for this process.
- Every body but the ingest's is read whole through `readBody`, under
  one cap. The ingest streams its body under a cap of its own
  (`maxIngestBytes`).
- A route whose resource is a name refines the caller's 400 in its own
  handler, before `writeClusterError` and never inside it. The create
  answers 409 on `qdb.IsTableExists`, and the delete answers 404 on
  `qdb.IsTableNotFound`. A query that names an unknown table stays 400.
  The handlers name no binding error, because `internal/qdb`
  classifies.
- The create checks the body's shape only, which is JSON with a present
  `shard_size` that fits a duration. The column vocabulary and the
  symtable rule are `internal/qdb`'s (`ErrInvalidColumn`, found before a
  session is leased), and names and sizes are the C API's. A create or
  a delete is never retried.
- The login proves credentials by `Cluster.Authenticate`, which is one
  direct dial outside the pools, and it mints only for credentials the
  cluster accepted. A refusal is 401 with no `WWW-Authenticate`, because
  no bearer scheme was used. The response is RFC 6749's token response
  (`access_token`, `token_type`, `expires_in`, `refresh_token`), written
  by `writeTokenResponse`, which the refresh shares. `expires_in` is the
  access lifetime through `Tokens.AccessTTL`, and the handler reads no
  clock.
- The refresh is `POST /api/v2/auth/refresh`, `{"refresh_token"}` under
  the login's content type and cap. It verifies, requires `typ`
  `refresh`, renews through `Tokens.Renew` and answers the login's
  object. It never dials, so it has no 503. Its 401s carry the bearer
  challenge through `unauthorized`, and the detail says which check
  failed.
- The logout is `POST /api/v2/auth/logout` behind the bearer edge, 204,
  no body, no state.
- The session is `GET /api/v2/session` behind the bearer edge. It
  answers the claims, `Cluster.URI` and `Secured`, and `build.Version`.
  Times are the claims' unix seconds. It makes no cluster call.
- The query endpoint runs its body through `query.Run`, which calls
  `Cluster.Query` with `WithReadRetry` for a native body and
  `Cluster.Duck` for a `#sql` body, and streams the result inside the
  sink as the reader does. A statement without a result set is one
  empty batch with no columns, which encodes as an empty result. The
  JSON body of every result route is the array of results (ADR-0010).
- The table reader is `GET /api/v2/tables/{name}/rows`. `?columns=a,b`
  answers those fields in that order, with `$table` and `$timestamp`
  only when named. Without `?columns` it answers every column, the two
  first. `?start=&end=` are RFC 3339, both or neither, and the range is
  `[start, end)`, with the pair judged by the binding. `Accept` is
  negotiated as the query's. The read runs inside the sink. The status
  is decided before the sink runs: 404 on `qdb.IsTableNotFound`, and 400
  for a bad range, an unknown column and anything else the cluster
  answered, through `writeClusterError`. The sink sets `Content-Type`
  and runs `EncodeStream` through the byte counter, and the session is
  held until the client has read. The sink's error is kept apart from
  the call's. With zero bytes out it is 500. After the first byte the
  stream is cut with one warning line, and the sink still returns the
  error, so the breaker hears of a fetch that found the cluster gone. A
  read is never retried. An empty table answers its schema in every
  format.
- A `#sql` body on the query endpoint is DuckDB SQL over the cluster
  attached as `qdb`, run through `Cluster.Duck` on the caller's own
  DuckDB instance, with the prefix stripped by `query.Run`. The result
  streams as the reader's does, inside the sink, with the same split
  between the sink's error and the call's. DuckDB's parse, bind and
  execution errors arrive before the sink and are 400. A body of several
  statements runs every statement and answers the last statement's
  result (ADR-0018, Decision 12), the vendored driver's convention,
  which no test of this repository pins.
  The instance cap is 503 with `Retry-After`, and a failed
  attach is 503 without it. A column type no text encoder writes (a
  list, for one) is 500 before the first byte, and
  `internal/encoding/AGENTS.md` lists the types the text encoders write.
  A caller casts in SQL to one of those types or asks for Arrow. A DuckDB call is never retried.
- The ingest is `POST /api/v2/rows`. `Content-Type` picks the decoder
  from `decoders` by media type, and any other type is 415, naming the
  types accepted. The body streams into the decoder under a 64 MiB
  `MaxBytesReader` through `Cluster.Ingest`, and 413 surfaces from the
  decode. The body is never read whole.
  `?push-mode=transactional|fast|async` defaults to `fast`.
  `?deduplication-mode=drop|upsert` requires
  `?deduplication-columns=a,b` in either mode. The answer is 200 with
  `{"rows","tables","parse_ms","push_ms"}`, where `push_ms` under
  `async` is the time until the push call returned. A header-only body
  is 200 with zeros and no push. `encoding.ErrInvalidRows` and
  `qdb.ErrInvalidPushOptions` are 400. A `$table` the cluster does not
  know is 404 and fails the whole request. The rest goes through
  `writeClusterError` at 400. An ingest is never retried.
- `Content-Encoding: gzip|zstd` on the ingest is decompressed before
  the decoder reads the body, and any other coding, a list included, is
  415 naming the two. The cap bounds the bytes on the wire, so the
  decoded size of a hostile body is not bounded, because the brief
  excludes public-internet hardening. A corrupt compressed body is 400,
  through `ErrInvalidRows` once the decoder reads it or through the
  gzip reader's open.
- There is no flushing writer, because the encoder's own buffer and
  `net/http`'s chunking already stream. The handler wraps the response
  in a byte counter only, so an encode error with zero bytes out is a
  problem response and one with bytes out is a log line.
- `Accept` is matched by media type alone. The first listed match wins,
  and JSON is the default. `q` weights are not read.
- Unknown paths and wrong methods keep the stdlib mux's plain-text 404
  and 405.

## Middleware

- `withRequestLogging` wraps the mux once. `requireBearer` and
  `withCompression` wrap a route. The probes stay outside both. The
  login and the refresh are compressed but unauthenticated.
- Readiness reads nothing of the serving lifecycle. The drain closes
  the listeners before anything else (`cmd/qdb_rest/AGENTS.md`), so a
  prober's next connection is refused (ADR-0004, rule 2).
  The entry point places the signal (`cmd/qdb_rest/AGENTS.md`), and the
  Flight server does not, because no Flight call reads it.
- Compression follows ADR-0012. `Accept-Encoding` is read in the
  client's order, the first of `gzip`, `zstd` and `identity` wins, and
  `q` is not read. The writer holds the status back until the first body
  byte, then labels and compresses, so a bodiless status goes out bare
  and a problem body compresses like a result. The level is the fastest,
  there is one compressor per response, and there is no knob. The
  handler's `countingWriter` counts bytes into the compressor, and the
  access line counts bytes on the wire. The request side is
  `newDecompressor`, the inverse of `newCompressor`, with zstd at
  concurrency one so a request spawns no goroutines.
- The edge enriches and handlers do not. `requireBearer` places the
  claims on the ctx and tags the logger with `observe.KeyUser` (the
  username as the token carries it, empty for anonymous) and
  `observe.KeySession` (the `sid` claim).

## Tests

- The handler tests run through `NewHandler` with a context carrying a
  discarding logger, a fixture cluster and a keychain. `newServer` takes
  a `cluster.Fixture` and acts as its user, every handler test that
  dials qdbd runs on both clusters through `cluster.Each`, and
  `newServerOn` takes an unreachable URI. The keychain comes from
  `config.Default().Auth`, which is ephemeral and pays no argon2id cost.
  The query tests mint their token directly, and only the login tests go
  through the login. The fixture keeps its `*auth.Tokens` so a test mints
  what a row needs (an expired refresh token).
- The good path of the v2 surface is one round trip, `TestRoundtrip`
  (`roundtrip_test.go`), which is the in-process twin of the e2e flow.
  One to three generated tables of one column list are created over
  HTTP. The first is read empty in every format. All are ingested in one
  body of a drawn format, under a drawn request coding and a drawn push
  mode, read whole and under a drawn column subset, queried and read through DuckDB behind `#sql` on the query route
  in every format, deleted, re-created over the
  symtables the delete leaves, and deleted again. Under `async` the
  read-back is the query's alone, because a query sees the rows at once
  and the bulk reader, which qdb-duck's scan reads through, only after
  the server's async flush. The oracle is
  the encoder run directly over `Cluster.Read`, `Cluster.Query` or
  `Cluster.Duck`, byte for byte, and `table.Check` on the direct read
  against the rows generated. The DuckDB step's Arrow body is compared
  decoded, because DuckDB leaves the value bytes under a null slot
  undefined. Step 6 reads every table over Flight too, native and `#sql`,
  under the refreshed HTTP access token, against the generated rows and
  the query route's Arrow body (`checkFlight`), so the format-equivalence
  property covers the Flight plane (ADR-0018, Decision 14). The server
  fixture starts the Flight listener beside the handler for it. The
  encoders' own tests prove the bytes decode. A new route
  lands as a step of the round trip and not as a property of its own.
  Every read of the round trip, over HTTP and in the direct oracle,
  passes the span of the rows the test wrote, through the fixture's
  `Range`, because the round trip reads its first table empty in every
  format and an unbounded read of a table with no rows is slow
  (`internal/AGENTS.md`, Tests, says why). The empty read runs on one
  table only, because the tables share one schema.
- The DuckDB scalars have one test, `TestDuckDBScalars`
  (`duckdb_test.go`). It posts one row of every scalar the text encoders
  write behind `#sql` to `/api/v2/query` and checks each column's type
  word and cell. The instance cap test is `TestDuckDBInstanceLimit`.
  The round trip does not reach these types, because its tables are
  QuasarDB tables.
- Every error row of every route is one table, `TestErrorRows`
  (`errors_test.go`), where a row is a request, its status, its
  challenge and a problem body. A new route's rows join the table. The
  secure subtest adds one row per data route for an anonymous token,
  which the secure cluster refuses, answered 401 `invalid_token`. The
  whole table runs on both clusters, because running only the rows that
  reach qdbd would need a classification of every row that nothing
  checks. The
  login verdicts, the bearer edge, the codings and the DuckDB instance cap keep their own tests, because they need the secure cluster, a fixed clock, a decompressor or a cluster of their own limit.
- The bearer edge is pinned with a fixed clock passed to `auth.New`.
