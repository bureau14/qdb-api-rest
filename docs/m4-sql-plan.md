# M4 remainder: POST /api/v2/sql over one DuckDB instance per user

Status: draft

This plan covers the second and third slices of M4 as one unit
(`docs/log.md`, Current state, Next). The conventions are in
`docs/AGENTS.md`, Plans. The plan is deleted when the unit lands.

## 1. Outcome

When the unit lands, the tree has:

- a `duckdb` config block (`memory_limit`, `threads`, `spill_directory`,
  `max_spill_size`, `max_instances`) with its defaults in
  `examples/qdb_rest.yaml`;
- `internal/olap`, which opens an in-memory DuckDB instance under that
  block, attaches the cluster as `qdb` with one user's credentials
  through a temporary DuckDB secret, and runs a query on a connection
  of its own as a sequence of lent record batches;
- the instance as a field of the user pool state in `internal/qdb`,
  opened on the user's first sql call, capped by `duckdb.max_instances`,
  and closed with the user pool;
- `Cluster.SQL`, and `POST /api/v2/sql` in `internal/httpapi`, which
  streams the result through the shared encoders;
- the sql error rows in `TestErrorRows`, the sql step of the in-process
  round trip, and the sql step of the e2e round trip on both clusters;
- `internal/olap/AGENTS.md`, and the rows the other `AGENTS.md` files
  and `docs/e2e.md` gain;
- ADR-0017 Decision 11 stating that the round trip step is the
  equivalence check;
- M4 closed in `docs/log.md`, with the FreeBSD history removed from the
  dated entries and from Current state.

It leaves for later units, each listed under Next by the last commit:

1. widening the encoders to the DuckDB types outside the five wire
   types (owner, 2026-10-15);
2. running the Go data-path tests on the secure cluster too: the round
   trip, its deduplicated variant, the error rows and the qdb read
   tests (owner, 2026-10-15);
3. removing the qdb-duck branch pin from `.buildkite/pipeline.py`
   (section 8, question 1).

## 2. Verified facts

Probes are throwaway Go tests run on 2026-10-15 against the local qdbd
pair (insecure `2836`, secure `2838`) with duckdb-go v2.10504.0 and the
local `duck/` package, then deleted.

