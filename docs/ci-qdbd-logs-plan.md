# Plan: the qdbd death under TestRoundtrip on Windows

Status: approved

This is a troubleshooting unit. Its scope is not known in advance, so
the commits after the first phase are decided by evidence, not listed
here in full, and this plan is revised as the evidence comes in. Every
claim about the failure in this document carries where it was observed.
Nothing is concluded from intuition.

## Outcome

The goals, in this order, each pursued as far as the evidence allows:

1. Why qdbd dies under `TestRoundtrip` on the Windows agents: the
   daemon's own account of its death, from its log, its console output
   and its error dump.
2. What payload produces it: the request sequence and the generated
   tables, narrowed until the smallest sequence that kills the daemon is
   known, or until the evidence says the payload is not the cause.
3. A reproduction in C or C++ against the C API, and a ticket for the
   qdbd R&D team carrying it.

A later goal may turn out impossible (the payload is not the cause, or
the cause is outside qdbd); then this unit records what was established
and stops there.

Phase 1 is the telemetry that every later phase reads. When it lands,
every per-platform build step uploads an archive of each qdbd daemon's
log directory, console output and error dump with its test report,
whether the Go tests pass or fail, plus the rapid fail files the tests
wrote, and the REST server's log for a failed test.

## Verified facts

### The failure

- Three occurrences, all in build 90 (`qdb-api-rest`, commit `9653c1b`,
  branch `sc-19567/rr-prose-audit-touched`): windows-core2 first attempt
  (job `01a113a2-8267-499b-b140-68443dd1fe66`, finished 2026-10-07
  00:03Z), windows-haswell first attempt
  (`01a113a2-826a-4177-9ef3-51958bb5826d`, 00:02Z), windows-core2 manual
  retry (`01a113fb-aa2f-4133-9f0f-4ae9d39e187d`, 01:32Z). The
  windows-haswell manual retry passed (`01a113fb-b57d-4ecb-ab85-88f7bc0dc63e`).
  Both CPU variants fail; the failure is intermittent.
- The signature is the same in all three: inside `TestRoundtrip`, a read
  over HTTP fails with 503 and `reader_init (operation=reader_init,
tables=1): Connection refused.` (`roundtrip_test.go:102`,
  `checkReadFormats`); rapid's replay fails earlier at `create` with
  `circuit breaker open`, so rapid reports "flaky test, can not
  reproduce"; `TestRoundtripDeduplicated` then finds qdbd not answering
  on 2836. The insecure daemon is gone from the first failing read on.
- The daemon dies late: `TestRoundtrip` had run 532 s, 382 s and about
  7 s (the retry, where only a handful of cases ran before the death)
  when it failed; it passed in 553 s on windows-core2 in build 89.
- The failing cases' first tables (fixture `columnTypes` order: 0 int64,
  1 double, 2 string, 3 symbol, 4 blob, 5 timestamp): 5 columns
  `[5,0,1,3,2]`, 1 row; 5 columns `[5,5,3,3,2]`, 29 rows; 3 columns
  `[4,4,0]`, 1 row. No type, width or row count is common to all three.
  The read that failed follows a create, an empty read and an ingest of
  the same tables (`roundtrip_test.go:166-212`), so the death happened
  during the ingest or during `reader_init` itself; which one is unknown
  until a daemon log exists.
- The qdbd nightly differs by one commit between the last pass and the
  failures: build 89 ran quasardb-build 2720 (`9c0a2b1903`), build 90
  ran 2741 (`f29250aed8`, "Disable test-runner reconnect tests on
  Windows", tests only; quasardb `git log 9c0a2b1903..f29250aed8`).
  `ea118bba37` "Replace the server B-Tree accumulator" is in both.
  Builds 66 to 89 all passed windows-core2 except one unrelated failure
  (68) and one cancellation (88) (`bk api pipelines/qdb-api-rest/builds`).
- The same agent (`default-windows-amd64-h-2-79Mort`) passed build 89
  and failed both build 90 core2 runs; haswell failed on a different
  agent. The agent is not the discriminator.

### The telemetry

- Buildkite runs the post-command and pre-exit hooks plugins first and
  the repository last (hooks documentation, job hook order table). In
  build 90's retry job the plugin's post-command hook logged "Generating
  test report" at 1791336722072 and the repository pre-exit hook started
  at 1791336727068, after the upload. Any archive a hook makes misses
  the upload.
- The test-report plugin uploads in its post-command hook
  (`~/git/qdb-test-report-buildkite-plugin`, origin/master `3f2d988`).
  A `job.artifacts` item is `{name, input_path}`; a glob is resolved
  against the job's cwd with `recursive=True` and matches files only; an
  empty match warns and never fails (`lib/artifact_inputs.py`).
- `cleanup.sh::archive` writes
  `logs/qdbd-logs-<epoch>-{insecure,secure}.tar.gz` relative to the cwd
  for each log directory that exists. At qdb-test-setup PR 5's head
  (`147c4d2b3bb6c551e9c8a33acd615ad9c1eb0518`) each archive also carries
  the daemon's console files `qdbd_log_<mode>.{out,err}.txt`, and
  `start-services.sh` passes `--log-flush-interval` from
  `QDB_LOG_FLUSH_INTERVAL_MS`, default 100.
- `stop-services.sh` kills with `pkill -SIGKILL` and `Taskkill //F`
  (`utils.sh`, `kill_instances`); stopping flushes nothing. qdbd's
  default flush interval is 3 s (quasardb `qdb/log/config.hpp:18`).
- On a fatal signal or Windows structured exception qdbd logs
  "signal caught" with a backtrace at panic level, flushes the log, and
  writes `qdbd_<pid>_error_dump.log` into its log directory
  (`qdb/application/sig_handler.cpp:591-626`,
  `qdb/sys/seh_translation.cpp:34-47`, `apps/qdbd/runner.cpp:443-452`).
  The log directory archive therefore carries the crash account when
  the death is a crash. A death with no such entry is not a crash.
- rapid writes a fail file under `testdata/rapid/<Test>/` on every
  failure, the flaky verdict included (`vendor/pgregory.net/rapid/engine.go:290-297`);
  the path is gitignored (`**/testdata/rapid/`), so nothing is in the
  checkout for CI to replay (`git ls-files` shows none).
- The httpapi tests give the REST server a JSON logger writing into a
  `bytes.Buffer` that nothing reads (`internal/httpapi/readiness_test.go:36-39`,
  `observeContext`). The server's own account of a request (the access
  line, the error mapping) is lost today.
