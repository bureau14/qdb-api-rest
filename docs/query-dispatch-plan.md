# Plan: query dispatch -- one query route, and one function that runs a query string on the engine its `#sql` prefix names

Status: draft

This plan covers the first slice of M5 (`docs/log.md`, Current state). It is
written for an executor who has none of the seed conversation of
2026-10-19. Everything the build needs is here.

## 1. Outcome

When the unit lands:

- `Cluster.SQL` in `internal/qdb` is `Cluster.Duck`, after qdb-duck,
  and the comments and documents that call its call "a sql call" say "a
  DuckDB call". `Cluster.Query` stays the native path.
- A new package, `internal/query`, holds one exported function, `Run`. It
  takes a query string, recognizes the `#sql` prefix of ADR-0018, Decision
  3, and runs the query on the caller's DuckDB instance (`Cluster.Duck`)
  with the prefix stripped, or on the native engine (`Cluster.Query`)
  unchanged. Either way it hands the result to a sink as `qdb.Batches`.
- `POST /api/v2/query` is the one query route of the HTTP plane. It runs
  every body through `query.Run` and encodes through `EncodeStream`. A
  `#sql` body runs on DuckDB. The JSON body is the top-level array of
  results, one per batch, for both engines. CSV, NDJSON and Arrow bytes
  of a native query are unchanged.
- `POST /api/v2/sql` is removed, together with `handleSQL` and
  `internal/httpapi/sql.go`. A path the mux does not know answers the
  mux's plain-text 404.
- Every test that went through `/api/v2/sql` posts `#sql <text>` to
  `/api/v2/query` instead, the e2e round trip included, and the test
  names that said "sql" for DuckDB say "DuckDB".
- ADR-0010, ADR-0017, ADR-0018, the brief, `internal/AGENTS.md`,
  `internal/httpapi/AGENTS.md`, `internal/encoding/AGENTS.md`,
  `internal/olap/AGENTS.md`, `internal/encoding/json.go` and
  `docs/e2e.md` say so.

Left for later units, each named in `docs/log.md` once this lands:

- the Flight listener, Handshake, bearer and anonymous auth, the status
  codes and the statement query through `DoGet` (it calls `query.Run`);
- `GetSqlInfo` and the catalog commands on the table list core;
- the ADBC smoke test and the JDBC smoke test;
- the `tls_port` sample fix in `qdb-documentation`.

## 2. Verified facts

1. The prefix rule: first non-whitespace bytes `#sql`, case-insensitive,
   followed by one whitespace character; the prefix and that character
   are removed; anything else, `#sqlx` included, runs native "as is"
   [`docs/adr/0018-flight-sql.md`, Decision 3].
2. The dispatcher lives above `internal/qdb` and `internal/olap`,
   answers `Batches` either way, and `httpapi` and `flightsql` both call
   it. The native query is a one-step iterator so every result streams
   through `EncodeStream` [`docs/adr/0018-flight-sql.md`, Decision 5].
3. ADR-0018, Decision 4, keeps `POST /api/v2/sql` as the direct DuckDB
   route, and ADR-0017, Decision 1, names it the endpoint, with Decision
   11 testing through it [`docs/adr/0018-flight-sql.md:43`,
   `docs/adr/0017-embedded-duckdb.md:26-31`, `:81-85`]. The owner
   reverses both clauses in this unit (Rationale, "one query route").
4. `JSON.EncodeStream` writes a top-level array of results, `[` then one
   `{"columns":...}` per batch, comma separated, then `]`; a sequence of
   no batch is `[]` [`internal/encoding/json.go:334-345`]. `JSON.Encode`
   writes one bare object [`internal/encoding/json.go:321-331`].
5. `Arrow.EncodeStream` opens its IPC writer from `rec.Schema()` of the
   first step, so a nil batch as a step panics there
   [`internal/encoding/arrow.go:79-85`].
6. `Encode` keeps production callers after this unit: the e2e tool
   writes ingest bodies with it [`tests/e2e/tools/e2etool/main.go:196`,
   `:319`], and the fixture's bodies use it
   [`internal/httpapi/roundtrip_test.go:55`].
7. The JSON decoder reads both a bare result and an array of results
   [`internal/encoding/json.go:757-790`], so the e2e tool's `tocsv` reads
   the query's new array unchanged [`tests/e2e/round-trip.sh:231-245`,
   `tests/e2e/tools/e2etool/main.go:262-303`].
8. `qdb.Batches` is `iter.Seq2[arrow.RecordBatch, error]`: each batch
   lent for its step, the receiver releases none, an error step is the
   last, there is at least one step [`internal/qdb/read.go:94-98`].
9. `Cluster.Query` returns an owned batch, released once by the
   receiver, nil for a statement without a result set, and the session
   is back in its pool before the caller reads a row
   [`internal/qdb/cluster.go:387-405`]. `WithReadRetry` is safe on it
   because nothing has been sent [`internal/httpapi/query.go:131-133`].
