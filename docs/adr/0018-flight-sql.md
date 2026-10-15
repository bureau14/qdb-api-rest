# ADR-0018: Flight SQL: one listener, the `#sql` prefix, stateless tickets and the minimal surface

Status: accepted
Date: 2026-10-13

## Context

The brief (Arrow Flight SQL) puts a minimal Flight SQL server on its own
gRPC port and leaves the shapes to this milestone. Flight SQL carries a
query as one string in `CommandStatementQuery` and has no path or
dialect field, so the string itself has to say which of the two query
engines runs it (ADR-0015 places Flight SQL after DuckDB for this
reason). Flight fetches a result in two calls, `GetFlightInfo`
answering a ticket and `DoGet` streaming the batches for it, and the
two calls of one client may reach different instances behind a load
balancer. Stock drivers handshake once at connect and present the token
until it fails. arrow-go, already the vendored Arrow, ships the Flight
and Flight SQL protos pre-compiled, a server base type and a pure Go
client. The owner made the decisions below on 2026-10-13.

## Decision

### Listener

1. **The Flight SQL listener always starts.** Its address is
   `listen.flight`, default `:40493`. It serves TLS from
   `tls.certificate` and `tls.private_key`, with the same ephemeral
   self-signed fallback and startup warning as the HTTPS listener.
   There is no plaintext Flight listener.
2. **The implementation is arrow-go's Flight SQL server.** The binary
   embeds its base server in `internal/flightsql` and vendors grpc-go
   and protobuf with it. There are no hand-authored protos and no
   custom gRPC services.

### The `#sql` prefix

3. **One prefix chooses the engine.** A query whose first non-whitespace
   bytes are `#sql`, matched case-insensitively and followed by one
   whitespace character, runs on DuckDB with the prefix and that
   character removed. Every other query runs on the native engine as
   is. `#sqlx` and `#sql` without a following whitespace character are
   native queries and fail there.
4. **`POST /api/v2/query` honors the same prefix.** `POST /api/v2/sql`
   stays the direct DuckDB route of ADR-0017 and does not look for the
   prefix.
5. **The dispatcher is shared.** One function recognizes the prefix,
   strips it and calls the native query or the DuckDB query for the
   caller's user, answering the `Batches` iterator either way. It lives
   above `internal/qdb` and `internal/olap`, and `httpapi` and
   `flightsql` both call it. The native query is wrapped as a one-step
   iterator, so every result in the binary streams through
   `EncodeStream` and the Flight server has one `DoGet` body.

### Authentication

6. **Handshake is a login.** It takes username and secret, checks them
   against the cluster as `POST /api/v2/auth/login` does (ADR-0011),
   and answers the access token only. A client whose access token has
   expired reconnects, which is the stock driver's behaviour. Flight
   gets no refresh path until this proves to be a problem.
7. **The bearer is the `authorization` metadata.** Every other RPC
   presents the access token there, verified by the same verifier as
   HTTP, with `typ != access` refused.
8. **Anonymous is admitted.** On a cluster without security, a call
   with no token runs as the anonymous user, as HTTP admits the
   anonymous token.

### Tickets and the surface

9. **The server stores nothing between calls.** The ticket
   `GetFlightInfo` answers carries the query text after dispatch, and
   `DoGet` runs the query. `FlightInfo` has one endpoint and no
   schema, because the native engine does not know a result's columns
   before running the query. `GetSchemaStatement` is `UNIMPLEMENTED`
   rather than running every query twice.
10. **`GetSqlInfo` describes the server fully.** Server name, version,
    read-only, no transactions, no prepared statements, and the
    information the drivers read about identifiers, keywords and
    supported statements, described for DuckDB as the SQL engine with
    the native engine named in the server name. The field list is the
    handler's.
11. **Catalog commands are in this milestone.** `GetTables` answers the
    cluster's table list, and `GetCatalogs`, `GetDbSchemas` and
    `GetTableTypes` answer the fixed shape of one cluster. The table
    list core in `internal/qdb` is written here and the exploration
    milestone reuses it.
12. **Everything else is `UNIMPLEMENTED`.** Prepared statements,
    transactions, savepoints, `DoPut` in every form, substrait, session
    options and the poll and renew RPCs.
13. **Status codes.** A bad or missing token is `UNAUTHENTICATED`. A
    parse or execution error of either engine is `INVALID_ARGUMENT`
    with the engine's message. An open breaker or a budget timeout is
    `UNAVAILABLE`. The unsupported surface is `UNIMPLEMENTED`. A mint
    or encoding failure is `INTERNAL`.

### Tests

14. **The tests are Go property tests.** The format-equivalence
    property gains a Flight SQL client from arrow-go and requires the
    decoded Flight result to equal the HTTP formats, for the native
    engine and for the `#sql` engine against `/api/v2/sql`. A dispatch
    property generates prefixes and whitespace around them. There is
    no e2e step and no stress case for Flight.

## Consequences

- M5 starts with this ADR and ends when the listener, the dispatcher
  on both planes, Handshake and bearer auth, the statement and catalog
  commands, `GetSqlInfo` and the property tests are landed and green
  in Buildkite on every platform.
- A `#sql` body on `POST /api/v2/query` runs on DuckDB from M5 on, so
  the HTTP query handler moves to `EncodeStream` and the dispatcher.
- Any instance behind a load balancer serves a `DoGet` for a ticket
  another instance answered, because the ticket is the query.
- A long Flight job outlives `auth.access_ttl` only by reconnecting.
  The Python API as the primary client reconnects itself.
- `vendor/` grows by grpc-go, protobuf and arrow-go's flight packages.
- The old documentation's `tls_port: 40493` samples are corrected in
  `qdb-documentation` during this milestone.
- `docs/e2e.md` no longer defers a Flight question to this milestone's
  entry.

## Alternatives rejected

| Alternative                                            | Why not                                                                                                       |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| A gRPC metadata header choosing the engine             | needs a driver knob per client, and the query text travels through every driver untouched                     |
| Flight SQL session options choosing the engine         | server-side session state, which the project rules out everywhere else                                        |
| A listener that is off until configured                | the owner wants Flight SQL available on every install, and the port is the configurable part                  |
| Running the query in `GetFlightInfo` and caching it    | the two calls may reach different instances, and a cache behind a load balancer is state nobody can route to  |
| A refresh token accepted on the Flight plane           | widens what a refresh token opens, which ADR-0016 narrowed, and reconnecting is enough until proven otherwise |
| A `DoAction` refresh on the Flight port                | more surface before a problem is observed                                                                     |
| Catalog commands deferred to the exploration milestone | drivers call `GetTables` at connect, and the table list core is small                                         |
| An e2e Flight step through an `e2etool` subcommand     | the owner wants no e2e for Flight now, and the property test drives the same client                           |
| A plaintext Flight listener                            | the owner chose the HTTPS certificates for the port, and stock drivers default to TLS                         |
