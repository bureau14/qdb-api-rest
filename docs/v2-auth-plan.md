# Plan: v2 auth, the refresh lifetime, the token pair, refresh, logout and session

Status: draft

This plan is the only memory of the unit. The executor writes every
commit, comment and document row from it alone. The owner's decisions
are marked as such; everything marked `proposal` is settled by this
plan's approval.

## 1. Outcome

When the unit lands:

- `auth.refresh_ttl` is a configuration key, default 14 days, refused at
  startup unless positive and above `auth.access_ttl`.
- `POST /api/v2/auth/login` answers `access_token`, `refresh_token`,
  `token_type` and `expires_in`, the last being the access lifetime.
- `POST /api/v2/auth/refresh` takes `{"refresh_token": "..."}` and
  answers a fresh pair with the same `sid`, `gen` and `auth_time`,
  without dialing the cluster.
- `POST /api/v2/auth/logout` answers 204 behind the bearer edge and the
  server forgets nothing.
- `GET /api/v2/session` answers the caller's username, the cluster this
  server fronts, the token's times and handles, the caller's pool on
  this instance, and the server version, without a cluster round trip.
- The in-process round trip opens with login, refresh, session and
  logout, and the shell round trip's login step refreshes, continues
  with the refreshed token and reads the session.
- The log's M3 Next items are struck and M3 is closed (owner,
  2026-10-14: the agent closes it).

Left for later units: revocation (the `gen` claim stays minted as zero),
the Flight `Handshake` reusing the login's check (M5), the v1 token with
its fixed 12 hour lifetime (M8), and any instance identifier on the
session (none, ADR-0016 Decision 8).

## 2. Verified facts

| fact                                                                                                                                                                 | evidence                                                                    |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `config.Auth` has `TokenSecrets`, `Argon2id`, `AccessTTL`; defaults set `AccessTTL: 15 * time.Minute`                                                                | `internal/config/config.go:123-127,176-181`                                 |
| Flags and env variables derive from the struct's `yaml` and `help` tags by reflection, so a new field is a flag and a variable with no further code                  | `internal/config/config.go:207-235`, `cmd/qdb_rest/AGENTS.md`               |
| A duration key parses through `time.ParseDuration`, which has no day unit, so 14 days is spelled `336h`                                                              | `internal/config/config.go:251-255`                                         |
| `examples/qdb_rest.yaml` is pinned to `Default()` by test                                                                                                            | `internal/config/config_test.go:247-253`                                    |
| `validateAuth` owns the auth values' checks and names the key in the error; `AccessTTL <= 0` is refused there                                                        | `internal/auth/keychain.go:90-124`; `internal/AGENTS.md`, Code, validation  |
| `Tokens` holds `keys`, `accessTTL`, `now`; `MintAccess` is the only mint of a profile, `Mint(Claims)` seals any claims, `Verify` returns claims or the two sentinels | `internal/auth/token.go:85-222`                                             |
| `MintAccess` has one caller, the login                                                                                                                               | `internal/httpapi/login.go:78`                                              |
| `TestMintAccess` pins the claims a login fixes; `TestBadConfigRefused` has a "zero access ttl" row                                                                   | `internal/auth/auth_test.go:193-237`                                        |
| The bearer edge already refuses a non-access token with 401, `Bearer error="invalid_token"`, detail "not an access token", and this is pinned                        | `internal/httpapi/bearer.go:77-82`, `internal/httpapi/bearer_test.go:98`    |
| `requireBearer` places claims on the ctx and tags the logger; `ClaimsFrom` panics without them                                                                       | `internal/httpapi/bearer.go:83-87`, `internal/auth/context.go:36-44`        |
| `readBody` caps every body at `maxBodyBytes` (1 MiB), 413 over it, 400 unreadable                                                                                    | `internal/httpapi/query.go:18-38`                                           |
| `isJSON` judges `Content-Type`; a non-JSON login body is 415, an undecodable one 400                                                                                 | `internal/httpapi/login.go:43-69`                                           |
| `tokenResponse` is `access_token`, `token_type`, `expires_in`; `ExpiresIn` is `AccessTTL().Seconds()`                                                                | `internal/httpapi/login.go:35-41,85-89`                                     |
| `NewHandler` injects nothing; handlers read the logger, cluster and keychain from the request context, placed by main                                                | `internal/httpapi/httpapi.go:48-61`, `cmd/qdb_rest/main.go:184-204`         |
| The version is a `main` variable set by ldflags; nothing below `main` sees it                                                                                        | `cmd/qdb_rest/main.go:35,179`, `cmd/qdb_rest/AGENTS.md`                     |
| `Cluster` keeps `cfg config.Cluster` and `poolCfg config.Pool` unexported; `users` maps username to `*userPool`; `poolFor` creates a pool when absent                | `internal/qdb/cluster.go:30-33,185-196`                                     |
| The binding's pool answers `Stats()` with `InUse` and `Idle`                                                                                                         | `vendor/github.com/bureau14/qdb-api-go/v3/session_pool.go:169-176,349`      |
| `pool.per_user_max` is the per-user cap passed to every user pool                                                                                                    | `internal/config/config.go:97`, `internal/qdb/cluster.go:169-172`           |
| `cluster.public_key_file` empty means an insecure cluster                                                                                                            | `internal/config/config.go:66`                                              |
| The test fixture `server` holds `ctx`, `c`, `handler`, `token`; `newServerOn` mints the token directly and does not keep the `*auth.Tokens`                          | `internal/httpapi/query_test.go:31-51`                                      |
| `TestErrorRows` is one table of `errorRow{method, path, body, headers, status, challenge}`; a new route's rows join it                                               | `internal/httpapi/errors_test.go:20-27`, `internal/httpapi/AGENTS.md` Tests |
| The shell round trip's `login` posts the body file, expects 200 and prints `access_token`; `run_cluster` logs in once and uses the token for every step              | `tests/e2e/round-trip.sh:75-87,239-244`                                     |
| `docs/e2e.md` step 1 names the login's three fields; the specification says refresh and session join the login step                                                  | `docs/e2e.md:64-67,173-176`                                                 |
| The log's Next items 1 and 2 are the two M3 units; the M3 exit criteria list the config key, the pair, the three routes, their properties and the round-trip step    | `docs/log.md:22-28,37-40`                                                   |
| ADR-0011 (3) says there is no `refresh_token` until the refresh exists; ADRs are append-only and ADR-0016 (4) now governs the login's object                         | `docs/adr/0011-v2-login.md:44-46`, `docs/AGENTS.md`, ADRs                   |

