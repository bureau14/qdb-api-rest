# Plan: the stress's drain step, readiness without the drain signal, and the close of M6

Status: draft

## Outcome

When this unit lands:

- `make test-stress` exists in `tests/e2e/Makefile` and runs the
  stress's drain step on both clusters, by hand and never in CI. The
  step starts one server under a long `listen.shutdown_grace` and a
  small session budget, ingests generated rows into one table, starts
  one slow read of that table, sends the server SIGTERM once the first
  byte of the read has arrived, and asserts that a new connection is
  refused while the read is still open, that the slow body compares
  byte for byte with the generated rows, and that the server's log ends
  with a clean shutdown.
- Readiness is the dial and nothing else. The drain signal that main
  placed on every request context, the readiness branch that read it,
  and the Go test that pinned the 503 are gone. ADR-0004, the brief and
  the `AGENTS.md` files of `cmd/qdb_rest` and `internal/httpapi` say
  that the drain closes the listeners first, so a load balancer's next
  connection is refused, and that readiness never reads the lifecycle.
- The round trip's request, status, login, create, ingest and compare
  helpers live in `tests/e2e/common.sh`, sourced by both drivers.
- M6 is closed in `docs/log.md`: the table row, the criteria reconciled
  with the tree, Next item 1 removed, and the close entry.

Left for later units: the stress's budget step and its Flight exclusion
(M7, `docs/log.md`, Handoff), the heap-fault investigation
(`docs/log.md`, Next 2), and steps over HTTPS (`docs/log.md`, Handoff).

## Verified facts

1. `http.Server.Shutdown` closes the listening sockets as its first act,
   then closes idle connections, then polls until every active
   connection has finished or the context ends
   (`$GOROOT/src/net/http/server.go:3272-3310`, Go 1.27). A new TCP
   connection is therefore refused from the first instant of the drain,
   and a request already in flight runs on.
2. `shutdown` in `cmd/qdb_rest/main.go:216-231` runs every listener's
   `Shutdown` in parallel under one deadline of `listen.shutdown_grace`,
   then closes the cluster. When the deadline passes with a stream still
   open, `Shutdown` returns the context error, `httpListener.Shutdown`
   drops it (`main.go:90`), and `main` returns (`main.go:311-315`), so
   the stream is cut by process exit and the client sees a short body.
   The drain step's grace must therefore outlast the slow read.
3. Every HTTP request derives from the process context without its
   cancellation (`main.go:131-134`, `context.WithoutCancel`), so the
   signal cuts no stream. That line stays; only the `WithDrain` wrap
   around it goes.
4. `signal.NotifyContext`'s `stop` is called right after the serve loop
   ends (`main.go:308`), before the drain. `signal.Stop` restores the
   default disposition when no other handler remains, and SIGTERM's
   default terminates the process. A second SIGTERM during the drain
   kills the server mid-drain. The drain step sends one SIGTERM and
   waits for the exit; it never calls `stop_server` on a draining pid.
5. The readiness handler's drain branch is `internal/httpapi/httpapi.go:59-67`,
   the signal's plumbing is `httpapi.go:15-40` (`drainKey`, `WithDrain`,
   `draining`), the test is `TestReadinessDuringDrain`
   (`internal/httpapi/readiness_test.go:81-110`), and `probe`
   (`readiness_test.go:22-35`) and `newServerOn`
   (`internal/httpapi/query_test.go:65-68`) place an open signal. No
   other code reads the signal.
6. The sentences that describe the 503 during the drain are ADR-0004
   rule 2, last sentence (`docs/adr/0004-pure-probes.md:38-42`),
   `docs/brief.md:651-654` (Resilience) and `docs/brief.md:812-814`
   (Observability), `docs/log.md:28-30` (M6 criteria),
   `cmd/qdb_rest/AGENTS.md:26-31` and `internal/httpapi/AGENTS.md:138-142`,
   and the step comments at `main.go:131-134` and `main.go:296-304`.
7. The stress's definition is `docs/e2e.md:317-332`: shell and curl,
   `make test-stress`, rows from `e2etool gen` through `POST /api/v2/rows`,
   a server started with a small `pool.max_sessions`, the drain step's
   two assertions (the body and the refused connection), parameters in
   the Makefile, run by hand. `docs/e2e.md:339-341` already lists
   `test-stress` in the layout. The Makefile has no such target
   (`tests/e2e/Makefile`, targets at line 93).
