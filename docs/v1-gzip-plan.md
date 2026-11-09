# Plan: the v1 routes compress as v2 does, and the comparison asks for gzip

Thread: v1 gzip
Status: approved

## Outcome

When the unit lands, `POST /api/v1/login` and `POST /api/v1/query`, at
both spellings, answer a gzip or zstd body whenever the request's
`Accept-Encoding` names one, by the rule every v2 data route follows:
the client's order, case-insensitive, `q` unread, identity when nothing
matches. The compressing writer of `internal/httpapi` is exported as
`WithCompression`, and nothing else about it changes. The brief's
compatibility contract says so, and its deviations table has one row
for the places where this rule and the old server's substring test
answer differently. The by-hand comparison sends one query under
`Accept-Encoding: gzip`, asserts that both servers label the answer
gzip, and compares the decoded bodies as bytes.

Left for later units: the wildcard `*` in `Accept-Encoding`, which the
negotiation reads as identity, and a server preference for zstd over
gzip, both v2 rules outside this unit; the HTTPS steps of the round
trip, which the M9 handoff leaves open.

## Verified facts

- The old server gzipped every `/api` answer when the request's
  `Accept-Encoding` contained the bytes `gzip` anywhere, case-sensitive,
  with `gzip.NewWriter` at the default level, the header set before the
  handler ran, so error bodies were gzipped too, and it knew no zstd
  (`tests/e2e/old-server/restapi/configure_qdb_api_rest.go:843-852`,
  `HTTPSwitchMiddleWare`, applied to every route at `:867`).
- v2 negotiates in the client's order, the first of gzip, zstd and
  identity wins, case-insensitive, `q` unread, `*` is identity
  (`internal/httpapi/compress.go:21-38`, `compress_test.go:50-70`).
- The writer holds the status back until the first body byte, then sets
  `Content-Encoding`, drops `Content-Length` and opens the compressor;
  it adds `Vary: Accept-Encoding` to every answer of the route
  (`internal/httpapi/compress.go:62-131`).
- `withCompression` is unexported and has sixteen call sites, all in
  `internal/httpapi` (`query.go:204`, `login.go:249-252`,
  `tables.go:248-251`, `tags.go:78-80`, `find.go:45`, `read.go:131`,
  `cluster.go:81-82`, `rows.go:168`, and `compress_test.go:78`).
- The v1 routes write their bodies through `writeV1JSON`, which calls
  `WriteHeader` and then encodes (`internal/httpapi/v1/json.go:22-29`),
  so the held-back status of the writer applies to them unchanged.
- The inner v2 request of each v1 route carries no `Accept-Encoding`
  (`internal/httpapi/v1/login.go:43-44`, `query.go:40`), and the v1
  query decodes the v2 Arrow answer before it writes
  (`internal/httpapi/v1/query.go:155-178`), so v1 compresses only its
  own output.
- The v1 handlers record their own duration and are registered directly
  on the v1 mux, not through `handleData`
  (`internal/httpapi/v1/v1.go:12-24`, `login.go:75-76`,
  `query.go:180-183`).
- The request logger wraps the composed router once and counts the
  bytes it sees, which are wire bytes when a compressor sits inside it
  (`internal/httpapi/middleware.go:69-86`, `cmd/qdb_rest/main.go:298`).
- The v1 tests pin `Content-Type: application/json` and no
  `WWW-Authenticate` on a 401 and nothing about `Content-Encoding` or
  `Vary` (`internal/httpapi/v1/login_test.go:64-74`). The `decompress`
  helper of the httpapi tests is package-private
  (`internal/httpapi/compress_test.go:23`).
- The comparison's `request` helper writes `<name>.body` and
  `<name>.headers` per request (`tests/e2e/common.sh:201-205`), and
  `compare` compares a 200 as bytes (`tests/e2e/compare-v1.sh:68-94`).
  `curl --compressed` sends `Accept-Encoding` with the codings curl was
  built with, gzip among them, and decodes the body before writing it.
