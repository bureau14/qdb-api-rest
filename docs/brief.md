# QuasarDB REST API -- Rewrite Project Brief

Status: approved. This document is the source of truth for the scope and
direction of the qdb-api-rest rewrite. Hard design decisions made during
development are recorded as ADRs under `docs/adr/`; this brief records the
decisions made before development started. Progress against the milestones
below is tracked only in `docs/log.md`; documentation conventions and the
recommended reading order are in `docs/AGENTS.md`.

## Vision

A single, dependency-vendored Go binary that is QuasarDB's HTTP front door at
customer sites. It serves three things:

1. A versioned, streaming-first REST API (`/api/v2/*`) for query, ingestion,
   and database exploration. (The legacy unversioned API is retroactively
   "v1"; see Compatibility contract.)
2. An Arrow Flight SQL endpoint (gRPC) for Arrow-native clients (ADBC,
   JDBC), minimal by design.
3. An embedded DuckDB OLAP engine (via qdb-duck) exposing full SQL over
   QuasarDB data through a dedicated query endpoint.

Performance is the headline requirement. The current implementation fully
materializes query results in memory (twice) before writing a single byte;
the rewrite streams everything: response memory is bounded regardless of
result size, and time-to-first-byte is independent of result size wherever
the underlying client API allows it.

Operational and SRE concerns are first-class, not an afterthought: the
binary is cloud-native by default (12-factor configuration, stdout logging,
Prometheus metrics, health probes, graceful shutdown), fails fast and
honestly under overload, and treats QuasarDB connection reuse as
non-optional.

The project replaces the go-swagger-generated server on `master`. A
server-side rendered dashboard replacing the (unused) ClojureScript SPA is a
future direction, not part of the initial scope. The project is developed
entirely by LLM agents over roughly 3 months; this document and the ADRs
exist primarily so that an agent working on any milestone can recover the
intent and constraints without re-deriving them.

## Strategic context: the gateway direction

Why performance is existential rather than cosmetic. This is QuasarDB's
design, not the REST API's -- but it is the context this project exists in.

Terminology: the product is, and remains, the **QuasarDB REST API** --
`qdb_rest`, the `qdb-rest` packages -- in all company and customer
communication. "Gateway" in this document names the architectural _role_
the REST API plays in this direction (a query gateway / coordinator in
front of the cluster), not a new product name. If the gateway path ever
becomes the default interface for deployments, renaming becomes a
product/marketing decision to take then, not now.

QuasarDB's protocol offloads a large part of query processing to the
client: the model is map/reduce-style, and the reduce phase runs inside
`libqdb_api` in the client process. Two consequences: direct cluster
connections require a heavyweight native library plus client-side compute
and memory, and the qdbd protocol is peer-to-peer with the client -- it
does not work behind NAT.

A fast, low-latency, high-memory REST gateway co-located with the cluster
inverts this. Consider
`select user_id, count(user_id) from table in range (today(), -7d) group by user_id order by count(user_id) desc limit 10`:
a remote native client pulls per-shard partial aggregates across the WAN
and reduces locally, only to discard everything but ten rows; the gateway
reduces next to the data and ships exactly ten rows of Arrow. This is the
coordinator-node pattern of Trino/ClickHouse/Elasticsearch, applied to
QuasarDB's client-offload design -- and a single HTTPS/gRPC port is
LB-able and NAT-traversable in a way the native protocol cannot be.

The intended client story: client APIs accept either `qdb://<ip:port>`
(native link) or `http(s)://<uri>` (gateway link) and switch transports
dynamically on the URI scheme. On the gateway path clients become thin --
e.g. `quasardb.pandas` speaking Flight SQL via pyarrow/ADBC, receiving
Arrow, converting to DataFrames: no cgo, no bundled native library,
trivial packaging. Upgrades centralize the same way: updating the client
engine embedded in the gateway upgrades every gateway-path user at once,
which is impactful precisely because QuasarDB offloads so much to the
client. Migrating the individual client APIs is out of scope here
(separate projects, `qdb-api-python` first); this project designs the
protocol surface with those clients explicitly in the loop.

The goal, concretely: make this interface -- including the embedded
DuckDB engine -- good enough that the gateway is a reasonable choice for
high-volume, high-intensity workloads, and, depending on how well it
performs, perhaps eventually the default choice for all deployments. The
REST API is a super important component that may grow into an even more
prominent part of QuasarDB deployments; that is why the performance bar,
the SRE machinery (the gateway absorbs the reduce phase for all its
clients), and the horizontal-scale-friendly stateless design are set
where they are.

## Ecosystem

