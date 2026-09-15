# v2 Login -- Plan

Status: approved. The minimal `POST /api/v2/auth/login` of M1: the
endpoint that mints the access token `POST /api/v2/query` accepts
(`docs/brief.md`, Milestones, M1). Refresh tokens, logout, session
introspection and the refresh TTL are M2. The wire contract lands in
ADR-0011 with this work; the handler rules in `internal/httpapi/AGENTS.md`.
This plan is deleted when the endpoint lands.

## Contract

Request: `POST /api/v2/auth/login`, `Content-Type: application/json`,
body `{"username": "...", "secret_key": "..."}`, the fields of a
QuasarDB user security file. An empty or absent username is an anonymous
login; the secret key is then ignored.

Response 200, `application/json`, the RFC 6749 token response:

```json
{ "access_token": "<opaque>", "token_type": "Bearer", "expires_in": 900 }
```

`expires_in` is the access TTL in whole seconds. No `refresh_token`
until M2.

Errors are RFC 9457 problems (ADR-0010), the status saying who failed:

| Condition                             | Status                                          |
| ------------------------------------- | ----------------------------------------------- |
| `Content-Type` not `application/json` | 415                                             |
| body over the cap                     | 413                                             |
| body unreadable or not the object     | 400                                             |
| the cluster refused the credentials   | 401, no `WWW-Authenticate` (no bearer was sent) |
| the cluster unreachable or timed out  | 503                                             |
| the breaker open                      | 503, `Retry-After`                              |
| the caller's context ended            | nothing on the wire; one debug line             |
| minting failed                        | 500                                             |

The line between 401 and 503 is the one ADR-0010 draws for the query:
an answer from the cluster is the caller's problem, `IsClusterUnavailable`
is the cluster's.

## Credential check

Login dials the cluster as the presented user, outside the user pools:
a direct connect, closed on its own goroutine the moment it returns,
exactly the probe's shape (`internal/qdb/cluster.go`, `Probe`). The
pool is keyed by username and its dialer holds one set of credentials;
a login that went through the pool could only ever confirm the
credentials the pool was created with, and a second pool per username
is not a thing. The dial passes the breaker (open means 503 at once,
an unreachable cluster feeds it) and takes no budget unit: the session
lives for one handshake.

Verified 2026-09-15: `Cluster.connect(ctx, credentials, budgeted=false)`
is the direct dial the probe uses; the breaker bookkeeping lives inline
in `Call` and is factored into a helper both share.

## Claims

Every login is an original login: a fresh `sid` and `jti` (stdlib
`uuid`, v7), `gen` 0, `typ` `access`, `auth_time == iat == now`, `exp`
`now + auth.access_ttl`. The clock is the keychain's injected `now`
(ADR-0006): `Tokens` gains `MintAccess(username, secretKey)` returning
the token and its TTL, so no handler reads a clock.

## Configuration

`auth.access_ttl`, a duration, default `15m` (`docs/brief.md`, Open
questions, 1). `internal/config` parses it like every duration;
`internal/auth` refuses a non-positive value at startup, next to the
passphrase and argon2id checks. `examples/qdb_rest.yaml` documents it.

## Tests

- `internal/httpapi/login_test.go`: anonymous login on the insecure
  fixture, then a query with the minted token answers 200 -- the M1
  exit criterion; the error rows (415, 400, 401 on the secure fixture
  with a wrong secret, 503 with the breaker open) one by one.
- `internal/qdb`: `Authenticate` against both fixtures, good and bad
  credentials.
- `internal/auth`: `MintAccess` claims and expiry under the fixed clock.

## Out of scope

- A qdb-side secret rotation while the user's pool is alive: the pool
  keeps dialing with the credentials it was created with until idle
  eviction. Accepted (owner, 2026-09-15); nothing records it further.
- The legacy `/api/v1/login` wrapper (M3, ADR-0007) and refresh (M2).

## Decision log (2026-09-15)

| Decision                                         | Why                                                                                | Rejected                                                                                |
| ------------------------------------------------ | ---------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| Login verifies by a direct dial outside the pool | one credential set per pool; a pool cannot confirm a second one                    | a blind mint (the old server; bad credentials surface on the first query); a pool lease |
| Breaker yes, budget no                           | fail fast on a known-down cluster; the handshake session is too short to budget    | budgeted like `Call`; neither, like the probe                                           |
| `auth.access_ttl` in config now, default 15m     | M2 would add it anyway; one field                                                  | a constant until M2                                                                     |
| RFC 6749 token response                          | registered vocabulary, like RFC 9457 and 6750 for the query; stock clients read it | the brief sketch's `{access_token, expires_in}`                                         |
| Bad credentials are 401 without a challenge      | the credentials were the body, no bearer scheme was used                           | 403; 401 with `WWW-Authenticate`                                                        |
| `application/json` required, 415 otherwise       | one comparison, symmetric with the query's 415                                     | decoding any body as JSON                                                               |
| Secret rotation under a live pool: accepted      | rare; idle eviction resolves it                                                    | keying pools by a secret fingerprint                                                    |