- The brief's contract lists no compression rule for v1, and says v1
  answers as the old server's code does except the listed deviations,
  the code winning over the text (`docs/brief.md:244-250`, owner,
  2026-11-05). The deviations table is at `docs/brief.md:304-319`.
- ADR-0012's consequences say "M4's v1 wart (substring `gzip`, every
  route) exports what it needs of the writer when it exists"
  (`docs/adr/0012-v2-response-compression.md:43-45`); the owner chose
  the v2 rule for v1 instead on 2026-11-07.
- `docs/e2e.md:464-466` says the gzip comparison waits for this unit and
  is one more query line then.

## Design

### `internal/httpapi/compress.go`

```go
// WithCompression compresses one route's answers in the coding the
// client's Accept-Encoding names, problem bodies included, and marks
// every answer of the route with Vary: Accept-Encoding. [compress.go:113-131]
// It wraps a route, never the mux, because the probes answer empty
// bodies. [compress.go:117-118]
func WithCompression(next http.Handler) http.Handler {
	// style: doc comment + inline notes
	// because: the body is unchanged, one line asks why the close error is dropped
	//
	// at the deferred Close: a close error has no one left to tell, because the
	//   handler has returned and the status is on the wire [compress.go:126-128]
}
```

The rename touches the doc comment, the sixteen call sites and the
test's call. The body and the inline comment stay as they are.

### `internal/httpapi/v1/v1.go`

```go
// NewHandler builds the v1 router over the v2 router. It serves each v1
// route at both spellings, compressed as a v2 route is, and hands every
// other request to v2. [v1.go:10-24]
func NewHandler(v2 http.Handler) http.Handler {
	// style: doc comment + inline notes
	// because: two lines a reader would ask about, the double registration and the wrap order
	//
	// at the registrations: each spelling records under its own path, so the
	//   metrics show which spelling clients send [v1.go:14-15]
	// at the wrap: the compressor sits outside the handler, so the old JSON
	//   body is written plain and the writer labels and compresses it, and the
	//   duration the handler records includes the compression [json.go:22-29,
	//   Rationale: v1 compresses its own body]
	// at mux.Handle("/", v2): the v2 router keeps its own 404 and 405 for every
	//   path that is not a v1 route [v1.go:20-22]
	mux.Handle("POST /api/v1/login", httpapi.WithCompression(loginHandler(v2, "/api/v1/login")))
	// ... the other three the same way
}
```

### `internal/httpapi/v1/login.go`, `query.go`

No code changes. The step-1 comment of `loginHandler`
(`login.go:41-45`) and the comment at `callV2` (`query.go:40`) are
rewritten whole from the code, each keeping one claim: the inner
request carries no `Accept-Encoding`, because the v1 route decodes the
v2 answer and compresses only the body it writes itself [Rationale: v1
compresses its own body].

### `internal/httpapi/v1/login_test.go`

```go
// decoded answers the response body decoded by the coding its
// Content-Encoding names, with the standard library's gzip reader and
// klauspost's zstd reader. [compress_test.go:23-47 is package-private]
func decoded(t *testing.T, resp *httptest.ResponseRecorder) []byte {
	// style: doc comment alone
	// because: a switch on three codings, each line says what it does
}

// TestLoginCoding: at both spellings and on both clusters, a login under
// Accept-Encoding gzip and under zstd answers a body labelled with that
// coding that decodes to the plain login's bytes, and a 401 is labelled
// and compressed the same way. [Rationale: the v2 rule on v1]
func TestLoginCoding(t *testing.T) {
	// style: doc comment + inline notes
	// because: one loop over the codings, one line explains the failure row
	//
	// at the plain request: the plain body is the reference, because the
	//   token differs per login and the shape, not the bytes, is compared
	//   [login_test.go:98-101, the token is minted per login]
	// at the 401 row: an error body compresses like a result, as v2's
	//   problem bodies do [compress_test.go:84-104]
}
```