## 3. Design

In file order of the tree.

### `internal/config/config.go`

```go
// Auth is the token keychain configuration: the rolling passphrases,
// the argon2id cost, and the two token lifetimes. [config.go:120-127]
type Auth struct {
	TokenSecrets []string
	Argon2id     Argon2id
	AccessTTL    time.Duration `yaml:"access_ttl" help:"validity of an access token from its login"`
	// RefreshTTL is the validity of a refresh token from the login or
	// refresh that minted it. [ADR-0016 (1)]
	RefreshTTL time.Duration `yaml:"refresh_ttl" help:"validity of a refresh token from the login or refresh that minted it"`
}
```

- `LogValue` gains `slog.Duration("refresh_ttl", a.RefreshTTL)` next to
  `access_ttl` [config.go:129-137].
- `Default()` sets `RefreshTTL: 14 * 24 * time.Hour` [ADR-0016 (1)].
- `examples/qdb_rest.yaml` gains, under `access_ttl`:

```yaml
# Validity of a refresh token from the login or refresh that minted it.
# Fourteen days; durations have no day unit.
refresh_ttl: 336h
```

### `internal/auth/keychain.go`

```go
// validateAuth refuses what this package cannot use and names the
// config key in the error. [existing contract, keychain.go:90-96]
func validateAuth(a config.Auth) error {
	// style: doc comment alone + inline note at the refresh check
	// because: a list of independent checks, one line each, as it is today
	//
	// at the refresh check: the refresh lifetime must be positive and
	//   above the access lifetime, because a refresh token that dies before
	//   the access token it came with can renew nothing [owner, 2026-10-14;
	//   Rationale: refresh bound]
	//   error: "auth.refresh_ttl must be above auth.access_ttl, got %s with access_ttl %s"
}
```

### `internal/auth/token.go`

