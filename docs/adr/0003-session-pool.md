# ADR-0003: QuasarDB session pool: checkout model and budget

Status: accepted
Date: 2026-08-25
Milestone: M1

## Context

Every request the server serves runs one or more C API calls on a
`qdb_handle_t` authenticated as the calling user. The brief makes
connection reuse non-optional, sets a global session budget partitioned
per user, and wants honest fast failure (brief: "Resilience and
connection management"). The C API constrains the mechanism harder than
the brief assumes (verified in `~/git/quasardb`, 2026-08-25/26; qdbd
verified against the test cluster, 2026-08-28):

- There is no cancellation path for an in-flight call, and the binding
  has no `context.Context` plumbing. What bounds a call is
  `qdb_option_set_timeout`: a per-syscall socket timeout (whole
  seconds, at least one) that the C API retries around. It does not
  cover the TCP connect syscall itself, which the OS bounds separately.
- `qdb_close()` from another thread while a call is in flight is
  use-after-free, so is a double close, and `qdb_close()` may itself
  block for minutes (it joins the handle's worker threads).
- Nothing documents `qdb_handle_t` as thread-safe. Some operations on a
  connected handle happen to be guarded; configuration, `connect` and
  `qdb_get_last_error` (per handle, not per thread) are not. We treat
  every operation on a handle as non-thread-safe.
- `qdb-api-go` is vendored and never patched; it provides the session
  factory and a bounded `SessionPool` with dialer and closer hooks, and
  classifies its errors with `IsRetryable`. The global budget, the
  breaker and the per-user map are this layer's.
- A QuasarDB user has exactly one secret. Every login of a user, from
  any client, dials with the same credentials.
- Each handle is itself a pool: a thread pool of `client_max_parallelism`
  workers (0 = half the logical cores) plus per-node-address TCP
  connection pools, created at connect. Sixty-four sessions at the
  defaults on a 32-core host is about a thousand threads; the session
  pool sits above those, not instead of them.
- qdbd's own session pool is finite and exhaustion is punished: the test
  configuration accepted about 640 concurrent sessions, then logged
  `out of free sessions` and refused new ones for fifteen minutes. A
  handle holds its qdbd sessions until `qdb_close` returns, so closes in
  flight count against the cluster.

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
   `Cluster.Call(ctx, user, f)`. `f` receives a `Session` type
   owned by `internal/qdb` that exposes one method per C API operation
   the server uses; each method runs its C call under the error
   classification below and owns the release of any result. The
   binding's `HandleType` never leaves `internal/qdb`. Adding an
   operation means adding a wrapped method, never exposing the raw
   session.
3. **`IsRetryable` decides the session's fate.** A call that fails with
   an error `qdb-api-go` classifies as retryable -- every C API error
   it does not call fatal, plus anything that is not a C API error at
   all -- discards the session and counts toward the breaker; a fatal
   error (the binding's list: not implemented, incompatible type,
   uninitialized, out of bounds, invalid query, alias not found, alias
   already exists, invalid argument, network inbuf too small) says
   nothing about the session, which is reused.
   Idempotent reads may retry once on a fresh session after a retryable
   error; ingestion never retries.
4. **Closing never blocks the pool.** Every close runs on its own
   goroutine; the budget slot is released when the close completes, so
   a session counts against `max_sessions` until its `qdb_close` has
   returned.
5. **Breaker per cluster**, fed by retryable failures from dials and
   calls: opens after `breaker.failures` consecutive failures for
   `breaker.open_for`, half-open admits one call. While open, calls
   fail immediately with a retry-after hint.

The readiness probe is outside this mechanism entirely: ADR-0004.

## Consequences

- A call is bounded by the C API's own socket timeouts
  (`cluster.timeout`), never cancelled from Go; the TCP connect to a
  black-holed address is bounded by the OS connect timeout (about two
  minutes on Linux).
- A cluster that stops answering costs each in-flight call its socket
  timeout; after `breaker.failures` of those the breaker fails
  everything fast for `open_for`, and the budget bounds what the
  process holds meanwhile.
- The error matrix is upstream's: an error it calls retryable costs a
  reconnect even when the request caused it (a permission or quota
  error are the notable cases).
- The number of pools is bounded by the number of distinct users, not
  by the number of logins; a user with many clients behind one gateway
  holds at most the per-user cap.
- Every C API operation the server uses is a method on `Session`, so the
  error classification cannot be bypassed and the surface the server
  depends on is enumerable in one file.

## Alternatives rejected

| Alternative                                             | Why not                                                                               |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| A pool of our own, upstream-shaped, moved later         | the binding's pool is the same design and is maintained with the binding              |
| Pools keyed per REST session, as the brief first had it | N tokens of one user would hold N pools of identical sessions; no isolation gained    |
| Replace the user's pool on re-login                     | the old server's race against in-flight requests                                      |
| One handle per user shared by concurrent goroutines     | thread-safety undocumented, configuration racy, last-error per handle; owner rule     |
| `f` receives the binding's `HandleType`                 | nothing stops `f` from stashing it past the checkout                                  |
| `HandleType` plus a lint rule confining the import      | catches the import, not the misuse; the error classification would not wrap each call |
| A keep-list of request-caused errors maintained here    | duplicates upstream's matrix and drifts from it                                       |
| Discard on every error                                  | a reconnect per user mistake                                                          |
| Per-checkout health ping                                | a cluster round trip per request (brief)                                              |
| A warm-up session dialed at startup                     | needs a retry mechanism of its own; buys milliseconds on the first request            |
| Fail the start when the cluster is unreachable          | systemd restart loop; k8s expects not-ready                                           |
| Capping concurrent dials against a black-holed address  | complexity for a case the breaker already bounds; the connect timeout is the OS's     |
