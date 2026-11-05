# v1 login over the v2 login endpoint

Thread: M9 v1 drop-in compat
Status: approved

## Outcome

When this unit lands:

- `POST /api/v1/login` and its alias `POST /api/login` are served by a
  new package, `internal/httpapi/v1`. The handler takes the old
  server's body, calls the v2 login endpoint in process, and answers
  `{"token": <the v2 access token>}` on success. Every failure is
  `401 {"message": ...}`.
- The v2 package has one builder, `httpapi.NewHandler`, which returns
  the v2 router without the request logging. The v1 package has one
  builder, `v1.NewHandler(v2)`, which serves the v1 routes and hands
  every other request to the v2 router. The entry point nests the two
  and wraps the result once in `httpapi.WithRequestLogging`.
- M9 is in progress in `docs/log.md` with its three exit criteria.
- The brief, ADR-0007, ADR-0008, ADR-0015 and `docs/e2e.md` describe
  the v1 surface as the owner settled it on 2026-11-05. Each v1 route
  wraps the v2 endpoint. The v1 token is the v2 access token. The v1
  query keeps the old server's key order and 401 body, and accepts
  `#sql`. Compatibility is checked by a script run by hand that
  compares the live old server and the new one side by side.

Left for later units, each named in the log's Next:

- the v1 query: `POST /api/v1/query` and `/api/query`, the bearer
  header or `?token=`, the conversion from the v2 answer, the `find`
  prefix, the error rows and a round-trip step;
- the side-by-side compatibility script, run by hand, against the old
  server built from `master`.

## Verified facts

1. `NewHandler` builds a private mux, registers every v2 route and the
   probes, and returns `withRequestLogging(mux)`
   (`internal/httpapi/httpapi.go:104-120`).
2. `withRequestLogging` sets `X-Request-Id`, tags the context with the
   request id and `observe.PlaneHTTP`, and writes one `request` access
   line (`internal/httpapi/middleware.go:69-86`). Only
   `middleware_test.go:27` calls it directly. The handler tests call
   `NewHandler` and place the plane themselves through `observeContext`
   (`internal/httpapi/readiness_test.go:39-44`), so they do not depend
   on the wrap.
3. The entry point calls `httpapi.NewHandler(cfg.Observability)` once
   (`cmd/qdb_rest/main.go:295`), and nothing else imports
   `internal/httpapi`.
4. The v2 login takes `{"username","secret_key"}` under
   `Content-Type: application/json` (415 otherwise). It reads the body
   through `readBody` under its cap (`maxBodyBytes`). It answers 200 with
   `access_token`, `token_type`, `expires_in` and `refresh_token`, and
   every failure as an RFC 9457 problem whose `detail` names the cause
   (`internal/httpapi/login.go:72-110`, `internal/httpapi/problem.go:20-31`).
   A refusal is 401, and an unreachable cluster or an open breaker is
   503 (`internal/httpapi/query.go:110-138`).
5. The v2 login is registered through `handleData`, which records the
   request duration under `POST /api/v2/auth/login`, and wrapped in
   `withCompression` (`internal/httpapi/login.go:249`,
   `internal/httpapi/middleware.go:89-105`). A request without
   `Accept-Encoding` gets identity.
6. `observe.MetricsFrom(ctx).ObserveRequest(method, route, code, d)` is
   exported (`internal/observe/metrics.go:110-111`).
7. `auth.Tokens.Verify` checks a token's own `exp` and nothing about
   the configured lifetimes (`internal/auth/token.go:270-272`). The
   bearer edge requires `typ` `access` (`internal/httpapi/bearer.go:78`).
   So the v2 access token opens every v2 data route as a v1 token.