8. The round trip's helpers take their state as arguments: `request`
   (`tests/e2e/round-trip.sh:69-73`), `expect` (78-86), `login`
   (98-122), `create_body` (128-139), `create_table` (144-151), `ingest`
   (157-164). `compare_csv` (184-196) reads the script global `TOOL` at
   line 187, and `check_response` (207-218) calls it. The file ends in
   `main "$@"` (line 492), so it cannot be sourced.
9. The reader's CSV answer is the generated `rows.csv` byte for byte,
   with no decoder: `read_back` passes `raw` (`round-trip.sh:241`) and
   `compare_csv` skips the tool for a raw CSV (`round-trip.sh:186-191`).
   `e2etool gen` writes `rows.csv` with `$table` and `$timestamp` first
   (`tests/e2e/tools/e2etool/main.go:7,290-291`) and prints the row
   count (`main.go:273`).
10. `start_server` (`tests/e2e/common.sh:134-144`) backgrounds the
    command, writes the pidfile and waits for the port. `stop_server`
    (`common.sh:152-165`) sends SIGTERM, waits up to 10 s, then SIGKILLs,
    and its comment says a server under a longer grace is stopped by the
    step that configured it (`common.sh:146-150`). `clean` removes
    `*.pid` and `*.log` under `tests/e2e` (`Makefile:152-153`).
11. The flags are `--listen-shutdown-grace DURATION`,
    `--pool-max-sessions N` and `--pool-per-user-max N`
    (`internal/config/config.go:47,102-103,271-273`). Durations parse
    with `time.ParseDuration` (`config.go:289-294`). `per_user_max` must
    be within `1..max_sessions` (`config.go:603-605`), so a small budget
    passes both flags. Defaults: grace 8 s, budget 64, per user 8
    (`config.go:186,193-195`).
12. The CSV, JSON and NDJSON encoders write through a 4 KiB
    `bufio.Writer` and flush at the end (`internal/encoding/csv.go:228-233`,
    `json.go:247,319`). The loopback socket buffers are 128 KiB each way
    on this machine (`sysctl net.inet.tcp.sendspace`). A 1233-row CSV is
    80 KB (`tests/e2e/actual/round-trip/insecure/rows.csv`, 2026-10-28),
    so a 10000-row body fits in the kernel's buffers plus curl's and the
    read could finish before SIGTERM. The row count of the stress is
    sized so that the server's write blocks on the client.
13. The HTTP listener's log line on a clean stop is `shutdown complete`
    and on a failure `server failed` (`main.go:311-315`).
14. The Handoff item "the stress has no Flight case" is the owner's
    (`docs/log.md:89-90`, 2026-10-13) and stays until M7's budget step
    (owner, 2026-10-28).
15. The M5 close entry's form is `## 2026-10-26 -- M5 closed; ...` with
    "M5 exit criteria met by the tree and build N on the base head"
    (`docs/log.md:175-178`), and the table row became `done | closed
2026-10-26` (`docs/log.md:17`).

## Design

Shell sketches use the Go sketch shape with `#` comments.

### `internal/httpapi/httpapi.go`

`drainKey`, `WithDrain` and `draining` are deleted. `handleReadiness`
loses step 1 and its numbering:

```go
// handleReadiness reports whether this instance may take new traffic:
// 200 or 503 with an empty body and no Retry-After, and the cause of a
// 503 goes to the log and not the wire. [ADR-0004 rule 2]
// It reads nothing of the serving lifecycle: the drain closes the
// listeners before anything else, so a prober's next connection is
// refused and no 503 is needed. [Rationale 1]
func handleReadiness(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + inline note at the dial
	// because: one dial and one answer, a single decision point
	//
	// at the dial: as the REST API's own user, bypassing the pool, the
	// budget and the breaker, and a failure is 503 with the cause at
	// warn [ADR-0004 rule 2; internal/httpapi/httpapi.go:70-75]
}
```

### `internal/httpapi/readiness_test.go` and `query_test.go`

```go
// probe runs one GET against the readiness route with a cluster built
// for cfg and returns the response. [readiness_test.go:18-21]
func probe(t *testing.T, cfg config.Config) *httptest.ResponseRecorder {
	// style: doc comment alone
	// because: a fixture that builds a request and serves it
}
```

