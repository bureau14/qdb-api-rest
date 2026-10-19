# Secure data path -- plan

Status: draft

## 1. Outcome

When the unit lands, the tree holds the following:

- Every Go test that dials qdbd runs once per cluster of the test
  services, as subtests named `insecure` and `secure`. On the insecure
  cluster it acts as the anonymous user, and on the secure cluster as
  the fixture's test user. The tests are the round trip, its
  deduplicated variant, the error rows, the instance cap, the compressed
  query, the read range, the sql scalars, login-then-query, readiness,
  the encoders' round trip, the bulk-reader tests, the pool tests of
  `internal/qdb` and the olap query test.
- One test stays secure-only, `TestLoginVerdicts`, because an insecure
  cluster accepts any secret, so its refusal case cannot exist there.
  `TestAuthenticate` already covers both clusters with per-cluster
  expectations and keeps that shape. The tests against an unreachable
  URI dial no cluster of the test services and stay as they are.
- `internal/qdbtest` describes both clusters as data (`qdbtest.Cluster`),
  and `internal/qdbtest/cluster` binds each one for a test
  (`cluster.Fixture`, `cluster.Each`).
- The CI test step runs the test packages one at a time (`-p 1`).
- On a secure cluster, a dial that fails for any reason other than an
  unreachable cluster is `qdb.ErrCredentialsRefused`. It is not retried.
  A failed DuckDB attach on a secure cluster is classified by one
  direct dial. Every data route answers that error with 401 and
  `WWW-Authenticate: Bearer error="invalid_token"`. The login keeps its
  401 without a challenge.
- ADR-0010's status table carries the refused-credential row, written
  as if it had always been part of the decision, with no log entry.
  `docs/AGENTS.md`, ADRs, says that an in-place edit also covers a
  decision added to an accepted ADR within its subject.
- The uncommitted documents of the 2026-10-19 JDBC smoke-test decision
  are committed: ADR-0015, ADR-0018, the brief, `docs/e2e.md`,
  `tests/e2e/AGENTS.md` and the log.

The unit leaves the following for later:

- M5 maps `qdb.ErrCredentialsRefused` on the Flight plane to
  `UNAUTHENTICATED` (owner, 2026-10-19). `docs/log.md`, M5 criteria,
  does not name it yet, and commit 25 adds it to the Handoff list.
- A token whose username has a live pool reuses that pool's
  credentials, so a wrong secret under a known name is not refused
  while the pool lives (ADR-0011, Consequences). This unit does not
  change that.
- `GET /api/v2/session` opens no connection and answers a refused
  token's claims with 200 (open question 1).

## 2. Verified facts

1. On the local secure qdbd (`qdb://127.0.0.1:2838`), an anonymous
   connect fails with "Invalid reply from the remote host", with or
   without the cluster public key. The fixture user's connect runs
   `SELECT 1`. [qdbsh probe, session of 2026-10-19]
2. The binding classifies `ErrInvalidReply` as retryable and as a bad
   session, and does not count it as an unreachable cluster.
   [`vendor/github.com/bureau14/qdb-api-go/v3/error.go:320-322`, `:359`,
   `:396-407`]
3. A pool's dial error reaches the caller of `Do` unchanged.
   [`vendor/github.com/bureau14/qdb-api-go/v3/session_pool.go:316-320`,
   `:433-440`, `:414-418`]
4. Every native data call reaches qdbd through `Cluster.Call`, then
   the user's pool, `Cluster.dial` and `Cluster.connect`.
   [`internal/qdb/cluster.go:345`, `:176-180`, `:159-173`;
   `internal/qdb/table.go:76`, `:83`; `internal/qdb/ingest.go:168`;
   `internal/qdb/read.go:174`]
5. `Cluster.Authenticate` dials through `connect` too.
   [`internal/qdb/cluster.go:388`]
6. `Call` retries once when the call opted in and the error is
   retryable. [`internal/qdb/cluster.go:334-355`]
7. `feedBreaker` counts only an unreachable cluster as a failure, so a
   refused credential counts as an answer.
   [`internal/qdb/cluster.go:312-322`]
8. Every data route answers a cluster error through `writeClusterError`,
   which answers 503 for an unreachable cluster and the route's status
   for anything else. [`internal/httpapi/query.go:77-96`, `:122`;
   `internal/httpapi/read.go:96`; `internal/httpapi/rows.go:158`;
   `internal/httpapi/sql.go:60`; `internal/httpapi/tables.go:108`,
   `:123`]
9. The login answers a refused dial through `writeClusterError` with
   401, and ADR-0011 fixes that 401 to carry no challenge.
   [`internal/httpapi/login.go:88-94`;
   `docs/adr/0011-v2-login.md:62`, `:96`]