8. The old server's login answered `{"token": ...}` on 200 and
   `401 {"message": ...}` when it could not open the user's pool,
   unreachable cluster included
   (`master:restapi/configure_qdb_api_rest.go:276-306`,
   `master:models/token.go`, `master:models/qdb_error.go:21`). Its JSON
   producer is `json.NewEncoder` with `SetEscapeHTML(false)`, which
   ends each body with a newline
   (`master:vendor/github.com/go-openapi/runtime/json.go:32-35`). The
   golden captured from it carried `content-type: application/json` and
   no other header (`2ba2b63^:tests/e2e/golden/v1/01-login-anonymous/headers`).
9. The old server's query bodies key a column as `data`, `name`,
   `type`, omit an empty table name, and answer a find as
   `"columns":null` (`master:models/query_column.go:21-27`,
   `master:models/query_table.go:23-26`,
   `2ba2b63^:tests/e2e/golden/v1/08-query-seed-select/body`,
   `2ba2b63^:tests/e2e/golden/v1/12-query-find/body`). Its token
   failures answer `{"code":401,"message":...}`
   (`2ba2b63^:tests/e2e/golden/v1/03-query-unauthenticated/body`,
   `04-query-bad-token/body`). The brief's v1 query section disagrees
   on both, and the owner ruled that the code wins (owner, 2026-11-05).
10. Under Go's pattern mux, `GET /api/v1/login` matches the `/`
    pattern that hands over to the v2 router, so it answers the v2
    mux's 404 rather than 405. This follows from the stdlib's
    most-specific-match rule and the `/` registration.
11. The test fixture binds each cluster with its user: anonymous on the
    insecure cluster, the test user on the secure one
    (`internal/qdbtest/cluster/cluster.go:21-44`, `internal/AGENTS.md`,
    Tests).

## Design

### `internal/httpapi/middleware.go`

```go
// WithRequestLogging puts the request id on the request context and on
// the response, tags the context with the HTTP plane, and logs one
// access line when the handler returns. [middleware.go:69-71]
// The entry point wraps the composed router in it once. [Rationale 1]
func WithRequestLogging(next http.Handler) http.Handler {
	// style: doc comment alone
	// because: the body is unchanged and every line is evident; only the
	// name and the doc comment change.
}
```

`middleware_test.go` calls `WithRequestLogging`. Its comment at line 16
is rewritten to the new name.

### `internal/httpapi/httpapi.go`

The package comment is rewritten whole: it serves the v2 resource API,
the bearer middleware, the operational routes, and the request logging
that the entry point wraps around the composed router. The v1 package
wraps this one, and this package does not import it.

```go
// NewHandler builds the v2 router: the v2 routes, the probes, and
// /metrics and /debug/pprof/ when the observability block switches them
// on. [httpapi.go:100-103]
// It writes no access line, because the entry point wraps the composed
// router in WithRequestLogging. [Rationale 1]
func NewHandler(cfg config.Observability) http.Handler {
	// style: doc comment + inline notes
	// because: the two existing inline notes stay, and the last line
	// changes from the wrapped mux to the mux.
	//
	// at the parameter: configuration does not travel in the context. [httpapi.go:105-106]
	// at the first registrations: the unauthenticated routes sit together. [httpapi.go:108]
	// return mux
}
```

### `internal/httpapi/v1/v1.go` (new)

```go
// Package v1 serves the old server's login and query at /api/v1/<path>
// and at their unversioned aliases. [ADR-0008, Decisions 1-2]
// Each route calls its v2 endpoint in process and converts the answer
// into the old server's wire shape. [ADR-0007, Decision 2; Rationale 2]
// It is the only package that knows that shape. [ADR-0007, Decision 3]
package v1
```

```go
// NewHandler builds the v1 router over the v2 router. It serves each v1
// route at both spellings and hands every other request to v2.
// [ADR-0008, Decision 2; Rationale 3]
func NewHandler(v2 http.Handler) http.Handler {
	// style: doc comment + inline notes
	// because: one registration line would make a reader ask why every
	// route is listed twice, and why "/" goes to v2.
	//
	// at the two login lines: each spelling records under its own path,
	// so the metrics show which spelling clients send. [Rationale 6]
	//   mux.Handle("POST /api/v1/login", loginHandler(v2, "/api/v1/login"))
	//   mux.Handle("POST /api/login", loginHandler(v2, "/api/login"))
	// at mux.Handle("/", v2): the v2 router keeps its own 404 and 405 for
	// every path that is not a v1 route. [Verified 10]
}
```