`TestReadinessDuringDrain` is deleted. The two remaining readiness tests
drop the channel argument. `newServerOn` drops the `WithDrain` wrap.

### `cmd/qdb_rest/main.go`

`newListeners` step 1 and `main` step 6 are reworded; no code beyond
the `WithDrain` wrap changes:

```go
	// 1. Requests inherit the process values but not its cancellation:
	//    the signal cuts no stream, the grace ends it. [Verified 3]
	base := context.WithoutCancel(ctx)
```

```go
	// 6. Serve until a signal or a listener failure. Then end the
	//    process context, so the keepalive stops; then tell systemd the
	//    stop has begun, after the context's end and never before it, so
	//    no keepalive is planned after STOPPING=1; then drain the
	//    listeners first and the cluster second under
	//    listen.shutdown_grace. The listeners close their ports as the
	//    drain's first act, so a load balancer's next connection is
	//    refused while in-flight streams finish. [Verified 1; Rationale 1]
	//    A tick already in flight ... (unchanged)
```

### `tests/e2e/common.sh`

A new section between process control and the end, "requests", holding
`request`, `expect`, `login`, `create_body`, `create_table`, `ingest`
and `compare_csv`, moved from `round-trip.sh` with their comments as
they are, and these two changes:

```bash
# compare_csv <out_dir> <name> <format> <table> <body_file> <csv> <csv_is> <tool>:
# ... (the existing contract) ... The tool is an argument, because the
# helper is shared by both drivers and neither exports a global for it.
# [Rationale 3]
compare_csv() {
    # style: doc comment alone
    # because: one decode and one compare, as today
}
```

```bash
# wait_for_exit <pid> <seconds>: wait for the process to end on its own
# and die when it has not within the window. It sends no signal, because
# the one SIGTERM a draining server may receive is the caller's; a
# second one kills the process outright once main has released the
# signal handler. [Verified 4]
wait_for_exit() {
    # style: doc comment alone
    # because: one poll loop
    #
    # at the loop: kill -0 every tenth of a second, as stop_server polls
    # [common.sh:158-161]
}
```

`stop_server` keeps its comment: a server under a longer grace is
stopped by the step that configured it, which the stress now does
through `wait_for_exit`.

### `tests/e2e/round-trip.sh`

The moved functions leave. `check_response` passes `"$TOOL"` to
`compare_csv`. The file header's "generic request and status helpers"
sentence points at `common.sh`.

### `tests/e2e/stress.sh`

```bash
#!/usr/bin/env bash
# The stress (docs/e2e.md, Stress definition): shell and curl, run by
# hand, asserted as behaviour and never as a timing. Today it has the
# drain step. The budget step joins with M7. [Verified 7]
#
#   stress.sh drain --http <url> --pid-file <file> --log <file> --login-body <file> --tool <e2etool> --out <dir> --rows N --seed S --limit-rate RATE --exit-window SECONDS
#
# One call is one cluster and one server, because the step ends the
# server it was given and the Makefile starts the next. [Rationale 5]
# The Makefile is the only source of URLs, ports, paths and the
# parameters, and passes them as options. [tests/e2e/AGENTS.md:8-12]
```

```bash
# slow_read <out> <name> <url> <token> <table> <rate>: GET the table as
# CSV in the background under curl --limit-rate, body and headers to
# <out>/<name>.*, and print curl's pid. The status lands in
# <out>/<name>.status when curl ends. [Verified 9]
slow_read() {
    # style: doc comment alone
    # because: one backgrounded request
    #
    # at --limit-rate: the client is the slow side, so the server's
    # write blocks on it and the stream is genuinely in flight when the
    # signal arrives [Verified 12]
}
```

```bash
# wait_for_first_byte <file> <attempts>: wait until the file is
# non-empty, polling every tenth of a second, and fail after the
# attempts. The attempts are a failure guard for a server that never
# answers, not an assertion on time. [ADR-0015]
wait_for_first_byte() {
    # style: doc comment alone
    # because: one poll loop
}
```