| #   | fact                                                                                                                                                                                                                                                                                                                      | evidence                                                                                                                            |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| F1  | The quasar extension is loaded in a fresh in-memory instance with no `LOAD` statement.                                                                                                                                                                                                                                    | probe 2026-10-15: `duckdb_extensions()` answers `quasar`, loaded `true`                                                             |
| F2  | qdb-duck's ATTACH connection string splits every pair on each `=`, so a base64 key loses its padding, and a secure attach through the string fails with "The provided cryptographic key is invalid".                                                                                                                      | `../qdb-duck/src/extension/quasar_config.cpp:199-210`; probe 2026-10-15                                                             |
| F3  | qdb-duck's `quasar_*` options in the DuckDB config at open are refused with "The following options were not recognized".                                                                                                                                                                                                  | probe 2026-10-15                                                                                                                    |
| F4  | `CREATE SECRET <name> (TYPE quasar, HOST '..', PORT '..', USER_NAME '..', USER_PRIVATE_KEY '..', CLUSTER_PUBLIC_KEY '..')` followed by `ATTACH 'secret=<name>' AS qdb (TYPE quasar)` attaches the secure cluster as the test user, and the same secret without the user fields attaches the insecure cluster anonymously. | probe 2026-10-15; `../qdb-duck/README.md:150-155`; `../qdb-duck/src/extension/quasar_extension.cpp:40-50`                           |
| F5  | A value with a doubled single quote (`'it''s me'`) is accepted in a secret, and `duckdb_secrets()` shows `user_private_key=redacted`.                                                                                                                                                                                     | probe 2026-10-15                                                                                                                    |
| F6  | A second connection of the same connector sees the catalog that the first connection attached.                                                                                                                                                                                                                            | probe 2026-10-15                                                                                                                    |
| F7  | The DSN `?memory_limit=1073741824b&threads=4&max_temp_directory_size=0b` reads back as `1.0 GiB`, `4` and `0 bytes`. An in-memory instance's `temp_directory` defaults to `.tmp`, and `?temp_directory=` reads back as the empty string.                                                                                  | probe 2026-10-15                                                                                                                    |
| F8  | `NewConnector(dsn, nil)` with no path opens a fresh in-memory instance per connector. `NewArrowFromConn` and `Arrow.QueryContext` answer an `array.RecordReader`, under the `duckdb_arrow` tag.                                                                                                                           | duckdb-go v2.10504.0 `duckdb.go:60-110`, `arrow.go:1,83-115`                                                                        |
| F9  | Result types: `SELECT 1` is `int32`, `sum(42::BIGINT)` is `decimal(38, 0)`, `true` is `bool`. A scan of `qdb.<t>` answers `$timestamp` as `timestamp[ns]`, and the columns as `int64`, `float64`, `utf8` (string and symbol), `binary` and `timestamp[ns]`, with no `$table` column and no `max_width` metadata.          | probe 2026-10-15; `../qdb-duck/src/extension/quasar_value.cpp:196-251`                                                              |
| F10 | A parse error arrives from `QueryContext` as "Parser Error: ...". An unreachable cluster fails the ATTACH with "Catalog Error: Failed to connect to Quasar: Connection refused.", and a bad key fails it with "Catalog Error: Failed to set user credentials ...".                                                        | probe 2026-10-15                                                                                                                    |
| F11 | Importing the duckdb-go root package adds `github.com/google/uuid v1.6.0 // indirect` to `go.mod`. Today the vendor tree holds only `duckdb-go/v2/mapping`.                                                                                                                                                               | probe 2026-10-15 (`git diff go.mod`); `vendor/github.com/duckdb/duckdb-go/v2/`                                                      |
| F12 | qdb-duck's scan reads through the bulk reader, so after an async push it sees what the table reader sees.                                                                                                                                                                                                                 | `../qdb-duck/src/extension/quasar_reader.cpp:188`                                                                                   |
| F13 | The fixture's tables hold 0 to 40 rows with strictly increasing `$timestamp`, so `ORDER BY "$timestamp"` fixes the row order.                                                                                                                                                                                             | `internal/qdbtest/table/table.go:140-180`                                                                                           |
| F14 | The table fixture runs as the anonymous user, which the secure cluster refuses.                                                                                                                                                                                                                                           | `internal/qdbtest/table/table.go:409-411`                                                                                           |
| F15 | The encoders accept five Arrow types, and any other is `ErrUnsupportedType` naming the column.                                                                                                                                                                                                                            | `internal/encoding/AGENTS.md`, The seam; `internal/encoding/error.go:16-18`                                                         |
| F16 | `qdb.New` takes the whole `config.Config`, so the binary needs no wiring for a new block.                                                                                                                                                                                                                                 | `cmd/qdb_rest/main.go:178`; `internal/qdb/cluster.go:55`                                                                            |
| F17 | bureau14/qdb-duck#17 is merged as `5b80617`, and qdb-duck master build 90 promoted the FreeBSD packages. The qdb-artifacts plugin falls back from the requested ref to `refs/heads/master`.                                                                                                                               | `gh pr view 17` 2026-10-15; message from the qdb-duck session 2026-10-15; qdb-artifacts-buildkite-plugin `lib/artifacts.py:732-736` |

## 3. Design

### `internal/config/config.go`

```go
// DuckDB governs every DuckDB instance the server opens, one per user:
// the memory and worker threads one instance may take, where and how much
// it may spill, and how many instances may be live at once. [ADR-0017 D6]
// The sizes are bytes. [owner 2026-10-14, docs/log.md Next]
type DuckDB struct {
	MemoryLimit    int64  `yaml:"memory_limit" help:"memory one DuckDB instance may use, in bytes"`
	Threads        int    `yaml:"threads" help:"worker threads of one DuckDB instance"`
	SpillDirectory string `yaml:"spill_directory" help:"directory a DuckDB instance spills to; empty disables spilling"`
	MaxSpillSize   int64  `yaml:"max_spill_size" help:"bytes one DuckDB instance may spill"`
	MaxInstances   int    `yaml:"max_instances" help:"DuckDB instances live at once, one per user"`
	// style: doc comment alone
	// because: a config struct whose fields carry their help text
}
```

`Config` gains `DuckDB DuckDB \`yaml:"duckdb"\``after`Auth`. `Default`
gains:

```go
		// style: inline note at the DuckDB literal
		// because: the defaults are a choice an operator reads
		//
		// at DuckDB: the defaults are conservative, because DuckDB's own take most of the RAM and every core, which suits a workstation and not a gateway next to qdbd [ADR-0017 D6; docs/brief.md, Embedded DuckDB]
		// at Threads: four threads bound one user's scan at four qdbd sessions, because a scan takes one worker per four shards and one handle per worker [ADR-0017 Consequences]
		// at SpillDirectory: empty disables spilling, so a query over the limit fails rather than writing to disk [owner 2026-10-15]
		DuckDB: DuckDB{MemoryLimit: 1 << 30, Threads: 4, MaxInstances: 16},
```