```go
// Tokens mints and verifies this gateway's tokens under the derived
// keychain. One verifier serves every typ profile. [token.go:82-89]
type Tokens struct {
	keys       keychain
	accessTTL  time.Duration
	refreshTTL time.Duration // [ADR-0016 (1)]
	now        func() time.Time
}

// New ... unchanged contract; sets refreshTTL from a.RefreshTTL. [token.go:94-104]

// RefreshTTL is how long a refresh token minted now stays valid. [ADR-0016 (1)]
func (t *Tokens) RefreshTTL() time.Duration

// Pair is the access and refresh token of one login or refresh. [ADR-0016 (4)]
type Pair struct {
	Access  string
	Refresh string
}

// MintPair seals the pair of a login: a fresh session id, auth_time and
// iat both now, the access token expiring one access lifetime from now
// and the refresh token one refresh lifetime from now. [ADR-0011 (4),
// ADR-0016 (1, 4)]
func (t *Tokens) MintPair(username, secretKey string) (Pair, error) {
	// style: doc comment alone
	// because: it builds one base Claims value and forwards to mintPair
	//
	// at the base: sid is fresh per login because it is the security
	//   handle of one login across its refreshes [brief, Authentication;
	//   token.go:109-111]
}

// Refresh seals the pair a refresh token renews: the same username,
// secret, sid, gen and auth_time, under the current key. The caller has
// verified c and checked its typ. [ADR-0016 (5)]
func (t *Tokens) Refresh(c Claims) (Pair, error) {
	// style: doc comment alone
	// because: one forward to mintPair with the verified claims as the base
}

// mintPair seals two tokens from one base: each gets its typ, a fresh
// jti, iat now and exp at now plus its lifetime; everything else is the
// base's. [ADR-0016 (5)]
func (t *Tokens) mintPair(base Claims) (Pair, error) {
	// style: doc comment + numbered overview + step comments
	// because: two mints that must agree on the clock and differ in three
	//   fields, and the login and the refresh both depend on which fields
	//
	// The base carries what login and refresh share, and this function
	// owns what differs between the two tokens, so a change to the pair's
	// shape is made once. [Rationale: one mint path]
	//
	// 1. Read the clock once, so both tokens carry the same iat and their
	//    exp differ by the lifetimes alone [tests: TestMintPair]
	// 2. The access token: typ access, fresh jti, exp now + accessTTL
	//    [ADR-0016 (1, 2)]
	// 3. The refresh token: typ refresh, fresh jti, exp now + refreshTTL;
	//    the old refresh token stays valid until its own exp, because the
	//    server holds no state in which to retire it [ADR-0016 (6)]
}
```

`MintAccess` is deleted. The v1 login of M8 mints its own 12 hour access
token when M8 needs it [Rationale: MintAccess removed].

### `internal/auth/auth_test.go`

- `tokensFor` sets `RefreshTTL: 14 * 24 * time.Hour`; a `tokensAt(t,
secrets, now func() time.Time)` variant takes a movable clock for the
  sliding property.
- `TestBadConfigRefused` gains rows "zero refresh ttl" and "refresh ttl
  not above access ttl" (`RefreshTTL == AccessTTL`).
- `TestMintPair` replaces `TestMintAccess`: both tokens verify; the
  access claims equal the expected value (typ `access`, `auth_time` and
  `iat` at epoch, `exp` at epoch plus the access lifetime) once the
  handles are blanked; the refresh claims equal the same with typ
  `refresh` and `exp` at epoch plus the refresh lifetime; `sid` and
  `auth_time` are equal across the two; `jti` differs between the two and
  between calls; `sid` differs between calls.
- `TestRefreshSlides` (rapid): mint a pair at epoch, advance the clock
  by a drawn duration under the refresh lifetime, `Refresh` the verified
  refresh claims; the new refresh token's `exp` is the new now plus the
  refresh lifetime, the new pair keeps `sid`, `gen`, `auth_time`,
  username and secret, has fresh `jti`, and the old refresh token still
  verifies [ADR-0016 (5, 6)].
- `TestRotationThroughRefresh`: a pair minted under `["old"]`;
  `Refresh` under `["new", "old"]`; the new pair verifies under
  `["new"]` and the old refresh token does not [ADR-0016 Consequences,
  rotation].

### `internal/qdb/cluster.go`

```go
// UserStats is one user's pool as this instance sees it: sessions in
// use, idle, and the per-user cap. [ADR-0016 (8)]
type UserStats struct {
	InUse, Idle, Max int
}

// UserStats answers u's pool without creating one. A user with no pool
// on this instance answers zeros under the cap. [ADR-0016 (8); Rationale:
// no pool created]
func (c *Cluster) UserStats(u User) UserStats {
	// style: doc comment alone + inline note at the lookup
	// because: one locked map read and one Stats call
	//
	// at the lookup: poolFor is not used, because a who-am-I call must
	//   not open a pool for a user who has run nothing [Rationale: no pool
	//   created]
}

// URI is the cluster URI this instance fronts. [ADR-0016 (8)]
func (c *Cluster) URI() string

// Secured reports whether the cluster has a public key configured, which
// is what makes a login's credentials matter. [config.go:66]
func (c *Cluster) Secured() bool
```

### `internal/httpapi/httpapi.go`

