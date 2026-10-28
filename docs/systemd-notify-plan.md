# systemd notify -- plan

Status: approved

Unit of M6 (`docs/log.md`, M6 criteria). Branch `sc-19567/rr-systemd-notify`.

## Outcome

When this unit lands, the binary speaks systemd's notify protocol. A
process started with `NOTIFY_SOCKET` set writes `READY=1` to that
socket once all three listeners are bound, `STOPPING=1` the moment the
drain starts, and `WATCHDOG=1` every half of the interval systemd
passes in `WATCHDOG_USEC`, from `READY=1` until the drain starts. The
HTTP and HTTPS listeners bind their ports before serving, in the same
function Flight already does, so a port in use fails the start and
`READY=1` is true when sent. Without `NOTIFY_SOCKET` nothing is sent
and the binary behaves as today. The sender and the keepalive live in
one new package, `internal/notify`, with no test, and a failed send
logs one warning and the binary goes on. The brief and the M6 criteria
name the keepalive as part of the protocol, so the open question of the
criteria is closed.

Left for later units: the stress's drain step and the close of M6
(`docs/log.md`, Next); the packaged unit files in `qdb-pkg-debian` and
`qdb-pkg-rpm`, which say `Type=simple` and change to `Type=notify` and
`WatchdogSec=` in those repositories once a release carries this
binary; the stale In flight and Blocked on lines of the log, which this
unit's deletion commit rewrites.

## Verified facts

- The protocol is one datagram per state to the `AF_UNIX` socket named
  by `NOTIFY_SOCKET`, the payload a newline-separated list of
  assignments with a trailing newline implied. A leading `@` names the
  Linux abstract namespace and stands for a NUL byte. `READY=1` ends
  startup, `STOPPING=1` begins the stop, `WATCHDOG=1` resets the
  watchdog timestamp (sd_notify(3), freedesktop.org, read 2026-10-27).
- systemd recommends a keepalive every half of `WATCHDOG_USEC`, and the
  variables are to be ignored when `WATCHDOG_PID` is set to another
  process's pid (sd_watchdog_enabled(3), read 2026-10-27).
- Under `Type=notify`, systemd keeps the unit in the starting state
  until `READY=1` and kills it at `TimeoutStartSec`. `NotifyAccess=`
  unset is forced to `main`, so the main process may send. A missed
  keepalive puts the service in the failed state and sends `SIGABRT`
  (systemd.service(5), read 2026-10-27).
- The watchdog is armed only in the start-post, running, reload and
  mounting states; `STOPPING=1` moves the service to the stop state,
  and the state change disarms it. A `WATCHDOG=1` that arrives in the
  stop state re-arms it (systemd `src/core/service.c` at commit
  `8a31b82`: `SERVICE_STATE_WITH_WATCHDOG`, `service_set_state`,
  `service_enter_stop_by_notify`, the `WATCHDOG=` branch of the notify
  handler; read 2026-10-28).
- Go's `syscall` rewrites a leading `@` in a unix socket name to NUL on
  Linux (`$GOROOT/src/syscall/syscall_linux.go:554-571`, go1.27), so
  the package passes the variable's value through unchanged.
- `net.Dial("unixgram", path)` opens a connected datagram socket with
  no local address, and a write on it is one datagram. On Windows the
  dial fails at run time and the package compiles, which is fine
  because the variable is never set there.
- `http.Server.Serve(ln)` serves a bound listener, tracks it, and
  `Shutdown` closes it (`$GOROOT/src/net/http/server.go:3272-3279`,
  `:3529`). `ServeTLS(ln, "", "")` uses `TLSConfig.Certificates` when
  the file names are empty (`:3608-3616`), and `tlsconf.Load` sets that
  field (`internal/tlsconf/tlsconf.go:130`). `ListenAndServeTLS("",
"")` is what the binary calls today (`cmd/qdb_rest/main.go:75`).
- Flight binds in `flightsql.New` so a port in use fails the start
  (`internal/flightsql/server.go:73-80`); the HTTP servers bind inside
  `Serve` (`cmd/qdb_rest/main.go:72-83`), so today the start cannot
  know every port is bound.
