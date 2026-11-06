# v1 query over the v2 query and a v2 find route

Thread: v1 query
Status: draft

## Outcome

When this unit lands:

- `POST /api/v2/find` takes a find expression as a text body and
  answers `{"entries":[...]}`, the names the expression matches, through
  `Cluster.Find`. It is a step of the round trip and has rows in
  `TestErrorRows`.
- `POST /api/v1/query` and its alias `POST /api/query` are served by
  `internal/httpapi/v1`. The handler takes the old server's
  `{"query": ...}` body and the token from the `Authorization` header,
  with or without `Bearer `, or from `?token=`. It calls
  `POST /api/v2/query` in process for Arrow IPC and answers the old
  `{"tables":[{"columns":[{"data","name","type"}]}]}` body. A query
  whose text begins with `find` calls `POST /api/v2/find` instead and
  answers one table per name with `"columns":null`.
- `internal/encoding` exports `JSONCell`, the appender of one column's
  JSON cells, so the v1 body writes a type outside the old server's six
  words as the v2 JSON writes it.
- The brief's endpoint sketch lists `POST /api/v2/find`, its find
  paragraph says v1 wraps that route, and its deviation table lists the
  v1 query's new deviations.
- The log's Next no longer lists the v1 query, and M9 exit criterion 2
  compares error bodies by status and shape (owner, 2026-11-06).

Left for later units, each named in the log's Next:

- the side-by-side compatibility script, run by hand, against the old
  server built from `master` (ADR-0015, Decision 4).

## Verified facts

1. The old query handler runs `QueryData`, which sends a query that
   begins with `find` (`strings.HasPrefix`) to `handle.Find().ExecuteString`
   and anything else to `handle.Query(q).Execute()`
   (`master:qdbinterface/query.go:105-111`).
2. The old find answers one table per name, `Columns` nil, so the body
   is `{"name":"<n>","columns":null}` per entry, and no match is
   `{"tables":[]}` (`master:qdbinterface/query.go:13-35`).
3. The old query answers `{"tables":[]}` for a nil result and for zero
   rows (`master:qdbinterface/query.go:48-57`). Otherwise it answers one
   table with an empty name, which `omitempty` drops
   (`master:qdbinterface/query.go:103`, `master:models/query_table.go:23-26`).
4. A column is `{"data":[...],"name":"...","type":"..."}`, `data` without
   `omitempty`, `name` and `type` with it
   (`master:models/query_column.go:21-27`). Its type starts as `none` and
   each non-null cell sets it, so the word is the last non-null row's
   (`master:qdbinterface/query.go:64-95`).
5. The old cells are Go values that `encoding/json` writes: `int64`,
   `float64`, `string`, `[]byte` (standard base64) and `time.Time`
   from `time.Unix(sec, nsec)` in the process's zone, and `nil` for a
   null (`master:vendor/github.com/bureau14/qdb-api-go/v3/query.go:114-136`,
   `master:vendor/github.com/bureau14/qdb-api-go/v3/time.go:43-45`).
   `encoding/json` fails on NaN and the infinities.
6. A failed query is 400 `{"message"}`, and a failed session lease is 500
   `{"message"}` (`master:restapi/configure_qdb_api_rest.go:431-450`).
7. The old security is two alternatives in order, the `Authorization`
   header and then `?token=` (`master:restapi/embedded_spec.go:657-676`).
   A scheme with an empty value is skipped, the first that verifies
   wins, and if every scheme with a value fails, the last failure is the
   answer (`master:vendor/github.com/go-openapi/runtime/security/authenticator.go:172-180`,
   `master:vendor/github.com/go-openapi/runtime/middleware/router.go:256-282`).
   With no value at all the answer is
   `errors.Unauthenticated("invalid credentials")`
   (`master:vendor/github.com/go-openapi/runtime/middleware/context.go:429-435`),
   whose message is `unauthenticated for invalid credentials`
   (`master:vendor/github.com/go-openapi/errors/auth.go:19-22`).