### `internal/httpapi/v1/json.go` (new)

```go
// writeV1JSON writes the status and v as the old server encoded its
// bodies: application/json, HTML characters unescaped, and a trailing
// newline. [Verified 8]
func writeV1JSON(w http.ResponseWriter, status int, v any) {
	// style: doc comment alone
	// because: the doc comment names the old server's encoder settings,
	// and the body applies them.
}
```

```go
// v1Error is the old server's error body. [master:models/qdb_error.go:21]
type v1Error struct {
	Message string `json:"message"`
}
```

### `internal/httpapi/v1/login.go` (new)

```go
// v1Token is the old server's login answer. [master:models/token.go]
type v1Token struct {
	Token string `json:"token"`
}
```

```go
// v2LoginPath is the v2 login endpoint that the v1 login calls.
const v2LoginPath = "/api/v2/auth/login"
```

```go
// loginHandler answers the old server's login through the v2 login. It
// answers 200 {"token"} with the v2 access token, and 401 {"message"}
// for every other answer the v2 login gives. [Rationale 4, Rationale 5]
// route is the path the request duration is recorded under. [Rationale 6]
func loginHandler(v2 http.Handler, route string) http.Handler {
	// style: doc comment + overview + inline notes
	// because: the inner call has to be built, run and recorded before
	// the conversion, and the duration is recorded after the answer is
	// written.
	//
	// 1. build the v2 login request from the v1 one
	// 2. run it through the v2 router in process and record its answer
	// 3. convert the answer: 200 to the token, anything else to 401
	// 4. record the duration under the v1 route
	//
	// 1. The inner request carries the outer context, because the v2
	//    handler reads the logger, the cluster and the keychain from it.
	//    [internal/AGENTS.md, Logging and metrics]
	//    in, err := http.NewRequestWithContext(r.Context(), POST, v2LoginPath, r.Body)
	// 1. Content-Type is set to application/json whatever the client sent,
	//    because the v2 login refuses anything else with 415 and the v1
	//    login has no 415. [Verified 4; Rationale 5]
	// 1. The inner request carries no Accept-Encoding, so the v2 answer
	//    comes back uncompressed. [Verified 5]
	// 2. The answer is recorded whole, because a login answer is a few
	//    hundred bytes. [Rationale 7]
	//    rec := httptest.NewRecorder(); v2.ServeHTTP(rec, in)
	// 3. On 200 the refresh token is dropped, because a v1 client has no
	//    refresh endpoint. [Rationale 4]
	// 3. Any other status, or a body that does not decode, is 401 with
	//    the problem's detail, because the old server answered 401 for
	//    every login it could not complete. [Verified 8; Rationale 5]
	// 4. ObserveRequest(POST, route, status, since(start)).
}
```

The handler logs nothing of its own: the v2 login logs its failures
on the shared context, and the access line comes from the wrap in
main.

### `cmd/qdb_rest/main.go`

Step 5's comment block is rewritten whole from the new line: the
handler is the v2 router nested in the v1 router, wrapped once in the
request logging, so every request writes one access line whichever
router answers it. The observability block is still an argument,
because configuration does not travel in the context.

```go
handler := httpapi.WithRequestLogging(v1.NewHandler(httpapi.NewHandler(cfg.Observability)))
listeners, err := newListeners(ctx, cfg, handler)
```

Commit 2 lands the line without `v1.NewHandler`, so the access line is
never lost between commits.

### `internal/httpapi/v1/login_test.go` (new)

```go
// newServer is the v1 router over the v2 router, on a context carrying a
// discarding logger, a metrics registry, the HTTP plane, the fixture's
// cluster and an ephemeral keychain. [internal/httpapi/AGENTS.md, Tests]
func newServer(t *testing.T, c *qdb.Cluster) (context.Context, http.Handler)
```