- The process context ends on `SIGINT`, `SIGTERM` or, through `stop()`,
  on a listener failure, before the drain (`cmd/qdb_rest/main.go:237`,
  `:275-277`). That end is the drain signal readiness reads
  (`cmd/qdb_rest/AGENTS.md`).
- `cmd/qdb_rest` has no test file, and the owner judged this unit glue
  with no test (owner, 2026-10-27).
- The packaged units are `Type=simple` with no `WatchdogSec=`
  (`../qdb-pkg-debian/qdb-rest/systemd/qdb_rest.service`,
  `../qdb-pkg-rpm/qdb-rest/qdb_rest.service`, read 2026-10-27).

## Design

### `cmd/qdb_rest/main.go`

```go
// httpListener adapts *http.Server to listener over a listener bound
// before serving. Serve serves the bound listener, over TLS when
// TLSConfig is set, and treats ErrServerClosed as a clean stop.
// Shutdown drops the error of http.Server.Shutdown and closes the
// listener with it. [server.go:3272-3279, :3529, :3638]
type httpListener struct {
	*http.Server
	ln net.Listener
}

func (l httpListener) Serve() error {
	// style: doc comment alone
	// because: one branch on TLSConfig, as today
	//
	// at the TLS branch: ServeTLS with empty file names serves
	// TLSConfig.Certificates, which tlsconf.Load filled
	// [server.go:3608-3616, tlsconf.go:130]
}
```

```go
// newHTTPServer binds addr and assembles one HTTP listener's server
// over it. The HTTPS listener carries a tls.Config, and plain HTTP
// passes nil. A port in use fails here, before serving, as it does
// for Flight. The caller passes the request base context, which
// carries the process values and the drain signal and outlives the
// signal (newListeners, step 1); BaseContext hands it to every
// request, and ErrorLog routes net/http's own complaints through the
// same handler. There is no global write timeout, because responses
// are streamed. [main.go:87-104, server.go:73-80]
func newHTTPServer(ctx context.Context, addr string, handler http.Handler, tlsConfig *tls.Config) (httpListener, error) {
	// style: doc comment + inline note at the bind
	// because: one bind and one struct literal, and the bind is the
	//          only line with a reason of its own
	//
	// at net.Listen: the bind happens here and not in Serve, so that
	// READY=1 is sent only once every port is held and a port in use
	// fails the start [Rationale: bind before serve]
	//   - rejected: a hook inside ListenAndServe, because net/http
	//     offers none [server.go:3473]
}
```

```go
// newListeners builds the listeners from config and logs what each one
// serves. HTTP is off unless configured, and the other two always
// start (ADR-0001). Every listener is bound when it returns, so a
// port in use fails the start. [main.go:106-168]
func newListeners(ctx context.Context, cfg config.Config, handler http.Handler) ([]listener, error) {
	// style: doc comment + numbered overview + step comments
	// because: the five steps and their reasons stand as today; steps
	//          3 and 4 gain the error of the bind
	//
	// 3. HTTP, when configured, bound and over base, with the warning
	//    as today; a bind error returns like the TLS error [step 2]
	// 4. HTTPS, bound and over base; a bind error returns
}
```

```go
// serveUntilDone serves all listeners until the context is cancelled
// by SIGINT or SIGTERM or one listener fails. It returns the first
// terminal error, or nil on a clean cancel. It shuts nothing down,
// because shutdown does that under the configured grace.
// [main.go:170-185, unchanged]
```

```go
func main() {
	// style: numbered overview as step comments, as today
	// because: the sequence is the story, and steps 5 and 6 gain the
	//          three sends
	//
	// 5. Build the listeners over the root context. Loading the TLS
	//    material and binding any of the three ports can fail here.
	//    Then tell systemd the service is up, because every port is
	//    held, and start the keepalive on the root context, so it ends
	//    with the serve loop and never ticks into the drain
	//    [Rationale: keepalive lifetime; service.c]
	//      notify.Send(ctx, notify.Ready)
	//      go notify.Watchdog(ctx)
	// 6. Serve until a signal or a listener failure. Then end the
	//    process context, so readiness answers 503 on the
	//    listener-failure path too and the keepalive stops; then tell
	//    systemd the stop has begun, after the context's end and never
	//    before it, so no keepalive is planned after STOPPING=1; then
	//    drain as today. A tick already in flight at the context's end
	//    may still land after STOPPING=1 and re-arm the watchdog, which
	//    the exit outruns [service.c, the WATCHDOG= branch]
	//      runErr := serveUntilDone(ctx, listeners)
	//      stop()
	//      notify.Send(ctx, notify.Stopping)
	//      shutdown(...)
}
```