8. The header's token has one leading `Bearer ` removed, case-sensitive,
   and a header without it is the token itself
   (`master:restapi/configure_qdb_api_rest.go:329-338`).
9. A go-openapi error is served as `{"code":<n>,"message":"..."}` with
   the status `n`, or 422 for a validation code of 600 and up
   (`master:vendor/github.com/go-openapi/errors/api.go:136-181`).
   Authentication runs before the body is bound
   (`master:vendor/github.com/go-openapi/runtime/middleware/security.go:19-38`).
10. An absent body is `errors.Required`, 422 with code 602, and a body
    that does not decode is `NewParseError`, 400 with code 400
    (`master:restapi/operations/query/post_query_parameters.go:49-80`,
    `master:vendor/github.com/go-openapi/errors/parsing.go:63-78`).
    `{}` decodes to an empty query, because `Query.Validate` checks
    nothing (`master:models/query.go:18-27`).
11. The v2 query reads a text body (`isQueryText`, absent counts as
    `text/plain`) under `maxBodyBytes`, and negotiates `Accept`
    (`internal/httpapi/query.go:27-52,141-200`). Its errors go through
    `writeClusterError`: 401 for refused credentials, 503 for an open
    breaker, the instance cap and an unreachable cluster, the caller's
    status otherwise (`internal/httpapi/query.go:110-138`). A column no
    text encoder writes is 500 on the text formats only, so an Arrow
    request carries it.
12. `Cluster.Find` runs the expression under the read retry and answers
    the names in the cluster's order. An expression that matches nothing
    answers an empty list (`internal/qdb/tag.go:39-53`). It answers the
    binding's slice, which may be nil.
13. `GET /api/v2/tags/{tag}` answers `entriesResponse` through
    `json.NewEncoder` (`internal/httpapi/tags.go:14-18,55-73`), and the
    tag routes register through `handleData` with `withCompression` and
    `requireBearer` (`internal/httpapi/tags.go:76-80`).
14. The v1 login builds an inner request on the outer context, records
    the v2 answer in an `httptest.Recorder`, and records its own
    duration with `ObserveRequest` under the v1 route
    (`internal/httpapi/v1/login.go:22-85`,
    `internal/observe/metrics.go:110-111`). `writeV1JSON` writes without
    HTML escaping and with a trailing newline
    (`internal/httpapi/v1/json.go:15-21`).
15. `jsonCell` is the type switch of the JSON encoders, one appender per
    column, every DuckDB scalar included, null and NaN written as `null`
    (`internal/encoding/json.go:120-216`).
16. The table fixture draws one to five columns of the six QuasarDB
    types, zero to 40 rows, and one null density from 0 to 100 percent
    per table, so all-null columns occur (`internal/qdbtest/table/table.go:186-230`).
    `Select` answers `$timestamp` and the columns in order, and
    `table.Check` compares a batch with the table
    (`internal/qdbtest/table/table.go:254-264,379-388`).
17. The round trip's step 7 tags every table with one drawn tag, reads
    the tag's entries as a set, and detaches it
    (`internal/httpapi/roundtrip_test.go:497-533`). The error rows are
    one map of `errorRow` (`internal/httpapi/errors_test.go:27-140`).
18. The brief records that no v2 route takes a find expression
    (`docs/brief.md:586-590`). The owner reversed it on 2026-11-06 in
    this unit's thread.

## Design

### `internal/qdb/tag.go`

`Tagged`'s inline comment at the `Find` call is rewritten whole from the
new code. The claim it carries:

```go
func (c *Cluster) Tagged(ctx context.Context, u User, tag string) ([]string, error) {
	// style: doc comment + inline notes
	// because: unchanged code, one comment rewritten
	//
	// at c.Find: the tag goes through the find core, which POST /api/v2/find also serves [Rationale 1]
}
```

### `internal/httpapi/find.go` (new)

