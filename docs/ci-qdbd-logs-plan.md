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

### 2026-10-07: the context decides, the round-trip loop in two contexts on one agent

On the same agent `default-windows-amd64-h-0`, the same `rt.test`
binary (`TestRoundtrip` from `internal/httpapi`) against the same
daemon dists, in two contexts:

- From an SSH session as the image's administrator user (session 1):
  eleven consecutive runs passed, about two hours, both daemons alive
  throughout, the traffic-carrying daemon's working set settling near
  150 MB.
- As a Windows service (a WinSW copy next to the agent's own, running
  as the `buildkite` service account in session 0, the way the agent
  runs its jobs): the insecure daemon died 73 seconds into the first
  run, on the same signature, a burst of `SELECT` evaluates then
  nothing, no error entry, no error dump, nothing on stderr.

At the suite's rate of about one death in seven runs, eleven passes
have about one chance in five of being luck, and a death on the first
service run about one in seven; together they say the service context
is where the daemon dies and the user session is where it does not.
The owner recalled that the agent runs as a service and that what it
spawns runs restricted; the loop bears that out. What the service
context changes for qdbd: session 0 with the non-interactive window
station and its 768 KB desktop heap (`SharedSection=1024,20480,768`
on the image), the `buildkite` account's privileges, and the job object
the agent puts a job's processes in (absent from the WinSW copy, which
still died, so the job object is not the cause). The watcher's exit
code, from the service loop restarted with the handle fix, is the next
sample.

### 2026-10-07: the exit code is 0xC0000005, an access violation

The service loop, restarted with the watcher's handle fix, passed
three runs and lost the insecure daemon in its fourth, at 13:03:28 UTC,
37 minutes after the daemons started. The watcher's exit line:

    exit pid=7448 code=-1073741819 hex=0xC0000005

H4a is confirmed: qdbd dies of an access violation that neither its
own structured-exception handler nor Windows Error Reporting records.
H5 (an exit from inside qdbd) and H4b (a kill from outside) are
refuted. The daemon's log ends the same way as every sample, a burst
of `SELECT` evaluates, then nothing: the fault happens before the
handler's "signal caught" entry can be written, or on a path the
handler does not cover. What is needed next is the fault itself, a
memory dump taken at the exception by a tool that does not depend on
WER (Sysinternals procdump attached to both daemons for the loop), so
the faulting address and the thread's stack can go to the qdbd R&D
team with the build id of the nightly.

### 2026-10-07: the crash dump, thread "a-pipe 00"

The service loop with Sysinternals procdump attached to both daemons
(`-e -ma`, a full dump at an unhandled exception) lost the insecure
daemon three minutes into its first run, at 13:11:22 UTC, and procdump
wrote `C:\BuildkiteAgent\dumps\qdbd_13660.dmp` (253 MB) on agent
`default-windows-amd64-h-0`. What `cdb` (Windows debugging tools,
installed on the agent for this) reads from it, without symbols:

- The daemon is `qdbd.exe` 3.15.0.dev0, build `91476e3abe`, image
  timestamp 2026-10-07 07:00:45, the nightly quasardb-build 2762; its
  dist carries no PDB, so the frames below are offsets from the image
  base, for the qdbd team to map on their build.
- The exception: `c0000005`, a read of `0x254e5d97000`, a page-aligned
  address, which is what a read past the end of a heap allocation
  into the next, unmapped page looks like.
- The faulting thread is number 18, named `a-pipe 00`, the async
  pipeline's thread. Its stack, innermost first:
  `KERNELBASE!RaiseException+0x6c`, `qdbd+0x1926ec6`,
  `ntdll!RcConsolidateFrames+0x6`, `qdbd+0x3fe6e4`, `qdbd+0x452c54`,
  `qdbd+0x597c36`, `qdbd+0x1910b93` (the thread's entry),
  `kernel32!BaseThreadInitThunk`. `RcConsolidateFrames` is the C++
  runtime executing a catch funclet, and the `RaiseException` above it
  carries the access violation's own code, so the fault was caught and
  re-raised with its original status, and that re-raise is what the
  process died of: no "signal caught" entry, no error dump, no Windows
  Error Reporting event.
- The daemon's log ends at 13:11:21.893 on "flushing async pipeline
  pipe_0 to disk: 1 entries - 19 rows" with no "rows written" line
  after it, where every earlier flush has one within two milliseconds.
  The fault is in the async pipeline's flush to disk, which also says
  why the create-and-empty-read loop never died: it pushed no row.
  The round trip draws its push mode, the async one included.

The dump is the artifact for the qdbd R&D team, with the build id and
the frames above; a copy is kept off the agent.

### 2026-10-08: a debug daemon with symbols, harvested from the siege agents

Where qdbd's symbols are, verified against the quasardb tree and the
build agents:

- quasardb compiles with `/Z7` (`cmake_modules/compiler_flags.cmake`,
  `CMAKE_MSVC_DEBUG_INFORMATION_FORMAT Embedded`): the debug records are
  embedded in the object files, and the linker runs with `/DEBUG:FULL`,
  which merges them into one PDB next to the executable. The executable
  carries only an RSDS record naming that PDB (`Q:\bin64\Debug\qdbdd.pdb`
  in the debug `qdbdd.exe` of quasardb-build 2762; its sections are code
  and data, no debug section). The PDB is never packaged: no artifact of
  quasardb-build's Windows variants carries one, and the release builds
  are linked without debug information, so a release `qdbd.exe` has no
  PDB anywhere. That is why the dump `qdbd_13660.dmp` cannot be
  symbolized.
- The debug PDB exists on the siege build agent's checkout from the
  moment the linker finishes until the next job's checkout cleans the
  tree, which is minutes to hours. The Windows siege agents
  (`siege-windows-amd64-h-0` to `h-3`) are Proxmox VMs on the Hetzner
  hosts 3 and 4, reachable like the default agents. SMB between the VMs
  is closed; files move through a `tar -I zstd` archive and scp via the
  operator's machine (a debug `qdbdd.exe` and its PDB compress eightfold).
- The pair in use: `qdbdd.exe` and `qdbdd.pdb` of quasardb-build 2785
  (`bf816772bd`, branch `mk-timeout-protocol-compatibility`, which is
  master `22f54da872` plus one commit that removes tests and five lines
  of `qdb/timeseries/types.hpp`), taken from `siege-windows-amd64-h-1`'s
  `bin64/Debug` right after the link, with `qdb_user_addd.exe` and
  `qdb_cluster_keygend.exe`. `symchk` confirms the PDB matches the
  executable's signature. A copy of both archives (core2 from `h-1`,
  haswell from `h-3`) is on the operator's machine.
- The debug daemon asserts `cfg.is_sane()` at startup
  (`apps/qdbd/config_validator.cpp:422`) on the submodule's test
  configuration: `cluster.statistics_refresh_interval` is 500 ms there
  and the minimum is one second (`qdb/config/cluster.hpp:73`). The
  release build only warns. The loop runs the daemons on a copy of
  `default.qdbd.cfg` with the interval at one second
  (`CONFIG_INSECURE` and `CONFIG_SECURE` point at it); nothing else in
  the configuration differs from CI.

The two loops started on 2026-10-07, both in session 0 with procdump
attached to both daemons and the watcher running, each in a workspace
outside the agent's build directory (`C:\BuildkiteAgent\rr\qdb-api-rest`,
the branch cloned from a bundle, the dists from the artifact store, and
`rt.test` built there):

- `default-windows-amd64-h-0`, service `qdb-rtsvc` as the agent
  account, the debug `qdbdd.exe` with its PDB in `qdb/bin`, so the next
  death yields a dump that `cdb` can read with symbols. The watcher and
  the event capture were taught the debug name (`qdbdd.exe`), which is
  what the submodule's `binaries.sh` falls back to when `qdbd.exe` is
  absent.
- `default-windows-amd64-h-1`, service `qdb-rtsvcsys` as LocalSystem,
  the release `qdbd.exe` of quasardb-build 2782 (master `22f54da872`).
  This is the one-variable experiment for the service-context question:
  session 0 as before, a different account with different privileges.
  A death here says the session, not the agent account, is the
  discriminator; a long run of passes says the account is.

- `default-windows-amd64-h-2`, service `qdb-rtsvcsys` as LocalSystem,
  the same debug `qdbdd.exe` and PDB as `h-0`: a second symbol source,
  and a second sample of session 0 without the agent account.

### 2026-10-08: the release daemon dies as LocalSystem, so the session is the discriminator, not the account

The LocalSystem loop on `default-windows-amd64-h-1` lost the insecure
release daemon (quasardb-build 2782, `22f54da872`) in its eighth run,
seventy minutes after the daemons started, on the same signature as
every earlier death: exit code `0xC0000005`, the daemon's log ending on
"flushing async pipeline pipe_0 to disk: 1 entries - 1 rows" with no
"rows written" after it, no error entry, no error dump, nothing on
stderr, and the test's first error a `create` refused by the open
circuit breaker. procdump wrote `C:\BuildkiteAgent\dumps\qdbd_4700.dmp`
on `h-1`; a copy is on the operator's machine as
`qdbd_4700-h1-localsystem-release-22f54da872.dmp`. `cdb` reads the same
shape as the earlier dump: thread "a-pipe 00", `KERNELBASE!RaiseException`
above `ntdll!RcConsolidateFrames`, an access violation reading the
page-aligned address `0x2272fcd3000`, then three frames in `qdbd.exe`
(`+0x3d4e99`, `+0x429144`, `+0x56e276` on this build) and the thread
entry. So the agent account, its privileges and the Buildkite agent's
environment are not what makes the daemon die: LocalSystem in session 0
dies the same way. What session 0 and the user session do not share
remains the candidate list: the non-interactive window station and its
desktop heap, the absence of a logon session with a user profile, and
whatever the process inherits from the service control manager
(priority class, the console, the job object of the service).