10. `unauthorized(w, true, detail)` writes 401 with
    `Bearer error="invalid_token"`. [`internal/httpapi/bearer.go:33-44`]
11. A failed attach is `olap.ErrAttach`, which `qdb.IsClusterUnavailable`
    reports, because qdb-duck's message does not say whether the cluster
    refused or was unreachable. [`internal/olap/instance.go:45-48`;
    `internal/qdb/breaker.go:110-120`]
12. `instanceOf` takes a unit of `duckdb.max_instances` before it opens
    the instance, so a second user over the cap gets 503 before any
    dial. [`internal/qdb/cluster.go`, `instanceOf`, steps 2 and 3]
13. `credentialsOf` hands the user's name and secret and the cluster
    public key to qdb-duck on a secure cluster.
    [`internal/qdb/cluster.go:436-460`]
14. Pools are keyed by username, and the first caller's credentials
    build the pool. [`internal/qdb/cluster.go:213-220`;
    `docs/adr/0011-v2-login.md:19-21`, `:82-84`]
15. The test services have one secure user, `test-user`.
    [`scripts/tests/setup/start-services.sh:18`]
16. Each test qdbd allows 512 sessions.
    [`scripts/tests/setup/start-services.sh:68`]
17. The CI test step runs `go test ./...` without `-p 1`.
    [`scripts/cicd/30.test.sh:36`]
18. `internal/config` imports no package of this repository, so
    `internal/qdbtest` may import it without a cycle.
    [`go list -deps ./internal/config`, session of 2026-10-19]
19. The `package qdb` tests import `internal/qdbtest`, so
    `internal/qdbtest/cluster`, which imports `internal/qdb`, is closed
    to them. [`internal/qdbtest/cluster/cluster.go:1-5`;
    `internal/qdb/cluster_test.go:6`, `:18`]
20. `handleSession` answers from the token's claims and the
    configuration and opens no connection.
    [`internal/httpapi/login.go:220-231`]
21. The encoding tests query through `run`, which acts as the anonymous
    user. [`internal/encoding/encoding_test.go:28-35`]
22. Not verified: what qdb-duck answers when a secure attach gets an
    empty username. Fact 13 says the empty name and secret reach the
    attach. Commit 22's sql row on the secure cluster shows the answer.
    If the attach fails with something other than `olap.ErrAttach`, the
    executor reports it in the build-stage message and does not write a
    claim about it.

## 3. Design

### `internal/qdbtest/qdbtest.go`

The file keeps `Require`, `repoRoot`, the key file functions and
`SecureUser`. `InsecureURI` and `SecureURI` stay, because `Require`
and the URI constants are what `Cluster` is built from. Below
`SecureUser` it gains:

```go
// Cluster is one cluster of the test services, as data: the name its
// subtest runs under, its URI, whether it is secured, its key files,
// and the user a test acts as there. [Rationale R1]
// The user is anonymous on the insecure cluster and the fixture's test
// user on the secure one. [start-services.sh:18; Rationale R1]
type Cluster struct {
	Name             string
	URI              string
	Secured          bool
	PublicKeyFile    string
	UserSecurityFile string
	Username         string
	SecretKey        string
	// style: doc comment alone
	// because: plain data whose fields are named by the vocabulary
	//
	// at Secured: the field is stated, and not derived from
	// PublicKeyFile, so a test compares the server's answer with the
	// fixture and not with the server's own rule [roundtrip_test.go
	// authRoundTrip, Cluster.Secured at internal/qdb/cluster.go:99]
}

// Insecure is the insecure cluster, where a test acts as the anonymous
// user.
func Insecure() Cluster {
	// style: doc comment alone
	// because: one literal
}

// Secure is the secure cluster, where a test acts as the fixture's
// test user and the REST API's own user is the same user.
// [cluster.go NewSecure today]
func Secure(t testing.TB) Cluster {
	// style: doc comment alone
	// because: one literal over SecureUser
}

// Clusters is both clusters, insecure first. [Rationale R1]
func Clusters(t testing.TB) []Cluster {
	// style: doc comment alone
	// because: two calls
}

// Config is the default configuration bound to c: its URI, and on the
// secure cluster its public key file and the REST API's own user.
// It fails t with the start recipe when c does not answer.
// [Require; cluster.go NewSecure today]
func (c Cluster) Config(t testing.TB) config.Config {
	// style: doc comment alone
	// because: Require, then three assignments
	//
	// - rejected: building the config in each package, because the
	//   package qdb tests and the cluster fixture would hold two copies
	//   of one rule [Rationale R2]
}
```

The package doc comment gains one sentence: `Cluster` describes both
clusters of the test services, and `Clusters` lists them for a test
that runs once per cluster.

