# internal/httpapi/v1 -- Agent Instructions

Scope: the v1 compatibility surface, the old server's login and query at
`/api/v1/<path>` and at their unversioned aliases. The package-wide Go
rules and the test fixtures are in `internal/AGENTS.md`, and the v2
handlers these routes call are in `internal/httpapi/AGENTS.md`.

- Every v1 route calls its v2 endpoint in process through the v2 router
  that `NewHandler` takes, and converts the answer (ADR-0007). v1 and v2
  then share one implementation, at the cost of an in-process request per
  v1 call. No v1 route calls `internal/qdb`, `internal/query` or
  `internal/auth`.
- Each route is registered twice, at `/api/v1/<path>` and at its
  unversioned alias, and records its duration under the path it was
  registered at (ADR-0008). The inner v2 call records under its v2
  route, so one v1 request counts once under each.
- The login forwards the body as `application/json`, answers `{"token"}`
  with the v2 access token, drops the refresh token, and answers every
  failure 401 `{"message"}`, unreachable cluster included (owner,
  2026-11-05). The token lives `auth.access_ttl`, and a v1 client logs
  in again on a 401.
- The query reads `{"query"}` under any Content-Type, as the login reads
  its body, after the token, because the old server authenticated before
  it bound the body. No token is 401 `{"code","message"}`, an empty body
  422 code 602, and a body that does not decode 400 code 400, as
  go-openapi answered.
- The token is the `Authorization` header without one optional,
  case-sensitive `Bearer `, then `?token=`, each forwarded as the v2
  bearer, the first that is not 401 winning. v1 forwards the token and
  verifies nothing itself (ADR-0007).
- A query asks the v2 query for Arrow, because typed arrays give the old
  cell values and the null typing directly. It holds the whole result
  before it writes, because the old body lists every cell of a column
  before the next column, and nothing caps that result (owner,
  2026-11-06). It answers no table for no rows, else one table without
  a name. A column's word is `none` when every cell is null. The five
  old types keep their words and their `encoding/json` cells. Timestamps
  are in UTC whatever zone the process runs in (owner, 2026-11-06), and
  NaN and the infinities are null, because `encoding/json` cannot write
  them. Any other type answers its Arrow name and its v2 JSON cell
  through `encoding.JSONCell`, because only DuckDB answers such a type
  and no old client sends `#sql` (owner, 2026-11-06).
- A query that begins with `find` calls `POST /api/v2/find` and answers
  one table per name with `"columns":null`.
- A v2 401 is 401 `{"code","message"}`, a 400 or 413 is 400
  `{"message"}`, and anything else, 503 included, is 500 `{"message"}`,
  because the old server had no cap and answered 500 when it could not
  lease a session (owner, 2026-11-06).
- Every body goes through `writeV1JSON`, which encodes as the old server
  did: no HTML escaping and a trailing newline.
- Tests: one test per route over `cluster.Each`, each row at both
  spellings, against the v1 router nested over `httpapi.NewHandler`.