### `internal/notify/notify.go`

```go
// Package notify speaks systemd's notify protocol (sd_notify(3)): one
// datagram per state to the socket NOTIFY_SOCKET names, and the
// watchdog keepalive at half of WATCHDOG_USEC. Outside systemd the
// variables are unset and every function does nothing. The binary
// sends READY=1 once every listener is bound, STOPPING=1 when the
// drain starts, and WATCHDOG=1 between the two (cmd/qdb_rest/main.go).
// [docs/brief.md, Observability and logging]
package notify
```

```go
// The states the binary sends. Each is one assignment line of the
// protocol, and Send adds the newline. [sd_notify(3)]
const (
	Ready    = "READY=1"
	Stopping = "STOPPING=1"
	Watchdog = "WATCHDOG=1"
)
```

```go
// Send writes one state line to the socket NOTIFY_SOCKET names and
// does nothing when the variable is unset. A failed send logs one
// warning on the context's logger and is not an error to the caller,
// because the serving path does not depend on systemd and a bad socket
// surfaces at systemd's own start timeout. [Rationale: failed send]
func Send(ctx context.Context, state string) {
	// style: doc comment + numbered overview + step comments
	// because: two early returns and a rejected alternative at the
	//          failure
	//
	// 1. Unset means not under systemd, and nothing is sent; the empty
	//    string is the same case [sd_notify(3)]
	// 2. Dial a connected datagram socket on the path as given; Go
	//    rewrites a leading @ to the abstract namespace's NUL on Linux,
	//    so the value needs no edit [syscall_linux.go:554-571]
	// 3. Write the line with its newline and close; one write is one
	//    datagram [sd_notify(3)]
	// 4. On any failure warn with the path and the error and return
	//    - rejected: failing the start on a failed READY=1, because a
	//      misconfigured socket is systemd's to report and the serving
	//      path owes it nothing [Rationale: failed send]
	//    - rejected: silence, because the operator would read only
	//      systemd's timeout [Rationale: failed send]
}
```

```go
// Watchdog sends WATCHDOG=1 every half of the interval WATCHDOG_USEC
// names until ctx ends, and returns at once when the variable is
// unset or WATCHDOG_PID names another process. It blocks, so the
// caller runs it on a goroutine. Each tick proves that this process
// schedules goroutines and nothing about the cluster.
// [sd_watchdog_enabled(3); docs/log.md, M6 criteria]
func Watchdog(ctx context.Context) {
	// style: doc comment + numbered overview + step comments
	// because: three guards, a constant with a source and a rejected
	//          alternative at the loop
	//
	// 1. Unset, unparsable or zero means no watchdog, and the function
	//    returns; a pid in WATCHDOG_PID other than this process's means
	//    the variables were set for an ancestor, and the function
	//    returns [sd_watchdog_enabled(3)]
	// 2. Half the interval is systemd's own recommendation, not a
	//    measurement [sd_watchdog_enabled(3)]
	// 3. Tick until ctx ends, and send on each tick through Send, so a
	//    failed tick warns and the loop goes on
	//    - rejected: a probe of the cluster or of the HTTP listener per
	//      tick, because a cluster outage must not kill the gateway and
	//      the keepalive is tied to the serving loop alone [docs/log.md,
	//      M6 criteria; Rationale: keepalive proves the loop]
	//    - the context is the process context, which ends before
	//      STOPPING=1 is sent, so the loop ends when the drain starts
	//      [Rationale: keepalive lifetime]
}
```

### `cmd/qdb_rest/AGENTS.md`

```
- The binary speaks systemd's notify protocol through `internal/notify`:
  `READY=1` once `newListeners` has returned, which is once every port
  is bound, `STOPPING=1` right after the process context's end and
  before the drain, and `WATCHDOG=1` on the process context between the
  two. Nothing is sent when `NOTIFY_SOCKET` is unset, and a failed send
  is one warning. The HTTP servers bind in `newHTTPServer`, before
  serving, so that `READY=1` is true when sent.
```

### `internal/AGENTS.md`