```go
// The build version travels in the context next to the cluster and the
// keychain. main places it, and the session handler reads it. NewHandler
// keeps injecting nothing. [Rationale: version in the context]
type versionKey struct{}

// WithVersion returns ctx carrying the build version.
func WithVersion(ctx context.Context, v string) context.Context

// VersionFrom returns the version carried by ctx and panics without
// one, as ClusterFrom and TokensFrom do. [internal/auth/context.go:15-23]
func VersionFrom(ctx context.Context) string
```

`cmd/qdb_rest/main.go` step 4 gains `ctx = httpapi.WithVersion(ctx,
version)` after the keychain, and its step comment says the version
joins the context for the session endpoint [main.go:190-198].

### `internal/httpapi/login.go`

```go
// tokenResponse is RFC 6749's token response: the access token, its
// scheme, its validity in whole seconds, and the refresh token.
// [ADR-0011 (3), ADR-0016 (4)]
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// writeTokenResponse answers 200 with the pair; expires_in is the access
// lifetime, because the refresh lifetime is not the client's to count
// down. [ADR-0016 (4)]
func writeTokenResponse(w http.ResponseWriter, tk *auth.Tokens, p auth.Pair)

// handleLogin: contract unchanged except "answers with a token pair".
// [login.go:50-53]
func handleLogin(w http.ResponseWriter, r *http.Request) {
	// style: as today (step comments); the mint step changes
	//
	// at the mint: MintPair replaces MintAccess, and the response is
	//   written by writeTokenResponse so login and refresh answer one
	//   shape [ADR-0016 (4)]
}

// refreshRequest is the refresh body. [ADR-0016 (4)]
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// handleRefresh exchanges a refresh token for a fresh pair without
// dialing the cluster. A token that fails to verify, is expired, or is
// not a refresh token is 401 with the bearer challenge. [ADR-0016 (4, 5, 9)]
func handleRefresh(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + numbered overview + step comments
	// because: a handler of four decision points whose order is the
	//   contract (content type, body, verification, typ) [narrative rule 3]
	//
	// The refresh is a pure function of the token, the keychain and the
	// clock. It touches neither the cluster, the budget nor the breaker,
	// so it has no 503 and clients may call it as often as they like.
	// [ADR-0016 (5), Consequences]
	//
	// 1. Content type and body as the login's: 415, 413, 400 [ADR-0011 (1, 5)]
	// 2. Verify. Expired and invalid answer 401 with
	//    `Bearer error="invalid_token"` and the detail names which, through
	//    unauthorized(w, true, ...), because a token was presented
	//    [ADR-0016 (9); bearer.go:33-45]
	// 3. Require typ refresh; an access token answers "not a refresh
	//    token", because an access token must not renew itself [ADR-0016 (2, 9)]
	// 4. Re-mint through Tokens.Refresh and write the pair; a mint
	//    failure is 500 [ADR-0016 (9)]
}

// handleLogout answers 204. The server holds no session state, so there
// is nothing to forget; the verb exists for clients and the Flight
// layer, and a key rotation is the remedy for a leaked token. [ADR-0016 (7)]
func handleLogout(w http.ResponseWriter, _ *http.Request) {
	// style: doc comment alone
	// because: one WriteHeader
}

// sessionResponse is the caller's view of their session, assembled
// without a cluster round trip. The secret is never echoed. [ADR-0016 (8)]
type sessionResponse struct {
	Username string          `json:"username"`
	Cluster  sessionCluster  `json:"cluster"`
	Token    sessionToken    `json:"token"`
	Pool     sessionPool     `json:"pool"`
	Server   sessionServer   `json:"server"`
}
// sessionCluster: URI string `json:"uri"`; Secured bool `json:"secured"`
// sessionToken: Typ `json:"typ"`, IssuedAt `json:"iat"`, ExpiresAt `json:"exp"`,
//   JTI `json:"jti"`, AuthTime `json:"auth_time"`; unix seconds as the claims
//   carry them [Rationale: unix seconds]
// sessionPool: InUse `json:"in_use"`, Idle `json:"idle"`, Max `json:"max"`
// sessionServer: Version string `json:"version"`

// handleSession answers the caller's session from the claims, the
// cluster's view of the caller's pool, and the build version. It makes
// no cluster call, so it is cheap enough to poll. [ADR-0016 (8)]
func handleSession(w http.ResponseWriter, r *http.Request) {
	// style: doc comment alone + inline note at the pool
	// because: it assembles one value from three context reads
	//
	// at the pool: the numbers are this instance's truth and differ
	//   behind a load balancer [brief, endpoint sketch, "who am i"]
}

// registerAuthRoutes: login unauthenticated and compressed; refresh
// unauthenticated and compressed, because its credential is the body;
// logout and session behind requireBearer, session compressed.
// [login.go:92-97; ADR-0016 (7, 8)]
```

