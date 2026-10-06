# QuasarDB REST API -- Rewrite Project Brief

Status: approved. This document is the source of truth for the scope and
direction of the qdb-api-rest rewrite. Hard design decisions made during
development are recorded as ADRs under `docs/adr/`. This brief records the
decisions made before development started. Progress against the milestones
below is tracked only in `docs/log.md`. The documentation conventions and
the recommended reading order are in `docs/AGENTS.md`.

## Vision

The product is a single, dependency-vendored Go binary that is QuasarDB's
HTTP front door at customer sites. It serves three things:

1. A versioned, streaming-first REST API (`/api/v2/*`) for query, ingestion,
   and database exploration. (The old server's unversioned API is retroactively
   "v1". See Compatibility contract.)
2. An Arrow Flight SQL endpoint (gRPC) for Arrow-native clients (ADBC,
   JDBC), minimal by design.
3. An embedded DuckDB OLAP engine (via qdb-duck) exposing full SQL over
   QuasarDB data through a dedicated query endpoint.

Performance is the headline requirement. The server streams everything,
so response memory is bounded regardless of result size, and the time
to first byte is independent of result size wherever the underlying
client API allows it.

Operational and SRE concerns are first-class. The binary is cloud-native
by default (12-factor configuration, stdout logging, Prometheus metrics,
health probes, graceful shutdown). It fails fast and honestly under
overload, and it treats QuasarDB connection reuse as non-optional.

The project replaces the go-swagger-generated server on `master`. A
server-side rendered dashboard replacing the (unused) ClojureScript SPA is a
future direction and not part of the initial scope. The project is
developed entirely by LLM agents over roughly 3 months. This document and
the ADRs exist so that an agent working on any milestone can recover the
intent and constraints without re-deriving them.

## Strategic context: the gateway direction

This section says why performance is existential rather than cosmetic.
The design described here is QuasarDB's, and it is the context this
project exists in.

Terminology: the product is, and remains, the **QuasarDB REST API**
(`qdb_rest`, the `qdb-rest` packages) in all company and customer
communication. "Gateway" in this document names the architectural _role_
the REST API plays in this direction, a query gateway or coordinator in
front of the cluster. It is not a new product name. If the gateway path
ever becomes the default interface for deployments, renaming becomes a
product and marketing decision to take then.

QuasarDB's protocol offloads a large part of query processing to the
client. The model is map/reduce-style, and the reduce phase runs inside
`libqdb_api` in the client process. This has two consequences. Direct
cluster connections require a heavyweight native library plus client-side
compute and memory. And the qdbd protocol is peer-to-peer with the
client, so it does not work behind NAT.

A fast, low-latency, high-memory REST gateway co-located with the cluster
inverts this. Consider
`select user_id, count(user_id) from table in range (today(), -7d) group by user_id order by count(user_id) desc limit 10`.
A remote native client pulls per-shard partial aggregates across the WAN
and reduces locally, only to discard everything but ten rows. The gateway
reduces next to the data and ships ten rows of Arrow. This is the
coordinator-node pattern of Trino, ClickHouse and Elasticsearch, applied
to QuasarDB's client-offload design. A single HTTPS or gRPC port is also
load-balanceable and NAT-traversable in a way the native protocol cannot
be.

The intended client story is this. Client APIs accept either
`qdb://<ip:port>` (native link) or `http(s)://<uri>` (gateway link) and
switch transports on the URI scheme. On the gateway path clients become
thin. `quasardb.pandas`, for example, speaks Flight SQL through pyarrow or
ADBC, receives Arrow and converts it to DataFrames, with no cgo, no
bundled native library and trivial packaging. Upgrades centralize the
same way. Updating the client engine embedded in the gateway upgrades
every gateway-path user at once, which matters because QuasarDB offloads
so much to the client. Migrating the individual client APIs is out of
scope here (separate projects, `qdb-api-python` first). This project
designs the protocol surface with those clients in the loop.

The concrete goal is to make this interface, the embedded DuckDB engine
included, good enough that the gateway is a reasonable choice for
high-volume, high-intensity workloads, and, depending on how well it
performs, perhaps the default choice for all deployments. That prospect
sets the performance bar, the SRE machinery (the gateway absorbs the
reduce phase for all its clients), and the stateless, horizontally
scalable design.

## Ecosystem

| Project                         | Relationship                                                                                                                                                                                                          |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `quasardb`                      | The database server. REST API package version is pinned 1:1 to server releases.                                                                                                                                       |
| `qdb-api-go`                    | The cgo client binding; the primary path to the cluster. Vendored, never forked. Improvements are merged upstream first, and then the vendored copy is updated.                                                       |
| `qdb-duck`                      | Native DuckDB extension that attaches QuasarDB clusters (catalog integration, handle pool). Embedded into this binary via go-duckdb to provide the full-SQL endpoint.                                                 |
| `qdb-grafana-plugin`            | External consumer of `/api/v1/login` and `/api/v1/query` through their unversioned aliases. Defines the backwards-compatibility surface together with customer code.                                                  |
| `qdb-api-python` (and siblings) | Future gateway consumers: client APIs gain an `http(s)://` transport (Flight SQL / HTTP) next to the native `qdb://` link. Migrations are separate projects. This project designs the protocol with them in the loop. |
| `qdb-dashboard`                 | The old ClojureScript SPA, unused by customers. Retired, with no feature-parity obligation.                                                                                                                           |
| `qdb-pkg-debian`, `qdb-pkg-rpm` | Package the binary as `qdb-rest` with a systemd unit.                                                                                                                                                                 |
| `qdb-docker`                    | Today bundles the REST binary into the `qdb-dashboard` component image. This project adds a first-class standalone image.                                                                                             |
| `qdb-release`                   | Central version manager. The rewrite must register its version-string location(s) there.                                                                                                                              |
| `qdb-documentation`             | Public docs; `user-guide/tools/qdb_rest.rst` and `user-guide/api/rest.rst` must be rewritten at the end of the project.                                                                                               |

