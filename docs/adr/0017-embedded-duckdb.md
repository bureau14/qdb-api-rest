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
attached catalog, and qdb-duck builds on every platform of the matrix.
The owner made the decisions below on 2026-10-13, and Decision 4 on
2026-10-15.

## Decision

### Endpoint and dependency

1. **DuckDB is reached through `POST /api/v2/query` behind the `#sql`
   prefix (ADR-0018, Decision 4).** The request is the query's
   (ADR-0010): a text body, `Accept` negotiation,
   a bearer access token, the shared encoders. The query text is
   DuckDB SQL, the cluster is attached as `qdb`, and `qdb_query` is
   available. The result streams through `EncodeStream` one DuckDB
   vector at a time.
2. **The binary links qdb-duck's package.** `libduckdb_bundle.a` on
   Linux and FreeBSD, linked before `libqdb_api`, and the dynamic
   `libduckdb` on macOS and Windows, fetched by the qdb-artifacts plugin
   into `duck/` next to `qdb/` and located by `.envrc`. The binding's
   link tag follows the library: `duckdb_use_static_lib` on Linux and
   FreeBSD and `duckdb_use_lib` on macOS and Windows, with
   `duckdb_arrow` everywhere, exported once by `.envrc` through
   `GOFLAGS`. Under `duckdb_use_static_lib` the binding registers the
   bundle's linked extensions from its package init before any
   database opens, so the bundle carries DuckDB's generated loader. The
   dynamic libraries register their own. The binding's five prebuilt
   DuckDB library modules are replaced by stubs so that a re-vendor
   does not commit them (`third_party/AGENTS.md`).
3. **The DuckDB release is pinned in pairs.** The qdb-duck DuckDB
   submodule and the go-duckdb tag name the same release. Today that
   is 2.0.0, so go-duckdb is taken from the tag line that bundles it,
   and the next bump moves both.
4. **FreeBSD is coverage, not a release target.** FreeBSD links
   `libduckdb_bundle.a` next to the shared `libqdb_api.so`, and the
   route and the round trip's sql step behave the same on all eight
   platforms. The platform is in the matrix to surface obscure edge
   cases, and no FreeBSD release is expected.

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
11. **Tests.** The round trip gains a step that reads every generated
    table back through `#sql` on `/api/v2/query` in every format, and checks the
    DuckDB read against the rows generated, as the native read is
    checked. That step is the equivalence check between the native path
    and the DuckDB path. The e2e round trip gains the same step.

## Consequences

- M4 starts with this ADR and ends when the endpoint, the `duckdb`
  block and the round-trip steps are landed and green in Buildkite on
  every platform.
- `internal/olap` exposes the query as the batch iterator the encoders
  take, and M5's Flight SQL endpoint calls the same function for the
  DuckDB layer.
- `duckdb.threads` bounds how many qdbd sessions one user's query opens
  at once, because a qdb-duck scan takes one worker per four shards
  and one handle per worker.
- qdb-duck answers `$timestamp` as `timestamp[ns]`, the native
  reader's type, so the equivalence check needs no cast.
- On Windows the cgo compiler is a UCRT build of MinGW, because the
  DuckDB binding frees its own cgo allocations inside `duckdb.dll`, and
  only a UCRT MinGW shares the heap with the MSVC runtime there (owner,
  2026-10-16). The root `.envrc`, MINGW branch, explains the mechanism.
- Open question 2 of the brief is closed.

## Alternatives rejected

| Alternative                                                     | Why not                                                                                                                                        |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| One shared DuckDB instance with every user's catalog            | every connection sees every attached catalog, so one user queries under another's credentials                                                  |
| One instance per request                                        | every request attaches and dials the cluster, and the handle pool is thrown away each time                                                     |
| A shared service credential for DuckDB                          | the user's permissions would no longer apply, which the brief rules out                                                                        |
| Dropping FreeBSD from the platform matrix                       | the platform surfaces obscure edge cases, and the binary there is the same as on Linux                                                         |
| Bumping qdb-duck to the newest go-duckdb release                | the header and the library must come from one release, and qdb-duck's pin is the one tested against the cluster                                |
| Refusing `CREATE VIEW` and `CREATE SEQUENCE` server-side        | qdbd already enforces write permission per user, and a statement filter would be a second authorization layer to keep correct                  |
| An upstream pool size parameter before M4                       | the pool is correct as is, and the budget question is better answered once the milestones are done                                             |
| A standalone property test of native-versus-DuckDB equivalence  | the round trip's sql step already checks both paths against the generated rows, and a second test would be a second oracle for the same rows   |
| An msvcrt build of MinGW on Windows                             | its heap is private, so the binding's frees inside `duckdb.dll` corrupt it                                                                     |
| A MinGW build of qdb-duck's Windows package                     | a second Windows build to maintain, while the MSVC package and a UCRT MinGW already share one heap                                             |
| A registration call of this repository's own in `internal/olap` | the binding's init already makes it under the static-lib tag, and a second call is a no-op that would have to be kept in step with the binding |
| Freeing the binding's cgo allocations with `C.free` in vendor/  | the next re-vendor overwrites a local patch, and the change belongs upstream in duckdb-go-bindings                                             |
