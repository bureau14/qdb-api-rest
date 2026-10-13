# ADR-0017: embedded DuckDB: the qdb-duck package, one instance per user, governance and FreeBSD

Status: accepted
Date: 2026-10-13

## Context

The brief (Embedded DuckDB) embeds DuckDB with the quasar extension
behind a dedicated endpoint, and ADR-0015 places it first among the
preview's features. qdb-duck now links the static C API on Linux and
packages DuckDB with the extension inside, in the layout of the C API
package, for exactly this consumer. go-duckdb links such a package
through its `duckdb_use_static_lib` and `duckdb_arrow` build tags and
names the Linux archive `libduckdb_bundle`. Three facts of the two
dependencies shape the rest: DuckDB gives every connection of an
instance the same view of every attached catalog, qdb-duck binds
credentials at `ATTACH` time and pools up to 100 qdbd handles per
attached catalog, and neither qdb-duck nor go-duckdb builds on FreeBSD.
The owner made the decisions below on 2026-10-13.

## Decision

### Endpoint and dependency

1. **The endpoint is `POST /api/v2/sql`.** Its request is the one of
   `POST /api/v2/query` (ADR-0010): a text body, `Accept` negotiation,
   a bearer access token, the shared encoders. The query text is
   DuckDB SQL, the cluster is attached as `qdb`, and `qdb_query` is
   available. The result streams through `EncodeStream` one DuckDB
   vector at a time.
2. **The binary links qdb-duck's package.** `libduckdb_bundle.a` on
   Linux, linked before `libqdb_api.a`, and the dynamic `libduckdb` on
   macOS and Windows, fetched by the qdb-artifacts plugin into its own
   directory next to `qdb/` and located by `.envrc`. The two build tags
   are set in the Makefile and `20.build.sh`.
3. **The DuckDB release is pinned in pairs.** The qdb-duck DuckDB
   submodule and the go-duckdb tag name the same release. Today that
   is 1.5.4, so go-duckdb is taken from the tag line that bundles it,
   and the next bump moves both.
4. **FreeBSD builds without DuckDB.** `internal/olap` carries a
   `!freebsd` build constraint and a FreeBSD stub with the same
   surface. The route is mounted everywhere and answers `501` on
   FreeBSD. The round trip skips its sql step there and the platform
   matrix keeps its eight entries.

### Instances and credentials

5. **One DuckDB instance per user.** The instance is a field of the
   user's pool state in `internal/qdb`, opened on the user's first sql
   call, attached once with the username and secret from the token
   and the cluster public key from the config, and closed when the
   user pool is evicted. Anonymous is one user. A request opens one
   DuckDB connection on the instance and closes it when the response
   ends. No query runs under a shared service credential.
6. **Governance is the `duckdb` config block.** Memory limit, worker
   threads and spill directory with its size cap per instance, and a
   cap on live instances. Defaults are conservative. The key names and
   values are the handler's.
7. **qdb-duck's handle pool is accepted as is.** Its sessions do not
   count against `pool.max_sessions` and idle handles live until
   detach. The brief's Risks and open explorations carries the item
   for after the milestones.
8. **Writes are qdbd's decision.** `CREATE VIEW` and `CREATE SEQUENCE`
   write blobs under the user's credentials and qdbd refuses them for
   a user without write permission. The server adds no filter.
9. **Isolation is logical.** Own instances, own memory limit, no state
   shared with the native pool. A crash in DuckDB is a crash of the
   binary, and the breaker is not fed from this path.

### Errors and tests

10. **Errors.** A DuckDB parse, bind or execution error is `400` with
    DuckDB's message as the detail. The instance cap is `503` with
    `Retry-After`. Content type, body cap and auth reuse ADR-0010.
11. **Tests.** A property test generates a table, reads it through
    `/api/v2/sql` and `/api/v2/query` and requires the decoded results
    to be equal. The round trip gains a step that reads the generated
    rows back through `/api/v2/sql` in every format and coding.

## Consequences

- M4 starts with this ADR and ends when the endpoint, the `duckdb`
  block, the equivalence property test and the round-trip step are
  landed and green in Buildkite on every platform, FreeBSD answering
  `501`.
- `internal/olap` exposes the query as the batch iterator the encoders
  take, and M5's Flight SQL endpoint calls the same function for the
  DuckDB layer.
- `duckdb.threads` bounds how many qdbd sessions one user's query opens
  at once, because a qdb-duck scan takes one worker per four shards
  and one handle per worker.
- The `$timestamp` to DuckDB type mapping decides whether the
  equivalence test passes without a cast, and is checked when the test
  is written.
- Open question 2 of the brief is closed.

## Alternatives rejected

| Alternative                                              | Why not                                                                                                                       |
| -------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| One shared DuckDB instance with every user's catalog     | every connection sees every attached catalog, so one user queries under another's credentials                                 |
| One instance per request                                 | every request attaches and dials the cluster, and the handle pool is thrown away each time                                    |
| A shared service credential for DuckDB                   | the user's permissions would no longer apply, which the brief rules out                                                       |
| Dropping FreeBSD from the platform matrix                | the REST binary without DuckDB is a valid build there, and a `501` route costs a stub                                         |
| Bumping qdb-duck to the newest go-duckdb release         | the header and the library must come from one release, and qdb-duck's pin is the one tested against the cluster               |
| Refusing `CREATE VIEW` and `CREATE SEQUENCE` server-side | qdbd already enforces write permission per user, and a statement filter would be a second authorization layer to keep correct |
| An upstream pool size parameter before M4                | the pool is correct as is, and the budget question is better answered once the milestones are done                            |
