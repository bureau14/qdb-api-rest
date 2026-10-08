# internal/httpapi -- Agent Instructions

Scope: the v2 HTTP handlers, the middleware and the probes. The
package-wide Go rules, the logging rules and the test fixtures are in
`internal/AGENTS.md`. ADR-0010 holds the wire contract of the query
endpoint and the v2 error shape. ADR-0011 holds the login's. The brief
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
- The status is decided before the first byte. `Cluster.Query` is
  called with `WithReadRetry` because nothing has been sent yet. A
  handler that has started streaming never retries.
- The error mapping is by who failed, which is ADR-0010's table, and it
  lives in one place, `writeClusterError`. A `*qdb.BreakerOpenError` or
  `qdb.IsClusterUnavailable` is 503, with the breaker's `RetryAfter` on
  the header and no header otherwise. Any other cluster error is the
  caller's, at the status the handler passes (400 for a query, 401 for
  a login) with the binding's message as the detail. The caller's own
  context ending gets nothing on the wire and one debug line. 500 is
  reserved for this process.
- Every body is read through `readBody`, which caps all of them alike.
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
  (`access_token`, `token_type`, `expires_in`). The TTL is
  `auth.access_ttl` through `Tokens.AccessTTL`, and the handler reads no
  clock.
- The batch is released on return. A nil batch, which is a statement
  without a result set, has nothing to release and encodes as empty.
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
  login is compressed but unauthenticated.
- Compression follows ADR-0012. `Accept-Encoding` is read in the
  client's order, the first of `gzip`, `zstd` and `identity` wins, and
  `q` is not read. The writer holds the status back until the first body
  byte, then labels and compresses, so a bodiless status goes out bare
  and a problem body compresses like a result. The level is the fastest,
  there is one compressor per response, and there is no knob. The
  handler's `countingWriter` counts bytes into the compressor, and the
  access line counts bytes on the wire.
- The edge enriches and handlers do not. `requireBearer` places the
  claims on the ctx and tags the logger with `observe.KeyUser` (the
  username as the token carries it, empty for anonymous) and
  `observe.KeySession` (the `sid` claim).

## Tests

- The handler tests run through `NewHandler` with a context carrying a
  discarding logger, a fixture cluster and a keychain. The cluster comes
  from `qdbtest/cluster`, insecure by default, and `newServerOn` takes
  the secure one or an unreachable URI. The keychain comes from
  `config.Default().Auth`, which is ephemeral and pays no argon2id cost.
  The query tests mint their token directly, and only the login tests go
  through the login.
- The good path of the v2 surface is one round trip, `TestRoundtrip`
  (`roundtrip_test.go`), which is the in-process twin of the e2e flow.
  One to three generated tables of one column list are created over
  HTTP. The first is read empty in every format. All are ingested in one
  body under a drawn push mode, read whole and under a drawn column
  subset, queried in every format, deleted, re-created over the
  symtables the delete leaves, and deleted again. Under `async` the
  read-back is the query's alone, because a query sees the rows at once
  and the bulk reader only after the server's async flush. The oracle is
  the encoder run directly over `Cluster.Read` or `Cluster.Query`, byte
  for byte, and `table.Check` on the direct read against the rows
  generated. The encoders' own tests prove the bytes decode. A new route
  lands as a step of the round trip and not as a property of its own.
  The empty read runs on one table only, because an empty read through
  the bulk reader costs about a tenth of a second where a filled one
  costs milliseconds.
- Every error row of every route is one table, `TestErrorRows`
  (`errors_test.go`), where a row is a request, its status, its
  challenge and a problem body. A new route's rows join the table. The
  login verdicts, the bearer edge and the codings keep their own tests,
  because they need the secure cluster, a fixed clock or a decompressor.
- The bearer edge is pinned with a fixed clock passed to `auth.New`.
