# internal/httpapi -- Agent Instructions

Scope: the v2 HTTP handlers, the middleware and the probes. Package-wide
Go rules, logging and the test fixtures: `internal/AGENTS.md`. The wire
contract of the query endpoint and the v2 error shape: ADR-0010; of the
login: ADR-0011; of the table routes: `docs/brief.md`, "Tables: create
and delete"; of the table reader: this file, Handlers.

## Handlers

- A handler reads the logger, the cluster, the keychain and the claims
  from the request context; nothing is injected. `auth.ClaimsFrom`
  panics on a route registered without `requireBearer`.
- Every v2 error goes through `writeProblem`, one RFC 9457 body. A
  header that belongs to the status (`WWW-Authenticate`, `Retry-After`)
  is set before the call. Never a 200 with an error body.
- The status is decided before the first byte. `Cluster.Query` is
  called with `WithReadRetry` because nothing has been sent yet; a
  handler that has started streaming never retries.
- The error mapping is by who failed, ADR-0010's table, in one place,
  `writeClusterError`: a `*qdb.BreakerOpenError` or
  `qdb.IsClusterUnavailable` is 503 (the breaker's `RetryAfter` on the
  header, nothing otherwise); any other cluster error is the caller's,
  at the status the handler passes (400 for a query, 401 for a login)
  with the binding's message as the detail; the caller's own context
  ending gets nothing on the wire and one debug line; 500 is reserved
  for this process.
- Every body is read through `readBody`, one cap for all of them.
- A route whose resource is a name refines the caller's 400 in its own
  handler, before `writeClusterError` and never inside it: the create
  answers 409 on `qdb.IsTableExists`, the delete 404 on
  `qdb.IsTableNotFound`. A query that names an unknown table stays 400.
  The handlers name no binding error; `internal/qdb` classifies.
- The create checks the body's shape only: JSON, a present
  `shard_size` that fits a duration. The column vocabulary and the
  symtable rule are `internal/qdb`'s (`ErrInvalidColumn`, found before a
  session is leased); names and sizes are the C API's. A create or a
  delete is never retried.
- The login proves credentials by `Cluster.Authenticate`, one direct
  dial outside the pools, and mints only for credentials the cluster
  accepted; a refusal is 401 with no `WWW-Authenticate`, since no
  bearer scheme was used. The response is RFC 6749's token response
  (`access_token`, `token_type`, `expires_in`); the TTL is
  `auth.access_ttl` through `Tokens.AccessTTL`, never a clock read in
  the handler.
- The batch is released on return; a nil batch (a statement without a
  result set) has nothing to release and encodes as empty.
- The table reader, `GET /api/v2/tables/{name}/rows`: `?columns=a,b`
  answers exactly those fields in that order (`$table` and `$timestamp`
  only when named; every column, the two first, otherwise);
  `?start=&end=` in RFC 3339, both or neither, `[start, end)`, the pair
  judged by the binding; `Accept` negotiated as the query's. The read
  runs inside the sink: the status is decided before it (404 on
  `qdb.IsTableNotFound`, 400 for a bad range, an unknown column and
  anything else the cluster answered, through `writeClusterError`), the
  sink sets `Content-Type` and runs `EncodeStream` through the byte
  counter, and the session is held until the client has read. The
  sink's error is kept apart from the call's: with zero bytes out it is
  500, after the first byte the stream is cut with one warning line,
  and the sink still returns it so the breaker hears of a fetch that
  found the cluster gone. Never retried. An empty table answers its
  schema in every format.
- The ingest, `POST /api/v2/rows`: `Content-Type` `text/csv` (415
  otherwise), the body streamed into `Cluster.IngestCSV` under a 64 MiB
  `MaxBytesReader` (413 surfaces from the parse), never read whole;
  `?push-mode=transactional|fast|async` (default `fast`),
  `?deduplication-mode=drop|upsert` with `?deduplication-columns=a,b`
  required by either. 200 with `{"rows","tables","parse_ms","push_ms"}`,
  `push_ms` under `async` the time until the push call returned; a
  header-only body is 200 with zeros and no push. `qdb.ErrInvalidRows`
  and `qdb.ErrInvalidPushOptions` are 400; a `$table` the cluster does
  not know is 404 and fails the whole request; the rest is
  `writeClusterError` at 400. Never retried.
- No flushing writer: the encoder's own buffer and `net/http`'s chunking
  already stream. The handler wraps the response in a byte counter only,
  so an encode error with zero bytes out is a problem response and with
  bytes out is a log line.
- `Accept` is matched by media type alone, first listed match wins,
  JSON otherwise; `q` weights are not read.
- Unknown paths and wrong methods keep the stdlib mux's plain-text 404
  and 405.

## Middleware

- `withRequestLogging` wraps the mux once; `requireBearer` and
  `withCompression` wrap a route. The probes stay outside both; the
  login is compressed but unauthenticated.
- Compression (ADR-0012): `Accept-Encoding` read in the client's order,
  first of `gzip`, `zstd`, `identity` wins, no `q`; the writer holds the
  status back until the first body byte, then labels and compresses, so
  a bodiless status goes out bare and a problem body compresses like a
  result. Fastest level, one compressor per response, no knob. The
  handler's `countingWriter` counts bytes into the compressor and the
  access line counts bytes on the wire.
- The edge enriches, handlers do not: `requireBearer` places the claims
  on the ctx and tags the logger with `observe.KeyUser` (the username
  as the token carries it, empty for anonymous) and `observe.KeySession`
  (the `sid` claim).

## Tests

- The handler tests run through `NewHandler` with a context carrying a
  discarding logger, a fixture cluster (`qdbtest/cluster`, insecure by
  default, `newServerOn` for the secure one or an unreachable URI) and
  a keychain from `config.Default().Auth`, which is ephemeral and pays
  no argon2id cost; the query tests mint their token directly, only
  the login tests go through the login.
- The good path of the v2 surface is one round trip, `TestRoundtrip`
  (`roundtrip_test.go`), the in-process twin of the e2e flow: one to three
  generated tables of one column list are created over HTTP, the first
  read empty in every format, all ingested in one body, read (whole and
  under a drawn column subset) and queried in every format, deleted,
  re-created over the symtables the delete leaves, deleted again. The
  oracle is the encoder run directly over `Cluster.Read` or
  `Cluster.Query`, byte for byte, and `table.Check` on the direct read
  against the rows generated; the encoders' own tests prove the bytes
  decode. A new route lands as a step of the round trip, not as a property of
  its own. The empty read runs on one table only: an empty read through
  the bulk reader costs about a tenth of a second where a filled one
  costs milliseconds.
- Every error row of every route is one table, `TestErrorRows`
  (`errors_test.go`): a request, its status, its challenge, a problem
  body. A new route's rows join the table. The login verdicts, the
  bearer edge and the codings keep their own tests: they need the
  secure cluster, a fixed clock or a decompressor.
- The bearer edge is pinned with a fixed clock passed to `auth.New`.