### `internal/httpapi/AGENTS.md`, Handlers

```
- The login row: "... The response is RFC 6749's token response
  (`access_token`, `token_type`, `expires_in`, `refresh_token`), written
  by `writeTokenResponse`, which the refresh shares. `expires_in` is the
  access lifetime through `Tokens.AccessTTL`, and the handler reads no
  clock."
- The refresh is `POST /api/v2/auth/refresh`, `{"refresh_token"}` under
  the login's content type and cap. It verifies, requires `typ`
  `refresh`, re-mints through `Tokens.Refresh` and answers the login's
  object. It never dials, so it has no 503. Its 401s carry the bearer
  challenge through `unauthorized`, and the detail says which check
  failed.
- The logout is `POST /api/v2/auth/logout` behind the bearer edge, 204,
  no body, no state.
- The session is `GET /api/v2/session` behind the bearer edge. It
  answers the claims, `Cluster.UserStats` for the caller, `Cluster.URI`
  and `Secured`, and `VersionFrom`. Times are the claims' unix seconds.
  It makes no cluster call.
```

`internal/httpapi/AGENTS.md`, Tests: "The fixture keeps its `*auth.Tokens`
so a test mints what a row needs (an expired refresh token). The fixture
places the version `test` in the context."

### `internal/httpapi/login_test.go`

