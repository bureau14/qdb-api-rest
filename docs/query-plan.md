# POST /api/v2/query -- Plan

Status: draft. The first v2 data-plane unit: the query endpoint, its
content negotiation, the v2 error body, and the bearer middleware that
authenticates it. Decisions and verified facts land here first; when
the unit lands, the wire contract moves to an ADR, the handler rules to
`internal/httpapi/AGENTS.md`, and this file is deleted (`docs/AGENTS.md`,
Plans).

## Scope

In: `POST /api/v2/query`; `Accept` negotiation over the four encoders
in `internal/encoding`; the error body every v2 endpoint will use; the
bearer middleware. Out: `POST /api/v2/auth/login` and gzip (the next
M1 unit), the legacy wrapper (M3), `/metrics` (M4).

## Request

`POST /api/v2/query`. The body is the query text and nothing else:

```
curl -X POST http://127.0.0.1:40080/api/v2/query \
     -H 'Authorization: Bearer ...' -H 'Accept: text/csv' \
     --data-binary 'SELECT * FROM "reproduce" LIMIT 10'
```

- `Content-Type`: `text/plain` (any charset parameter is accepted; the
  C API reads UTF-8) or `application/sql` (IANA-registered). Absent
  counts as `text/plain`. Anything else answers 415, so a client that
  sends v1's `{"query": ...}` by habit is told why, not handed a qdbd
  parse error.
- The body is capped at 1 MiB (`http.MaxBytesReader`); over it, 413. A
  QuasarDB query is a line of text.
- An empty body is 400. The text is otherwise not inspected: no
  trimming, no prefix routing, no statement splitting.
- One way in: no `?query=`, no GET. A GET form can be added later
  without touching this one.