```go
// findPath is the find route. [Verified 13]
const findPath = "/api/v2/find"

// handleFind answers the names of the entries the body's find expression
// matches, as {"entries":[...]}. An expression that matches nothing is 200
// with an empty list. Anything the cluster answered is the caller's 400. [Verified 12] [Rationale 2]
func handleFind(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + inline notes
	// because: one handler of four lines with two lines a reader asks about
	//
	// at isQueryText: the expression is query text, under the query's content types and cap [Rationale 3]
	// at names == nil: the binding answers nil for no match, and the list body never says null [Verified 12]
	//    names, err := qdb.ClusterFrom(ctx).Find(ctx, caller(r), string(expr))
}

// registerFindRoute serves the find route behind the bearer middleware.
func registerFindRoute(mux *http.ServeMux) {
	// style: doc comment alone
	// because: one registration, as registerTagRoutes
}
```

`NewHandler` calls `registerFindRoute` beside `registerTagRoutes`.
`entriesResponse` stays in `tags.go`, and its comment says it is the
body of both routes.

### `internal/httpapi/errors_test.go`

Three rows join the map under a `// the find` heading:

```go
// "find: no token":           POST findPath, "find(tag='qdbtest_tag')", nil -> 401, "Bearer"
// "find: json body":          POST findPath, `{"query":"x"}`, jsonBody -> 415
// "find: invalid expression": POST findPath, "find(", bearer -> 400
```

### `internal/httpapi/roundtrip_test.go`, step 7

```go
// 7. ... The find route answers the same set for the tag's expression, and
//    after the detach it answers 200 with no entries, while the tag route
//    answers 404 [Verified 12] [Rationale 2]
```

A helper `func (s server) find(expr string) *httptest.ResponseRecorder`
joins `tags_test.go` beside `tagged`. The file header's sentence on the
tags names the find.

### `internal/encoding/json.go`

```go
// JSONCell answers the function that appends cell i of the column as the
// JSON encoder writes it, null included. A type the encoder does not write
// is ErrUnsupportedType, which names the column. [Verified 15]
func JSONCell(f arrow.Field, a arrow.Array) (func(dst []byte, i int) []byte, error) {
	// style: doc comment alone
	// because: a forwarder to jsonCell
}
```

### `internal/httpapi/v1/json.go`

```go
// v1AuthError is the old server's 401, and the body of its request
// errors: the status as code, and the message. [Verified 9]
type v1AuthError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
```

### `internal/httpapi/v1/tables.go` (new)

```go
// v1Tables is the old server's query answer. [Verified 2-4]
type v1Tables struct {
	Tables []v1Table `json:"tables"`
}

// v1Table is one table: its name, empty for a query, and its columns, nil
// for a find. [Verified 2-4]
type v1Table struct {
	Name    string     `json:"name,omitempty"`
	Columns []v1Column `json:"columns"`
}

// v1Column is one column, its keys in the old server's order. [Verified 4]
type v1Column struct {
	Data []any  `json:"data"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// v1Word answers the old server's word for an Arrow type: blob, double,
// int64, string or timestamp, and the Arrow type's name for any other. [Verified 4] [Rationale 7]
func v1Word(t arrow.DataType) string {
	// style: doc comment + inline notes
	// because: one switch on the type id with one line a reader asks about
	//
	// at the default: only DuckDB answers another type, so no old client sees one [Rationale 7]
}

// v1Cell answers the function that turns cell i of the column into the
// value encoding/json writes as the old server wrote it. A null is nil,
// and so are NaN and the infinities. A type outside the old server's
// five answers the v2 JSON cell as raw JSON. [Verified 5] [Rationale 6-8]
func v1Cell(f arrow.Field, a arrow.Array) (func(i int) any, error) {
	// style: doc comment + inline notes
	// because: one type switch like jsonCell, with three lines a reader asks about
	//
	// at the timestamp case: converted to UTC, whatever zone the process runs in [Rationale 6]
	// at the float64 case: encoding/json cannot write NaN or an infinity, so they are null [Rationale 8]
	// at the default: encoding.JSONCell writes the cell, so a DuckDB type reads as on v2 [Rationale 7]
}

