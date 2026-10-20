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
