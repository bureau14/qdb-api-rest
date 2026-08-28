# ADR-0003: QuasarDB session pool: checkout model, budget, failsafe

Status: proposed
Date: 2026-08-25
Milestone: M1

## Context

Every request the server serves runs one or more C API calls on a
`qdb_handle_t` authenticated as the calling user. The brief makes
connection reuse non-optional, sets a global session budget partitioned
per user, and wants honest fast failure (brief: "Resilience and
connection management"). The C API constrains the mechanism harder than
the brief assumes (verified in `~/git/quasardb`; details in
`docs/pool-plan.md` while it is alive, then here):

- There is no cancellation path for an in-flight call.
  `qdb_option_set_timeout` is a per-syscall socket timeout (whole
  seconds, at least one) that the C API retries around, and it does not
  cover the TCP connect syscall itself, which the OS bounds separately.
  Only a Go-side deadline bounds an operation.
- `qdb_close()` from another thread while a call is in flight is
  use-after-free, so is a double close, and `qdb_close()` may itself
  block for minutes (it joins the handle's worker threads).
- Nothing documents `qdb_handle_t` as thread-safe. Some operations on a
  connected handle happen to be guarded; configuration, `connect` and
  `qdb_get_last_error` (per handle, not per thread) are not. We treat
  every operation on a handle as non-thread-safe.
- `qdb-api-go` is vendored and never patched; it has no pool and
  classifies its errors with `IsRetryable`.
- A QuasarDB user has exactly one secret. Every login of a user, from
  any client, dials with the same credentials.

## Decision

1. **The pool key is the user.** Sessions are pooled per user, keyed by
   `(cluster, username)`, under one process-wide `max_sessions` budget
   and a per-user cap; idle user pools are LRU-evicted. Every REST session
   and every token of one user share that user's pool. Login finds the
   existing pool or creates one; it never replaces or drains one, so
   in-flight requests are never raced. The REST session id claim in tokens is
   a security abstraction (for example, logging out a user's other
   sessions), never a pool key. Nothing dials at startup; every session
   is dialed on demand, and the process starts with the cluster
   unreachable (only configuration errors refuse a start).
2. **Checkout through a narrow wrapper.** A session is used by exactly
   one goroutine at a time, obtained only through
   `Cluster.Call(ctx, user, op)`. `op` receives a `Session` type
   owned by `internal/qdb` that exposes one method per C API operation
   the server uses; each method runs its C call under the deadline and
   the error classification below, and owns the release of any result.
   The binding's `HandleType` never leaves `internal/qdb`. Adding an
   operation means adding a wrapped method, never exposing the raw
   session.
3. **Deadline over cgo, abandon-and-close.** Every `Call` runs under
   `pool.call_timeout`. When it fires with a C call in flight, the
   wrapped method returns an error at once and poisons the `Session`
   (every later method on it fails without touching cgo); the goroutine
   blocked in cgo is abandoned and closes the handle itself when the C
   call returns. A session is never closed from another goroutine, never
   cancelled, and never reused after a timeout or a cancelled context.
   Dial runs under the same mechanism.
4. **`IsRetryable` decides the session's fate.** A call that fails with
   an error `qdb-api-go` classifies as retryable -- every C API error
   it does not call fatal, plus anything that is not a C API error at
   all, deadline expiry included -- discards the session and counts
   toward the breaker; a fatal error (the binding's list: not
   implemented, incompatible type, uninitialized, out of bounds,
   invalid query, alias not found, alias already exists, invalid
   argument) says nothing about the session, which is reused.
   Idempotent reads may retry once on a fresh session after a retryable
   error; ingestion never retries.
5. **Closing never blocks the pool.** Every close runs on its own
   goroutine; the budget slot is released when the close completes, so
   wedged sessions keep counting against `max_sessions` and are reported
   separately.
6. **Breaker per cluster**, fed by retryable failures from dials and
   calls: opens after `breaker.failures` consecutive failures for
   `breaker.open_for`, half-open admits one call. While open, calls
   fail immediately with a retry-after hint.

The readiness probe is outside this mechanism entirely: ADR-0004.

## Consequences

- No request can hang on a dead cluster: the deadline bounds it, the
  breaker shortens everything after the first few.
- Degraded sessions are contained: at worst a wedged session pins one
  budget slot and one C API thread set until the C call returns, which
  the C API's own socket timeouts eventually force -- except for the
  TCP connect to a black-holed address, which the OS releases after
  its own connect timeout (about two minutes on Linux).
- The error matrix is upstream's: an error it calls retryable costs a
  reconnect even when the request caused it (an oversized reply,
  `ErrNetworkInbufTooSmall`, and a permission or quota error are the
  notable cases), and consecutive calls slower than `call_timeout`
  open the breaker for everyone for `open_for`, healthy cluster or not.
- A cluster that stops answering shows up as budget draining toward
  zero plus an open breaker; it never shows up as goroutines piling
  behind cgo.
- The number of pools is bounded by the number of distinct users, not
  by the number of logins; a user with many clients behind one gateway
  holds at most the per-user cap.
- Every C API operation the server uses is a method on `Session`, so the
  failsafe and the error classification cannot be bypassed and the
  surface the server depends on is enumerable in one file.
- Operations cannot be cancelled early; the only lever is the size of
  `call_timeout` per call class, which later units set per endpoint.
- The abandon path is not exercised against the C API: doing so means
  stalling a real C call, which tests the C API rather than this code.
  The guard's ownership rules are pinned on their own; the C-side facts
  they rely on are the ones in Context. Accepted.

## Alternatives rejected

| Alternative                                             | Why not                                                                             |
| ------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| Pools keyed per REST session, as the brief first had it | N tokens of one user would hold N pools of identical sessions; no isolation gained  |
| Replace the user's pool on re-login                     | the old server's race against in-flight requests                                    |
| One handle per user shared by concurrent goroutines     | thread-safety undocumented, configuration racy, last-error per handle; owner rule   |
| `op` receives the binding's `HandleType`                | nothing stops `op` from stashing it; the deadline would cover `op`, not each C call |
| `HandleType` plus a lint rule confining the import      | catches the import, not the misuse; the failsafe still would not wrap each call     |
| Close the handle from the timing-out goroutine          | use-after-free in the C API                                                         |
| A keep-list of request-caused errors maintained here    | duplicates upstream's matrix and drifts from it                                     |
| Discard on every error                                  | a reconnect per user mistake                                                        |
| Free the budget slot when a call times out              | the session still holds cluster connections and threads; the budget would lie       |
| Per-checkout health ping                                | a cluster round trip per request (brief)                                            |
| A warm-up session dialed at startup                     | needs a retry mechanism of its own; buys milliseconds on the first request          |
| Fail the start when the cluster is unreachable          | systemd restart loop; k8s expects not-ready                                         |
| Capping concurrent dials against a black-holed address  | complexity for a case the breaker already bounds; the connect timeout is upstream's |