// readV1Tables reads an Arrow IPC stream into the old server's body: no
// table for no rows, and otherwise one table without a name holding every
// batch's rows. A column's type is its word when a cell is not null, and
// none when every cell is. [Verified 3-4] [Rationale 5]
func readV1Tables(r io.Reader) (v1Tables, error) {
	// style: doc comment + overview + inline notes
	// because: the columns are built across batches and the empty answer is decided after the last one
	//
	// 1. open the stream and take the columns from its schema
	// 2. append every batch's cells to the columns, noting each column that holds a value
	// 3. answer no table for no rows, else one table with each column's word
	//
	// 1. at the schema: a stream of no batches still carries its columns [Verified 11]
	// 2. at the append: the cells are copied into Go values, because the reader releases each batch at Next [internal/AGENTS.md, Code]
	// 3. at rows == 0: the old server answered no table for no rows, and a statement without a result set has none [Verified 3]
	// 3. at the word: one non-null cell is enough, because an Arrow column has one type [Verified 4]
}
```

The whole result is held before the body is written, because the old
body lists every cell of a column before the next column. That is a fact
for the v1 `AGENTS.md`, and no line of `readV1Tables` restates it.

### `internal/httpapi/v1/token.go` (new)

```go
// unauthenticated is the old server's answer when a request carries no
// token at all. [Verified 7]
const unauthenticated = "unauthenticated for invalid credentials"

// v1Tokens answers the tokens a request presents, in the order the old
// server tried them: the Authorization header, without one leading
// "Bearer ", then ?token=. An empty value is no token. [Verified 7-8]
func v1Tokens(r *http.Request) []string {
	// style: doc comment + inline notes
	// because: two lookups with one line a reader asks about
	//
	// at strings.CutPrefix: the prefix is case-sensitive and optional, because the old server stripped it and kept a bare header whole [Verified 8]
}
```

### `internal/httpapi/v1/query.go` (new)

```go
// v1Query is the old server's query body. [Verified 10]
type v1Query struct {
	Query string `json:"query"`
}

// v2QueryPath and v2FindPath are the v2 endpoints the v1 query calls.
const (
	v2QueryPath = "/api/v2/query"
	v2FindPath  = "/api/v2/find"
)

// callV2 posts the text body to the v2 path under the token, asking for
// the media type, and answers the recorded response. [Verified 14]
func callV2(ctx context.Context, v2 http.Handler, path, token, accept string, body []byte) *httptest.ResponseRecorder {
	// style: doc comment + inline notes
	// because: one request built and served, with two lines a reader asks about
	//
	// at the context: the inner request carries the outer context, because the v2 handler reads the logger, the cluster and the keychain from it [Verified 14]
	// at the headers: no Accept-Encoding, so the v2 answer comes back uncompressed [internal/httpapi/v1/login.go:37-41]
}

// callAuthorized calls the v2 path under each token in turn and answers
// the first response that is not 401, or the last 401. [Verified 7] [Rationale 4]
func callAuthorized(ctx context.Context, v2 http.Handler, tokens []string, path, accept string, body []byte) *httptest.ResponseRecorder {
	// style: doc comment + inline notes
	// because: a loop with one line a reader asks about
	//
	// at the 401 check: a 401 answers before the cluster runs anything, so the next token runs the query once [Verified 11]
}

// writeV2Failure answers a v2 failure in the old server's shape: 401 as
// {"code","message"}, 400 and 413 as 400 {"message"}, and anything else
// as 500 {"message"}. The message is the problem's detail. [Verified 6, 9] [Rationale 9]
func writeV2Failure(w http.ResponseWriter, rec *httptest.ResponseRecorder) int {
	// style: doc comment + inline notes
	// because: one switch on the status with two lines a reader asks about
	//
	// at 413: the old server had no cap, so an oversized query is the caller's 400 [Rationale 9]
	// at the default: 503 is 500, because the old server answered 500 when it could not lease a session [Verified 6] [Rationale 9]
}