```go
// login posts body to path without a Content-Type and answers the
// recorded response. [Rationale 5]
func login(t *testing.T, ctx context.Context, h http.Handler, path, body string) *httptest.ResponseRecorder
```

```go
// TestLogin: on both clusters and at both spellings, the fixture's user
// gets 200 with a body of one key, token, which opens GET
// /api/v2/session as that user. Every failure is 401 {"message"}.
func TestLogin(t *testing.T) {
	// style: doc comment + inline notes
	// because: the table's rows and the session read need a reason each.
	//
	// rows, run over cluster.Each and over both paths:
	//   the fixture's user, no Content-Type: 200 [Verified 11; Rationale 5]
	//   an undecodable body: 401 [Rationale 5]
	// secure subtest only:
	//   a wrong secret: 401 [ADR-0011]
	//   anonymous: 401, because the secure cluster refuses it
	// at the body check: the body decodes into a map of exactly one key,
	//   because the refresh token must not reach a v1 client. [Rationale 4]
	// at the session read: the token opens a v2 data route, because one
	//   verifier serves every token. [Verified 7]
	// at the 401 check: the body has message and nothing else, and no
	//   WWW-Authenticate. [Verified 8]
}
```

```go
// TestLoginUnreachable: a cluster that does not answer is 401 at both
// spellings, as the old server answered. [Verified 8; Rationale 5]
func TestLoginUnreachable(t *testing.T) {
	// style: doc comment + inline notes
	// because: the URI is the one line a reader would stop at.
	//
	// at the URI: qdb://127.0.0.1:1 refuses the connection at once.
	//   [internal/httpapi/login_test.go:113]
}
```

### `internal/httpapi/v1/AGENTS.md` (new)

```
# internal/httpapi/v1 -- Agent Instructions

Scope, then:

- Every v1 route calls its v2 endpoint in process through the v2
  router that `NewHandler` takes, and converts the answer (ADR-0007).
  No v1 route calls `internal/qdb`, `internal/query` or `internal/auth`.
- Each route is registered twice, at `/api/v1/<path>` and at its
  unversioned alias, and records its duration under the path it was
  registered at (ADR-0008). The inner v2 call records under its v2
  route, so one v1 request counts once per route.
- The login forwards the body as `application/json`, answers
  `{"token"}` with the v2 access token, drops the refresh token, and
  answers every failure 401 `{"message"}`, unreachable cluster
  included (owner, 2026-11-05).
- Every body goes through `writeV1JSON`, which encodes as the old
  server did: no HTML escaping and a trailing newline.
- Tests: one test per route over `cluster.Each`, each row at both
  spellings, against the v1 router nested over `httpapi.NewHandler`.
```

### `internal/AGENTS.md`, `internal/httpapi/AGENTS.md`, `cmd/qdb_rest/AGENTS.md`

```
internal/AGENTS.md, Code, the v1 bullet:
- the v1 package wraps the v2 endpoints, not the v2 functions, and has
  its own builder; open internal/httpapi/v1/AGENTS.md before touching
  a v1 route.
internal/AGENTS.md, Logging and metrics, the composition sentence:
- the entry point nests the v2 router in the v1 router and wraps the
  result in the request logging once (ADR-0007).
internal/httpapi/AGENTS.md, Middleware, first bullet:
- WithRequestLogging wraps the router the entry point composes, once,
  and NewHandler returns the v2 router without it.
internal/httpapi/AGENTS.md, Tests:
- the handler tests run through NewHandler, which writes no access
  line; middleware_test.go calls WithRequestLogging itself.
cmd/qdb_rest/AGENTS.md, the observability bullet:
- main passes the observability block to httpapi.NewHandler, nests the
  v2 router in v1.NewHandler and wraps the result in
  httpapi.WithRequestLogging.
```

### Documents