### `internal/qdbtest/cluster/cluster.go`

`NewInsecure`, `NewInsecureWith` and `NewSecure` are removed once
their last caller has moved (commit 18). `bind` stays. The file gains:

```go
// Fixture is one cluster of the test services bound for a test's life:
// the cluster as data, the bound *qdb.Cluster, and the user a test acts
// as there. [Rationale R1]
type Fixture struct {
	Spec    qdbtest.Cluster
	Cluster *qdb.Cluster
	User    qdb.User
	// style: doc comment alone
	// because: three fields
}

// Bind binds spec for t's life under the default configuration as
// edit leaves it. A nil edit keeps the defaults.
// [NewInsecureWith today]
func Bind(t testing.TB, spec qdbtest.Cluster, edit func(*config.Config)) Fixture {
	// style: doc comment alone
	// because: spec.Config, edit, bind, and the user from spec
}

// Each runs test once per cluster of the test services, as a subtest
// named by the cluster, each with its own bound cluster.
// [olap_test.go:103-108; Rationale R1]
func Each(t *testing.T, test func(t *testing.T, f Fixture)) {
	// style: doc comment alone
	// because: one loop over qdbtest.Clusters
}

// EachWith is Each under a configuration edit, for a test that needs a
// smaller limit. [errors_test.go:142]
func EachWith(t *testing.T, edit func(*config.Config), test func(t *testing.T, f Fixture)) {
	// style: doc comment alone
	// because: one loop
}
```

The package doc comment is rewritten as a whole. It says that `Bind`
binds one cluster of the test services for a test, that `Each` runs a
test once per cluster, and that the package is a subpackage for the
reason it states today.

### `internal/olap/olap_test.go`

`fixture`, `insecure` and `secure` are replaced by one function:

```go
// credentialsOf is what an instance attaches with as f's user: the
// host and port of f's URI, and on the secure cluster the user's
// credentials and the cluster public key.
// [olap_test.go:66-96 today; internal/qdb/cluster.go:436-460]
func credentialsOf(t *testing.T, f cluster.Fixture) olap.Credentials {
	// style: doc comment alone
	// because: one branch on Spec.Secured
}
```

`TestQueryReadsGeneratedTable` runs through `cluster.Each`. Its step 1
keeps its claim: the secure attach proves the secret's credentials. The
loop is now the fixture's. Steps 2 to 5 do not change.

### `internal/qdb/cluster_test.go` (package qdb)

`insecureConfig`, `secureConfig` and `anonymous` are removed. Each of
`TestRejectedQueryReusesSession`, `TestPerUserCapAndSharing`,
`TestRetryOnceOnRetryableFailure` and `TestIdleUserPoolEvicted` loops
over `qdbtest.Clusters(t)` with `t.Run(spec.Name, ...)`. It builds
`New(spec.Config(t), ...)` with its own edits and acts as
`User{Username: spec.Username, SecretKey: spec.SecretKey}`.

```go
// userOf is the user a test acts as on spec. [Rationale R2]
func userOf(spec qdbtest.Cluster) User {
	// style: doc comment alone
	// because: one literal
}
```

`TestSecureDialAsOwnUser` becomes `TestProbeAsOwnUser` and runs on both
clusters. On the insecure cluster the REST API's own user is anonymous,
because no user security file is configured. [cluster.go:104-106]

`TestAuthenticate` keeps its shape: the secure cluster accepts its user
and refuses a wrong secret, and the insecure cluster accepts the
anonymous user. It takes its configs from `qdbtest.Secure(t).Config(t)`
and `qdbtest.Insecure().Config(t)`. Commit 19 adds one assertion: the
refusal is `ErrCredentialsRefused`.

The file header is rewritten as a whole and names the two clusters:
every test here runs on both, except `TestAuthenticate`, which checks
each cluster's own verdict, and the unreachable-cluster tests.

### `internal/qdb/read_test.go` (package qdb_test)

The helper `read` takes the user (`qdb.User`) it reads as.
`TestReadAnswersRowsWritten` and `TestReadDropsTrailingNUL` run
through `cluster.Each` and create through `table.CreateAs(rt,
f.Cluster, f.User, tbl)`. The canary's doc comment keeps its claim
that the defect is the C API's. The test runs on both clusters
because the owner asked for every test on both. [Rationale R1]

### `internal/encoding/encoding_test.go`, `encode_test.go`

`run(t, c, q)` becomes `run(t, c, u, q)` and queries as `u`.
`TestRoundTrip` runs through `cluster.Each` and creates through
`table.CreateAs`. No other test of the package dials qdbd.
[encode_test.go:469-470, grep of 2026-10-19]