The shape is ClickHouse's and Trino's. Rejected: a JSON envelope
(`{"query": ...}`, v1's shape). The query is the resource's whole
input, and every field such an envelope grows next to it (a format, a
database) restates an HTTP header or the server's configuration.
Rejected: `?query=`: URL length limits and URL logging, and a second
way in.

## Response

- `Accept` selects the encoder: `application/json` (columnar, the
  brief's own shape), `application/x-ndjson`, `text/csv`,
  `application/vnd.apache.arrow.stream`. The listed media ranges are
  read in order and the first one an encoder matches wins; `*/*`, an
  absent header, and no match all mean JSON. `q`
  weights are not read: a client that wants a format names it. Edge
  left open: `text/csv;q=0` selects CSV.
- Status 200, `Content-Type` from the encoder, body chunked (its size
  is unknown until encoded), `X-Request-Id` echoed by the existing
  middleware. Compression is the gzip unit's.
- The status is always decided before the first byte. The batch is
  materialized before anything is written (`docs/brief.md`, Data
  plane), so every cluster error is known up front; `Encode` fails
  before its first write only on a column type it cannot render, and
  after it only on a write error, which is the client leaving. A
  mid-stream failure is therefore a cut stream, logged, never a
  status: the one contract HTTP can keep.
- No flushing writer. The encoder writes through its own 4 KiB
  buffer; `net/http` frames each write as a chunk once 2 KiB have
  accumulated and puts it on the wire through a 4 KiB connection
  buffer, so the body streams as the encoder produces it and the server
  holds those three buffers, nothing more. A `Flush` per encoder write
  would add chunk frames and change nothing else. Edge left open: a
  result under 4 KiB goes out when the handler returns, which is at
  once. The only writer wrapper the handler needs is a byte counter, so
  it knows whether a problem body may still be written.

## Errors

Every v2 error is an RFC 9457 problem details body,
`application/problem+json`: the one error shape with a registered
media type and a fixed vocabulary, what modern REST frameworks emit
by default, and one a client parses without knowing this API.

```
{"status":400,"title":"Bad Request","detail":"empty query"}
```

`title` is the status text; `type` is omitted (`about:blank`, the
status code's meaning, nothing more); `instance` is omitted, the
request id is already a header. One helper writes them. The mapping:

| Condition                                              | Status                                          |
| ------------------------------------------------------ | ----------------------------------------------- |
| empty or unreadable body                               | 400                                             |
| body over the cap                                      | 413                                             |
| `Content-Type` not text                                | 415                                             |
| no bearer, bad bearer, expired bearer                  | 401, `WWW-Authenticate: Bearer`                 |
| the cluster answered, whatever it said                 | 400, `detail` the binding's message             |
| the cluster unreachable or timed out; the breaker open | 503, `Retry-After` in whole seconds, rounded up |
| the caller's context ended                             | nothing on the wire; one debug line             |
| the REST API itself failed                             | 500                                             |

The status says who failed, because load balancers and service meshes
act on it: a run of 5xx ejects a backend, and a client typing invalid
queries must never eject a healthy gateway. So an answer from the
cluster is the caller's problem, 400, an invalid query, an unknown
table, a denied access and an oversized reply
(`ErrNetworkInbufTooSmall`) alike: the breaker draws the same line
(`internal/qdb/cluster.go`, `Call`). An unreachable cluster is 503,
honest because the readiness probe fails in the same moment, and the
binding's `IsClusterUnavailable` already names the condition. 500 is
reserved for this process: a column the encoder cannot render, a
panic. Refining 400 into 403 or 404 per `ErrorType` is a later, one-row
change when a client needs it. A 200 carrying an error body is
rejected: the status line is the one signal every client, `curl -f`
and every retry policy reads, and a problem body under 200 is parsed
as a result by anything that does not know this API.

## Bearer middleware

- `Authorization: Bearer <token>`, the scheme case-insensitive (RFC
  9110), one token. No `?token=`, no cookie: v2 never carries a token in
  a URL, and cookie auth is the future dashboard's concern.
- The token is verified with the keychain from the ctx; `typ` must be
  `access`. A refresh token is not a credential for the data plane; the
  legacy 12h token is an access token.
- Missing header: 401 with `WWW-Authenticate: Bearer`. Present but
  invalid or expired: 401 with
  `WWW-Authenticate: Bearer error="invalid_token"` (RFC 6750); the
  problem `detail` says which, since the verifier already distinguishes
  the two and expiry of a genuine token is no oracle.
- No anonymous requests: an anonymous caller logs in with empty
  credentials and receives a token (the login unit). The middleware sees
  only tokens.
- On success the claims ride the ctx (`auth.WithClaims` /
  `auth.ClaimsFrom`: `internal/auth` owns the caller, `docs/brief.md`,
  Project structure), the logger gains `user` and `session` attributes
  (`observe.KeyUser`, `observe.KeySession`, new), and the handler builds
  its `qdb.User` from the claims. The edge enriches; the handler reads.
- Applied per route (`requireBearer(handler)`), never to the mux: the
  probes and the login are unauthenticated.

## The handler, step by step

1. The mux pattern fixes method and path.
2. `Content-Type` checked, body capped and read.
3. `Accept` negotiated to one encoder.
4. `qdb.User` from the claims on the ctx.
5. `Cluster.Query(ctx, u, q, qdb.WithReadRetry())`: a read is
   idempotent and no byte has been sent, so one retry on a fresh
   session is safe.
6. An error is mapped by the table above: `BreakerOpenError` and
   `IsClusterUnavailable` to 503, the breaker's `RetryAfter` on the
   header, anything else the cluster said to 400.
7. `Content-Type` set, the batch released on return, `Encode` over the
   counting writer.
8. An `Encode` error with zero bytes out is a problem response; with
   bytes out it is a log line.

## Tests

- `internal/httpapi/query_test.go`, live fixture: one `SELECT *` over a
  `qdbtest/table` per media type through `NewHandler`, asserting the
  status, the `Content-Type`, and the body byte-equal to the encoder
  run directly over `Cluster.Query` of the same table. The encoders'
  own tests already prove the bytes decode; the handler test proves
  routing, negotiation and headers. The error mapping on the same
  table: empty body, JSON body, invalid query, no token, bad token.
- `internal/httpapi/bearer_test.go`: a minted access token passes; a
  refresh `typ`, garbage, and an expired token (fake clock) answer 401
  with the right `WWW-Authenticate`.
- Nothing for the counting writer or the problem helper.

## Where the facts go when this lands

- The wire contract (request body, negotiation, the problem shape, the
  bearer rule) to ADR-0010 "v2 query: request, negotiation and errors".
  The brief says endpoint shapes are decided in ADRs during the v2
  milestones (`docs/brief.md`, "/api/v2 endpoint sketch"); 0009 was
  retired with the Arrow plan and its number stays retired.
- The handler rules and the error table to `internal/httpapi/AGENTS.md`
  (new, with its `CLAUDE.md`), and a pointer row in `internal/AGENTS.md`.
- `docs/log.md`: In flight cleared, one entry for the ADR, one for this
  plan's deletion.

## Commits

1. `feat(observe): user and session keys`
2. `feat(auth): the claims travel in the request context`
3. `feat(httpapi): problem responses`
4. `feat(httpapi): the bearer middleware`
5. `feat(httpapi): Accept negotiation picks one of the four encoders`
6. `feat(httpapi): POST /api/v2/query runs the query and streams the batch`
7. `test(httpapi): one query per media type, the error mapping, the bearer edge`
8. `docs(adr): ADR-0010 v2 query: request, negotiation and errors`
9. `docs(httpapi): the query handler rules get their own AGENTS.md`
10. `docs(log): query-plan.md deleted; facts moved to ADR-0010 and internal/httpapi/AGENTS.md`

## Decision log (2026-09-12)

| Decision                                                                      | Why                                                                                               | Rejected                                                                      |
| ----------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| The query is the request body                                                 | ClickHouse and Trino; an envelope's other fields restate an HTTP header                           | `{"query": ...}` envelope; `?query=`                                          |
| Format by `Accept` only, unmatched means JSON                                 | one mechanism; the brief's table                                                                  | `?format=`; 406                                                               |
| Errors are RFC 9457 problem details                                           | registered media type, fixed vocabulary, what modern frameworks emit                              | `{"message"}` (v1's); a house envelope                                        |
| Status by who failed: cluster answered 400, unreachable 503, this process 500 | a run of 5xx ejects a backend from a load balancer; the binding already classifies unavailability | 500 for every cluster error; 200 with an error body; an `ErrorType` table now |
| The bearer middleware is in this unit                                         | little work, and the endpoint is never unauthenticated on the base branch                         | a later unit, anonymous until then                                            |
| No flushing writer                                                            | the encoder's and `net/http`'s buffers already stream; a `Flush` per write only adds frames       | a writer that flushes per encoder write                                       |
| No full-table `text/csv` e2e target                                           | not a target                                                                                      | the awk comparator over `reproduce.csv`                                       |

## Open questions

1. `Content-Type`: strict (415 for anything but text) as proposed, or
   not inspected at all?
2. The 1 MiB body cap: keep, or no cap?
3. The contract's permanent home: ADR-0010 as proposed, or a section in
   the brief?
4. The `typ` check in the middleware now (one line), or when M2 mints
   refresh tokens?