Deployment reality: customers run `qdb_rest` as a systemd service from
deb/rpm packages, with HTTP on 40080 and HTTPS on 40443, often behind
a load balancer whose health checks hit the status endpoints. Docker images
exist, but package-based deployment dominates today. The rewrite should
make container deployment first-class without breaking the package path.

## Vocabulary

These are QuasarDB's words, used unchanged in configuration keys, flags,
code and prose:

- **Cluster**: one or more qdbd nodes behind one `qdb://` URI. A secured
  cluster has a _cluster public key_ (`cluster_public.key`, which clients
  hold inline or as a file) and a _cluster private key file_ (server side,
  never seen by this project).
- **User**: a QuasarDB identity with a _username_ and a _secret key_ (the
  user's private key). Both travel together in the _user security file_,
  the JSON file QuasarDB's tooling generates
  (`{"username": ..., "secret_key": ...}`), or inline. The cluster keeps
  the users' public keys in its user list. The REST API's _own user_ is
  the one it authenticates as on its own behalf (the readiness probe).
  Every other user is a caller.
- **Session**, at three layers, qualified whenever the layer is not
  obvious from the context. A _qdbd session_ is the backend's resources
  reserved for one connection (`--total-sessions`). A _client session_
  (bare "session" inside `internal/qdb`) is one authenticated connection a
  client API hands back, the unit the pool leases. A _REST session_ is a
  login, the token that carries a user's credentials back to the REST
  API.
- **Handle**: the C API's `qdb_handle_t` (`qdb-api-go`'s `HandleType`),
  the object underneath a client session. The word is used only when the
  cgo layer itself is meant.

## Goals

1. **Protocol performance.** The response path streams end to end, with
   no full-response buffering, content-negotiated wire formats, and
   HTTP/1.1 and HTTP/2. Compression (zstd, gzip) is client-negotiated
   through `Accept-Encoding`. Identity is the default and the server does
   not force compression, so datacenter clients pay nothing and WAN
   clients opt in. Correctness is gated in CI. Performance is measured by
   the assessment bench (Testing doctrine).
2. **Backwards compatibility** for the endpoints customers and the Grafana
   plugin actually use: `/api/v1/login` and `/api/v1/query`. The two are
   byte-shape compatible, warts included (see Compatibility contract).
3. **A proper versioned API** (`/api/v2/*`) with a real resource model:
   query, table listing and schema inspection, table creation, ingestion
   (multi-table), tags, cluster and node status, health.
4. **Arrow Flight SQL**, minimal implementation, for the Arrow-native
   client ecosystem (ADBC, JDBC).
5. **Embedded DuckDB** (qdb-duck) behind a dedicated endpoint, which
   offers full SQL (joins, window functions, the entire DuckDB surface)
   over QuasarDB data from the same binary.
6. **Security hygiene**: no default cryptographic keys baked into the
   binary, authenticated encryption for tokens, rolling key support,
   short-lived access tokens.
7. **Cloud-native by default**: YAML/env/flag configuration, structured
   logs to stdout, Prometheus `/metrics`, k8s-compatible liveness and
   readiness probes, graceful shutdown draining in-flight streams, and a
   statically linked Linux binary (new static `libqdb_api.a`) so
   deployment is copy-one-file.
8. **SRE-first resilience**: elaborate QuasarDB connection pooling
   (connection reuse is non-optional), a session budget that bounds
   load, circuit breakers, honest fast failure when the cluster is
   down. See Architecture.
9. **Useful statistics**: a Prometheus exposition endpoint with request
   and query latencies, rows/bytes streamed per format, time-to-first-byte,
   pool and breaker state, qdb call durations, Go runtime and build info.

## Non-goals

- **No SPA, no frontend framework, no node/npm anywhere.**
- **No packaging and no Docker image.** deb/rpm packaging, the
  standalone Docker image and the `qdb-pkg-*` / `qdb-docker` integration
  are out of scope until deployment becomes a concern. When packaging
  returns, the RPM marks the config `%config(noreplace)` (Debian's
  conffile handling is the reference).
- **No dashboard in the initial scope.** The SSR dashboard is a future
  direction (see Architecture: Dashboard). The routing seam and
  cookie-compatible auth are preserved for it.
- **No Prometheus remote read/write, no CSV table export, no
  `/api/option/*`.** The old server's endpoints that v1 does not carry are
  listed under Compatibility contract, "Explicitly dropped". (The
  `/metrics` exposition endpoint is unrelated to the Prometheus
  remote-storage integration.)
- **No config-file compatibility.** The config format is redesigned. Only
  the wire protocol is compatibility-constrained. Customers' pain is their
  custom client code, not their install scripts.
- **No user management.** Users are managed through QuasarDB itself.
- **No changes to `qdb-api-go` inside this repo.** Where the binding is
  the bottleneck (Architecture: Data plane, the materialization
  constraint), upstream changes are filed and merged separately.
- **No truncate push mode.** The C API's batch-push truncate/backfill mode
  is deliberately not exposed through the REST ingestion API.
- **No multi-tenancy / public-internet hardening** beyond standard TLS and
  auth. This product runs inside customer networks.

## Why this rewrite exists

Two real drivers:

1. **Protocol performance.** The old server materializes entire query
   results as per-cell boxed `interface{}` values, then performs a single
   reflective `json.Encode` over the whole structure, and runs without
   HTTP timeouts, so a large result blows memory and hangs rather than
   failing fast. The time to first byte of the reference query is the
   full materialization time (the bench measures it, `docs/bench.md`).
   Escaping go-swagger matters only because it stands in the way of
   high-performance protocols.
2. **Token and credential hygiene.** The old JWT embeds the user's raw
   `secret_key`, encrypted with an RSA key that defaults to a keypair
   hardcoded in the binary. A token leak is a credential leak, and every
   insecure-mode deployment shares one key.

Additionally, the old codebase accumulated a class of bugs (shared mutable
globals for cluster status, a user's connection pool torn down and
replaced on every re-login, racing in-flight requests) that the new
architecture excludes by construction. The old dashboard is unused, so
the rewrite sheds it.

