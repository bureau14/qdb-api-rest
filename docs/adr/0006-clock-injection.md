# ADR-0006: The clock is an injected function, not context or synctest

Status: accepted
Date: 2026-09-01
Milestone: M1

## Context

Two packages hold wall-clock-dependent logic that tests must pin:
`internal/qdb` (session ages, breaker windows) and `internal/auth`
(token expiry). Both take `now func() time.Time` at construction
(`qdb.New`, `auth.New`) and hold it as a field; tests pass a fixed
instant, `main` passes `time.Now`.

Time is the only dependency of its kind in this codebase, not the first
of a family. The other injectable things already have homes: the logger
travels in the context by ADR-0002; components (cluster, keychain) are
constructed in `main` with named arguments and travel in the context;
randomness is deliberately not injectable (property tests pin
roundtrips, never ciphertext bytes, and a swappable RNG in a crypto
path is a bug vector, not a seam); the filesystem and environment are
parameterized at config load only. Time is uniquely awkward because it
is ambient global state in the stdlib that pure-ish logic genuinely
depends on -- which is why the stdlib grew two mechanisms for it and
nothing comparable for anything else: `tls.Config.Time` (injection for
wall-clock-dependent verification) and `testing/synctest` (a fake clock
for timer-driven concurrency).

## Decision

Wall-clock reads are injected as `now func() time.Time`, a named
constructor argument stored as a field -- the `tls.Config.Time` idiom.
`main` passes `time.Now`; tests pass a fixed instant. This is the house
idiom for every component whose logic reads the clock.

Scalability is a non-concern by construction: the pattern has exactly
one member, and a hypothetical second ambient dependency gets the same
answer -- a named field on the component that needs it, set by its
`New`. No shared clock package, no dependency registry, until a third
consumer exists; a little copying beats a little dependency.

`testing/synctest` is not adopted now, but is the designated tool for a
future pure-Go unit built on internal timers with no live-qdbd
dependency (an admission queue, a revocation cache). Using synctest
there, alongside injection for plain wall-clock reads, is the stdlib's
own division of labor, not a second competing idiom.

## Consequences

- Every clock-dependent component is deterministic under test with one
  short argument at its construction site; the dependency is visible in
  the signature, per explicit-over-implicit.
- Property tests draw arbitrary absolute instants -- before or after a
  fixed epoch, both directions -- as plain integers.
- A reader can trust that `time.Now()` appearing inline (as in HTTP
  handlers stamping `iat`) is deliberate: it means the value is data in
  flight, not logic under test; logic that branches on time takes the
  injected clock.

## Alternatives rejected

| Alternative                       | Why not                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Clock in the context              | The context carries request-scoped values crossing API boundaries; a clock is a process-wide dependency that never varies per request. In the context it becomes invisible at call sites, any caller can silently change what time it is, and every read pays a type-assertion with a fallback-or-panic choice. ADR-0002 earned its context slot with a written justification; time has no comparable case.                                                                                                                                                              |
| `testing/synctest` now            | Its bubble clock starts at a fixed epoch and moves forward only, so it cannot express the arbitrary and backward instants our expiry properties draw; one bubble around `rapid.Check` shares monotonic time across iterations (cases stop being independent, shrunk counterexamples replay at different instants), while per-iteration bubbles route failures past `rapid`'s shrinker; and blocking on real network or cgo is not "durably blocked", so the fake clock stalls in `internal/qdb`'s live-qdbd tests, which ADR-0003's no-mocking doctrine makes mandatory. |
| A `Clock` interface (clockwork)   | Earns its weight only when timers, tickers and sleeps must be controlled; our components read `Now` and nothing else. A third-party dependency for one method.                                                                                                                                                                                                                                                                                                                                                                                                           |
| A shared clock package / registry | A `Deps` grab-bag or service locator for a family of one; scales worse than the problem it solves.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