```
ADR-0007, edited in place:
- Context: the paragraph on the red `make -C tests/e2e test-v1` bar
  and ADR-0013 is rewritten to the side-by-side check run by hand.
- Decision 2: a v1 route builds the v2 request, calls the v2 endpoint
  in process through the v2 router, and converts the answer. The
  translation cost is accepted, because duplicated logic is the
  greater cost (owner, 2026-11-05).
- Decision 3: the v1 package has its own builder, which takes the v2
  router and hands it every request that is not a v1 route. The entry
  point nests the two and wraps the result in the request logging once.
- Consequences: the "decided when the package is written" bullet
  becomes the nesting; the login bullet says it wraps the v2 login
  endpoint.
- Alternatives: the v2 builder taking route callbacks (owner: not
  Go-like); v1 calling exported v2 functions (duplicates the handler's
  parsing and error mapping).

ADR-0008, edited in place:
- Context and Consequences: the goldens become the side-by-side cases,
  which run at both spellings against the new server.

ADR-0015, edited in place:
- Decision 4: M9 checks compatibility by a script run by hand that
  starts the old server built from master and the new one on the same
  cluster, sends each case to both and compares the answers. It stores
  nothing (owner, 2026-11-05).
- The restated rule: the stress and the v1 comparison run by hand.
- Consequences: "the golden suite they mention is the one M9 builds"
  becomes the comparison.

docs/brief.md:
- Compatibility contract, opening: the two endpoints answer as the old
  server's code does, and where the brief and that code disagree the
  code wins (owner, 2026-11-05). A script run by hand compares both
  servers, and no case exercises a deviation.
- Compatibility contract: a v1 route calls its v2 endpoint and converts
  the answer (ADR-0007).
- POST /api/v1/login: the token is the v2 login's access token, which
  lives auth.access_ttl, and the refresh token is not returned. Every
  failure is 401 {"message"}, unreachable cluster included.
- POST /api/v1/query: the column keys are data, name, type; an empty
  table name is omitted; a find answers "columns":null; a token failure
  is {"code":401,"message":...}. A #sql query runs on DuckDB as on v2
  (owner, 2026-11-05).
- Deliberate deviations: add "the v1 token lives auth.access_ttl, where
  the old server's lived 12 hours".
- Authentication, the v1 token bullet: the v1 token is the v2 access
  token, with auth.access_ttl, and no refresh token.
- Testing doctrine: the stress and the v1 comparison run by hand.
- Milestones, M9: the wrappers call their v2 endpoints, and a script
  run by hand compares the live old server with the new one.
- Project structure, the v1 row: v1 wrappers over the v2 endpoints.

docs/e2e.md:
- the purpose paragraph and "In Buildkite": the v1 comparison of M9
  joins the harness and runs by hand, never in CI.

docs/log.md, Current state:
- M9 in progress. Its exit criteria:
  1. POST /api/v1/login and POST /api/v1/query, with their aliases
     /api/login and /api/query, are served by internal/httpapi/v1 as
     wrappers that call the v2 endpoints and convert their answers.
  2. A script run by hand builds the old server from master, starts
     both servers on both clusters, sends each case to both and
     compares the answers: bytes for query bodies and error bodies,
     shape for the token. It is green, and it is not in Buildkite.
  3. The find prefix and the deviations the brief lists behave as
     specified, and each deviation's case is left out of the
     comparison.
- In flight: the v1 login (this unit's branch).
- Next: 1. the v1 query. An all-null column may carry its Arrow type's
  word instead of "none" if matching the old server costs more than a
  mapping (owner, 2026-11-05). 2. the side-by-side script.
- Entry 2026-11-05 -- M9 started: owner decisions; docs/brief.md,
  Compatibility contract and Milestones.
```

## Rationale