The catch funclet's re-raise is the only exception the second-chance
dumps carry, and it hides the faulting frame. From this heading on all
three loops run procdump with `-e 1 -f C0000005 -n 3`, which writes a
full dump at the first-chance access violation, before qdbd's handler
runs, and keeps the unhandled re-raise's dump as well. The debug loops
on `h-0` and `h-2` were restarted for that and lost their first run,
which the debug daemon had not finished in eighty minutes (the release
daemon finishes one in about ten).

### 2026-10-08: the fault itself, from a first-chance dump: a Stream VByte decode reads past the end of its input

The LocalSystem loop on `h-1`, restarted with first-chance capture,
lost the insecure release daemon again in its second run, eleven
minutes after the start, and procdump wrote the first-chance dump
`qdbd_2784.dmp` (a copy is on the operator's machine, suffixed
`firstchance`). Without symbols, `cdb` still reads the instruction and
its operands:

- The faulting instruction is `movdqu xmm0, xmmword ptr [rdx]`, an
  unaligned sixteen-byte load, at `rdx = 0x2c94e39aff1`, fifteen bytes
  before the page boundary at `0x2c94e39b000`. `!address` says the
  bytes before the boundary are the last of a committed 64 KB run of a
  segment heap and the page after it is reserved, not committed. The
  load spans the boundary; the first byte on the reserved page is the
  access violation.
- The surrounding code is a shuffle-table decode loop: `movdqu` from
  the input, `pshufb` with a mask from a table in the image, a store,
  then the next `movdqu` four bytes further on (`r8`, `rdx`, `rax` hold
  `...afed`, `...aff1`, `...aff5`). Sixteen-byte loads advancing by the
  encoded length of a group of four values, four bytes when every value
  fits in one byte, is Stream VByte's SSE decoder
  (`thirdparty/streamvbyte-2.0.0/src/streamvbyte_x64_decode.c:13-32`).
- The library documents the over-read: "Our decoding functions may read
  (but not use) STREAMVBYTE_PADDING extra bytes beyond the compressed
  data: the user needs to ensure that this region is allocated"
  (`thirdparty/streamvbyte-2.0.0/include/streamvbyte.h:52-69`,
  `STREAMVBYTE_PADDING` is 16). qdbd calls the decoder from
  `qdb/compression/streamvbyte.cpp:39`, `delta_rle_streamvbyte.cpp:190-203`,
  `rle.cpp:197` and `delta4c.cpp:596,639`; which of them decodes during
  the async pipeline's flush to disk, and whether its input buffer
  carries the padding, is what the debug daemon's symbols will say.
  The exception is raised on thread "a-pipe 00" with the same three
  frames below the decoder as every earlier dump, so this is the one
  bug, not a second one.
- The two later dumps of the same death are the re-raise from the
  catch funclet (`KERNELBASE!RaiseException`), first-chance and then
  unhandled; the daemon's own handler never sees the original fault
  because the catch funclet consumes it.

The source confirms the missing padding. The generic decoder for the
Stream VByte codec builds its input as a view of exactly the stored
compressed size into the incoming serialized buffer, with the comment
"zero copy decompression, we will decompress directly from the
incoming buffer" (`qdb/compression/streamvbyte.hpp:86-116`), and
`streamvbyte_decompressor::read` hands that view to
`streamvbyte_decode` (`qdb/compression/streamvbyte.cpp:30-42`). The
sibling codecs decode from `src` into the serialized input the same
way, with no padding after the compressed bytes
(`delta_rle_streamvbyte.cpp:175-203`, `rle.cpp:185-200`,
`delta4c.cpp:585-642`). When the compressed bytes are the last field
of a value whose buffer ends at a heap commit boundary, the decoder's
last sixteen-byte load crosses it.

So the daemon dies of a read of up to fifteen bytes past the end of a
heap buffer that holds Stream VByte data. It is latent everywhere and
faults only when the buffer ends within fifteen bytes of the end of a
committed heap run, which is what the context changes.

What the context does not change, measured on `h-1`: the daemon's
process heap is the Segment Heap (signature `ddeeddee`, `!heap -s`)
both when started from the SSH logon and as a service; and the SSH
logon on these agents is itself session 0 (`(Get-Process -Id $PID).SessionId`
prints 0 for the SSH shell), so the "session 1" the earlier heading
assigned to the SSH runs was never measured, and the eleven passes
there were session-0 runs as Administrator with a network logon. The
remaining differences between the contexts that die and the one that
did not are the logon type and token, the parent process (WinSW with
its job object against sshd) and the environment, none of which
should move a heap commit boundary; the eleven passes are also within
the odds of luck at the observed death rate. The deciding experiment
runs now on `h-1`: full page heap for `qdbd.exe` (`gflags /p /enable
qdbd.exe /full`), which puts every allocation at the end of a page with
an unmapped page after it, then the round trip from the SSH logon. A
death at the same instruction in the first run makes the layout the
whole explanation and gives the qdbd team a deterministic reproduction
in any context.

### 2026-10-08: page heap is too slow to decide, so the SSH-logon loop runs long instead

Full page heap on the release `qdbd.exe` (`gflags /p /enable qdbd.exe
/full`, from the SSH logon on `h-1`) made one round-trip case take
sixty-nine minutes and did not fault in it. One case is no sample, so
the experiment says nothing either way; it also says the decoded bytes
are probably not an exact-size allocation of their own, or page heap
would have put an unmapped page right after them. Page heap is
disabled again on `h-1` (`gflags /p` lists no application).

The question the earlier eleven passes left open is tested directly
now: the same release loop, from the SSH logon on `h-1`, with the
watcher and first-chance procdump, up to fourteen runs. A death there
removes the context from the explanation; fourteen passes against a
death rate of one in two to eight runs as a service would make the
context real and leave the logon type and token, the parent process
and the environment as the candidates.

### 2026-10-08: the debug daemon asserts before it can crash: an invalid int64 column index

The debug daemon on `h-2` (LocalSystem) ended its third run seventy
minutes in, not with an access violation but with
`Assertion failed: idx.valid(), file ..\..\qdb/kernel/containers/ts/indexer.hpp, line 239`
on its stderr, then an orderly shutdown and exit code -3, which is
`emergency_shutdown` on `SIGABRT` (`apps/qdbd/runner.cpp:304,338`).
Line 239 is `compute_index` for an int64 column:
`compute_index_arithmetic<int64_column_index>` followed by
`BOOST_ASSERT(idx.valid())`. An int64 index is invalid when the
non-null count exceeds the count, when the first, last, minimum or
maximum value is the type's "none" sentinel while the column has
non-null values, or when the running sum is that sentinel
(`qdb/metadata/column_index.hpp:91-100,152-161,217-229`). The release
build skips the assertion and continues with the invalid index. No
dump exists for this death: it is not an exception, and procdump was
filtered to access violations.

Whether the invalid index and the decoder's over-read are one bug or
two is open. Both happen on the async pipeline's flush thread; a
decoder that reads past the end of a too-short input would decode
garbage and could produce exactly such an index, and a test that draws
the int64 sentinel value as data would too. The next debug death
decides it, with symbols: the debug loops on `h-0` and `h-2` now run
with `cdb` attached to both daemons instead of procdump, which writes
a full dump at the first-chance access violation and hands the
exception on, and a full dump at `qdbdd!_wassert` before `abort()`
runs (the breakpoint resolves; the CRT is linked statically).