- `30.test.sh` runs with `set -euxo pipefail` and changes to `BASE_DIR`
  before the tests (`scripts/cicd/30.test.sh:7,17`). `logs/`,
  `*.out.txt`, `*.err.txt` and `**/testdata/rapid/` are gitignored.
- No other API or tool pipeline uploads qdbd logs (qdb-api-go,
  qdb-api-python, qdb-nats-connector `.buildkite/`, 2026-10-07).

## Design

### Phase 1: telemetry

#### `scripts/cicd/00.common.sh`

Header list gains one line:

```
#   cicd_archive_qdbd_logs_on_exit -- EXIT trap: archive both daemons' logs, keep the exit status
```

New function, appended after `cicd_setup_qdb_env`, in the file's
comment-block convention:

```
# cicd_archive_qdbd_logs_on_exit -- EXIT trap for a test step script: archive
# both qdbd daemons' log directories, console files and error dumps into
# logs/, then exit with the status the script was exiting with.
#
# Install it after cd "${BASE_DIR}" with `trap cicd_archive_qdbd_logs_on_exit
# EXIT`. The archives are the submodule's (scripts/tests/setup/cleanup.sh,
# archive), so they have the shape hooks/pre-exit produces, and the test-report
# plugin uploads them from logs/qdbd-logs-*.tar.gz (.buildkite/steps/_build.yml).
# qdbd keeps running; hooks/pre-exit stops it.
#
# Inputs:  BASE_DIR -- the checkout; cleanup.sh resolves its paths from the cwd.
# Outputs: logs/qdbd-logs-<epoch>-{insecure,secure}.tar.gz, when a log
#          directory exists.
cicd_archive_qdbd_logs_on_exit() {
    # The archive has to happen here, inside the command, and not in a hook:
    # Buildkite runs the repository's post-command and pre-exit hooks after the
    # plugin's post-command hook, which is the upload. The steps:
    #
    #  1. capture the script's exit status before anything else can change it;
    #  2. archive through the submodule in a subshell that cannot fail the
    #     step: a missing tar or log directory is not a test failure;
    #  3. exit with the captured status so a red test stays red.

    # 1. The first line of the handler reads $? of the exiting command.
    local status=$?
    trap - EXIT

    # 2. cleanup.sh sources config.sh (set -xe, argument parsing) and defines
    # archive; the subshell keeps both out of this shell. qdbd stays up: its
    # log is flushed every QDB_LOG_FLUSH_INTERVAL_MS and on a fatal signal,
    # and stopping would SIGKILL it, which flushes nothing.
    (
        cd "${BASE_DIR}" \
            && source scripts/tests/setup/cleanup.sh \
            && archive
    ) || echo "cicd_archive_qdbd_logs_on_exit: archiving the qdbd logs failed; the step's status is unchanged" >&2

    # 3. The status of the tests, not of the archive.
    exit "${status}"
}

export -f cicd_archive_qdbd_logs_on_exit
```

Narrated under `narrative.md` rule 3: the order of the three steps
matters and the subshell is a deliberate construct.

#### `scripts/cicd/30.test.sh`

Header gains, after the JUnit sentence:

```
# At exit, whatever the tests' outcome, both qdbd daemons' logs are archived
# into logs/ for the test-report plugin to upload (cicd_archive_qdbd_logs_on_exit).
```

After `cd "${BASE_DIR}"`:

```
# The archive runs inside this command because the plugin uploads before any
# repository hook runs (00.common.sh, cicd_archive_qdbd_logs_on_exit).
trap cicd_archive_qdbd_logs_on_exit EXIT
```

#### `.buildkite/steps/_build.yml`

Header gains:

```
# The test step archives both qdbd daemons' logs into logs/ at its exit
# (scripts/cicd/30.test.sh) and the test-report plugin uploads them with the
# report, together with the rapid fail files a failed property test wrote;
# the pre-exit hook runs after the upload, so its own archive is unused.
```

The plugin block gains:

```
        artifacts:
          - name: "qdbd logs"
            input_path: "logs/qdbd-logs-*.tar.gz"
          - name: "rapid fail files"
            input_path: "internal/**/testdata/rapid/**/*.fail"
```

`python3 pipeline.py check` passes after the change.

#### `.buildkite/hooks/pre-exit`

Comment gains:

```
# stop-services.sh archives the log directories as well, but this hook runs
# after the test-report plugin has uploaded, so the archive Buildkite shows is
# the one scripts/cicd/30.test.sh made at its exit.
```

#### `internal/httpapi/readiness_test.go`, `observeContext`

Becomes `observeContext(t testing.TB) context.Context`: the handler is
built with `slog.LevelDebug`, the buffer is kept and written to `t.Log`
in a `t.Cleanup` when `t.Failed()`. Doc comment:

```
// observeContext carries a REST server logger whose output is shown with
// the test's own when the test fails, and discarded otherwise. A failed
// round trip against the live daemon is read from three sides: the
// test's draws, the server's log and the daemon's log
// (docs/ci-qdbd-logs-plan.md while it is alive; internal/AGENTS.md, Tests).
//
// The level is debug for the duration of that investigation, so a
// failure shows every request the server saw with its details; it
// returns to the default when the plan is deleted.
```

Body: one overview comment saying the buffer is per test so a passing
test adds no output and a failing one shows every request the server
saw, in order. Every caller passes its `t`. Bare otherwise (rule 2).

#### `scripts/cicd/AGENTS.md`

Contract gains one bullet:

```
- qdbd's logs reach Buildkite only from inside a step command: Buildkite
  runs the repository's post-command and pre-exit hooks after the
  test-report plugin's post-command upload. `30.test.sh` installs
  `cicd_archive_qdbd_logs_on_exit` as its EXIT trap, which archives both
  daemons' logs through the submodule's `cleanup.sh` without stopping
  them, and `_build.yml` uploads `logs/qdbd-logs-*.tar.gz`. A later test
  script in the same step installs the same trap; its archive is a
  superset of the earlier one.