The rewrite also modernizes the product's posture: proper RESTful resource
modeling where pragmatic, and a binary that feels native in cloud
ecosystems and is trivial to deploy.

## Compatibility contract

The versioning stance: the old server's unversioned API is retroactively
**v1**, frozen, warts and all, and served forever. New endpoints are
minted under `/api/v2/*` only. The canonical spelling of every v1
endpoint is `/api/v1/<path>`. The historical unversioned path is an
alias served by the same handler, never a redirect (ADR-0008).

v1 routes carry no parallel implementation. A v1 route wraps its v2
counterpart and translates request and response shapes around the v2
core, and v1 code lives in its own package (ADR-0007).

The contract covers two endpoints, the login and the query. The status
probes are outside it (Observability and logging). The two endpoints
behave byte-shape identically to the old server, except for the
deliberate deviations listed at the end of this section. Golden
responses captured from the old server are part of the e2e test suite,
and no golden exercises a deviation (ADR-0013). This section is the
specification.

### POST /api/v1/login

- Request: `{"username": "...", "secret_key": "..."}`, the content of a
  QuasarDB user private-key file. An empty or absent username means an
  anonymous login (insecure clusters). The Grafana plugin relies on this.
- Response 200: `{"token": "<opaque string>"}`. Clients treat the token
  as opaque. Its format is this server's alone (ADR-0005). The token is
  valid for 12 hours.
- Response 401: `{"message": "..."}`.
- Tokens minted by the old server are rejected. Clients log in again on
  401 (the Grafana plugin clears its token and retries on 401).

### POST /api/v1/query

- Auth: `Authorization: Bearer <token>` or the `?token=<token>` query
  parameter. The parameter exists in v1 only, as v2 does not accept
  tokens in URLs.
- Request: `{"query": "..."}`, the object form the Grafana plugin sends.
  A bare JSON string is not accepted.
- Response 200: `{"tables": [{"name": "...", "columns": [{"name": "...",
"type": "...", "data": [...]}]}]}`.
- Column types: `blob | double | int64 | string | timestamp | none`. A
  `COUNT(...)` column is an `int64`. The old server's `count` type
  named the same number, and no client told the two apart.
- Null cells of every type are JSON `null`, and the column type is taken
  from the last non-null row (`"none"` if every row is null). The old
  server's `"(void)"` and `"(undefined)"` sentinel strings are not
  reproduced. The C API types every null cell `qdb_query_result_none`,
  so no query could produce them.
- Warts preserved verbatim on this v1 endpoint (and only here):
  - A query whose text begins with the literal prefix `find` is routed to
    the tag-find API and returns tables with names only and no columns.
    The match is a raw, case-sensitive, untrimmed prefix test
    (`strings.HasPrefix(query, "find")`). Leading whitespace defeats it,
    and any query starting with those four bytes (`finder ...`, for
    example) is routed too.
- Errors 400/500: `{"message": "..."}`.

### Deliberate deviations

This table lists every place where v1 answers differently from the old
server. No v1 golden exercises an entry of this list (ADR-0013).

| Deviation                                                                  | Specified in              |
| -------------------------------------------------------------------------- | ------------------------- |
| a `COUNT(...)` column is typed `int64`, where the old server said `count`  | POST /api/v1/query, above |
| the `"(void)"` and `"(undefined)"` sentinel strings are not reproduced     | POST /api/v1/query, above |
| tokens minted by the old server are rejected                               | POST /api/v1/login, above |
| bad credentials on a secured cluster are `401` at login, not a blind `200` | ADR-0011                  |
| the dropped endpoints answer `404`                                         | Explicitly dropped, below |

### Explicitly dropped

`/api/prometheus/read`, `/api/prometheus/write`, `/api/tables/{name}.csv`,
`/api/option/parallelism`, `/api/option/max-in-buffer-size`,
`/api/cluster`, `/api/cluster/nodes/{id}`, `/api/tags`. None has a
known consumer (the Grafana plugin calls none of them). The cluster
endpoints and the Prometheus remote-storage integration are publicly
documented, so the release milestone's migration notes and the
`qdb-documentation` rewrite cover their removal. v2 provides
cluster-status equivalents.

## Architecture

### Data plane: streaming HTTP + Arrow Flight SQL

One binary, two listeners:

- **HTTP listener** (existing ports): REST control plane, v1 compat
  endpoints, `/api/v2` data plane. HTTP/1.1 and HTTP/2.
- **gRPC listener** (dedicated port, default 40493): Arrow Flight SQL. It
  is not multiplexed onto the HTTP port. gRPC and plain HTTP/2 are
  indistinguishable at accept time (same `h2` ALPN), so sharing a port
  means either grpc-go's shared-port path (`Server.ServeHTTP` through
  `net/http`, officially experimental and slower than its native
  transport, which defeats the purpose) or fragile byte-sniffing.
  REST-appropriate write timeouts and LB idle rules would also kill
  long-lived gRPC streams. Customers who do not use Flight SQL do not
  open the port.

`POST /api/v2/query` is a streamed response in a content-negotiated format
(ClickHouse-HTTP-style):

| Accept                                | Encoding                                                                   |
| ------------------------------------- | -------------------------------------------------------------------------- |
| `application/json` (default)          | Columnar, one object per column, v2's own shape, streamed as it serializes |
| `application/x-ndjson`                | One JSON object per row                                                    |
| `text/csv`                            | RFC 4180                                                                   |
| `application/vnd.apache.arrow.stream` | Arrow IPC stream, columnar batches                                         |

All formats are produced by one query-execution core with N encoders. v2
uses proper nulls per format instead of the old server's sentinel strings.

Compression, spelled out once. Response compression is negotiated
through `Accept-Encoding` (zstd, gzip), with identity as the default,
and it is not forced. Ingestion symmetrically accepts
`Content-Encoding: zstd|gzip` request bodies. Arrow IPC additionally
supports its own in-format record-batch buffer compression (lz4 or
zstd, part of the IPC spec), which is independent of HTTP-level
compression and also request-negotiable.

