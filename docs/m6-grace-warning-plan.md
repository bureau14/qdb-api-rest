# M6: the shutdown grace as a key, and the plain-text warning -- Plan

Status: draft

## Outcome

When this unit lands:

- The deadline the binary drains under on SIGTERM is the configuration
  key `listen.shutdown_grace`, so also `--listen-shutdown-grace` and
  `QDB_REST_LISTEN_SHUTDOWN_GRACE`, eight seconds by default, and a
  value that is not positive refuses the start naming the key. The
  constant `shutdownGrace` in `cmd/qdb_rest/main.go` is gone, and
  `examples/qdb_rest.yaml` carries the key at its default.
- The server logs one line at warn level at startup when the HTTP
  listener is on and the cluster has a public key, because access
  tokens then travel in plain text. No key silences it.
- The round trip's driver takes the log file of each server it was
  started with, asserts the warning's message is present in the secure
  server's log and absent from the insecure server's log, and skips the
  step for a server that came as a URL.
- `docs/e2e.md` and `tests/e2e/AGENTS.md` carry the two options and the
  step, and the Handoff item for the warning is deleted from
  `docs/log.md`.

Left for later units: the stress's drain step and the close of M6, as
one slice (`docs/log.md`, Next), which runs its server under a long
grace through the key this unit adds; the HTTPS steps of the round trip
(`docs/log.md`, Handoff item 2).

## Verified facts

- The grace is a constant, eight seconds, used once to build the drain
  context (`cmd/qdb_rest/main.go:56-61`, `:180-182`). Its comment says
  the value is not measured and that whatever kills the binary after a
  stop waits longer than this (2026-10-26).
- `shutdown` drains every listener in parallel under that context and
  then closes the cluster under the same context, logging a warning
  when the cluster does not drain in time (`cmd/qdb_rest/main.go:180-191`).
- The Flight server's `Shutdown` runs `GracefulStop` on a goroutine and
  calls `Stop` when the context ends, and its comment says this keeps
  shutdown inside "the harness's SIGTERM-to-SIGKILL window"
  (`internal/flightsql/server.go:90-107`). That claim is about the
  harness, and it stops being true of the binary once the grace is an
  operator's setting.
- `newListeners` adds the HTTP listener at its step 2 when
  `cfg.Listen.HTTP != ""` and logs one info line for it
  (`cmd/qdb_rest/main.go:136-139`). The cluster is in the context
  before `newListeners` runs (`cmd/qdb_rest/main.go:221-222`, `:242`),
  and `Cluster.Secured` answers whether a public key file is configured
  (`internal/qdb/cluster.go:97-100`).
- Every key is one field of `Config` with a `yaml` tag and a `help`
  tag; the flag and the variable derive from the path
  (`internal/config/config.go:218-240`, `:247-255`). `Listen` has the
  three addresses (`internal/config/config.go:38-43`).
- `positive(key, d)` refuses a duration that is not strictly positive
  and names the key (`internal/config/config.go:566-571`); `validate`
  checks the two required listeners first (`internal/config/config.go:631-637`).
- `TestLoadIsTheLayerFold` draws `0s` and `-1s` for every duration key
  and checks that `Load` accepts a fold if and only if `validate` does
  (`internal/config/config_test.go:44-57`, `:112-178`), so the new
  bound is covered without a test of its own.
- `TestExampleIsTheDefaults` loads `examples/qdb_rest.yaml` and requires
  it to equal `Default()` (`internal/config/config_test.go:265-272`), so
  the example changes in the commit that adds the key.
- The harness starts each round-trip server with the three listener
  flags and no log flag, so the server logs JSON at info to the file the
  Makefile names (`tests/e2e/Makefile:33-45`, `:76-78`, `:134-141`;
  `internal/config/config.go:177`). A server given as a URL has no log
  file (`tests/e2e/Makefile:129-131`).
- `stop_server` sends SIGTERM, waits up to ten seconds in tenths, then
  SIGKILL, and its comment names `shutdownGrace`
  (`tests/e2e/common.sh:146-160`).
- The driver's `main` runs the insecure cluster, then the secure one,
  and dies when either failed (`tests/e2e/round-trip.sh:447-456`);
  `parse_args` takes named options only (`:38-56`). `TYPES_QUERY` and
  `TYPES_CSV` are the driver's pattern for a text the Go side must
  spell the same (`tests/e2e/round-trip.sh:319-325`).
