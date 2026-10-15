# internal/olap -- Agent Instructions

Scope: the embedded DuckDB engine behind `POST /api/v2/sql`.
Package-wide Go rules: `internal/AGENTS.md`. The decisions are in
ADR-0017.

- An `Instance` is one in-memory DuckDB instance with the cluster
  attached as `qdb` under one user's credentials. Every connection of an
  instance sees every catalog the instance attached, so an instance
  serves one user, and no query runs under a shared credential.
  `internal/qdb` owns the instances. It holds each one on the user's
  pool state, opens it on the user's first sql call and closes it with
  the pool. The package knows nothing of `internal/qdb`.
- The credentials reach qdb-duck as a temporary DuckDB secret of quoted
  SQL literals, and the attach names the secret. qdb-duck's ATTACH
  string splits every pair on each `=`, so a base64 key loses its
  padding and a secure attach through the string fails. qdb-duck's
  `quasar_*` options are refused in the DuckDB config at open. The
  secret keeps the user's text inside quoted literals, and
  `duckdb_secrets()` shows the private key redacted. A failed attach is
  `ErrAttach`, because qdb-duck's message does not tell a refused attach
  from an unreachable cluster.
- The `duckdb` config block becomes the instance's DuckDB settings when
  it opens (`dsn`), and DuckDB judges them there.
- qdb-duck pools its own qdbd sessions per attached catalog, up to 100,
  and they live until the instance closes. They do not count against
  `pool.max_sessions` (ADR-0017, Decision 7).
- A query answers DuckDB's Arrow types. The encoders take five types,
  and a column of any other type fails at the encoder, which names the
  column. A caller casts in SQL to one of the five.