A known constraint: the query path is the C API's unwrapped Arrow path,
`qdb_query_arrow`. It builds the result as Arrow columns without a
row-major intermediate and hands them to Go zero-copy, but it is
one-shot. The whole result is materialized before the first byte, and
the C API has no incremental-delivery mode (`qdb_query_continuous` is a
live-query subscription that re-delivers results on a refresh interval,
not a cursor). Streaming therefore overlaps serialization and
transmission with iteration over the materialized batch. That bounds
REST-server memory and gives an early first byte, but it does not
remove the binding-side materialization. Relieving that for queries
requires upstream work (a cursor-style query API), which is out of scope
here. Whole tables take another door. `GET /api/v2/tables/{name}/rows`
reads through the bulk reader's batched Arrow fetch, one record batch
per fetch, and each batch is encoded and sent before the next is
fetched, so a table read is bounded on both sides. A `SELECT *` of a
whole table is not canonical QuasarDB. The gateway direction raises the
stakes. Large raw `SELECT`s from thin clients materialize in the
gateway, so the session budget is the short-term backstop and upstream
streaming is the long-term relief valve.

### Arrow Flight SQL (minimal)

Decision: Flight SQL rather than plain Flight RPC, implemented minimally.

The rationale: QuasarDB is not a full SQL database. It has no
client-driven transactions (individual queries are atomic), no prepared
statements, and its own query dialect. But Flight SQL treats query text
as an opaque, dialect-agnostic string, and transactions and prepared
statements are optional capabilities advertised through `GetSqlInfo`.
The value of Flight SQL over plain Flight is the existing client
ecosystem: stock ADBC and JDBC drivers work without custom client code.
QuasarDB already maintains an ODBC driver and generally ships
compatibility layers. Having this ecosystem available is a selling point
worth the constraint.

The minimal implementation scope: `Handshake` (username+secret -> token)
and bearer-token metadata auth, `CommandStatementQuery` ->
`GetFlightInfo` -> `DoGet` streaming Arrow record batches, and
`GetSqlInfo` honestly advertising what is not supported. Catalog and
metadata commands are implemented only as far as cheap and honest
(`GetTables` from the table list, for example). Prepared statements,
transactions and everything else return UNIMPLEMENTED. Ingestion through
`DoPut` is a possible later addition and not part of the minimal scope.

The layering, so nobody re-litigates "Flight SQL vs gRPC". gRPC is the
RPC framework. Arrow Flight is a specific standardized gRPC service
(`FlightService`) whose `FlightData` messages envelope raw Arrow IPC
buffers and whose implementations bypass protobuf serialization on the
hot path, because protobuf itself is a poor container for bulk columnar
data. Flight SQL adds no RPCs, only a standardized command vocabulary
inside Flight's opaque descriptors and tickets, so stock drivers can
operate any conforming server. A custom gRPC service would re-derive
Flight minus the ecosystem and minus the protobuf bypass, so it is
dominated for data transport. QuasarDB-specific control semantics live
on the HTTP plane, and Flight's `DoAction` (application-defined actions,
tolerated alongside Flight SQL) is the escape hatch if a qdb-specific
RPC is ever needed on this port.

There are no hand-authored `.proto` files, because Flight and Flight SQL
protos ship pre-compiled in `arrow-go`. There are no custom gRPC
services. The control plane is HTTP-only.

A gateway note: under the gateway direction (see Strategic context), the
primary Flight SQL client is expected to become QuasarDB's own Python
API. This partially de-risks the minimal-subset bet, because we control
both ends of the connection that matters most, and it means the subset
is chosen with `qdb-api-python`'s needs in the loop. Stock ADBC and JDBC
compatibility is upside, not a hard dependency.

### Embedded DuckDB (qdb-duck)

QuasarDB's own query language is deliberately limited. `qdb-duck` is the
native C++ DuckDB extension that attaches QuasarDB clusters into DuckDB
(catalog integration, so schemas and tables are visible and queryable).
This project embeds DuckDB into the REST binary (through go-duckdb, cgo)
with the quasardb extension loaded, behind a **dedicated endpoint**
(working name `POST /api/v2/sql`). The endpoint exposes full SQL (joins,
window functions, the entire DuckDB surface) over QuasarDB data through
the same streaming, content-negotiated response path. DuckDB's native
Arrow integration makes the Arrow IPC encoder near-free for this path.

This is a major, accepted scope item. It is deliberately isolated. It
has its own endpoint and its own resource governance (DuckDB memory
limits configured explicitly), and a failure of the DuckDB subsystem
must not degrade the native query path.

Authorization matches the native path. qdb-duck binds credentials at
`ATTACH` time, so the server maintains one attached catalog and session
pool per user, LRU-evicted like the native pools. User queries never
run under a shared service credential. qdb-duck is read-only by design
(`INSERT`, `UPDATE` and `DELETE` are rejected), which is the contract
this endpoint wants.

### /api/v2 endpoint sketch

**This is a very early sketch.** Endpoint shapes, names, and payloads are
decided in ADRs during the v2 milestones. The sketch fixes intent, not
contract. Multi-table ingestion is a hard requirement. `qdb-api-go`'s
`Writer` pushes multiple tables in a single batch-push call
(`qdb_exp_batch_push_with_options`), and the ingest API is designed around
that call. Schema endpoints use the full server-side column-type
vocabulary (`blob`, `double`, `int64`, `string`, `symbol`, `timestamp`).
`symbol` is a distinct schema-level type even though query results
surface it as `string`.