| decision                                                                                                                | why                                                                                                       | rejected, and why                                                                                                                                          | gained                                          | given up                                                                            | settled by        |
| ----------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------- | ----------------------------------------------------------------------------------- | ----------------- |
| 1. `NewHandler` returns the v2 router unwrapped, and main wraps the composed router in an exported `WithRequestLogging` | the owner wants two builders composed in main; one wrap gives every request one access line               | the v2 builder taking route callbacks: the owner found it not Go-like; v1 serving a separate router beside v2: v1 requests would miss the log or log twice | v2 knows nothing of v1; every request logs once | main knows to wrap                                                                  | owner, 2026-11-05 |
| 2. a v1 route calls the v2 endpoint in process and converts its answer                                                  | the owner: v1 is built on top of v2 as far as possible, to avoid duplicating logic, at a performance cost | calling exported v2 functions (`Cluster.Authenticate`, `Tokens.Issue`): the v1 package would repeat the body parsing and the error mapping                 | one login implementation                        | an in-process request per v1 call, and the v2 body re-parsed                        | owner, 2026-11-05 |
| 3. `v1.NewHandler(v2)` serves the v1 routes and hands `/` to v2                                                         | main then knows no v1 path                                                                                | a root mux in main listing the v1 paths and aliases: main would carry v1 knowledge                                                                         | the v1 surface is one package                   | `GET` on a v1 path is the v2 mux's 404, not 405                                     | proposal          |
| 4. the v1 token is the v2 access token as it comes, and the refresh token is dropped                                    | the owner: the bearer token's format and lifetime may differ; only the request data must be accepted      | a 12-hour v1 lifetime: it needs a v1 concern in the v2 login or a new config key                                                                           | `internal/auth` and the v2 login stay untouched | v1 tokens live `auth.access_ttl`, a listed deviation; a client logs in again on 401 | owner, 2026-11-05 |
| 5. every v1 login failure is 401 `{"message"}`, malformed body and unreachable cluster included                         | the old server answered 401 for every login it could not complete                                         | passing the v2 503 through: a down cluster would read differently than the old server                                                                      | a v1 client sees what it saw                    | the v1 login has no 503 and no Retry-After                                          | owner, 2026-11-05 |
| 5b. the inner request is sent as `application/json` whatever the client sent                                            | the v1 login accepts the same request data as the old server, and v1 has no 415                           | forwarding the client's header: a client that omits it would get 401                                                                                       | the Grafana plugin and curl both work           | v1 accepts a body under any media type                                              | proposal          |
| 6. each v1 route records its duration under the path it was registered at, through `observe`                            | the metric labels stay bounded by registration, as v2's are                                               | exporting `handleData`: the owner asked for nothing exported beyond the builders                                                                           | the alias's use is visible                      | a v1 request counts once under v1 and once under the v2 login                       | proposal          |
| 7. the inner answer is recorded with `httptest.ResponseRecorder`                                                        | a login answer is a few hundred bytes, and the recorder is the standard library's                         | a streaming converter: the login has nothing to stream                                                                                                     | no type of our own                              | a production import of `net/http/httptest`                                          | proposal          |
| 8. compatibility is a script run by hand that compares the live servers and stores nothing                              | the owner: these are manual validation runs on the owner's machine, and not to be over-engineered         | captured goldens replayed by hand (ADR-0015 as written): every golden would be one more stored file to audit                                               | nothing to recapture                            | every run needs a build of the old server                                           | owner, 2026-11-05 |
| 9. M9's exit criteria are the three in the log                                                                          | the owner approved them as drafted                                                                        | a v1 round-trip step in Buildkite: the owner asked for nothing more in CI                                                                                  | M9 closes on a by-hand run                      | no CI evidence of the old server's behaviour                                        | owner, 2026-11-05 |
| 10. the brief's v1 query section is corrected to the old server's key order, its 401 body and `#sql`                    | the owner: evidence from the old server's code overrules the brief                                        | keeping the brief's sketch: the query slice would build a shape the old server never sent                                                                  | the next slice starts from a correct spec       | none                                                                                | owner, 2026-11-05 |
| 11. the v1 login has no compression                                                                                     | the old server gzipped when asked, and the query slice decides v1 compression for both routes             | wrapping the v1 login in `withCompression`: it needs an export and settles a question the query slice owns                                                 | no export                                       | a client asking for gzip gets identity until the query slice                        | proposal          |