// queryHandler answers the old server's query through the v2 query, or
// through the v2 find for a query that begins with find. It answers the
// old {"tables"} body, and every failure in the old server's shapes.
// route is the path the request duration is recorded under. [Verified 1-10]
func queryHandler(v2 http.Handler, route string) http.Handler {
	// style: doc comment + overview + inline notes
	// because: the token is checked before the body, as the old server did, and the v2 call depends on the prefix
	//
	// 1. take the tokens, and answer 401 when there are none
	// 2. read the body and decode {"query"}
	// 3. call the v2 find for a find query, or the v2 query for Arrow
	// 4. convert the answer into the old body, or the failure into the old shapes
	// 5. record the duration under the v1 route
	//
	// 1. the old server authenticated before it bound the body [Verified 9]
	// 2. at the read: the body is read under any Content-Type, as the v1 login reads it [Rationale 10]
	// 2. at an empty body: 422 with code 602, the old server's required-body error [Verified 10]
	// 2. at a decode failure: 400 with code 400, the old server's parse error, and a bare string is one [Verified 10]
	// 3. at strings.HasPrefix: raw and case-sensitive, so leading whitespace or FIND reaches the native engine [Verified 1]
	// 4. at a find: one table per name with columns null [Verified 2]
	// 4. at readV1Tables' error: a type no cell writer knows is 500, as v2 answers it [Verified 11]
	// 5. the v2 route records under its own path, so one v1 request counts once under each [internal/httpapi/v1/AGENTS.md]
}
```

### `internal/httpapi/v1/v1.go`

`NewHandler` registers `queryHandler(v2, "/api/v1/query")` at
`POST /api/v1/query` and `queryHandler(v2, "/api/query")` at
`POST /api/query`, beside the login. The existing comments stay true.

### `internal/httpapi/v1/query_test.go` (new)

```go
// TestQuery: on both clusters, at both spellings and under each way the
// old server took a token, a generated table queried through v1 answers
// the old body: no table for no rows, else one table without a name, the
// columns in order with their old words, none for an all-null column, and
// cells that read back as the rows written. [Verified 3-5, 16]
func TestQuery(t *testing.T) {
	// style: doc comment + inline notes
	// because: a property over generated tables, as the round trip
	//
	// at the token draw: Bearer header, bare header and ?token= are the three forms the old server took [Verified 7-8]
	// at the body prefix: the bytes begin {"tables":[{"columns":[{"data": so the key order and the dropped name are pinned [Verified 3-4]
	// at the read-back: readOldBody turns the columns into a batch of the table's schema and table.Check compares it [Verified 16]
	// at the timestamp: every timestamp text ends in Z [Rationale 6]
}

// TestQueryErrors: each row answers its status and its body shape, at both
// spellings. [Verified 6-10]
func TestQueryErrors(t *testing.T) {
	// style: doc comment + inline notes
	// because: one table of rows, as TestErrorRows
	//
	// rows: no token -> 401 {"code","message":unauthenticated}; bad token -> 401 {"code","message"};
	//       bad header and good ?token= -> 200; empty body -> 422 code 602; bare string -> 400 code 400;
	//       undecodable -> 400 code 400; invalid query -> 400 {"message"}; unknown table -> 400 {"message"};
	//       #sql SELECT [1] -> 500 {"message"}
}

// TestQueryUnreachable: a cluster that does not answer is 500 {"message"}
// at both spellings. [Verified 6] [Rationale 9]
func TestQueryUnreachable(t *testing.T) {
	// style: doc comment + inline notes
	// because: as TestLoginUnreachable
	//
	// at the token: minted from the keychain, because a login on this cluster fails
}

