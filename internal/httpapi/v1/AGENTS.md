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
- Every body goes through `writeV1JSON`, which encodes as the old server
  did: no HTML escaping and a trailing newline.
- Tests: one test per route over `cluster.Each`, each row at both
  spellings, against the v1 router nested over `httpapi.NewHandler`.
