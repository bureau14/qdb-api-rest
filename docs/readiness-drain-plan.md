# M6: readiness answers 503 for the whole drain -- Plan

Status: approved

## Outcome

When this unit lands:

- From the first moment of the shutdown drain, `GET /api/status/readiness`
  and its `/api/v2` mirror answer `503` with an empty body and no
  `Retry-After`, without dialing the cluster, on every shutdown path: a
  SIGINT or SIGTERM, and a listener that fails. Liveness is unchanged.
- The drain signal travels on the request context as a value
  `internal/httpapi` owns, placed by the entry point and read by the
  readiness handler. A request context without the signal is a
  programming error and the reader panics, as `ClusterFrom` and
  `Logger` do, so a wiring mistake in the entry point is loud at the
  first probe and never a server that dials through its drain.
- Every HTTP request derives its context from the process context's
  values and not from its cancellation, so the signal cuts no stream in
  flight and the grace of `listen.shutdown_grace` is what ends one.
- The readiness tests pin, on both clusters, `200` while the signal is
  open and `503` once it has fired.
- `internal/httpapi/AGENTS.md` and `cmd/qdb_rest/AGENTS.md` carry the
  two rules, and `docs/log.md` Next is re-cut into the slices that
  follow.

Left for later units: the systemd notify protocol (`READY=1` once every
listener is bound, `STOPPING=1` when the drain starts), which needs the
HTTP bind moved out of `ListenAndServe`; the stress's drain step and the
close of M6; the `WATCHDOG=1` keepalive, which is open (owner,
2026-10-26).

## Verified facts

- The readiness handler calls `Cluster.Probe` on every request and
  answers `503` on its error, logging the cause at warn
  (`internal/httpapi/httpapi.go:24-32`). `Probe` dials a fresh session
  as the REST API's own user and runs `status.readiness_query`
  (`internal/qdb/cluster.go:519-532`). Nothing tells the handler a
  drain has begun (2026-10-26).
- The handlers read the logger, the cluster and the keychain from the
  request context through unexported keys with a `With*` placer and a
  `*From` reader that panics on absence (`internal/observe/observe.go:79-99`,
  `internal/qdb/context.go`, `internal/auth/context.go:11-25`).
- The HTTP servers hand every request the process context through
  `BaseContext` (`cmd/qdb_rest/main.go:99`), and that context is the
  one `signal.NotifyContext` cancels on SIGINT or SIGTERM
  (`cmd/qdb_rest/main.go:231`). `net/http` derives each connection's
  and each request's context from the base context with its own cancel
  on top (`$GOROOT/src/net/http/server.go:3563-3604`, `:1089-1090`), so
  cancelling the base cancels every request in flight.
- Every encoder returns `ctx.Err()` at a batch boundary
  (`internal/encoding/arrow.go:55`, `csv.go:207`, `json.go:249`,
  `json.go:328`, `encoding.go:49`), and `writeClusterError` treats a
  context that has ended as a caller who left, with one debug line and
  nothing on the wire (`internal/httpapi/query.go:76-83`). So at SIGTERM
  an HTTP stream in flight is cut at its next batch and the body is
  truncated, while the brief promises the drain completes in-flight
  streams on both planes (`docs/brief.md`, Resilience and connection
  management). Flight is unaffected: its interceptors splice the
  process values onto the call's own context
  (`internal/flightsql/server.go:113-122`).
- `serveUntilDone` returns nil when the process context ends and the
  first error when a listener fails, and `main` then runs `shutdown`,
  which drains the listeners in parallel and the cluster second under
  the grace (`cmd/qdb_rest/main.go:168-203`, `:264-267`). On a listener
  failure the process context is still live when the drain runs, so
  readiness would dial through the drain on that path (2026-10-26).
- `context.WithoutCancel` keeps a parent's values and drops its
  cancellation (`$GOROOT/src/context/context.go:586`); `go.mod` is at
  Go 1.27.
- The readiness tests build the request context themselves through
  `probe`, with a discarding logger and a cluster built for a config,
  and run once per cluster through `qdbtest.Clusters`
  (`internal/httpapi/readiness_test.go:19-39`, `:44-61`). On a live
  cluster the probe answers `200` today (`:44-61`), so a `503` there
  under a fired signal proves the signal decided and not the dial.
