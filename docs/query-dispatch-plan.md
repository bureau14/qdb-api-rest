# Plan: query dispatch -- one function runs a query string on the engine its `#sql` prefix names

Status: draft

This plan covers the first slice of M5 (`docs/log.md`, Current state). It is
written for an executor who has none of the seed conversation of
2026-10-19. Everything the build needs is here.

## 1. Outcome

When the unit lands:

- A new package, `internal/query`, holds one exported function, `Run`. It
  takes a query string, recognizes the `#sql` prefix of ADR-0018, Decision
  3, and runs the query on the caller's DuckDB instance (`Cluster.SQL`)
  with the prefix stripped, or on the native engine (`Cluster.Query`)
  unchanged. Either way it hands the result to a sink as `qdb.Batches`.
- `POST /api/v2/query` runs every body through `query.Run` and encodes
  through `EncodeStream`. A `#sql` body runs on DuckDB. The JSON body of
  the route is the top-level array of results, one per batch, for both
  engines. CSV, NDJSON and Arrow bytes of a native query are unchanged.
- `handleQuery` and `handleSQL` share one streaming body,
  `serveStatement`, and differ only in the function that runs the query.
- ADR-0010, ADR-0018, the brief, `internal/AGENTS.md`,
  `internal/httpapi/AGENTS.md`, `internal/encoding/AGENTS.md`,
  `internal/encoding/json.go` and `docs/e2e.md` say so.

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
3. `POST /api/v2/sql` does not look for the prefix
   [`docs/adr/0018-flight-sql.md`, Decision 4].
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
10. `Cluster.SQL` returns the sink's error unchanged, is never retried,
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
    Both become wrong in this unit and are edited in place
    (`docs/AGENTS.md`, ADRs: an in-place edit for a decision within the
    ADR's subject, no log entry).
15. The lint stack is golangci-lint `standard` (which includes `unused`)
    plus forbidigo and sloglint [`.golangci.yml:5-8`]. An unexported
    function used only from a later commit would fail lint in the commit
    that adds it.
16. Unverified: that qdbd answers `#sqlx SELECT 1` with a parse error
    (400) rather than something else. The executor checks it in commit 7
    before writing the row, and reports a different answer in the
    build-stage message.

## 3. Design

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
	// 2. a DuckDB query is Cluster.SQL, which is never retried and streams its own lent batches [fact 10]
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

### `internal/httpapi/sql.go`, in file order

```go
// runner runs a query string as u and hands its result to sink, as query.Run and Cluster.SQL do. [fact 10; internal/query]
type runner func(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error
```

```go
// serveStatement answers a query request: it reads the body as query text, runs it through run, and streams the result in the negotiated format. [fact 12]
// The status is decided before the first byte. [ADR-0010 D5]
func serveStatement(w http.ResponseWriter, r *http.Request, run runner) {
	// style: doc comment + numbered overview + step comments
	// because: the body of handleSQL moved here unchanged, and it already has this shape [internal/httpapi/sql.go:13-24]
	//
	// The query runs inside the sink, and every error of the run arrives before the sink runs, so the status is decided before the first byte. [fact 9, fact 10]
	// Two kinds of failure come out, and they answer differently. [sql.go:17-23]
	//
	// 1. read the body as query text: a non-text body is 415, and the body is capped and read whole [sql.go:26-33]
	// 2. run the query, with the encoder streaming inside the sink [sql.go:36-42]
	// 3. a failure of the stream: with zero bytes out it is 500, where a column type no encoder writes lands; after the first byte the stream is cut and only the log hears of it [sql.go:44-52]
	// 4. a failure of the run, before any byte: the engine's error is the caller's 400, and the instance cap, a refused credential, an open breaker and an unreachable cluster answer as writeClusterError maps them [query.go:66-105]
}
```

```go
// handleSQL runs the body as DuckDB SQL as the bearer's user and streams the result batch by batch in the negotiated format. [sql.go:11-12]
// It does not look for the #sql prefix. [ADR-0018 D4]
func handleSQL(w http.ResponseWriter, r *http.Request) {
	// style: doc comment alone
	// because: one call to serveStatement with Cluster.SQL as the runner
}
```

### `internal/httpapi/query.go`

`maxBodyBytes`, `readBody`, `isQueryText`, `countingWriter`, `retryAfter`
and `writeClusterError` keep their code and comments. `handleQuery`
changes:

```go
// handleQuery runs the body through query.Run as the bearer's user and streams the result in the negotiated format. [Outcome]
// A #sql body runs on DuckDB, and any other body on the native engine. [ADR-0018 D4]
func handleQuery(w http.ResponseWriter, r *http.Request) {
	// style: doc comment alone
	// because: one call to serveStatement with query.Run as the runner; the steps are serveStatement's
}
```

### Tests in `internal/httpapi`

```go
// query_test.go
// direct runs q through query.Run as the server's user and encodes the stream with e. The endpoint must answer these exact bytes. [fact 4; internal/httpapi/AGENTS.md, Tests: the oracle is the encoder run directly]
func (s server) direct(t *rapid.T, e encoding.Encoder, q string) []byte {
	// style: doc comment alone
	// because: one call and one encode, like directSQL
	//
	// at the call: the ctx is s.ctx, because query.Run reads the cluster from ctx [Run's contract]
}
```

`TestQueryCompressed` keeps its steps. Its step 2 comment changes from
"written by the JSON encoder" to the stream encoder's bytes, which is the
array of one result [fact 4].

```go
// roundtrip_test.go
// checkSQLFormats posts tbl's DuckDB select in every format to /api/v2/sql, and behind the #sql prefix to /api/v2/query, and compares each body with directSQL. [Outcome; ADR-0018 D4]
func (s server) checkSQLFormats(t *rapid.T, tbl table.Table) {
	// style: doc comment alone + inline note at the loop
	// because: the existing loop runs once per route
	//
	// at the route loop: the prefixed body is compared with directSQL over the stripped text, so the bytes prove the prefix reached DuckDB and the rest of the text reached it unchanged [fact 1]
	// the existing null-slot note on the Arrow comparison stays [roundtrip_test.go:248-251]
}
```

The round trip's file comment and step 6 comment say that the DuckDB
read goes through both routes [roundtrip_test.go:1-13, :391-398].

```go
// errors_test.go, the query block gains
// "query: invalid #sql":           POST /api/v2/query, "#sql NOT A QUERY", bearer, 400 [fact 10, fact 13]
// "query: #sql unsupported type":  POST /api/v2/query, "#sql SELECT [1]", bearer, 500 [errors_test.go:84 "a list is a type no text encoder writes"]
// "query: #sqlx is native":        POST /api/v2/query, "#sqlx SELECT 1", bearer, 400 [ADR-0018 D3; fact 16]
// the secure block gains
// "query: #sql anonymous on the secure cluster": POST /api/v2/query, "#sql SELECT 1", anonymous bearer, 401 invalid_token [internal/AGENTS.md, the refused credential]
```

### Document rows

`docs/adr/0018-flight-sql.md`, Decision 5, one sentence added:

```
- The dispatcher is query.Run in internal/query.
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
Endpoint sketch, the POST /api/v2/query line:
- native qdb query, or DuckDB behind a #sql prefix (ADR-0018); streamed, content-negotiated; JSON is the array of results, one per batch (ADR-0010)
```

`internal/AGENTS.md`, Code, after the sql-call bullet:

```
- A query string reaches an engine through query.Run (internal/query), which alone recognizes the #sql prefix. [ADR-0018 D5]
- The HTTP query route calls it, and the Flight server will; POST /api/v2/sql calls Cluster.SQL directly, because it takes no prefix. [ADR-0018 D4]
- A native result reaches the sink as one step, released by Run after the sink returns. [Rationale: release in Run]
```

`internal/httpapi/AGENTS.md`, Handlers:

```
- The "status is decided before the first byte" bullet: Cluster.Query is called with WithReadRetry inside query.Run. [fact 9]
- The "batch is released on return" bullet is replaced: query.Run releases the native batch, and a statement without a result set is one step of no fields, which encodes as an empty result. [Rationale: empty batch]
- The query endpoint and the sql endpoint share serveStatement; the query route runs query.Run and honors #sql, and the sql route runs Cluster.SQL and does not. [Rationale: shared handler body]
- The JSON body of every result route is the array of results. [ADR-0010 D5 as edited]
Tests:
- The round trip's sql step posts the DuckDB select to both routes, behind #sql on the query route. [Outcome]
```

`internal/encoding/AGENTS.md`, The seam and the JSON bullets:

```
- Encode writes one batch: the bodies the e2e tool and the fixture write. EncodeStream writes every response the server sends. [fact 6]
- The JSON decoder reads one result (what Encode writes) or the array of results (what every route answers). [fact 7]
```

`internal/encoding/json.go`, the `JSON` type comment: "because one query
is one result" becomes "because one batch is one result" [fact 4], and
the comment of `jsonResults` and the decoder's doc at `:863` name the
array as every route's answer and the object as `Encode`'s [fact 7].

`docs/e2e.md:161`: the JSON the tool reads is the array every route
answers or the one result `Encode` writes [fact 7].

## 4. Rationale

| decision                                                              | why                                                                                                                                        | rejected, and why                                                                                                                                                                                                                                    | gained                                                   | given up                                                                                                                  | settled by                                                                 |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| JSON on `POST /api/v2/query` is always the array of results           | one body shape per route whatever the prefix, the shape `/sql` and the reader answer, and ADR-0018 D5 as written                           | the native object with the array for `#sql` only: one route answers two shapes by query text and the dispatcher needs two result paths; merging batches into one object: cannot stream                                                               | one JSON shape across the result routes, one encode path | the v2 query's JSON shape breaks for existing v2 callers (no released client)                                             | owner, 2026-10-19                                                          |
| ascii whitespace: space, tab, LF, CR, VT, FF, byte by byte            | matches ADR-0018's "bytes"; no UTF-8 decoding, so any encoding splits alike                                                                | `unicode.IsSpace` over runes: depends on decoding, larger alphabet                                                                                                                                                                                   | a split independent of the text's encoding               | a Unicode space before or after `#sql` makes the query native                                                             | owner, 2026-10-19                                                          |
| package `internal/query`, function `Run`                              | the package turns one query string into one result stream, and `query.Run` reads as that at the call site                                  | `internal/dispatch`: names nothing; `internal/engine`: as generic and `engine.Run` reads oddly; inside `internal/httpapi`: the Flight server would import the HTTP package; a `Cluster` method: ADR-0018 D5 puts the dispatcher above `internal/qdb` | one home for the prefix rule, callable from both planes  | a package of one file                                                                                                     | owner, 2026-10-19                                                          |
| `Run` reads the cluster from ctx                                      | handlers read the cluster from the request context and nothing is injected                                                                 | a `*qdb.Cluster` parameter: a second way in for what ctx already carries                                                                                                                                                                             | the handlers' rule holds below them                      | callers need a ctx that carries a cluster (`qdb.ClusterFrom` panics otherwise)                                            | `internal/AGENTS.md`, Logging                                              |
| the native batch is released by `Run` after the sink returns          | a sink that never pulls the step would otherwise leak C-allocated buffers                                                                  | release inside the iterator after the step, as `qdb.lent` does: leaks when the sink never iterates                                                                                                                                                   | no leak on any sink path                                 | the batch lives until the sink returns, which is longer than one step (harmless, the session is already back in its pool) | proposal                                                                   |
| a statement without a result set is one step of an empty-schema batch | Arrow's `EncodeStream` reads the first step's schema, and a nil step panics there [fact 5]                                                 | a nil step: panics in Arrow; zero steps: breaks the `Batches` rule of at least one step                                                                                                                                                              | every encoder sees a step                                | JSON answers `[{"columns":[]}]` where `Encode` answered `{"columns":[]}`                                                  | proposal                                                                   |
| one read retry on the native leg, none on DuckDB                      | the native query runs before the sink and nothing has been sent; a sql call is never retried                                               | retrying in the handler: the handler no longer calls `Cluster.Query`                                                                                                                                                                                 | both planes get the retry from one place                 | none                                                                                                                      | `internal/httpapi/AGENTS.md`, Handlers; `internal/AGENTS.md`, the sql call |
| `split` and `isSpace` unexported, `Run` the only export               | nothing outside the package needs the split; the property is white-box                                                                     | an exported `Split` or an `Engine` type: surface with no caller                                                                                                                                                                                      | minimal surface                                          | the Flight slice exports something only if it needs it                                                                    | proposal                                                                   |
| `split` and `Run` land in one commit                                  | `unused` in the lint stack fails a commit that adds an unexported function with no caller [fact 15]                                        | split first, Run next: the first commit fails lint                                                                                                                                                                                                   | every commit lints                                       | one commit carries two functions                                                                                          | `.golangci.yml:5-8`                                                        |
| `handleQuery` and `handleSQL` share `serveStatement`                  | the two handlers are the same body except for the call that runs the query [fact 12]                                                       | two copies of the body: a fix to one misses the other                                                                                                                                                                                                | one streaming body                                       | one more indirection in the handler file                                                                                  | proposal                                                                   |
| no `#sql` request in the e2e round trip                               | the route is not new, the sql step already proves DuckDB over HTTP on the built binary, and the in-process round trip gets the `#sql` step | a `#sql` request in `round-trip.sh`                                                                                                                                                                                                                  | a shorter e2e                                            | the built binary's `#sql` on `/query` is proven only in process                                                           | owner, 2026-10-19                                                          |
| the brief's endpoint-sketch line names the array                      | owner asked to keep the sketch in step with reality                                                                                        | leaving the sketch alone and pointing at ADR-0010                                                                                                                                                                                                    | the sketch reads correctly                               | one more place states the shape, with ADR-0010 as its owner                                                               | owner, 2026-10-19                                                          |

## 5. Knowledge

Commit 2 (`internal/query`):

- `split`, `Run`: the overview claims of section 3, with ADR-0018 D3 and
  D5, facts 5, 8, 9, 10 and 11, and the rows "ascii whitespace",
  "release in Run" and "empty batch" as their evidence.
- `docs/adr/0018-flight-sql.md`, Decision 5: the package name (row
  "package internal/query").
- `docs/brief.md`, Project structure: the new line.
- `internal/AGENTS.md`, Code: the three bullets of section 3, which carry
  the rows "release in Run" and "package internal/query" and ADR-0018 D4.
- The rows "Run reads the cluster from ctx" and "unexported split" land
  in `Run`'s doc comment (the ctx) and nowhere else, because the code
  states the second.

Commit 3 (`TestSplit`): the shapes of section 3, with the "ascii
whitespace" row at step 1 and step 3.

Commit 4 (`serveStatement`): the overview moved from `handleSQL`; the row
"shared handler body" lands in `internal/httpapi/AGENTS.md` in commit 5,
where the second caller arrives.

Commit 5 (`handleQuery` on `query.Run`):

- ADR-0010 edits of section 3: the rows "JSON array" (with both
  rejected alternatives) and the status-table row.
- `docs/brief.md`, endpoint sketch line: row "brief sketch".
- `internal/httpapi/AGENTS.md`: Handlers bullets of section 3, rows
  "shared handler body", "empty batch" and "one read retry".
- `internal/encoding/AGENTS.md`, `internal/encoding/json.go`,
  `docs/e2e.md:161`: facts 4, 6 and 7.
- `direct` and `TestQueryCompressed` comments.

Commit 6 (round trip): `checkSQLFormats`, the file comment and step 6;
`internal/httpapi/AGENTS.md`, Tests.

Commit 7 (error rows): the rows themselves; fact 16 is checked here.

The row "no `#sql` in the e2e round trip" has its home in `docs/e2e.md`,
The v2 round trip, as one sentence in commit 6: the round trip's sql step
goes to `/api/v2/sql` only, and `#sql` on `/api/v2/query` is proven by
the in-process round trip.

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
internal/httpapi internal/encoding docs/adr/0010-v2-query-request-negotiation-errors.md
docs/adr/0018-flight-sql.md docs/brief.md docs/e2e.md internal/AGENTS.md`
   and land one small commit per finding.
5. Before the build-stage message, run `/doc-discipline check` over the
   same paths with `docs/query-dispatch-plan.md`, and fix or report every
   "Unlanded from the plan" finding.
6. Run `npx prettier --write` on every Markdown file touched, `make lint`
   and `go test -p 1 ./...` (with the qdbd services up) before each
   commit.

## 7. Commits

1. `docs(plan): query-dispatch-plan.md, one function runs a query string on the engine its #sql prefix names` (this document)
2. `feat(query): Run answers a query as batches on the engine its #sql prefix names`
3. `test(query): a property over generated prefixes and ascii whitespace pins the split`
4. `refactor(httpapi): the sql handler streams through serveStatement`
5. `feat(httpapi): POST /api/v2/query streams through query.Run and honors #sql`
6. `test(httpapi): the round trip posts the duckdb select behind #sql to POST /api/v2/query`
7. `test(httpapi): the error rows answer #sql failures on POST /api/v2/query and #sqlx natively`
8. The `/doc-discipline all` run of section 6, one `docs(<scope>): ...` commit per finding, then the `/doc-discipline check` against this plan.
9. `docs(log): m5 in flight with query dispatch on the http plane`
10. Verify: push `sc-19567/rr-query-dispatch`, build its head in
    Buildkite, wait for the result. Green: report the build number. Red:
    fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

1. Should `Run` release the native batch after the sink returns, or
   should it be released inside the iterator after the step as `qdb.lent`
   does? Recommendation: in `Run`, because it cannot leak.
2. Should `handleQuery` and `handleSQL` share `serveStatement`?
   Recommendation: yes, one streaming body for both result routes.
3. A native statement without a result set answers JSON
   `[{"columns":[]}]`. Recommendation: accept it as the array shape's
   empty result.
