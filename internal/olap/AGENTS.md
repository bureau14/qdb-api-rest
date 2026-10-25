# internal/olap -- Agent Instructions

Scope: the embedded DuckDB engine behind the `#sql` prefix of
`POST /api/v2/query`.
Package-wide Go rules: `internal/AGENTS.md`. The decisions are in
ADR-0017.

- An `Instance` is one in-memory DuckDB instance with the cluster
  attached as `qdb` under one user's credentials. Every connection of an
  instance sees every catalog the instance attached, so an instance
  serves one user, and no query runs under a shared credential.
  `internal/qdb` owns the instances. It holds each one on the user's
  pool state, opens it on the user's first DuckDB call and closes it with
  the pool. The package knows nothing of `internal/qdb`.
- The credentials reach qdb-duck only through a temporary DuckDB
  secret, and the attach names the secret. `secretOf` says why the
  ATTACH string and qdb-duck's `quasar_*` options were rejected.
- qdb-duck pools its own qdbd sessions per attached catalog, up to 100,
  and they live until the instance closes. They do not count against
  `pool.max_sessions` (ADR-0017, Decision 7).
- On Windows the package links only with a UCRT build of MinGW, because
  the binding frees its own cgo allocations inside `duckdb.dll` (root
  `.envrc`, MINGW branch, and ADR-0017).
- A query answers DuckDB's Arrow types, and the sql endpoint answers
  them as DuckDB converts them. A TIMESTAMPTZ arrives as a UTC instant
  tagged with the instance's TimeZone, which defaults to the host's
  zone, and the text encoders write it in UTC. A TIMETZ arrives as a
  plain `time64` without its offset. Setting the instance's TimeZone and
  carrying the TIMETZ offset were rejected, because on the DuckDB path
  the endpoint answers what DuckDB answers and adds no rule of its own.
- A describe prepares one statement and maps its column types to the
  Arrow types DuckDB's conversion gives them (`arrowTypeOf`), because
  duckdb-go keeps the logical type it would need to ask DuckDB. A type
  outside the map is `ErrUndescribable`, and `TestDescribe` keeps the map
  equal to the conversion for every type it covers. A DuckDB bump runs
  that test before anything else.
- `internal/encoding/AGENTS.md` lists the types the text encoders write,
  and a column of any other type fails at the encoder, which names the
  column.