The plain and the compressed login mint different tokens, so the test
compares the decoded body's keys through `keysOf`, as `TestLogin` does,
not the bytes.

### `internal/httpapi/v1/query_test.go`

```go
// TestQueryCoding: at both spellings and on both clusters, the select
// star under Accept-Encoding gzip and under zstd answers a body labelled
// with that coding whose decoded bytes equal the plain answer's, the
// client's order picks the coding, and a 400 is labelled and compressed
// the same way. [Rationale: the v2 rule on v1]
func TestQueryCoding(t *testing.T) {
	// style: doc comment + inline notes
	// because: rows over a table the fixture draws, one line per row that needs a why
	//
	// at "zstd, gzip": the client's order decides, so zstd wins where the old
	//   server would have answered gzip [compress.go:28-37, brief deviations row]
	// at "x-gzip": not a coding this server produces, so the body is plain where
	//   the old server gzipped [compress_test.go:67, brief deviations row]
	// at the 400 row: an error body compresses like a result [compress_test.go:84-104]
}
```

The rows are `gzip`, `zstd`, `zstd, gzip`, `GZIP`, `x-gzip` and the
invalid query under `gzip`, each at both spellings. The plain answer of
the same query is the reference, because the body is deterministic for
one table.

### `docs/brief.md`, Compatibility contract

Under "POST /api/v1/query", after the `#sql` bullet, and under "POST
/api/v1/login", after the 401 bullet, one bullet each:

```
- Both endpoints compress as the v2 routes do: Accept-Encoding is read in
  the client's order, the first of gzip, zstd and identity wins, case-
  insensitive, q unread, and nothing is forced (ADR-0012). The old server
  gzipped when the header contained the bytes gzip anywhere and knew no
  zstd (owner, 2026-11-07). [Rationale: the v2 rule on v1]
```

Written once, under the query, with the login bullet pointing at it.
One row in the deviations table:

```
| Accept-Encoding is negotiated as v2 does, zstd included, where the old
  server gzipped on the substring gzip | POST /api/v1/query, above |
```

### `docs/adr/0012-v2-response-compression.md`, Consequences

The second bullet is rewritten from the tree: the v1 routes wrap the
same exported writer, so one rule reads every client's
`Accept-Encoding` on both surfaces (ADR-0007). No mention of the
substring wart, which the tree does not have.

### `internal/httpapi/AGENTS.md`, Middleware

The first bullet names `WithCompression` as exported, with
`WithRequestLogging`, and says the v1 package wraps its routes in it.

### `internal/httpapi/v1/AGENTS.md`

One bullet after the token bullet:

```
- Each route is wrapped in httpapi.WithCompression at registration, so a
  v1 answer compresses by the v2 rule (ADR-0012) and carries Vary. The
  handler writes the old body plain, and the inner v2 request carries no
  Accept-Encoding, because the route decodes the v2 answer and compresses
  only what it writes itself. The differences from the old server's
  substring test are a listed deviation (docs/brief.md, Deliberate
  deviations).
```

### `tests/e2e/compare-v1.sh`

```bash
# gzip_case <out> <old_url> <new_url>: the select star under
# Accept-Encoding gzip to the old server and to the new server at both
# spellings; each answer is labelled gzip, and the decoded bodies are
# compared as bytes through compare. [Rationale: decoded bytes]
gzip_case() {
    # style: doc comment + inline notes
    # because: the curl flag and the header grep each need their why
    #
    # at --compressed: curl names gzip and decodes the body, so compare sees
    #   plain bytes; the servers compress at different levels, so the wire
    #   bytes never match [old-server configure_qdb_api_rest.go:849 default
    #   level; compress.go:49 BestSpeed; Rationale: decoded bytes]
    # at the header grep: Content-Encoding: gzip in each headers file is the
    #   proof the answer was compressed, because the decoded body alone would
    #   pass against a plain answer [common.sh:201-205 writes the headers]
    # at the query: the select star, because it is the largest body and the
    #   one a gzip client sends [QUERIES[0]]
}
```

