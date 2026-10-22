# internal/flightsql -- Agent Instructions

Scope: the Flight SQL server, the gRPC listener's interceptors and the
Handshake. The package-wide Go rules, the logging rules and the test
fixtures are in `internal/AGENTS.md`. ADR-0018 holds the wire contract.
Each rule below names the function whose comments carry its reason.

## Server

- The gRPC server is built by hand (`grpc.NewServer`) and not through
  arrow-go's `flight.Server` helper. `New` says why, and
  `Server.Shutdown` is the drain the helper could not give: a
  `GracefulStop` raced against the caller's deadline, then `Stop`.
- The interceptor chain is the access line outermost and the auth edge
  inside it, on the unary and the stream path alike. `New` says why,
  and the consequence is that the access line carries no user tag, as
  the HTTP access line does not.
- The access line is one `request` line per RPC with the method, the
  gRPC code and the duration, in the HTTP line's vocabulary, under a
  request id minted per call and never read from the metadata
  (`accessContext`).
- The call context gets the process values (the logger, the cluster and
  the keychain) through `callContext`, one explicit line per value,
  because grpc-go has no `BaseContext` as `net/http` has.
- The server embeds `flightsql.BaseServer`, so a command this package
  does not implement answers `UNIMPLEMENTED` without a line of code
  here.

## Auth

- Every call runs under `authenticate`'s one rule: no `authorization`
  metadata is the anonymous user on an insecure cluster and
  `UNAUTHENTICATED` on a secure one, and a present header must be a
  Bearer carrying a verifiable access token, with `typ != access`
  refused as the HTTP edge refuses it. Whether the anonymous user is
  admitted is the cluster's mode, read once in `New` and not per call.
  The rule is this package's own interceptor and not arrow-go's
  basic-auth middleware, because that middleware refuses every call
  without a header (ADR-0018, Decision 8; owner, 2026-10-20). The
  interceptor runs in both modes, because a presented token names the
  user whose pool, DuckDB instance and log lines the call uses.
- The edge enriches and handlers do not: the claims ride the ctx, zero
  claims for the anonymous user so `auth.ClaimsFrom` never panics, and
  the logger carries `observe.KeyUser` and `observe.KeySession`.
- The Handshake takes `authorization: Basic base64(user:secret)` and
  answers `authorization: Bearer <access token>` in the reply's headers,
  the form arrow-go's client and the ADBC and JDBC drivers send and
  read. `handshake` says why the token goes out through `SendHeader`.
  An empty username is the anonymous login, as the HTTP login treats
  it.
- A Handshake answers the access token only. Flight has no refresh
  path, and a client whose token expired reconnects (ADR-0018,
  Decision 6).
- A login's codes (ADR-0018, Decision 13) are `statusOf`'s, the one
  mapping the login and the statement share, with a refused credential
  as its answered code, `UNAUTHENTICATED`. A mint failure is `INTERNAL`,
  mapped in `login`.

## Statement

- GetFlightInfo for a statement answers one endpoint whose ticket is the
  query text verbatim, with no schema, no location and unknown totals;
  it runs nothing and keeps nothing. `GetFlightInfoStatement` says why
  the text travels unchanged (ADR-0018, Decision 9; owner, 2026-10-21).
- DoGet for a statement ticket is this package's own method on the
  registered service, and every other ticket goes to arrow-go's router.
  The query runs through `query.Run` inside the sink, and the sink writes
  each lent batch to the stream through arrow-go's Flight writer, as the
  HTTP handler encodes inside its sink. `DoGet` says why the hook with a
  channel was rejected. A statement is never retried once the stream has
  started.
- A batch on the Flight wire is the producer's batch: `model.BatchRows`
  rows from the native engine and the bulk reader, one vector from
  DuckDB, so one message stays under gRPC's receive limit.
- The codes of a running query (ADR-0018, Decision 13) are `statusOf`'s,
  which the login shares: the engine's error is INVALID_ARGUMENT with
  its message, an open breaker and a full instance cap are UNAVAILABLE
  with the error's message and a `RetryInfo` detail carrying the
  retry-after, an unreachable cluster and a failed attach are
  UNAVAILABLE with the message alone, a refused credential is
  UNAUTHENTICATED, the caller's own ctx ending is its code. A failure
  after the first message closes the stream with INTERNAL, or the ctx's
  code when the caller left, and one warning line (owner, 2026-10-21).

## Tests

- One test file, `flightsql_test.go`, drives arrow-go's Flight SQL
  client against the server on a port of its own with the ephemeral
  certificate and verification off, as the driver smoke tests connect.
  `TestHandshake` runs on both clusters through `cluster.Each`, and
  `TestBearerRows` on the insecure cluster alone with a fixed clock,
  because none of its rows reaches the cluster. Tokens are minted
  directly, as the HTTP handler tests mint them.