- The owner decided the key's name, the assertion's home and that both
  logs are asserted on 2026-10-26 (the seed's challenge round). The
  warning itself and that a load-balanced deployment is fine with it
  are the owner's (`docs/log.md`, Handoff item 1, 2026-10-24).

## Design

### `internal/config/config.go`

```go
// Listen holds the listener addresses and the drain deadline. The HTTP
// listener is off unless an address is set, and it is the only one that
// can be off (ADR-0001). [internal/config/config.go:36-43]
type Listen struct {
	HTTP          string        `yaml:"http" help:"HTTP listener address; off unless set"`
	HTTPS         string        `yaml:"https" help:"HTTPS listener address"`
	Flight        string        `yaml:"flight" help:"Flight SQL listener address (gRPC over TLS)"`
	ShutdownGrace time.Duration `yaml:"shutdown_grace" help:"deadline for draining in-flight calls on every listener and the session pool on SIGTERM"`
	// style: doc comment alone
	// because: a struct of tagged fields, and the help tag is each field's contract
}
```

```go
// Default returns the configuration used when nothing is specified: [existing doc comment]
//   - the HTTPS and Flight listeners on their customary ports, and eight
//     seconds to drain on SIGTERM [Rationale: default stays eight seconds]
func Default() Config {
	// style: doc comment alone
	// because: a literal, and the doc comment's list is its contract
	//
	// at ShutdownGrace: eight seconds is not measured; anything that stops
	// the binary and then kills it waits longer than this before the kill
	// [cmd/qdb_rest/main.go:56-61 today; Rationale: default stays eight seconds]
}
```

```go
// validate rejects impossible configurations before the server starts. [existing]
func validate(cfg Config) error {
	// style: doc comment alone
	// because: a list of checks whose error strings say what each requires
	//
	// at positive("listen.shutdown_grace", ...): after the two required
	// listeners, in field order; a zero grace would cut every call at
	// SIGTERM and a negative one is meaningless [internal/AGENTS.md, Code:
	// config owns the simple bounds and names the key]
}
```

### `examples/qdb_rest.yaml`

```yaml
listen:
  # ... the three addresses as they are ...
  # Deadline for draining in-flight calls on every listener and then the
  # session pool on SIGTERM. Whatever stops the binary and then kills it
  # must wait longer than this before the kill.
  shutdown_grace: "8s"
```

### `cmd/qdb_rest/main.go`

The constant `shutdownGrace` and its comment are removed. The `listener`
interface's comment, which says Shutdown drains "under the caller's
deadline", stays as it is.

```go
// newListeners builds the listeners from config and logs what each one
// serves. HTTP is off unless configured, and the other two always start
// (ADR-0001). [existing doc comment]
func newListeners(ctx context.Context, cfg config.Config, handler http.Handler) ([]listener, error) {
	// style: doc comment + numbered overview + step comments
	// because: four steps whose order matters, already narrated; step 2 gains one claim
	//
	// 2. HTTP, when configured, with one warning when the cluster is secure,
	//    because access tokens then travel in plain text and a deployment
	//    inside a VPC is the one case that accepts it [docs/log.md, Handoff
	//    item 1 (owner, 2026-10-24); Rationale: no key silences the warning]
	//    - the condition is read from the cluster in the context, because
	//      Secured is the one reading of "has a public key" in the binary
	//      [internal/qdb/cluster.go:97-100]
	//    - the message text is spelled once here and once in the round
	//      trip's driver, and a change to one changes the other
	//      [tests/e2e/round-trip.sh, PLAINTEXT_WARNING; Rationale: the
	//      driver greps the message]
	//    if cfg.Listen.HTTP != "" {
	//        listeners = append(listeners, newHTTPServer(...))
	//        log.InfoContext(ctx, "listening", ...)
	//        if qdb.ClusterFrom(ctx).Secured() {
	//            log.WarnContext(ctx, "http listener on a secure cluster: access tokens travel in plain text",
	//                "addr", cfg.Listen.HTTP,
	//                "hint", "serve the HTTPS listener only, unless the network between clients and this server is already encrypted")
	//        }
	//    }
}
```

```go
// serveUntilDone serves all listeners until the context is cancelled by
// SIGINT or SIGTERM or one listener fails. It returns the first terminal
// error, or nil on a clean cancel. It shuts nothing down, because
// shutdown does that under the configured grace. [existing, one word changes]
func serveUntilDone(ctx context.Context, listeners []listener) error {
	// style: doc comment alone
	// because: unchanged; the doc comment names the constant today and names the key after
}
```

```go
// shutdown drains the listeners first and the cluster second, both under
// the grace of listen.shutdown_grace. The listeners drain in parallel.
// The Flight server drains next to the HTTP servers and is not stopped
// hard, because the graceful drain covers Flight streams (docs/brief.md,
// Resilience). The order matters, because in-flight calls on either plane
// hold sessions. Once every listener has drained, the cluster has no
// callers left, and its close waits only for idle sessions.
// [cmd/qdb_rest/main.go:173-179 today, the constant's name replaced by the key's]
func shutdown(ctx context.Context, grace time.Duration, listeners []listener, cluster *qdb.Cluster) {
	// style: doc comment alone
	// because: unchanged shape; the doc comment already tells the order and its why
	//
	// at context.WithTimeout(context.Background(), grace): the deadline is
	// the caller's, read from the configuration once in main, so this
	// function has no constant of its own [Rationale: the key replaces the constant]
}
```

```go
func main() {
	// style: doc comment + numbered overview + step comments
	// because: six steps whose order matters, already narrated; step 6 names the key
	//
	// 6. Serve until a signal or a listener failure, then drain the
	//    listeners first and the cluster second, under listen.shutdown_grace.
	//    shutdown(ctx, cfg.Listen.ShutdownGrace, listeners, cluster)
}
```

### `internal/flightsql/server.go`

```go
// Shutdown stops accepting calls, waits for the calls in flight until the
// context ends, and then cuts the rest. [existing]
func (s *Server) Shutdown(ctx context.Context) {
	// style: doc comment alone + inline note at Stop
	// because: unchanged; the note at Stop is rewritten
	//
	// at s.grpc.Stop(): Stop cuts the calls still open when the grace ends,
	// so shutdown never outlives the grace the operator configured
	// [cmd/qdb_rest/main.go, shutdown; the harness window is the harness's
	// own claim, tests/e2e/common.sh, stop_server]
}
```

### `tests/e2e/common.sh`

```bash
# Send SIGTERM, wait up to 10s, then send SIGKILL. The wait sits above the
# default shutdown grace (listen.shutdown_grace, 8s), so a clean drain of
# a server at the default is never cut short. A server started under a
# longer grace is stopped by the step that configured it.
# Usage: stop_server <pidfile>
stop_server() {
    # style: doc comment alone
    # because: unchanged code; the comment names the key and the rule for a longer grace
    # [Rationale: the stop window stays at ten seconds]
}
```

### `tests/e2e/round-trip.sh`

```bash
# The header's usage line gains [--insecure-log <file>] [--secure-log <file>],
# and the paragraph says: each server's log file is optional, because a
# server given as a URL has none, and the warning step is skipped for it.
# [docs/e2e.md, The driver; Rationale: the driver takes the log path]

# PLAINTEXT_WARNING is the message the server logs at startup when its
# HTTP listener serves a secure cluster, the text newListeners spells
# (cmd/qdb_rest/main.go), so the log step proves the warning fires there
# and nowhere else. A change to one changes the other.
PLAINTEXT_WARNING='http listener on a secure cluster: access tokens travel in plain text'
# style: doc comment alone
# because: one constant, like TYPES_CSV [tests/e2e/round-trip.sh:319-325]

# check_log <name> <log_file> <present|absent>: the plain-text warning
# is in the server's log, or is not, by a fixed-string grep. An empty
# log file path skips the step with a line saying so, because a server
# given as a URL has no log the harness can read.
check_log() {
    # style: doc comment alone
    # because: one grep and one branch; the doc comment is the whole story
    #
    # at the empty-path branch: skipped and not failed, because a URL server
    # is a supported way to run the round trip [tests/e2e/Makefile:129-131]
    # at grep -qF: a fixed string, because the line is JSON and the message
    # is the one field the driver knows [internal/config/config.go:177]
}

main() {
    # style: doc comment alone
    # because: a sequence of four calls; the log step follows each cluster's run
    #
    # at check_log insecure "$INSECURE_LOG" absent: the insecure server also
    # serves HTTP, so an unconditional warning fails here [owner, 2026-10-26]
    # at check_log secure "$SECURE_LOG" present: the M6 criterion [docs/log.md, M6 criteria]
}
```

`parse_args` gains the two options and `INSECURE_LOG` and `SECURE_LOG`
start empty; the usage check does not require them.

### `tests/e2e/Makefile`

```make
# The test-round-trip recipe: each server the recipe starts passes its log
# file to the driver, and a server given as a URL passes none.
#   insecure_log=""; secure_log=""
#   ... start_server ...; insecure=http://...; insecure_log=$(REST_LOG)
#   $(ROUND_TRIP) run ... --insecure-log "$$insecure_log" --secure-log "$$secure_log"
# style: inline note at the two assignments
# because: the recipe's comment above already says which servers are started here
```

### `tests/e2e/AGENTS.md`

```markdown
- (the round-trip bullet gains:) The driver asserts the startup warning
  of a secure cluster behind plain HTTP by grepping each server's log:
  present in the secure server's, absent from the insecure server's.
  The message is spelled in `round-trip.sh` (`PLAINTEXT_WARNING`) and
  in `cmd/qdb_rest/main.go`, and a change to one changes the other. A
  server given as a URL has no log, and the step is skipped for it.
```

### `docs/e2e.md`

```markdown
The v2 round trip, a new item 8 after the Flight SQL drivers: 8. The warning line. The server warns at startup when its HTTP listener
serves a cluster with a public key, because access tokens then
travel in plain text (`docs/brief.md`, Observability and logging).
The driver greps each server's log for the message: present in the
secure server's, absent from the insecure server's. A server given
as a URL has no log and the step is skipped for it.

The driver: the usage line gains `[--insecure-log <file>]
[--secure-log <file>]`, and one sentence says the Makefile passes the
log of each server it started and nothing for a server given as a URL.
```

### `docs/brief.md`, Observability and logging

```markdown
- (a new bullet after the probes:) The server warns once at startup when
  the HTTP listener is on and the cluster has a public key, because
  access tokens then travel in plain text. Nothing silences it: a
  deployment whose network already encrypts the path, inside a VPC
  behind a load balancer, reads the line and goes on (owner, 2026-10-24).
```

## Rationale

| decision                                                                            | why                                                                                                                                | rejected, and why                                                                                                             | gained                                                  | given up                                                     | settled by                                         |
| ----------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------------ | -------------------------------------------------- |
| The key is `listen.shutdown_grace`                                                  | the owner's choice of the three names offered                                                                                      | a new `shutdown` block (one key alone in a block) and a `server` block (nothing else would sit in it)                         | no new block; the key sits with the listeners it drains | the pool's drain runs under a key named after the listeners  | owner, 2026-10-26                                  |
| The default stays eight seconds and the key must be positive                        | the criterion says the current constant is the default; a zero grace would cut every call at SIGTERM                               | a longer default, because nothing measures what a drain needs (ADR-0015)                                                      | the shipped behaviour is unchanged                      | none                                                         | `docs/log.md`, M6 criteria; proposal for the bound |
| The key replaces the constant, and `shutdown` takes the duration as a parameter     | `main` is the one reader of the config, and `shutdown` keeps no constant of its own                                                | `shutdown` reading a package variable, because the binary has no package-level state (`docs/brief.md`, Development standards) | one place the grace is read                             | one more parameter                                           | `internal/AGENTS.md`, Code; proposal               |
| The Flight `Shutdown` note names the configured grace and not the harness window    | the window is the harness's claim about itself, and an operator's grace can exceed it                                              | leaving the note, because it would be false for a grace above ten seconds                                                     | a comment that is true for every grace                  | none                                                         | `tests/e2e/common.sh:146-149`; proposal            |
| The stop window of `stop_server` stays at ten seconds and its comment names the key | the window covers the default grace, which is what the round trip runs under; a step that sets a longer grace stops its own server | the window reading the grace from an argument, because no step today sets one, and the stress slice adds its own step         | no change to a helper every target uses                 | a server above ten seconds is the configuring step's to stop | proposal                                           |
| One warn line in `newListeners` at step 2, read from `Cluster.Secured`              | the condition and the listener are both in hand there, and the log is already the startup's                                        | a line in `main` before step 5, because it would read the config a second time for what step 2 already decides                | one place that knows the HTTP listener exists           | none                                                         | proposal                                           |
| No key silences the warning                                                         | the load-balanced deployment is fine with the line, and v1 did not allow the combination at all                                    | a `listen.allow_plain_http` key, because the owner named no case that needs silence                                           | nothing to configure                                    | an operator inside a VPC reads one warning per start         | `docs/log.md`, Handoff item 1 (owner, 2026-10-24)  |
| The driver takes the log path per server as an option and greps the message         | the assertion sits with the other steps and the Makefile stays the one source of paths                                             | the Makefile grepping after the driver, because the assertion would live outside the driver next to the server lifecycle      | one driver that asserts everything                      | two options that are empty for a URL server                  | owner, 2026-10-26                                  |
| Present on secure, absent on insecure                                               | both servers serve HTTP, so an unconditional warning must fail too                                                                 | the criterion's secure-only grep, because it passes a warning that fires on every start                                       | the condition is proven, not only the line              | none                                                         | owner, 2026-10-26                                  |
| The message is a fixed string spelled in the driver and in `main.go`                | the line is JSON, and the message is the one field the driver knows; a fixed-string grep is pass or fail with no timing (ADR-0015) | matching the JSON by key with `jq`, because the step would then parse a log format the driver knows nothing else about        | one grep                                                | two spellings to keep in step, as `TYPES_CSV` already is     | proposal                                           |
| A URL server skips the log step with a line saying so                               | a server given as a URL is a supported way to run the round trip and has no log                                                    | failing the round trip, because it would make the URL form unusable                                                           | the URL form keeps working                              | the step is unproven in that form                            | proposal                                           |

## Knowledge

Commit 2, `feat(config)`: the `Listen` doc comment and the help tag of
`ShutdownGrace`; `Default`'s doc list and the note at the field (the
value is not measured, the kill must wait longer); the note at the
`positive` check in `validate`; the comment under `listen` in
`examples/qdb_rest.yaml`. Rationale rows: the key's name, the default
and the bound.