```
POST   /api/v2/auth/login          {username?, secret_key?} -> {access_token, refresh_token, expires_in}
POST   /api/v2/auth/refresh        refresh token -> new token pair
POST   /api/v2/auth/logout         invalidates client state; server-side revocation is a future layer
GET    /api/v2/session             "who am i": session/user/instance introspection (see below)
POST   /api/v2/query               native qdb query; streamed, content-negotiated (see above)
POST   /api/v2/sql                 full SQL via embedded DuckDB; streamed, content-negotiated
POST   /api/v2/rows                multi-table ingest, a $table column routes each row; body content-negotiated
                                   (CSV, NDJSON, Arrow IPC; Content-Encoding); push and deduplication modes
                                   via parameters; M2, the contract is the handler's
GET    /api/v2/tables              list tables (prefix filter, pagination)
POST   /api/v2/tables              create table (name, shard size, columns); M2, see Tables below
GET    /api/v2/tables/{name}       schema: columns, types, shard size, tags
DELETE /api/v2/tables/{name}       remove table; M2, see Tables below
GET    /api/v2/tables/{name}/rows  the table reader: the bulk reader streamed batch by batch;
                                   content-negotiated; optional time range and column selection; M2, the contract is the handler's
GET    /api/v2/tags                list tags
GET    /api/v2/tags/{tag}          entries carrying the tag
GET    /api/v2/cluster             cluster status (nodes, disk, memory)
GET    /api/v2/cluster/nodes/{id}  node detail
GET    /api/v2/status/liveness     unauthenticated probe
GET    /api/v2/status/readiness    unauthenticated probe (dials as the REST API's own user)
GET    /metrics                    Prometheus exposition (config-gatable)
```

`GET /api/v2/session` ("who am i") returns the authenticated caller's view
of their session, assembled without any cluster round-trip, so it is
cheap enough to poll. It carries the username (or anonymous), the
cluster URI this server fronts and whether cluster security is enabled,
token introspection (type, issued-at, expires-at, `jti`, logged-in-since
via the `auth_time` claim, which survives refreshes), this instance's
local view of the caller's user pool (sessions in use, idle and the cap,
shared by every session of that user), and a server instance identifier
and version. The instance identifier matters. Behind a load balancer,
pool numbers are per-instance truth, and the id makes that legible.
Whatever user metadata QuasarDB exposes (permissions, for example) can
be added later behind the same endpoint. Secret material is never echoed
back. The primary audience is debugging "who am I logged in as", "why am
I unauthorized" and "where are my connections", the most common support
questions an API like this gets.

### Tables: create and delete

Both routes take a bearer access token and answer errors as ADR-0010's
problems. Its status table applies, with the two rows named here.

`POST /api/v2/tables`, `Content-Type: application/json`:

```json
{
  "name": "trades",
  "shard_size": 86400000,
  "columns": [
    { "name": "price", "type": "double" },
    { "name": "venue", "type": "symbol", "symtable": "venues" }
  ]
}
```

`shard_size` is an integer number of milliseconds, the C API's unit,
and it is required, because the REST API has no duration syntax of its
own next to qdbsh's. `type` is a word of the schema vocabulary above.
`symtable` is required on a `symbol` column, as the C API requires it,
and refused on any other. `$timestamp` is implied. The route answers
`201` with `Location: /api/v2/tables/<name>` and an empty body, and
`409` when the name is taken.

`DELETE /api/v2/tables/{name}` answers `204` with an empty body, and
`404` when the cluster knows no such name. It removes the table only. A
symtable is its own entry, which other tables may share.

### Resilience and connection management

Operational and SRE concerns are primary. QuasarDB connection reuse is
non-optional. The pool is an explicit, elaborate mechanism, not a
convenience wrapper. The decisions:

- **Session budget**: one configured `max_sessions` for the whole server,
  a predictable ceiling on what this binary imposes on the cluster,
  partitioned into per-user sub-pools with caps. The budget is also the
  overload mechanism. A request past it waits for a session or times
  out at its deadline. Sessions are pooled per user, keyed by (cluster,
  username). A QuasarDB user has one secret key, so every REST session
  of that user dials identically and shares the pool (anonymous is one
  user). Login finds the existing pool or creates one. It does not
  replace or drain one, so in-flight requests are not raced. Idle user
  pools are LRU-evicted. The session id claim (Authentication) is a
  security handle, not a pool key.
- **Circuit breaker, fail fast**: a breaker per cluster opens on
  consecutive connect or timeout failures. While it is open, requests
  fail immediately with 503 and `Retry-After`, and half-open probes test
  recovery. There is no hanging, no queueing onto a dead cluster and no
  goodput collapse.
- **Timeouts**: every request and every stream write carries a deadline.
  Graceful shutdown drains in-flight streams. QuasarDB calls are bounded
  by the C API's own socket timeout (`cluster.timeout`), because
  `qdb-api-go` exposes no `context.Context` plumbing and a blocking cgo
  call cannot be cancelled from Go.
- All of it (pool occupancy, breaker state, retry counts) is exported
  through `/metrics`.

### Authentication

Authentication is stateless by requirement. Sessions must be long-lived
and freely balanced across multiple REST servers with no shared state.
Because each server must open cluster connections _as the logged-in
user_, the token necessarily carries reconnect material. The design
makes that safe:

- Tokens are JWE (authenticated encryption with modern primitives, not
  RSA-OAEP + A128CBC). A token contains the username and secret, a
  `jti`, a session id claim (stable across refreshes, a security handle
  for features such as logging out a user's other sessions, and never a
  pool key), a session generation claim, a `typ` (`access` or `refresh`)
  claim, and an `auth_time` claim (the original login time, preserved
  across refreshes and surfaced as "logged in since" by
  `GET /api/v2/session`).
- **Access tokens** are short-lived (minutes). **Refresh tokens** are
  long-lived and sliding. `/api/v2/auth/refresh` returns a fresh pair. One
  verifier handles both, plus the 12h tokens minted by v1
  `/api/v1/login`.
- **Keys from passphrases**: the config holds human-friendly passphrases.
  The actual keys are derived once at startup through argon2id
  (memory-hard, so an attacker brute-forcing a captured token pays
  ~100ms per passphrase guess) and HKDF for fixed-length key material
  and domain separation (the encryption key and the `kid` are derived
  independently). Derivation adds no entropy, so the passphrase still
  needs to be decent. It multiplies the attacker's cost per guess and
  satisfies the AEAD's key requirements.