The debug daemon at the `detailed` log level wrote sixty-three
gigabytes of `qdbd.json` in four hours and filled `h-0`'s disk
(uploads to it failed, the daemon's log writes with it); the loops now
run the daemons at `info`, and the dumps carry what the log was read
for.

On `h-1` the release loop from the SSH logon passed all fourteen runs
(two hours twenty minutes of traffic, both daemons alive at the end),
twenty-five consecutive passes with the earlier eleven, against four
deaths in about fifteen service runs of the same binaries on the same
agents. The service context is a real factor, through the heap layout;
what the service start changes in the layout is not established.

Since 2026-10-08 00:27 UTC `h-1` runs the same debug loop as
LocalSystem (its release loop finished), and since 00:37 UTC
`default-windows-amd64-h-3` (proxmox-2, taken out of the pool at the
owner's request) runs it as the agent account, with the service
definition copied from `h-0`. Four debug loops, two per account, each
yielding a round-trip run about every eighty-five minutes.

All four agents' Buildkite services are stopped for the duration; the
jobs they were running retry elsewhere (quasardb's steps retry on agent
loss, `.buildkite/steps/_test.yml`).

### 2026-10-08: handoff to the qdbd R&D team, as the evidence stands

What to report, in the order a reader needs it:

1. **Symptom.** On the Windows CI agents `qdbd.exe` 3.15.0.dev0 dies
   with exit code `0xC0000005` under the qdb-api-rest round-trip test,
   leaving no log entry, no error dump and no Windows Error Reporting
   event, because the access violation is caught by a catch funclet
   and re-raised (`RaiseException` above `RcConsolidateFrames`), and
   that re-raise is what ends the process. Every death is on thread
   "a-pipe 00" during "flushing async pipeline pipe_0 to disk".
2. **Fault.** Symbolized first-chance dumps `av_qdbd_7260.dmp`,
   `av_qdbd_7740.dmp`, `av_qdbd_15336.dmp` (quasardb-build 2796 of
   branch `sc-19567/rr-ci-qdbd-logs`, release with PDB, master
   `22f54da872` plus a CI-only commit): `svb_decode_sse41`
   (`thirdparty/streamvbyte-2.0.0/src/streamvbyte_x64_decode.c:105`)
   loads sixteen bytes at fifteen bytes before a page boundary with the
   next page not committed. The header says the decoder may read
   `STREAMVBYTE_PADDING` (16) bytes beyond the input and the caller
   must allocate them (`include/streamvbyte.h:52-69`). The caller is
   `qdb::compression::delta4c_read` (`qdb/compression/delta4c.cpp:596,639`)
   under `v2::delta4c_null_filter_decompressor::read`, decoding the
   stored values of a timestamp column out of the serialized value
   that `persistence::key_value::find_data_unmarshal` fetched from the
   store, while `ts_table_inserter::load_bucket` loads the bucket the
   async flush (`ts_async::writer::commit`) merges into. The full
   stack is in the plan's 04:34 heading and in the analyses next to
   the dumps.
3. **A second bug, int64 only.** `qdbdd.exe` of quasardb-build 2785
   (`bf816772bd`, master plus a test-only commit) asserts
   `idx.valid()` at `qdb/kernel/containers/ts/indexer.hpp:239`
   (`compute_index` for an int64 column) when a bucket's int64 values
   sum, modulo two to the sixty-fourth, to `qdb_int64_undefined`: the
   running sum wraps (`qdb/aggregation/timeseries.cpp:194-213`), the
   result constructor turns that sum into the undefined pair
   (`aggregation_result.hpp:270-290`), and the index is invalid with
   every other field real. Two rows reproduce it on any push mode: the
   int64 maximum and one. The release build stores the index and
   `arithmetic_column_index::sum` then composes null for a shard whose
   values are all present (`qdb/metadata/column_index.hpp:231-234`).
   Symbolized dumps: `assert_qdbdd_10704` (async, thread "a-pipe 00",
   through `ts_async::writer::commit`, `ts_table_inserter::update_bucket`
   and the duplicate eraser) and `assert_qdbdd_9320-fast` (fast, the
   request thread, through `batch_push::process`).
4. **Context.** The death needs the service context: as a Windows
   service (the Buildkite agent's account, or LocalSystem) it dies in
   one of two to eight runs; from an SSH logon as Administrator on the
   same machine, same binaries, twenty-three consecutive runs passed.
   Both contexts are session 0 and both use the Segment Heap; the
   mechanism is the heap layout at the end of a committed run, and what
   the service start changes in that layout is not established. Full
   page heap makes the daemon too slow to use (one case in sixty-nine
   minutes).
5. **Artifacts.** On the operator's machine: `qdbd_13660.dmp` (build
   `91476e3abe`, second chance), `qdbd_4700-h1-localsystem-release-22f54da872.dmp`
   (second chance), `qdbd_2784-h1-localsystem-release-22f54da872-firstchance.dmp`
   with `qdbd_2784-fc.txt` and `heap-2784.txt`; the debug pairs
   `dbgpair-bf816772bd-{core2,haswell}.tar.zst` (`qdbdd.exe`,
   `qdbdd.pdb`, the debug `qdb_user_addd.exe` and
   `qdb_cluster_keygend.exe`). On the agents: `C:\BuildkiteAgent\dumps`
   on `h-0`, `h-1`, `h-2`.
6. **Fix shape, for the team to judge.** Either give the decoders an
   input with sixteen readable bytes after the compressed data (a copy
   into a padded buffer, or a serialization format that always
   trails the compressed bytes by at least sixteen bytes), or decode
   the last group of four values with scalar code. The test-only
   reproduction: any Stream VByte decode whose input ends at a page
   boundary with the next page unmapped (`VirtualAlloc` two pages,
   place the compressed bytes at the end of the first, free the
   second), which the C API tests can carry without a server.
   For the second bug: a saturating or checked sum, or a sum that
   steps around the sentinel; the reproduction is the two-row push
   above, which `h7_test.go` in `~/qdb-rr-scratch/scripts` runs
   through the Go binding.

### 2026-10-08 02:05 UTC: the four debug loops have not died again; H7, the int64 sum wraps to the null sentinel

The loops' state, read over SSH from each agent: every daemon alive,
no new dump, eight debug round-trip passes since the cdb-attached
loops started and no death among them. The one assert death remains
the single observation of the debug daemon failing.

| agent | account     | current run | passes this loop | run started (UTC) |
| ----- | ----------- | ----------- | ---------------- | ----------------- |
| h-0   | buildkite   | 4           | 3                | 01:40             |
| h-2   | LocalSystem | 4           | 3                | 01:30             |
| h-1   | LocalSystem | 2           | 1                | 01:52             |
| h-3   | buildkite   | 2           | 1                | 01:53             |

What the quasardb source at master `22f54da872` says about the
assertion, read before the next sample:

- `compute_index` for an int64 column asserts `idx.valid()`
  (`qdb/kernel/containers/ts/indexer.hpp:236-240`). For an int64 index
  `valid()` is the base check (the non-null count does not exceed the
  count, the first and last elements are real values), the numeric
  check (the minimum and maximum are real values) and the arithmetic
  check: when the bucket has non-null values, `element_sum` is not the
  null sentinel (`qdb/metadata/column_index.hpp:91-100,152-161,217-229`).
- `element_sum` is `aggregation::sum(bucket).int_value`
  (`indexer.hpp:121-133`). The int64 sum is a plain wrapping addition,
  by SIMD or by `acc += v` (`qdb/aggregation/timeseries.cpp:194-213`),
  and the result's constructor turns a sum equal to
  `qdb_int64_undefined` into the undefined pair
  (`qdb/aggregation/aggregation_result.hpp:270-290`). So a bucket whose
  int64 values sum, modulo two to the sixty-fourth, to the sentinel
  gets an invalid index: the debug daemon asserts, the release daemon
  stores the index and continues.
- The round trip draws int64 cells from the whole range above the
  sentinel (`internal/qdbtest/table/table.go:101-103`), and rapid's
  biased integer draw returns the range bound itself with raised
  probability and favors small magnitudes
  (`vendor/pgregory.net/rapid/utils.go:64-88`). The maximum and one in
  one shard sum to the sentinel; so do the minimum above the sentinel
  and minus one.
- The other types cannot reach that state from the test's draws: the
  double sentinel is NaN and the test draws neither NaN nor an
  infinity (`table.go:104-106`), so a double sum is never NaN; the
  timestamp index has no sum (`indexer.hpp:243-247`); blobs, strings
  and symbols carry a base index only.

H7, written before the experiment: the assertion is a second bug,
independent of the decoder over-read. The int64 column index's running
sum wraps on overflow, and a sum that lands on the null sentinel makes
the index invalid, which the debug build asserts and the release build
keeps. It is int64-only by construction. The over-read remains the
cause of the access violation.

| hypothesis                              | confirmed by                                                                                                                                                                                                                        | refuted by                                                                                                                                                                                                  |
| --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| H7: the int64 sum wraps to the sentinel | a push of two int64 rows, the maximum and one, into a fresh table on a debug daemon asserts `idx.valid()` at once, and the assert dump's `idx` shows `element_sum` equal to the sentinel with real first, last, minimum and maximum | the push passes without an assertion; or an assert dump whose `idx` is invalid in another field (a none first or last element, or a non-null count above the count), which says the decoder or another path |
| H7 is int64-only                        | the same push with doubles and timestamps, and the type scope read from the source above, hold; every assert dump is `compute_index` for int64                                                                                      | an assert dump on `compute_index` for another type                                                                                                                                                          |

The experiment: a Go test pushes the two rows into a fresh table on
`h-3`'s live insecure debug daemon, from the SSH logon. An assertion
does not depend on the heap layout, so the service context is not
needed; the push into the loop's daemon costs that agent's current run
and yields the symbolized assert dump through the attached `cdb`
(owner, 2026-10-08: the agent with the youngest loop). `h-3`'s loop is
restarted after the dump is read.

### 2026-10-08 02:30 UTC: H7 confirmed, the int64 sum wraps to the sentinel and the debug daemon asserts on any push mode

The push of two int64 rows, the maximum and one, into a fresh table
on `h-3`'s live insecure debug daemon (pid 10704, `h7_test.go` in
`~/qdb-rr-scratch/scripts`, async mode) ended the daemon within six
seconds of the push, at 02:30:25 UTC: stderr
`Assertion failed: idx.valid(), file ..\..\qdb/kernel/containers/ts/indexer.hpp, line 239`,
exit code -3, the same ending as the loop's earlier assert death on
`h-2`. The attached `cdb` hit its `_wassert` breakpoint and wrote the
full dump. What it says:

- The thread is "a-pipe 00". The stack, innermost first: `_wassert`,
  `qdb::kernel::compute_index` (the int64 overload),
  `compute_index<__int64>`, three `primitive::duplicate_eraser` frames,
  `ts_table_inserter::update_bucket`, `ts_entry_inserter::process_data`,
  `tables_directory::entry_primitive<ts_table_inserter>::execute`,
  `ts_async::writer::commit`, `ts_async::pending_requests::write`,
  `ts_async::pipeline::async_process`. So the async pipeline's flush
  recomputes the bucket's index through the duplicate eraser
  (`qdb/kernel/primitives/detail/erase.hpp:44,256`).
- The PDB names every frame but carries no locals (`dv` answers
  "Private symbols are required for locals"), so the index was read
  raw at the hidden return pointer `compute_index` received, which
  `kv` shows as the frame's first argument. The struct, in the field
  order of `column_index.hpp`: element count 2, non-null count 2,
  first element (2020-01-01T00:00:00Z, the int64 maximum), last
  element (one second later, 1), minimum (one second later, 1),
  maximum (2020-01-01T00:00:00Z, the int64 maximum), element sum
  `0x8000000000000000`, the sentinel. Every field but the sum is a
  real value, which is exactly H7's confirming observation.
- The fast push asserts the same way on the request thread
  ("sl lo1 mux-0"): `batch_push::process`, `update_bucket`, the
  duplicate eraser, `compute_index`. Dump `assert_qdbdd_9320-fast.dmp`
  on `h-3`. The transactional push asserts as well, on the request thread
  (dump `assert_qdbdd_7620-transactional.dmp`). Three modes, three
  asserts, two rows each.

H7 holds: the int64 column index's running sum wraps on overflow, a
sum that lands on the null sentinel makes the index invalid, the debug
build asserts and the release build stores the index. It is a second
bug, independent of the Stream VByte over-read: a different thread in
the fast case, no decoder in the stack, and two rows suffice. It is
int64-only by the type scope read from the source under the 02:05
heading, and the dumps bear it out: both are `compute_index` for
int64. In the release build `arithmetic_column_index::sum` composes a
sentinel sum as null (`qdb/metadata/column_index.hpp:231-234`), so an
aggregate served from that index answers null for a shard whose
values are all present.

Two defects of the operator tooling, found on the way and fixed:

- The loop script's `cdb` command lost its backslashes to the shell
  quoting, so a dump was written under the drive-relative name
  `C:BuildkiteAgentdumps<name>.dmp` into `cdb`'s working directory,
  the workspace, and not into `C:\BuildkiteAgent\dumps`. The dump is
  complete; only its place and name were wrong. `rtsvc3.sh` now writes
  the paths with forward slashes, which `cdb` accepts; `h-3` runs the
  fixed script, the loops on `h-0`, `h-1` and `h-2` carry the old
  command until their next restart, and a dump from them is found as
  `C:\BuildkiteAgent\rr\qdb-api-rest\BuildkiteAgentdumps*.dmp`.
- An SSH command that starts the daemons does not return while they
  run, and `nohup` with every descriptor redirected does not survive
  the session's end either. The reproduction runs are launched in the
  background on the operator's machine and the session is killed once
  the dump exists.

Artifacts on the operator's machine, `~/qdb-rr-scratch/dumps/`:
`assert_qdbdd_10704-h3-buildkite-debug-bf816772bd.dmp` with its
`-stack.txt` and `-raw.txt`, and `assert_qdbdd_9320-h3-fast-debug-bf816772bd-stack.txt`.

### 2026-10-08 03:33 UTC: the second bug reproduces from a C program in one push

`~/qdb-rr-scratch/scripts/h7repro.c` (goal 3 for the second bug): open a
handle, create a table with one int64 column, push two rows, the int64
maximum and one, in the mode named on the command line. Built on `h-3`
with the agent's MinGW gcc against the release C API of quasardb-build
2782 and run against a fresh debug daemon under `cdb`: the create
succeeded, the fast push ended the daemon with the same assertion, and
`cdb` wrote `assert_qdbdd_7568-c-fast.dmp`. The Go binding adds nothing
to the trigger.

Two things the program had to get right, for whoever runs it next:

- `qdb_ts_create` and `qdb_ts_create_ex` list the implied `$timestamp`
  column first, as a `qdb_ts_column_timestamp`; without it the client
  answers invalid argument, with "Missing required $timestamp column"
  in `qdb_get_last_error` (`qdb/client/node/ts.cpp:138-140` in
  quasardb). The Go binding prepends that column itself
  (`vendor/github.com/bureau14/qdb-api-go/v3/entry_timeseries_common.go:311-332`).
- The batch push carries the row timestamps in `data.timestamps` and
  the int64 column alone in `data.columns`, with a null schema pointer
  so the client reads the schema from the server.

`qdbsh` on the same daemon created a table without complaint, which is
how the client-side rejection was told apart from a server one.

### 2026-10-08 03:40 UTC: release symbols through a quasardb branch of the same name

The release daemon has no PDB because quasardb's CI configures every
build with `QDB_ENABLE_DEBUG_INFO=OFF` unless the environment says
otherwise (`scripts/cicd/build/cmake.sh:26-28`), and with it off the
linker gets `/DEBUG:NONE` in every configuration
(`cmake_modules/linker_flags.cmake:45-50`). With it on, every
configuration compiles with embedded debug records and links with
`/DEBUG:FULL` (`cmake_modules/compiler_flags.cmake:66-72`), release
included, and the release link keeps `/OPT:REF /OPT:ICF`. Debug
information does not change code generation. The harvested debug PDB
names functions but has no locals and no source lines for qdb code,
which fits a Debug configuration whose default `/debug` link survived
while the compile ran without debug records.

The owner's instruction, on a colleague's advice (2026-10-08): a
quasardb branch named `sc-19567/rr-ci-qdbd-logs`, off the latest
master, with the switch set in the branch's own pipeline, so the
artifacts plugin's branch matching gives this repository's builds the
release daemon with its PDB, and master commits cannot change the
binary under the PDB. The branch lives in the worktree
`~/git/quasardb-ci-qdbd-logs`; its one change sets
`QDB_ENABLE_DEBUG_INFO` to `ON` in the Windows environment of
`.buildkite/pipeline.py`, and `pipeline.py check` passes. The PDB is
not packaged by any step, so it is harvested from the siege agent's
release output directory right after the link, as the debug pair was
(`pdbwatch.sh`, adjusted to `bin64/Release/qdbd.pdb`). A Windows
release build job takes ten to fifteen minutes on a siege agent (the
last three master builds).

### 2026-10-08 04:20 UTC: the release daemon with its PDB runs in the LocalSystem loop on h-1

quasardb-build 2796 built the branch `sc-19567/rr-ci-qdbd-logs`
(`4b955fa4a4`) for the Windows release variants, tests skipped: the
build message is parsed as YAML filter tags by the pipeline generator
(`.buildkite/tools/qdb_pipeline/selection.py`, `get_pipeline_selection`),
so a conventional-commit message such as `ci(buildkite): ...` reads as
an unsupported tag and fails the generation (builds 2794 and 2795);
the message of 2796 is `os: windows`, `build_type: Release`,
`skip_test: true`. The core2 job ran on siege `h-1` and the haswell job
on siege `h-2`; `relpdbwatch.sh` packed `bin64/Release/qdbd.exe` with
`qdbd.pdb` on each within a minute of the link. Both pairs are on the
operator's machine under `~/qdb-rr-scratch/relpair/`.

The core2 pair is in `h-1`'s workspace (`swaprel.sh`), and its loop
runs `rtsvc4.sh`, which starts whichever daemon `qdb/bin` holds: the
release `qdbd.exe` when present, the debug `qdbdd.exe` otherwise.
`cdb` attached to both release daemons loads the PDB as private
symbols and names `compute_index` and its siblings; the `_wassert`
breakpoint has no match in release, as expected, and the first-chance
access-violation capture is what this loop is for. The daemon is the
same source as master `22f54da872` plus the pipeline change, so the
release C API of quasardb-build 2782 in the workspace matches it.

The siege agents are reachable like the default ones; `agent.sh` maps
them as `s-0` to `s-3` (`10.64.129.43`, `10.64.131.254`,
`10.64.130.205`, `10.64.129.108`).

### 2026-10-08 04:36 UTC: all four loops run the release daemon with its PDB

The owner's decision (2026-10-08): the assertion is understood and
reproduced, nothing in this repository can work around it, and the
debug daemon has shown no access violation in about twelve runs, so
the debug loops stop and every loop runs the release `qdbd.exe` of
quasardb-build 2796 with its PDB, `cdb` attached with first-chance
access-violation capture. `h-0` and `h-3` as the agent account, `h-1`
and `h-2` as LocalSystem, all restarted between 04:20 and 04:36 UTC
with `rtsvc4.sh`. The debug pair stays in each workspace next to the
release one for a swap back.

### 2026-10-08 04:34 UTC: the access violation with symbols, three times in three minutes

Every release loop but `h-3` lost its insecure daemon in its first run
after the swap: `h-1` at 04:33:01, `h-0` at 04:34:41, `h-2` at
04:36:59 UTC, two to three minutes after each start. `cdb` wrote the
first-chance dumps `av_qdbd_7740.dmp` (`h-1`), `av_qdbd_7260.dmp`
(`h-0`) and `av_qdbd_15336.dmp` (`h-2`); copies with their analyses are
in `~/qdb-rr-scratch/dumps/`. The daemons then ended with
`0xC0000354` because `cdb` stopped at the second chance with nothing
left to read on its input, which changes nothing: the re-raise would
have ended them anyway.

The three dumps carry the same fault and the same stack, named by the
PDB of quasardb-build 2796. Innermost first, on thread "a-pipe 00":

- `svb_decode_sse41` (`thirdparty/streamvbyte-2.0.0/src/streamvbyte_x64_decode.c:105`),
  inlined into `svb_decode_sse41_simple`, a `movdqu` load at fifteen
  bytes before a page boundary, the page after it not committed.
- `streamvbyte_decode`, called by `qdb::compression::delta4c_read`
  (`qdb/compression/delta4c.cpp`, the `streamvbyte_decode` calls at
  lines 596 and 639).
- `qdb::compression::v2::delta4c_null_filter_decompressor::read`,
  `delta4c_decompress<..., std::vector<qdb::timespec>>`,
  `decoder<delta4c_null_filter_decompressor>::decode`,
  `decompression_dispatcher::process_type`,
  `serialization::container_compression_codec<delta4c_null_filter_compressor, std::vector<qdb::timespec>>::decode`,
  `serialization::codec<qdb::timeseries<qdb::timespec>>::decode_values_v1`
  and `::decode`: the stored values of a timestamp column, delta4c
  compressed, are decoded straight out of the serialized value.
- `qdb::persistence::key_value::find_data_unmarshal<qdb::kernel::column<qdb::timespec>>`,
  `storage::find_data_unmarshal`, `column_persistence::find`,
  `object_cache::find_and_pin`, `find_and_pin_column`,
  `ts::load_column_from_directory<column<timespec>, ts_deleter>`: the
  value comes from the key-value store, and the decoder's input is a
  view into that stored value.
- `load_bucket_impl<ts_table_inserter>`, `ts_table_inserter::load_bucket`,
  `ts_entry_inserter::access_data`, `entry_primitive::unsafe_execute`,
  `ts_async::writer::commit`, `pending_requests::write`,
  `pipeline::async_process`: the async pipeline's flush loads the
  bucket it is about to merge into.

So the first bug, as the ticket states it: when the async flush
merges into an existing bucket, the inserter loads the bucket's
timestamp column from storage and decodes its delta4c payload with
Stream VByte from a view of exactly the stored size; the SSE decoder
reads up to fifteen bytes beyond that view, as its header documents,
and when the stored value ends within fifteen bytes of the end of a
committed heap run the read faults. The over-read is latent in every
build and every context; the heap layout under the service decides
whether the next page is committed. The debug daemon ran about twelve
round trips without it, the release daemon with symbols three for
three within minutes.

### 2026-10-08 05:30 UTC: the fourth release death, what the dumps say about the buffer, and the C program so far

`h-3` lost its release daemon at 04:53 UTC, in its first run after the
swap (dump `av_qdbd_9620.dmp`), on the same decoder, reached through
`svb_decode_sse41` line 99 instead of 105. Four release loops, four
deaths in the first run each, all in `delta4c_read` under
`ts_table_inserter::load_bucket` on the async flush thread.

The frames' locals in `av_qdbd_7260.dmp` (`h-0`, `locals.sh` and
`bufinfo.sh` in `~/qdb-rr-scratch/scripts`) say what was being
decoded and where it lived:

- The column is a data column of type timestamp (column index 6, not
  `$timestamp`), stored delta4c through the null filter, so it has
  nulls. The stored bucket held 320 timestamps; the stored value is
  1285 bytes, its compressed payload 1249 bytes.
- `key_value::find_data_unmarshal` read the value through a
  `rocksdb::PinnableSlice` that is not pinned: RocksDB copied the value
  into the slice's own `std::string`, size 1285, capacity 1295, one
  1296-byte heap block. That block ends exactly at the page boundary
  and the next page is not committed. The decoder's last sixteen-byte
  load starts eleven bytes before that boundary.

So the over-read runs off the end of an ordinary heap allocation, the
string RocksDB copied the stored value into. Whether it faults depends
only on where the heap placed that block, which is why the round trip
dies within minutes in some processes and survives hours in others.

The out-of-the-box reproduction, `avrepro.c` (plain C API, the batch
push with explicit columns, no Arrow, no REST server), has not faulted
yet. Its versions and why each could not have:

1. One int64 column, timestamps one second apart, the table removed
   right after the async push: constant deltas take delta4c's constant
   encoding and never reach Stream VByte, and the flush finds no stored
   bucket. 100000 iterations, no death.
2. Pool mode: a nullable timestamp data column with random values,
   thirty-two tables kept alive across many async and fast pushes
   (rows verified to land, with nulls): 200000 pushes, no death. The
   object cache has no eviction of its own and the test limiter allows
   gigabytes, so the flushes found their buckets in memory and decoded
   nothing from storage.