```

#### `.buildkite/AGENTS.md`

The "qdbd runs in CI" fact gains:

```
  Both daemons' logs, console output and error dumps are uploaded with
  the test report through the plugin's `job.artifacts` block
  (`steps/_build.yml`), the way quasardb's test step uploads its server
  logs, together with the rapid fail files of a failed property test.
  The archive is made inside the test command (`scripts/cicd/AGENTS.md`),
  because post-command and pre-exit hooks run plugins first and the
  repository last, so nothing a hook produces reaches the upload.
  Buildkite's `artifact_paths` is not used: the logs are not a release
  artifact.
```

#### `internal/AGENTS.md`, Tests

One sentence: the REST server's log in a test is shown only when the
test fails (`observeContext`), so a live-daemon failure can be read
from the server's side as well as the daemon's.

#### `scripts/tests/setup` (submodule)

Pinned to `147c4d2b3bb6c551e9c8a33acd615ad9c1eb0518` (PR 5's head) in
commit 2; re-pinned to PR 5's squash commit on master before the merge
stage, because the squash-merge deletes the branch that holds the head.

#### `docs/log.md`

Current state, after phase 1: Next item 1 leaves; item 2 is rewritten
from the evidence above (both Windows variants, three occurrences, the
nightly ruled out as the only change, the signature) and points at this
plan while it is alive. Later phases rewrite item 2 again as the
evidence narrows it.

### Phases 2 to 4: the investigation

The method, which every later commit follows:

- Each run of the Windows jobs is one sample. Every build of this unit
  is paired with a second build of the same head, so a pair yields two
  samples per variant. Buildkite refuses to retry a job that passed
  (`PUT jobs/<id>/retry` and the GraphQL `jobTypeCommandRetry` both
  answer "Only failed, timed out, canceled or expired jobs can be
  retried", 2026-10-07), so the re-run the owner asked for is a new
  build when the first run passes, and a retry only when it failed.
  More runs per head only when the tally proves two are not enough. The outcome, the duration of
  `TestRoundtrip` and, for a failure, the daemon log's last entries,
  the error dump and the failing draws are tabulated in this plan under
  a dated heading.
- A hypothesis is written down with the observation that would confirm
  it and the one that would refute it before the next sample is taken.
  A hypothesis the samples refute is recorded as refuted and not tried
  again.
- Phase 2 (the cause): the daemon log around the death answers whether
  qdbd crashed (a "signal caught" entry and an error dump), exited, or
  stopped answering while alive. The server's log answers which request
  was in flight. Only then is a cause named.
- Phase 3 (the payload): if the cause points at a request, the fail
  files replay the draws locally against a Windows daemon when one is
  available, or the request is narrowed on an agent through the Go test
  with `-rapid.failfile`. If the cause is unrelated to the payload (a
  resource limit, a port, the agent), the phase records that and stops.
- Phase 4 (the reproduction): a C or C++ program against the C API in
  the quasardb or qdb-api-c tree. Where the ticket for the qdbd R&D team
  goes is decided then, by the owner, and only if a reproduction exists.

Each phase's commits are added to this plan when the phase starts.

### 2026-10-07: hypotheses before the first sample (build 91)

Phase 1 is the head of build 91 (`f3868df`). Each Windows job is re-run
once after its first run finishes. Three hypotheses about the death,
each with the observation that confirms it and the one that refutes it:

| hypothesis                                                         | confirmed by                                                                                                                             | refuted by                                                                         |
| ------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| H1: qdbd crashes (a fatal signal or Windows structured exception)  | a "signal caught" panic entry with a backtrace at the end of the insecure daemon's log, and a `qdbd_<pid>_error_dump.log` in its archive | a log that ends without such an entry, and no error dump                           |
| H2: qdbd exits on its own account (a limit, a fatal error it logs) | a last log entry at error or panic level that names the reason, and no error dump                                                        | a log that ends mid-flight with no error or panic entry                            |
| H3: qdbd is alive but stops answering (a stall, a port, the agent) | log entries after the first refused `reader_init`, or the console files showing the process alive at stop time                           | no entry after the death and `Taskkill` in `stop-services.sh` reporting no process |

The server's log of the failed test names the request in flight when
the first 503 was answered; the daemon log around that time says which
of H1 to H3 holds. A Windows run that passes is a sample as well: its
`TestRoundtrip` duration goes into the tally.

### 2026-10-07: build 93, both Windows variants, the daemon vanishes silently

Build 93 is the second build of phase 1's head (`7f9e1d8`, documents
only above `f3868df`). Both Windows jobs failed; every other platform
passed. What the archives and the job logs say:

- The insecure daemon's log ends on a routine pipeline-flush entry, 22 s
  after start on core2 and 16 s on haswell, with no entry at error or
  panic level, no "signal caught", no backtrace and no error dump. The
  console stderr file is empty on both. H1 and H2 are refuted for these
  two samples: the daemon neither reported a crash nor logged a reason
  to exit.
- The secure daemon on the same agent, same binary, kept logging until
  the archive was taken. Only the insecure daemon disappears.
- The Go side never saw an in-flight request fail. In all five failed
  samples (build 90's three, build 93's two) the first error is a
  refused connect, or a refused `reader_init`, which opens a new
  connection; no test reported a reset or a closed connection. A refused
  connect is TCP's answer when nothing listens on the port, so the
  listener is gone. H3 as written ("alive but stops answering") is
  refuted for the listener; whether the process is alive is not known,
  because nothing records that yet.
- The death is not tied to `TestRoundtrip` or to the Arrow read: in
  build 93 it happened during the small query and read tests, before
  `TestRoundtrip` ran a case; build 90 placed it at 7 s, 382 s and
  532 s into `TestRoundtrip`. The last requests the daemon logged were
  `SELECT` evaluates on test tables; at `detailed` level it logs
  evaluates and sessions, not creates, pushes or bulk reads, so the
  request in flight, if any, is not in the daemon log.
- The job log cannot place the death on the test timeline either: `go
test ./...` buffers each package's output until the package finishes,
  so every `=== RUN` line of a package carries the package's end time.
- A connection accepted and left open with no logged request is normal:
  the passing run has several (one open 6 s before any entry, one 10 s).
- The daemon logs its memory only at start (a 38 MiB process, the agent
  at 15 % of 32 GiB), so the samples carry no memory trend. It runs as
  community edition: 8 concurrent sessions, 8 GiB in memory; at the end
  3 sessions (core2) and 2 (haswell) were open.

H4, written before the next sample: the insecure qdbd process is
terminated by a path that bypasses its signal and SEH handlers and its
logger, such as a fast-fail, a stack overflow, an allocation failure
that escapes the handler, or a kill from outside the process.

| hypothesis                       | confirmed by                                                                                                                                                                                                     | refuted by                                    |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| H4a: a crash outside the handler | an Application log event 1000 ("Application Error") for `qdbd.exe` at the death time, whose exception code names the kind (stack overflow, fast-fail, access violation, heap corruption), or a 1001 error report | no event for `qdbd.exe` around the death time |
| H4b: killed from outside         | no Application Error event, and `tasklist` at trap time shows no `qdbd.exe` for the insecure instance; a Defender operational event 1116 or 1117 names the file                                                  | an Application Error event (then H4a)         |
| H4c: alive but not listening     | `tasklist` at trap time still shows the insecure instance's pid                                                                                                                                                  | the pid is gone                               |

The telemetry for H4 is the next commit: on Windows the EXIT trap also
writes `logs/windows-events-<epoch>.txt` with the Application log's
events 1000, 1001 and 1026 and the Defender operational log's 1116 and
1117 from the last two hours, and `tasklist` filtered on `qdbd.exe`; the
test-report plugin uploads it next to the archives. PowerShell
`Get-WinEvent` is used rather than `wevtutil` because MSYS bash rewrites
arguments that start with `/` as paths.

### 2026-10-07: builds 94 and 95, the H4 telemetry works, all four Windows runs passed

Both builds of head `45f77d0` passed on both Windows variants. The
event capture of build 94's core2 run shows the shape a failing sample
will have: `tasklist` lists both `qdbd.exe` processes (the insecure one
at 225 MB after the full run, the secure one at 80 MB), and both
`Get-WinEvent` queries ran with the agent user's rights and answered
"No events were found", so an empty answer on a failing run is a
finding and not a permission problem. The tally after builds 91 to 95:
two of ten Windows runs failed, both in one build.

### 2026-10-07: builds 96 and 97, the first sample with the system's account

Build 97's windows-core2 run failed; the other three Windows runs of
the pair passed (build 97's linux-core2 job also failed, with exit
status -1 and its agent `lost` mid-run, which is the agent and not the
branch). What the first event capture of a failing run says:

- `tasklist` at trap time lists one `qdbd.exe`, pid 8512, which is the
  secure daemon by its own log. The insecure daemon, pid 4776, is gone.
  H4c (alive but not listening) is refuted.
- The Application log holds no event 1000, 1001 or 1026 in the two
  hours before, and the Defender operational log no 1116 or 1117. H4a
  is not confirmed, and not refuted either: event 1000 is written by
  the Windows Error Reporting service, and the agents' state of that
  service is unknown. The next capture records it (`WerSvc` status and
  the `Disabled` value of the Windows Error Reporting key).
- The insecure daemon's log ends on "accepting", the line after a
  "connection accepted" for the fourth connection of a burst: three
  connections opened and closed within a millisecond each (the
  fixture's port probes), then one kept. Build 93's two samples show the
  same burst one and two seconds before their end. The death follows a
  new client connection within two seconds in all three samples that
  have a daemon log.
- On the outside-kill branch of H4b: every Windows agent is one agent
  per host (`bk api agents`, distinct hostnames), so no other Buildkite
  job runs on the machine; quasardb's own cleanup
  (`scripts/cicd/service-cleanup.sh`, `scripts/tests/setup/utils.sh`)
  kills by image name, which would take the secure daemon too. Nothing
  in this repository kills a process.

H5, written before the next sample: the insecure daemon ends through a
process exit rather than a fault, during or right after a new session's
handshake. The exit code decides between the branches: a Windows status
such as access violation, stack overflow, fast-fail or heap corruption
says a fault that WER did not record (H4a); a small integer says
`exit()` from inside qdbd (H5); a code set by another process says a
kill (H4b).

| hypothesis            | confirmed by                                                                   | refuted by              |
| --------------------- | ------------------------------------------------------------------------------ | ----------------------- |
| H4a, fault            | exit code is an NTSTATUS (0xC0000005, 0xC00000FD, 0xC0000409, 0xC0000374)      | a small exit code, or 1 |
| H5, qdbd exits itself | exit code 0, 1 or another small integer, with no fault event                   | an NTSTATUS exit code   |
| H4b, killed           | exit code 1 or 0xC000013A (console close), with WER enabled and no fault event | an NTSTATUS fault code  |

The telemetry for H5 is the next commit: on Windows `30.test.sh` starts
a watcher before the tests, a background PowerShell that holds a handle
on every `qdbd.exe`, samples each one's working set, private bytes,
threads and handles every second, and on exit writes the exit code and
time, all into `logs/qdbd-watch-<epoch>.txt`; the event capture also
records the WER service state. The plugin uploads the watch file.

### 2026-10-07: builds 98 and 99, the watcher works, all four Windows runs passed

Both builds of head `e2b9fe9` passed on both Windows variants. What the
watch files and the event captures say:

- The watcher took its handles on both daemons before the first test and
  sampled each once a second until the trap killed it; the start line
  carries each pid's command line, so `-a 127.0.0.1:2836` names the
  insecure daemon directly. Over a passing run the insecure daemon's
  working set grows about five-fold and the secure one's by two thirds;
  threads and handles end within ten of where they started. No exit line,
  as expected of a passing run.
- Windows Error Reporting is active on the agents: the Application log
  of build 98 holds event 1001 reports for other programs (a quasardb
  integration test's `RADAR_PRE_LEAK_64` report on core2, a Chrome
  installer's crashpad report on haswell), written for the same agent
  user. `WerSvc` shows `Stopped`, start type `Manual`, which is its
  on-demand default. The registry query printed nothing. So build 97's
  empty Application log is a finding: qdbd did not fault through WER.
  H4a is refuted for build 97 unless the exit code says otherwise.
- The tally after builds 91 to 99: three of eighteen Windows runs
  failed.

H4a is now the least likely branch; H5 (an exit from inside qdbd) and
H4b (a kill from outside) stand, and the next failing run's watch file
decides between them by the exit code. Experiment A starts on this head,
as the owner ordered: the tests' traffic moves to the secure daemon and
the insecure one idles.

### 2026-10-07: build 107, the first death under experiment A is the secure daemon's

Builds 105 to 109 are five builds of head `b3a41ec`, where every
insecure test binds the secure daemon as its test user and the insecure
daemon idles. Build 107's windows-haswell run failed; what its archives
say:

- The watch file: the secure daemon (pid 12100, `-a 127.0.0.1:2838`),
  the one carrying all the traffic, exited at 09:07:11 UTC; the idle
  insecure daemon (pid 1248) was sampled alive to the end of the step.
  The death follows the traffic, not the instance or the port.
- The watcher wrote an empty exit code and exit time, and the job log
  carries its PowerShell error: a Process object from `Get-Process`
  opens its handle lazily, and after the exit that open fails, so
  `HasExited` is true with nothing behind it. The script now reads
  `Handle` at start, which opens and keeps the handle; the exit code of
  the next death will be in the file.
- The secure daemon's log ends at 09:07:10.202 on a burst of nine
  `SELECT` evaluates, with no entry at error or panic level and no
  error dump: the same silent ending as builds 93 and 97.
- The REST server's log places the death within milliseconds. Its last
  answered requests are two table creates at 09:07:10.220 (201); the
  round trip's next step is the bulk read of the first table while it
  is empty, which the test runs directly over the cluster, and that
  `reader_init` was refused. The daemon was gone between the create's
  answer and the reader's connect, or died on the reader's first
  request and the C API's reconnect found the port closed. Either way,
  the first bulk read of a table created milliseconds earlier is the
  operation in flight, and every earlier sample ended on "connection
  accepted" with nothing after, which is what a reader's fresh
  connection looks like from the daemon's side.
- Windows Error Reporting recorded nothing for `qdbd.exe` on an agent
  where it recorded other programs the same morning.
- The failing case: table `mzzkzcacrdbdocnj`, five columns of types
  `[4,3,2,2,5]` (blob, symbol, string, string, timestamp), read empty.
  The rows drawn (36, null share 82) were never pushed.

H6, written before the next sample: qdbd dies on the bulk reader's
initialisation (`qdb_bulk_reader`, `reader_init`) over a table created
moments earlier, before any row is pushed, over a new client session.

| hypothesis                                          | confirmed by                                                                                                                                      | refuted by                                                                            |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| H6: the empty bulk read of a fresh table kills qdbd | an in-place loop of create, empty bulk read, delete over the C API reproducing the death on an agent, with the watcher's exit code                | the loop running for an hour and more without a death while the full suite still dies |
| H5 and H4b, refined by the exit code                | the next death's exit code: an NTSTATUS says a fault (H4a after all), a small integer an exit from inside qdbd (H5), 1 or 0xC000013A a kill (H4b) |                                                                                       |

Experiment A's tally, builds 105 to 109: ten Windows runs, one death,
the secure daemon's, while the idle insecure daemon survived every run.
The conclusion the experiment was designed for: the death follows the
traffic. The experiment stays switched on in `30.test.sh` until the
investigation ends, so every further Windows run is a sample of the
same kind. The tally since build 91: four deaths in twenty-eight runs.

The in-place reproduction is the next step, on an agent paused out of
the Buildkite pool (owner, 2026-10-07; the API token lacks the
`write_agents` scope, so the agent's Windows service is stopped on the
machine instead, and started again when the loop is over): the Windows agents are Proxmox
VMs on the Hetzner hosts, reachable over the private network with the
WARP client connected, by SSH as the image's administrator or through
`qm guest exec` on the host; each keeps its last checkout of the
pipeline with the daemon dists and the Go toolchain.

### 2026-10-07: the in-place H6 loop, no death in an hour

On agent `default-windows-amd64-h-0`, taken out of the pool by stopping
its Buildkite service, a loop of draw a schema, create the table, bulk
read it empty, remove it (`internal/qdb/h6loop_test.go`, `TestH6Loop`,
not committed) ran against the insecure daemon with the watcher
attached, as the image's administrator user rather than the agent's
service user. Fifty minutes and about six thousand checks at two per
second: both daemons alive, no exit line. The suite's four deaths came
from about twenty-eight hundred round-trip cases over twenty-eight
runs, so the bare empty read of a fresh table does not kill the daemon
at the suite's rate. H6 as the whole story is refuted; the empty read
may still be the last request of a sequence that matters. The
traffic-carrying daemon's working set grew from about 50 MB to about
280 MB over the hour, and its private bytes from 100 MB to 345 MB,
which the next samples keep an eye on.

The next in-place loop is `TestRoundtrip` itself, the reproducer the
suite already is (`rt.test`, built from `internal/httpapi`), run
against the insecure daemon until a run fails, so that the watcher's
exit code decides H4a, H5 and H4b.

How the agent is driven, for the next session: SSH as the image's
administrator over the WARP private network, `bash.exe` from Git for
Windows, scripts copied to `C:\BuildkiteAgent\` (`rtrun.sh`,
`rtstatus.sh`), the test step's own helpers sourced from the checkout
under `C:\BuildkiteAgent\builds\<agent>\quasar-1\qdb-api-rest`.
A detached launch through `Start-Process` breaks the Go toolchain
(`compile.exe` exits 0xC0000142), so the loop runs in the foreground
of a long-lived SSH session instead.

### Samples

| build | job             | variant         | run | outcome | TestRoundtrip | daemon log's last entries                                                        | error dump                                                    | failing draws                                                                                    |
| ----- | --------------- | --------------- | --- | ------- | ------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| 91    | `01a11503-8a62` | windows-core2   | 1   | passed  | 557 s         | async pipeline flushes, no error entry                                           | none                                                          | none                                                                                             |
| 91    | `01a11503-8a65` | windows-haswell | 1   | passed  | 549 s         | async pipeline flushes, no error entry                                           | none                                                          | none                                                                                             |
| 93    | `01a11510-7353` | windows-core2   | 1   | failed  | not reached   | a connection accepted, then two seconds of pipeline flushes, then nothing        | none                                                          | `TestReadTableRange` and `TestReadAnswersRowsWritten`, both at `create`, breaker open            |
| 93    | `01a11510-7357` | windows-haswell | 1   | failed  | not reached   | a connection accepted, then one second of flushes, then nothing                  | none                                                          | `TestReadTableRange` at `create`, breaker open                                                   |
| 94    | `01a11521-1a25` | windows-core2   | 1   | passed  | 550 s         | not read                                                                         | none                                                          | none                                                                                             |
| 94    | `01a11521-1a27` | windows-haswell | 1   | passed  | 558 s         | not read                                                                         | none                                                          | none                                                                                             |
| 95    | `01a11521-6893` | windows-core2   | 1   | passed  | 554 s         | not read                                                                         | none                                                          | none                                                                                             |
| 95    | `01a11521-6896` | windows-haswell | 1   | passed  | 550 s         | not read                                                                         | none                                                          | none                                                                                             |
| 96    | `01a1152d-fc24` | windows-core2   | 1   | passed  | 559 s         | not read                                                                         | none                                                          | none                                                                                             |
| 96    | `01a1152d-fc27` | windows-haswell | 1   | passed  | 553 s         | not read                                                                         | none                                                          | none                                                                                             |
| 97    | `01a1152e-1803` | windows-core2   | 1   | failed  | not reached   | a connection accepted and "accepting", then nothing, 72 s after start            | none; no Windows fault event; the process is gone             | `TestReadTableRange`, `TestReadAnswersRowsWritten`, at `create`, breaker open                    |
| 97    | `01a1152e-1806` | windows-haswell | 1   | passed  | 553 s         | not read                                                                         | none                                                          | none                                                                                             |
| 98    | `01a11563-7e76` | windows-core2   | 1   | passed  | 558 s         | not read; watch file: no exit, both daemons sampled to the end                   | none; WER active, 1001 for another program                    | none                                                                                             |
| 98    | `01a11563-7e7a` | windows-haswell | 1   | passed  | 550 s         | not read; watch file: no exit                                                    | none; WER active, 1001 for another program                    | none                                                                                             |
| 99    | `01a11563-8d87` | windows-core2   | 1   | passed  | 555 s         | not read; watch file: no exit                                                    | none                                                          | none                                                                                             |
| 99    | `01a11563-8d8a` | windows-haswell | 1   | passed  | 549 s         | not read; watch file: no exit                                                    | none                                                          | none                                                                                             |
| 105   | `01a11599-2d6f` | windows-core2   | 1   | passed  | 554 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 105   | `01a11599-2d72` | windows-haswell | 1   | passed  | 550 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 106   | `01a11599-59d4` | windows-core2   | 1   | passed  | 555 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 106   | `01a11599-59d7` | windows-haswell | 1   | passed  | 549 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 107   | `01a11599-7c8f` | windows-core2   | 1   | passed  | 550 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 107   | `01a11599-7c93` | windows-haswell | 1   | failed  | 176 s         | secure daemon (the traffic): nine SELECT evaluates, then nothing; insecure alive | none; no Windows fault event; exit code lost (watcher defect) | `TestRoundtrip`, the empty bulk read right after the create of `mzzkzcacrdbdocnj`, `[4,3,2,2,5]` |
| 108   | `01a11599-a818` | windows-core2   | 1   | passed  | 537 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 108   | `01a11599-a81a` | windows-haswell | 1   | passed  | 552 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 109   | `01a1159e-12bc` | windows-core2   | 1   | passed  | 550 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |
| 109   | `01a1159e-12be` | windows-haswell | 1   | passed  | 559 s         | not read; both daemons alive                                                     | none                                                          | none                                                                                             |

What the first archives show (build 91, both Windows variants): each
archive carries `insecure/log/0-0-0-1/qdbd.json`, the binary `Q___LOG`
and the console files. The daemon applied the submodule's settings:
its configuration dump at startup says `logger/flush_interval = 100`,
`log_level = detailed` and `json_file_output = True`, so the log holds
every request's `debug` entries. The console `.out.txt` is the same
stream as `qdbd.json`; at archive time it held two entries more, both
emitted in the same second as the tar, which is the interval's lag, so
the console file is the one to read for the last entries. The only warnings are the startup ones (security disabled, the
eviction threshold below the baseline), identical on both variants.

## Rationale

| decision                                                                          | why                                                                                                                                                                                                 | rejected, and why                                                                                                                                | gained                                                            | given up                                                                                                                              | settled by                                                                      |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| The unit is the investigation; the log upload is its phase 1                      | the owner's annotation on the first plan: a free-form troubleshooting task with the three goals above                                                                                               | a unit per phase: the scope is unknown, so the cut cannot be made in advance                                                                     | one plan carries the evidence end to end                          | the fixed commit list                                                                                                                 | owner, 2026-10-07 (plannotator annotation)                                      |
| Evidence before conclusions; every claim carries its observation                  | the owner asked for strictly evidence-driven debugging                                                                                                                                              | reasoning from the payload shapes or the nightly: the three payloads share nothing and the nightly differs by a test-only commit                 | no wasted phase on a refuted guess                                | speed on the first sample                                                                                                             | owner, 2026-10-07                                                               |
| Each Windows job is re-run once on every build, no more                           | the failure is intermittent (build 90 failed three of four Windows runs) and the owner suspects a rate near one in two, so one re-run per build should show it; the owner wants few changes at once | several re-runs per build: more agent time and more moving parts before the first sample says what is needed                                     | two samples per variant per build                                 | a slower tally if the rate is lower than suspected                                                                                    | owner, 2026-10-07 (plannotator annotation)                                      |
| Archive inside the test command through an EXIT trap                              | Buildkite runs the repository's post-command and pre-exit hooks after the plugin's upload                                                                                                           | a repository hook: its archive lands after the upload (build 90 job log)                                                                         | the archive exists when the plugin uploads                        | a trap in every test script of the step                                                                                               | Buildkite hooks documentation; owner, 2026-10-07                                |
| Upload through `job.artifacts`, not `artifact_paths`                              | the logs belong with the test report; quasardb's test step does the same                                                                                                                            | `artifact_paths`: a release artifact for something that is not one                                                                               | one place to look for a job's test evidence                       | nothing                                                                                                                               | owner, 2026-10-07                                                               |
| qdbd keeps running; the trap archives live logs                                   | stopping is a SIGKILL that flushes nothing; qdbd flushes on a fatal signal itself; `40.test-e2e.sh` is planned after the Go tests against the same daemons                                          | stop in the trap: no flush gained, the later e2e step loses its daemons                                                                          | composes with later test scripts                                  | the last 100 ms of log lines of a daemon that is alive                                                                                | `utils.sh` `kill_instances`; `sig_handler.cpp:626`; `docs/e2e.md`, In Buildkite |
| The flush interval and the console files are the submodule's                      | every API and tool pins the same submodule, so one change aligns them all                                                                                                                           | a second glob and a flag in this repo: project-local                                                                                             | one archive shape for every consumer                              | a dependency on PR 5                                                                                                                  | owner, 2026-10-07; qdb-test-setup PR 5                                          |
| Reuse `cleanup.sh::archive`                                                       | one home for how the daemon logs are archived                                                                                                                                                       | a tar in `00.common.sh`: a second shape to keep in step                                                                                          | zero archive logic here                                           | sourcing `config.sh` in a subshell                                                                                                    | proposal                                                                        |
| The rapid fail files are uploaded                                                 | they are the exact draws of the failing case, written even on the flaky verdict, and gitignored so nothing else keeps them                                                                          | reading the draws off the job log: present, but not replayable                                                                                   | `-rapid.failfile` replay on an agent or locally                   | nothing                                                                                                                               | `engine.go:290-297`; proposal                                                   |
| The server's test log is shown on failure, at debug level, as a temporary measure | the server is the second witness: it says which request was in flight when the daemon vanished; today its log is written to a buffer nobody reads; the owner accepts debug level for the samples    | logging into the test output always: noise on every passing test; a file: one more artifact glob; info level: may not name the request in flight | every request the server saw, with its details, for a failed test | one `testing.TB` argument per caller; the level returns to the default when this plan is deleted, or a Handoff item says why it stays | `readiness_test.go:36-39`; owner, 2026-10-07 (plannotator annotation)           |
| Pin PR 5's head now, re-pin before the merge stage                                | the owner wants samples now; the squash deletes the branch                                                                                                                                          | wait for the merge: samples wait with it                                                                                                         | the first samples today                                           | one more commit and build                                                                                                             | owner, 2026-10-07                                                               |
| The pre-exit hook's duplicate archive stays                                       | removing it means editing the submodule                                                                                                                                                             | a local `stop-services.sh`                                                                                                                       | no submodule change                                               | two unused tarballs per job                                                                                                           | root `AGENTS.md`, Sub-folders                                                   |

## Knowledge

- Commit 2 (submodule bump): no prose.
- Commit 3 (`00.common.sh`): the function's comment block and overview
  carry the hook-order reason, the live-archive reason (`kill_instances`,
  the flush interval, the signal handler's flush) and the subshell
  reason. `scripts/cicd/AGENTS.md` gains the Contract bullet.
- Commit 4 (`30.test.sh`): the header sentence and the trap comment.
- Commit 5 (`_build.yml`, `pre-exit`, `.buildkite/AGENTS.md`): the
  quasardb precedent, the `artifact_paths` rejection, the hook-order
  fact, the fail files; the hook comment says its archive is unused.
- Commit 6 (`observeContext`): the doc comment and overview carry the
  three-witness reason and say the debug level is temporary;
  `internal/AGENTS.md`, Tests, gains the sentence. The reversal of the
  level is this plan's: it happens, or is handed off, when the plan is
  deleted.
- Commit 7 (`docs/log.md`): Current state only; the evidence table above
  is the plan's and moves to the ticket and the log entry when the unit
  lands.
- The investigation's findings (phases 2 to 4) land in this plan first,
  dated; what survives goes to the ticket if one follows, to
  `internal/AGENTS.md` if a rule for the tests follows, and to the log
  entry that deletes the plan.

## How the knowledge lands

1. `/doc-discipline read` before the first code commit (done 2026-10-07).
2. Every commit's comments and `AGENTS.md` rows come from Design and
   Knowledge, in the same commit as the code.
3. A why that arises while building and is not in this plan is written
   where it is decided, with its evidence, or asked of the owner through
   the question tool before the commit.
4. After commit 7, `/doc-discipline all scripts/cicd .buildkite internal/httpapi/readiness_test.go`,
   one small commit per finding.
5. Before each build-stage message,
   `/doc-discipline check scripts/cicd .buildkite internal docs/ci-qdbd-logs-plan.md`;
   every "Unlanded from the plan" finding is fixed with a small commit
   or reported with the reason it was dropped.
6. After every `.buildkite` change, `python3 .buildkite/pipeline.py check`
   from a venv with `.buildkite/requirements.txt` and `BUILDKITE_BRANCH`
   set.
7. Every sample and every hypothesis goes into this plan under a dated
   heading before the next sample is taken.

## Commits

Phase 1:

1. `docs(plan): ci-qdbd-logs-plan.md, the build step uploads qdbd's logs with the test report` (landed, `6dcabcb`); revised by `docs(plan): ci-qdbd-logs-plan.md, the unit is the qdbd death investigation and the upload is its telemetry`.
2. `build(deps): bump qdb-test-setup to the head of PR 5, flush interval and console files`
3. `ci(cicd): cicd_archive_qdbd_logs_on_exit archives both daemons' logs through the submodule's cleanup.sh`
4. `ci(cicd): 30.test.sh archives the qdbd logs at exit, whatever the tests' outcome`
5. `ci(buildkite): the build step uploads the qdbd log archives and the rapid fail files with the test report`
6. `test(httpapi): the REST server's log is shown at debug level when a test fails`
7. `docs(log): CI uploads qdbd's logs; the windows-core2 death is both Windows variants, three of four runs`
8. `/doc-discipline all` on the touched paths, one small commit per finding; then `/doc-discipline check` with this plan.
9. Verify, first samples: push `sc-19567/rr-ci-qdbd-logs`, build its head (API-created, branch-filter bypass, full SHA; `.buildkite/AGENTS.md`), wait. Then re-run each Windows job once through the API. Tabulate every run in this plan. Green or red, the archives of every Windows run are downloaded and read; a red Windows run is the sample the unit exists for and is not "fixed".