- **Rolling keys** fall out of the config shape: a list of passphrases,
  where the first is current (minting) and the rest are accepted for
  verification. Tokens carry `kid`. A refresh re-mints under the current
  key, so rotation propagates within one refresh cycle and old entries
  can then be removed.

  ```yaml
  auth:
    token_secrets:
      - "current passphrase" # minting + verification
      - "previous passphrase" # verification only, during rotation
  ```

- **Ease of use over ceremony**: env-var interpolation in the YAML covers
  cloud secret injection. If no secret is configured, the server generates
  an ephemeral key at startup and logs a warning. Dev setups and
  anonymous clusters thus work with zero config, at the documented cost
  that tokens do not survive a restart and do not validate across
  multiple instances behind a load balancer (each instance derives its
  own ephemeral key). Secured clusters require an explicit secret. There
  are no default keys.
- The REST API has its **own QuasarDB user** (`cluster.user_security_file`),
  which it authenticates as on its own behalf, for readiness checks and
  future central coordination.
- **Revocation is deliberately deferred.** The `jti` and generation
  claims make a later revocation layer backed by QuasarDB's key-value
  store (bump the generation on logout, verify with a ~30s in-memory
  cache) purely additive.
- The same token works everywhere: `Authorization: Bearer` on HTTP and
  `authorization` metadata on gRPC (ADBC and JDBC support this natively).
  The Flight `Handshake` RPC accepts username and secret and returns a
  token, so DSN-based clients work.

### Cluster binding: one gateway, one cluster

The cluster URI and the cluster public key are server configuration,
never per-session client input. QuasarDB authentication needs the
username, the user secret and the cluster public key. With the cluster
fixed per process, the public key is fixed with it. This was decided
deliberately, for three reasons:

1. **The public key is a trust anchor**, not a connection parameter. If
   clients supplied the URI and the key at login, the server would
   connect outbound to any endpoint any authenticated caller names (an
   open proxy with SSRF characteristics inside the customer network),
   the trust decision would be delegated to arbitrary clients, and
   tokens, which embed reconnect material by design, would become
   portable cross-cluster credentials. If clients supplied only a URI,
   the server would need a preconfigured key registry anyway, which is
   multi-cluster configuration rather than per-session freedom.
2. **The version pin.** This binary links `libqdb_api` pinned 1:1 to a
   server release. Which cluster it can correctly front is a
   deployment-time property, not a session-time one.
3. **The gateway thesis is locality.** The performance argument is the
   reduce phase running co-located with the cluster. N clusters mean N
   co-located gateways, not one gateway dialing N clusters. Per-session
   clusters would also add a cluster dimension to every piece of the
   resilience machinery (budgets, pools, breakers, readiness, metrics).

The escape hatch, if multi-cluster demand ever materializes, is a
**named cluster registry** in the server config (name -> URI, public key
and own user), selected by name at login, with the name carried as a
token claim and pools, breakers and budgets per named cluster. It is
deferred until someone asks. The door stays open at near-zero cost: an
optional cluster-name claim is reserved in the token, and pools are
internally keyed by (cluster, username) even while the cluster count is
one.

### Configuration

The configuration is a YAML file (which plays well with helm and
cloud-init), overridable by environment variables and flags, with
env-var interpolation inside the YAML for secret injection. There is no
compatibility with the old JSON config.
`examples/qdb_rest.yaml` is the commented reference config, pinned to the
defaults by test.

### Observability and logging

- Structured logging goes through the stdlib `slog`, with the JSON
  handler by default and the pretty console handler for interactive use.
  The application logs to stdout and does not manage its own log files.
  journald and systemd own capture on Linux, and the container runtime
  owns it in Docker and k8s.
- The logger travels in `context.Context` (`internal/observe`), never as
  a global. The HTTP middleware tags each request context with its id
  (`X-Request-Id`, honored when well-formed, minted otherwise, and always
  echoed) and writes one access line. Auth adds the user and the session
  the same way, and every line logged below inherits those attributes.
  The decision and its rationale are in ADR-0002.
- The one exception is Windows service mode, where there is no console and
  no logrotate convention. Service lifecycle and fatal events go to the
  Windows Event Log (the native facility monitoring agents collect from),
  and the application log stream goes to a self-rotating file through
  `natefinch/lumberjack` (the ecosystem-standard rotation writer, vendored
  unmodified). An optional `log.file` config key exposes the same sink on
  any platform for users who want it.
- `/metrics` (Prometheus exposition) as described under Goals.
- The status probes are `GET /api/status/liveness` and
  `GET /api/status/readiness`, mirrored under `/api/v2/status/*`,
  unauthenticated, with an empty body and `200` or `503`. Readiness
  dials the cluster as the REST API's own user on every probe
  (ADR-0004). The probes are an operational surface outside the
  compatibility contract. The unversioned paths are kept because load
  balancers at customer sites are configured with them, not because the
  old server's answers are reproduced.

### Dashboard (future, out of initial scope)

A server-side rendered dashboard (Go `html/template`, `go:embed`, no build
chain) remains the intended replacement for the retired SPA: login, query
console, cluster overview, node detail, table browser. It is not part of
the initial project. The HTTP routing seam and cookie-compatible token
auth are preserved so it can be added without rework.

### Project structure

```
cmd/qdb_rest/          entry point (all platforms; Windows service mode included)
internal/config/       YAML config + flags + env
internal/tlsconf/      HTTPS certificates (files or ephemeral self-signed; ADR-0001)
internal/auth/         JWE tokens, key derivation, the caller's user
internal/qdb/          session pools, circuit breaker, query execution, ingestion (wraps qdb-api-go)
internal/encoding/     format encoders and decoders: json, ndjson, csv, arrow
internal/model/        the neutral table representation the layers share, Arrow record batches by indirection
internal/httpapi/      /api/v2 handlers, status probes, middleware, the router
internal/httpapi/v1/   v1 wrappers over the v2 core; the only package that knows the v1 wire shape (ADR-0007)
internal/flightsql/    Arrow Flight SQL server
internal/olap/         embedded DuckDB (go-duckdb + quasardb extension)
internal/observe/      metrics, logging setup
docs/                  this brief, ADRs, plans, the project log
scripts/tests/setup/   shared qdb-test-setup (qdbd as a service; copied from qdb-nats-connector)
tests/e2e/             e2e harness (make + shell + curl, live qdbd; runs in Buildkite); bench/ inside is temporary and local
vendor/                vendored dependencies (committed)
```