### `internal/httpapi` test fixture (`query_test.go`, `bearer_test.go`)

```go
// server is the root handler plus the request context main would give
// it: logger, cluster and keychain, and the user every request acts as,
// with one access token minted for that user. [query_test.go:31-38]
type server struct {
	ctx     context.Context
	c       *qdb.Cluster
	tk      *auth.Tokens
	handler http.Handler
	user    qdb.User
	token   string
	// style: doc comment alone
	// because: fields
	//
	// at user: the direct oracle calls and the fixture's cleanups act as
	// this user, so on the secure cluster they pass the cluster's check
	// [roundtrip_test.go:95, :115, :180, :194; table.go:439-462]
}

// newServer binds the server to f and acts as f's user.
func newServer(t *testing.T, f cluster.Fixture) server {
	// style: doc comment alone
	// because: one call
}

// newServerOn is newServer over any cluster and user, for the
// unreachable cluster. [login_test.go:108-118]
func newServerOn(t *testing.T, c *qdb.Cluster, u qdb.User) server {
	// style: doc comment alone
	// because: wiring
	//
	// at the token: it carries the user's secret key, because the data
	// routes dial the cluster with the token's credentials
	// [query.go:116-117; tables.go:78]
}

// mint seals a token of kind typ for u, expiring at exp.
func mint(t *testing.T, tk *auth.Tokens, typ string, u qdb.User, exp time.Time) string {
	// style: doc comment alone
	// because: one Mint call
}
```

`direct`, `directRead`, `checkRead`, `checkQuery`, `directSQL` and
`checkSQL` act as `s.user`. The call sites of `mint` pass `qdb.User{}`
for the anonymous user and `qdb.User{Username: "qdbtest_other"}` in
`TestSQLInstanceLimit`.

### `internal/httpapi` tests on both clusters

- `TestLoginThenQuery` runs through `cluster.Each`. It logs in with the
  fixture user's body, which is the empty body on the insecure cluster.
  Its doc comment names both clusters.
- `TestReadinessOKAgainstLiveCluster` loops over `qdbtest.Clusters`
  and probes under `spec.Config(t)`. On the secure cluster the probe
  dials as the fixture user, the REST API's own user.
- `TestQueryCompressed`, `TestReadTableRange` and `TestSQLScalars` run
  through `cluster.Each` and create through `table.CreateAs`.
- `authRoundTrip` logs in with the fixture user's body. The expected
  session is `Username: s.user.Username` and
  `Secured: f.Spec.Secured`. Its doc comment says that it logs in as
  the cluster's user, which is anonymous on the insecure cluster.
- `TestRoundtrip` and `TestRoundtripDeduplicated` run through
  `cluster.Each`. `table.RemoveOnCleanup` becomes
  `table.RemoveOnCleanupAs(rt, s.c, s.user, tbl)`. The file header
  says once that the round trip runs on both clusters.
- `TestErrorRows` runs through `cluster.Each`, and its two fixed tables
  are created with `table.CreateAs`. `TestSQLInstanceLimit` runs
  through `cluster.EachWith`. Its step 2 comment changes: the cap is
  taken before the second user's instance opens, so the second user
  needs no credential that the cluster accepts. [Verified fact 12]
- `TestLoginVerdicts` binds `cluster.Bind(t, qdbtest.Secure(t), nil)`
  and stays secure-only. Its doc comment says why: the insecure cluster
  accepts any secret, so the refusal case has no insecure twin.
  [Rationale R3]

### `scripts/cicd/30.test.sh`

The `go test` line gains `-p 1`. The comment above it states the
following claims:

```
# - -p 1 runs the test packages one at a time, because every package
#   that dials qdbd shares the two test clusters, and one test's effect
#   on a cluster can fail a test in another package [owner, 2026-10-19]
# - running packages in parallel would be safe only against a qdbd per
#   package, which brings problems of its own [owner, 2026-10-19]
# - rejected: running the tests that dial no qdbd in parallel, because
#   orchestrating the split is hard and not worth it [owner, 2026-10-19]
```

### `internal/qdb/cluster.go`

```go
// ErrCredentialsRefused is the error of a dial the secure cluster
// refused: the user's name and secret, or the anonymous user, are not
// accepted. [Verified facts 1, 2]
// The binding's error is wrapped, and its message stays in the chain.
var ErrCredentialsRefused = errors.New("qdb: the cluster refused the credentials")
	// style: doc comment alone
	// because: a sentinel
	//
	// - the binding answers a refused handshake with ErrInvalidReply,
	//   which it also uses for a confused protocol, so the server
	//   cannot tell the two apart from the code [fact 1; error.go:359]
	// - the classification covers a dial only, so ErrInvalidReply on a
	//   push (sc-19944) keeps its own answer [internal/AGENTS.md, Tests]
```

