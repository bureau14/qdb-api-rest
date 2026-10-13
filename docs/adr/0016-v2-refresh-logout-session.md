# ADR-0016: v2 refresh, logout and session: lifetimes, shapes and what the server forgets

Status: accepted
Date: 2026-10-13

## Context

The brief (Authentication) requires stateless tokens that carry the
user's credentials, split into a short-lived access token and a
long-lived refresh token, with key rotation propagating through the
refresh. ADR-0005 fixes the cryptography and ADR-0011 the login, which
mints the access token alone. This ADR fixes the three remaining
endpoints of the v2 auth milestone and the two lifetimes, and it records
the trade the owner made: no server-side state of any kind, which rules
revocation out and makes the refresh free of cluster traffic.

Two things make the shapes expensive to change. Clients parse the
responses, and the Flight SQL `Handshake` and bearer metadata (brief,
Arrow Flight SQL) reuse the same tokens and the same verifier.

## Decision

### Lifetimes

1. **`auth.access_ttl` defaults to 15 minutes and `auth.refresh_ttl` to
   14 days.** Both are configuration, and the access default is
   conservative because the token embeds the user's secret. An operator
   whose jobs run many queries over hours raises the access lifetime
   rather than teaching the job to refresh.
2. **An access token is minted by login and by refresh only.** An
   access token cannot be exchanged for a new one, because its lifetime
   is the bound on a leaked token and a self-renewing access token would
   live as long as a refresh token.
3. **The v1 token is an access token with a fixed 12 hour lifetime and
   no refresh token** (brief, `POST /api/v1/login`). The lifetime is a
   constant in the code because the old server fixed it, and it is not
   configuration. One verifier serves every token, and `typ` has the
   two values `access` and `refresh`.

### Refresh

4. **`POST /api/v2/auth/refresh` takes the JSON body
   `{"refresh_token": "..."}`** with `Content-Type: application/json`,
   under the login's body cap, and answers the login's RFC 6749 object
   (ADR-0011) with `refresh_token` added. `expires_in` is the access
   token's lifetime in whole seconds. Login answers the same object
   from this ADR on, so a client reads one shape from both.
5. **A refresh verifies and re-mints, and it does not dial the
   cluster.** The verification is ADR-0005's (decryption under the
   `kid`, `typ` equal to `refresh`, `exp` in the future). The new pair
   carries the same username, secret, `sid`, `gen` and `auth_time`,
   fresh `jti` values, `iat` now, and `exp` at now plus the respective
   lifetime, minted under the current key. The credentials were proven
   at login, re-proving them is a usability question rather than a
   security one, and a refresh that costs the cluster nothing is one
   clients can call as often as they like.
6. **The refresh token is multi-use and slides.** The old refresh token
   stays valid until its own `exp`, and the new one expires
   `auth.refresh_ttl` from now. A refresh token exercised at least once
   per lifetime lives until the keys rotate. This is accepted, because
   the server holds no state in which to retire a token, and the remedy
   for a leaked token is a key rotation (brief, Rolling keys), which
   also retires every other token minted under the old key.

### Logout

7. **`POST /api/v2/auth/logout` takes a bearer access token and answers
   204 with no body.** The server forgets nothing, because it holds
   nothing. The endpoint exists so clients and the Flight SQL layer have
   the verb, and the `gen` claim stays minted as zero so a revocation
   layer can be added without a token format change.

### Session

8. **`GET /api/v2/session` takes a bearer access token and answers a JSON
   object** assembled without a cluster round trip: the username (empty
   for anonymous), the cluster URI this server fronts and whether it is
   secured, the token's `typ`, `iat`, `exp`, `jti` and `auth_time`, the
   caller's user pool as this instance sees it (sessions in use, idle
   and the cap), and this instance's version. The secret is never
   echoed. The exact field names are the handler's, and the brief's
   endpoint sketch names the intent.

### Errors

9. **Errors map to RFC 9457 problems** (ADR-0010). Refresh takes
   ADR-0011's rows for the content type, the cap and the body, and
   adds its own, which say which token failed and how:

   | Condition                                    | Status                                                |
   | -------------------------------------------- | ----------------------------------------------------- |
   | refresh token malformed, foreign or tampered | 401, `WWW-Authenticate: Bearer error="invalid_token"` |
   | refresh token expired                        | 401, the same challenge, detail "token expired"       |
   | an access token presented to refresh         | 401, the same challenge, detail "not a refresh token" |
   | a refresh token presented to any other route | 401, the same challenge, detail "not an access token" |
   | minting failed                               | 500                                                   |

   Logout and session sit behind the bearer middleware and answer its
   401s. Neither touches the breaker, the budget or the cluster, so
   neither has a 503.

## Consequences

- The refresh endpoint is a pure function of the token, the keychain
  and the clock, so its property tests need no qdbd and run with the
  auth package's.
- A key rotation completes within one access lifetime for active
  clients and within one refresh lifetime for idle ones. An operator
  removes the previous passphrase after `auth.refresh_ttl` has passed
  to keep every idle client, or earlier and accepts that those clients
  log in again.
- A qdb-side secret rotation surfaces when the user's pool next dials,
  which ADR-0011 accepts, and never through a refresh.
- The round trip's login step gains a refresh and a session call
  (`docs/e2e.md`, "The v2 round trip").
- ADR-0005's third `typ` profile for v1 does not exist. The v1 token is
  an access token with a different lifetime.

## Alternatives rejected

| Alternative                           | Why not                                                                                                         |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| A 1 hour access default               | the token embeds the secret, and the operator who needs an hour has the key                                     |
| Single-use refresh tokens             | a reuse cannot be detected without server state, so single use would be a client convention and not a guarantee |
| Re-dialing the cluster on refresh     | a usability question and not a security one, and it would put every refresh on the cluster                      |
| Server-side revocation on logout      | needs shared state the brief rules out, and a key rotation is the breach remedy                                 |
| An access token that refreshes itself | its lifetime would be the refresh lifetime, and the split would bound nothing                                   |
| The refresh token as a bearer header  | the login takes a JSON body, and one body shape across the two auth posts is one parser                         |
| A third `typ` for the v1 token        | a lifetime is a claim value, and a profile per lifetime multiplies the verifier's cases for nothing             |
| `auth.v1_ttl` as configuration        | the old server hard-coded 12 hours and the wrapper mirrors it                                                   |