The `Default` doc comment gains "one DuckDB instance per user at 1 GiB
and four threads, no spilling, sixteen at once" in its list. The block
is not validated (`internal/AGENTS.md`, Code: config owns shape only):
DuckDB judges the values when an instance opens, and `internal/qdb`
judges `max_instances`. `examples/qdb_rest.yaml` gains a commented
`duckdb:` block with the defaults, worded like the `pool:` block, and
`TestExampleIsTheDefaults` pins it (`internal/config/config_test.go:247`).
If the startup log logs each block through `LogValue`, the executor adds
`duckdb` in the same way. Otherwise nothing is added.

### `internal/model/model.go`

```go
// EmptyBatch is a batch of schema and no rows, the step that a reader of
// no rows answers so that every receiver sees the schema. [internal/qdb/read.go:94-107]
func EmptyBatch(schema *arrow.Schema) arrow.RecordBatch {
	// style: doc comment alone
	// because: a constructor of three obvious steps, moved unchanged
}
```

The function moves from `internal/qdb/read.go` (`emptyBatch`)
unchanged. `qdb.lent` calls it. The move gives the schema-only batch one
home that both `internal/qdb` and `internal/olap` reach
(`internal/model`, "the neutral table representation the layers share",
`docs/brief.md`, Project structure).

### `internal/olap/instance.go` (new file, after `olap.go`)

```go
// Credentials are what an instance attaches the cluster with: the first
// node of the cluster URI, the user, and the cluster public key. An empty
// Username is the anonymous user, and an empty ClusterPublicKey is an
// insecure cluster. [docs/brief.md, Vocabulary; F4]
type Credentials struct {
	Host, Port                    string
	Username, SecretKey           string
	ClusterPublicKey              string
	// style: doc comment alone
	// because: a value type
}

// LogValue renders the credentials without the secret key. [internal/AGENTS.md, Logging: a type that holds a secret implements slog.LogValuer]
func (c Credentials) LogValue() slog.Value

// ErrAttach is the error of an instance whose cluster attach failed. The
// cluster refused the attach or could not be reached, and qdb-duck's
// message does not say which. [F10]
var ErrAttach = errors.New("olap: attach failed")

// Instance is one in-memory DuckDB instance with the cluster attached as
// qdb under one user's credentials. Every connection of the instance sees
// that catalog, so an instance serves one user. [ADR-0017 D5; F6]
// It is safe for concurrent queries, each on its own connection.
type Instance struct {
	connector *duckdb.Connector
	// style: doc comment alone
	// because: a handle type
}

// dsn renders the duckdb block as the settings an instance opens with.
// [F7]
func dsn(cfg config.DuckDB) string {
	// style: doc comment alone + inline note at temp_directory
	// because: one url.Values with a note on the one setting that is not obvious
	//
	// at memory_limit and max_temp_directory_size: DuckDB reads a bare byte count with the b suffix [F7]
	// at temp_directory: always set, because an in-memory instance spills to .tmp in the working directory when it is unset, and the empty string disables spilling [F7; owner 2026-10-15]
}

// quote renders s as a SQL string literal. [F5]
func quote(s string) string {
	// style: doc comment alone
	// because: one ReplaceAll
}

// secretOf is the CREATE SECRET statement that holds c. [F4]
func secretOf(c Credentials) string {
	// style: doc comment alone + inline note at the user fields
	// because: one statement built in one pass
	//
	// at the statement: the values travel as quoted literals of a secret, because qdb-duck's ATTACH string splits on every = and a QuasarDB key ends in base64 padding [F2; Rationale R2]
	// at the statement: the secret is TEMPORARY, so it lives in memory with the instance and is never written to disk [Rationale R2]
	// at the user fields: the user fields are left out for the anonymous user and the key for an insecure cluster [F4]
}

// Open opens an instance under cfg and attaches the cluster as qdb with c.
// An attach that fails is ErrAttach with qdb-duck's message, and no
// instance is left open. [F10; Rationale R5]
func Open(ctx context.Context, cfg config.DuckDB, c Credentials) (*Instance, error) {
	// style: doc comment + numbered overview + step comments
	// because: three things fail on the way, and a failure after the open must close what it opened
	//
	// The instance is in memory, so it holds nothing but the attach and what one user's queries create. [ADR-0017 D5]
	//
	// 1. open the connector under the duckdb block, which DuckDB judges here [internal/AGENTS.md, Code: the consumer owns its checks]
	// 2. on one connection, create the secret and attach through it, because the attach is visible to every later connection of the instance [F6]
	// 3. on a failed attach, close the connector and wrap the message in ErrAttach [F10]
}

// Query runs q on a connection of its own and hands its result to sink,
// one record batch per step, each lent for that step. An error before
// sink runs is DuckDB's (a parse, bind or execution error) and is
// returned as is. The error of sink is returned unchanged. A result of
// no batches is one step of its schema alone. [internal/qdb/read.go:113-145, lent; ADR-0017 D10]
func (i *Instance) Query(ctx context.Context, q string, sink func(iter.Seq2[arrow.RecordBatch, error]) error) error {
	// style: doc comment + numbered overview + step comments
	// because: the connection and the reader each have a lifetime that ends on every path
	//
	// The connection lives for one query and closes when sink returns, so a request holds nothing of the instance afterwards. [ADR-0017 D5]
	//
	// 1. open a connection and its Arrow interface [F8]
	// 2. run q. DuckDB's error is the caller's, and nothing has reached sink [ADR-0017 D10]
	// 3. run sink over the reader's batches. The reader owns each batch until its next step, so the receiver releases none [F8]
	//    - an error the reader reports after its last batch is the last step's error [ADR-0017 D1, the result streams]
	//    - no batch at all yields model.EmptyBatch of the reader's schema [internal/qdb/read.go:136-144]
	// 4. release the reader and close the connection, whatever sink did
}

// Close closes the instance. Its attached catalog and qdb-duck's sessions close with it. [ADR-0017 D7]
func (i *Instance) Close() error
```