| Project                         | Relationship                                                                                                                                                                                                          |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `quasardb`                      | The database server. REST API package version is pinned 1:1 to server releases.                                                                                                                                       |
| `qdb-api-go`                    | The cgo client binding; the primary path to the cluster. Vendored, never forked: improvements are merged upstream first, then the vendored copy is updated.                                                           |
| `qdb-duck`                      | Native DuckDB extension that attaches QuasarDB clusters (catalog integration, handle pool). Embedded into this binary via go-duckdb to provide the full-SQL endpoint.                                                 |
| `qdb-grafana-plugin`            | External consumer of `/api/login`, `/api/query`, `/api/tags`. Defines the backwards-compatibility surface together with customer code.                                                                                |
| `qdb-api-python` (and siblings) | Future gateway consumers: client APIs gain an `http(s)://` transport (Flight SQL / HTTP) next to the native `qdb://` link. Migrations are separate projects; this project designs the protocol with them in the loop. |
| `qdb-dashboard`                 | Legacy ClojureScript SPA, unused by customers. Retired; no feature-parity obligation.                                                                                                                                 |
| `qdb-pkg-debian`, `qdb-pkg-rpm` | Package the binary as `qdb-rest` with a systemd unit.                                                                                                                                                                 |
| `qdb-docker`                    | Today bundles the REST binary into the `qdb-dashboard` component image; this project adds a first-class standalone image.                                                                                             |
| `qdb-release`                   | Central version manager. The rewrite must register its version-string location(s) there.                                                                                                                              |
| `qdb-documentation`             | Public docs; `user-guide/tools/qdb_rest.rst` and `user-guide/api/rest.rst` must be rewritten at the end of the project.                                                                                               |

Deployment reality: customers run `qdb_rest` as a systemd service from
deb/rpm packages, with HTTP on 40080 and HTTPS on 40443, often behind
a load balancer whose health checks hit the status endpoints. Docker images
exist but package-based deployment dominates today; the rewrite should make
container deployment first-class without breaking the package path.

## Goals

1. **Protocol performance.** Streamed response path end-to-end; no
   full-response buffering; content-negotiated wire formats; HTTP/1.1 and
   HTTP/2. Compression (zstd, gzip) is strictly client-negotiated via
   `Accept-Encoding` -- identity is the default, the server never forces
   it, so datacenter clients pay nothing and WAN clients opt in.
   Performance budgets enforced in CI.
2. **Backwards compatibility** for the endpoints customers and the Grafana
   plugin actually use: `/api/login`, `/api/query`, `/api/tags`, and the
   status probes. Byte-shape compatible, warts included (see Compatibility
   contract).
3. **A proper versioned API** (`/api/v2/*`) with a real resource model:
   query, table listing and schema inspection, table creation, ingestion
   (multi-table), tags, cluster and node status, health.
4. **Arrow Flight SQL**, minimal implementation, for the Arrow-native
   client ecosystem (ADBC, JDBC).
5. **Embedded DuckDB** (qdb-duck) behind a dedicated endpoint: full SQL --
   joins, window functions, the entire DuckDB surface -- over QuasarDB
   data, from the same binary.
6. **Security hygiene**: no default cryptographic keys baked into the
   binary, authenticated encryption for tokens, rolling key support,
   short-lived access tokens.
7. **Cloud-native by default**: YAML/env/flag configuration, structured
   logs to stdout, Prometheus `/metrics`, k8s-compatible liveness and
   readiness probes, graceful shutdown draining in-flight streams, and a
   statically linked Linux binary (new static `libqdb_api.a`) so
   deployment is copy-one-file.
8. **SRE-first resilience**: elaborate QuasarDB connection pooling
   (connection reuse is non-optional), circuit breakers, admission
   control, honest fast failure under overload. See Architecture.
9. **Useful statistics**: a Prometheus exposition endpoint with request
   and query latencies, rows/bytes streamed per format, time-to-first-byte,
   pool and breaker state, qdb call durations, Go runtime and build info.

## Non-goals

- **No SPA, no frontend framework, no node/npm anywhere.**
- **No packaging and no Docker image** (owner decision, 2026-08-24):
  deb/rpm packaging and the standalone Docker image are removed from the
  milestone plan entirely and return only as a deliberate re-add when
  deployment becomes a concern. The `qdb-pkg-*` / `qdb-docker`
  integration moves with them, as does the packaging-finalization note
  that the RPM must mark the config `%config(noreplace)` (Debian's
  conffile handling is the reference).
- **No dashboard in the initial scope.** SSR dashboard is a future
  direction (see Architecture: Dashboard); the routing seam and
  cookie-compatible auth are preserved for it.
- **No Prometheus remote read/write.** The legacy `/api/prometheus/*`
  storage-integration endpoints are dropped. (The new `/metrics`
  exposition endpoint is unrelated to these.)
- **No CSV table-export endpoint.** `/api/tables/{name}.csv` and its
  hardcoded per-customer column logic are dropped.
- **No `/api/option/*` endpoints.**
- **No config-file compatibility.** The config format is redesigned; only
  the wire protocol is compatibility-constrained. (Customers' pain is their
  custom client code, not their install scripts.)
- **No user management.** Users are managed through QuasarDB itself.
- **No changes to `qdb-api-go` inside this repo.** Where the binding is the
  bottleneck (SQL query results arrive fully materialized from the C API's
  one-shot `qdb_query`, which has no incremental-delivery mode), we stream
  what we can (serialization) and file/merge upstream changes separately.
- **No truncate push mode.** The C API's batch-push truncate/backfill mode
  is deliberately not exposed through the REST ingestion API.
- **No multi-tenancy / public-internet hardening** beyond standard TLS and
  auth; this product runs inside customer networks.

## Why this rewrite exists

Two real drivers:

1. **Protocol performance.** The old server materializes entire query
   results as per-cell boxed `interface{}` values, then performs a single
   reflective `json.Encode` over the whole structure. Its deployed
   entrypoint never parses the go-swagger server flag group, so the
   documented HTTP timeouts (60s write, 30s read) are never applied:
   large results blow memory and hang rather than fail fast. Measured on
   the reference dataset (see Testing doctrine): a 5.6M-row `SELECT *`
   produces 834 MB of JSON with a 29 s time-to-first-byte (transmission
   itself takes ~0.1 s -- everything is materialize-then-encode) and
   ~8.4 GB of server RSS. Escaping go-swagger matters only because it
   stands in the way of high-performance protocols.