- `TestLoginThenQuery`: `tr.RefreshToken` verifies as typ `refresh` with
  the same `sid` as the access token; the refresh token is refused by the
  query route with "not an access token" (one request, not a table row,
  because it needs the login's token).

### `internal/httpapi/errors_test.go`

Rows joining `TestErrorRows` [ADR-0016 (9)]:

```
"refresh: text body"      POST /api/v2/auth/refresh  "x"                       Content-Type text/plain      415  ""
"refresh: undecodable"    POST /api/v2/auth/refresh  `{"refresh_token":`       json                         400  ""
"refresh: garbage token"  POST /api/v2/auth/refresh  `{"refresh_token":"nope"}` json                        401  `Bearer error="invalid_token"`
"refresh: expired"        POST /api/v2/auth/refresh  expired refresh token      json                         401  `Bearer error="invalid_token"`
"refresh: access token"   POST /api/v2/auth/refresh  s.token in the body        json                         401  `Bearer error="invalid_token"`
"logout: no token"        POST /api/v2/auth/logout   ""                         nil                          401  "Bearer"
"logout: refresh token"   POST /api/v2/auth/logout   ""                         Bearer <refresh token>       401  `Bearer error="invalid_token"`
"session: no token"       GET  /api/v2/session       ""                         nil                          401  "Bearer"
"session: refresh token"  GET  /api/v2/session       ""                         Bearer <refresh token>       401  `Bearer error="invalid_token"`
```

The fixture gains `tk *auth.Tokens` so the expired and refresh tokens
are minted with `mint`.

### `internal/httpapi/roundtrip_test.go`

`TestRoundtrip` opens with the auth steps before the tables, and the
rest of the round trip runs under the refreshed access token:

1. login anonymous over HTTP: 200, the pair;
2. refresh with the pair's refresh token: 200, a new pair whose access
   token verifies with the login's `sid` and `auth_time`;
3. session with the new access token: 200, `username` empty, `cluster.uri`
   equal to the fixture's URI, `cluster.secured` false, `token.typ`
   `access`, `token.jti` equal to the new access token's, `pool.max` equal
   to `config.Default().Pool.PerUserMax`, `server.version` `test`;
4. logout: 204, empty body;
5. `s.token` becomes the refreshed access token, and the existing steps
   follow.

[`internal/httpapi/AGENTS.md`, Tests: a new route lands as a step of the
round trip]

### `tests/e2e/round-trip.sh`

```
# login <out_dir> <url> <body_file>: ... checked by shape, a non-empty
# access_token and refresh_token. Then refresh exchanges the refresh
# token for a new pair, and session reads the caller back under the new
# access token; the new access token is printed and authenticates every
# later step, so the round trip proves a refreshed token works.
# [docs/e2e.md step 1; Rationale: refreshed token continues]
login() {
    # 1. POST /api/v2/auth/login: 200, access_token and refresh_token non-empty
    # 2. POST /api/v2/auth/refresh with {"refresh_token": ...}: 200, both fields non-empty
    # 3. GET /api/v2/session under the new access token: 200, .username equals the
    #    body file's username ("" on the insecure cluster)
    # 4. print the new access token
}
```

`docs/e2e.md` step 1 becomes:

```
1. `POST /api/v2/auth/login`: 200 and the RFC 6749 shape, with
   `access_token` and `refresh_token` non-empty strings, `token_type`
   `Bearer` and `expires_in` a positive integer (ADR-0011, ADR-0016).
   `POST /api/v2/auth/refresh` with the refresh token: 200 and the same
   shape. `GET /api/v2/session` under the refreshed access token: 200
   and `username` equal to the login's. The refreshed access token
   authenticates every later step. Logout has no step, because it
   changes nothing a later step could observe.
```

### `docs/log.md`

Current state: M3 `done`, "closed 2026-10-14"; the M3 criteria block
and Next items 1 and 2 deleted; Next item 3 (the re-vendor) becomes
item 1. Entry: `## 2026-10-14 -- M3 closed: the token pair, refresh,
logout and session landed; v2-auth-plan.md deleted` with one line:
"Owner decisions in ADR-0016 and this plan's Rationale, moved to
`internal/httpapi/AGENTS.md`, Handlers, and the comments of
`internal/auth/token.go`."

## 4. Rationale

| decision                                                                                               | why                                                                                                                                     | rejected, and why                                                                                                                                          | gained                                                | given up                                                         | settled by                                                 |
| ------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------- |
| refresh bound: `auth.refresh_ttl` must be positive and above `auth.access_ttl`                         | a refresh token that dies before its access token can renew nothing, and the owner wants the odd configuration refused at startup       | a positive-only check: accepts a configuration whose refresh token is useless                                                                              | the misconfiguration fails at startup, naming the key | one more rule the code and the example config keep               | owner, 2026-10-14                                          |
| one mint path: `mintPair(base)` serves `MintPair` (login) and `Refresh`                                | ADR-0016 (5) fixes which fields a refresh keeps and which it renews; one function owns the difference so login and refresh cannot drift | two independent mints: the same three-field rule written twice                                                                                             | one place to change the pair's shape                  | `MintPair` builds a `Claims` only to pass it on                  | proposal                                                   |
| MintAccess removed                                                                                     | the login mints the pair; a single-token mint has no caller until M8                                                                    | keeping it for M8's 12 hour token: dead code for two milestones, and M8's token has a fixed lifetime this method does not take                             | no dead code                                          | M8 writes its own mint                                           | proposal                                                   |
| `expires_in` is the access lifetime; no `refresh_expires_in`                                           | ADR-0016 (4): the login's RFC 6749 object with `refresh_token` added, `expires_in` the access lifetime                                  | a second expiry field: not in the ADR, and a client refreshes on 401 rather than on a countdown                                                            | one shape stock clients read                          | the client does not learn the refresh lifetime                   | `docs/adr/0016-v2-refresh-logout-session.md:44-49`         |
| the refresh is unauthenticated at the edge and compressed, its credential is the body                  | ADR-0016 (4): the body carries the refresh token; a bearer header would be an access token, which must not refresh                      | the refresh token as a bearer header: rejected in ADR-0016's alternatives, one body shape across the two auth posts                                        | one parser for login and refresh                      | none                                                             | `docs/adr/0016-v2-refresh-logout-session.md`, Alternatives |
| the refresh's 401s carry `Bearer error="invalid_token"` through `unauthorized`                         | ADR-0016 (9) names that challenge for every refresh 401                                                                                 | a challenge-less 401 like the login's: the login's credentials were never a token, the refresh's are                                                       | one 401 writer                                        | none                                                             | `docs/adr/0016-v2-refresh-logout-session.md:87-100`        |
| logout is 204 and stateless                                                                            | ADR-0016 (7): the server holds nothing; the breach remedy is a key rotation                                                             | server-side revocation: needs shared state the brief rules out                                                                                             | no state                                              | a stolen token lives to its exp or the rotation                  | `docs/adr/0016-v2-refresh-logout-session.md:67-71`         |
| session field names and nesting (`cluster`, `token`, `pool`, `server`)                                 | ADR-0016 (8) leaves the exact names to the handler and lists the facts; grouping names the source of each number                        | a flat object: twelve fields with prefixes, harder to read for the support questions the endpoint serves                                                   | legible groups                                        | one level of nesting for clients                                 | proposal                                                   |
| unix seconds for the session's times                                                                   | the claims carry unix seconds (the JWT convention) and the handler converts nothing                                                     | RFC 3339 strings: a conversion in the handler and two spellings of one time across the API's auth surface                                                  | one representation                                    | less legible in a terminal                                       | proposal                                                   |
| no instance identifier on the session                                                                  | ADR-0016 (8) lists the version and no identifier; the brief's sketch is pre-decision intent                                             | an instance id: not decided, and nothing in the process has one today                                                                                      | nothing invented                                      | pool numbers behind a load balancer are not labelled by instance | `docs/adr/0016-v2-refresh-logout-session.md:75-83`         |
| version in the context (`WithVersion`, `VersionFrom`), panic without one                               | handlers read everything from the context and `NewHandler` injects nothing; `ClusterFrom` and `TokensFrom` panic the same way           | a `NewHandler(version)` parameter: breaks the tree's rule; re-pointing ldflags at an internal package: touches `scripts/cicd` and the version block's home | one wiring pattern                                    | the test fixture must place a version                            | proposal                                                   |
| `Cluster.UserStats` creates no pool                                                                    | a who-am-I call must not open cluster resources for a user who has run nothing, and `poolFor` would                                     | reusing `poolFor`: creates and pins an empty pool per session call                                                                                         | the session endpoint stays free of side effects       | a separate map read next to `poolFor`                            | proposal                                                   |
| `Cluster.URI` and `Cluster.Secured` accessors                                                          | the session needs the cluster URI and whether it is secured; the cluster already holds the config                                       | reading `config` in the handler: the handler has no config in its context, and the cluster is the one door to the binding                                  | no second config path                                 | two accessors                                                    | proposal                                                   |
| refreshed token continues: the round trips run every later step under the refreshed access token       | it proves a refreshed token is a working token end to end, at the price of nothing                                                      | keeping the login's token: the refresh is then checked by shape alone                                                                                      | the refresh is exercised against every route          | none                                                             | proposal                                                   |
| no logout step in the shell round trip; logout is a step of the in-process round trip and an error row | logout changes nothing a later shell step could observe, and the e2e specification lists refresh and session alone                      | a shell logout step: asserts a status and nothing else                                                                                                     | the shell suite stays the definition                  | none                                                             | `docs/e2e.md:173-176`, ADR-0016 Consequences; proposal     |
| the auth properties (pair, sliding, rotation through refresh) live in `internal/auth` without qdbd     | ADR-0016 Consequences: the refresh is a pure function of the token, the keychain and the clock                                          | handler-level properties: need the fixture cluster for nothing                                                                                             | fast tests                                            | none                                                             | `docs/adr/0016-v2-refresh-logout-session.md:104-106`       |
| M3 closed by the agent in the landing commit                                                           | the exit criteria are this unit's content and the green build                                                                           | the owner closes it: a second round trip through the owner for a fact the build proves                                                                     | one commit closes the unit and the milestone          | none                                                             | owner, 2026-10-14                                          |
| ADR-0011 (3) stays as written                                                                          | ADRs are append-only and ADR-0016 (4) supersedes the clause                                                                             | editing ADR-0011: against `docs/AGENTS.md`                                                                                                                 | the record stays                                      | one stale clause in an older ADR                                 | `docs/AGENTS.md`, ADRs                                     |

## 5. Knowledge

Per commit, where each why lands:

- Commit 2 (`config`): the field's help text and the example's comment
  carry "from the login or refresh that minted it" [ADR-0016 (1)]; the
  example's `336h` line says fourteen days and that durations have no
  day unit [config.go:251-255].
- Commit 3 (`validateAuth`): the inline note at the refresh check carries
  the "refresh bound" row's why [owner, 2026-10-14].
- Commit 4 (`MintPair`, `Refresh`, `mintPair`): `mintPair`'s overview
  carries "one mint path"; step 3 carries ADR-0016 (6)'s "old refresh
  token stays valid, no state to retire it"; `MintPair`'s inline note
  carries why `sid` is fresh per login. "MintAccess removed" lands
  nowhere in code (it is an absence) and in no document; it dies with
  the plan by design, and M8's plan rediscovers it from ADR-0016 (3).
- Commit 5 (login answers the pair): `writeTokenResponse`'s doc comment
  carries "`expires_in` is the access lifetime" [ADR-0016 (4)]; the
  `internal/httpapi/AGENTS.md` login row names the four fields and the
  shared writer.
- Commit 6 (e2e login checks `refresh_token`): `docs/e2e.md` step 1 and
  the `login` function's comment.
- Commit 7 (`Tokens.Refresh` and the two properties): the test comments
  carry ADR-0016 (5, 6) and the rotation consequence in one line each.
- Commit 8 (refresh handler): the overview carries "pure function, no
  503" [ADR-0016 (5)]; step 2 carries the challenge row; step 3 carries
  "an access token must not renew itself" [ADR-0016 (2)]; the
  `AGENTS.md` refresh row; the error rows.
- Commit 9 (logout): the doc comment carries ADR-0016 (7); the
  `AGENTS.md` logout row; the error rows.
- Commit 10 (`UserStats`, `URI`, `Secured`): `UserStats`'s inline note
  carries "no pool created"; `Secured`'s doc comment carries "a public
  key is what makes credentials matter" [config.go:66].
- Commit 11 (`WithVersion`): the type comment carries "version in the
  context" and names `NewHandler` injecting nothing; main's step 4
  comment names the session endpoint as the reader.
- Commit 12 (session handler): the response type comment carries "no
  cluster round trip, secret never echoed" [ADR-0016 (8)]; the times'
  field comment carries "unix seconds as the claims carry them"; the
  pool's inline note carries "this instance's truth"; the `AGENTS.md`
  session row; the error rows.
- Commit 13 (in-process round trip): the step comments carry "refreshed
  token continues" and why logout is a step here (it is observable
  in-process as a 204 and nowhere else).
- Commit 14 (shell round trip): the `login` comment and `docs/e2e.md`
  step 1 carry "refreshed token continues" and "no logout step".
- Commit 16 (log): M3 closed; the entry names where the facts went.

Rows with no landing place other than this plan: "MintAccess removed"
(an absence; accepted), "ADR-0011 (3) stays as written" (the rule is
`docs/AGENTS.md`'s already).

## 6. How the knowledge lands

The executor runs `/doc-discipline read` before the first code commit.
Every commit's comments and document rows are written from sections 3
and 5, in the same commit as the code, in the shape the root
`AGENTS.md`, "Code comments", prescribes: every listed claim appears
once at the place named, no claim is added without evidence, and a claim
the code contradicts is reported in the build-stage message and not
written. A why that arises while building and is not in this plan is
written where it is decided with its evidence, or asked of the owner
through the question tool before the commit that needs it. After the
last code commit, `/doc-discipline all` over the touched paths, one
small commit per finding. Before the build-stage message,
`/doc-discipline check <paths> docs/v2-auth-plan.md`, whose "Unlanded
from the plan" findings are fixed with further small commits or reported
with the reason they were dropped.

## 7. Commits

1. `docs(plan): v2-auth-plan.md, the refresh lifetime, the token pair, refresh, logout and session` (this document)
2. `feat(config): auth.refresh_ttl, default 14 days`
3. `feat(auth): the keychain refuses a refresh lifetime not above the access lifetime`
4. `feat(auth): Tokens.MintPair mints the access and refresh tokens of one login`
5. `feat(httpapi): the login answers refresh_token next to access_token`
6. `test(e2e): the round trip's login step checks refresh_token`
7. `feat(auth): Tokens.Refresh re-mints the pair from a refresh token under the current key`
8. `feat(httpapi): POST /api/v2/auth/refresh answers a fresh pair without dialing the cluster`
9. `feat(httpapi): POST /api/v2/auth/logout answers 204 behind the bearer edge`
10. `feat(qdb): Cluster answers one user's pool and the cluster it fronts`
11. `feat(httpapi): the build version travels in the context next to the keychain`
12. `feat(httpapi): GET /api/v2/session answers the caller's user, token, pool and server`
13. `test(httpapi): the round trip logs in, refreshes, reads its session and logs out`
14. `test(e2e): the round trip's login step refreshes and reads the session`
15. `/doc-discipline all` over `internal/auth internal/config internal/httpapi internal/qdb cmd/qdb_rest tests/e2e docs examples`, one small commit per finding; then `/doc-discipline check` over the same paths with `docs/v2-auth-plan.md`, fixes as further small commits.
16. `docs(log): M3 closed, the token pair, refresh, logout and session landed; v2-auth-plan.md deleted`
17. Verify: push `sc-19567/rr-v2-auth`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again. The mechanics are `.buildkite/AGENTS.md`'s.

## 8. Open questions and recommendations

1. Session times as unix seconds. Recommendation: unix seconds, as the
   claims carry them (Rationale row).
2. Session shape nested in four groups. Recommendation: nested, as
   section 3 sketches it (Rationale row).
3. The version reaches the handler through the context, panicking when
   absent. Recommendation: yes, matching `ClusterFrom` and `TokensFrom`.