`olap.go`'s package comment gains one sentence: the package opens one
instance per user and answers its queries as lent batches
(`internal/olap/AGENTS.md`). `go mod vendor` runs in this commit (F11).

### `internal/qdbtest/table/table.go`

```go
// CreateAs is Create as the user u, for a cluster that refuses the
// anonymous user. [F14]
func CreateAs(t T, c *qdb.Cluster, u qdb.User, tbl Table)

// RemoveOnCleanupAs is RemoveOnCleanup as the user u. [F14]
func RemoveOnCleanupAs(t T, c *qdb.Cluster, u qdb.User, tbl Table)
```

`Create` and `RemoveOnCleanup` become one-line calls of the `As` forms
with `qdb.User{}`, and `call` takes the user. No existing call site
changes. Both get `style: doc comment alone`, because they forward.

### `internal/olap/olap_test.go` (new, `package olap_test`)

```go
// TestQueryReadsGeneratedTable: on each cluster, a generated table read
// through qdb.<name> on an instance attached as that cluster's user
// answers the rows written, and a statement DuckDB cannot parse answers
// before the sink runs. [owner 2026-10-15: the olap test runs on both clusters]
func TestQueryReadsGeneratedTable(t *testing.T) {
	// style: doc comment + step comments
	// because: internal/AGENTS.md, Tests: test bodies carry step comments
	//
	// 1. per cluster, insecure as the anonymous user and secure as qdbtest.SecureUser, so the secure attach proves the secret's credentials [F4; owner 2026-10-15]
	// 2. one instance per cluster, opened once and closed on cleanup, because qdb-duck holds up to 100 qdbd sessions per attach and the test cluster punishes exhaustion [../qdb-duck/src/include/quasar_pool.hpp:14; internal/AGENTS.md, Tests]
	// 3. per rapid iteration, CreateAs a generated table and read it with SELECT "$timestamp", <columns> FROM qdb.<name> ORDER BY "$timestamp" [F13]
	// 4. table.Check every batch against the table [internal/AGENTS.md, Tests: no comparer of its own]
	// 5. the parse error arrives from Query with the sink never called [F10]
}
```

The test is an external test package, because it imports
`qdbtest/table`, which imports `internal/qdb`, which imports
`internal/olap` (section 4, R7).

### `internal/qdb/cluster.go`

```go
// userPool is one user's bounded set of sessions, the user's DuckDB
// instance once a sql call has opened it, the connections open on it, and
// the time at which the pool was last found holding none of these. [ADR-0017 D5; Rationale R4]
type userPool struct {
	pool       *qdbapi.SessionPool
	emptySince time.Time
	// duckMu serialises the instance's open, which dials the cluster, so
	// that two first calls of one user open one instance and other users
	// are not held. [Rationale R4]
	duckMu sync.Mutex
	duck   *olap.Instance // nil until the user's first sql call; guarded by duckMu
	duckIn int            // connections open on duck; guarded by Cluster.mu
	// style: doc comment alone + inline note at duckMu
	// because: a state type with one lock that is not the cluster's
}
```

`Cluster` gains `duckCfg config.DuckDB` and `instances int` (live
instances, guarded by `mu`), set in `New`.

```go
// InstanceLimitError is the error of a sql call that would open an
// instance past duckdb.max_instances. RetryAfter is the eviction tick,
// the soonest an idle instance can close. [ADR-0017 D10; owner 2026-10-15, Decision 8; cluster.go:18]
type InstanceLimitError struct{ RetryAfter time.Duration }
```