2. **Token and credential hygiene.** The old JWT embeds the user's raw
   `secret_key`, encrypted with an RSA key that defaults to a keypair
   hardcoded in the binary: token leak equals credential leak, and every
   insecure-mode deployment shares one key.

Additionally, the old codebase accumulated a class of bugs (shared mutable
globals for cluster status, a user's connection pool torn down and
replaced on every re-login, racing in-flight requests) that the new
architecture excludes by construction; and the old dashboard is unused, so
the rewrite sheds it.

The rewrite also modernizes the product's posture: proper RESTful resource
modeling where pragmatic, and a binary that feels native in cloud
ecosystems and is trivial to deploy.

## Compatibility contract

Versioning stance: the legacy unversioned API is retroactively **v1** --
frozen, warts and all, served forever at its historical unversioned paths.
The new API is **v2** under `/api/v2/*`. Legacy endpoints are **not**
redirected (a 307/308 on POST breaks conservative HTTP clients and changes
observable behavior); they are served directly. No new `/api/v1/*` aliases
are minted: that would create brand-new URLs with a permanent support
obligation, purely for taxonomy.

The following endpoints must behave byte-shape identically to the old
server. Golden responses captured from the old server are part of the e2e
test suite; this section is the specification.

### POST /api/login

- Request: `{"username": "...", "secret_key": "..."}` -- literally the
  content of a QuasarDB user private-key file. Empty or absent username
  means anonymous login (insecure clusters); the Grafana plugin relies on
  this.
- Response 200: `{"token": "<opaque string>"}`. The token was a JWT in the
  old server; clients treat it as opaque, so the internal format may change.
  Validity: 12 hours (preserved for this endpoint).
- Response 401: `{"message": "..."}`.
- Outstanding old-server tokens do not survive the upgrade; clients are
  expected to re-login on 401 (verified: the Grafana plugin clears its
  token and retries on 401).

### POST /api/query

- Auth: `Authorization: Bearer <token>` or `?token=<token>` query parameter
  (legacy only; v2 does not accept tokens in URLs).
- Request: `{"query": "..."}`. (The old README showing a bare JSON string is
  wrong; the deployed binder and the Grafana plugin both use the object
  form.)
- Response 200: `{"tables": [{"name": "...", "columns": [{"name": "...",
"type": "...", "data": [...]}]}]}`.
- Column types: `blob | double | int64 | string | timestamp | count | none`.
- Warts preserved verbatim on this legacy endpoint (and only here):
  - The string `"(void)"` appears in place of a minimum-timestamp sentinel.
  - The string `"(undefined)"` appears in place of a null int64.
  - A query whose text begins with the literal prefix `find` is routed to
    the tag-find API and returns tables with names only, no columns. The
    match is a raw, case-sensitive, untrimmed prefix test
    (`strings.HasPrefix(query, "find")`): leading whitespace defeats it,
    and any query starting with those four bytes (e.g. `finder ...`) is
    routed too.
- Errors 400/500: `{"message": "..."}`.

### GET /api/tags

- Auth as above. Optional `?regex=` parameter.
- Response: the same `QueryResult` shape as `/api/query`, containing a
  single table named `""` with one column `name` of type `string`.
- Consumer: the Grafana plugin's tag autocomplete.

### GET /api/status/liveness, GET /api/status/readiness

- Unauthenticated, empty body. Load balancers at customer sites
  health-check these paths; keep them verbatim (also mirrored under
  `/api/v2/status/*`). Success is `200`; readiness failure is `503`
  (the old server answered `500` with a JSON message; that shape is
  deliberately not preserved, `503` being what probes and load
  balancers understand as "not ready"). Readiness dials the cluster as
  the service user on every probe (ADR-0004).

### Explicitly dropped

`/api/prometheus/read`, `/api/prometheus/write`, `/api/tables/{name}.csv`,
`/api/option/parallelism`, `/api/option/max-in-buffer-size`,
`/api/cluster`, `/api/cluster/nodes/{id}`. The cluster endpoints have no
known consumer (the Grafana plugin does not call them) but are publicly
documented, as is the Prometheus remote-storage integration; both removals
are covered explicitly in the M5 migration notes and the
`qdb-documentation` rewrite. v2 provides cluster-status equivalents.

## Architecture

### Data plane: streaming HTTP + Arrow Flight SQL

One binary, two listeners:

- **HTTP listener** (existing ports): REST control plane, legacy compat
  endpoints, `/api/v2` data plane. HTTP/1.1 and HTTP/2.
- **gRPC listener** (dedicated port, default 40493): Arrow Flight SQL. Not
  multiplexed onto the HTTP port: gRPC and plain HTTP/2 are
  indistinguishable at accept time (same `h2` ALPN), so sharing a port
  means either grpc-go's shared-port path (`Server.ServeHTTP` through
  `net/http`, officially experimental and slower than its native
  transport, which defeats the purpose) or fragile byte-sniffing -- and
  REST-appropriate write timeouts and LB idle rules would kill long-lived
  gRPC streams. Customers who do not use Flight SQL simply do not open the
  port.

`POST /api/v2/query` is a streamed response in a content-negotiated format
(ClickHouse-HTTP-style):

