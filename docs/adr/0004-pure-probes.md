# ADR-0004: Probes and observability endpoints stay pure

Status: proposed
Date: 2026-08-27
Milestone: M1

## Context

Load balancers at customer sites and orchestrators poll
`/api/status/liveness` and `/api/status/readiness` on their own cadence
(brief: "Compatibility contract"); M2 adds Prometheus `/metrics`. The
resilience machinery of ADR-0003 carries state -- breaker, budget, pool
occupancy -- that a probe could consult (report the breaker as
readiness) or feed (count probe failures toward the breaker), and a
probe verdict is cheap to cache. Each of these makes the endpoint report
something other than what it is named for: a memory instead of the
present, load instead of reachability, or the prober's cadence driving
the serving path. The old server's readiness ran a full statistics scan
per probe and answered `500` with a JSON body on failure.

## Decision

Monitoring endpoints are pure: each answers exactly the question it is
named for, by doing the real thing, with no side effect on the serving
path and no dependency on its state.

1. **Liveness** is the canonical k8s definition: the process serves
   HTTP. It is never cluster-aware.
2. **Readiness** dials a fresh handle as the service user under
   `pool.call_timeout` (the abandon-on-deadline mechanism of ADR-0003),
   runs one check -- `status.readiness_query` if configured, otherwise
   `qdb_wait_for_stabilization` -- and closes the handle on its own
   goroutine. Success is `200`, failure `503`, both with an empty body
   and no `Retry-After`; the cause goes to the log line, not the wire.
   The probe never touches the pool, the budget or the breaker, neither
   reading nor feeding them, and nothing is cached: every poll performs
   the call. The process starts and reports not-ready while the cluster
   is unreachable.
3. **`/metrics`** (M2) reads in-process counters only. A scrape never
   triggers cluster work, is not gated on auth or on cluster state, and
   the only switch is the configuration key that disables the endpoint.

## Consequences

- A readiness `200` proves the cluster is reachable from this instance,
  now, and that the service user authenticates. It says nothing about
  pool capacity; capacity is reported on real requests (`429`/`503`)
  and, from M2, on `/metrics`.
- Each concurrent prober costs one transient handle outside
  `max_handles` -- the old server's cost. The operator's `readiness_query`
  defines what "ready" means for their workload; the default is one
  remote round trip.
- The prober's cadence never drives resilience, and an open breaker
  never masks a cluster that has recovered.
- A `/metrics` scrape is safe during an outage and under overload.
- The old server's `200/500` readiness contract is broken deliberately
  (brief: "Compatibility contract").

## Alternatives rejected

| Alternative                                           | Why not                                                                              |
| ----------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Cluster-aware liveness                                | the orchestrator would restart a healthy gateway on cluster trouble                  |
| Readiness on a pooled handle, or with a reserved slot | a pooled handle proves the cluster was reachable when dialed; the slot needs warm-up |
| Readiness consulting the breaker                      | reports a memory, and an open breaker outlives the outage by `open_for`              |
| Readiness feeding the breaker; `Retry-After` on it    | the prober's cadence would drive resilience                                          |
| Cached verdict (TTL, singleflight)                    | lies for its TTL; a probe must be light enough to run on every poll                  |
| `Statistics()` as the default check                   | a full stat-key scan per probe                                                       |
| `500` with `{"message": ...}`                         | probes and load balancers understand `503` as "not ready"                            |
| `/metrics` behind auth, or gathering cluster stats    | a scrape must be free and must work when the cluster does not                        |
