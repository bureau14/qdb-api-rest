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
- The server embeds `flightsql.BaseServer`, so a command this package
  does not implement answers `UNIMPLEMENTED` without a line of code
  here.
