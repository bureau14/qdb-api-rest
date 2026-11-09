# Plan: the log stays a stream, and Windows service mode is the package's wrapper

Thread: rr-windows-service: M10 Windows service mode
Status: draft

## Outcome

When the unit lands, an accepted ADR-0020 says that the binary writes
its log to stdout as one stream, writes no log file, rotates nothing
and has no service mode of its own, and that on Windows the installer
runs the binary under a service wrapper that captures and rotates
stdout and reports the service's lifecycle to the Event Log. The
brief's observability paragraph, project structure, M10 line and
ecosystem table say the same in one sentence each, and
`cmd/qdb_rest/AGENTS.md` has one bullet so an agent about to add a
service path reads the rule first. The log marks M9 done and M10 done,
because every item of M10 is work in another repository (owner,
2026-11-09), and the handoff items M9 did not consume stay listed as
open after the milestones.

Left for later: the wrapper's configuration and the installer change,
which live in `qdb-pkg-windows`; the documentation rewrite, the
migration notes and the version registration, which live in
`qdb-documentation` and `qdb-release`; and the HTTPS steps of the round
trip, which stay open.

## Verified facts

- The process logger writes to `os.Stdout`, at the one call site of
  `observe.NewLogger`, which takes an `io.Writer`
  (`cmd/qdb_rest/main.go:256`, `internal/observe/observe.go:62`).
- The vendor tree holds `golang.org/x/sys/windows` and
  `windows/registry` and no `windows/svc`, and no rotation module is a
  dependency (`vendor/modules.txt:315-319`, `go.mod`).
- The brief promises a rotating file through `natefinch/lumberjack`,
  the Event Log for lifecycle and fatal events and an
  `observability.log.file` key, in one paragraph (`docs/brief.md:823-828`);
  names Windows service mode in the project structure (`:864`) and the
  M10 line (`:1047`); and says the application logs to stdout and
  manages no log files (`:29`, `:812-816`).
- The Windows installer is `qdb-pkg-windows/qdb-server.iss`. It ships
  `qdb_rest.exe` and `qdb_rest_service.exe` (`:96-97`) and registers the
  service by running `qdb_rest_service.exe /install` (`:127`), as it
  registers qdbd through `qdb_service.exe /install` (`:86`, `:110`).
- `qdb_rest_service.exe` is the old server linked into a second binary
  under `kardianos/service`, Windows-only, reading its config path from
  the registry value `ConfigFile` under
  `SYSTEM\CurrentControlSet\Services\qdb_rest` and answering the verbs
  `install`, `uninstall`, `remove`, `start`, `status` and `stop`
  (`master:apps/qdb_rest_service/main.go`).
- Go's `log/slog` has a logger, a handler and a plain `io.Writer` and
  no rolling file writer (standard library). lumberjack is unmaintained
  and Caddy replaced it with the fork timberjack in 2025; Consul, Nomad
  and Vault carry their own rotating writer; Kubernetes removed file
  logging and rotation from its components in 1.26 (KEP-2845) and ships
  `kube-log-runner` for Windows services and distroless images (session
  of 2026-11-09, sources in the unit's Inbox thread).
- The newest release of `duckdb-go-bindings` is the pinned `v0.20000.0`
  (`go list -m -versions`, 2026-11-09), so the Blocked item stands.
- M9's criteria 1 and 3 are in the tree (`internal/httpapi/v1/`,
  `tests/e2e/compare-v1.sh`, `docs/brief.md:310-326`). Criterion 2 ran green by hand
  on 2026-11-09: `make test-round-trip` seeded both clusters (seed 1794215471) and `make test-v1-compare` passed on both, the old server
  built from `master` at the same time.
- The ADR index ends at 0019 (`docs/adr/README.md`), so the new record
  is 0020.

## Design

No code changes. Every entry is a document.

### `docs/adr/0020-stream-only-logging.md`