`IsClusterUnavailable` (`breaker.go:110`) gains `||
errors.Is(err, olap.ErrAttach)`, and its doc comment says that an
attach that failed counts as an unavailable cluster, because the
server cannot tell a refused attach from an unreachable cluster and a
credential was proven at login (F10, R5). `feedBreaker` keeps calling
`qdbapi.IsClusterUnavailable`, so the breaker is not fed from this path
(ADR-0017 D9).

```go
// credentialsOf is what u's instance attaches with: the first node of
// the cluster URI and the cluster public key read from its file. [owner 2026-10-14, docs/log.md Next: the first node]
func (c *Cluster) credentialsOf(u User) (olap.Credentials, error) {
	// style: doc comment alone + inline note at the URI split
	// because: two lookups with one rule each
	//
	// at the URI split: qdb-duck's ATTACH takes one host and one port, so a multi-node URI is handed over as its first node [owner 2026-10-14]
	// at the key read: the file is read at each open, so a replaced key file takes effect for the next instance [proposal]
}

// instanceOf answers u's instance, opening it on u's first sql call. The
// open counts against duckdb.max_instances, and past the cap the answer
// is an *InstanceLimitError. A failed open is not kept, so the next call
// tries again. [ADR-0017 D5, D10]
func (c *Cluster) instanceOf(ctx context.Context, up *userPool, u User) (*olap.Instance, error) {
	// style: doc comment + numbered overview + step comments
	// because: the cap, the lock order and the failure path each need their reason
	//
	// 1. under duckMu, answer the instance when it exists
	// 2. take a unit of the cap under mu, or answer InstanceLimitError [ADR-0017 D10]
	// 3. open outside mu, because the attach dials the cluster and mu guards every user [Rationale R4]
	// 4. on failure, give the unit back [ADR-0017 D10]
}

// SQL runs q on DuckDB as u and hands its batches to sink, on a
// connection of u's instance that closes when sink returns. An error
// before sink runs is the cap, the attach or DuckDB's. The error of sink
// is returned unchanged. A sql call is never retried and does not feed
// the breaker. The budget does not count it, because qdb-duck pools its
// own qdbd sessions. [ADR-0017 D7, D9; owner 2026-10-15, Decision 10]
func (c *Cluster) SQL(ctx context.Context, u User, q string, sink func(Batches) error) error {
	// style: doc comment + numbered overview + step comments
	// because: the open connection must be counted from before the query until after the sink, on every path
	//
	// 1. find or create u's pool, as a native call does [cluster.go:191-203, poolFor]
	// 2. answer u's instance, opening it on the first call
	// 3. count the connection under mu, so eviction sees the pool in use [owner 2026-10-15, Decision 7]
	// 4. run the query and the sink on the instance
	// 5. uncount the connection, whatever happened
}
```

`poolFor` returns the `*userPool` to `SQL`, so the native path and the
sql path find the same entry. If returning it changes the signature
`Call` uses, `Call` takes `.pool`. `takeIdle`'s doc comment and its
emptiness test gain the two rules: a pool with a connection open on its
instance is in use, and an idle instance on its own does not keep a
pool, so the instance closes with the pool after `pool.idle_timeout`
(owner 2026-10-15, Decision 7). `evictOnce` and `Close` close a taken
pool's instance after its sessions and give its unit back. `Stats`
gains `Instances int`, which follows the `FooStats` rule
(`internal/AGENTS.md`, Code).

### `internal/httpapi/query.go`

`writeClusterError` gains one case before `IsClusterUnavailable`:

```go
	// at the case: an *qdb.InstanceLimitError is 503 with its RetryAfter, like the open breaker, because both say the server is full for now [ADR-0017 D10]
```

Its doc comment says that a full instance cap is 503 with
`Retry-After`, next to the open breaker.

### `internal/httpapi/sql.go` (new)

```go
// handleSQL runs the body as DuckDB SQL as the bearer's user and streams
// the result batch by batch in the negotiated format. [ADR-0017 D1]
func handleSQL(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + numbered overview + step comments
	// because: the reader's shape, two failure kinds that answer differently
	//
	// The query runs inside the cluster call's sink, so the status is decided before the sink for every error DuckDB finds before its first batch. [internal/httpapi/read.go:52-64]
	//
	// 1. a body that is not query text is 415, and the body is read capped [query.go:89-99]
	// 2. run the query, with the encoder streaming inside the sink [read.go:70-77]
	// 3. a failure of the stream: with zero bytes out it is 500, which is also where a type outside the five lands, and after the first byte the stream is cut and only the log hears of it [owner 2026-10-15, Decisions 3 and 10; F15]
	// 4. a failure of the call, before any byte: writeClusterError at 400, where the cap and the attach are 503 [ADR-0017 D10; Rationale R5]
}

// registerSQLRoutes serves the DuckDB endpoint behind the bearer
// middleware. The mux pattern fixes method and path.
func registerSQLRoutes(mux *http.ServeMux)
```