Phase 2, from the build 93 samples:

10. `docs(plan): ci-qdbd-logs-plan.md, build 93 died silently on both Windows variants; H4 and the Windows event telemetry`
11. `build(deps): bump qdb-test-setup to the squash commit of PR 5` (PR 5 merged 2026-10-07 as `877cda6`, the same tree as its head)
12. `ci(cicd): on Windows the EXIT trap records the Application and Defender events and whether qdbd is alive`
13. `ci(buildkite): the build step uploads the Windows event capture with the test report`
14. Verify: push, build the head, then a second build of the same head; tabulate both Windows runs of each; read every failed run's event capture against H4.
15. `docs(plan): ci-qdbd-logs-plan.md, build 97 core2 died with no Windows fault event and the process gone; H5 and the exit-code watcher`
16. `ci(cicd): on Windows 30.test.sh watches every qdbd.exe for its exit code and samples its memory and threads`
17. `ci(buildkite): the build step uploads the qdbd watch file with the test report`
18. Verify: push, build the head twice; read every failed run's watch file against H4 and H5.
19. `ci(cicd): the Windows event capture records the Windows Error Reporting state`
20. `docs(plan): ci-qdbd-logs-plan.md, builds 98 and 99 passed, the watcher works, WER is active on the agents`

Experiment A, the secure daemon:

21. `test(qdbtest): QDBTEST_TRAFFIC_TO_SECURE binds every insecure test to the secure daemon`
22. `ci(cicd): on Windows 30.test.sh moves the tests' traffic to the secure daemon`
23. Verify: push, build the head until ten Windows runs are tabulated; a death of the secure daemon follows the traffic, a death of the idle insecure one follows the instance.
24. `ci(cicd): the watcher opens a handle on each qdbd.exe so its exit code survives the exit`
25. `docs(plan): ci-qdbd-logs-plan.md, build 107 killed the secure daemon under experiment A; H6, the empty bulk read of a fresh table`

Phases 3 and 4: commits added to this plan as each phase starts, under
the method above.

## Status for the next session (2026-10-07, written before a context reset)

The branch is `sc-19567/rr-ci-qdbd-logs`, pushed; the head carries the
telemetry of phase 1 and the Windows event capture, both verified in
builds 91 to 97. The submodule is pinned to PR 5's squash commit
(`877cda6`). Nothing is merged. The owner's decisions in the session:

- The second sample per head is a second build of the same commit
  (Buildkite cannot retry a passed job).
- The investigation continues from the samples, strictly by evidence;
  the owner asked, in this order: first the exit-code watcher (commits
  16 and 17 above), then an experiment that points the tests at the
  secure daemon, then a bisection by test.