```
# ADR-0020: Logging is one stream, and a Windows service is the package's wrapper
Status: accepted, Date: 2026-11-09

Context
- the brief's vision: stdout logging, journald and the container runtime own
  capture [brief.md:29, :812-816]
- a Windows service has no stdout, so the brief planned a rotating file,
  lumberjack and the Event Log in the binary [brief.md:823-828]
- the old package registers qdb_rest_service.exe, the old server linked into a
  second binary under kardianos/service [qdb-server.iss:97,127;
  master:apps/qdb_rest_service/main.go]
- slog has no rolling writer; the reference projects split, and the ones
  that dropped file logging did so for the Windows-service case [Verified
  facts]

Decision
- the binary writes its log to stdout as one stream; it writes no file,
  rotates nothing and has no service mode of its own [owner, 2026-11-09]
- on Windows the installer runs the binary under a service wrapper that
  captures and rotates stdout and reports lifecycle to the Event Log; the
  wrapper and its configuration live in qdb-pkg-windows [owner, 2026-11-09;
  Rationale: the wrapper]
- WinSW is the wrapper proposed; the package may pick an equivalent without
  a change here [Rationale: WinSW]

Consequences
- internal/observe keeps one writer, the one NewLogger takes, and no key
  names a file [main.go:256, observe.go:62]
- cmd/qdb_rest gains no windows/svc handler and no Event Log writer; the
  vendor tree gains no module for either
- qdb_rest_service.exe leaves the installer with the SPA assets; the
  wrapper replaces its /install line [qdb-server.iss:97,103,127]
- a start the wrapper cannot complete is the wrapper's to report; the binary's
  own startup failures stay on stderr and exit 1 or 2 as today [main.go]

Alternatives rejected
| a log.file block with a rotating writer of our own (the Consul pattern) | 150 lines to own for one platform, when a wrapper does it outside the binary |
| a log.file block over lumberjack or timberjack | a module to carry for the same reason; lumberjack is unmaintained |
| a native service handler and Event Log writer in the binary (the brief's text) | needs the file sink to be usable at all, and doubles the service lifecycle code the wrapper already has |
| a second binary linking the server, as master's qdb_rest_service.exe | the server would have to be a library, and the lifecycle code moves rather than disappears |
| a file without rotation (etcd's default) | on Windows nothing rotates, so the file grows until the service restarts |
```

The ADR's Decision says what is; its Context cites the brief and the
installer and never the plan or the thread.

### `docs/adr/README.md`

One row: `0020`, "Logging is one stream, and a Windows service is the
package's wrapper", accepted.

### `docs/brief.md`

```
Observability and logging, the Windows bullet [brief.md:823-828], rewritten:
- Windows has no stdout for a service, and the binary still writes one stream:
  the installer runs it under a service wrapper that captures and rotates
  stdout and reports the service's lifecycle to the Event Log (ADR-0020).
  No key names a log file.

Project structure [brief.md:864]:
- cmd/qdb_rest/  entry point (all platforms; a Windows service is the
  package's wrapper, ADR-0020)

Milestones, M10 [brief.md:1044-1048], rewritten:
- M10 -- Release: the work that lives in other repositories, and no unit of
  this one (owner, 2026-11-09): the docs rewrite in qdb-documentation, with
  the cluster-endpoint and Prometheus remote-storage sections removed and
  migration notes for them; the VERSION file's registration in qdb-release;
  and the Windows installer running the binary under a service wrapper in
  qdb-pkg-windows (ADR-0020). "hardening" is dropped [owner, 2026-11-09]

Ecosystem table [brief.md:92-105], one row:
- qdb-pkg-windows | The Inno Setup installer. Registers qdb_rest as a Windows
  service through a wrapper (ADR-0020) and drops the SPA assets.
```

### `cmd/qdb_rest/AGENTS.md`

```
- The binary has no service mode and writes its log to stdout only. On
  Windows the installer runs it under a service wrapper (ADR-0020), so no
  windows/svc handler, Event Log writer or log file belongs here.
```

### `docs/log.md`

```
Current state, rewritten in place [docs/AGENTS.md, Project log]:
- Last updated: 2026-11-09
- M9 row: done, closed 2026-11-09; M10 row: done, closed 2026-11-09, note
  "its work lives in other repositories"
- the M9 exit criteria block is removed and no M10 block replaces it
- In flight: this unit (sc-19567/rr-stream-only-logging)
- Next: none
- "Handoff to M9" becomes "Open after the milestones", the three items
  unchanged, plus the brief's Risks item 5, the session budget lent to
  qdb-duck, which the brief defers to this point [brief.md, Risks 5]
- Blocked on: unchanged

Entries, newest first, one to three lines each:
- 2026-11-09 -- M9 and M10 closed: the comparison ran green by hand on both
  clusters, and M10's work lives in other repositories (owner decision);
  docs/brief.md, Milestones.
- 2026-11-09 -- the log is one stream: owner decision, ADR-0020; the brief's
  observability paragraph and M10 line follow.
```

## Rationale