| Accept                                | Encoding                                                          |
| ------------------------------------- | ----------------------------------------------------------------- |
| `application/json` (default)          | Legacy-compatible tables/columns shape, streamed as it serializes |
| `application/x-ndjson`                | One JSON object per row                                           |
| `text/csv`                            | RFC 4180                                                          |
| `application/vnd.apache.arrow.stream` | Arrow IPC stream, columnar batches                                |

All formats are produced by one query-execution core with N encoders. v2
uses proper nulls per format instead of the legacy sentinel strings.

Compression, spelled out once: response compression is negotiated via
`Accept-Encoding` (zstd, gzip; identity default, never forced); ingestion
symmetrically accepts `Content-Encoding: zstd|gzip` request bodies; Arrow
IPC additionally supports its own in-format record-batch buffer compression
(lz4/zstd, part of the IPC spec), which is independent of HTTP-level
compression and also request-negotiable.

Known constraint: `qdb-api-go` returns SQL query results fully materialized
-- the C API's one-shot `qdb_query` has no incremental-delivery mode
(`qdb_query_continuous` is a live-query subscription that re-delivers
results on a refresh interval, not a cursor). Streaming therefore initially
overlaps serialization and transmission with iteration over the
materialized result -- bounding REST-server memory and giving early first
byte, but not removing the binding-side materialization. Relieving that
requires upstream work (a cursor-style query API, or building on the C
API's unwrapped Arrow paths: `qdb_query_to_arrow`, the bulk reader's
batched fetch), out of scope here.

### Arrow Flight SQL (minimal)

Decision: Flight SQL rather than plain Flight RPC, implemented minimally.

Rationale: QuasarDB is not a full SQL database (no client-driven
transactions -- individual queries are atomic -- no prepared statements,
its own query dialect) -- but Flight SQL treats query
text as an opaque, dialect-agnostic string, and transactions/prepared
statements are optional capabilities advertised via `GetSqlInfo`. The value
of Flight SQL over plain Flight is the existing client ecosystem: stock
ADBC and JDBC drivers work without custom client code. QuasarDB already
maintains an ODBC driver and generally ships compatibility layers; having
this ecosystem available is a selling point worth the constraint.

Minimal implementation scope: `Handshake` (username+secret -> token) and
bearer-token metadata auth, `CommandStatementQuery` -> `GetFlightInfo` ->
`DoGet` streaming Arrow record batches, and `GetSqlInfo` honestly
advertising what is not supported. Catalog/metadata commands are
implemented only as far as cheap and honest (e.g. `GetTables` from the
table list); prepared statements, transactions, and everything else return
UNIMPLEMENTED. Ingestion via `DoPut` is a possible later addition, not part
of the minimal scope.

Layering, so nobody re-litigates "Flight SQL vs gRPC": gRPC is the RPC
framework; Arrow Flight is a specific standardized gRPC service
(`FlightService`) whose `FlightData` messages envelope raw Arrow IPC
buffers and whose implementations bypass protobuf serialization on the hot
path -- protobuf itself is a poor container for bulk columnar data; Flight
SQL adds no RPCs, only a standardized command vocabulary inside Flight's
opaque descriptors/tickets so stock drivers can operate any conforming
server. A custom gRPC service would re-derive Flight minus the ecosystem
and minus the protobuf-bypass, so it is dominated for data transport;
QuasarDB-specific control semantics live on the HTTP plane, and Flight's
`DoAction` (application-defined actions, tolerated alongside Flight SQL)
is the escape hatch if a qdb-specific RPC is ever needed on this port.

No hand-authored `.proto` files: Flight and Flight SQL protos ship
pre-compiled in `arrow-go`. No custom gRPC services; the control plane is
HTTP-only.

Gateway note: under the gateway direction (see Strategic context), the
primary Flight SQL client is expected to become QuasarDB's own Python API.
This partially de-risks the minimal-subset bet -- we control both ends of
the connection that matters most -- and means the subset is chosen with
`qdb-api-python`'s needs explicitly in the loop; stock ADBC/JDBC
compatibility is upside, not a hard dependency.

### Embedded DuckDB (qdb-duck)

QuasarDB's own query language is deliberately limited; `qdb-duck` is the
native C++ DuckDB extension that attaches QuasarDB clusters into DuckDB
(catalog integration: schemas and tables visible and queryable). This
project embeds DuckDB into the REST binary (via go-duckdb, cgo) with the
quasardb extension loaded, behind a **dedicated endpoint** (working name
`POST /api/v2/sql`), exposing full SQL -- joins, window functions, the
entire DuckDB surface -- over QuasarDB data through the same streaming,
content-negotiated response path. DuckDB's native Arrow integration makes
the Arrow IPC encoder near-free for this path.

This is a major, accepted scope item. It is deliberately isolated: its own
endpoint, its own resource governance (DuckDB memory limits configured
explicitly), failure of the DuckDB subsystem must never degrade the native
query path.

Authorization matches the native path: qdb-duck binds credentials at
`ATTACH` time, so the server maintains one attached catalog and handle
pool per principal, LRU-evicted like the native pools. User queries never
run under a shared service credential. qdb-duck is read-only by design
(`INSERT`/`UPDATE`/`DELETE` are rejected), which is exactly the contract
this endpoint wants.

### /api/v2 endpoint sketch