10. `Cluster.SQL` (`Cluster.Duck` from commit 4 on) returns the sink's error unchanged, is never retried,
    does not feed the breaker, and answers the instance cap, a refused
    credential, a failed attach or DuckDB's error before the sink runs
    [`internal/qdb/cluster.go:575-610`, `internal/olap/instance.go:183-215`].
11. `model.EmptyBatch(schema)` builds a batch of schema and no rows,
    owned by the caller [`internal/model/model.go:28-31`].
12. `handleSQL` already has the shape the query route takes: body read,
    run with the encoder inside the sink, stream error kept apart from
    the call error, 500 with zero bytes out, a warning after the first
    byte [`internal/httpapi/sql.go:11-60`].
13. `writeClusterError` already maps everything a `#sql` body can fail
    with: refused credential 401 `invalid_token`, breaker and instance
    cap 503 with `Retry-After`, unreachable cluster and failed attach 503,
    the rest at the status passed [`internal/httpapi/query.go:66-105`].
14. ADR-0010, Decision 1, says the body reaches the C API with "no
    prefix routing", and Decision 5 says "The batch is materialized
    before anything is written" [`docs/adr/0010-v2-query-request-negotiation-errors.md:20-46`].
    Both become wrong in this unit.
15. The lint stack is golangci-lint `standard` (which includes `unused`)
    plus forbidigo and sloglint [`.golangci.yml:5-8`]. An unexported
    function used only from a later commit would fail lint in the commit
    that adds it.
16. Every mention of the sql route outside vendored code and log
    entries (grep of 2026-10-19): `tests/e2e/round-trip.sh:249-262`,
    `internal/olap/AGENTS.md:3`, `internal/httpapi/errors_test.go:81-84`,
    `:136`, `:166-180`, `internal/httpapi/sql_test.go:1-3`, `:16-48`,
    `internal/httpapi/roundtrip_test.go:178-260`,
    `internal/httpapi/httpapi.go:57`, `internal/httpapi/AGENTS.md:82-93`,
    `:160`, `:176-179`, `internal/httpapi/sql.go`, `docs/brief.md:406`,
    `:458`, `:546`, `:931`, `docs/e2e.md:109`,
    `docs/adr/0017-embedded-duckdb.md:26`, `:82`,
    `docs/adr/0018-flight-sql.md:43`, `:102`. Log entries are
    append-only and keep their wording (`docs/AGENTS.md`, The log).
17. `TestSQLScalars` reads the JSON array `/api/v2/sql` answers
    [`internal/httpapi/sql_test.go:48-`], so on `/api/v2/query` behind
    `#sql` it reads the same body.
18. Unverified: that qdbd answers `#sqlx SELECT 1` with a parse error
    (400) rather than something else. The executor checks it in commit 10
    before writing the row, and reports a different answer in the
    build-stage message.
19. Every mention of `Cluster.SQL` and of "sql call" outside vendored
    code and log entries (grep of 2026-10-19): `internal/qdb/cluster.go:25`,
    `:35-36`, `:448`, `:532`, `:575-581`, `:633`,
    `internal/httpapi/sql.go:42`, `internal/httpapi/roundtrip_test.go:183`,
    `:197`, `internal/httpapi/errors_test.go:162`,
    `internal/httpapi/AGENTS.md:85`, `:93`, `:167`,
    `internal/AGENTS.md:95-102`, `internal/olap/AGENTS.md:12`,
    `internal/config/config.go:612`, `docs/brief.md:489`.
    `docs/log.md:81` is a log entry and keeps its wording.

## 3. Design

### `internal/qdb/cluster.go`, the rename

```go
// Duck runs q, DuckDB SQL, as u on u's own DuckDB instance and hands its batches to sink. [cluster.go:575-581, the doc comment of SQL with the name changed]
// The rest of the doc comment and the body of SQL stay as they are, with "sql call" read "DuckDB call". [Rationale: Cluster.Duck]
func (c *Cluster) Duck(ctx context.Context, u User, q string, sink func(Batches) error) error {
	// style: doc comment + numbered overview + step comments
	// because: the body of SQL moves unchanged, and it already has this shape [cluster.go:582-610]
}
```

The rename touches nothing but the name and the wording of fact 19: the
callers (`sql.go:42`, `roundtrip_test.go:183`, `:197`), the field
comments of `userPool.duck` and `duckIn`, `InstanceLimitError`'s comment,
the eviction comments, `config.go:612`, and the document rows of fact 19,
where "a sql call" becomes "a DuckDB call" and `Cluster.SQL` becomes
`Cluster.Duck`. The field names `duck` and `duckIn` already say Duck.

### `internal/query/query.go` (new), in file order

```go
// Package query runs one query string on the engine its text names. [ADR-0018 D5]
// A string whose first non-whitespace bytes are #sql runs on DuckDB, any other on the native engine. [ADR-0018 D3]
// The HTTP query route calls it, and the Flight server will. [ADR-0018 D5]
package query
```