// TestQuerySQL: on both clusters, one #sql row of every DuckDB scalar
// answers each column under its word, the old word for the old five types
// and the Arrow name for the rest, and each cell as the v2 JSON writes it. [Rationale 7]
func TestQuerySQL(t *testing.T) {
	// style: doc comment + inline notes
	// because: one table of expressions, as TestDuckDBScalars
	//
	// at the expected cells: copied from TestDuckDBScalars, because the cell is the v2 JSON's
}

// TestFind: on both clusters, a query that begins with find answers one
// table per entry the expression matches, with columns null, and no table
// for no match. Leading whitespace and FIND reach the native engine, which
// answers 400. [Verified 1-2]
func TestFind(t *testing.T) {
	// style: doc comment + inline notes
	// because: a few fixed rows over one tagged table
}
```

`readOldBody` is the test helper that turns the old body back into a
batch of the table's schema: `int64` from `json.Number`, `double` from
the number, `string` and `symbol` from the string, `blob` from base64,
`timestamp` from RFC 3339.

### `internal/httpapi/AGENTS.md`, Handlers

```
- The find route is `POST /api/v2/find`, a find expression as a text body
  under the query's content types and cap, answering `{"entries":[...]}`
  through `Cluster.Find` in the cluster's order. No match is 200 with an
  empty list, and anything the cluster answered is 400 through
  `writeClusterError`. The v1 `find` wraps it.
```

The round trip's description under Tests names the find with the tags.

### `internal/encoding/AGENTS.md`, The text formats

```
- `JSONCell` lends one column's JSON appender to the v1 query, which
  writes a DuckDB type as the JSON encoder does.
```

### `internal/httpapi/v1/AGENTS.md`

```
- The query reads `{"query"}` under any Content-Type, after the token.
  No token is 401 `{"code","message"}`, an empty body 422 code 602, and a
  body that does not decode 400 code 400, as go-openapi answered.
- The token is the `Authorization` header without one optional,
  case-sensitive `Bearer `, then `?token=`, each forwarded as the v2
  bearer, the first that is not 401 winning.
- A query asks the v2 query for Arrow and holds the whole result before
  it writes, because the old body lists every cell of a column before
  the next column. It answers no table for no rows, else one table
  without a name. The five old types keep their words and their
  `encoding/json` cells, timestamps in UTC, NaN and infinities null.
  Any other type answers its Arrow name and its v2 JSON cell.
- A query that begins with `find` calls `POST /api/v2/find` and answers
  one table per name with `"columns":null`.
- A v2 401 is 401 `{"code","message"}`, a 400 or 413 is 400
  `{"message"}`, and anything else, 503 included, is 500 `{"message"}`.