3. Pool mode with `qdb_trim_all` every 500 pushes and one push in ten
   large: 200000 pushes, no death. Whether the trim makes the next
   flush page the bucket in is not verified.
4. Cycle mode, one round-trip case per iteration (create, async push,
   remove, re-create, remove): 200000 cases, no death.

The experiment running now (owner's direction: release daemons only,
the program instead of the test framework): full page heap for
`qdbd.exe` on `h-1`, limited to allocations between 500 and 2000 bytes
(`pageheap-on.ps1`; the bounds include the 1296-byte block whether
gflags reads them as decimal or hex), with the program in pool mode
under the LocalSystem service and `cdb` attached. Page heap places such
a block at the end of a page with an inaccessible page after it, so if
the program reaches the decode of a stored timestamp column at all, it
faults at once in any context. A fault decides that the program
reaches the path and gives the deterministic recipe; no fault means the
program never decodes a stored timestamp column, and the next step is
to prove that with a breakpoint counter before changing the program.

### 2026-10-08 05:45 UTC: H8, the operation that makes the suite's flush load a stored bucket; three experiments on the idle agents

The state read over SSH at 05:40 UTC: `h-1` is in run 1 of `avrepro.exe`
pool mode under the size-limited page heap as LocalSystem since 05:23,
both daemons alive, no new dump, and the program progresses (its pool
tables have been re-created thousands of times and hold hundreds of rows
each), so the size-limited page heap does not slow the daemon the way
the full one did. `h-0`, `h-2` and `h-3` are idle with leftover daemons
and `cdb` attached to them; the Buildkite agent service is stopped on
all four. One poller runs, on `h-1`. The program's progress is not
readable while a run lasts, because `avsvc.sh` pipes its output through
`tail -6`; the next restart of any `avrepro.exe` loop writes the output
unbuffered to `logs/avrun-<epoch>.txt` instead.

What the round-trip case does that no version of the program has done,
read from `internal/httpapi/roundtrip_test.go:166-230`: after the push
it queries every table through `SELECT` (`checkQuery`,
`checkQueryFormats`), bulk-reads every table unless the mode was async,
then deletes each table, creates it again under the same name, deletes
it again and deletes it a third time, all within the async flush
deadline of half a second (`scripts/tests/setup/default.qdbd.cfg:5`).
The dumps say the flush merged into a bucket that already held 320
timestamps and that it loaded that bucket from storage through the
object cache's miss path (`av_qdbd_7260-...-analysis.txt:160-168`).
The program's pool mode pushes into the same tables hundreds of times
and never faulted, and the plan's reading was that its flushes found
their buckets in the cache. Neither the suite's miss nor the program's
hit has been measured, and the release link's `/OPT:ICF` folds
identical template instantiations, so a stack frame's template argument
names one of several folded callers (the same analysis names
`find_and_pin_column<ts_table_aggregator>` under
`load_bucket_impl<ts_table_inserter>`); a breakpoint therefore goes on
the outer function and is verified to resolve to one address.

H8, written before the samples: the suite's async flush misses the
object cache because an operation between the push and the flush drops
or bypasses the cached column, and the program lacks that operation.
The candidates, in the order the case performs them: the `SELECT` on a
table whose rows are pending in the pipeline, the bulk read, the delete
and re-creation under the same name. A second shape of H8 is that one
push is flushed as several commits to the same bucket and the first
commit stores without caching, which the program would reach as well.

| hypothesis                                                                     | confirmed by                                                                                                                                                                                                                                     | refuted by                                                                                                                                                                                |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| H8: an operation of the case, absent from the program, forces the storage read | under the round trip, `load_bucket` hits that reach `find_data_unmarshal<column<timespec>>`, their keys naming tables the case queried, read or re-created; under the program in pool mode, `load_bucket` hits that never reach the storage read | the program's flushes reach the storage read at a rate like the suite's: then the path is reached and only the heap layout differs, and the page heap run on `h-1` is the deciding sample |
| H8, second shape: one push flushes as several commits                          | storage reads under the program or the suite whose keys repeat within one flush                                                                                                                                                                  | every key read once per flush                                                                                                                                                             |

The experiments, one per agent, all with the release daemon of
quasardb-build 2796 and its PDB:

- A, `h-1`, running: the program in pool mode under the size-limited
  page heap as LocalSystem. A dump at the decoder with the stack of the
  04:34 heading: the program reaches the path and page heap is the
  deterministic recipe. A run that ends without one: the program did
  not reach the path, or page heap did not cover the allocation, and B
  and D tell which.
- B, `h-0`, from the SSH logon against fresh daemons: `cdb` on the
  insecure daemon with counting breakpoints on
  `ts_table_inserter::load_bucket` and
  `find_data_unmarshal<column<timespec>>`, each printing the frame's
  locals and continuing, then the program in pool mode for a few
  minutes. The counts and the keys are H8's refuting or confirming
  observation for the program's side. Counting does not depend on the
  heap layout, so the logon context suffices, as it did for H7.
- C, `h-2`, the same breakpoints under one run of `rt.test` from the
  SSH logon: the suite's side of the same measurement, and the keys say
  which operation preceded each storage read.
- D, `h-3`, the round trip under the same size-limited page heap, in
  the service as the agent account, `cdb` attached: the control for the
  oracle. A fault within a run says page heap covers the allocation and
  A's silence means the program does not reach the path; a run without
  a fault says the size filter misses the block and A decides nothing.

Then the program: with B and C read, `avrepro.c` gains a mode `case`
that performs the round-trip case as the test does, with the operation
C names included, and runs under page heap on the agent whose
experiment ended first. It is shrunk only from a version that faults,
one operation per step, each step recorded here before it is taken.

### 2026-10-08 06:12 UTC: the C program faults under page heap with the suite's stack; the program reaches the storage read at a high rate

Experiment A: `h-1`'s insecure release daemon (pid 8784) died at
06:05:08 UTC under the size-limited page heap, in the program's second
run of pool mode (the first run's two hundred thousand pushes passed;
the second faulted at its push 44540, forty-two minutes after the
daemons started). `cdb` wrote the first-chance dump `av_qdbd_8784.dmp`
on `h-1`. Its faulting stack, read with the PDB of quasardb-build 2796
(`srcline.sh`), is the stack of the 04:34 heading frame for frame:
`svb_decode_sse41` inlined into `svb_decode_sse41_simple`,
`streamvbyte_decode`, `qdb::compression::delta4c_read`,
`v2::delta4c_null_filter_decompressor::read`, the `delta4c_decompress`
and `decoder::decode` templates over `std::vector<qdb::timespec>`,
`codec<timeseries<timespec>>::decode_values_v1`,
`key_value::find_data_unmarshal<column<timespec>>`,
`object_cache::find_and_pin`, `load_column_from_directory<column<timespec>>`,
`load_bucket_impl<ts_table_inserter>`, `ts_table_inserter::load_bucket`,
`ts_entry_inserter::access_data`, `entry_primitive::unsafe_execute`,
`ts_async::writer::commit`. So the plain C API program, with no Arrow
and no REST server, drives the daemon into the first bug: goal 3 for
the first bug has a reproduction. What it is not yet is quick: one
fault in about two hundred and forty thousand pushes under page heap,
where a page heap fault is deterministic for any decode whose
over-read crosses the block's end, so the over-read crosses the end
only for a small fraction of the decodes. The dump's locals say which
fraction (next heading).

Experiment B refutes H8's first shape before C has run: under the
program in pool mode, twenty thousand pushes in half a minute from the
SSH logon with counting breakpoints (`avcount.sh prog`), the insecure
daemon hit `ts_table_inserter::load_bucket` 7434 times,
`find_data_unmarshal<column<timespec>>` 1287 times and
`find_data_unmarshal<column<__int64>>` 2116 times. The program's
flushes load their buckets from storage at a high rate; the plan's
reading that they found them in the cache was wrong, and the
`qdb_trim_all` the program issues is not needed for that (whether the
trim contributes is not measured). The three breakpoints resolved to
one address each (`symq.sh`), so the folded-symbol caveat did not
apply to them.