**This is a very early sketch.** Endpoint shapes, names, and payloads are
decided in ADRs during the v2 milestone; the sketch fixes intent, not
contract. Multi-table ingestion is a hard requirement: `qdb-api-go`'s
`Writer` pushes multiple tables in a single batch-push call
(`qdb_exp_batch_push_with_options`), and the ingest API is designed around
that from the start (these APIs were designed with that function in mind).
Schema endpoints use the full server-side column-type vocabulary (`blob`,
`double`, `int64`, `string`, `symbol`, `timestamp`): `symbol` is a
distinct schema-level type even though query results surface it as
`string`.

```
POST   /api/v2/auth/login          {username?, secret_key?} -> {access_token, refresh_token, expires_in}
POST   /api/v2/auth/refresh        refresh token -> new token pair
POST   /api/v2/auth/logout         invalidates client state; server-side revocation is a future layer
GET    /api/v2/session             "who am i": session/user/instance introspection (see below)
POST   /api/v2/query               native qdb query; streamed, content-negotiated (see above)
POST   /api/v2/sql                 full SQL via embedded DuckDB; streamed, content-negotiated
POST   /api/v2/ingest              multi-table bulk ingest; body content-negotiated (Arrow IPC, NDJSON, CSV);
                                   push mode (transactional|fast|async) via parameter
GET    /api/v2/tables              list tables (prefix filter, pagination)
POST   /api/v2/tables              create table (name, shard size, columns)
GET    /api/v2/tables/{name}       schema: columns, types, shard size, tags
POST   /api/v2/tables/{name}/rows  single-table ingest convenience
GET    /api/v2/tags                list tags
GET    /api/v2/tags/{tag}          entries carrying the tag
GET    /api/v2/cluster             cluster status (nodes, disk, memory)
GET    /api/v2/cluster/nodes/{id}  node detail
GET    /api/v2/status/liveness     unauthenticated probe
GET    /api/v2/status/readiness    unauthenticated probe (uses the service user)
GET    /metrics                    Prometheus exposition (config-gatable)
```

`GET /api/v2/session` ("who am i") returns the authenticated caller's view
of their session, assembled without any cluster round-trip (cheap enough to
poll): username (or anonymous), the cluster URI this server fronts and
whether cluster security is enabled, token introspection (type, issued-at,
expires-at, `jti`, logged-in-since via the `auth_time` claim, which
survives refreshes), and this instance's local view of the caller's
user pool (handles in use / idle / cap; shared by every session of that
user) plus a server instance identifier and version. The instance identifier matters: behind a load
balancer, pool numbers are per-instance truth, and the id makes that
legible instead of confusing. Whatever user metadata QuasarDB exposes
(e.g. permissions) can be added later behind the same endpoint. Secret
material is never echoed back. Primary audience: debugging "who am I
logged in as / why am I unauthorized / where are my connections" -- the
most common support questions an API like this gets.

An OpenAPI 3 document for v2 is maintained by hand as a published artifact
and verified against the implementation in tests. It is not used for code
generation.

### Resilience and connection management

Operational/SRE concerns are primary. QuasarDB connection reuse is
non-optional; the pool is an explicit, elaborate mechanism, not a
convenience wrapper. Decisions:

- **Handle budget**: one configured `max_handles` for the whole server --
  a predictable ceiling on what this binary imposes on the cluster --
  partitioned into per-user sub-pools with caps (handles are
  authenticated per user; anonymous is one user). Idle user pools are
  LRU-evicted. The user is the principal: the pool key is (cluster,
  username), never a session or a token. A QuasarDB user has exactly one
  secret, so every session of a user dials identically and all of them
  share the user's pool. Login finds the existing pool or creates one and
  never replaces or drains one, so in-flight requests are never raced.
  The session id claim (Authentication) is a security abstraction, not a
  pool key. Mechanism: ADR-0003.
- **Circuit breaker, fail fast**: a breaker per cluster opens on
  consecutive connect/timeout failures; while open, requests fail
  immediately with 503 + `Retry-After` (half-open probes test recovery).
  No hanging, no queueing onto a dead cluster, no goodput collapse.
- **Handle health**: no per-checkout ping (a cluster round-trip per
  request contradicts the performance goal). Connection-class errors
  discard the handle and transparently retry once on a fresh one -- for
  idempotent reads only. Ingestion is never auto-retried (batch push
  offers no way to prove non-application); the error is surfaced to the
  client. Handles additionally carry a max lifetime.
- **Admission control**: a configured global limit on concurrent query
  execution with per-principal fair share, so one noisy user cannot starve
  others; excess receives a fast 429 + `Retry-After`. Bounded memory,
  honest overload signaling.
- **Timeouts everywhere**: every qdb call, every request, every stream
  write carries a deadline; graceful shutdown drains in-flight streams.
  Note: `qdb-api-go` exposes only a session-wide timeout and no
  `context.Context` plumbing, so per-call deadlines and cancellation over
  blocking cgo calls are implemented at the pool layer.
- All of it -- pool occupancy, breaker state, admission queue, retry
  counts -- is exported via `/metrics`.

### Authentication

Stateless by requirement: sessions must be long-lived and freely balanced
across multiple REST servers with no shared state. Because each server must
open cluster connections _as the logged-in user_, the token necessarily
carries reconnect material; the design makes that safe rather than
pretending otherwise:

- Tokens are JWE (authenticated encryption; modern primitives, not
  RSA-OAEP + A128CBC) containing username + secret, a `jti`, a session id
  claim (stable across refreshes; a security handle for features such as
  logging out a user's other sessions, never a pool key), a session
  generation claim, a `typ` (`access` or `refresh`) claim, and an
  `auth_time` claim (the original login time, preserved across refreshes;
  surfaced as "logged in since" by `GET /api/v2/session`).
- **Access tokens** are short-lived (minutes); **refresh tokens** are
  long-lived and sliding. `/api/v2/auth/refresh` returns a fresh pair. One
  verifier handles both, plus the 12h tokens minted by legacy `/api/login`.
- **Keys from passphrases**: config holds human-friendly passphrases;
  actual keys are derived once at startup via argon2id (memory-hard: an
  attacker brute-forcing a captured token pays ~100ms per passphrase
  guess) + HKDF for fixed-length key material and domain separation
  (encryption key and `kid` derived independently). Derivation adds no
  entropy -- the passphrase still needs to be decent -- it multiplies
  attacker cost per guess and satisfies the AEAD's key requirements.
- **Rolling keys** fall out of the config shape: a list of passphrases,
  first is current (minting), the rest are accepted for verification.
  Tokens carry `kid`; refresh re-mints under the current key, so rotation
  propagates within one refresh cycle and old entries can then be removed.

  ```yaml
  auth:
    token_secrets:
      - "current passphrase" # minting + verification
      - "previous passphrase" # verification only, during rotation
  ```

- **Ease of use over ceremony**: env-var interpolation in the YAML covers
  cloud secret injection. If no secret is configured, the server generates
  an ephemeral key at startup and logs a warning -- dev setups and
  anonymous clusters work with zero config, at the documented cost that
  tokens do not survive a restart and do not validate across multiple
  instances behind a load balancer (each instance derives its own
  ephemeral key). Secured clusters require an explicit secret. No default
  keys, ever.
- The REST API has its **own QuasarDB service user**, used for readiness
  checks and available for future central coordination.
- **Revocation is deliberately deferred.** The `jti` + generation claims
  make a later QuasarDB-k/v-backed revocation layer (bump generation on
  logout, verify with a ~30s in-memory cache) purely additive.
- Same token everywhere: `Authorization: Bearer` on HTTP, `authorization`
  metadata on gRPC (ADBC/JDBC support this natively), and the Flight
  `Handshake` RPC accepts username+secret and returns a token so DSN-based
  clients work.

### Cluster binding: one gateway, one cluster

The cluster URI and cluster public key are server configuration, never
per-session client input. QuasarDB authentication needs username + user
secret + cluster public key; with the cluster fixed per process, the
public key is fixed with it. Decided deliberately, for three reasons:

1. **The public key is a trust anchor**, not a connection parameter. If
   clients supplied URI + key at login, the server would connect outbound
   to any endpoint any authenticated caller names (an open proxy with
   SSRF characteristics inside the customer network), the trust decision
   would be delegated to arbitrary clients, and tokens -- which embed
   reconnect material by design -- would become portable cross-cluster
   credentials. If clients supplied only a URI, the server would need a
   preconfigured key registry anyway, which is multi-cluster
   configuration, not per-session freedom.
2. **The version pin.** This binary links `libqdb_api` pinned 1:1 to a
   server release; which cluster it can correctly front is a
   deployment-time property, never a session-time one.
3. **The gateway thesis is locality.** The performance argument is the
   reduce phase running co-located with the cluster; N clusters mean N
   co-located gateways, not one gateway dialing N clusters. Per-session
   clusters would also add a cluster dimension to every piece of the
   resilience machinery (budgets, pools, breakers, readiness, metrics).

Escape hatch if multi-cluster demand ever materializes: a **named
cluster registry** in the server config (name -> URI + public key +
service user), selected by name at login, with the name carried as a
token claim and pools/breakers/budgets per named cluster. Deferred until
someone asks; the door stays open at near-zero cost (an optional
cluster-name claim is reserved in the token, and pools are internally
keyed by (cluster, username) even while the cluster count is one).

### Configuration

YAML file (plays well with helm/cloud-init), overridable by environment
variables and flags; env-var interpolation inside the YAML for secret
injection. No compatibility with the old JSON config.
`examples/qdb_rest.yaml` is the commented reference config, pinned to the
defaults by test.

### Observability and logging

- Structured logging via stdlib `slog`: JSON handler by default, pretty
  console handler for interactive use. The application logs to stdout and
  does not manage its own log files -- journald/systemd owns capture on
  Linux, the container runtime in Docker/k8s.
- The logger travels in `context.Context` (`internal/observe`), never as
  a global. The HTTP middleware tags each request context with its id
  (`X-Request-Id`: honored when well-formed, minted otherwise, always
  echoed) and writes one access line; auth adds principal and session the
  same way, and every line logged below inherits those attributes.
  Decision and rationale: ADR-0002.
- The one exception is Windows service mode, where there is no console and
  no logrotate convention: service lifecycle and fatal events go to the
  Windows Event Log (the native facility monitoring agents collect from),
  and the application log stream goes to a self-rotating file via
  `natefinch/lumberjack` (the ecosystem-standard rotation writer; vendored
  upstream, unmodified -- the old repo's fork of it is retired). An
  optional `log.file` config key exposes the same sink on any platform for
  users who want it.
- `/metrics` (Prometheus exposition) as described under Goals.

### Dashboard (future, out of initial scope)

A server-side rendered dashboard (Go `html/template`, `go:embed`, no build
chain) remains the intended replacement for the retired SPA: login, query
console, cluster overview, node detail, table browser. Not part of the
initial project; the HTTP routing seam and cookie-compatible token auth are
preserved so it can be added without rework.

### Project structure

```
cmd/qdb_rest/          entry point (all platforms; Windows service mode included)
internal/config/       YAML config + flags + env
internal/tlsconf/      HTTPS certificates (files or ephemeral self-signed; ADR-0001)
internal/auth/         JWE tokens, key derivation, principals
internal/qdb/          handle/session pools, circuit breaker, query execution, ingestion (wraps qdb-api-go)
internal/encoding/     format encoders: json, ndjson, csv, arrow
internal/httpapi/      /api/v2 handlers + legacy compat handlers + middleware
internal/flightsql/    Arrow Flight SQL server
internal/olap/         embedded DuckDB (go-duckdb + quasardb extension)
internal/observe/      metrics, logging setup
docs/                  this brief, ADRs, OpenAPI document
scripts/tests/setup/   shared qdb-test-setup (qdbd as a service; copied from qdb-nats-connector)
tests/e2e/             golden-data harness (make + shell + curl + awk, live qdbd); bench/ inside is temporary
vendor/                vendored dependencies (committed)
```

Routing: stdlib `net/http` (1.22+ pattern routing) or chi; no framework, no
code generation. Files follow the book pattern: definitions before use,
readable top to bottom.

## Development standards

- **Go version**: always the latest release (currently 1.27), tracked via
  the `toolchain` directive; upgraded promptly when new versions ship.
  New-in-1.27 features are explicitly fair game where they fit: generic
  methods (type parameters on method declarations), the stdlib `uuid`
  package (RFC 9562 -- use it instead of vendoring a third-party UUID
  library, e.g. for token `jti` claims), and `encoding/json/v2` /
  `encoding/json/jsontext` where their streaming or strictness helps an
  encoder (candidate for `internal/encoding`; adopting them there is an
  ADR-worthy decision, not a default).
- **CI**: Buildkite, all platforms; all tests run in Buildkite
  (qdb-nats-connector is the reference for how this should feel). The
  pipeline is authored from scratch for this repo, not carried over from
  the old repo's pipeline: this project doubles as the occasion to
  refactor the shared `qdb-cicd-tools` library, and such improvements are
  made in that repo (tracked as separate work), never as local forks.
  Linux builds statically link the new `libqdb_api.a`.
- **Static checks as gates**: golangci-lint v2 with the gofumpt formatter
  enabled -- the same linter stack as qdb-nats-connector -- pinned to a
  specific version in-repo and installed by the lint step itself, so the
  pin lives in this repository rather than in the builder image.
- **No package-level mutable state.** `context.Context` flows through
  every call path.
- **Logging** via `slog` only, through the context-carried logger and its
  `*Context` methods (ADR-0002); no third-party logging framework.
- **Style**: small composable functions with descriptive names; files read
  top-to-bottom (book pattern); explicit over implicit.

## Testing doctrine

Strong preference for generative and end-to-end tests over unit tests. Unit
tests exist only where a pure function has genuine logic worth pinning.

1. **Generative property tests** (`pgregory.net/rapid`, quickcheck-style),
   run against a live qdbd:
   - _Format equivalence_: for randomly generated schemas, data, and
     queries, the decoded results of JSON, NDJSON, CSV, Arrow IPC, and
     Flight SQL are identical.
   - _Ingest/query roundtrip_: randomly generated data pushed through the
     v2 ingest endpoints (each input format) reads back exactly.
   - _Auth properties_: token roundtrip, expiry, key-rotation continuity,
     refresh behavior -- generated over key/claim space.
2. **Golden-data e2e** (pattern from qdb-nats-connector ADR-007): Make +
   shell + curl + awk orchestration against a live qdbd started by the
   shared `scripts/tests/setup/start-services.sh` (qdbd is a persistent
   service, never started by a test). The canonical dataset is a
   customer-derived 5,613,032-row table (story sc-19522) distributed as
   CSV + `qdb_import` config, sha256-pinned, S3-hosted the way the
   nats-connector golden datasets are, and loaded idempotently by
   `make load`; the input CSV doubles as the expected output for
   full-table `text/csv` equivalence (awk tolerance-compare). Includes
   _legacy equivalence_: small golden request/response pairs captured
   from the old server replayed against the new one. Plan:
   `docs/e2e-plan.md`.
3. **Performance budgets and stress as CI gates**, in the same harness:
   for the full 5.6M-row query -- time-to-first-byte under a fixed
   bound, server RSS delta bounded and independent of result size,
   sustained throughput floor per format (Flight SQL path included);
   plus a concurrency stress (N parallel clients) asserting fast
   429/503 + `Retry-After` under overload rather than goodput collapse,
   and that in-flight streams complete across a graceful-shutdown
   drain. Budgets are versioned numbers in the repo, revised
   deliberately, never silently.
4. **Local assessment benchmark** (`tests/e2e/bench/`; developer
   machines, deliberately not CI; **temporary** -- retired once the
   rewrite demonstrably beats the old server, no abstractions built for
   it): one Python harness, one headline KPI -- wall-clock time until
   the client holds a fully materialized DataFrame -- measured for
   exactly one (protocol, server) pair per run: the native `quasardb`
   Python client against qdbd, the legacy `/api/query` JSON protocol
   (parsed client-side) against the old _and_ the new server, and Arrow
   Flight SQL against the new server. Running the unchanged legacy client
   code against both servers is the drop-in compatibility check
   (semantic, via normalized result fingerprints; byte-shape lives in
   item 2). Supporting metrics: time to first byte (per-protocol
   definition), client peak RSS, REST-server peak RSS, and the two data
   volumes (qdbd -> reducer, reducer -> client) plus client CPU that
   make the map/reduce offload visible for aggregate/top-k queries.
   Judgment rule:
   increased gateway compute is a win whenever client wall clock
   improves versus the old server. It consumes the e2e harness's qdbd
   and dataset and owns only its venv, the old-server build, and the
   REST-server lifecycle per run. Cross-run functional equivalence is
   validated by comparing persisted, normalized result fingerprints.
   The plan lives in `docs/bench-plan.md`.

## Milestones

Ordered by dependency and risk, no artificial timelines. Each milestone has
entry/exit criteria defined when it starts.

- **M0 -- Foundation**: repo skeleton, YAML config, logging, TLS, status
  probes, Buildkite CI on all supported platforms (Linux, Windows,
  FreeBSD, macOS; the CI matrix defines the exact arch/variant list, with
  qdb-nats-connector's pipeline as the reference) with the C-API artifact
  dance (static `libqdb_api.a` on Linux), e2e harness and benchmark
  scaffolding against a live qdbd.
- **M1 -- Drop-in compat**: auth core (JWE, key derivation, rolling keys,
  legacy 12h tokens), connection pool core (budget, breaker, retry),
  legacy `/api/login`, `/api/query`, `/api/tags` with golden equivalence
  tests. Outcome: replaces the old binary at a customer site with no
  client changes.
- **M2 -- v2 data plane**: streaming query engine + all four encoders,
  compression, v2 auth endpoints, tables/schema/tags/cluster endpoints,
  multi-table ingestion, admission control, `/metrics`, performance
  budgets enforced.
- **M3 -- Flight SQL (minimal)**: gRPC listener, Handshake auth,
  `CommandStatementQuery`/`DoGet`, honest `GetSqlInfo`, ADBC smoke tests.
- **M4 -- Embedded DuckDB**: `/api/v2/sql` backed by go-duckdb with the
  quasardb extension, resource governance, streamed responses through the
  shared encoders.
- **M5 -- Release**: hardening, docs rewrite in `qdb-documentation`
  (including removal of the cluster-endpoint and Prometheus
  remote-storage sections, and fixing the stale `tls_port` sample
  configs), `qdb-release` version registration, migration notes covering
  the dropped cluster endpoints and Prometheus remote read/write.

M1 before M2 is deliberate: shipping the drop-in early de-risks the
compatibility story while the new protocol work proceeds.

## Versioning and release

- Package version pinned 1:1 to the QuasarDB server release, as today.
- One version string location in this repo: a `VERSION` file at the repo
  root, registered with `qdb-release`'s central version manager (the old
  registration pointed at go-swagger artifacts that no longer exist). The
  registration must match `qdb-release`'s expected version-string format
  for this project (`{xyz}-{stage}.{stage_version}`), not just supply a
  new file path. Build metadata (version, commit, build time, build mode,
  arch level) is injected into the binary via `-ldflags` at build time,
  following qdb-nats-connector's ADR-011 pattern; no version constants
  live in source files, and nothing is sed-patched during the build.
- Vendoring: full `vendor/` tree committed; `qdb-api-go` updated by version
  bump only, never patched locally.

## Risks and open explorations

1. **Static `libqdb_api.a` availability** per platform: the server build
   bundles a self-contained `.a` on Linux only, and `qdb-api-go` links it
   statically there since QDB-19065 (the upstream change the vendoring
   rule required); every other platform links the shared library and
   relies on rpath or loader-path setup (`.envrc`).
2. **qdb-api-go materialization ceiling**: server-side streaming of native
   query results requires upstream binding work and possibly C API work
   (no existing C API function delivers one-shot query results
   incrementally; candidates are a cursor-style query API or the Arrow
   paths); until then, memory is bounded in this binary but not in the
   binding. The gateway direction raises the stakes: large raw `SELECT`s
   from thin clients materialize in the gateway, making admission control
   the short-term backstop and upstream streaming the long-term relief
   valve.
3. **Gateway latency shape**: aggregation-heavy queries get dramatically
   faster from thin clients; small point queries pay one extra
   (in-datacenter, sub-millisecond over persistent channels) hop versus a
   direct native connection. Benchmarks must cover both so the trade is
   measured, not assumed.
4. **Array-typed query results**: the C API defines `array_*` query-result
   value types that `qdb-api-go` currently drops silently; v2 cannot
   return array-valued results correctly until that is fixed upstream.

## Open questions

1. Access/refresh token TTL defaults (proposal: 15 min access, 7 day
   sliding refresh; legacy endpoint stays at 12 h).
2. Whether v2 ingestion should also accept the legacy tables/columns JSON
   shape for symmetry, or Arrow IPC/NDJSON/CSV only.
3. zstd via pure-Go `klauspost/compress` is assumed acceptable for
   vendoring (it is pure Go, no cgo) -- confirm.
4. Final name for the DuckDB-backed endpoint (`/api/v2/sql` is the working
   name).