```

### `docs/brief.md`

```
- endpoint sketch: POST /api/v2/find   find expression -> {"entries":[...]}, the names it matches; M9
- the find paragraph: the find core serves the tag routes and POST /api/v2/find, and the v1 find wraps the route (owner, 2026-11-06)
- M8: the tag routes over a find core in internal/qdb (the v1 clause goes)
- Compatibility contract, POST /api/v1/query: timestamps are written in UTC; NaN and the infinities are null
- Deliberate deviations: two rows, timestamps in UTC where the old server wrote the process's zone, and NaN and the infinities as null where the old server's encoder failed
```

## Rationale

| decision                                                               | why                                                                             | rejected, and why                                                                                                                                                                             | gained                                                        | given up                                                           | settled by                              |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------ | --------------------------------------- |
| 1. A v2 find route that v1 wraps                                       | v1 wraps a v2 endpoint and never calls `internal/qdb` (ADR-0007)                | v1 calls `Cluster.Find`, an exception to ADR-0007; accepting only `find(tag='x')`, which breaks compound expressions; keeping "no v2 route takes a find expression", which the owner reversed | ADR-0007 holds with no exception, and v2 gains a general find | a new v2 route to maintain                                         | owner, 2026-11-06                       |
| 2. No match is 200 with an empty list                                  | a free expression names no resource whose absence a 404 reports                 | 404 as `GET /api/v2/tags/{tag}` answers                                                                                                                                                       | the old find's `{"tables":[]}` maps directly                  | the find route and the tag route answer no match differently       | proposal                                |
| 3. The find body is query text under the query's cap                   | a find expression is a line of text, as a query is                              | a JSON body `{"expression"}`, which v1 would build and v2 would parse                                                                                                                         | one reader, one cap, one 415                                  | none                                                               | proposal                                |
| 4. The token is forwarded as the v2 bearer, each in the old order      | v1 does not verify tokens (ADR-0007)                                            | verifying in v1 through `auth.Tokens`, which ADR-0007 forbids                                                                                                                                 | the old precedence, with one v2 verifier                      | a second inner call when the header fails and `?token=` is present | proposal                                |
| 5. v1 asks v2 for Arrow and reads the batches                          | typed arrays give the null typing and the old cell values directly              | re-parsing the v2 JSON, which parses every number and timestamp twice                                                                                                                         | exact types, one parse                                        | v1 imports arrow-go                                                | owner, 2026-11-06                       |
| 6. Timestamps in UTC                                                   | the owner wants v1 to convert, whatever zone the process runs in                | the process's zone, as the old server wrote it                                                                                                                                                | one text on every host                                        | a deviation where the old server ran outside UTC                   | owner, 2026-11-06                       |
| 7. A type outside the six answers its Arrow word and its v2 cell       | only DuckDB answers such a type, and no deployment sends `#sql` yet             | widening into the six words, which loses types; refusing the column, which fails queries that work on v2                                                                                      | `#sql` works on v1 with every scalar                          | a word an old client does not know, never reached by one           | owner, 2026-11-06                       |
| 8. NaN and the infinities are null                                     | `encoding/json` cannot write them, so the old body broke                        | an error, which fails a query that works on v2                                                                                                                                                | a valid body                                                  | a deviation                                                        | proposal                                |
| 9. 503 is 500, 413 is 400                                              | the old server answered 500 when it could not lease a session, and had no cap   | passing 503 and `Retry-After` through                                                                                                                                                         | the old status                                                | `Retry-After` on v1                                                | owner, 2026-11-06 (503); proposal (413) |
| 10. The body is read under any Content-Type                            | the v1 login reads it so (owner, 2026-11-05)                                    | the old server's JSON consumer and its 415                                                                                                                                                    | one rule for both v1 routes                                   | none, because the Grafana plugin sends JSON                        | proposal                                |
| 11. The v1 tests check v1 against the generated rows, never against v2 | each version is tested on its own, and the old server is compared by the script | a v1 step in `TestRoundtrip`; v1 compared with the v2 answer                                                                                                                                  | independent tests                                             | none                                                               | owner, 2026-11-06                       |
| 12. Error messages are not byte-matched                                | out of scope for the comparison                                                 | matching the old `err + results.ErrorMessage()`                                                                                                                                               | no message mapping                                            | error bodies are compared by status and shape                      | owner, 2026-11-06                       |
| 13. No memory cap on a v1 result                                       | the owner wants none                                                            | a cap with a 413 or 500                                                                                                                                                                       | the old behaviour                                             | a large v1 query holds its whole result                            | owner, 2026-11-06                       |

## Knowledge

- Commit 2: `internal/qdb/tag.go` `Tagged` (Rationale 1),
  `internal/httpapi/find.go` `handleFind` (Verified 12, Rationale 2-3),
  `entriesResponse` comment.
- Commit 3: the find rows' heading in `errors_test.go`.
- Commit 4: step 7's comment and the file header in `roundtrip_test.go`
  (Rationale 2).