- The probes stay outside the bearer and compression middleware
  (`internal/httpapi/AGENTS.md`, Middleware).
- ADR-0004 rule 2 says readiness answers `503` without dialing from the
  first moment of the drain, because the question it is named for has
  the answer no, and a load balancer that polls it takes the instance
  out of rotation before a listener closes (`docs/adr/0004-pure-probes.md`,
  Decision, rule 2). The brief says the same under Observability and
  logging and Resilience, and the M6 criteria repeat it (`docs/log.md`,
  Current state). All three were committed on the base at the start of
  this plan stage (owner, 2026-10-26).
- The harness's `stop_server` waits ten seconds above the eight-second
  default grace and sends SIGTERM to an idle server
  (`tests/e2e/common.sh`, `stop_server`), so no round-trip step observes
  a stream cut by the signal today.

## Design

In file order. The sketches carry the claims and their evidence; the
build writes the prose.

### `internal/httpapi/httpapi.go`

Before `handleLiveness`, after the imports:

```go
// The drain signal travels in the request context next to the logger
// and the cluster: the entry point places it, and the readiness handler
// reads it. [internal/qdb/context.go, the same pattern]
type drainKey struct{}

// WithDrain returns ctx carrying done, a channel that is closed once the
// server has begun its shutdown drain. [Rationale: signal on the context]
// The entry point passes the process context's Done channel, so the
// signal is the process context's end. [Rationale: Done as the signal]
func WithDrain(ctx context.Context, done <-chan struct{}) context.Context {
	// style: doc comment alone
	// because: one WithValue, like WithCluster
}

// draining reports whether the drain has begun. It panics on a context
// without the signal, because a request context the entry point did not
// build is a programming error, as it is for the cluster and the logger.
// [Rationale: panic on absence; internal/qdb/context.go:17-23]
func draining(ctx context.Context) bool {
	// style: doc comment alone
	// because: one type assertion with a panic and one non-blocking
	// receive, like ClusterFrom
	//
	// at the panic: the message names the package and the missing value,
	// as "qdb: no cluster in context" does [internal/qdb/context.go:21]
	// at the select: a receive with a default, because the handler must
	// answer at once whether or not the drain has begun [handleReadiness]
}
```

`handleReadiness`, replacing the existing doc comment and body:

```go
// handleReadiness reports whether this instance may take new traffic.
// It answers 200 or 503 with an empty body and no Retry-After, and the
// cause of a 503 goes to the log line and not the wire. [ADR-0004 rule 2]
func handleReadiness(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + numbered overview + step comments
	// because: two reasons to answer 503 with different log levels, and
	// the order between them is the point
	//
	// The drain is checked before the dial, because the answer during a
	// drain is no whatever the cluster would say, and a dial there would
	// cost a session for an answer already known. [ADR-0004 rule 2]
	//
	// 1. Answer 503 while draining, without a dial, because the question
	//    the probe is named for ("may this instance take new traffic")
	//    has the answer no, and a load balancer that polls it takes the
	//    instance out of rotation before a listener closes and a
	//    connection is refused. [docs/adr/0004-pure-probes.md, rule 2]
	//    The line logs at debug, because the drain is not a failure and
	//    a prober polls through the whole grace. [Rationale: debug]
	// 2. Dial as the REST API's own user and run the readiness query,
	//    caching nothing and bypassing the pool, the budget and the
	//    breaker; a failure is 503 with the cause at warn.
	//    [internal/qdb/cluster.go:519-532, ADR-0004 rule 2]
	// 3. 200 with an empty body.
}
```

`registerStatusRoutes`: the doc comment's last sentence gains "and
answers 503 without a dial while the server drains" (one claim,
`docs/adr/0004-pure-probes.md` rule 2).

### `internal/httpapi/readiness_test.go`

`probe` gains a third parameter:

```go
// probe runs one GET against the readiness route with a cluster built
// for cfg and the drain signal drain on the request context, and returns
// the response. Every caller passes a signal, because the handler panics
// without one. [Rationale: panic on absence]
func probe(t *testing.T, cfg config.Config, drain <-chan struct{}) *httptest.ResponseRecorder {
	// style: doc comment alone
	// because: a fixture that assigns
}
```