## Knowledge

- Commit 2: Rationale 1 lands in the doc comments of `NewHandler` and
  `WithRequestLogging`, and in the package comment of `httpapi.go`.
  The main step-5 comment is rewritten for the wrap.
- Commit 3: Rationale 2 lands in the package comment of `v1.go`.
  Rationale 3 and 6 land inline in `NewHandler`. Rationale 4, 5 and 5b
  land inline in `loginHandler`, and Rationale 7 at its recorder line.
  Verified 8 lands in `writeV1JSON`'s doc comment.
- Commit 4: the main comment names the nesting.
- Commit 5: the test claims of the Design.
- Commit 6: `internal/httpapi/v1/AGENTS.md` takes Rationale 2, 4, 5, 6
  and the test rule. `internal/AGENTS.md`,
  `internal/httpapi/AGENTS.md` and `cmd/qdb_rest/AGENTS.md` take
  Rationale 1 and the pointer.
- Commit 7: ADR-0007 takes Rationale 1, 2 and 3 with their rejected
  alternatives. ADR-0008 and ADR-0015 take Rationale 8.
- Commit 8: the brief takes Rationale 4, 5, 8 and 10, and the new
  deviation row.
- Commit 9: `docs/e2e.md` takes Rationale 8.
- Commit 10: the log takes Rationale 9, the Next items, and the owner's
  answer on all-null column types for the query slice.
- Rationale 11 is `plan only`: the query slice decides compression, and
  its plan records the answer.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and
document row from the Design and Knowledge sections in the same commit
as the code, and reconciles the code against this plan before the
build-stage message.

Specific to this unit:

- Commit 2 changes `main.go` as well, so the access line is never lost
  between commits.
- The v1 test cannot use the unexported fixtures of
  `internal/httpapi`'s tests, so it builds its own small context. If it
  grows past about thirty lines, ask the owner whether a shared
  fixture under `internal/qdbtest` is wanted before writing one.
- The ADR edits read as if the decision had always been the ADR's,
  and they get no log entry (`docs/AGENTS.md`, ADRs).

## Commits

1. `docs(plan): v1-login-plan.md, the v1 login over the v2 login endpoint`
2. `refactor(httpapi): NewHandler answers the v2 router, and main wraps it in WithRequestLogging`
3. `feat(v1): POST /api/v1/login and /api/login answer the v2 access token as {"token"}, 401 on any failure`
4. `feat(cmd): main nests the v2 router in the v1 router under one request log`
5. `test(v1): the login rows on both clusters and both spellings, and the token opens a v2 route`
6. `docs(agents): internal/httpapi/v1/AGENTS.md, and the composition in the internal, httpapi and cmd agents files`
7. `docs(adr): ADR-0007 wraps the v2 endpoint, ADR-0008 and ADR-0015 compare the old server live`
8. `docs(brief): the v1 contract follows the old server's code, and the v1 token is the v2 access token`
9. `docs(e2e): the v1 comparison runs by hand beside the stress`
10. `docs(log): M9 started with its three exit criteria`
11. `/doc-discipline all internal/httpapi cmd/qdb_rest`, and any fix it
    makes as further `docs(...)` commits.
12. `/doc-discipline check docs/v1-login-plan.md`, and any finding
    fixed before the next commit.
13. `docs(log): v1-login-plan.md deleted, facts moved to internal/httpapi/v1/AGENTS.md, internal/AGENTS.md, internal/httpapi/AGENTS.md, cmd/qdb_rest/AGENTS.md, ADR-0007, ADR-0008, ADR-0015, docs/brief.md, docs/e2e.md and the comments of internal/httpapi, internal/httpapi/v1 and cmd/qdb_rest`
14. Verify: push `sc-19567/rr-v1-login`, build its head in Buildkite,
    wait for the result. Green: report the build number. Red: fix with
    further small commits on this branch, push, build again.

## Open questions and recommendations

none