Page heap on `h-1` is confirmed from the registry: `GlobalFlag`
`0x02000000`, `PageHeapFlags` `0x83`, `PageHeapSizeRangeStart` `0x1f4`
and `PageHeapSizeRangeEnd` `0x7d0`, so gflags read the bounds as
decimal, five hundred to two thousand bytes.

Experiments C (`h-2`, counters under one `rt.test` run) and D (`h-3`,
the round trip under the same page heap in the service as the agent
account, started 06:12 UTC) run on; their results get their own
headings. The poller on `h-1` stopped on the new dump, as designed;
one runs on `h-3`.

### 2026-10-08 06:18 UTC: the fault's shape from the page heap dump; H9, the over-read leaves the stream only when the value count is a multiple of thirty-two

What `av_qdbd_8784.dmp` says (`avfault.sh`, copied home as
`av_qdbd_8784-h1-pageheap-release-4b955fa4a4-firstchance-fault.txt`):

- The decoded column held 86432 timestamps (`delta4c_read`'s
  `depacked` vector and `streamvbyte_decode`'s `count`), and 86432 is
  2701 times 32. The suite's dump of the 05:30 heading held 320, which
  is 10 times 32.
- The Stream VByte stream is the last field of the stored value:
  `delta4c_read` subtracts the decoder's return from `remaining` and
  fails unless nothing is left (`qdb/compression/delta4c.cpp:639-640`),
  and in the dump the stream's end (`src` plus `remaining`) and the
  value's end (`value.data_` plus `value.size_`) are the same address.
- The faulting load is the last group's: its data is six bytes long and
  ends at the value's end; the sixteen-byte load reads ten bytes past
  it, nine of which are the bytes RocksDB keeps after the value in its
  block, and the tenth is the first byte of page heap's guard page.
- This time the slice is pinned (`value.pinned_` true): the decoder
  reads RocksDB's own block buffer, not a copied string, so the
  over-read runs off whichever allocation holds the value.

The decoder's structure says when the over-read leaves the stream.
`streamvbyte_decode` hands the SSE path `count` values; the SSE path
decodes `count / 32` blocks of thirty-two values with sixteen-byte
loads, each group of four values consuming four to sixteen bytes, and
the remaining `count % 32` values are decoded by scalar code
(`thirdparty/streamvbyte-2.0.0/src/streamvbyte_decode.c:66-73`,
`streamvbyte_x64_decode.c:40-108`). The last SSE load therefore reads
sixteen minus the last group's length bytes beyond that group. When
`count % 32` is zero there is no scalar tail, and those bytes lie
beyond the stream and the value. When a tail exists its data follows
the SSE data and absorbs the over-read, in whole when the tail is long
enough. The group's length is short when the four values are small,
which delta4c makes them: the Stream VByte values are indices into the
encoding table of deltas (`encoding_table`, 5356 entries in the dump),
so a bucket with few distinct deltas encodes in one or two bytes per
value.

H9, written before the sample: the over-read leaves the stored value
only when the column's value count is a multiple of thirty-two (or
leaves a tail of fewer values than the over-read has bytes), and it
faults when the value ends within that over-read of an unreadable page,
which page heap arranges for every allocation. Confirmed by: the
program's `shape` mode, which pushes a multiple of thirty-two rows into
a fresh table with the fast push and then one more row with the async
push, so the flush loads the stored bucket and decodes it, faulting
within its first few iterations under page heap with the same stack.
Refuted by: hundreds of such iterations without a fault, while the pool
mode faults.

The experiment runs on `h-1` from the SSH logon, page heap still on,
fresh daemons, `cdb` attached (`avshape.sh`). A fault settles the
quick reproduction for the ticket; the shrinking then removes the
int64 column, the nulls and the second column one step at a time.

### 2026-10-08 06:24 UTC: experiment C, the round trip under the counters dies from the SSH logon, without a dump

On `h-2`, from the SSH logon as Administrator, with `cdb` attached to
the insecure release daemon for the three counting breakpoints and no
first-chance capture (`avcount.sh rt`), one run of `rt.test`
`TestRoundtrip` failed after 149 seconds on the suite's signature: a
`reader_init` refused, then the circuit breaker open. The insecure
daemon is gone and the secure one alive (`tasklist`); no dump was
written, because the counter script's `cdb` had no `sxe av` command,
and `cdb` is gone with the daemon. Before the death the breakpoints
counted 59 `load_bucket` hits and 116 `find_data_unmarshal<column<timespec>>`
hits, so under the suite the storage read of a timestamp column is
reached more often outside `load_bucket` (the bulk reads and queries)
than inside it, and the suite's rate per bucket load is of the same
order as the program's.

Two things this sample says. The round trip's death is not bound to the
service context: this one happened from the logon, where twenty-five
earlier runs had passed, with a debugger attached and breakpoints
firing on the flush path. And the counter script is not a capture
script: every `cdb` attach from here on carries the first-chance
access-violation dump command, counters or not. The key dumps the
breakpoints printed (`dt -r1` of `data_key`) show the structure and
not the table name; the name sits behind the `alias` slice and was not
read.

### 2026-10-08 06:28 UTC: H9 confirmed, the shape mode faults in seventy-one iterations, this time on the int64 column

The shape mode (`avrepro.c`, mode `shape`: a fresh table, a fast push
of a multiple of thirty-two rows with no nulls, an async push of one
row into the same day, a pause past the flush deadline, remove) ran on
`h-1` from the SSH logon with page heap on (`avshape.sh`), and the
insecure release daemon faulted during the seventy-first iteration,
within two minutes of the start; `cdb` wrote `av_qdbd_13952-shape.dmp`.
The stack is the same decoder path as every earlier dump,
`svb_decode_sse41` under `streamvbyte_decode`, `delta4c_read`,
`delta4c_null_filter_decompressor::read`, `find_data_unmarshal`,
`object_cache::find_and_pin`, but through `column<__int64>` instead of
`column<timespec>`: the flush decoded the stored int64 column `c_int`.
Its bounds (`shapeinfo.sh`): 423 values, an empty encoding table, a
stream of 0x211 bytes ending two bytes before the end of a page heap
block; the last SSE group's four bytes of data are followed by the
seven one-byte values of the scalar tail, and the group's sixteen-byte
load runs twelve bytes past the group, five past the stream, three into
the guard page. So the over-read leaves the stream whenever the scalar
tail is shorter than the last group's over-read, and a multiple of
thirty-two is the case with no tail at all. H9 holds in that form, and
the bug is the decoder's, for every delta4c column type: the suite's
dumps show it on a timestamp column, the shape mode on an int64 one.

The quick reproduction for the ticket is therefore: under page heap
(`gflags /p /enable qdbd.exe /full`, with or without a size range),
push a few hundred rows with the fast push into a fresh table, then one
row with the async push into the same shard; repeat with fresh tables;
the daemon dies within a hundred iterations with the stack above. Why
the pool mode needed two hundred and forty thousand pushes while the
shape mode needs seventy iterations is not measured; the shape mode
decodes a freshly stored bucket on every iteration, the pool mode's
flushes mostly merged in memory or decoded values whose tails absorbed
the over-read.

The shrinking, one variable per step, each run to a fault or three
hundred iterations under page heap: step 1 drops the int64 column, so
the table has the timestamp data column alone (the suite's case); step
2 drops the timestamp data column instead, int64 alone. The program
gains an eighth argument, `both`, `ts` or `int`, for the data columns
it creates and pushes. Steps 1 and 2 are independent and run at the
same time, step 1 on `h-1` and step 2 on `h-0` (page heap enabled
there first).

### 2026-10-08 06:40 UTC: shrink steps 1 and 2 both fault, and the decoded column is an int64 column the program did not create

Step 1 (`h-1`, the timestamp data column alone, `avshape.sh 300 2048
ts`) faulted in its 106th iteration, ninety-five seconds after the
daemons started; step 2 (`h-0`, the int64 column alone, `avshape.sh
300 2048 int`) in its 107th, ninety-five seconds as well. Both dumps
(`av_qdbd_14772-shape.dmp` on `h-1`, `av_qdbd_5696-shape.dmp` on `h-0`)
carry the decoder stack through `column<__int64>`, so step 1's fault
decoded an int64 column while its table had no int64 data column.
The three shape-mode faults decoded streams of 418, 419 and 423
values with no encoding table and about 0x20b bytes, whatever the
table's row count (864, 1856, 1824) and columns; the counts grow with
the operations the program performed (about four per iteration:
create, two pushes, remove), not with the rows it pushed. So the
stored column the flush decodes is one of the daemon's own: the
persisted firehose the daemon creates at startup ("creating persisted
firehose $qdb.firehose with a shard size of 3600000ms" in every
daemon log), which records entry events as rows, is the candidate;
the data key read from the dumps' `find_data_unmarshal` frame names
it or refutes it (`keyinfo.sh`).

What this changes: the shape mode faults the daemon through its own
bookkeeping table, so the recipe for the ticket is smaller than two
pushes per table. If the key is the firehose's, step 3 removes the
pushes: create and remove tables, nothing else, under page heap. The
suite's dumps remain the user-table case of the same bug (a timestamp
data column of 320 values), so the ticket states both: any stored
delta4c column whose Stream VByte stream ends with a scalar tail
shorter than the last SSE group's over-read.

### 2026-10-08 06:50 UTC: shrink step 3, create and remove alone, faults in 210 iterations

Mode `churn` of `avrepro.c` (create a table, remove it, pause, nothing
else) ran on `h-1` from the SSH logon under page heap (`avshape.sh 400
64 both churn`) and the insecure release daemon faulted during the
211th iteration, three minutes after the start (`av_qdbd_1164-shape.dmp`
on `h-1`, copied home). The stack is the decoder path through
`column<__int64>` again, the stream has 416 values, thirteen blocks of
thirty-two and no tail, and the data key is read next. So the
reproduction for the ticket needs no rows: a client that creates and
removes tables in a loop, against a daemon started under page heap,
ends it within minutes through the daemon's own firehose table. The
program is `~/qdb-rr-scratch/scripts/avrepro.c`; its driver on the
agents is `avshape.sh`.

