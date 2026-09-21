# internal/httpapi -- Agent Instructions

Scope: the v2 HTTP handlers, the middleware and the probes. Package-wide
Go rules, logging and the test fixtures: `internal/AGENTS.md`. The wire
contract of the query endpoint and the v2 error shape: ADR-0010; of the
login: ADR-0011; of the table routes: `docs/brief.md`, "Tables: create
and delete".

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
- The query property draws a table per iteration and compares each
  format's body byte for byte with the encoder run directly over
  `Cluster.Query`; the encoders' own tests prove the bytes decode.
- The table property draws a schema with `table.GenerateSchema`,
  creates it over HTTP and reads it back through the query endpoint;
  `table.RemoveOnCleanup` removes what the create leaves behind.
- The bearer edge is pinned with a fixed clock passed to `auth.New`.