```go
// connect dials one session for u. A budgeted dial takes a budget unit
// first and holds it until the session's close returns. A dial that fails
// gives its unit back at once. On a secure cluster, a dial that fails
// other than by an unreachable cluster is ErrCredentialsRefused.
func (c *Cluster) connect(ctx context.Context, u credentials, budgeted bool) (qdbapi.Session, error) {
	// style: doc comment alone + inline note at the refusal branch
	// because: the existing four lines plus one branch
	//
	// at the refusal branch: an insecure cluster checks no credential,
	// so only a secure cluster's failed dial is a refusal
	// [cluster.go:445-449]
	// at the refusal branch: the login and the data routes see one
	// classification, because both dial through connect
	// [cluster.go:388; facts 4, 5]
}
```

```go
// Call (doc comment unchanged, plus one sentence: a refused credential
// is not retried.)
func (c *Cluster) Call(ctx context.Context, u User, f func(*Session) error, opts ...CallOption) error {
	// style: doc comment alone + inline note at the retry test
	// because: the existing loop with one more condition
	//
	// at the retry test: a refusal is not retried although the binding
	// calls ErrInvalidReply retryable, because a second dial with the
	// same credentials is refused the same way [fact 2; Rationale R6]
}
```

```go
// instanceOf (doc comment unchanged, plus one sentence: on a secure
// cluster a failed attach whose credentials the cluster refuses is
// ErrCredentialsRefused.)
func (c *Cluster) instanceOf(ctx context.Context, up *userPool, u User) (*olap.Instance, error) {
	// style: doc comment + numbered overview + step comments
	// because: it already has that shape, and it gains one step
	//
	// 1. to 3. unchanged
	// 4. on a secure cluster, classify a failed attach by one direct
	//    dial, because qdb-duck's message does not say whether the
	//    cluster refused or was unreachable [instance.go:45-48]
	//    - a refused dial replaces the attach error, and any other
	//      outcome keeps olap.ErrAttach and its 503 [Rationale R7]
	//    - rejected: refusing an empty username in credentialsOf,
	//      because it misses a user removed after login [Rationale R7]
	//    - the extra dial runs on the failure path only
	// 5. on failure, give the unit back (the old step 4)
}
```

The doc comment of `SQL` adds `ErrCredentialsRefused` to the errors
that come before the sink.

### `internal/httpapi/query.go`

```go
// writeClusterError (doc comment rewritten whole: a refused credential
// is 401 with the invalid_token challenge, so the client logs in
// again.)
func writeClusterError(ctx context.Context, w http.ResponseWriter, err error, answered int) {
	// style: doc comment alone + inline note at the new case
	// because: a switch with one comment per case, as today
	//
	// at the refused case: the token's credentials are what failed, so
	// the answer is the bearer edge's own for an invalid token
	// [bearer.go:33-44; RFC 6750 section 3.1, invalid_token]
	// at the refused case: 403 was rejected, because the cluster never
	// accepted the identity [Rationale R8]
	// at the refused case: the case comes before IsClusterUnavailable,
	// because a refused attach would otherwise count as unreachable
	// [breaker.go:110-120; Rationale R7]
}
```

### `internal/httpapi/login.go`

```go
// handleLogin (doc comment unchanged)
func handleLogin(w http.ResponseWriter, r *http.Request) {
	// style: the existing step comments, with one more case at the dial
	// because: the handler already narrates its steps
	//
	// at the dial: a refused credential answers 401 without a challenge
	// before writeClusterError runs, because the credentials were the
	// body and no bearer was in play [ADR-0011:62, :96]
}
```

### `internal/httpapi/errors_test.go`

The secure subtest of `TestErrorRows` adds one row per data route,
each with an anonymous access token and the answer 401 with
`Bearer error="invalid_token"`. The routes are the query, the sql, the
read of table `a`, the ingest, the create and the delete. The insecure
subtest has no such rows, because the insecure cluster accepts the
anonymous user.

```go
	// style: one comment above the rows
	// because: the rows are a group with one reason
	//
	// - the secure cluster refuses the anonymous user, and an anonymous
	//   token reaches it only by minting, because the login refuses it
	//   [login_test.go:79-107; fact 1]
	// - the rows run only on the secure cluster [Rationale R4]
```

`GET /api/v2/session` gets no row, because the route opens no
connection. [fact 20; open question 1]

### Documents

`docs/adr/0010-v2-query-request-negotiation-errors.md`, Decision 7. The
table gains a row below the bearer rows, and the paragraph below the
table gains its reason. Both are written as if they were always part
of the decision:

```
| the secure cluster refused the token's credentials | 401, `WWW-Authenticate: Bearer error="invalid_token"` |

- a refused credential is the caller's token failing, so the client
  logs in again as on any invalid token
- the login answers its own refusal without a challenge (ADR-0011)
```

`docs/AGENTS.md`, ADRs. The sentence on in-place edits gains this
claim:

```
- an in-place edit also adds a decision that falls within the
  accepted ADR's subject, written as if it was always part of it, with
  no log entry (owner, 2026-10-19)
```

`internal/AGENTS.md`, Code. A new bullet after the query-result bullet:

```
- on a secure cluster, a dial that fails other than by an unreachable
  cluster is qdb.ErrCredentialsRefused, whether a pool, the login or
  the classification of a failed DuckDB attach dialed
- it is not retried, it is an answer to the breaker, and the data
  routes answer it 401 invalid_token (ADR-0010)
- the binding's code for a refused handshake is ErrInvalidReply, so
  the classification is the dial's alone
```

`internal/AGENTS.md`, Tests. The fixture bullet is rewritten as a
whole:

```
- the fixture's home is internal/qdbtest: qdbtest.Cluster describes
  both clusters, Clusters lists them, Config binds a configuration to
  one, and Require fails fast with the start hint
- every test that dials qdbd runs once per cluster, as subtests,
  through cluster.Each, or over qdbtest.Clusters inside package qdb,
  which cannot import the cluster fixture
- the test acts as the anonymous user on the insecure cluster and as
  the test user on the secure one
- TestLoginVerdicts is secure-only, because the insecure cluster
  accepts any secret
- test packages run one at a time (go test -p 1 ./...), in CI too,
  because the packages share the two clusters and one test's effect
  can fail another; running the tests that dial no qdbd in parallel
  was rejected as orchestration not worth it (owner, 2026-10-19)
```

The table-fixture sentence about `CreateAs` and `RemoveOnCleanupAs`
stays as it is.

`internal/httpapi/AGENTS.md`, Tests. "insecure by default, and
`newServerOn` takes the secure one or an unreachable URI" becomes:
`newServer` takes a `cluster.Fixture`, every handler test runs on both
clusters through `cluster.Each`, and `newServerOn` takes an unreachable
URI. The error-rows bullet gains one claim: the secure subtest adds an
anonymous-token row per data route.

`docs/log.md`. Next drops the secure-cluster item. The Handoff list
gains: M5 answers `qdb.ErrCredentialsRefused` on the Flight plane with
`UNAUTHENTICATED` (owner, 2026-10-19; `internal/AGENTS.md`, Code).
After the `/doc-discipline check` run, commit 27 deletes this plan and
adds one entry: `secure-data-path-plan.md deleted; facts moved to ...`.

## 4. Rationale