### 2026-10-08 06:55 UTC: the ticket for the qdbd team, with the C reproduction

The data key of step 3's decode is `..ts.$qdb.firehose.col.2.bkt.001791439200000`,
the same as steps 1 and 2. What the ticket says, in order:

1. Symptom: `qdbd.exe` 3.15.0.dev0 (master `22f54da872`) on Windows
   dies with `0xC0000005` on thread "a-pipe 00" during "flushing async
   pipeline pipe_0 to disk", with no log entry, no error dump and no
   Windows Error Reporting event, because the access violation is
   caught by a catch funclet and re-raised.
2. Fault: `svb_decode_sse41` (`thirdparty/streamvbyte-2.0.0/src/streamvbyte_x64_decode.c`,
   inlined into `svb_decode_sse41_simple`) under `streamvbyte_decode`,
   `qdb::compression::delta4c_read` (`qdb/compression/delta4c.cpp:639`),
   `v2::delta4c_null_filter_decompressor::read`, the container codec,
   `key_value::find_data_unmarshal`, `object_cache::find_and_pin`,
   `ts_table_inserter::load_bucket`, `ts_async::writer::commit`. The
   SSE decoder's last sixteen-byte load reads past the end of its input
   by up to twelve bytes; the input is a view of exactly the stored
   compressed size into the value RocksDB returned (its block buffer
   when pinned, its copied string otherwise), with no padding after it.
   The library requires `STREAMVBYTE_PADDING` readable bytes after the
   input (`include/streamvbyte.h:52-69`). The over-read leaves the
   value when the scalar tail (`count % 32` values) is shorter than the
   last group's over-read, and faults when the value ends within that
   distance of an unmapped page. Seen on a user table's timestamp
   column (the CI suite, 320 values) and on `$qdb.firehose` column 2
   (int64, 416 to 423 values).
3. Reproduction, deterministic within minutes: enable page heap for the
   daemon (`gflags /p /enable qdbd.exe /full`, a size range of 500 to
   2000 bytes suffices), start it with the test configuration, then run
   `avrepro.c` (plain C API) in mode `churn`: create a table, remove
   it, pause, repeat. The daemon's firehose flush decodes its own int64
   column and dies within about two hundred iterations. Mode `shape`
   (a fast push of a multiple of thirty-two rows, an async push of one
   row, remove) dies within about a hundred. Without page heap the
   fault depends on the heap layout: the CI suite dies in one of two to
   eight runs as a service.
4. Fix shape, for the team: decode from a padded copy, or serialize
   the compressed bytes with at least sixteen trailing bytes, or decode
   the last block with scalar code.
5. The second bug, separately: the int64 column index's running sum
   wraps to `qdb_int64_undefined` and the debug build asserts
   `idx.valid()` (`qdb/kernel/containers/ts/indexer.hpp:239`); two rows,
   the int64 maximum and one, reproduce it on any push mode
   (`h7repro.c`).
6. Artifacts, on the operator's machine under `~/qdb-rr-scratch/dumps/`:
   the first-chance dumps with the release PDB of quasardb-build 2796
   (`av_qdbd_7260`, `7740`, `15336`, `9620` from the suite; `8784`
   from the pool mode; `13952`, `14772`, `5696` from the shape mode;
   `1164` from the churn mode), their analyses, and the release pairs
   `relpair-4b955fa4a4-{core2,haswell}.tar.zst`; `scripts/avrepro.c`
   and `scripts/h7repro.c`.

Where the ticket goes is the owner's decision (plan, Outcome, goal 3).

### 2026-10-08 08:05 UTC: the agents are back in the pool

Owner decisions: the tickets are written by hand, outside the QuasarDB
workflow, and no further run is needed. All eight agents were restored
(`restore.ps1` on `h-0` to `h-3`, `siegeclean.ps1` on `s-0` to `s-3`,
both in `~/qdb-rr-scratch/scripts`): the loop services stopped and
uninstalled, page heap disabled, every process of the investigation
ended, the `rr` workspaces, service directories, symbol caches and
scripts deleted, the Buildkite agent service started; on the siege
agents the PDB watch loops, their scripts, logs and archives removed.
Left in place, pending the owner's word: `C:\BuildkiteAgent\dumps` and
`C:\BuildkiteAgent\tools` (cdb, procdump) on `h-0` to `h-3`; every dump
the plan names has a copy in `~/qdb-rr-scratch/dumps/`. The quasardb
branch `sc-19567/rr-ci-qdbd-logs` and its worktree still exist.

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

## Status for the next session (2026-10-07, after the in-place reproduction)

The branch is `sc-19567/rr-ci-qdbd-logs`, pushed; commits after
`4a02b86` are unsigned at the owner's request (1Password was away).
Nothing is merged. What the unit established, in the order of its
goals:

1. The daemon's account of its death: there is none, because the death
   is an access violation (`0xC0000005`, a read at a page boundary) on
   the async pipeline's thread `a-pipe 00` during a flush to disk,
   caught and re-raised by qdbd's own exception translation, which
   leaves no log entry, no error dump and no Windows Error Reporting
   event. The full dump of one death is
   `C:\BuildkiteAgent\dumps\qdbd_13660.dmp` on agent
   `default-windows-amd64-h-0` and a copy is in the owner's hands;
   qdbd build `91476e3abe` (3.15.0.dev0, quasardb-build 2762), no
   PDB in the dist. The dated headings above carry the stack offsets.
2. The payload: the round trip's async pushes. The death follows the
   traffic (experiment A), never happens in a loop without pushes (the
   H6 loop), and happens only when qdbd runs in the agent's service
   context (session 0, the `buildkite` account): eleven clean runs
   from a user session against deaths in the first and fourth runs as
   a service, on the same machine with the same binaries. A reading
   that fits all of it, offered to the qdbd team as a hypothesis and
   not a finding: a buffer over-read in the flush that is latent
   everywhere and faults only where the heap layout leaves the next
   page unmapped. The smallest sequence is not yet known: a loop of
   async pushes and flushes against the C API is the next narrowing
   step, and it must run in the service context to be a test at all.