```go
// isSpace reports whether b is ASCII whitespace: space, tab, LF, CR, VT or FF. [Rationale: ascii whitespace]
func isSpace(b byte) bool {
	// style: doc comment alone
	// because: one comparison chain, and the set is the contract the doc comment states
}
```

```go
// split answers the text to run and whether DuckDB runs it. [ADR-0018 D3]
// A #sql prefix after ASCII whitespace, in any case, followed by one ASCII whitespace byte, is DuckDB's, and text is what follows that byte. [ADR-0018 D3; Rationale: ascii whitespace]
// Any other q is native, and text is q unchanged, leading whitespace included. [ADR-0018 D3 "as is"]
func split(q string) (text string, duck bool) {
	// style: doc comment + numbered overview + step comments
	// because: the rule has three conditions in an order, and each failing one sends q native unchanged
	//
	// The test reads bytes and decodes no UTF-8, so a query in any encoding splits the same way. [Rationale: ascii whitespace]
	//
	// 1. skip the leading whitespace [ADR-0018 D3 "first non-whitespace bytes"]
	// 2. match #sql in any case [ADR-0018 D3 "case-insensitively"]
	//    strings.EqualFold on the four bytes
	// 3. require one whitespace byte after it, so #sqlx and a bare #sql stay native [ADR-0018 D3]
	// 4. strip the prefix and that one byte, and keep every byte after it, a second whitespace byte included [ADR-0018 D3 "that character removed"]
}
```

```go
// Run runs q as u on the engine its text names and hands the result to sink. [ADR-0018 D5]
// The cluster is the one ctx carries. [internal/AGENTS.md, Logging: handlers read the cluster from ctx]
// A #sql query runs on u's DuckDB instance with the prefix stripped, and a native query runs unchanged. [ADR-0018 D3]
// Every error that is not sink's comes before sink runs, so a caller decides its answer before it writes. [fact 9, fact 10]
// The error of sink is returned unchanged. [fact 10]
// A native statement without a result set is one step of a batch with no fields and no rows. [fact 5; Rationale: empty batch]
func Run(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error {
	// style: doc comment + numbered overview + step comments
	// because: two engines with different retry and ownership rules meet in one result shape
	//
	// Both engines end in the lent Batches, so a caller has one way to read a result. [ADR-0018 D5]
	//
	// 1. split q into its text and its engine
	//    text, duck := split(q)
	// 2. a DuckDB query is Cluster.Duck, which is never retried and streams its own lent batches [fact 10]
	// 3. a native query is Cluster.Query with one read retry, safe because the query runs before sink and nothing has been sent [fact 9]
	// 4. the native batch is owned here and released after sink returns, so a sink that never pulls a step leaks nothing [fact 9; Rationale: release in Run]
	//    a nil batch becomes model.EmptyBatch of an empty schema, because Arrow's EncodeStream reads the first step's schema [fact 5, fact 11]
	// 5. hand sink the batch as one step [ADR-0018 D5]
	//    sink(one(rec))
}
```

```go
// one is rec as Batches of one step. The caller owns rec and releases it after the step. [fact 8; Rationale: release in Run]
func one(rec arrow.RecordBatch) qdb.Batches {
	// style: doc comment alone
	// because: one yield, and the ownership rule is the contract
}
```

### `internal/query/query_test.go` (new)

```go
// TestSplit: for any leading ASCII whitespace, any case of #sql, any one ASCII whitespace byte and any rest, the split answers DuckDB and the rest. [ADR-0018 D14 "a dispatch property generates prefixes and whitespace"]
// For any query that misses one condition, the split answers native and the query unchanged. [ADR-0018 D3]
func TestSplit(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: the property draws one of several shapes, and each shape has its own expected answer
	//
	// It dials nothing, because split is pure. [internal/AGENTS.md, Tests]
	//
	// 1. draw the leading whitespace from the six ASCII bytes, zero to three of them [Rationale: ascii whitespace]
	// 2. draw the case of each of the four prefix letters [ADR-0018 D3]
	// 3. draw a shape and build q from it:
	//    - DuckDB: prefix, one whitespace byte, any rest (rest may itself start with whitespace) -> duck, text == rest
	//    - glued: prefix then a non-whitespace byte, so #sqlx -> native, text == q
	//    - bare: prefix and nothing after it -> native, text == q
	//    - unicode space: prefix, then U+00A0 or U+0085 or U+2003, then rest; or one of those before the prefix -> native, text == q [Rationale: ascii whitespace]
	//    - other: any string whose first non-whitespace byte is not '#' -> native, text == q
	// 4. compare split(q) with the shape's answer
}
```

### `internal/httpapi/query.go`

`maxBodyBytes`, `readBody`, `isQueryText`, `countingWriter`, `retryAfter`
and `writeClusterError` keep their code and comments. `handleQuery`
takes the body of `handleSQL` [fact 12], with `query.Run` as the call:

```go
// handleQuery runs the body through query.Run as the bearer's user and streams the result in the negotiated format. [Outcome]
// A #sql body runs on DuckDB, and any other body on the native engine. [ADR-0018 D3, D4 as edited]
// The status is decided before the first byte. [ADR-0010 D5]
func handleQuery(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + numbered overview + step comments
	// because: handleSQL's body moves here, and it already has this shape [internal/httpapi/sql.go:13-24]
	//
	// The query runs inside the sink, and every error of the run arrives before the sink runs, so the status is decided before the first byte. [fact 9, fact 10]
	// Two kinds of failure come out, and they answer differently. [sql.go:17-23]
	//
	// 1. read the body as query text: a non-text body is 415, and the body is capped and read whole [sql.go:26-33]
	// 2. run the query through query.Run, with the encoder streaming inside the sink [sql.go:36-42]
	// 3. a failure of the stream: with zero bytes out it is 500, where a column type no encoder writes lands; after the first byte the stream is cut and only the log hears of it [sql.go:44-52]
	// 4. a failure of the run, before any byte: the engine's error is the caller's 400, and the instance cap, a refused credential, an open breaker and an unreachable cluster answer as writeClusterError maps them [query.go:66-105]
}
```

`registerQueryRoutes` keeps its code and comment.

### `internal/httpapi/sql.go` and `httpapi.go`

`sql.go` is deleted in commit 12: `handleSQL` and `registerSQLRoutes` have
no route left. `NewHandler` loses `registerSQLRoutes(mux)`. Its comment
needs no change.

### Tests in `internal/httpapi`

```go
// query_test.go
// direct runs q through query.Run as the server's user and encodes the stream with e. The endpoint must answer these exact bytes. [fact 4; internal/httpapi/AGENTS.md, Tests: the oracle is the encoder run directly]
func (s server) direct(t *rapid.T, e encoding.Encoder, q string) []byte {
	// style: doc comment alone
	// because: one call and one encode
	//
	// at the call: the ctx is s.ctx, because query.Run reads the cluster from ctx [Run's contract]
}
```

`TestQueryCompressed` keeps its steps. Its step 2 comment changes from
"written by the JSON encoder" to the stream encoder's bytes, which is the
array of one result [fact 4].

`roundtrip_test.go`: `directSQL`, `checkSQL` and `checkSQLFormats` are
renamed `directDuckDB`, `checkDuckDB` and `checkDuckDBFormats` (Rationale,
"DuckDB in the test names"). `directDuckDB` and `checkDuckDB` keep their
bodies, because they call `Cluster.Duck` directly as the oracle and the
check.

```go
// checkDuckDBFormats posts tbl's DuckDB select behind the #sql prefix to /api/v2/query in every format and compares each body with directDuckDB over the select alone. [Outcome; fact 1]
func (s server) checkDuckDBFormats(t *rapid.T, tbl table.Table) {
	// style: doc comment alone + inline note at the comparison
	// because: the existing loop with one body changed
	//
	// at the comparison: the oracle runs the stripped text on DuckDB directly, so equal bytes prove the prefix reached DuckDB and the rest of the text reached it unchanged [fact 1]
	// the existing null-slot note on the Arrow comparison stays [roundtrip_test.go:248-251]
}
```

The round trip's file comment and step 6 comment say "read through
DuckDB behind `#sql` on the query route" where they say "DuckDB SQL"
[roundtrip_test.go:1-13, :391-398].

`sql_test.go` becomes `duckdb_test.go`, and `TestSQLScalars` becomes
`TestDuckDBScalars`. It posts `"#sql SELECT " + ...` to `/api/v2/query`
[fact 17]. The file comment says "the DuckDB scalar test" and the test
comment says "behind `#sql` to `/api/v2/query`".

`errors_test.go`:

```go
// TestDuckDBInstanceLimit (was TestSQLInstanceLimit): both posts become "#sql SELECT 'a' AS s" to /api/v2/query; its comment says "a DuckDB call" for "sql call". [errors_test.go:162-180]
// the query block gains, and the sql block is removed:
// "query: invalid #sql":           POST /api/v2/query, "#sql NOT A QUERY", bearer, 400 [fact 10, fact 13]
// "query: #sql unsupported type":  POST /api/v2/query, "#sql SELECT [1]", bearer, 500 [errors_test.go:80 "a list is a type no text encoder writes"]
// "query: #sqlx is native":        POST /api/v2/query, "#sqlx SELECT 1", bearer, 400 [ADR-0018 D3; fact 18]
// the secure block: "sql: anonymous on the secure cluster" becomes
// "query: #sql anonymous on the secure cluster": POST /api/v2/query, "#sql SELECT 1", anonymous bearer, 401 invalid_token [internal/AGENTS.md, the refused credential]
// "sql: json body" and "sql: no token" are dropped, because "query: json body" and "query: no token" are the same request on the one route
```