The two existing tests pass an open channel. `newServerOn`
(`internal/httpapi/query_test.go:65-68`) places an open signal on its
context too, so a future error row that reaches a probe does not panic
(one line, no comment beyond the line's own). One test joins the
readiness tests:

```go
// TestReadinessDuringDrain: on each reachable cluster the route answers
// 200 while the drain signal is open and 503 once it has fired, with an
// empty body and no Retry-After both times. The 503 on a live cluster
// proves the signal decided and not the dial. [readiness_test.go:44-61,
// ADR-0004 rule 2]
func TestReadinessDuringDrain(t *testing.T) {
	// style: doc comment + step comments
	// because: two probes whose difference is the channel's state
	//
	// 1. an open channel answers 200, because the signal's presence is not
	//    the drain, its close is [draining]
	// 2. a closed channel answers 503 with an empty body and no
	//    Retry-After [ADR-0004 rule 2]
}
```

### `cmd/qdb_rest/main.go`

`newListeners`: the overview gains a first step, and the others move
down one.

```go
func newListeners(ctx context.Context, cfg config.Config, handler http.Handler) ([]listener, error) {
	// style: doc comment + numbered overview + step comments (existing)
	//
	// 1. Derive the context every HTTP request inherits: the process
	//    values (logger, cluster, keychain) and the drain signal, without
	//    the process context's cancellation. [Rationale: WithoutCancel]
	//    - the signal cancels the process context at SIGTERM; a request
	//      context under it is cancelled with it, every encoder stops at
	//      its next batch, and the body is truncated, so the drain and not
	//      the signal must end a stream [internal/encoding/arrow.go:55,
	//      $GOROOT/src/net/http/server.go:1089]
	//    - the drain signal is the process context's Done channel, so the
	//      first moment of the drain is the moment the context ends
	//      [Rationale: Done as the signal]
	//    - rejected: a channel shutdown closes, because the event already
	//      exists [Rationale: Done as the signal]
	//    base := httpapi.WithDrain(context.WithoutCancel(ctx), ctx.Done())
	// 2. Load the TLS material once. (existing)
	// 3. Add HTTP, when configured, over base, with one warning when the
	//    cluster is secure. (existing, base replaces ctx)
	// 4. Add HTTPS over base. (existing)
	// 5. Add Flight. Flight takes ctx and not base, because its
	//    interceptors splice the process values onto each call's own
	//    context and it has no readiness route.
	//    [internal/flightsql/server.go:113-122]
}
```

`newHTTPServer`: its doc comment's `BaseContext` sentence says the
caller passes the request base context, which carries the process
values and the drain signal and outlives the signal
(`newListeners` step 1).

`main` step 6:

```go
	// 6. Serve until a signal or a listener failure. Then end the process
	//    context, so readiness answers 503 on the listener-failure path
	//    too, where no signal ended it [Rationale: stop before shutdown;
	//    cmd/qdb_rest/main.go:168-179], and drain the listeners first and
	//    the cluster second under listen.shutdown_grace. [existing]
	//    stop() is idempotent, so the deferred call stays. [os/signal]
	runErr := serveUntilDone(ctx, listeners)
	stop()
	shutdown(ctx, cfg.Listen.ShutdownGrace, listeners, cluster)
```

`shutdown` is unchanged. Its doc comment gains no sentence, because
the drain signal fires before it runs and it reads nothing of it.

### `internal/httpapi/AGENTS.md`, Middleware

After "The probes stay outside both.":

```
- Readiness reads the drain signal `WithDrain` places on the request
  context and nothing else of the lifecycle: once the channel is closed
  it answers 503 without a dial, at debug (ADR-0004, rule 2). A request
  context without the signal panics, as one without the cluster does,
  so every test fixture that builds a request context places one, open.
  The entry point places the signal (`cmd/qdb_rest/AGENTS.md`), and the
  Flight server does not, because no Flight call reads it.
```

### `cmd/qdb_rest/AGENTS.md`

A new bullet after the listeners bullet:

```
- Every HTTP request inherits the process values and the drain signal
  through a context without the process context's cancellation
  (`newListeners`), so SIGINT or SIGTERM cuts no stream in flight and
  `listen.shutdown_grace` is what ends one. The drain signal is the
  process context's end, which `main` brings about on a signal or a
  listener failure before the drain, so readiness answers 503 on both
  paths. Flight takes the process context itself, because its
  interceptors splice the values onto each call's own context.
```

### `docs/log.md`

Current state: In flight stays "Nothing". Next becomes:

```
1. `/rr-start` M6: the systemd notify protocol, `READY=1` once every
   listener is bound and `STOPPING=1` when the drain starts, written to
   `$NOTIFY_SOCKET` with the standard library and silent when the
   variable is unset; the HTTP bind moves out of `ListenAndServe` so
   READY follows the bind.
2. `/rr-start` M6: the stress's drain step (`docs/e2e.md`, Stress
   definition) green by hand on both clusters, with its server run
   under a long grace through `listen.shutdown_grace`, and the close of
   M6, as one slice.
```

The entry for the plan's deletion follows the template.

## Rationale

| decision                                                                                                         | why                                                                                                                                                                           | rejected, and why                                                                                                                                                                                                                        | gained                                                           | given up                                                                        | settled by                             |
| ---------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- | ------------------------------------------------------------------------------- | -------------------------------------- |
| Readiness answers 503 without a dial from the first moment of the drain                                          | the question it is named for has the answer no, and a load balancer polling it takes the instance out before a listener closes                                                | letting the dial decide through the drain: a `200` during the drain keeps new connections coming until they are refused                                                                                                                  | the instance leaves rotation before a connection is refused      | nothing                                                                         | `docs/adr/0004-pure-probes.md`, rule 2 |
| The drain signal is a value `internal/httpapi` places on the request context (`WithDrain`)                       | the owner's choice: the cluster binding stays free of lifecycle                                                                                                               | a flag on `qdb.Cluster` set by `shutdown`: puts a serving-lifecycle fact on the binding; the request context's own cancellation: conflates a client hangup with the drain and stops working once the request context outlives the signal | handlers read one more value the way they read the cluster       | one more process value wired in `main`                                          | owner, 2026-10-26                      |
| The signal is the process context's `Done` channel, placed on a base context without that context's cancellation | the event already exists, and the first moment of the drain is the moment the context ends                                                                                    | a channel `shutdown` closes: a second event for the same moment, and `shutdown` would take one more parameter                                                                                                                            | `shutdown` is untouched                                          | the signal fires a few microseconds before `shutdown` runs, which is the intent | owner, 2026-10-27                      |
| `main` ends the process context before `shutdown` on every path                                                  | a listener failure reaches `shutdown` with the context live, so readiness would dial through that drain                                                                       | leaving the failure path alone: the process exits anyway, but its drain would run with readiness still `200`                                                                                                                             | one rule for both paths                                          | nothing; `stop` is idempotent                                                   | owner, 2026-10-27                      |
| The request base context is `context.WithoutCancel` of the process context                                       | the signal otherwise cancels every request in flight and the encoders stop at the next batch, so the body is truncated instead of drained                                     | a cancel the drain's deadline triggers: `http.Server.Shutdown` already cuts what the grace does not cover                                                                                                                                | the drain completes in-flight HTTP streams as the brief promises | a stream that never ends is bounded by the grace alone                          | owner, 2026-10-26                      |
| A request context without the signal panics, as one without the cluster or the logger does                       | design by contract: the entry point is the one builder of request contexts, so a missing signal is a programming error and never a silent server that dials through its drain | absence means not draining: spares two fixture lines, at the cost of a wiring mistake in `main` that nothing would catch                                                                                                                 | a wiring mistake is loud at the first probe                      | every test fixture that builds a request context places an open signal          | owner, 2026-10-27                      |
| The drain refusal logs at debug                                                                                  | the drain is not a failure, and a prober polls through the whole grace                                                                                                        | the existing warn line "readiness probe failed": misreads an expected answer as a failure, once per poll                                                                                                                                 | a quiet drain                                                    | an operator at info sees no trace of the refusals                               | proposal                               |
| Flight takes the process context and not the request base                                                        | its interceptors splice the process values onto each call's own context, and it has no readiness route                                                                        | passing the base: copies a value nothing on that plane reads                                                                                                                                                                             | nothing changes on Flight                                        | nothing                                                                         | `internal/flightsql/server.go:113-122` |
| The uncommitted spec was committed on the base before the branch                                                 | the branch and its plan start from the spec they implement                                                                                                                    | carrying the edits onto the branch: the spec is reviewed with the diff instead of before it                                                                                                                                              | one base commit, as the criteria commits before it               | nothing                                                                         | owner, 2026-10-26                      |

## Knowledge

Per commit; the number is the commit's in Commits.

- Commit 2 (`internal/httpapi/httpapi.go`): `WithDrain`'s doc comment
  carries the signal-on-the-context row and the Done-as-the-signal row;
  `draining`'s doc comment carries the panic-on-absence row;
  `handleReadiness`'s overview and step 1 carry ADR-0004 rule 2 and the
  debug row; `registerStatusRoutes`'s doc comment gains the one claim.
- Commit 3 (`internal/httpapi/readiness_test.go`,
  `internal/httpapi/query_test.go`): `probe`'s doc comment carries the
  panic-on-absence row from the test's side, and `newServerOn` gains
  its one line;
  `TestReadinessDuringDrain`'s doc comment says why a live cluster is
  the right one.
- Commit 4 (`cmd/qdb_rest/main.go`): `newListeners` step 1 carries the
  WithoutCancel row and its evidence (the encoders' batch check, the
  `net/http` derivation); `newHTTPServer`'s doc comment names the base
  context.
- Commit 5 (`cmd/qdb_rest/main.go`): `newListeners` step 1 carries the
  Done-as-the-signal row and the rejected channel; step 5 carries the
  Flight row; `main` step 6 carries the stop-before-shutdown row.
- Commit 6 (`internal/httpapi/AGENTS.md`, `cmd/qdb_rest/AGENTS.md`):
  the two bullets of Design, one per file, which are the cross-function
  homes of the signal and of the base context (`docs/AGENTS.md`, "How to
  work in a folder").
- Commit 8 (`docs/log.md`): Next re-cut as in Design, and the deletion
  entry. The base-commit row has no home after the plan and dies with
  it, as a workflow event `git log` already records.

Every other Rationale row lands in a comment named above. ADR-0004 and
the brief already carry the decision itself and are not edited.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from Design and Knowledge in the same commit as the code and
reconciles the code against this plan before the build-stage message.

Specific to this unit: commit 4 and commit 5 edit the same step of
`newListeners`; commit 4 writes the step with the WithoutCancel claims
and passes the process context's `Done` channel already (the signature
of `WithDrain` exists from commit 2), and commit 5 adds the `stop()`
call and the step-6 and step-5 claims. If the executor finds that
`http.Server.Shutdown` cancels request contexts on its own (it does not
in `$GOROOT/src/net/http/server.go`, checked 2026-10-26), the
WithoutCancel row is wrong and the owner is asked before commit 4.

## Commits

1. `docs(plan): readiness-drain-plan.md, readiness answers 503 without a dial for the whole drain and the request context outlives the signal`
2. `feat(httpapi): WithDrain places the drain signal on the request context, and readiness answers 503 without a dial once it has fired`
3. `test(httpapi): readiness answers 200 while the drain signal is open and 503 once it has fired, on both clusters, and every test server places a signal`
4. `fix(cmd): requests derive from the process context without its cancellation, so the signal cuts no HTTP stream and the grace ends it`
5. `feat(cmd): the drain signal is the process context's end, fired on a signal or a listener failure before the listeners drain`
6. `docs(httpapi,cmd): AGENTS.md say readiness reads the drain signal and the request context outlives the signal`
7. `/doc-discipline all internal/httpapi cmd/qdb_rest docs/readiness-drain-plan.md`, then `/doc-discipline all check` on the same paths; findings land as further `docs(...)` commits before the next step.
8. `docs(log): readiness-drain-plan.md deleted, facts moved to internal/httpapi/AGENTS.md, cmd/qdb_rest/AGENTS.md and the comments of internal/httpapi and cmd/qdb_rest`
9. Verify: push `sc-19567/rr-readiness-drain`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. The drain refusal's log level: debug recommended, because a prober
   polls through the whole grace and the drain is not a failure.
2. `docs/log.md` Next re-cut into the notify slice and then the drain
   step with the close of M6: recommended, in that order, because the
   close needs both.