`NewHandler` calls `registerSQLRoutes` after `registerQueryRoutes`.

### Tests in `internal/httpapi`

`TestErrorRows` gains, under `// the sql`:

```go
		// "sql: invalid sql": a parse error before any byte is 400 [ADR-0017 D10; F10]
		// "sql: json body": 415, the query's content type rule [ADR-0017 D1]
		// "sql: no token": 401 with the bearer challenge [ADR-0017 D1]
		// "sql: unsupported type": SELECT 1 is int32, which no encoder writes, so 500 [owner 2026-10-15, Decision 3; F9]
```

The cap row needs a cluster whose `max_instances` is zero, so it is one
request in its own short test, `TestSQLInstanceLimit` (503 with
`Retry-After`), as the login verdicts keep their own tests
(`internal/httpapi/AGENTS.md`, Tests).

`roundtrip_test.go`:

```go
// directSQL runs q on DuckDB over the cluster and encodes the stream
// with e. The endpoint must answer these exact bytes. [roundtrip_test.go:95-120, the reader's twin]
func (s server) directSQL(t *rapid.T, e encoding.Encoder, q string) []byte

// checkSQL reads tbl through DuckDB over the cluster and compares what
// comes back with the rows generated, so the DuckDB path and the native
// path answer the same rows. [ADR-0017 D11 as edited]
func (s server) checkSQL(t *rapid.T, tbl table.Table)

// checkSQLFormats posts tbl's select to /api/v2/sql in every format and
// compares each body with directSQL.
func (s server) checkSQLFormats(t *rapid.T, tbl table.Table)
```

`table.Table` gains `SelectSQL()`, which is `Select` against
`qdb.<name>` with quoted names and `ORDER BY "$timestamp"` (F9, F13).
In `TestRoundtrip` step 6, the sql checks sit after the table reader's,
under the same `async` skip, and the step comment gains why: qdb-duck
scans through the bulk reader (F12). The file's header comment names the
sql read among the reads. The round trip runs on the insecure cluster
(owner 2026-10-15).

### `tests/e2e/round-trip.sh`

```bash
# sql_back <out_dir> <url> <token> <table> <format> <media_type> <coding> <csv>:
# POST /api/v2/sql with SELECT '<table>' AS "$table", * FROM qdb.<table>
# ORDER BY "$timestamp", checked like the query through the tool. The
# scan has no $table column, and the decoder routes rows by it, so the
# select names the table as a constant. [F9; compare_csv, round-trip.sh:166-175]
#
# style: the header comment of a driver function, as query_back
```

`run_cluster` calls `sql_back` next to `query_back`, and its
`[OK]` line names the sql read. It runs on both clusters (owner
2026-10-15). If the e2e edge rows fail through DuckDB, the executor
reports it in the build-stage message rather than changing the rows.

## 4. Rationale