| decision                                                                                           | why                                                                                                                                       | rejected, and why                                                                                                                                          | gained                                                     | given up                                                              | settled by                               |
| -------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | --------------------------------------------------------------------- | ---------------------------------------- |
| the log is one stream: no file, no rotation in the binary                                          | the reference projects that dropped file logging did so for the Windows-service case, and a wrapper covers it                             | a `log.file` block with a rotating writer, own or vendored: code or a module to own for one platform                                                       | no new key, writer or module                               | an operator on Windows configures the wrapper, not the binary         | owner, 2026-11-09                        |
| the wrapper: the installer runs the binary under a service wrapper                                 | a service has no stdout, and the wrapper captures, rotates and reports lifecycle without a line in the binary                             | a `windows/svc` handler and Event Log writer in the binary: unusable without a file sink, and lifecycle code the wrapper already has                       | the binary is one program on every platform                | the Windows package carries a second executable and its configuration | owner, 2026-11-09                        |
| WinSW is the wrapper named                                                                         | MIT, one executable and one XML file, stdout rotation modes, Event Log reporting                                                          | NSSM: an installer-style tool with registry-held configuration; the old `qdb_rest_service.exe`: links the server, so the server would have to be a library | a concrete name for the package to start from              | nothing; the package may pick an equivalent                           | proposal                                 |
| the decision is an ADR                                                                             | it constrains `internal/observe` and `cmd/qdb_rest` until reversed, and a reader of the brief's old paragraph would rebuild the file sink | a plan decision-log row: dies with the plan                                                                                                                | one place a later agent reads before adding a service path | one more ADR                                                          | `docs/AGENTS.md`, ADRs                   |
| a start whose log file cannot open refuses the start                                               | fail fast is the project norm                                                                                                             | the library's lazy open: a silent log                                                                                                                      | nothing: moot once no file exists                          | nothing                                                               | owner, 2026-11-09; `plan only`           |
| M9 closes in this unit's log commit                                                                | criteria 1 and 3 are in the tree and criterion 2 ran green by hand on 2026-11-09                                                          | a separate docs unit for the close: a plan for one table row                                                                                               | the milestone table is current when the unit lands         | nothing                                                               | owner, 2026-11-09; the run of 2026-11-09 |
| M10 closes with this unit, and the brief's M10 line names the three repositories its work lives in | every M10 item is a change to qdb-documentation, qdb-release or qdb-pkg-windows, and none is a unit of this repository                    | M10 kept open with exit criteria this repository cannot meet: a milestone no unit here could close                                                         | the milestone table ends                                   | the word "hardening", which no document defined                       | owner, 2026-11-09                        |

## Knowledge

- Commit 2, `docs(adr)`: ADR-0020 holds the Rationale rows "the log is
  one stream", "the wrapper" and "WinSW", with the installer facts of
  Verified facts in its Context; the README row. The row "a start whose
  log file cannot open" is `plan only`.
- Commit 3, `docs(brief)`: the four brief edits of the Design (scope
  and intent: the brief's row of the routing table). The row "the
  decision is an ADR" is `plan only`.
- Commit 4, `docs(agents)`: the `cmd/qdb_rest/AGENTS.md` bullet (a rule
  of a folder).
- Commit 5, `docs(log)`: the milestone rows, the removed criteria block,
  the open items, the two entries (progress and dated events: the log's
  rows). The row "M9 closes" lands here, and "M10 closes" here and in
  the brief's M10 line (commit 3).
- Commit 7, `docs(log)`: the deletion entry and `Last updated`.

## How the knowledge lands

The build stage is `/rr-build`, which writes every document row of the
sections above and reconciles the tree against this plan before the
build-stage message. Specific to this unit: commit 5 marks M9 done on
the comparison's result recorded in Verified facts, and M10 done on the
owner's decision in the Rationale, so no criteria block is written.

## Commits

1. `docs(plan): stream-only-logging-plan.md, the log stays a stream and Windows service mode is the package's wrapper`
2. `docs(adr): ADR-0020, the log is one stream on stdout and a Windows service runs the binary under a service wrapper`
3. `docs(brief): the observability paragraph, the project structure and the ecosystem table say the wrapper, and the M10 line names the repositories its work lives in`
4. `docs(agents): the cmd agents file says the binary has no service mode and logs to stdout only`
5. `docs(log): M9 and M10 closed, the release work lives in other repositories`
6. `/doc-discipline check docs/stream-only-logging-plan.md docs/brief.md docs/adr cmd/qdb_rest docs/log.md`, findings fixed before the next commit.
7. `docs(log): stream-only-logging-plan.md deleted, facts moved to ADR-0020, docs/brief.md and cmd/qdb_rest/AGENTS.md`
8. Verify: push `sc-19567/rr-stream-only-logging`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

none