### `tests/e2e/round-trip.sh`

```sh
# duckdb_back <out_dir> <url> <token> <table> <format> <media_type> <coding> <csv>: (was sql_back)
# POST /api/v2/query with #sql SELECT '<table>' AS "$table", * FROM qdb.<table> ORDER BY "$timestamp" as the text/plain body, checked through check_response like the query. [round-trip.sh:249-262]
# qdb-duck's scan answers no $table column, and the tool routes rows by it, so the select names the table as a constant. [round-trip.sh:252-254, unchanged]
```

The response name suffix `sql` stays, because it names the step's files
under `actual/`, which no document lists. The executor renames it to
`duckdb` only if nothing else reads those names.

### Document rows

`docs/adr/0018-flight-sql.md`, edited in place (owner, 2026-10-19):

```
Decision 4, rewritten:
- POST /api/v2/query is the one query route of the HTTP plane, and a #sql body runs on DuckDB there as on the Flight port. There is no POST /api/v2/sql.
Decision 5, one sentence added:
- The dispatcher is query.Run in internal/query.
Decision 14:
- "for the #sql engine against /api/v2/sql" becomes "for the #sql engine against #sql on /api/v2/query".
Consequences:
- "A #sql body on POST /api/v2/query runs on DuckDB from M5 on" stays; the sentence after it names the removal of /api/v2/sql.
Alternatives rejected, one row:
- POST /api/v2/sql kept as a direct DuckDB route | the prefix already chooses the engine on both planes, so a second route is a second way to say the same thing (owner, 2026-10-19)
```

`docs/adr/0017-embedded-duckdb.md`, edited in place:

```
Decision 1:
- "The endpoint is POST /api/v2/sql" becomes "DuckDB is reached through POST /api/v2/query behind the #sql prefix (ADR-0018, Decision 4)". The rest of the decision (text body, Accept, bearer, shared encoders, qdb attached, qdb_query, EncodeStream) stays.
Decision 11:
- "back through /api/v2/sql" becomes "back through #sql on /api/v2/query".
```

`docs/adr/0010-v2-query-request-negotiation-errors.md`, edited in place:

```
Decision 1:
- The body bytes reach the engine unchanged, except that a #sql prefix (ADR-0018, Decision 3) is stripped and sends the rest to DuckDB.
Decision 5:
- The JSON body is a top-level array of results, one per batch, for either engine (owner, 2026-10-19).
- The status is decided before the first byte: a native batch is materialized before anything is written, and DuckDB answers a #sql query's errors before its first batch.
Decision 7, the status table, one row:
- the DuckDB instance cap is full (a #sql query) | 503, Retry-After in whole seconds, rounded up
Alternatives rejected, two rows:
- A JSON object for a native query and the array for #sql | one route would answer two JSON shapes by its query text, and the dispatcher would need two result paths (owner, 2026-10-19)
- One JSON object merging every batch | a columnar object finishes column one before column two starts, so it cannot stream
```

`docs/brief.md`:

```
Project structure, after internal/olap/:
- internal/query/       one query string to the engine its #sql prefix names (ADR-0018)
Endpoint sketch:
- the POST /api/v2/query line: native qdb query, or DuckDB behind a #sql prefix (ADR-0018); streamed, content-negotiated; JSON is the array of results, one per batch (ADR-0010)
- the POST /api/v2/sql line is removed
Arrow Flight SQL, "One query string, two engines" (:406):
- "POST /api/v2/query honors the same prefix, and POST /api/v2/sql stays the direct DuckDB route" becomes "POST /api/v2/query honors the same prefix and is the one query route of the HTTP plane (owner, 2026-10-19)"
Embedded DuckDB (:458):
- "behind a dedicated endpoint, POST /api/v2/sql (owner, 2026-10-13)" becomes "behind the #sql prefix of POST /api/v2/query (owner, 2026-10-19)"
Milestones, M4 (:931):
- "/api/v2/sql backed by go-duckdb" becomes "DuckDB behind #sql on /api/v2/query, backed by go-duckdb"
```

`internal/AGENTS.md`, Code, after the sql-call bullet:

```
- A query string reaches an engine through query.Run (internal/query), which alone recognizes the #sql prefix. [ADR-0018 D5]
- The HTTP query route calls it, and the Flight server will. [ADR-0018 D5]
- A native result reaches the sink as one step, released by Run after the sink returns. [Rationale: release in Run]
```

`internal/httpapi/AGENTS.md`:

```
Scope paragraph:
- "ADR-0017 holds the sql endpoint's" becomes "ADR-0017 holds DuckDB's, which the query endpoint reaches behind #sql".
Handlers:
- The "status is decided before the first byte" bullet: Cluster.Query is called with WithReadRetry inside query.Run. [fact 9]
- The "batch is released on return" bullet is replaced: query.Run releases the native batch, and a statement without a result set is one step of no fields, which encodes as an empty result. [Rationale: empty batch]
- The sql-endpoint bullet (:82-93) becomes the query endpoint's DuckDB bullet: a #sql body runs on the caller's own DuckDB instance; its errors arrive before the sink and are 400; the instance cap is 503 with Retry-After; a failed attach is 503 without it; a column type no text encoder writes is 500 before the first byte, and a caller casts in SQL or asks for Arrow; a DuckDB query is never retried. [sql bullet, unchanged facts]
- The JSON body of every result route is the array of results. [ADR-0010 D5 as edited]
Tests:
- The round trip's DuckDB step posts the select behind #sql to the query route. [Outcome]
- The DuckDB scalars have one test, TestDuckDBScalars (duckdb_test.go), behind #sql on the query route.
- The instance cap test is TestDuckDBInstanceLimit.
```

`internal/encoding/AGENTS.md`, The seam and the JSON bullets:

```
- Encode writes one batch: the bodies the e2e tool and the fixture write. EncodeStream writes every response the server sends. [fact 6]
- The JSON decoder reads one result (what Encode writes) or the array of results (what every route answers). [fact 7]
```

`internal/olap/AGENTS.md`, Scope: "behind `POST /api/v2/sql`" becomes
"behind the `#sql` prefix of `POST /api/v2/query`".

`internal/encoding/json.go`, the `JSON` type comment: "because one query
is one result" becomes "because one batch is one result" [fact 4], and
the comment of `jsonResults` and the decoder's doc at `:863` name the
array as every route's answer and the object as `Encode`'s [fact 7].

`docs/e2e.md`:

```
- :109, step 6: "read through POST /api/v2/sql with SELECT ..." becomes "read through POST /api/v2/query with #sql SELECT ...".
- :161: the JSON the tool reads is the array every route answers or the one result Encode writes. [fact 7]
```

## 4. Rationale