```
- The systemd notify protocol lives in `internal/notify`, and the
  binary's entry point alone calls it. The package reads the variables
  systemd sets and has no configuration key.
```

### `docs/brief.md`

```
Observability and logging: the notify sentence gains the keepalive:
`WATCHDOG=1` every half of `WATCHDOG_USEC` between the two, tied to the
serving loop and never to the cluster.

Milestones, M6: the notify clause gains "and the `WATCHDOG=1`
keepalive at half of `WATCHDOG_USEC`, tied to the serving loop".

Project structure: a row `internal/notify/   systemd notify protocol:
READY, STOPPING, the watchdog keepalive`.
```

### `docs/log.md`

```
M6 criteria: the sentence "whether the binary also sends the
`WATCHDOG=1` keepalive, tied to the serving loop and never to the
cluster probe, is open (owner, 2026-10-26)" becomes "and sends the
`WATCHDOG=1` keepalive at half of `WATCHDOG_USEC`, tied to the serving
loop and never to the cluster probe, from `READY=1` until the drain
starts".

Entries: one dated entry, the keepalive decided in, pointing at the
brief's M6 clause.

Deletion commit: Next item 1 removed; In flight says none; Blocked on
says none; the readiness-drain lines go, because that unit merged.
```

## Rationale

| decision                                                                                                                     | why                                                                                                                                  | rejected, and why                                                                                                                                                                                                                            | gained                                                      | given up                                                                                                  | settled by                                    |
| ---------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| The unit is the notify protocol plus the watchdog keepalive, eight commit slots                                              | the keepalive was the open M6 criterion, and the owner took it into this unit                                                        | the five-commit cut without the keepalive, because the owner would review the keepalive as the same change                                                                                                                                   | M6's open criterion closes                                  | nothing                                                                                                   | owner, 2026-10-27                             |
| The sender and the keepalive live in `internal/notify`                                                                       | a new small package over functions in `main.go`, which has no tests, and over `internal/observe`, whose name says logging            | `internal/observe`, because it would gain a systemd dependency its name does not say; `main.go`, because the protocol is its own concern                                                                                                     | one package the entry point calls in three lines            | one more package                                                                                          | owner, 2026-10-27                             |
| Failed send: one warning, the binary goes on                                                                                 | the serving path does not depend on systemd, and under `Type=notify` a bad socket surfaces at systemd's start timeout                | failing the start, because a misconfigured socket is systemd's to report; silence, because the operator would read only systemd's timeout                                                                                                    | the binary serves wherever it was started                   | a misconfiguration is found at the timeout, not at once                                                   | owner, 2026-10-27                             |
| Bind before serve: `net.Listen` in `newHTTPServer`, `Serve(ln)` after                                                        | `READY=1` must be true when sent, and net/http offers no hook inside `ListenAndServe`                                                | a hook inside `ListenAndServe`, because there is none (`server.go:3473`)                                                                                                                                                                     | a port in use fails the start for all three listeners alike | nothing                                                                                                   | proposal                                      |
| The keepalive proves the serve loop and nothing about the cluster                                                            | the M6 criteria tie it to the serving loop and never to the cluster probe; a cluster outage must not kill the gateway                | a liveness request over HTTP per tick, because it costs a TLS client and a request and proves little more; a cluster dial, because it kills the gateway on an outage                                                                         | a tick is one datagram                                      | a stuck handler is not detected by the watchdog                                                           | `docs/log.md`, M6 criteria; owner, 2026-10-27 |
| The keepalive ends when the drain starts: it runs on the process context, and `STOPPING=1` goes out after that context's end | systemd disarms the watchdog on `STOPPING=1`, and a tick during the stop would re-arm it (`service.c`)                               | ticking through the drain, because it re-arms a watchdog systemd dropped and the comment would have to say the ticks do nothing useful; the owner's earlier pick rested on the false claim that systemd keeps the watchdog armed during stop | one context ends the serve loop and the keepalive           | a tick in flight at the context's end may land after `STOPPING=1`; the exit outruns the re-armed watchdog | owner, 2026-10-28                             |
| Half the interval, not measured                                                                                              | systemd's own recommendation                                                                                                         | a configuration key, because systemd passes the interval and the operator sets it in the unit                                                                                                                                                | no key                                                      | nothing                                                                                                   | sd_watchdog_enabled(3); proposal              |
| No configuration key for the socket or the interval                                                                          | systemd sets `NOTIFY_SOCKET`, `WATCHDOG_USEC` and `WATCHDOG_PID`, and the brief says the binary is silent when the variable is unset | a key, because nothing but systemd would set it                                                                                                                                                                                              | `examples/qdb_rest.yaml` is unchanged                       | nothing                                                                                                   | `docs/brief.md`, Milestones M6; proposal      |
| No tests                                                                                                                     | the owner judged the package glue over the standard library, proved by the first systemd host                                        | Go tests on a unix datagram socket, and a round-trip step through an e2etool listener, because the owner declined both                                                                                                                       | no fixture                                                  | the three sends and the loop are unpinned                                                                 | owner, 2026-10-27                             |