- Commit 5: `internal/httpapi/AGENTS.md`, Handlers and Tests (Rationale 1-3).
- Commit 6: `encoding.JSONCell` and `internal/encoding/AGENTS.md` (Rationale 7).
- Commit 7: `v1/tables.go` (Verified 2-5, Rationale 5-8).
- Commit 8: `v1/token.go`, `v1AuthError` (Verified 7-9).
- Commit 9: `v1/query.go`, `v1/v1.go` (Verified 1, 6, 9-11, Rationale 4, 9, 10).
- Commits 10-14: the test comments (Verified 1-16, Rationale 6-7, 11).
- Commit 15: `internal/httpapi/v1/AGENTS.md` (Rationale 4-10, 13).
- Commit 16: `docs/brief.md` (Rationale 1, 6, 8).
- Commit 19: `docs/log.md`, Current state: Next drops the v1 query, and
  M9 exit criterion 2 compares error bodies by status and shape
  (Rationale 12).
- Plan only: Rationale 11 (the conversation's settling of where the v1
  tests live; `internal/httpapi/v1/AGENTS.md` already says one test per
  route) and the rejected alternatives of Rationale 4 and 5 that no
  reader of those lines would propose.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message.

- Commit 7 checks first that a native query with no rows and a statement
  without a result set reach `readV1Tables` as a stream with zero rows,
  so the `rows == 0` comment holds for both.
- Commit 10 checks that `encoding/json` writes a `time.Time` in UTC with
  the `Z` suffix and trailing zeros dropped, which the test's timestamp
  claim rests on.

## Commits

1. `docs(plan): v1-query-plan.md, the v1 query over the v2 query and a v2 find route`
2. `feat(httpapi): POST /api/v2/find answers the entries a find expression matches`
3. `test(httpapi): the error rows cover the find route`
4. `test(httpapi): the round trip finds each tagged table through the find route`
5. `docs(agents): the httpapi agents file names the find route`
6. `feat(encoding): JSONCell lends a column's json cell to the v1 query`
7. `feat(v1): readV1Tables writes arrow batches as the old server's tables and type words`
8. `feat(v1): the v1 token comes from the authorization header or ?token= in the old order`
9. `feat(v1): POST /api/v1/query and /api/query answer the v2 query as the old tables body`
10. `test(v1): the query rows on both clusters, both spellings and every token form`
11. `test(v1): the query error rows and the unreachable cluster`
12. `test(v1): a #sql row of the duckdb scalars answers old words and arrow words`
13. `feat(v1): a query beginning with find answers the v2 find as tables without columns`
14. `test(v1): the find rows, leading whitespace and case included`
15. `docs(agents): the v1 agents file holds the query, the token and the find`
16. `docs(brief): the sketch lists POST /api/v2/find, and the deviations list utc timestamps and null nan`
17. `/doc-discipline all internal/httpapi internal/encoding internal/qdb`,
    with any fixes as `docs(...)` commits.
18. `/doc-discipline check docs/v1-query-plan.md`, with any findings
    fixed as `docs(...)` commits.
19. `docs(log): v1-query-plan.md deleted, facts moved to internal/httpapi/v1/AGENTS.md, internal/httpapi/AGENTS.md, internal/encoding/AGENTS.md, docs/brief.md and the comments of internal/httpapi, internal/httpapi/v1, internal/encoding and internal/qdb`
20. Verify: push `sc-19567/rr-v1-query`, build its head in Buildkite,
    wait for the result. Green: report the build number. Red: fix with
    further small commits on this branch, push, build again.

## Open questions and recommendations

1. A bad token with a bad body answers the body's 400 or 422 only when
   the request carries no token at all; with a presented token, v1 reads
   the body first and answers its error, where the old server answered 401. Recommendation: accept it, and the comparison leaves the case
   out, because probing the token through `GET /api/v2/session` first
   costs an inner call on every failed body.
2. `POST /api/v2/find` lands in M9 although M8 owned exploration.
   Recommendation: the sketch marks it M9, because the v1 find is what
   needs it.
3. The `finder` row of the brief has no test, because a `finder` query
   answers 400 on both engines and only the message tells them apart.
   Recommendation: no test, and leading whitespace and `FIND` cover the
   routing.