Routing is the stdlib `net/http` (1.22+ pattern routing) or chi, with no
framework and no code generation. Files follow the book pattern:
definitions come before use, and a file reads top to bottom.

## Development standards

- **Go version**: always the latest release, pinned by the `toolchain`
  directive in `go.mod` and bumped promptly when a new one ships. The
  features of the pinned release are fair game where they fit. From 1.27
  these are generic methods (type parameters on method declarations), the
  stdlib `uuid` package (RFC 9562, used instead of vendoring a
  third-party UUID library, for token `jti` claims for example), and
  `encoding/json/v2` and `encoding/json/jsontext` where their streaming,
  strictness or appenders help an encoder (`internal/encoding` writes
  cells through the `jsontext` appenders).
- **CI**: Buildkite, on all platforms. All tests run in Buildkite
  (qdb-nats-connector is the reference for how this should feel). The
  pipeline is authored from scratch for this repo, not carried over from
  the old repo's pipeline. This project doubles as the occasion to
  refactor the shared `qdb-cicd-tools` library, and such improvements are
  made in that repo (tracked as separate work), never as local forks.
  Linux builds statically link the new `libqdb_api.a`.
- **Static checks as gates**: golangci-lint v2 with the gofumpt formatter
  enabled (the same linter stack as qdb-nats-connector), pinned to a
  specific version in-repo and installed by the lint step itself, so the
  pin lives in this repository rather than in the builder image.
- **No package-level mutable state.** `context.Context` flows through
  every call path.
- **Logging** goes through `slog` only, through the context-carried
  logger and its `*Context` methods (ADR-0002). There is no third-party
  logging framework.
- **Style**: small composable functions with descriptive names, files
  that read top to bottom (the book pattern), and explicit over implicit.

## Testing doctrine

The project prefers generative and end-to-end tests over unit tests.
Unit tests exist only where a pure function has genuine logic worth
pinning.

There are four layers, one question each (ADR-0013). The first three
run in Buildkite on every platform and gate. The fourth runs on a
developer machine and gates nothing. A question whose answer is yes or
no belongs to the first three. A question whose answer is a number
belongs to the fourth.

1. **Generative property tests** (`pgregory.net/rapid`, quickcheck-style),
   run against a live qdbd. Is the logic right, for any input?
   - _Format equivalence_: for randomly generated schemas, data, and
     queries, the decoded results of JSON, NDJSON, CSV, Arrow IPC, and
     Flight SQL are identical.
   - _Ingest/read roundtrip_: randomly generated data pushed through the
     v2 ingest endpoint (each input format) reads back exactly through
     the table reader.
   - _Auth properties_: token roundtrip, expiry, key-rotation continuity,
     refresh behavior, generated over the key and claim space.
2. **End-to-end** (the pattern of qdb-nats-connector ADR-007). Does the
   built binary, driven over HTTP like a client, do what a client
   expects? Make, shell and curl orchestrate requests against a live
   qdbd started by the shared `scripts/tests/setup/start-services.sh`
   (qdbd is a persistent service, never started by a test). There are
   two suites. The `v2` flow (ADR-0014) logs in, creates a table,
   queries it empty, ingests generated rows through every input format,
   and reads them back in every format and content coding, with each
   response decoded to CSV and compared byte for byte with the generated
   rows. Nothing is captured and nothing is audited, because the
   expected value is known by construction. The `v1` goldens (ADR-0013)
   are small request/response pairs captured from the old server,
   audited, committed, and replayed against the v1 endpoints, none of
   them exercising a deliberate deviation (Compatibility contract).
   Error rows are Go tests, never e2e cases. The v1 suite and the bench
   read the canonical dataset, a customer-derived 5,613,032-row table
   (story sc-19522) distributed as CSV plus a `qdb_import` config,
   sha256-pinned, S3-hosted the way the nats-connector golden datasets
   are, and loaded idempotently by `make load`. The specification is
   `docs/e2e.md`.
3. **Stress as behaviour**, in the same harness: a concurrency stress
   (N parallel clients) asserting that the session budget bounds memory
   and load (excess waits or times out, no goodput collapse), and that
   in-flight streams complete across a graceful-shutdown drain. Every
   assertion is pass or fail on behaviour. None is a timing or a
   measured number.
4. **Local assessment benchmark** (`tests/e2e/bench/`), on developer
   machines and deliberately not in CI. It is **temporary**: it retires
   once the rewrite demonstrably beats the old server, and no
   abstractions are built for it. It is one Python harness with one
   headline KPI, the wall-clock time until the client holds a fully
   materialized DataFrame, measured for one (protocol, server) pair per
   run. The pairs are the native `quasardb` Python client against qdbd,
   the `/api/v1/query` JSON protocol (parsed client-side) against the
   old _and_ the new server, and Arrow Flight SQL against the new
   server. Running the unchanged v1 client code against both servers is
   the semantic drop-in compatibility check, through normalized result
   fingerprints. The byte-shape check lives in item 2. The Arrow IPC
   stream of `POST /api/v2/query` against the new server is a fifth
   pair, the first number the rewrite gets. The bench is the one home of
   every measured number. The supporting metrics are time to first byte
   (with a per-protocol definition), client peak RSS, REST-server peak
   RSS, and the two data volumes (qdbd -> reducer, reducer -> client)
   plus client CPU, which make the map/reduce offload visible for
   aggregate and top-k queries. The judgment rule: increased gateway
   compute is a win whenever the client wall clock improves versus the
   old server. The bench consumes the e2e harness's qdbd and dataset and
   owns only its venv, the old-server build, and the REST-server
   lifecycle per run. Cross-run functional equivalence is validated by
   comparing persisted, normalized result fingerprints. The
   specification lives in `docs/bench.md`.