```bash
# probe_refused <url> <server_pid> <curl_pid> <attempts>: connect to the
# HTTP listener until the connection is refused, while the server is
# still alive and the slow read still open, and fail after the attempts
# or when either has ended. [Rationale 6]
probe_refused() {
    # style: doc comment + numbered overview + step comments
    # because: a loop with three exits, and the order of its checks is
    # the assertion
    #
    # A connect that still succeeds is retried and not failed, because
    # the listener closes a moment after the signal and the assertion is
    # that it becomes refused while the stream is open. [Verified 1]
    #
    # 1. both pids alive, else fail: a refusal from a dead server proves
    #    nothing about the drain [Rationale 6]
    # 2. curl against the liveness path with no token, because that is
    #    the path a load balancer polls; exit code 7 is the refusal
    #    [curl(1) exit codes]
    #    - rejected: readiness 503 as the signal, because the listener is
    #      closed before any prober reaches it [Rationale 1]
    # 3. a success or another error sleeps a tenth of a second and goes
    #    round, up to the attempts
}
```

```bash
# drain_step <out> <url> <token> <table> <csv> <pid_file> <log> <rate>
# <exit_window>: the drain step of docs/e2e.md: one slow read across
# SIGTERM, a refused connection while it runs, the body byte for byte,
# and a clean shutdown in the log. Returns 1 on the first failed
# assertion.
drain_step() {
    # style: doc comment + numbered overview + step comments
    # because: a sequence whose order is the test
    #
    # The signal goes out only once the first byte has arrived, so the
    # stream is provably in flight, and the step waits for the server to
    # exit by itself, because a second SIGTERM would kill it mid-drain.
    # [Verified 4; Rationale 6]
    #
    # 1. start the slow read [slow_read]
    # 2. wait for its first byte [wait_for_first_byte]
    # 3. read the pid, confirm it is alive, send one SIGTERM
    #    [read_pid_file, common.sh:122-130]
    # 4. the refused connection, while the read is open [probe_refused]
    # 5. wait for curl, then its status must be 200 and the body must
    #    compare equal to the generated CSV with no decoder [Verified 9;
    #    compare_csv ... raw]
    # 6. wait for the server to exit within the window [wait_for_exit]
    #    - rejected: stop_server, because it sends a second SIGTERM
    #      [Verified 4]
    # 7. the log carries "shutdown complete" and not "server failed",
    #    a fixed string as the round trip's warning check greps
    #    [Verified 13; round-trip.sh:410-425]
}
```

```bash
# run_cluster <name>: login, the table, the generated rows ingested as
# CSV, then the drain step. The table is e2e_stress on both clusters and
# is created delete-first, as the round trip's are. [create_table]
run_cluster() {
    # style: doc comment + numbered overview + step comments
    # because: four steps in a fixed order, each a shared helper
    #
    # 1. login as the round trip logs in [login]
    # 2. gen once with --table e2e_stress, because $table is a column
    #    of the body [round-trip.sh:447-449]
    # 3. create and ingest as CSV, one format because the formats are
    #    the round trip's question and not the stress's [Rationale 4]
    # 4. the drain step [drain_step]
}
```

`main` parses the options, writes the anonymous login body when the
given body file is the insecure one (as `round-trip.sh:478-479` does),
and runs `run_cluster` once.

### `tests/e2e/Makefile`

```make
# ---------------------------------------------------------------- stress

# The stress (docs/e2e.md, Stress definition) runs by hand and never in
# CI. Its parameters live here. STRESS_ROWS is sized so the CSV body
# outgrows the loopback socket buffers and curl's own, so that the
# server's write blocks on the slow client; not measured, the first
# value that held on macOS and Linux. STRESS_LIMIT_RATE makes the read
# outlast the signal by seconds. STRESS_GRACE outlasts the read by a
# wide margin, because a grace that expires cuts the stream by process
# exit and fails the body assertion, and STRESS_EXIT_WINDOW sits above
# the grace. The budget is two sessions, the smallest that leaves the
# ingest and the read one each. [Verified 2, 11, 12]
STRESS_ROWS        ?= 200000
STRESS_SEED        ?= $(shell date +%s)
STRESS_LIMIT_RATE  ?= 500k
STRESS_GRACE       ?= 60s
STRESS_EXIT_WINDOW ?= 70
STRESS_FLAGS       := --listen-shutdown-grace $(STRESS_GRACE) --pool-max-sessions 2 --pool-per-user-max 2
STRESS_PID         := $(E2E_DIR)/stress.pid
STRESS_LOG         := $(E2E_DIR)/stress.log
STRESS_PID_SECURE  := $(E2E_DIR)/stress-secure.pid
STRESS_LOG_SECURE  := $(E2E_DIR)/stress-secure.log
STRESS             := $(E2E_DIR)/stress.sh

# The stress always starts its servers, because the step ends the server
# with SIGTERM, so no URL form exists. The round trip's ports are reused,
# because the two suites never run at once on one machine, and a clash
# fails the bind loudly. Own pidfiles and logs, because the log is
# asserted. One cluster after the other, each server started here and
# ended by the step; the trap covers a failed step. [Rationale 5, 7]
test-stress: services-check e2etool
	...
```