| decision                                                                                                                                                      | why                                                                                                                                                                                                                 | rejected, and why                                                                                                                                                                                                                                    | gained                                                               | given up                                                                                                                  | settled by                                                                 |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| one query route: `POST /api/v2/query`, and `POST /api/v2/sql` is removed                                                                                      | "query" is not a QuasarDB term and "sql" is not a DuckDB term, so routes named that way mislead; with the prefix on `/query`, `/sql` is a second way to say the same thing, and the Flight port has only the prefix | keeping both routes and naming the handlers by engine: the routes keep the naming problem                                                                                                                                                            | one way to choose the engine on both planes, one handler             | a reversed owner decision (2026-10-13), moved tests                                                                       | owner, 2026-10-19                                                          |
| the reversal is an in-place edit of ADR-0017 and ADR-0018, with no new ADR and no log entry                                                                   | one clause of each changes and the rest of both stays valid                                                                                                                                                         | a new ADR-0019 superseding the clause: one more document, and two ADRs left "accepted" with one clause overridden                                                                                                                                    | the decision reads in the ADRs that own it                           | stretches the in-place rule of `docs/AGENTS.md` to a reversed clause                                                      | owner, 2026-10-19                                                          |
| JSON on `POST /api/v2/query` is always the array of results                                                                                                   | one body shape per route whatever the prefix, the shape the reader answers, and ADR-0018 D5 as written                                                                                                              | the native object with the array for `#sql` only: one route answers two shapes by query text and the dispatcher needs two result paths; merging batches into one object: cannot stream                                                               | one JSON shape across the result routes, one encode path             | the v2 query's JSON shape breaks for existing v2 callers (no released client)                                             | owner, 2026-10-19                                                          |
| ascii whitespace: space, tab, LF, CR, VT, FF, byte by byte                                                                                                    | matches ADR-0018's "bytes"; no UTF-8 decoding, so any encoding splits alike                                                                                                                                         | `unicode.IsSpace` over runes: depends on decoding, larger alphabet                                                                                                                                                                                   | a split independent of the text's encoding                           | a Unicode space before or after `#sql` makes the query native                                                             | owner, 2026-10-19                                                          |
| package `internal/query`, function `Run`                                                                                                                      | the package turns one query string into one result stream, and `query.Run` reads as that at the call site                                                                                                           | `internal/dispatch`: names nothing; `internal/engine`: as generic and `engine.Run` reads oddly; inside `internal/httpapi`: the Flight server would import the HTTP package; a `Cluster` method: ADR-0018 D5 puts the dispatcher above `internal/qdb` | one home for the prefix rule, callable from both planes              | a package of one file                                                                                                     | owner, 2026-10-19                                                          |
| the tests name DuckDB where they said "sql" (`TestDuckDBScalars`, `TestDuckDBInstanceLimit`, `checkDuckDB*`, `directDuckDB`, `duckdb_back`, `duckdb_test.go`) | the owner's review: "sql" for DuckDB makes the naming problem worse, and these names change with the route anyway                                                                                                   | keeping the names: they point at a route that is gone                                                                                                                                                                                                | names that say which engine a test drives                            | a rename in the diff                                                                                                      | proposal                                                                   |
| `Cluster.SQL` is `Cluster.Duck`, and "a sql call" is "a DuckDB call"                                                                                          | the owner names DuckDB after qdb-duck, and "SQL" names no engine once `/api/v2/sql` is gone                                                                                                                         | keeping `Cluster.SQL`: it names the SQL text and reads as a route that no longer exists; `Cluster.DuckDB`: the owner picked Duck, the name of the extension                                                                                          | `Cluster.Query` and `Cluster.Duck` name the two engines side by side | one rename across the callers and the documents of fact 19                                                                | owner, 2026-10-19 for the name; proposal for "a DuckDB call"               |
| `olap` names and the fixture's `SelectSQL` keep their names                                                                                                   | they name the DuckDB SQL text they run and no route                                                                                                                                                                 | renaming them here: a wider diff with no route change behind it                                                                                                                                                                                      | a smaller unit                                                       | the word "sql" stays in `olap` and the fixture                                                                            | proposal                                                                   |
| the tests move to `/query` before `/sql` is removed                                                                                                           | each commit builds and passes its tests                                                                                                                                                                             | removal first: the sql tests fail in that commit                                                                                                                                                                                                     | every commit green                                                   | one more commit                                                                                                           | proposal                                                                   |
| `Run` reads the cluster from ctx                                                                                                                              | handlers read the cluster from the request context and nothing is injected                                                                                                                                          | a `*qdb.Cluster` parameter: a second way in for what ctx already carries                                                                                                                                                                             | the handlers' rule holds below them                                  | callers need a ctx that carries a cluster (`qdb.ClusterFrom` panics otherwise)                                            | `internal/AGENTS.md`, Logging                                              |
| the native batch is released by `Run` after the sink returns                                                                                                  | a sink that never pulls the step would otherwise leak C-allocated buffers                                                                                                                                           | release inside the iterator after the step, as `qdb.lent` does: leaks when the sink never iterates                                                                                                                                                   | no leak on any sink path                                             | the batch lives until the sink returns, which is longer than one step (harmless, the session is already back in its pool) | proposal                                                                   |
| a statement without a result set is one step of an empty-schema batch                                                                                         | Arrow's `EncodeStream` reads the first step's schema, and a nil step panics there [fact 5]                                                                                                                          | a nil step: panics in Arrow; zero steps: breaks the `Batches` rule of at least one step                                                                                                                                                              | every encoder sees a step                                            | JSON answers `[{"columns":[]}]` where `Encode` answered `{"columns":[]}`                                                  | proposal                                                                   |
| one read retry on the native leg, none on DuckDB                                                                                                              | the native query runs before the sink and nothing has been sent; a DuckDB call is never retried                                                                                                                     | retrying in the handler: the handler no longer calls `Cluster.Query`                                                                                                                                                                                 | both planes get the retry from one place                             | none                                                                                                                      | `internal/httpapi/AGENTS.md`, Handlers; `internal/AGENTS.md`, the sql call |
| `split` and `isSpace` unexported, `Run` the only export                                                                                                       | nothing outside the package needs the split; the property is white-box                                                                                                                                              | an exported `Split` or an `Engine` type: surface with no caller                                                                                                                                                                                      | minimal surface                                                      | the Flight slice exports something only if it needs it                                                                    | proposal                                                                   |
| `split` and `Run` land in one commit                                                                                                                          | `unused` in the lint stack fails a commit that adds an unexported function with no caller [fact 15]                                                                                                                 | split first, Run next: the first commit fails lint                                                                                                                                                                                                   | every commit lints                                                   | one commit carries two functions                                                                                          | `.golangci.yml:5-8`                                                        |
| the brief's endpoint sketch names the array and drops the sql line                                                                                            | the owner asked to keep the sketch in step with the tree                                                                                                                                                            | leaving the sketch alone and pointing at ADR-0010                                                                                                                                                                                                    | the sketch reads correctly                                           | one more place states the shape, with ADR-0010 as its owner                                                               | owner, 2026-10-19                                                          |

## 5. Knowledge

Commit 4 (`Cluster.Duck`): the wording of fact 19, and row "Cluster.Duck"
in the method name itself. No document records the old name.

Commit 5 (`internal/query`):

- `split`, `Run`: the overview claims of section 3, with ADR-0018 D3 and
  D5, facts 5, 8, 9, 10 and 11, and the rows "ascii whitespace",
  "release in Run" and "empty batch" as their evidence.
- `docs/adr/0018-flight-sql.md`, Decision 5: the package name (row
  "package internal/query").
- `docs/brief.md`, Project structure: the new line.
- `internal/AGENTS.md`, Code: the three bullets of section 3, which carry
  the rows "release in Run" and "package internal/query".
- The row "Run reads the cluster from ctx" lands in `Run`'s doc comment.
  The row "unexported split" is stated by the code and lands nowhere.

