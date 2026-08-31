# Cluster Connection -- Plan

Status: draft. This document is the working plan for the first unit of
M1: the `cluster:` / `pool:` / `status:` configuration, the QuasarDB
session pool in `internal/qdb`, and the readiness probe. Auth, the legacy
endpoints and `/metrics` are separate units of M1 and are not planned
here. Scope and standing constraints: `docs/brief.md`, "Resilience and
connection management" and "Cluster binding"; progress: `docs/log.md`.
Hard decisions taken here become ADR-0003 (the pool) and ADR-0004 (the
probes).

## Purpose

After this unit lands, `qdb_rest` binds to one configured cluster, owns
a bounded set of authenticated C API sessions with a failsafe around
every call, and answers `GET /api/status/readiness` by dialing the
cluster as its own user and running one check. The later units
(auth, `/api/login`, `/api/query`) only add users to a pool that
already exists.

## Owner decisions

| Decision                                                                                                                                                                                                              | Why                                                                                                                                                     | Rejected                                                                                          |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| The pool core is `qdb-api-go`'s `SessionPool` and `SessionFactory`, vendored; this layer owns only what is REST-specific: the user map, the global budget, the breaker, the guard and the `Session` wrapper           | one pool, maintained with the binding; nothing the binding could carry is kept here                                                                     | a pool of our own, upstream-shaped, to be moved later                                             |
| Pools are keyed by `(cluster, username)`; REST sessions are a token-level security abstraction, never a pool key                                                                                                      | a user has one secret, so every session of a user dials identically; login never replaces an existing pool                                              | `(cluster, session id)` as the brief first had it (brief amended 2026-08-27)                      |
| One handle per goroutine at a time; never share a checked-out handle                                                                                                                                                  | `qdb_handle_t` is treated as non-thread-safe throughout (Verified facts)                                                                                | one handle per user plus a semaphore                                                              |
| Code reaches a handle only through the narrow `Handle` wrapper inside `Cluster.Call`; the binding's `HandleType` never leaves `internal/qdb`                                                                          | the failsafe and the error classification wrap every C call by construction; nothing can stash a raw handle                                             | `op(h qdbapi.HandleType)` plus a lint rule confining the import; convention only                  |
| Per-session knobs (`parallelism`, `max_in_buffer_size`, connections per address), all at the C API default unless configured                                                                                          | matches the C API's own granularity and the old server's flags                                                                                          | a global budget the pool divides; a server-specific `parallelism` default                         |
| Every call runs under a Go-side deadline; a timed-out call discards its session                                                                                                                                       | the C API has no cancellation path and is sometimes messy; a session that timed out once is never trusted                                               | trusting the C API's own timeouts                                                                 |
| Error classification is `qdb-api-go`'s `IsRetryable`: retryable discards the session, feeds the breaker and permits one read retry                                                                                    | one matrix, maintained upstream, already covering the whole vocabulary; unknown errors default to retryable                                             | a keep-list of request-caused errors maintained here; discarding on every error                   |
| Start when the cluster is unreachable; report not-ready                                                                                                                                                               | k8s pattern; the breaker absorbs the outage; only config errors refuse to start                                                                         | fail the start (systemd restart loop)                                                             |
| Nothing dials at startup; every session is dialed on demand                                                                                                                                                           | a pre-dialed session needs a retry mechanism of its own and buys milliseconds on the first request                                                      | a warm-up session for the REST API's own user; a reserved budget slot for it                      |
| Defaults: `max_sessions` 64, per-user cap 8, idle 5m, lifetime 15m, breaker 3 failures / 10s                                                                                                                          | short lifetime bounds the blast radius of a degraded session                                                                                            | 1h lifetime, 5 failures                                                                           |
| `max_lifetime` is checked on release only; each user pool runs the binding's reaper (10 s) and the cluster evicts empty pools on its own 10 s tick, both constants                                                    | a dialed session is always used at least once more; the ticks only need to sit well under `idle_timeout`                                                | a lifetime check on checkout; a `pool.reap_interval` key; one shared reaper driven by the cluster |
| Liveness, readiness and `/metrics` are pure probes                                                                                                                                                                    | ADR-0004 holds the decision, its consequences and the rejected alternatives                                                                             | see ADR-0004                                                                                      |
| Readiness is a dial plus one query, `status.readiness_query`, default `SELECT 1`                                                                                                                                      | the dial proves reachability and the REST API's own user; the query proves the session serves one; nothing heavier                                      | `qdb_wait_for_stabilization` (ring stability, not service); `Statistics()`                        |
| The cluster travels in `context.Context` next to the logger; handlers take it from the request context through package-level `qdb` entry points, and `ClusterFrom` panics without one, like `observe.Logger`          | a value passed along is simpler and more stable than injection plumbing; the logger already sets the pattern                                            | constructor injection into `httpapi.NewHandler`; a nil-returning lookup                           |
| `/metrics` is out of scope for this unit                                                                                                                                                                              | M2 owns the exposition endpoint; the pool exposes counters in-process only                                                                              | a minimal `/metrics` now                                                                          |
| Tests run against the live qdbd of `scripts/tests/setup/` wherever a real database is the cheapest fixture; a mechanism that would need the C API stalled or mocked is left untested beyond its pure part             | we are not testing the C API, which has no way to mock an error or a stall; a listener that never answers exercises the C API's blocking, not this code | stalling `connect` behind a silent listener; a fake dialer injected into the pool                 |
| The cluster key and the REST API's own user come from files only (`public_key_file`, `user_security_file`); callers' credentials arrive inline through the API, never through config                                  | files are what QuasarDB's tooling produces; a secret in the YAML invites leaks                                                                          | inline config keys with `${VAR}` injection; inline next to file with a precedence rule            |
| Every key is a file key, a `QDB_REST_<KEY_PATH>` variable and a `--<key-path>` flag (dots and underscores as hyphens), derived from the one config struct; `--cluster` and `--user-security-file` are `qdbsh` aliases | the operator picks the layer; one definition, no hand-kept list per layer                                                                               | flags for `qdbsh`'s three keys only; a hand-written table per layer                               |
| The loader is koanf over stdlib `flag`: defaults from the struct, the file, the environment, the explicitly passed flags, in that order                                                                               | layering and type coercion without hand-written setters; stdlib `flag` keeps the GNU usage                                                              | a hand-rolled reflection walker; viper with `pflag`                                               |
| The C API judges the dial options at dial time; the config validates only its vocabulary, the whole-second socket timeout, the buffer size and the pool cross-checks; there is no eager validation                    | one matrix, in the C API; a startup fail-fast would duplicate it for the sake of a few seconds                                                          | duplicating the C API's checks here; eager validation in the binding (code bloat)                 |
| Every dial goes through the binding's `SessionFactory`, one built per dial from the per-user `HandleOptions` this layer assembles from config                                                                         | credential merge and option order live next to `HandleOptions`, upstream; a factory is a pointer, so caching one buys nothing                           | keeping the options translation here for good; a cached factory per pool                          |
| Vocabulary is QuasarDB's (`docs/brief.md`, Vocabulary): a user has a `username` and a `secret_key`, or a `user_security_file`; the pooled client connection is a session                                              | the config, the flags, the user security file and the binding read the same; qdbd, client and REST sessions are one word at three layers                | invented terms                                                                                    |
| CI starts qdbd in the build step, the `qdb-nats-connector` way                                                                                                                                                        | tests assume the database; the e2e harness itself stays out of CI                                                                                       | skipping database tests in CI; a `-tags=integration` split                                        |
| No client out-buffer knob                                                                                                                                                                                             | the C API has none (out buffer fixed at 256 MiB)                                                                                                        | `max_out_buffer_size`                                                                             |
| The binding's global logger is replaced at startup by an adapter over the process logger; the binding's Info maps to Debug                                                                                            | `qdb-api-go` logs through a package-level logger that defaults to text on stderr; ADR-0002 wants one stream, and one that stays quiet                   | leaving the binding's stderr output in place; Info as Info                                        |
| The global budget is accounted in the pool's dialer and closer hooks (`WithDialer`, `WithCloser`)                                                                                                                     | the pool owns both ends of a session's life; the closer runs after `qdb_close` returns, which is when the cluster lets go of the session                | releasing the unit when a lease ends; a shared budget type upstream                               |
| A black-holed cluster address is documented, not engineered around                                                                                                                                                    | the OS bounds the connect syscall; the breaker opens after `breaker.failures` such dials; readiness is separate                                         | capping concurrent dials per user; an upstream connect timeout on M1's path                       |