## Milestones

The milestones are ordered by dependency and risk, with no artificial
timelines. Each milestone has entry and exit criteria defined when it
starts.

- **M0 -- Foundation**: repo skeleton, YAML config, logging, TLS, status
  probes, Buildkite CI on all supported platforms (Linux, Windows,
  FreeBSD, macOS, where the CI matrix defines the exact arch and variant
  list, with qdb-nats-connector's pipeline as the reference) with the
  C-API artifact dance (static `libqdb_api.a` on Linux), e2e harness and
  benchmark scaffolding against a live qdbd.
- **M1 -- v2 query**: auth core (JWE, key derivation, rolling keys),
  connection pool core (budget, breaker, retry), `POST /api/v2/query`
  streamed through all four encoders, bearer authentication,
  `POST /api/v2/auth/login` (access token only), gzip and zstd
  response compression.
- **M2 -- Tables, reader and ingest**: `POST /api/v2/tables` (create),
  `DELETE /api/v2/tables/{name}`, `GET /api/v2/tables/{name}/rows`
  (the table reader: the bulk reader streamed through all four
  encoders) and `POST /api/v2/rows` (multi-table ingest routed by a
  `$table` column, with CSV, NDJSON and Arrow IPC bodies,
  `Content-Encoding`, and push and deduplication modes) with their
  ingest/read roundtrip property tests per input format, and the v2 e2e
  flow (login, create, query empty, ingest, read in every format and
  coding, Testing doctrine 2) green in Buildkite.
- **M3 -- v2 auth**: `/api/v2/auth/refresh`, `/api/v2/auth/logout`,
  `GET /api/v2/session`, access and refresh TTL configuration, key
  rotation through refresh.
- **M4 -- Drop-in compat**: the v1 endpoints as thin wrappers over
  their v2 counterparts: `/api/v1/login` (12h tokens) and
  `/api/v1/query` (and their unversioned compat aliases) with the
  `v1` golden suite green in Buildkite, and the tag-find core in
  `internal/qdb` that the `find` wart wraps (the v2 core M7's tags
  endpoint reuses). The outcome replaces the old binary at a customer
  site with no client changes. It is the first shippable binary.
- **M5 -- Resilience**: `/metrics`, the graceful-drain and
  concurrency stress as behaviour assertions in Buildkite.
- **M6 -- Flight SQL (minimal)**: gRPC listener, Handshake auth,
  `CommandStatementQuery`/`DoGet`, honest `GetSqlInfo`, ADBC smoke tests.
  The bench retires once the new server wins on both of its rows.
- **M7 -- Exploration**: tables list and schema, tags, and cluster and
  node status.
- **M8 -- Embedded DuckDB**: `/api/v2/sql` backed by go-duckdb with the
  quasardb extension, resource governance, streamed responses through the
  shared encoders.
- **M9 -- Release**: hardening, docs rewrite in `qdb-documentation`
  (including removal of the cluster-endpoint and Prometheus
  remote-storage sections, and fixing the stale `tls_port` sample
  configs), `qdb-release` version registration, Windows service mode,
  migration notes covering the dropped cluster endpoints and Prometheus
  remote read/write.

The ordering rationale. A v1 route wraps its v2 counterpart, so the v2
core must exist before any v1 route is written (ADR-0007). M1 carries a
minimal login so the query endpoint is exercisable end to end. M2
follows at once because the e2e flow needs a table to create, rows to
ingest and a whole-table read that does not materialize, and the
endpoints are cheaper than a fixture the flow would later throw away.
The ingest is multi-table from the start, because one batch push over
several tables is the common case and the writer already takes several
tables. From M2 on, every milestone closes on CI evidence of the built
binary (ADR-0013, ADR-0014). M3
completes v2 auth before anything ships, so the first shippable binary
exposes no v2 endpoint whose shape or lifetime is still moving, and the
v1 login wraps a finished counterpart. Flight SQL precedes exploration and
multi-table ingestion because the gateway thesis is why the project
exists, the Arrow encoder is fresh from M1, and the bench retires as
soon as Flight SQL is measured.

## Versioning and release

- Package version pinned 1:1 to the QuasarDB server release.
- There is one version string location in this repo, a `VERSION` file at
  the repo root, registered with `qdb-release`'s central version manager
  in that tool's version-string format for this project
  (`{xyz}-{stage}.{stage_version}`). Build metadata (version, commit,
  build time, build mode, arch level) is injected into the binary
  through `-ldflags` at build time, following qdb-nats-connector's
  ADR-011 pattern. No version constants live in source files, and
  nothing is sed-patched during the build.
- Vendoring: the full `vendor/` tree is committed, and `qdb-api-go` is
  updated by version bump only, never patched locally.

## Risks and open explorations

1. **Static `libqdb_api.a` availability** per platform: the server build
   bundles a self-contained `.a` on Linux only, where `qdb-api-go` links
   it statically. Every other platform links the shared library and
   relies on rpath or loader-path setup (`.envrc`).
2. **qdb-api-go materialization ceiling**: memory is bounded in this
   binary but not in the binding (Architecture: Data plane, the
   materialization constraint), and lifting that ceiling is upstream
   work this project does not control.
3. **Gateway latency shape**: aggregation-heavy queries get dramatically
   faster from thin clients. Small point queries pay one extra
   (in-datacenter, sub-millisecond over persistent channels) hop versus a
   direct native connection. Benchmarks must cover both, so the trade is
   measured rather than assumed.
4. **Array-typed query results**: the C API defines `array_*` query-result
   value types that `qdb-api-go` currently drops silently. v2 cannot
   return array-valued results correctly until that is fixed upstream.

## Open questions

1. Access/refresh token TTL defaults (proposal: 15 min access and 7 day
   sliding refresh, with the v1 endpoint staying at 12 h).
2. Whether v2 ingestion should also accept the v1 tables/columns JSON
   shape for symmetry, or Arrow IPC/NDJSON/CSV only.
3. Final name for the DuckDB-backed endpoint (`/api/v2/sql` is the working
   name).