The help block gains `make test-stress  the stress, by hand: the drain
step on both clusters`.

### Documents

`docs/adr/0004-pure-probes.md`, rule 2: the last sentence ("From the
first moment of the shutdown drain ...") is replaced:

```
- During the shutdown drain readiness is not consulted: the listeners
  close their ports as the drain's first act, so a load balancer's next
  connection is refused and the instance leaves rotation without a
  probe. [Verified 1; Rationale 1]
```

Alternatives rejected gains one row:

```
| Readiness 503 during the drain | the listener is closed before any prober reaches the handler, so the refused connection already does the job and the branch is dead code (owner, 2026-10-28) |
```

`docs/brief.md` Resilience (651-654): "Graceful shutdown drains
in-flight streams on both planes under a configured grace, and the
listeners close first, so a load balancer's next connection is refused
and the instance leaves rotation." Observability (812-814): the clause
"and answers `503` without a dial for the whole shutdown drain
(ADR-0004)" is removed. Milestones M6 (brief 986-995): the readiness
clause becomes "the listeners close first during the drain, so a new
connection is refused while in-flight streams finish (ADR-0004)".

`cmd/qdb_rest/AGENTS.md` bullet (26-31):

```
- Every HTTP request inherits the process values through a context
  without the process context's cancellation (`newListeners`), so
  SIGINT or SIGTERM cuts no stream in flight and `listen.shutdown_grace`
  is what ends one. The drain closes every listener's port first, so a
  new connection is refused while streams finish, and readiness is not
  consulted (ADR-0004). Flight takes the process context itself, because
  its interceptors splice the values onto each call's own context.
```

`internal/httpapi/AGENTS.md` bullet (138-142):

```
- Readiness reads nothing of the serving lifecycle. The drain closes the
  listeners before anything else (`cmd/qdb_rest/AGENTS.md`), so a
  prober's next connection is refused and no drain branch exists
  (ADR-0004, Alternatives).
```

`docs/e2e.md` Stress definition (317-332) is rewritten in place to say:
the server runs under a long `listen.shutdown_grace` with a budget of
two sessions; the drain step runs on both clusters, the secure server
under the key files; the signal goes out once the first byte has
arrived; the assertions are the refused connection while the read is
open, the body byte for byte, and `shutdown complete` in the log; the
step waits for the server to exit on its own because a second SIGTERM
kills it; the parameters and their reasons are the Makefile's. The
Layout row for `test-stress` and the one for `common.sh` ("helpers:
log_*, pidfile/start_server/stop_server/wait_for_exit, the request,
login, create, ingest and compare helpers, qdbsh wrapper") are updated,
and `stress.sh` gets a row. The driver section's "The driver is one
file: generic request and status helpers ..." names `common.sh` as the
helpers' home. A decision-log table dated 2026-10-28 carries Rationale
rows 3 to 7.

`tests/e2e/AGENTS.md`: the `common.sh` bullet names the shared request
helpers and `wait_for_exit`; a new bullet says the stress (`stress.sh`,
`make test-stress`) runs by hand, ends its own server, and that the
only SIGTERM a draining server receives is the step's.

`tests/e2e/README.md`: a usage line for `make test-stress
QDB_REST_BIN=...` and a short "The stress" section with the drain step
in two sentences and the parameters' names.

`docs/log.md`: commit 3 appends the entry
`## 2026-10-28 -- readiness no longer answers 503 during the drain`
("Owner decision; ADR-0004, rule 2 and Alternatives. The refused
connection is the signal a load balancer needs."). Commit 9 sets the M6
row to `done | closed 2026-10-28`, rewrites the M6 criteria's readiness
clause to the refused connection asserted by the drain step, removes
Next item 1, sets "In flight: none", and appends
`## 2026-10-28 -- M6 closed` ("M6 exit criteria met by the tree, the Buildkite build
of the branch head and `make test-stress` green by hand on both
clusters. The Handoff item on the stress's Flight case stays for M7.").

## Rationale

| decision                                                                                                                                                                                                                 | why                                                                                                                                                                                                                            | rejected, and why                                                                                                                                                                                                                | gained                                                                           | given up                                                                     | settled by              |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ----------------------- |
| 1. Readiness no longer answers 503 during the drain; the drain signal, its branch and its test are removed                                                                                                               | the listener closes as the drain's first act, so no prober reaches the handler; the refused connection already takes the instance out of rotation and is the simpler mechanism                                                 | keeping the branch and only dropping the claim, because code the documents disown is dead code; holding the port open for a window before Shutdown, because it adds a key and a mechanism for a signal the refusal already gives | one fewer context value, one fewer panic path, readiness with one decision point | a prober never sees a 503 that names the drain; it sees a refused connection | owner, 2026-10-28       |
| 2. The Handoff item "the stress has no Flight case" stays until M7                                                                                                                                                       | the item constrains both steps of the stress, and the budget step is M7's                                                                                                                                                      | deleting it at the M6 close, because the budget step would lose the rule                                                                                                                                                         | the rule survives to the step that needs it                                      | the M6 criterion's last clause is met vacuously                              | owner, 2026-10-28       |
| 3. The request, login, create, ingest and compare helpers move to `common.sh`, and `compare_csv` takes the tool as an argument                                                                                           | `common.sh` holds every shared helper and nothing is duplicated (`tests/e2e/AGENTS.md`)                                                                                                                                        | a second library file, because the rule names one; duplicating the six in the stress, because a wire change would then be fixed twice                                                                                            | one driver change per wire change                                                | `round-trip.sh` is no longer one file                                        | proposal                |
| 4. The stress ingests as CSV and reads as CSV, compared raw                                                                                                                                                              | the formats are the round trip's question; the stress asks whether a stream survives the drain, and the raw CSV compare needs no decoder                                                                                       | every format, because it would repeat the round trip under a slow client                                                                                                                                                         | one body file, one compare                                                       | nothing the round trip does not already prove                                | proposal                |
| 5. One `stress.sh` call is one cluster; the Makefile starts each server and the step ends it; the round trip's ports are reused and the pidfiles and logs are the stress's own                                           | the step ends the server it was given, so the Makefile's start-per-cluster is the lifecycle; the suites never run at once on one machine; the log is asserted                                                                  | a URL form for the server, because a server the step cannot signal cannot be drained; separate ports, because a clash is loud and the suites are run one at a time                                                               | the Makefile stays the one owner of start and stop                               | the stress has no URL form                                                   | proposal                |
| 6. SIGTERM goes out once the first byte of the slow read has arrived; the refused-connection probe retries until refused while both the server pid and curl are alive, and asserts curl exit 7 against the liveness path | a byte proves the stream is in flight, and a refusal from a dead server or after the read ended proves nothing; the listener closes a moment after the signal, so a probe that lands in that moment is retried                 | a fixed sleep before the signal, because it is a timing (ADR-0015); readiness 503 as the probe, because it is unreachable (Rationale 1)                                                                                          | assertions on behaviour only                                                     | a bounded retry loop whose cap is a failure guard                            | proposal                |
| 7. The step waits for the server to exit through a new `wait_for_exit`, never `stop_server`; the row count outgrows the socket buffers; the grace is long and the exit window longer                                     | a second SIGTERM kills the server once main has released the handler (Verified 4); a body that fits in the buffers finishes before the signal (Verified 12); a grace that expires cuts the stream by process exit (Verified 2) | giving `stop_server` a window argument, because it would still send the killing SIGTERM; the default grace, because the read must outlast the signal by seconds and the grace must outlast the read                              | a drain step that fails for the right reason                                     | a run of the order of a minute per cluster                                   | proposal                |
| 8. `make test-stress` is run locally by the build stage against the `start-services.sh` clusters, and the close names that run                                                                                           | the stress runs by hand and never in CI (`docs/brief.md`, Testing doctrine, owner 2026-10-26)                                                                                                                                  | a CI step, because the agents are small (ADR-0015)                                                                                                                                                                               | M6 closes on the run the criteria name                                           | the build has no evidence of the stress                                      | `docs/brief.md:900-902` |

## Knowledge

- Commit 2 (Go removal): `handleReadiness` doc comment carries the
  "reads nothing of the lifecycle" claim; `newListeners` step 1 and
  `main` step 6 carry Verified 1 and 3 in plain words; `probe`'s doc
  comment loses the signal.
- Commit 3 (documents): Rationale 1 lands in ADR-0004 rule 2 and its
  Alternatives row, in the brief's Resilience, Observability and M6
  paragraphs, in `cmd/qdb_rest/AGENTS.md` and `internal/httpapi/AGENTS.md`,
  and as the log entry of 2026-10-28.
- Commit 4 (helpers): Rationale 3 lands in `compare_csv`'s doc comment
  and the `common.sh` section comment; `docs/e2e.md` Layout and driver
  rows name `common.sh` as the helpers' home (commit 8).
- Commit 5 (`wait_for_exit`): Verified 4 lands in its doc comment, and
  in `tests/e2e/AGENTS.md`'s stress bullet (commit 8).
- Commit 6 (`stress.sh`): Rationale 4, 5 and 6 land in the file header,
  `slow_read`, `probe_refused`, `drain_step` and `run_cluster`.
- Commit 7 (Makefile): Rationale 5 and 7 and Verified 2, 11 and 12 land
  in the stress block's comments; the parameters are the values.
- Commit 8 (documents): Rationale 3 to 7 land in `docs/e2e.md`'s Stress
  definition and its decision-log table of 2026-10-28, and in
  `tests/e2e/AGENTS.md` and the README.
- Commit 9 (log): Rationale 2 and 8 land in the M6 close entry and the
  criteria.
- Verified 14 and 15 are forms the close follows; they need no home.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit:

- Commit 7's `STRESS_ROWS` is a value the executor confirms by running
  the step on this machine: the body must still be streaming when the
  signal goes out, which the step's own wait for the first byte and the
  refused-connection probe show. If a run shows the read finishing
  before the signal, the row count goes up and the comment keeps "not
  measured".
- The close (commit 9) is written after `make test-stress` is green on
  both clusters here; the entry names no build number, because the
  build of the final head runs after it (Verify).
- Commit 3's log entry and commit 9's close entry are two entries, not
  one, because they record two events.

## Commits

1. `docs(plan): stress-drain-plan.md, the stress's drain step on both clusters, readiness without the drain signal, and the close of M6`
2. `refactor(httpapi,cmd): readiness is the dial alone, and the drain signal leaves the request context and the tests`
3. `docs(adr,brief,cmd,httpapi,log): the drain refuses new connections and readiness never reads the lifecycle`
4. `test(e2e): request, expect, login, create_table, ingest and compare_csv move to common.sh, and compare_csv takes the tool`
5. `test(e2e): wait_for_exit waits for a draining server to end on its own`
6. `test(e2e): stress.sh runs the drain step, one slow read across SIGTERM, a refused connection and a clean shutdown`
7. `test(e2e): make test-stress starts one server per cluster under a long grace and a small budget and runs the drain step on both`
8. `docs(e2e): e2e.md, tests/e2e/AGENTS.md and the README carry the stress's grace, its clusters and what the drain step asserts`
9. `/doc-discipline all docs/stress-drain-plan.md internal/httpapi cmd/qdb_rest tests/e2e docs` and its fix commits, if any
10. `docs(log): M6 closed, the criteria reconciled, Next item 1 removed`
11. `docs(log): stress-drain-plan.md deleted, facts moved to ADR-0004, docs/e2e.md, tests/e2e/AGENTS.md and the comments of internal/httpapi, cmd/qdb_rest and tests/e2e`
12. Verify: push `sc-19567/rr-stress-drain`, build its head in Buildkite,
    wait for the result. Green: report the build number. Red: fix with
    further small commits on this branch, push, build again. Beside it,
    `make test-stress QDB_REST_BIN=bin/qdb_rest` on this machine against
    both clusters, green, is the by-hand evidence the close names.

## Open questions and recommendations

1. The base is three commits ahead of origin. Recommendation: the merge
   stage of this unit pushes the base, as it always does.