| #   | decision                                                                        | why                                                                                                                                         | rejected, and why                                                                                                                                                                                | gained                                                      | given up                                                   | settled by                  |
| --- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------- | ---------------------------------------------------------- | --------------------------- |
| R1  | every qdbd test runs on both clusters as subtests over one shared fixture       | the owner wants every test on both clusters unless impossible, and the fixture has one home                                                 | local per-package copies, because they break the one-home rule; one CI run per cluster through an environment variable, because it needs a second CI step and runs tests that dial no qdbd twice | the secure path under every data test                       | CI wall time roughly doubles for the qdbd tests            | owner, 2026-10-19           |
| R2  | the clusters as data in `qdbtest`, bound in `qdbtest/cluster`                   | the package qdb tests cannot import `qdbtest/cluster`, and `qdbtest` may import `internal/config` (facts 18, 19)                            | a config builder in each package, because it would hold two copies of one rule                                                                                                                   | one description of each cluster                             | `qdbtest` imports `internal/config`                        | proposal                    |
| R3  | `TestLoginVerdicts` stays secure-only                                           | authentication works differently: the insecure cluster accepts any secret                                                                   | an insecure twin, because it would have no refusal to check                                                                                                                                      | none                                                        | none                                                       | owner, 2026-10-19           |
| R4  | the whole error-row table on both clusters, plus anonymous-token rows on secure | simplest, and the owner asked for the anonymous case now                                                                                    | only the rows that reach qdbd, because every new row would need a classification nobody checks                                                                                                   | the refusal is pinned per route                             | a few dozen repeated requests                              | owner, 2026-10-19           |
| R5  | `-p 1` in CI                                                                    | the packages share two qdbd clusters, and one test's effect can fail another                                                                | running packages in parallel, because it is safe only with a qdbd per package; running the tests that dial no qdbd in parallel, because the orchestration is hard and not worth it               | test isolation                                              | parallelism across packages                                | owner, 2026-10-19           |
| R6  | a refused dial on a secure cluster is `ErrCredentialsRefused`, not retried      | the login already treats it as a refusal (ADR-0011), and a retry with the same credentials is refused again (fact 2)                        | classifying by the binding's code, because `ErrInvalidReply` also means a confused protocol; classifying on the insecure cluster too, because it checks no credential                            | one classification for the login and the data plane         | a protocol failure during a secure dial reads as a refusal | proposal                    |
| R7  | a failed attach on a secure cluster is classified by one direct dial            | qdb-duck's message does not say whether it refused or the cluster was unreachable (fact 11)                                                 | refusing an empty username in `credentialsOf`, because it misses a user removed after login                                                                                                      | `/api/v2/sql` answers a refusal like every other data route | one more dial on the attach's failure path                 | proposal                    |
| R8  | data routes answer a refusal 401 with `invalid_token`                           | RFC 6750 names a token "invalid for other reasons" `invalid_token`, and the Grafana plugin logs in again on 401 (`docs/brief.md`, v1 login) | 403, because the cluster never accepted the identity; 400 as today, because it blames the request and not the token                                                                              | clients recover by logging in again                         | none                                                       | proposal                    |
| R9  | the login keeps 401 without a challenge                                         | ADR-0011 fixes it, because the credentials were the body                                                                                    | the data plane's challenge on the login, which ADR-0011 rejected                                                                                                                                 | the login contract holds                                    | the login handles the refusal before `writeClusterError`   | `docs/adr/0011-v2-login.md` |
| R10 | ADR-0010 edited in place, written as always, no log entry                       | the owner's rule for decisions that fall within an accepted ADR                                                                             | a new ADR, and an in-place edit with a log entry                                                                                                                                                 | one home for the status table                               | the in-place rule in `docs/AGENTS.md` widens               | owner, 2026-10-19           |
| R11 | the JDBC smoke-test documents land on this branch                               | the owner folded them into this unit                                                                                                        | their own branch first                                                                                                                                                                           | one review                                                  | the branch carries two subjects                            | owner, 2026-10-19           |
| R12 | the session route opens no connection and gets no error row                     | the brief designs it to be cheap enough to poll, and it reports what the token says (fact 20)                                               | dialing on each call, because it costs a connection per poll and changes the brief's contract                                                                                                    | the route stays cheap                                       | a refused token's session read answers 200                 | proposal (open question 1)  |
| R13 | M5 answers the refusal on the Flight plane with `UNAUTHENTICATED`               | the same sentinel serves both planes                                                                                                        | none recorded                                                                                                                                                                                    | one rule for both planes                                    | none                                                       | owner, 2026-10-19           |

## 5. Knowledge

| commit | where the whys land                                                                                                                                                                                                                                                                           |
| ------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2-6    | the JDBC documents as they stand in the working tree (R11). The rows already exist and need no new claim                                                                                                                                                                                      |
| 7      | `scripts/cicd/30.test.sh`, the comment above `go test` (R5)                                                                                                                                                                                                                                   |
| 8      | `qdbtest.Cluster` and its functions, with their doc comments (R1, R2). `Cluster.Secured`'s inline note                                                                                                                                                                                        |
| 9      | the `cluster.Fixture`, `Bind`, `Each` and `EachWith` doc comments, and the package doc comment (R1)                                                                                                                                                                                           |
| 10     | `olap_test.go`, `credentialsOf`. Step 1 of `TestQueryReadsGeneratedTable` keeps its claim                                                                                                                                                                                                     |
| 11     | `cluster_test.go`, the file header and `TestProbeAsOwnUser`'s doc comment, which names the anonymous own user on the insecure cluster                                                                                                                                                         |
| 12     | `read_test.go`, the canary's doc comment (R1)                                                                                                                                                                                                                                                 |
| 13     | `encoding_test.go`, `run`'s doc comment                                                                                                                                                                                                                                                       |
| 14     | the httpapi `server` type and its `user` inline note, plus `newServerOn`'s note on the token                                                                                                                                                                                                  |
| 15-18  | the test doc comments named in section 3. `TestLoginVerdicts` carries R3, and `TestSQLInstanceLimit` step 2 carries fact 12. `NewInsecure`, `NewInsecureWith` and `NewSecure` are removed in 18                                                                                               |
| 19     | `ErrCredentialsRefused`'s doc comment, `connect`'s doc comment and inline notes, and `Call`'s note (R6). `TestAuthenticate`'s assertion                                                                                                                                                       |
| 20     | `instanceOf`, step 4 with R7's rejected alternative, and `SQL`'s doc comment                                                                                                                                                                                                                  |
| 21     | `writeClusterError`'s doc comment and the notes on the refused case (R7, R8). The note at the login's dial (R9)                                                                                                                                                                               |
| 22     | the comment above the anonymous rows in `errors_test.go` (R4)                                                                                                                                                                                                                                 |
| 23     | ADR-0010, Decision 7, the row and its reason (R8, R10). `docs/AGENTS.md`, ADRs, the widened in-place rule (R10)                                                                                                                                                                               |
| 24     | `internal/AGENTS.md`, Code (the refusal bullet: R6, R7, R8) and Tests (both clusters, R1, R3, R5). `internal/httpapi/AGENTS.md`, Tests (R1, R4)                                                                                                                                               |
| 25, 27 | `docs/log.md`: Next drops the item and Handoff gains R13 (25). The plan is deleted and one entry names where the facts moved (27). R12 lands in `internal/httpapi/login.go`, `handleSession`'s doc comment, in commit 21: the route answers from the claims and opens no connection (fact 20) |

