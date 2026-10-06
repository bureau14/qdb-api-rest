# ADR-0010: v2 query: request, negotiation and errors

Status: accepted
Date: 2026-09-15

## Context

`POST /api/v2/query` is the first v2 data-plane endpoint, and the shapes
it fixes (how the query arrives, how a format is chosen, what an error
looks like, how a caller authenticates) are the shapes every later v2
endpoint inherits. The brief fixes the format table and the bearer
scheme (`docs/brief.md`, Data plane and Authentication) and leaves the
endpoint contract to an ADR ("/api/v2 endpoint sketch"). Clients that
parse these shapes are customer code and the future thin client APIs,
so a change after release is a protocol change.

## Decision

### Request

1. **The query is the request body**, `text/plain` or `application/sql`
   (absent counts as `text/plain`, and anything else is 415). The body
   bytes reach the C API unchanged, with no transcoding, no trimming and
   no prefix routing. A charset parameter is neither honored nor checked.
   An empty or blank body reaches the cluster like any other query.
2. **The body is capped at 1 MiB**, and over it the answer is 413. A
   QuasarDB query is a line of text.
3. **One way in**: `POST` with the body. There is no `?query=` and no
   `GET`. A GET form can be added later without touching this one.

### Response

4. **`Accept` selects the encoder**: `application/json` (columnar, the
   v2 shape), `application/x-ndjson`, `text/csv`,
   `application/vnd.apache.arrow.stream`. The listed media ranges are
   read in order and the first one an encoder matches wins. `*/*`, an
   absent header and no match all mean JSON. `q` weights are not read.
   A client that wants a format names it (`text/csv;q=0` selects CSV).
5. **200, `Content-Type` from the encoder, body chunked.** The status is
   decided before the first byte. The batch is materialized before
   anything is written, so every cluster error is known up front. A
   failure in the middle of the stream cuts the stream and is logged.
   It is not a status.

### Errors

6. **Every v2 error is an RFC 9457 problem details body**,
   `application/problem+json`, with `status`, `title` (the status text)
   and `detail`. `type` and `instance` are omitted (`about:blank`, and
   the request id is already the `X-Request-Id` header). A 200 never
   carries an error body, because the status line is the one signal
   every client, `curl -f` and every retry policy reads.
7. **The status says who failed**, because load balancers and service
   meshes act on it. A run of 5xx ejects a backend, and a client typing
   invalid queries must not eject a healthy gateway.

   | Condition                              | Status                                                |
   | -------------------------------------- | ----------------------------------------------------- |
   | unreadable body                        | 400                                                   |
   | body over the cap                      | 413                                                   |
   | `Content-Type` not text                | 415                                                   |
   | no bearer                              | 401, `WWW-Authenticate: Bearer`                       |
   | bad, expired or non-access bearer      | 401, `WWW-Authenticate: Bearer error="invalid_token"` |
   | the cluster answered, whatever it said | 400, `detail` the binding's message                   |
   | the cluster unreachable or timed out   | 503                                                   |
   | the breaker open                       | 503, `Retry-After` in whole seconds, rounded up       |
   | the caller's context ended             | nothing on the wire, one debug line                   |
   | the REST API itself failed             | 500                                                   |

   An answer from the cluster is the caller's problem, whether it is an
   invalid query, an unknown table, a denied access or an oversized
   reply. That is the same line the breaker draws. An unreachable
   cluster is 503, which is honest because the readiness probe fails in
   the same moment. 500 is reserved for this process, for a column the
   encoder cannot encode or a panic.
   Unknown paths and wrong methods keep the stdlib mux's plain-text 404
   and 405.

### Bearer

8. **`Authorization: Bearer <token>`**, with the scheme case-insensitive
   (RFC 9110) and one token. There is no `?token=` and no cookie. v2
   does not carry a token in a URL.
9. **The token's `typ` must be `access`.** A refresh token is a
   credential for `POST /api/v2/auth/refresh` only, never for the data
   plane. The v1 12h token is an access token.
10. **The middleware is applied per route, never to the mux**, so the
    probes and the login are unauthenticated. There are no anonymous
    requests. An anonymous caller logs in with empty credentials and
    receives a token.

## Consequences

- Every later v2 endpoint writes errors through the same problem helper
  and authenticates through the same middleware. The table above grows
  rows, never a second shape.
- Refining 400 into 403 or 404 per the binding's error type is a
  one-row change when a client needs it.
- A `GET` form, a `?format=` parameter or `q` weights can be added
  without breaking a client that follows this contract.
- A result under the encoder's buffer goes out when the handler returns,
  which is at once. Nothing flushes per write.

## Alternatives rejected

| Alternative                                 | Why not                                                                                           |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| A JSON envelope or `?query=` for the query  | the query is the resource's whole input, and a format or a database belongs in a header or config |
| `?format=`, or 406 on no match              | one mechanism, the brief's table, and an unmatched `Accept` still gets the default                |
| A house error envelope                      | RFC 9457 has a registered media type and a fixed vocabulary a client parses without knowing us    |
| 500 for every cluster error, or 200 + error | a run of 5xx ejects a healthy gateway, and a problem under 200 is parsed as a result              |
| An `ErrorType` to status table now          | no client asks for it yet, and one row when one does                                              |
| Not checking `Content-Type`, no body cap    | one comparison turns a JSON body into a clear 415, and one line bounds what a request can read    |
| A writer that flushes per encoder write     | the encoder's and `net/http`'s buffers already stream, and a flush only adds chunk frames         |
| Deferring the `typ` check to M3             | one line, and a refresh token is never a data-plane credential                                    |

                                     |

| A section in the brief | endpoint shapes are decided in ADRs during the v2 milestones (`docs/AGENTS.md`, ADRs) |
