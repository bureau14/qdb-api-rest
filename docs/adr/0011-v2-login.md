# ADR-0011: v2 login: request, token response and credential check

Status: accepted
Date: 2026-09-15

## Context

`POST /api/v2/auth/login` is the endpoint that turns a QuasarDB user's
credentials into the access token every v2 data-plane request carries
(ADR-0010, Bearer). The brief fixes the token itself (ADR-0005), the
claims (`docs/brief.md`, Authentication) and leaves the endpoint shape
to an ADR. Two things make the shape hard to change later. Clients
parse the response, and `/api/v1/login` wraps this endpoint
(ADR-0007), so its behaviour bounds what the wrapper can promise.

The old server minted a token without asking the cluster, so bad
credentials surfaced on the first query. That is cheap, but a login
that answers 200 to anything is not a login, and the pool cannot do the
check either. A user's pool is keyed by username and its dialer holds
the credentials it was created with, so a lease could only ever confirm
those.

## Decision

### Request

1. **The body is JSON**, `Content-Type: application/json` (anything
   else is 415), and the object `{"username": ..., "secret_key": ...}`,
   which are the fields of a QuasarDB user security file. An empty or
   absent username is an anonymous login, and the secret key is
   ignored. The body is capped like the query's, and over the cap it is 413.

### Credential check

2. **Login dials the cluster as the presented user, once, outside the
   pools.** It is a direct connect, closed on its own goroutine the
   moment the handshake has answered. It passes the breaker, so open
   means 503 with `Retry-After` at once and an unreachable cluster
   feeds it, and it takes no unit of the session budget, because the
   session lives for one handshake. A refused handshake is the caller's 401.

### Response

3. **200 is the RFC 6749 token response**: `access_token`, `token_type`
   (`Bearer`) and `expires_in` in whole seconds. There is no
   `refresh_token` until the refresh endpoint exists, and adding it is
   one field.
4. **The token is an access token**, with fresh `sid` and `jti`, `typ`
   `access`, `auth_time` equal to `iat`, and `exp` at `iat` plus
   `auth.access_ttl` (default 15 minutes), under the keychain's clock.

### Errors

5. **Errors map to RFC 9457 problems** (ADR-0010) as follows, and the
   status says who failed:

   | Condition                             | Status                                  |
   | ------------------------------------- | --------------------------------------- |
   | `Content-Type` not `application/json` | 415                                     |
   | body over the cap                     | 413                                     |
   | body unreadable or not the object     | 400                                     |
   | the cluster refused the credentials   | 401, no `WWW-Authenticate`              |
   | the cluster unreachable or timed out  | 503                                     |
   | the breaker open                      | 503, `Retry-After` in whole seconds     |
   | the caller's context ended            | nothing on the wire, and one debug line |
   | minting failed                        | 500                                     |

   There is no challenge on the 401, because the credentials were the
   body and no bearer scheme was in play. The line between 401 and 503
   is the one the query draws. An answer from the cluster is the
   caller's problem, and `IsClusterUnavailable` is the cluster's.

## Consequences

- A login costs one cluster handshake, and a login storm is bounded by
  the breaker and the C API's socket timeout rather than by
  `pool.max_sessions`.
- The v1 wrapper (ADR-0007) inherits the credential check, so the old
  server's blind 200 for bad credentials on a secured cluster is not
  reproduced. On an insecure cluster the handshake accepts anything, so
  the goldens hold.
- Login does not detect a qdb-side secret rotation while the user's
  pool is alive. The pool keeps dialing with the credentials it was
  created with until idle eviction. This is accepted as rare.
- The refresh endpoint (M3) reuses the token response and the problem
  table, adding `refresh_token` and its own rows.

## Alternatives rejected

| Alternative                                       | Why not                                                                                      |
| ------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| A blind mint, as the old server                   | 200 to any credentials, and the failure moves to the first query, where no client expects it |
| Verifying through the user's pool                 | a pool holds one credential set, and a second pool per username is not a thing               |
| A budgeted login dial                             | the session lives for one handshake, and budgeting it adds a wait where nothing is held      |
| The brief sketch's `{access_token, expires_in}`   | RFC 6749 is a registered vocabulary stock clients read, like RFC 9457 and 6750 for the query |
| 403, or 401 with `WWW-Authenticate`, on bad creds | the caller is unauthenticated, and no bearer scheme was used                                 |
| Decoding any body as JSON                         | one comparison gives a clear 415, symmetric with the query endpoint                          |
| A TTL constant until M3                           | M3 adds the key anyway, and it is one config field now                                       |