## Verified facts

`qdb-api-go`, vendored at `d6c0112`:

- `SessionFactory` (`session.go`) dials from immutable `HandleOptions`:
  transport, then limits, then credentials, then `Connect`. Nothing is
  validated at construction; the C API judges the options at dial, and
  a key or a user given both inline and as a file is read from the
  file. `HandleOptions` covers URI, cluster public key (inline or
  file), user name/secret (inline or file), encryption, compression,
  client max parallelism, client max in-buffer size, timeout. Its
  defaults are the binding's, not the C API's: compression `CompBalanced`
  where the C API defaults to none, timeout 120 s where the C API
  defaults to 5 min. Every knob is applied only when non-zero, so a zero
  in our config means the C API default.
- `SessionPool` (`session_pool.go`): a per-pool `max_sessions`, idle and
  lifetime ages measured on `WithClock`, its own reaper
  (`WithReapInterval`, 10 s), `WithDialer` (the default ignores the
  context: the C API cannot cancel a connect) and `WithCloser` (run on
  the pool's close goroutine, after `qdb_close` returns), every close on
  its own goroutine, `Lease.Done(err)` discarding on `IsRetryable`, and
  `Acquire` after `Close` failing with a fatal error. Its invariants are
  pinned upstream by a `rapid` model.
- `IsRetryable(err)` (`error.go`) is true exactly when `ClassifyError`
  answers `ErrorClassRetryable`. `ErrorType.ErrorClass()` calls nine
  codes fatal -- `ErrNotImplemented`, `ErrIncompatibleType`,
  `ErrUninitialized`, `ErrOutOfBounds`, `ErrInvalidQuery`,
  `ErrAliasNotFound`, `ErrAliasAlreadyExists`, `ErrInvalidArgument`,
  `ErrNetworkInbufTooSmall` --
  success and informational codes none, and every other code
  retryable, permission and quota errors included; an error that is not
  an `ErrorType` (unwrapped via `errors.As`; `wrapError` uses `%w`) is
  retryable. An oversized reply (`ErrNetworkInbufTooSmall`) is therefore
  the request's fault: the session is reused and the breaker untouched;
  an `ErrAccessDenied` is retryable and costs a reconnect.
- The binding logs through a package-level logger (`logger.go`,
  `SetLogger`), defaulting to a text handler on stderr at Info; a dial
  logs `successfully connected` at Debug. The adapter must implement
  `Detailed`, `Debug`, `Info`, `Warn`, `Error`, `Panic`, `With`.
- `Query.Execute` returns a `*QueryResult` the caller closes with
  `Close()` (nil-safe, idempotent); a result may come back next to an
  error, so it is closed on every path. `qdb_close` frees whatever is
  left, so a leak lasts as long as the handle.
- `HandleOptions.WithConnectionsPerAddress(int)` is validated
  `0 | 2..100000` and applied before `Connect`; the C API default is
  not assumed anywhere ("depends on the C API version").
- `SELECT 1` is a valid query on an empty cluster and returns one row;
  a connected handle answers it without naming a table.
- Not wrapped: `qdb_option_set_client_soft_memory_limit` and
  `qdb_option_set_stabilization_max_wait`. The binding is never patched
  (`docs/brief.md`, Vendoring), so exposing them is an upstream change.

C API, verified in `~/git/quasardb` on 2026-08-25 and 2026-08-26 (the
facts the failsafe is built on):

- **`qdb_handle_t` is treated as non-thread-safe.** Some operations on
  a connected handle happen to be guarded (ring under a shared mutex,
  per-connection checkout from the broker); configuration setters,
  `connect` and `qdb_get_last_error` are not, and no header documents
  any of it. The rule is the conservative one: one handle per
  goroutine, never shared, every operation serialized by ownership.
  The worst case is concrete: **`qdb_close()` from another thread
  while a call is in flight is use-after-free.** `qdb_close` is
  `delete handle;` behind a canary check at function entry
  (`api/src/client.cpp`, `api/src/api_wrapper.hpp`); no refcount, no
  lock, no drain. So is a second `qdb_close` on the same handle. A
  handle is closed exactly once, by the goroutine that owns the
  in-flight call, after the call returns.
- **There is no cancellation path.** No `qdb_cancel*`, no per-call
  timeout. The library has one internally (`qdb/client/async.hpp`
  `cancel()` closes the socket) but it is not reachable from the C API.
- **`qdb_option_set_timeout` is a per-syscall socket timeout**
  (`SO_RCVTIMEO`/`SO_SNDTIMEO`), not an operation deadline: values
  under 1 s are rejected with `qdb_e_invalid_argument` and the rest is
  truncated to whole seconds (`node_client_base.cpp`,
  `set_timeout`); a receive loops until the whole message arrives; the
  broker retries a request once; `qdb_query` and batch push retry the
  whole operation up to 3 more times on connection-origin errors; the
  conflict/clock-skew loop is bounded by `transaction_max_wait`
  (default 6 min). The Go-side deadline is therefore the only
  operation-level bound, and one C call may legitimately outlive
  `cluster.timeout` several times over.
- **The TCP connect syscall has its own timeout, the OS's.**
  `qdb/network/client.cpp`, `client::connect`: `sync_connect()` runs
  first, then `set_blocking_timeout(_timeout)` ("it might be ignored
  before"). A closed port fails at once (RST); a black-holed address
  (firewall DROP, dead host) blocks for the OS connect timeout, about
  two minutes on Linux. Everything after the connect (handshake, ring
  fetch) runs under the socket timeout, a few round trips in all.
- **`qdb_close()` can block for minutes.** The destructor joins the
  handle's dedicated thread pool (asynchronous client push tasks, each
  bounded only by the socket timeout) and every multiplexer thread.
  Close runs on a goroutine the server is willing to abandon. Under the
  static link (Linux) closing the last handle leaves the library
  initialized; under the shared library (every other platform) it calls
  `qdb_uninitialize_api`, and the next dial re-initializes it.
- **Each handle is itself a pool, so there are two layers of pooling.**
  A handle owns a thread pool of `client_max_parallelism` threads (0 =
  half the logical cores), created before connect, and per node address
  a pool of TCP connections bounded by the soft limit below. Those are
  the C API's pools, inside one handle, sized by the per-handle knobs.
  The pool this plan builds is a pool of sessions above them: N
  sessions, each owning one handle with its own threads and connections.
  Sixty four sessions at the default on a 32-core host is about a thousand
  threads. `max_in_buffer_size` is a per-handle cap as well.
- `qdb_option_set_connection_per_address_soft_limit` exists ("maximum
  number of connections for a given IP address that this handle should
  have"; soft, may be exceeded temporarily), accepts 2..100000, and is
  split in half between the sync and async client pools per endpoint.
  There is no client-side "max out buffer size"; the out buffer is
  fixed at 256 MiB (`api/src/handle.hpp`). `client_max_parallelism`
  must be set before connect.

qdbd, verified against the test cluster on 2026-08-28:

- **The session pool is finite and exhaustion is punished.** With the
  test configuration qdbd accepted about 640 concurrent sessions, then
  logged `out of free sessions, trying again in 900000ms` and stopped
  accepting for fifteen minutes, sessions freed in the meantime or not.
  A handle holds its sessions until `qdb_close` returns, so closes in
  flight count: that is why the budget is released when a close
  completes (ADR-0003), and why every test bounds the sessions it holds
  open, closes included, at every step.

The old server opened a fresh session on every readiness probe and ran
`Statistics()` unless `readiness_query` was set; it pooled sessions with
`silenceper/pool` keyed by username. The per-probe session survives; the
rest does not.

## Configuration

Three new blocks; every key at its default. Durations are Go duration
strings; sizes are bytes.

```yaml
cluster:
  # Cluster URI; several nodes as a comma-separated list.
  uri: "qdb://127.0.0.1:2836"
  # Cluster public key file; empty means an insecure cluster.
  public_key_file: ""
  # User security file of the REST API's own user, the one it
  # authenticates as on its own behalf (the readiness probe). Required
  # when the cluster has a public key; empty means anonymous.
  user_security_file: ""
  # Client-side C API compression: none | balanced (the binding exposes
  # only these two).
  compression: "none"
  # Client-cluster traffic encryption: none | aes.
  encryption: "none"
  # C API socket timeout per session, whole seconds, at least 1s (per
  # send/receive, retried inside the C API); the operation-level bound
  # is pool.call_timeout.
  timeout: "60s"
  # Per session, 0 = C API default: client input buffer cap (bytes) and
  # C API worker threads (the C API default is half the logical cores,
  # per session).
  max_in_buffer_size: 0
  parallelism: 0
  # Per session: soft limit on connections to any one node address
  # (0 = C API default; otherwise 2..100000).
  connections_per_address: 0
pool:
  # Sessions this process may hold across all users; readiness probes
  # dial outside this budget.
  max_sessions: 64
  # Sessions one user may hold, across all of that user's REST sessions;
  # the anonymous user is one user.
  per_user_max: 8
  # A session unused this long is closed; a user pool that has held no
  # session for this long is evicted (so at most twice this after the
  # last request).
  idle_timeout: "5m"
  # A session older than this is closed on return, never on checkout.
  max_lifetime: "15m"
  # Deadline for one C API call, dial included; on expiry the caller
  # gets an error and the session is discarded.
  call_timeout: "60s"
  breaker:
    # Consecutive retryable failures (dial errors, deadline expiries,
    # retryable C API errors) that open the breaker.
    failures: 3
    # How long the breaker stays open before one call is let through.
    open_for: "10s"
status:
  # Query executed by the readiness probe, as the REST API's own user,
  # after the dial; result discarded.
  readiness_query: "SELECT 1"
```

Mechanics:

- Precedence and `${VAR}` interpolation as today (`internal/config`).
  The loader is koanf over stdlib `flag`: `Default()` marshalled to YAML
  is the bottom layer, then the file, then the environment, then the
  flags actually passed; every key path comes from one reflection walk
  over the config struct's `yaml` tags, which also carry the `help` text
  the usage prints. The environment variable is the path upper-cased
  with dots as underscores (`QDB_REST_CLUSTER_USER_SECURITY_FILE`); the
  flag is the path with dots and underscores as hyphens
  (`--cluster-user-security-file`); `--cluster` and
  `--user-security-file` are the `qdbsh` aliases. Typed values are
  parsed at the layer that carries them, so an error names the variable
  or the flag. `Config` stays comparable with `==` (the tests depend on
  it): `uri` is one string, never a slice. The layer-fold property test
  draws every key in every layer from the same walk.
- Validation, only what the binding does not check when a session is
  dialed: vocabulary checks on `compression`/`encryption`;
  `cluster.timeout` a whole number of seconds, at least 1 (the C API
  rejects less and truncates the rest; the binding only requires a
  positive value); `max_in_buffer_size` not negative;
  `per_user_max <= max_sessions`; `readiness_query` non-empty. No
  ordering between `call_timeout` and `cluster.timeout` is required (one
  bounds a syscall, the other an operation). The URI scheme and the knob
  ranges are the C API's, judged at dial, so a mistake there surfaces on
  the first dial, not at startup; eager validation is deliberately
  absent.
- `examples/qdb_rest.yaml` gains the blocks verbatim, pinned to the
  defaults by the existing test.
- Types holding the user secret and the cluster key implement
  `slog.LogValuer` (`internal/AGENTS.md`).

## The pool

The core is the binding's: `qdbapi.SessionPool` leases `qdbapi.Session`s
(connected handles) dialed by a `SessionFactory`, bounded per pool, with
idle and lifetime ages, its own reaper, and dialer and closer hooks
(Verified facts). This layer builds one such pool per user and adds what
is REST-specific: the user map, the global budget, the breaker, the
deadline guard over cgo, and the `Session` wrapper that keeps the
binding's handle inside this package.

### `internal/qdb` -- the REST layer

- **User**: `{Username, SecretKey string}`; the pool key is
  `(cluster, username)` (ADR-0003). REST sessions and tokens are auth's
  concern and never reach this layer: every REST session of a user
  shares the user's pool. Until auth exists, the REST API's own user is the only
  user; anonymous is `{"", ""}`.
- **Cluster**: the `cluster:` block turned into per-user
  `HandleOptions`, a `SessionPool` per user (`per_user_max`, the ages,
  the cluster clock, the budgeted dialer and closer; a rejected option
  is a programming error, config validation having bounded them all),
  the breaker, the global budget, and the map of user pools. Looking up a
  user finds its pool or creates one; nothing ever replaces a pool
  that exists.
- **Budget**: one weighted semaphore of `max_sessions`, taken in the
  pool's dialer before a session is dialed and released in its closer
  once `qdb_close` has returned -- so a session whose dial or call was
  abandoned on deadline, which still holds cluster connections and
  threads until the C call returns, keeps counting until then. The
  per-user cap is the user pool's `max_sessions`.
- **Eviction**: a user pool that has held no conns for `idle_timeout`
  is closed and removed (LRU by last release); in-flight leases are
  never revoked. Each pool's own reaper closes its idle sessions; the
  cluster's ticker (`evictInterval`, 10 s, a constant, not a config
  key) only evicts pools that have held nothing for `idle_timeout`,
  closing them so their reapers stop. The map is bounded by the number
  of distinct users, not logins.
- **Breaker**: per cluster; counts consecutive retryable failures from
  dials and calls (`IsRetryable`, which includes Go-side deadline
  expiries), opens for `open_for`, half-open lets one call through
  while the rest fail fast, a success closes it. While open, `Call`
  fails immediately with `ErrBreakerOpen` carrying the remaining open
  time (the HTTP layer maps it to `503` + `Retry-After`). Consequence,
  accepted: `breaker.failures` consecutive calls slower than
  `call_timeout` open the breaker for everyone for `open_for`, healthy
  cluster or not.
- **Call** is the only way code touches a session:

  ```go
  func (c *Cluster) Call(ctx context.Context, u User, op func(h *Session) error) error
  ```

  `Session` is this package's narrow wrapper around the leased
  `qdbapi.Session`, built per checkout and never returned. It has
  one method per C API operation the server uses -- this unit needs
  query execution and tag lookup; later units add table creation,
  batch push and cluster status as they need them
  -- and each method runs its C call on its own goroutine under the
  `ctx` that `Call` derived with `call_timeout`, on the failsafe below.
  A method whose result must be freed (`QueryResult`) hands it to a
  callback on the calling goroutine and releases it when the callback
  returns, so `QueryResult.Close` never leaves the package either.
  `Call` itself: acquire (breaker, budget, user pool, ctx wait; an
  acquire error feeds the breaker only when `IsRetryable` holds and the
  caller's own context did not end) -> run `op` on the calling goroutine -> on return, `qdbapi.IsRetryable(err)`
  decides: false (the request caused it) releases the session for
  reuse; true, or any non-`ErrorType` error, discards it and counts
  toward the breaker. A ctx that ends while a C call runs -- client
  gone, server draining -- is a deadline for the session's fate: the
  call cannot be stopped, so the session is discarded. Idempotent reads
  may ask for one transparent retry on a fresh session after a
  retryable error (`Retry: true` option); ingestion never does (brief).

- **The failsafe**: when the deadline fires with a method's C call
  still blocked in cgo, the method returns `ErrCallTimeout` at once
  and poisons the `Session`: every later method on it returns
  `ErrCallTimeout` without touching cgo, so `op` unwinds quickly. The
  goroutine running the C call is abandoned and, when the call finally
  returns, releases any result and discards the lease itself, which
  closes the session. Nothing else ever touches that session again:
  closing it from another thread is use-after-free (Verified facts),
  and there is no cancellation path. To the core the lease is simply
  still in use; the REST layer counts such leases as `wedged` in its
  own stats, next to the budget in use, so a cluster that stops
  answering shows up as a draining budget while the breaker fails the
  rest fast.
- **Dial under the same failsafe**: the dial runs through the same
  abandon-on-deadline mechanism, bounded by `call_timeout`. A dial
  abandoned on deadline returns an error to the core (no slot consumed
  there) while the abandoned goroutine keeps the budget unit until
  `NewSession` returns and has closed the handle. A
  black-holed address therefore pins one budget unit per attempted dial
  for the OS connect timeout; the breaker stops new attempts after
  `breaker.failures` of them.
- **Shutdown**: `Cluster.Close(ctx)` drains every user pool within
  the `shutdownGrace` already in `cmd/qdb_rest` (8 s, under the
  harness's 10 s SIGTERM-to-SIGKILL window) and returns `ctx.Err()` for
  whatever is still wedged; the process exits regardless.
- Logging: the pool logs nothing yet; when a line is added (dial,
  discard and eviction at debug, breaker transitions at warn, with `Err`
  for the cause), its attribute key is added to `observe` with it
  (`internal/AGENTS.md`). `cmd/qdb_rest` installs the adapter
  over the process logger with `qdbapi.SetLogger` before the first
  dial; the binding's Info maps to Debug. The adapter holds the
  process logger by value: the binding's `Logger` interface carries no
  context.
- **Context**: `qdb.WithCluster(ctx, c)` / `qdb.ClusterFrom(ctx)` carry
  the cluster the way `observe` carries the logger; `cmd/qdb_rest` puts
  it in the process context that `BaseContext` hands every request, so
  handlers need no constructor injection.

## Readiness

The contract is ADR-0004; the mechanics here. `GET /api/status/readiness`
(and the `/api/v2` mirror) calls `Cluster.Probe(ctx)`, which never
touches the pool, the budget or the breaker:

1. Dial a fresh session as the REST API's own user under `pool.call_timeout`,
   through the same abandon-on-deadline mechanism as `Call`, wrapped in
   the same `Session`.
2. Run `status.readiness_query` (default `SELECT 1`) and release its
   result at once. The dial proves the cluster is reachable and the
   REST API's own user authenticates; the query proves the session serves one.
3. Close the session on its own goroutine, whatever the outcome.
4. Success: `200`, empty body, no `Content-Type` (golden 21). Failure:
   `503`, empty body, no `Retry-After`; the cause goes to the log line,
   not the wire.

Liveness stays as it is.

## Tests

Integration tests in Go, run by `go test ./...`, against the live qdbd
pair of `scripts/tests/setup/start-services.sh` (insecure `2836`,
secure `2838` with `cluster_public.key` / `user_private.key`, written
into the directory `start-services.sh` is run from -- the repo root,
where they are gitignored -- the same fixture `qdb-api-go`'s own tests
use). They fail fast with the `start-services.sh` hint when the port
does not answer; nothing is skipped under `-short`.

- `internal/qdb`: per-user cap under concurrent calls and two logins of
  one user sharing one pool; breaker opening against an unreachable
  URI; retry-once after a retryable failure; a fatal error returning
  the session to the pool; LRU eviction (fake clock; the test runs the
  pool's reap pass itself instead of waiting for its tick); the
  secure-cluster dial as the REST API's own user. The failsafe is
  pinned on `guard` alone, with no C call: which side owns the outcome
  when the work finishes first and when the deadline fires first. The
  stall the deadline races against is not exercised -- that would test
  the C API's blocking behaviour, and the C API offers no way to mock a
  stall or an error (`internal/AGENTS.md`, Tests).
- `internal/httpapi`: readiness `200` against the live cluster, `503`
  against an unreachable cluster, no `Retry-After` either way.
- e2e: golden 21 stays green; `make test-legacy` unchanged.

CI (owner decision): `go test` depends on the database, so the build
step provisions qdbd exactly as `qdb-nats-connector`'s
`.buildkite/steps/_build.yml` does: `bash
scripts/tests/setup/start-services.sh` as the first command, the
`*-server.tar.zst` and `*-utils.tar.zst` archives added to the
`qdb-artifacts` download block next to the C API, a
`.buildkite/hooks/pre-exit` that runs `stop-services.sh` (idempotent),
and the step timeout raised from 30 to 60 minutes.
`scripts/cicd/30.test-unit.sh` drops `-short` (nothing skips) and is
renamed to say what it runs. One suite, no `-tags=integration` split:
every test assumes the service. The e2e harness stays out of CI. Four
recorded facts change with this and are rewritten in the same commit:
`.buildkite/AGENTS.md` ("only the c-api archive is downloaded"), the
comment in `.buildkite/steps/_build.yml`, the header of
`30.test-unit.sh` ("unit tests only"), and the Tests section of
`internal/AGENTS.md` (a bare `go test ./...` needs qdbd).

## Implementation order

Small commits, each green on its own:

1. `qdb-api-go` bumped to the upstream that carries the session factory
   and the pool.
2. `internal/config`: typed env machinery; `cluster:`, `pool:`,
   `status:` blocks; validation; example config and its pinned test.
3. `internal/qdb`: the user, the `Session` wrapper, dial, budget,
   breaker, `Call`, `Probe` on the binding's pool; integration tests.
4. `internal/httpapi`: readiness on `Probe`; `503` shape; tests.
5. `cmd/qdb_rest`: install the logger adapter, construct the cluster
   from config, close on shutdown.
6. CI: qdbd in the build step (Tests).
7. ADR-0003 and ADR-0004 accepted; `docs/log.md` Current state
   updated; this plan deleted with its facts moved (`docs/AGENTS.md`,
   Plans).

## Handoff notes for later units

- `cluster.max_in_buffer_size` at the C API default (256 MiB) cannot
  return the 5.6M-row `SELECT *` the bench runs; the old server's e2e
  flags raise it to 8 GiB (`tests/e2e/Makefile`). `legacy@new-rest`
  needs the same; an oversized reply (`ErrNetworkInbufTooSmall`) is
  fatal in the binding, so it costs no reconnect and M2 only has to map
  it to a client error.
- Auth's session id claim is a security abstraction (logging out a
  user's other sessions, later revocation); it never reaches
  `internal/qdb`. `/api/login` for a known user finds the existing pool
  and dials nothing.