## Knowledge

- Commit 2, `main.go`: `httpListener`'s doc comment carries the bound
  listener and what Shutdown closes; `newHTTPServer`'s doc comment
  carries the bind and the failure; the inline note at the bind carries
  the bind-before-serve row and the rejected hook; `newListeners`'s
  steps 3 and 4 carry the bind error.
- Commit 3, `notify.go`: the package comment carries the protocol and
  the three moments; `Send`'s doc comment carries the failed-send row;
  its steps carry the unset case, the abstract namespace fact and the
  two rejected failure policies.
- Commit 4, `main.go`: step 5 carries `READY=1`'s reason; step 6
  carries the order of the context's end and `STOPPING=1`.
- Commit 5, `notify.go`: `Watchdog`'s doc comment carries the pid rule
  and what a tick proves; its steps carry the half-interval source, the
  rejected probes and the lifetime row.
- Commit 6, `main.go`: step 5 carries the keepalive's start on the
  process context and the lifetime row; step 6 carries the tick in
  flight.
- Commit 7: the `cmd/qdb_rest/AGENTS.md` bullet, the `internal/AGENTS.md`
  bullet and the brief's project structure row.
- Commit 8: the brief's Observability sentence and M6 clause, the log's
  M6 criteria and its dated entry carry the keepalive decision.
- The unit files of the packaging repositories: Outcome names them, and
  the deletion commit's log entry names the two repositories as the
  home of the `Type=notify` change; no document in this repository
  carries the unit file.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message. Specific to
this unit: commit 4 cites the order of `stop()` and `STOPPING=1` which
commit 6 then relies on, so commit 6 reads step 6 before adding the
keepalive; `make lint` runs under `GOTOOLCHAIN=go1.27.0`.

## Commits

1. `docs(plan): systemd-notify-plan.md, the binary speaks systemd's notify protocol with READY, STOPPING and the watchdog keepalive, and the HTTP listeners bind before serving`
2. `feat(cmd): the HTTP listeners bind in newHTTPServer and serve the bound listener, so a port in use fails the start`
3. `feat(notify): Send writes one state line to the socket NOTIFY_SOCKET names and does nothing when it is unset`
4. `feat(cmd): main sends READY=1 once every listener is bound and STOPPING=1 when the drain starts`
5. `feat(notify): Watchdog sends WATCHDOG=1 every half of WATCHDOG_USEC until its context ends`
6. `feat(cmd): main runs the watchdog on the process context, from READY=1 until the drain starts`
7. `docs(cmd,internal,brief): AGENTS.md and the project structure name the notify package and the three sends`
8. `docs(brief,log): M6 names the watchdog keepalive, and the criteria no longer leave it open`
9. `/doc-discipline all cmd/qdb_rest internal/notify docs/systemd-notify-plan.md`, then `/doc-discipline all check` on the same paths; any finding is a further small commit.
10. `docs(log): systemd-notify-plan.md deleted, facts moved to docs/brief.md, cmd/qdb_rest/AGENTS.md, internal/AGENTS.md and the comments of internal/notify and cmd/qdb_rest`
11. Verify: push `sc-19567/rr-systemd-notify`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. The packaged unit files (`qdb-pkg-debian`, `qdb-pkg-rpm`) stay
   `Type=simple` until a release carries this binary; recommendation:
   change them to `Type=notify` with a `WatchdogSec=` in those
   repositories at the M10 release, not before, because `Type=notify`
   against an older binary times out at start.