Commit 3, `feat(cmd)`: the doc comment of `shutdown` and the note at its
context; `serveUntilDone`'s doc comment; step 6 of `main`; the note at
`Stop` in `internal/flightsql/server.go`. Rationale rows: the key
replaces the constant, the Flight note.

Commit 4, `docs(e2e)`: `stop_server`'s comment in `tests/e2e/common.sh`.
Rationale row: the stop window.

Commit 5, `feat(cmd)`: step 2 of `newListeners` and the warning's
`hint`; the new bullet of `docs/brief.md`, Observability and logging,
which is the owner's decision's permanent home now that the code
exists (`docs/AGENTS.md`, routing table: architecture is the brief's).
Rationale rows: one warn line in `newListeners`, no key silences it.

Commit 6, `test(e2e)`: the header of `round-trip.sh`, `PLAINTEXT_WARNING`,
`check_log`, the two notes in `main`, the two assignments in the
Makefile recipe. Rationale rows: the driver takes the log path, present
and absent, the fixed string, the URL skip.

Commit 7, `docs(e2e)`: `docs/e2e.md` item 8 and the driver's usage;
`tests/e2e/AGENTS.md`'s round-trip bullet.

Commit 10, `docs(log)`: Handoff item 1 deleted, because the work that
consumes it landed; Next item 1 rewritten to the remaining slice, the
stress's drain step and the M6 close; the entry naming where the facts
went.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit: commit 6 spells `PLAINTEXT_WARNING`
from the message commit 5 wrote, so the executor copies the string from
`main.go` and does not retype it; commit 5's brief bullet lands with
the code, because the brief is the decision's home and the handoff item
is deleted in commit 10.

## Commits

1. `docs(plan): m6-grace-warning-plan.md, the shutdown grace as listen.shutdown_grace and the plain-text warning asserted by the round trip` (this document)
2. `feat(config): listen.shutdown_grace is the drain deadline on SIGTERM, eight seconds by default and positive, pinned in the example`
3. `feat(cmd): shutdown drains the listeners and the cluster under listen.shutdown_grace`
4. `docs(e2e): common.sh's stop window is sized above the default grace and names the key`
5. `feat(cmd): the server warns at startup when the HTTP listener is on and the cluster is secure`
6. `test(e2e): the round trip takes each server's log and asserts the plain-text warning on the secure one and its absence on the insecure one`
7. `docs(e2e): e2e.md and tests/e2e/AGENTS.md carry the log options and the warning step`
8. `/doc-discipline all cmd/qdb_rest internal/config internal/flightsql/server.go tests/e2e docs/e2e.md`, committed as `docs(...)` commits if it changes anything
9. `/doc-discipline all check docs/m6-grace-warning-plan.md cmd/qdb_rest internal/config tests/e2e`, findings fixed before the next commit
10. `docs(log): m6-grace-warning-plan.md deleted, facts moved to docs/brief.md, docs/e2e.md, tests/e2e/AGENTS.md and the comments of cmd/qdb_rest, internal/config and tests/e2e`
11. Verify: push `sc-19567/rr-m6-grace-warning`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. The warning's message text and hint are the plan's wording;
   recommendation: keep them, because the driver greps the message and
   the text is reviewed in the diff of commit 5.
2. The round trip in Buildkite starts both servers, so the log step runs
   there on every platform; recommendation: no CI change, and the URL
   form stays the one case where the step is skipped.