`run_cluster` calls `gzip_case` after the query loop. The grep is
case-insensitive on the header name, because Go writes
`Content-Encoding` and the old server's `net/http` does the same, and
the old server's value is `gzip` (`configure_qdb_api_rest.go:848`).

### `docs/e2e.md`, The v1 comparison

The sentence "A gzip comparison waits for the unit ... one more query
line then" is rewritten: one case sends the select star under
`Accept-Encoding: gzip` through curl `--compressed`, asserts the gzip
label on both servers and compares the decoded bytes, because the two
servers compress at different levels and the compressed bytes are no
part of the contract. The list of what the queries prove gains that
line.

## Rationale

| decision                                                                                                                           | why                                                                                                                                                                             | rejected, and why                                                                                                                               | gained                                       | given up                                                                                                              | settled by                                  |
| ---------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------- |
| the v2 rule on v1: v1 negotiates `Accept-Encoding` as v2 does, zstd included                                                       | reusing v2's mechanism is easier, and supporting zstd too breaks no v1 client                                                                                                   | the old server's case-sensitive substring test, gzip only: a second rule and a second writer for a difference no client sends                   | one rule and one writer on both surfaces     | three header spellings answer differently from the old server (`zstd, gzip`, `GZIP`, `x-gzip`), listed as a deviation | owner, 2026-11-07                           |
| the writer is exported from `internal/httpapi` as `WithCompression`                                                                | one compressor, one level, one labelling for both surfaces; the v1 package already imports `internal/httpapi` (ADR-0007)                                                        | a gzip writer local to the v1 package: fifteen lines, but a second place to change the level or the labelling                                   | no duplicate                                 | one more exported name in `internal/httpapi`                                                                          | owner, 2026-11-07                           |
| v1 compresses its own body: the compressor wraps the v1 handler, and the inner v2 request carries no `Accept-Encoding`             | the v1 query decodes the v2 Arrow answer into the old JSON, so a compressed v2 answer would be decompressed and recompressed                                                    | asking v2 for gzip and passing the bytes through: impossible for the query, which re-encodes, and pointless for the login, which re-encodes too | one compression per answer                   | nothing                                                                                                               | tree (`query.go:155-178`, `login.go:57-73`) |
| the deviation is one row and no comparison case exercises it                                                                       | the contract rule: every place v1 answers differently is listed, and the comparison exercises no listed deviation                                                               | leaving it unlisted: the contract would then be wrong for `zstd, gzip`                                                                          | the contract stays complete                  | nothing                                                                                                               | `docs/brief.md:304-308`                     |
| decoded bytes: the comparison's gzip case decodes with curl, asserts the label in both headers files, and compares the plain bytes | the old server compressed at gzip's default level and the new one at the fastest (ADR-0012, D4), so the compressed bytes differ by construction and are no part of the contract | matching the old server's level on v1 to compare wire bytes: a second level for one test, against ADR-0012 D4                                   | a case that proves the label and the content | the compressed stream itself is unproven, which no client reads                                                       | proposal                                    |
| `Vary: Accept-Encoding` on v1 answers                                                                                              | the writer sets it on every answer of a route it wraps, and no v1 client reads it                                                                                               | stripping it in the v1 package: code for a header the comparison does not compare                                                               | nothing to maintain                          | a header the old server did not send                                                                                  | proposal                                    |
| the v1 coding tests decode with the standard library and klauspost directly                                                        | the httpapi test helper is package-private, and exporting a test helper is not done in this tree                                                                                | an exported test helper in `internal/httpapi`                                                                                                   | no test-only export                          | one small decoder per test package                                                                                    | proposal                                    |

## Knowledge

- Commit 2, `refactor(httpapi)`: the doc comment of `WithCompression`
  (contract, two claims); the Middleware bullet of
  `internal/httpapi/AGENTS.md` names the export (landed in commit 7).