- Experiment A, the secure daemon: a switch in `internal/qdbtest`
  (an environment variable `30.test.sh` sets on Windows only, the
  owner's choice of 2026-10-07, so the other platforms stay a control)
  makes every insecure binding use the secure cluster, with the
  fixture's key files, so all traffic moves to port 2838 and the
  insecure daemon idles. Secure dies: the death follows the traffic.
  Insecure dies while idle: it follows the instance or the port. Ten
  runs are the minimum at a rate near one in five.
- Experiment B, the bisection: disable every test but one and re-enable
  tests selectively, one build pair per step, until the death is
  observed again; the set that first reproduces it is the payload of
  phase 3. The owner asked for this after experiment A.

How the samples are read, so the next session does not rediscover it:

- The plugin uploads to Cloudflare R2, bucket `qdb-cicd-artifacts`,
  endpoint and credentials in the cicd account's SSM under
  `/services/buildkite/config/artifacts/object-store/` (`endpoint-url`,
  `r2/access-key-id`) and
  `/services/buildkite/credentials/artifacts/r2/secret-access-key`;
  `aws sso login --sso-session qdb` first. `aws s3 cp --endpoint-url
<endpoint> --recursive` with those as `AWS_ACCESS_KEY_ID` and
  `AWS_SECRET_ACCESS_KEY`, region `auto`. The public domain
  `cicd-artifacts.quasar.ai` sits behind Cloudflare Access and answers
  `curl` with a login page.
- The key is
  `qdb-api-rest/refs/heads/<branch>/reports/builds/<build uuid>/variants/<variant>/jobs/<job uuid>/artifacts/<qdbd-logs|rapid-fail-files|windows-events>/...`;
  the build uuid is `id` in the build's JSON, the job uuid is the job's
  `id`.
- The job log (`bk api pipelines/qdb-api-rest/builds/<n>/jobs/<id>/log`,
  field `content`) carries the test output with every line of a package
  stamped at the package's end, the `cicd_record_windows_events` lines,
  and the plugin's upload lines.
- Building a head: `bk api --method POST pipelines/qdb-api-rest/builds`
  with the full SHA, the branch and `ignore_pipeline_branch_filters`;
  one request creates one build, so parse the answer from a file, not a
  pipe. Then `PUT jobs/<id>/reprioritize` with priority 100 for every
  `scheduled` job. The Windows agents are shared with quasardb-build's
  nightly tests and a job can wait twenty minutes for one.

## Open questions and recommendations

None. The owner settled the three the first revision carried: one
re-run of each Windows job per build, the ticket's destination is a
question for when a reproduction exists, and the server's test log runs
at debug level as a temporary measure.