Commit 6 (`TestSplit`): the shapes of section 3, with the "ascii
whitespace" row at step 1 and step 3.

Commit 7 (`handleQuery` on `query.Run`):

- `handleQuery`'s narrative of section 3.
- ADR-0010 edits of section 3: the row "JSON array" with both rejected
  alternatives, and the status-table row.
- `docs/brief.md`, the endpoint-sketch line for `/query`: row "brief
  sketch".
- `internal/httpapi/AGENTS.md`, Handlers: the WithReadRetry, release and
  array bullets, rows "empty batch" and "one read retry".
- `internal/encoding/AGENTS.md`, `internal/encoding/json.go`,
  `docs/e2e.md:161`: facts 4, 6 and 7.
- `direct` and `TestQueryCompressed` comments.

Commits 8 to 11 (tests and e2e on `/query`): the comments of section 3;
`internal/httpapi/AGENTS.md`, Tests; `docs/e2e.md:109`; row "DuckDB in
the test names" in the renamed names themselves, and row "tests move
first" in the commit order.

Commit 12 (`/sql` removed):

- ADR-0018 Decisions 4 and 14, Consequences and the rejected row; ADR-0017
  Decisions 1 and 11: rows "one query route" and "in-place reversal".
- `docs/brief.md` `:406`, `:458`, the sketch's sql line, `:931`.
- `internal/httpapi/AGENTS.md`: Scope paragraph and the DuckDB bullet.
- `internal/olap/AGENTS.md`, Scope.
- Row "`olap` names and `SelectSQL` keep their names" lands nowhere,
  because a name that stays needs no record.

## 6. How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write each commit's comments and document rows from sections 3 and 5,
   in the same commit as the code. Every claim listed appears once, at
   the place named. No claim is added without evidence. A claim the code
   contradicts is reported in the build-stage message and not written.
3. A reason that comes up while building and is not in this plan is
   written where it is decided, with its evidence, or asked of the owner
   through the question tool before the commit that needs it.
4. After the last code commit, run `/doc-discipline all internal/query
internal/httpapi internal/encoding internal/olap/AGENTS.md
internal/AGENTS.md tests/e2e/round-trip.sh docs/adr/0010-v2-query-request-negotiation-errors.md
docs/adr/0017-embedded-duckdb.md docs/adr/0018-flight-sql.md
docs/brief.md docs/e2e.md` and land one small commit per finding.
5. Before the build-stage message, run `/doc-discipline check` over the
   same paths with `docs/query-dispatch-plan.md`, and fix or report every
   "Unlanded from the plan" finding.
6. Before each commit, run `npx prettier --write` on every Markdown file
   touched, `make lint` and `go test -p 1 ./...` (with the qdbd services
   up). Commit 11 also runs `make -C tests/e2e test-round-trip`.
7. After commit 12, grep fact 16's list again. Only log entries may still
   name `/api/v2/sql`.

## 7. Commits

1. `docs(plan): query-dispatch-plan.md, one function runs a query string on the engine its #sql prefix names` (committed)
2. `docs(plan): query-dispatch-plan.md, post /api/v2/query becomes the one query route` (committed)
3. `docs(plan): query-dispatch-plan.md, Cluster.SQL becomes Cluster.Duck` (this revision)
4. `refactor(qdb): Cluster.SQL is Cluster.Duck, after qdb-duck`
5. `feat(query): Run answers a query as batches on the engine its #sql prefix names`
6. `test(query): a property over generated prefixes and ascii whitespace pins the split`
7. `feat(httpapi): POST /api/v2/query streams through query.Run and honors #sql`
8. `test(httpapi): the round trip reads every table through #sql on POST /api/v2/query`
9. `test(httpapi): the duckdb scalars and the instance cap go through #sql on POST /api/v2/query`
10. `test(httpapi): the error rows answer #sql failures on POST /api/v2/query and #sqlx natively`
11. `test(e2e): the round trip reads every table through #sql on POST /api/v2/query`
12. `feat(httpapi): POST /api/v2/sql is removed, and #sql on POST /api/v2/query reaches duckdb`
13. The `/doc-discipline all` run of section 6, one `docs(<scope>): ...` commit per finding, then the `/doc-discipline check` against this plan.
14. `docs(log): m5 in flight with one query route over both engines`
15. Verify: push `sc-19567/rr-query-dispatch`, build its head in
    Buildkite, wait for the result. Green: report the build number. Red:
    fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

1. Should the error table keep one row proving `POST /api/v2/sql` is
   gone (404)? Recommendation: no. The mux's 404 for an unknown path
   needs no row, and the row would name a route nothing serves.
2. Should `Run` release the native batch after the sink returns, or
   should it be released inside the iterator after the step as `qdb.lent`
   does? Recommendation: in `Run`, because it cannot leak.
3. A native statement without a result set answers JSON
   `[{"columns":[]}]`. Recommendation: accept it as the array shape's
   empty result.