- Commit 3, `feat(v1)`: the inline note at the wrap in `v1.NewHandler`
  (why the compressor is outside the handler, from the Rationale row
  "v1 compresses its own body"); the rewritten step-1 comment in
  `loginHandler` and the comment at `callV2` (the inner request carries
  no `Accept-Encoding`, same row). The Rationale rows "the v2 rule on
  v1" and "the writer is exported" land in the brief (commit 5) and
  ADR-0012 (commit 6); their "why" lines are the owner's answers and
  are `plan only`.
- Commit 4, `test(v1)`: the inline notes of the two coding tests at the
  rows that differ from the old server, with the brief's deviations row
  as their home; the decoder helper's reason is the Rationale row "the
  v1 coding tests decode directly", `plan only`.
- Commit 5, `docs(brief)`: the contract bullet and the deviations row
  (routing table: scope and contract live in the brief).
- Commit 6, `docs(adr)`: the ADR-0012 consequence, edited in place
  because the tree proved its detail wrong (`docs/AGENTS.md`, ADRs).
- Commit 7, `docs(agents)`: the `internal/httpapi/AGENTS.md` Middleware
  bullet and the new `internal/httpapi/v1/AGENTS.md` bullet (what spans
  the files of a folder).
- Commit 8, `test(e2e)`: the three inline notes of `gzip_case`
  (Rationale row "decoded bytes"); the `Vary` row is `plan only`.
- Commit 9, `docs(e2e)`: the v1 comparison paragraph (the specification
  of a subsystem too large for an `AGENTS.md`).
- Commit 12, `docs(log)`: the deletion entry and `Last updated`.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row of the sections above in the same commit as the code and reconciles
the code against this plan before the build-stage message. Specific to
this unit: commit 8 is run by hand through `make test-v1-compare` once,
after `make test-round-trip` has seeded both clusters, before the
build-stage message, because Buildkite never runs the comparison; its
result is reported in the conversation and nowhere else.

## Commits

1. `docs(plan): v1-gzip-plan.md, the v1 routes compress as v2 does and the comparison asks for gzip`
2. `refactor(httpapi): WithCompression is exported, so the v1 router can wrap its routes in it`
3. `feat(v1): the login and the query answer the coding Accept-Encoding names, as v2 does`
4. `test(v1): the gzip and zstd rows on both routes, both spellings, a 200 and a failure`
5. `docs(brief): v1 negotiates Accept-Encoding as v2 does, and the old substring test is a listed deviation`
6. `docs(adr): ADR-0012's consequence names the exported writer the v1 routes wrap`
7. `docs(agents): the httpapi and v1 agents files say the v1 routes wrap WithCompression`
8. `test(e2e): compare-v1.sh sends the select star under gzip, asserts the coding on both servers and compares the decoded bytes`
9. `docs(e2e): the v1 comparison's gzip line`
10. `/doc-discipline all internal/httpapi internal/httpapi/v1 tests/e2e docs/brief.md docs/e2e.md docs/adr/0012-v2-response-compression.md`, committed as `docs(...)` commits where it changes anything.
11. `/doc-discipline check docs/v1-gzip-plan.md internal/httpapi internal/httpapi/v1 tests/e2e`, findings fixed before the next commit.
12. `docs(log): v1-gzip-plan.md deleted, facts moved to docs/brief.md, ADR-0012, internal/httpapi/AGENTS.md, internal/httpapi/v1/AGENTS.md, docs/e2e.md and the comments of internal/httpapi, internal/httpapi/v1 and tests/e2e`
13. Verify: push `sc-19567/rr-v1-gzip`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Whether `*` in `Accept-Encoding` should answer a coding, and whether
   the server should prefer zstd when the client lists both. Both are
   v2 rules. Recommendation: leave them as ADR-0012 fixes them, and
   open a separate unit only if a client asks.
