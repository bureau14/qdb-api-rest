# internal/flightsql -- Agent Instructions

Scope: the Flight SQL server, the gRPC listener's interceptors and the
Handshake. The package-wide Go rules, the logging rules and the test
fixtures are in `internal/AGENTS.md`. ADR-0018 holds the wire contract.

## Server

- The gRPC server is built by hand (`grpc.NewServer`) and not through
  arrow-go's `flight.Server` helper, because the helper's shutdown is a
  bare `GracefulStop` with no forced stop after the grace, and
  `GracefulStop` has no deadline of its own. `Server.Shutdown` races
  `GracefulStop` against the caller's deadline and calls `Stop` when it
  ends.
- The interceptor chain is the access line outermost and the auth edge
  inside it, on the unary and the stream path alike, so a refused call
  is logged too. The access line carries no user tag, as the HTTP
  access line does not, because on both planes the bearer edge runs
  inside the logging edge. The user and the session are on the lines
  the handler logs below it.
- The access line is one `request` line per RPC with the method, the
  gRPC code and the duration, in the HTTP line's vocabulary, under a
  request id minted per call. The id is never read from the metadata,
  because no stock driver sends one by default and the inbound-id
  validation lives in `httpapi`.
- The call context gets the process values (the logger, the cluster and
  the keychain) through `callContext`, one explicit line per value,
  because grpc-go has no `BaseContext` as `net/http` has. A merged
  context type was rejected as less explicit about what a handler may
  read.
- The server embeds `flightsql.BaseServer`, so a command this package
  does not implement answers `UNIMPLEMENTED` without a line of code
  here.

## Auth

- Every call runs under `authenticate`'s one rule: no `authorization`
  metadata is the anonymous user on an insecure cluster and
  `UNAUTHENTICATED` on a secure one, and a present header must be a
  Bearer carrying a verifiable access token, with `typ != access`
  refused as the HTTP edge refuses it. The rule is this package's own
  interceptor and not arrow-go's basic-auth middleware, because that
  middleware refuses every call without a header, and the stock ADBC
  and JDBC drivers send none unless a username or a token is
  configured (ADR-0018, Decision 8; owner, 2026-10-20).
- The edge enriches and handlers do not: the claims ride the ctx, zero
  claims for the anonymous user so `auth.ClaimsFrom` never panics, and
  the logger carries `observe.KeyUser` and `observe.KeySession`.
- The Handshake takes `authorization: Basic base64(user:secret)` and
  answers `authorization: Bearer <access token>` in the reply's headers,
  the form arrow-go's client and the ADBC and JDBC drivers send and
  read. The token goes out through `SendHeader` and not `SetHeader`,
  because a header that is set and never flushed leaves with the status
  as a trailers-only reply, which the JDBC driver does not read. An
  empty username is the anonymous login, as the HTTP login treats it.
- A Handshake answers the access token only. Flight has no refresh
  path, and a client whose token expired reconnects (ADR-0018,
  Decision 6).
- A login's codes (ADR-0018, Decision 13): a refused credential is
  `UNAUTHENTICATED`, an open breaker or an unreachable cluster is
  `UNAVAILABLE` with no retry-after in the detail, a caller that went
  away is `CANCELED`, and a mint failure is `INTERNAL`.

## Tests

- One test file, `flightsql_test.go`, drives arrow-go's Flight SQL
  client against the server on a port of its own with the ephemeral
  certificate and verification off, as the driver smoke tests connect.
  `TestHandshake` runs on both clusters through `cluster.Each`, and
  `TestBearerRows` on the insecure cluster alone with a fixed clock,
  because none of its rows reaches the cluster. Tokens are minted
  directly, as the HTTP handler tests mint them.