3. The reproduction and the ticket: not done. The reproduction exists
   as `TestRoundtrip` under the temporary WinSW service
   (`rtsvc.sh`, kept in this plan's dated headings), not yet as a C
   program. Where the ticket goes is the owner's call.

What is left on the agent and in the tree: the agent's Buildkite
service was started again and the temporary `qdb-rtsvc` service
removed; `C:\BuildkiteAgent\tools` (procdump, the debugging tools)
and `C:\BuildkiteAgent\dumps` stay until the owner says otherwise.
`QDBTEST_TRAFFIC_TO_SECURE` is still set on Windows in `30.test.sh`,
and `observeContext` still logs at debug level; both are experiment
settings to reverse before the merge stage, or to hand off.

How the agents are reached is in the dated heading "the in-place H6
loop".

### Snapshot, 2026-10-07 evening: what is known, what is not, what the owner wants next

Known, each with its evidence above:

- The death is an access violation re-raised by qdbd's own exception
  translation, on the async pipeline thread, during a flush to disk
  (the dump, `qdbd_13660.dmp`; the daemon log's last line).
- It needs the tests' pushes and follows the traffic (experiment A;
  the H6 loop).
- It happens in the agent's service context and not from a user
  session on the same machine with the same binaries (eleven passes
  against deaths in the first and fourth service runs).
- Neither Buildkite's job object nor any process kill is involved (the
  WinSW copy has no job object; the agent logs no kill; the exit code
  is the exception's own status).

Not known, and the owner is not satisfied until they are:

- The faulting instruction and the full stack with symbols. The
  consolidated frames hide the original fault; the dist has no PDB;
  the stack is image offsets (`qdbd+0x3fe6e4`, `+0x452c54`,
  `+0x597c36`, the re-raise at `+0x1926ec6`, the thread entry at
  `+0x1910b93`) against build `91476e3abe`.
- Why the service context. The page-layout reading above is a
  hypothesis. What differs in session 0 and has not been tested one
  at a time: the account (`buildkite` as a service against
  Administrator in a session), the session itself (session 0 against
  an interactive one, same account), the window station's desktop
  heap, the environment the agent hands its jobs. A loop as the
  `buildkite` account in an interactive session, or as Administrator
  in a service, separates the account from the session.

The owner's next steps, 2026-10-07:

1. A full stack trace: a symbolized `qdbd.exe` of the same source, or
   the PDB of build `91476e3abe` from the qdbd team, so `cdb` can
   name the frames in `qdbd_13660.dmp`.
2. A debug build of qdbd in the loop instead of the release one.
   quasardb-build has `windows-core2-debug` and
   `windows-haswell-debug` variants (their reports are in the artifact
   store); on a paused agent the debug server dist goes into `qdb/`
   in place of the release one and the service loop runs as before.
   A debug build keeps its asserts and its symbols, so the fault
   should name itself.
3. An answer to the service-context question, by the one-variable
   experiments above.

The tools for all three are in place: the agent access, the service
loop (`rtsvc.sh` under WinSW as the `buildkite` account), the watcher,
procdump and the debugging tools on agent `h-0`.

## Status for the next session (2026-10-08 05:45 UTC, H8 and the three experiments)

Read the dated headings of 2026-10-08 above first, the last one
(05:45) in full; this section is the operational state only. It is
written so that a session started with `/rr-start @docs/ci-qdbd-logs-plan.md`
can resume without anything from the conversation that wrote it.

### Where this unit lives

- Worktree: `~/git/qdb-api-rest-ci-qdbd-logs`, branch
  `sc-19567/rr-ci-qdbd-logs`, pushed. Main checkout `~/git/qdb-api-rest`
  is not touched from here. Commits made while 1Password was away are
  unsigned; sign or leave them, the owner decides.
- quasardb worktree: `~/git/quasardb-ci-qdbd-logs`, branch
  `sc-19567/rr-ci-qdbd-logs` (one commit, `4b955fa4a4`, on master
  `22f54da872`, pushed): `.buildkite/pipeline.py` sets
  `QDB_ENABLE_DEBUG_INFO=ON` for Windows so release builds carry a PDB.
  quasardb-build 2796 built it for the Windows release variants. The
  branch is never merged; it is deleted when the investigation ends.
  Its `.buildkite/tools` submodule is not checked out (the clone is
  refused); the pipeline check runs with `~/qdb-rr-scratch/.venv-qdb-pipeline`
  after copying the main checkout's tools in, which then must be
  removed again before any commit.
- Scratch, outside both trees: `~/qdb-rr-scratch/`. Nothing in it is
  committed.

| path                         | what it holds                                                                                                                                   |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `agent.sh`                   | SSH as Administrator: `agent.sh h-N '<cmd>'` (default agents) or `s-N` (siege agents); `agent.sh push <h-N> <files>` to `C:\BuildkiteAgent\rr\` |
| `pull.sh`                    | `pull.sh <h-N\|s-N> '/C:/path' <local>`: copy a file from an agent                                                                              |
| `poll/poll.sh`, `poll/*.log` | the per-agent poller and its logs                                                                                                               |
| `dumps/`                     | every dump copied home with its analysis; the four release first-chance dumps are `av_qdbd_*-h<N>-release-4b955fa4a4-firstchance.dmp`           |
| `relpair/`                   | `qdbd.exe` with `qdbd.pdb` of quasardb-build 2796, core2 and haswell                                                                            |
| `dbgpair/`                   | the debug `qdbdd.exe` with PDB of quasardb-build 2785                                                                                           |
| `scripts/`                   | everything the agents run (below)                                                                                                               |

The scripts that matter now, all in `scripts/`:

| script                                                    | runs on        | does                                                                                                                      |
| --------------------------------------------------------- | -------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `rtsvc4.sh`, `rtsvcstatus4.sh`, `svc-restart4.ps1`        | default agents | the round-trip loop under the service against whichever daemon `qdb/bin` holds (release `qdbd.exe` first), `cdb` attached |
| `avrepro.c`                                               | default agents | the C reproduction; usage and modes in its header                                                                         |
| `avsvc.sh`, `svc-restart-av.ps1`                          | default agents | the same service loop running `avrepro.exe` instead of the round trip; `AV_MODE` defaults to `pool`                       |
| `avrun.sh`                                                | default agents | `avrepro.exe` from the SSH logon against fresh daemons                                                                    |
| `avcheck.sh`                                              | default agents | the program's progress, the daemons, the dumps                                                                            |
| `analyze-dbg.sh`, `srcline.sh`, `locals.sh`, `bufinfo.sh` | default agents | read a dump: `!analyze`, frames with lines, every frame's locals, the decoder's input buffer                              |
| `swaprel.sh`                                              | default agents | unpack a release pair into `qdb/bin`                                                                                      |
| `pageheap-on.ps1`                                         | default agents | size-limited full page heap for `qdbd.exe`                                                                                |
| `relpdbwatch.sh`                                          | siege agents   | pack `bin64/Release/qdbd.exe` with its PDB right after the link                                                           |
| `h7repro.c`, `h7_test.go`, `h7c-run.sh`                   | default agents | the second bug's two-row reproduction                                                                                     |

### What is running

| agent | IP            | service | account | daemon in `qdb/bin`                                            | state at 05:30 UTC                                                      |
| ----- | ------------- | ------- | ------- | -------------------------------------------------------------- | ----------------------------------------------------------------------- |
| h-1   | 10.64.130.209 | none    | agent   | release `4b955fa4a4` (dists in the agent's own build dir only) | restored to the Buildkite pool at 08:04 UTC; `dumps` and `tools` remain |
| h-0   | 10.64.129.249 | none    | agent   | release `4b955fa4a4` (dists in the agent's own build dir only) | restored to the Buildkite pool at 08:04 UTC; `dumps` and `tools` remain |
| h-2   | 10.64.129.133 | none    | agent   | release `4b955fa4a4` (dists in the agent's own build dir only) | restored to the Buildkite pool at 08:04 UTC; `dumps` and `tools` remain |
| h-3   | 10.64.130.170 | none    | agent   | release `4b955fa4a4` (dists in the agent's own build dir only) | restored to the Buildkite pool at 08:04 UTC; `dumps` and `tools` remain |

The Buildkite agent service runs on all four again.

The siege agents (`s-0` 10.64.129.43, `s-1` 10.64.131.254, `s-2`
10.64.130.205, `s-3` 10.64.129.108) are in the Buildkite pool and run
nothing of this unit; `C:\BuildkiteAgent\relpair-4b955fa4a4.*` and
`C:\BuildkiteAgent\rr\relpdbwatch.sh` are leftovers there.

### How to resume, in this order

1. Start one poller per agent that runs a loop, each in its own
   background task so the harness wakes the session when it exits:

       ~/qdb-rr-scratch/poll/poll.sh h-1

   `poll.sh` asks the agent's `rtsvcstatus.sh` every four minutes,
   appends to `poll/<h-N>.log`, and exits on a daemon exit in the watch
   file, a loop end, fewer than two daemons, or a dump that was not
   there at its first poll. For the C program on `h-1` a faster check
   is `agent.sh h-1 'C:\Git\bin\bash.exe C:\BuildkiteAgent\rr\avcheck.sh'`.
   Pollers die with the session; the loops on the agents do not.

2. Read `h-1`'s outcome with `avcheck.sh`. A new `av_qdbd_<pid>.dmp`
   in `C:\BuildkiteAgent\dumps` is the page heap fault; a finished run
   ("run 1 ended") without one is the other branch.
3. On a fault: `agent.sh push h-1 scripts/srcline.sh`, then
   `agent.sh h-1 'C:\Git\bin\bash.exe C:\BuildkiteAgent\rr\srcline.sh C:\BuildkiteAgent\dumps\<file>.dmp'`
   and check the stack against the 04:34 heading. Same stack: the
   program reproduces the bug deterministically under page heap. Then
   shrink it one variable at a time (no trim; one table; fast pushes
   only; no nulls; no int64 column) to the smallest sequence that still
   faults, and record each step here before taking it.
4. Without a fault: prove whether the program decodes a stored
   timestamp column at all, before changing it. Attach `cdb` to the
   insecure daemon with a counting breakpoint on
   `qdbd!qdb::persistence::key_value::find_data_unmarshal<qdb::kernel::column<qdb::timespec> >`
   and run the program for a minute. Zero hits: the program never
   pages a timestamp column in; find what makes the round trip do it
   (its bulk reads and queries between pushes are the next candidates).
   Hits without a fault under page heap: the string capacity leaves
   slack past the over-read for the sizes drawn, so vary row counts
   toward the 320-row, 1285-byte value of the dumps.
5. Restart a loop on an agent with
   `agent.sh <h-N> 'powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\BuildkiteAgent\rr\svc-restart-av.ps1 -Svc <rtsvc|rtsvcsys> -Dir <rtsvc|rtsvcsys>'`
   for the C program, or `svc-restart4.ps1` with the same arguments for
   the round trip. `rtsvc` is the agent account on `h-0` and `h-3`,
   `rtsvcsys` LocalSystem on `h-1` and `h-2`.

### Gotchas on these agents

- A command run over SSH that starts qdbd never returns while the
  daemons live; run it as a local background task and kill the local
  client afterwards. `nohup` on the agent does not outlive the session.
- Quoting through `agent.sh` breaks on `$(...)`, pipes and nested
  quotes; put the command in a script, push it, run it with
  `C:\Git\bin\bash.exe C:\BuildkiteAgent\rr\<script>`.
- MSYS bash rewrites `/p` and similar arguments into paths; run
  `gflags` through PowerShell (`pageheap-on.ps1`).
- In a `cdb -c` command inside bash double quotes, backslashes in a
  dump path are lost; write the path with forward slashes.
- `qdb_ts_create` needs the `$timestamp` column listed first or
  answers invalid argument.
- `cdb`'s `sxe -c "...; gn" av` writes the first-chance dump and passes
  the exception on; the daemon then ends with `0xC0000354` instead of
  `0xC0000005`, which changes nothing.
- A quasardb-build created through the API takes its message as YAML
  filter tags; use `os: windows`, `build_type: Release`,
  `skip_test: true` and never a conventional-commit line.

### Restoring the agents, when the owner says so

On `h-1` first: page heap off, through PowerShell:
`& 'C:\Program Files (x86)\Windows Kits\10\Debuggers\x64\gflags.exe' /p /disable qdbd.exe`,
then `gflags /p` must list no application. On each of h-0 to h-3: stop
and uninstall the loop service (`C:\BuildkiteAgent\<rtsvc|rtsvcsys>\<rtsvc|rtsvcsys>.exe stop`
then `uninstall`), kill `rt.test`, `avrepro`, `cdb`, `qdbd`, `qdbdd`,
`bash`, then `Start-Service buildkite-agent`. Keep
`C:\BuildkiteAgent\dumps` and `C:\BuildkiteAgent\tools` until the
owner releases them; the copies home cover every dump named in this
plan. On the siege agents delete the `relpair-*` files and
`rr\relpdbwatch.sh`. Delete the quasardb branch on origin and its
worktree.

### Repository state

Two experiment settings are on this branch and must be reversed or
handed off before any merge: `QDBTEST_TRAFFIC_TO_SECURE` in
`30.test.sh`, and `observeContext` at debug level. The commit that
teaches the watcher and the event capture the debug name stays.

### Next, in the owner's order

1. The out-of-the-box reproduction of the decode fault, C API only,
   no Arrow (running: page heap on `h-1`).
2. The ticket for the qdbd team from the handoff heading, both bugs,
   once the reproduction exists.
3. Restore the agents and delete the quasardb branch.

## Open questions and recommendations

None. The owner settled the three the first revision carried: one
re-run of each Windows job per build, the ticket's destination is a
question for when a reproduction exists, and the server's test log runs
at debug level as a temporary measure.