| #   | decision                                                                                                                                         | why                                                                                                                   | rejected, and why                                                                                                                                | gained                                                                                          | given up                                                                                   | settled by                                             |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------ |
| R1  | The encoders keep their five wire types, and another type answers 500 naming the column.                                                         | Keeps this unit in band, and a caller can CAST in SQL.                                                                | Widening the encoders now: about four more commits and new wire type names. olap casting to the five: decimal precision and bool meaning lost.   | A small unit on unchanged encoders.                                                             | `SELECT 1` and `sum()` fail until the widening unit.                                       | owner, 2026-10-15                                      |
| R2  | The credentials reach qdb-duck as a temporary DuckDB secret of quoted literals, and the attach names the secret.                                 | The ATTACH string drops `=` padding (F2), and config options at open are refused (F3).                                | The ATTACH string: the secure attach fails. `SET quasar_*`: verified to work, but not chosen. Fixing qdb-duck first: blocks on an outside merge. | No user text in SQL outside a quoted literal, and a key that is redacted in `duckdb_secrets()`. | One more statement per open.                                                               | owner, 2026-10-15                                      |
| R3  | The `duckdb` block defaults are 1 GiB, 4 threads, no spilling, 16 instances.                                                                     | DuckDB's defaults suit a workstation. Four threads bound one user's scan at four qdbd sessions.                       | 512 MiB, 2 threads, 8: too tight for joins. Spilling to the temp directory: writes to disk by default.                                           | A worst case of 16 GiB and 64 threads.                                                          | A query over 1 GiB fails until the operator raises the limit.                              | owner, 2026-10-15                                      |
| R4  | The instance lives on `userPool`, its open is serialised per user by `duckMu`, and an open connection counts as use of the pool.                 | One instance per user (ADR-0017 D5). The attach dials, so it cannot run under the cluster-wide `mu`.                  | A separate idle timer for instances: one more knob. Opening under `mu`: one user's attach stalls every user.                                     | Two first calls open one instance, and eviction never closes an instance in use.                | An idle instance closes with its pool after `pool.idle_timeout`.                           | owner, 2026-10-15 (Decision 7); `duckMu` is a proposal |
| R5  | A failed attach answers 503 with no `Retry-After` and is not cached. A parse, bind or execution error is 400.                                    | qdb-duck's message does not tell a refusal from an unreachable cluster (F10), and the credential was proven at login. | 400 for a failed attach: blames the caller for the cluster.                                                                                      | The next call retries the attach.                                                               | A bad key after a key rotation reads as 503.                                               | owner, 2026-10-15 (Decision 9); ADR-0017 D10           |
| R6  | ATTACH gets host, port and credentials only.                                                                                                     | qdb-duck's defaults apply.                                                                                            | Forwarding `cluster.timeout`, `compression` and `encryption`: qdb-duck may spell the vocabulary differently.                                     | No mapping to keep.                                                                             | The DuckDB path ignores those three knobs.                                                 | owner, 2026-10-15 (Decision 11)                        |
| R7  | `internal/qdb` imports `internal/olap`, and olap knows nothing of qdb.                                                                           | The instance is a field of the user pool state (ADR-0017 D5).                                                         | olap holding the pool: inverts the owner's layout.                                                                                               | One direction.                                                                                  | The olap test is an external package, and the schema-only batch moves to `internal/model`. | ADR-0017 D5                                            |
| R8  | The equivalence check is the round trip's sql step. Each table read through DuckDB is checked against the generated rows, as the native read is. | A new route lands as a round trip step (`internal/httpapi/AGENTS.md`, Tests).                                         | A standalone property test (ADR-0017 D11 as written): a second oracle for the same rows.                                                         | One place for the good path.                                                                    | No separate property.                                                                      | owner, 2026-10-15                                      |
| R9  | The instance cap's `Retry-After` is the eviction tick, 10s.                                                                                      | It is the soonest an idle instance can close.                                                                         | A configurable value: one more knob.                                                                                                             | No new key.                                                                                     | A client may come back before a slot frees.                                                | owner, 2026-10-15 (Decision 8)                         |
| R10 | Secure coverage in this unit: the olap test on both clusters and the e2e sql step on both. The in-process round trip stays insecure.             | Running the round trip on the secure cluster is the deferred unit.                                                    | The whole round trip on both clusters now: two more commits and double the runtime.                                                              | The secure attach is tested in Go and over HTTP.                                                | The in-process sql step is insecure only.                                                  | owner, 2026-10-15                                      |
| R11 | Mid-stream errors follow the table reader: 500 with zero bytes out, otherwise cut with a warning. No retry.                                      | DuckDB can fail after its first batch.                                                                                | Buffering the result to decide the status: defeats streaming.                                                                                    | One rule for both streamed routes.                                                              | A late failure is a cut stream.                                                            | owner, 2026-10-15 (Decision 10)                        |

## 5. Knowledge

| commit | where the whys land                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2      | `DuckDB` and `Default` comments (R3). `examples/qdb_rest.yaml` comments (R3).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 3      | `model.EmptyBatch` doc comment.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| 4      | `olap.Open`, `secretOf` and `dsn` comments (R2, F2, F7, R5, R6). `ErrAttach` (F10). New `internal/olap/AGENTS.md`: the package's rules (one instance per user, owned by `internal/qdb`; credentials through a temporary secret and why, R2; types outside the five fail at the encoder, R1; qdb-duck's pool outside the budget, ADR-0017 D7; the test is an external package and why, R7). A pointer to it goes into `internal/AGENTS.md`, Code, next to the encoding and httpapi pointers, because the root sub-folder table lists `internal/` as one folder. |
| 5      | `Instance.Query` comments.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 6      | `CreateAs` doc comments (F14). `internal/AGENTS.md`, Tests: the fixture's `As` forms for a cluster that refuses the anonymous user.                                                                                                                                                                                                                                                                                                                                                                                                                            |
| 7      | `TestQueryReadsGeneratedTable` comments (R10).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| 8      | `userPool`, `instanceOf`, `takeIdle`, `credentialsOf`, `IsClusterUnavailable`, `InstanceLimitError` comments (R4, R5, R9). `internal/AGENTS.md`, Code: one paragraph on the sql path (the instance on the user pool, the cap, eviction with the pool, never retried, breaker and budget untouched).                                                                                                                                                                                                                                                            |
| 9      | `Cluster.SQL` comments (R11).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| 10     | `handleSQL` comments. `writeClusterError` doc comment. `internal/httpapi/AGENTS.md`, Handlers: a bullet for `POST /api/v2/sql` (request as the query, streaming as the reader, 400, 503 for the cap with `Retry-After` and for the attach without it, 500 for a type outside the five, R1), and the scope line names ADR-0017.                                                                                                                                                                                                                                 |
| 11     | `TestSQLInstanceLimit` doc comment.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| 12     | Round trip comments (R8, F12). `internal/httpapi/AGENTS.md`, Tests: the round trip reads through `/api/v2/sql` too. ADR-0017 Decision 11 and its Consequences edited in place (R8).                                                                                                                                                                                                                                                                                                                                                                            |
| 13     | `sql_back` comment. `docs/e2e.md`: the round trip section names the sql step and drops "and `/api/v2/sql` add steps" from the future list. A decision-log row for the select's `$table` constant (F9).                                                                                                                                                                                                                                                                                                                                                         |
| 14     | `docs/log.md`: Current state rewritten (M4 done, M5 next with its entry met, Next holds the three later units of section 1 in order, Last updated 2026-10-15), and one entry `M4 closed ...; m4-sql-plan.md deleted; facts moved to internal/olap/AGENTS.md, internal/AGENTS.md, internal/httpapi/AGENTS.md, docs/e2e.md and ADR-0017`. The dated entries of 2026-10-13, 2026-10-14 and 2026-10-15 are rewritten as if FreeBSD had never been excluded (owner, 2026-10-15).                                                                                    |

