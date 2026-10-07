# Plan: the build step uploads qdbd's logs with the test report

Status: draft

## Outcome

When this unit lands, every per-platform build step uploads an archive of
each qdbd daemon's log directory and console output with its test report,
whether the Go tests pass or fail. The report page of a job links the
archives under "qdbd logs". The archives hold every log line up to a tenth
of a second before a daemon stops or dies, because the qdb-test-setup
submodule now starts qdbd with a short log flush interval.

Left for later units: the windows-core2 daemon death itself (`docs/log.md`,
Next), and the e2e step's own log handling when `scripts/cicd/40.test-e2e.sh`
joins the build step.

## Verified facts

- Buildkite runs the post-command and pre-exit hooks plugins first and the
  repository last. Evidence: the hooks documentation's job hook order
  table ("post-command: Plugin (vendored), Plugin (non-vendored),
  Repository, Agent"), and the windows-core2 job of build 90
  (`01a113fb-aa2f-4133-9f0f-4ae9d39e187d`): the plugin's post-command
  hook logged "Generating test report" at timestamp 1791336722072 and the
  repository pre-exit hook started at 1791336727068, after the upload.
- The test-report plugin uploads in its post-command hook
  (`~/git/qdb-test-report-buildkite-plugin`, origin/master `3f2d988`,
  `hooks/post-command`). A `job.artifacts` item is `{name, input_path}`
  (`plugin.yml`); a glob is resolved against the job's cwd and matches
  files only; an empty match is a warning, never a failure
  (`lib/artifact_inputs.py`, `_collect_for_artifact` and
  `collect_artifact_files`). quasardb's test step uploads its server logs
  through this block (`~/git/quasardb/.buildkite/steps/_test.yml:36-40`).
- `cleanup.sh::archive` in the submodule writes
  `logs/qdbd-logs-<epoch>-{insecure,secure}.tar.gz` relative to the cwd,
  only for a log directory that exists (`scripts/tests/setup/cleanup.sh`,
  `archive_log_dir`; `config.sh`, `QDB_LOG_ARCHIVE_PATH`). At PR 5's head
  (`147c4d2b3bb6c551e9c8a33acd615ad9c1eb0518`) each archive also carries
  the daemon's console files `qdbd_log_<mode>.{out,err}.txt` when they
  exist, and `start-services.sh` passes `--log-flush-interval` from
  `QDB_LOG_FLUSH_INTERVAL_MS`, default 100.
- `stop-services.sh` kills with `pkill -SIGKILL` and `Taskkill //F`
  (`scripts/tests/setup/utils.sh`, `kill_instances`), so stopping qdbd
  flushes nothing. qdbd's default flush interval is 3 s, floor 10 ms
  (quasardb `qdb/log/config.hpp:18,33`).
- `30.test.sh` runs with `set -euxo pipefail` and changes to `BASE_DIR`
  before the tests (`scripts/cicd/30.test.sh:7,17`). `logs/`, `*.out.txt`
  and `*.err.txt` are gitignored (`.gitignore`).
- No other API or tool pipeline uploads qdbd logs: qdb-api-go,
  qdb-api-python and qdb-nats-connector have the same pre-exit hook and
  no artifacts block (their `.buildkite/` trees, 2026-10-07).
- The PR 5 head commit is on the branch
  `sc-19918/capture-complete-qdbd-logs-in-ci-archives` of
  `bureau14/qdb-test-setup`; a squash-merge replaces it with one commit
  on master, and the branch is deleted (the QuasarDB workflow).

## Design

### `scripts/cicd/00.common.sh`

Header list gains one line:

```
#   cicd_archive_qdbd_logs_on_exit -- EXIT trap: archive both daemons' logs, keep the exit status
```

New function, appended after `cicd_setup_qdb_env`, with its comment block
in the file's convention:

```
# cicd_archive_qdbd_logs_on_exit -- EXIT trap for a test step script: archive
# both qdbd daemons' log directories and console files into logs/, then exit
# with the status the script was exiting with.
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
    # logs are flushed every QDB_LOG_FLUSH_INTERVAL_MS, and stopping would
    # SIGKILL it, which flushes nothing.
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

Qualifies for a narrative under `narrative.md` rule 3: the order of the
three steps matters (reading `$?` first, re-raising last), and the
subshell is a deliberate construct.

### `scripts/cicd/30.test.sh`

Header comment gains one sentence after the JUnit sentence:

```
# At exit, whatever the tests' outcome, both qdbd daemons' logs are archived
# into logs/ for the test-report plugin to upload (cicd_archive_qdbd_logs_on_exit).
```

After `cd "${BASE_DIR}"`, before `cicd_setup_go_toolchain`:

```
# The archive runs inside this command because the plugin uploads before any
# repository hook runs (00.common.sh, cicd_archive_qdbd_logs_on_exit).
trap cicd_archive_qdbd_logs_on_exit EXIT
```

Bare otherwise (rule 1: the trap line says everything).

### `.buildkite/steps/_build.yml`

The header comment gains:

```
# The test step archives both qdbd daemons' logs into logs/ at its exit
# (scripts/cicd/30.test.sh) and the test-report plugin uploads them with the
# report; the pre-exit hook runs after the upload, so its own archive is unused.
```

The plugin block gains:

```
  - bureau14/qdb-test-report#master:
      title: "Test report {slug}"
      job:
        variant: {slug}
        junit_input_path: "test-reports/*.xml"
        artifacts:
          - name: "qdbd logs"
            input_path: "logs/qdbd-logs-*.tar.gz"
```

`python3 pipeline.py check` passes after the change (`.buildkite/AGENTS.md`,
Layout).

### `.buildkite/hooks/pre-exit`

The comment gains one sentence:

```
# stop-services.sh archives the log directories as well, but this hook runs
# after the test-report plugin has uploaded, so the archive Buildkite shows is
# the one scripts/cicd/30.test.sh made at its exit.
```

No code change.

### `scripts/cicd/AGENTS.md`

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

### `.buildkite/AGENTS.md`

The "qdbd runs in CI" fact gains:

```
  Both daemons' logs are uploaded with the test report through the
  plugin's `job.artifacts` block (`steps/_build.yml`), the way quasardb's
  test step uploads its server logs. The archive is made inside the test
  command (`scripts/cicd/AGENTS.md`), because post-command and pre-exit
  hooks run plugins first and the repository last, so nothing a hook
  produces reaches the upload. Buildkite's `artifact_paths` is not used:
  the logs are not a release artifact.
```

### `scripts/tests/setup` (submodule)

Pinned to `147c4d2b3bb6c551e9c8a33acd615ad9c1eb0518`, the head of
qdb-test-setup PR 5, in commit 2. Re-pinned to PR 5's squash commit on
master in commit 7, before the merge stage: the PR branch is deleted at
merge and its head may stop being fetchable.

### `docs/log.md`

Current state: Next item 1 leaves; item 2 loses the sentence "The cause is
unknown until item 1 gives us a daemon log" and becomes startable as
written. Last updated date set. One entry:

```
## 2026-10-07 -- CI uploads qdbd's logs with the test report; ci-qdbd-logs-plan.md deleted

- Owner decisions: the archive is made inside the test command and
  uploaded through the test-report plugin, never `artifact_paths`; the
  flush interval and the console files are qdb-test-setup's (PR 5). The
  rules went to `scripts/cicd/AGENTS.md` and `.buildkite/AGENTS.md`.
```

The entry is written when the plan is deleted, in the merge stage, not in
this unit's commits. The Current state change is commit 6.

## Rationale

| decision                                                                | why                                                                                                                              | rejected, and why                                                                                                | gained                                       | given up                                         | settled by                                                                  |
| ----------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | -------------------------------------------- | ------------------------------------------------ | --------------------------------------------------------------------------- |
| Archive inside the test command through an EXIT trap                    | Buildkite runs the repository's post-command and pre-exit hooks after the plugin's post-command upload                           | a repository post-command or pre-exit hook: its archive lands after the upload (build 90 job log)                | the archive exists when the plugin uploads   | a trap in every test script of the step          | Buildkite hooks documentation; owner, 2026-10-07                            |
| Upload through the plugin's `job.artifacts`, not `artifact_paths`       | the logs belong with the test report, not in the release artifact tab; quasardb's test step does the same                        | `artifact_paths`: a regular artifact for something that is not one                                               | one place to look for a job's test evidence  | nothing                                          | owner, 2026-10-07                                                           |
| qdbd keeps running; the trap archives live logs                         | stopping is a SIGKILL that flushes nothing, and `40.test-e2e.sh` is planned to run against the same daemons after the Go tests   | stop in the trap: no flush gained and the later e2e step loses its daemons                                       | composes with later test scripts in the step | the last flush interval of log lines, now 100 ms | `utils.sh` `kill_instances`; `docs/e2e.md`, In Buildkite; owner, 2026-10-07 |
| The flush interval and the console files are the submodule's            | every API and tool pins the same submodule, so one change aligns them all                                                        | a second artifact glob for `qdbd_log_*.txt` here, and a flag in this repo's step: project-local, nothing aligned | one archive shape for every consumer         | a dependency on PR 5 landing                     | owner, 2026-10-07; qdb-test-setup PR 5                                      |
| Reuse `cleanup.sh::archive` instead of a second tar                     | one home for how the daemon logs are archived; the glob also matches the pre-exit hook's archives                                | a tar in `00.common.sh`: a second shape to keep in step with the submodule's                                     | zero archive logic in this repo              | sourcing `config.sh` in a subshell               | proposal                                                                    |
| The helper is one trap function in `00.common.sh`                       | the only caller is a trap; shared helpers live in `00.common.sh` so `40.test-e2e.sh` can install the same trap                   | a separate archive function plus a trap wrapper: two names for one use                                           | one line per test script                     | nothing                                          | `scripts/cicd/AGENTS.md`, Contract                                          |
| Pin PR 5's head now, re-pin to the squash commit before the merge stage | the owner wants to debug the windows-core2 death now, before PR 5 lands; the squash-merge deletes the branch that holds the head | wait for the merge: the investigation waits with it                                                              | the first build with complete logs today     | one more commit and one more build               | owner, 2026-10-07                                                           |
| The pre-exit hook's duplicate archive stays                             | removing it means editing the submodule, which this repo never does; the duplicate is written after the upload and gitignored    | skip `cleanup` in the hook: a local copy of `stop-services.sh`                                                   | no submodule change                          | two unused tarballs per job on the agent         | root `AGENTS.md`, Sub-folders                                               |

## Knowledge

- Commit 2 (submodule bump): no prose; the submodule carries its own.
- Commit 3 (`00.common.sh`): the function's comment block and the
  narrative above carry the hook-order reason (evidence: Verified facts,
  first item), the live-archive reason (`kill_instances`, flush interval)
  and the subshell reason (`config.sh` runs `set -xe` and parses
  arguments). `scripts/cicd/AGENTS.md` gains the Contract bullet.
- Commit 4 (`30.test.sh`): the header sentence and the trap comment; no
  new reason.
- Commit 5 (`_build.yml`, `pre-exit`, `.buildkite/AGENTS.md`): the
  template comment and the Facts paragraph carry the quasardb precedent,
  the `artifact_paths` rejection and the hook-order fact; the hook
  comment says its archive is unused.
- Commit 6 (`docs/log.md`): Current state only.
- Rationale rows with no code home: "pin PR 5's head now, re-pin before
  merge" lives in commit 7's subject and in this plan only; it dies with
  the plan, which is right, since nothing remains to defend once the
  squash commit is pinned.

## How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write every commit's comments and `AGENTS.md` rows from the Design
   and Knowledge sections, in the same commit as the code.
3. A why that arises while building and is not in this plan is written
   where it is decided, with its evidence, or asked of the owner through
   the question tool before the commit.
4. After commit 6, run `/doc-discipline all scripts/cicd .buildkite`
   and make one small commit per finding.
5. Before the build-stage message, run
   `/doc-discipline check scripts/cicd .buildkite docs/ci-qdbd-logs-plan.md`
   and fix every "Unlanded from the plan" finding with a small commit, or
   report why it was dropped.
6. After every `.buildkite` change, run `python3 .buildkite/pipeline.py check`
   from a venv with `.buildkite/requirements.txt` and `BUILDKITE_BRANCH`
   set.

## Commits

1. `docs(plan): ci-qdbd-logs-plan.md, the build step uploads qdbd's logs with the test report`
2. `build(deps): bump qdb-test-setup to the head of PR 5, flush interval and console files`
3. `ci(cicd): cicd_archive_qdbd_logs_on_exit archives both daemons' logs through the submodule's cleanup.sh`
4. `ci(cicd): 30.test.sh archives the qdbd logs at exit, whatever the tests' outcome`
5. `ci(buildkite): the build step uploads the qdbd log archives with the test report`
6. `docs(log): CI captures qdbd's logs; the windows-core2 qdbd death is next`
7. `/doc-discipline all scripts/cicd .buildkite`, one small commit per finding; then `/doc-discipline check scripts/cicd .buildkite docs/ci-qdbd-logs-plan.md`.
8. `build(deps): bump qdb-test-setup to the squash commit of PR 5`, as soon as the squash commit exists; before the merge stage in every case.
9. Verify: push `sc-19567/rr-ci-qdbd-logs`, build its head in Buildkite (API-created, branch-filter bypass, full SHA; `.buildkite/AGENTS.md`), wait for the result. Green: report the build number and confirm one job's report page links "qdbd logs" with both archives. Red: fix with further small commits on this branch, push, build again. The Verify step runs after commit 6 for the owner's debugging and again after commit 8.

## Open questions and recommendations

1. Should the windows-core2 job of the first green build be inspected in this unit, or does the daemon log go straight to the next unit? Recommendation: this unit only confirms the archives are linked; reading them is the next unit.