## 6. How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write the comments and document rows of every commit from their
   specifications in sections 3 and 5, in the same commit as the code.
   Each listed claim appears once, at the place the specification
   names. No claim is added without evidence. A claim that the code
   contradicts is not written. It is reported in the build-stage
   message.
3. A reason that arises while building and is not in this plan is
   written where it is decided, with its evidence, or asked of the
   owner through the question tool before the commit.
4. Fact 22 is open. Commit 22's secure sql row decides it. If the
   anonymous attach does not fail as `olap.ErrAttach`, report it and
   ask the owner before writing a comment about it.
5. Run `npx prettier --write` on every Markdown file a commit touches,
   and `golangci-lint` and `go test -p 1 ./...` before each code commit,
   against the local test services.
6. After the last code commit, run `/doc-discipline all` on
   `internal/qdbtest internal/qdb internal/olap internal/encoding
internal/httpapi scripts/cicd docs`, with one small commit per finding.
7. Before the build-stage message, run `/doc-discipline check
internal/qdbtest internal/qdb internal/olap internal/encoding
internal/httpapi scripts/cicd docs docs/secure-data-path-plan.md`.
   The build-stage message quotes its report, or says it was clean.
   Commit 27 deletes the plan only after this check.

## 7. Commits

1. `docs(plan): secure-data-path-plan.md, every qdbd test runs on both clusters and a refused credential answers 401`
2. `docs(adr): ADR-0018 decision 14 adds a smoke test through arrow's flight sql jdbc driver`
3. `docs(adr): ADR-0015 lists the rules it keeps and names the jdbc smoke test`
4. `docs(e2e): the harness specifies the flight sql jdbc smoke test`
   (with `tests/e2e/AGENTS.md`)
5. `docs(brief): m5 and the testing doctrine name the jdbc smoke test`
6. `docs(log): m5 gains a smoke test through arrow's flight sql jdbc driver`
7. `ci(cicd): the test step runs the packages one at a time`
8. `test(qdbtest): the fixture describes both clusters with the user each acts as`
9. `test(qdbtest): the cluster fixture binds each cluster and runs a test on both`
10. `refactor(olap): the query test takes both clusters from the cluster fixture`
11. `test(qdb): the pool, retry, eviction and probe tests run on both clusters`
12. `test(qdb): the read tests run on both clusters`
13. `test(encoding): the round trip runs on both clusters`
14. `test(httpapi): the server fixture acts as a given user`
15. `test(httpapi): login-then-query and readiness run on both clusters`
16. `test(httpapi): the compressed query, the read range and the sql scalars run on both clusters`
17. `test(httpapi): both round trips run on both clusters`
18. `test(httpapi): the error rows and the instance cap run on both clusters`
19. `feat(qdb): a dial the secure cluster refuses is ErrCredentialsRefused and is not retried`
20. `feat(qdb): a failed attach on the secure cluster is classified by one direct dial`
21. `feat(httpapi): a refused credential answers 401 invalid_token on every data route`
22. `test(httpapi): the secure error rows refuse an anonymous token on every data route`
23. `docs(adr): ADR-0010's status table answers a refused credential with 401`
24. `docs(internal): the test rules name both clusters and the serial run, and the code rules the refused credential`
25. `docs(log): next drops the secure-cluster item, and the handoff names the flight refusal`
26. The `/doc-discipline all` findings, one small commit each, then
    the `/doc-discipline check` run and a commit per unlanded finding.
27. `docs(log): secure-data-path-plan.md deleted; facts moved to the comments, internal/AGENTS.md and ADR-0010`
28. Verify: push `sc-19567/rr-secure-data-path`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again.

## 8. Open questions and recommendations

1. `GET /api/v2/session` opens no connection, so a refused token's
   session read answers 200. Recommendation: keep it, and give the
   route no error row (R12).