R6 lands in `secretOf`'s comment (commit 4). R7 lands in
`internal/olap/AGENTS.md` (commit 4). R10 and R11 land in commits 7 and 9. Every row has a home.

## 6. How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write every commit's comments and document rows from sections 3 and
   5, in the same commit as the code. Every listed claim appears once,
   at the place named, in the prose of the root `AGENTS.md`, and the
   evidence brackets stay in the plan.
3. A claim the code contradicts is not written, and it is reported in
   the build-stage message. A why that arises while building and is not
   in this plan is written where it is decided, with its evidence, or
   asked of the owner through the question tool before the commit.
4. Each commit builds, passes `make lint` and passes
   `go test -p 1 ./...` against the local qdbd pair. Run
   `go mod tidy && go mod vendor` in commit 4 and commit the vendor
   tree (F11).
5. After the last code commit, run `/doc-discipline all` over the
   touched paths, with one small commit per finding.
6. Before the build-stage message, run `/doc-discipline check <paths>
docs/m4-sql-plan.md` and quote its report.
7. Run `npx prettier --write` on every Markdown file touched.

## 7. Commits

1. docs(plan): m4-sql-plan.md, post /api/v2/sql over one duckdb instance per user
2. feat(config): the duckdb block caps memory, threads, spill and live instances
3. refactor(model): the schema-only batch moves to internal/model for qdb and olap
4. feat(olap): an instance opens under the duckdb block and attaches the cluster through a temporary secret
5. feat(olap): a query runs on its own connection and answers its result as lent batches
6. test(qdbtest): the table fixture creates and removes as a given user
7. test(olap): a generated table reads back through qdb.<name> on both clusters
8. feat(qdb): the user pool holds its duckdb instance under the instance cap and closes it with the pool
9. feat(qdb): Cluster.SQL runs duckdb sql as the user and hands its batches to a sink
10. feat(httpapi): POST /api/v2/sql streams the duckdb result in the negotiated format
11. test(httpapi): the sql error rows and the instance cap
12. test(httpapi): the round trip reads every table through /api/v2/sql
13. test(e2e): the round trip reads the generated rows back through /api/v2/sql
14. docs(log): M4 closed, later units listed, FreeBSD history removed; m4-sql-plan.md deleted

After 14: `/doc-discipline all` over the touched paths, with one small
commit per finding, then `/doc-discipline check` against this plan.

15. Verify: push `sc-19567/rr-m4-sql`, build its head in Buildkite and
    wait for the result. Green: report the build number. Red: fix with
    further small commits on this branch, push, and build again.

## 8. Open questions and recommendations

1. bureau14/qdb-duck#17 has merged, and master build 90 promoted the
   FreeBSD packages (F17). Removing the pin from `.buildkite/pipeline.py`
   (`_DUCK_GIT_REF` and the `by_project` entry) needs no ref, because
   the plugin falls back to master. Recommendation: a one-commit unit
   right after this one, so this unit's build does not change its
   inputs midway.
2. The cluster public key file is read each time an instance opens.
   Recommendation: keep it, because no instance outlives
   `pool.idle_timeout`, and it needs no cache.
3. `max_instances` 0 answers 503 to every sql call, and nothing refuses
   it at startup. Recommendation: keep, per `internal/AGENTS.md`, Code
   (no eager check). `TestSQLInstanceLimit` uses exactly that.
