# The round trip in Buildkite -- Plan

Status: draft. This plan is the unit that runs the v2 round trip in every
platform's build step and closes M2. It is unit 4 of
`docs/e2e-v2-round-trip-plan.md`, "Units under this plan", and it is
deleted when the unit lands (`docs/AGENTS.md`, Plans).

## Outcome

When the unit lands:

- `scripts/cicd/40.test-e2e.sh` runs `make -C tests/e2e test-round-trip`
  after the Go tests in every platform's build step, against the qdbd
  `start-services.sh` started and the binary `20.build.sh` built, and a
  green build on all eight platforms is M2's exit (`docs/log.md`, M2
  criteria).
- The harness builds `e2etool` into the repository's `bin/` next to
  `qdb_rest`, so one directory holds every cgo binary the round trip
  runs, and the Windows DLL co-location is one copy.
- Every document that says the harness is not in CI says it is.
- `docs/e2e-round-trip-plan.md` and `docs/e2e-v2-round-trip-plan.md` are
  deleted, their surviving decisions moved to `docs/e2e.md` as the dated
  decision logs of 2026-10-11 and 2026-10-12.
- `docs/log.md` marks M2 done, with M3 not started and its criteria
  defined by the unit that starts it.

It leaves for later units:

- `make load seed test-v1` in the step, which joins when the v1 wrappers
  land (`docs/e2e.md`, "In Buildkite"; ADR-0013 6).
- The stress, which joins with the resilience milestone.
- Stopping REST servers from `hooks/pre-exit`: the owner keeps the
  Makefile's trap as the one stop (Rationale), so the hook is unchanged.

## Verified facts

- Build 125 passed on `d9fffd1`, the base head, on all eight platforms
  (`bk api pipelines/qdb-api-rest/builds/125`, session of 2026-10-12), so
  the round trip unit landed and its plan is stale.
- The build step runs `start-services.sh`, `20.build.sh`, `30.test.sh` in
  that order from the repo root (`.buildkite/steps/_build.yml:9-15`), and
  `pre-exit` stops qdbd (`.buildkite/hooks/pre-exit`).
- `start-services.sh` resolves its paths relative to the working
  directory: the key files `user_private.key` and `cluster_public.key`,
  the data and log directories `insecure/log` and `secure/log`, and the
  console logs `qdbd_log_<cluster>.{out,err}.txt`
  (`scripts/tests/setup/config.sh:12-29`, `.gitignore:3-12`). In CI that
  directory is the repo root, so the harness's `$(REPO_ROOT)/user_private.key`
  (`tests/e2e/Makefile:79-80`) is the file the Go tests already read.
- The step scripts source `00.common.sh`, call `cicd_trust_workspace`,
  `cicd_setup_go_toolchain`, `cicd_setup_cpu_baseline`,
  `cicd_assert_qdb_tree`, `cicd_setup_qdb_env` in that order
  (`scripts/cicd/30.test.sh:17-24`). `cicd_setup_go_toolchain` prepends
  `${GOROOT}/bin` to PATH (`00.common.sh:62-63`), which on Windows is a
  Windows-format entry the MSYS shell that `mingw32-make` spawns cannot
  parse, so a Makefile's bare `go` is not found there
  (`~/git/qdb-nats-connector/scripts/cicd/50.test-e2e.sh`, the cygpath
  block).
- The harness Makefile invokes a bare `go` after sourcing `.envrc`
  (`tests/e2e/Makefile:213-215`), needs GNU make (`tests/e2e/Makefile:2`),
  and sets `SHELL := bash` (`:16`).
- `E2ETOOL` is defined once (`tests/e2e/Makefile:62-63`), used by the
  `e2etool` target, the driver's `--tool` and `clean` (`:215-216`, `:241`,
  `:247`), and `.gitignore:23-24` ignores it at its current path. `bin/` is
  ignored whole (`.gitignore:14`).
- `test-round-trip` starts one server per cluster from `QDB_REST_BIN` and
  traps their stop on exit (`tests/e2e/Makefile:223-241`); `start_server`
  and `stop_server` work through pidfiles `tests/e2e/rest.pid` and
  `rest-secure.pid` with logs next to them (`tests/e2e/common.sh:134-162`,
  `Makefile:52-53`, `:81-82`).
- The driver needs `curl`, `jq`, `cmp` and `gunzip`
  (`tests/e2e/round-trip.sh:18-21`); the nats-connector e2e runs on the
  same agents with `jq` and `gunzip` (`docs/e2e.md:370-371`,
  `~/git/qdb-nats-connector/examples/finance-ohlc.sh`).
- On Windows the Buildkite service context skips the PATH-based DLL
  search, so a cgo binary launched by the harness must have the qdb DLLs
  in its own directory (`~/git/qdb-nats-connector/scripts/cicd/50.test-e2e.sh`,
  the DLL block, NTSTATUS 0xC00000BE). `qdb_rest.exe` is built into
  `bin/` (`scripts/cicd/20.build.sh:54-61`).
- `20.build.sh` and the Makefile's `e2etool` target both pass
  `-mod=vendor -buildvcs=false` (`20.build.sh:46-61`, `Makefile:209-215`).
- Three places say the harness is not in CI: `.buildkite/pipeline.py:12-13`,
  `.buildkite/steps/_build.yml:4-7`, `.buildkite/hooks/pre-exit:5-6`;
  `.buildkite/AGENTS.md:35-38` says it joins when its script exists.
  `scripts/cicd/AGENTS.md:16-19` says the per-platform scripts call `${GO}`
  instead of make.
- `docs/e2e.md:359-371` ("In Buildkite") already specifies the step: after
  the Go tests, `make test-round-trip` first, GNU make discovery, the
  Windows DLL co-location, a log dump on failure.
- `docs/e2e.md` has decision logs through 2026-09-18 (`:423-487`) and
  none for 2026-10-11 or 2026-10-12; the plans hold those tables
  (`docs/e2e-v2-round-trip-plan.md:760-770`,
  `docs/e2e-round-trip-plan.md:437-450`). The 2026-09-18 table of the
  umbrella plan (`:745-758`) is already in `docs/e2e.md:473-487`.
- `docs/log.md`: the M2 row (`:14`), the M2 criteria (`:25-30`), In flight
  (`:37-39`), Next (`:41-53`), Handoff to M2 (`:55-65`).
- Pipeline changes are checked with `python3 pipeline.py check` under
  `BUILDKITE_BRANCH` (`.buildkite/AGENTS.md:10-13`).

## Design

In the order the files have. The sketches carry the shape and the
claims; the build stage writes the prose with the code in view.

### `tests/e2e/Makefile`

```make
# The Go tool of the v2 round trip is built into the repository's bin/,
# next to qdb_rest, so every cgo binary the round trip runs sits in one
# directory and the Windows DLL co-location of the CI step is one copy
# [Rationale: bin]. It gains .exe on Windows like every binary the
# harness runs [unchanged].
E2ETOOL := $(REPO_ROOT)/bin/e2etool$(EXE_EXT)
```

The `e2etool` target gains a `mkdir -p $(REPO_ROOT)/bin`, because the
directory exists only once something was built there [`20.build.sh:52`].
`E2ETOOL_DIR` leaves if nothing else reads it. The help line and the
README's `make e2etool` line say where the tool lands.

### `.gitignore`

```
- the two tests/e2e/tools/e2etool/e2etool lines leave, because bin/ is
  ignored whole [.gitignore:14]
```

### `scripts/cicd/40.test-e2e.sh`

```bash
# This is the Buildkite e2e step for qdb-api-rest. .buildkite/steps/_build.yml
# invokes it after 30.test.sh. It runs the v2 round trip (docs/e2e.md,
# "In Buildkite") through the harness Makefile against the qdbd
# start-services.sh started and the binary 20.build.sh built, and it
# runs on every platform. [docs/e2e.md:361-366]
#
# style: file header as contract + numbered sections as steps
# because: five things happen in an order that matters, each with its
#   own platform reason, and the file is a script with no function
#
# 1. the preamble of 30.test.sh: trust the workspace, Go toolchain, cpu
#    baseline, qdb tree, cgo environment, because the harness compiles
#    e2etool through cgo and runs cgo binaries [scripts/cicd/AGENTS.md:28-33]
# 2. on Windows, prepend the Go directory in unix form, because the
#    harness Makefile invokes a bare go after sourcing .envrc and the
#    MSYS shell cannot parse the Windows-format entry the toolchain
#    function prepended [Verified facts; nats 50.test-e2e.sh cygpath block]
# 3. find GNU make: make, gmake on FreeBSD, mingw32-make on Windows,
#    because the harness Makefile needs GNU make and the binary's name
#    varies by platform; fail naming the candidates
#    [tests/e2e/Makefile:2; nats 50.test-e2e.sh make block]
#    - this is the one step script that uses make off Linux; the Go-direct
#      rule is for compiling, and the harness's lifecycle lives in its
#      Makefile [Rationale: make]
# 4. on Windows, copy the qdb DLLs from qdb/bin and qdb/lib into bin/,
#    because the service context skips the PATH-based DLL search and
#    both cgo binaries the round trip runs live in bin/
#    [Verified facts; Rationale: bin]
# 5. run make -C tests/e2e test-round-trip QDB_REST_BIN=bin/qdb_rest[.exe];
#    the Makefile starts one server per cluster, builds the tool and stops
#    the servers on exit [tests/e2e/Makefile:223-241]; on failure dump
#    the logs and exit 1 [dump_e2e_failure_logs]
```

```bash
# dump_e2e_failure_logs: the REST servers' logs and qdbd's console and
# structured logs, because the driver prints one failing assertion and
# the server side is never surfaced in the build output otherwise.
# [docs/e2e.md:369-370; nats 50.test-e2e.sh dump function]
dump_e2e_failure_logs() {
	# style: doc comment alone
	# because: a fixed list of tails
	#
	# at the paths: tests/e2e/rest.log and rest-secure.log [Makefile:53, :82];
	#   qdbd_log_<cluster>.err.txt and <cluster>/log/ at the repo root,
	#   because start-services.sh ran there [config.sh:18-29]
}
```

### `.buildkite/steps/_build.yml`

```yaml
# the comment's "not in CI" sentence becomes: the e2e harness runs
# last, through scripts/cicd/40.test-e2e.sh [docs/e2e.md:361]
commands:
  - echo \"+++ Tests - e2e\"
  - bash scripts/cicd/40.test-e2e.sh
```

### `.buildkite/pipeline.py`

```
- docstring lines 12-13: the e2e harness runs in the build step after
  the Go tests (.buildkite/AGENTS.md) [docs/e2e.md:361]
```

### `.buildkite/hooks/pre-exit`

```
- the comment's last sentence: the REST servers the harness starts are
  stopped by the harness Makefile's own trap, so the hook stops qdbd
  alone [Rationale: trap]
```

### `.buildkite/AGENTS.md`, Facts

```
- the e2e bullet: the harness runs in the build step after the Go tests
  through scripts/cicd/40.test-e2e.sh (docs/e2e.md, "In Buildkite"); the
  "until its step script exists" clause leaves [docs/e2e.md:361]
```

### `scripts/cicd/AGENTS.md`, Contract

```
- after the 10.lint.sh bullet: 40.test-e2e.sh is the one per-platform
  script that uses make, GNU make found by platform name, because the
  harness's server lifecycle lives in tests/e2e/Makefile and the
  Go-direct rule is about compiling [Rationale: make]
- a bullet: on Windows the step co-locates the qdb DLLs into bin/,
  where qdb_rest and e2etool are built, because the service context
  skips the PATH-based DLL search [Verified facts]
```

### `docs/e2e.md`

```
- "In Buildkite": every sentence checked against the script; the DLL
  sentence says "into bin/, next to qdb_rest and e2etool"
- "The tool" and "Layout": e2etool is built into bin/ [Makefile]
- a "Decision log (2026-10-11)" table: the six rows of
  docs/e2e-v2-round-trip-plan.md:760-770, moved as they are
- a "Decision log (2026-10-12)" table: the rows of
  docs/e2e-round-trip-plan.md:437-450 in the three-column form, decision,
  why, rejected; "settled by" folds into the why ("owner, 2026-10-12")
- a "Decision log (2026-10-12, CI)" table or three more rows of the
  same table: Rationale rows bin, make, trap
```

### `tests/e2e/README.md`, `tests/e2e/AGENTS.md`

```
- README usage: make e2etool builds the tool into bin/
- AGENTS.md v2 bullet: "built by make e2etool into the repository's bin/"
```

### `docs/log.md`, Current state

```
- M2 row: done, "closed <date of the green build>"
- M2 criteria paragraph leaves; M4 criteria stay
- Next: item 1 struck; the bench run becomes item 1, the upstream filing
  item 2
- Handoff to M2: the block is deleted whole, because M2 is done; the
  symtable fact is tables_test.go's already, and the owner dropped the
  "make load" and wall-clock bullets rather than re-homing them under
  M4 (owner, 2026-10-12, plan review)
- one entry: the round trip runs in Buildkite (build N), M2 closed, the
  two plans deleted with facts moved to docs/e2e.md
```

## Rationale

| decision                                                          | why                                                                                                                  | rejected, and why                                                                                                      | gained                                         | given up                                                    | settled by                                 |
| ----------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------ |
| bin: the harness builds e2etool into the repository's `bin/`      | the owner allows harness changes, and one directory for both cgo binaries makes the Windows DLL co-location one copy | overriding `E2ETOOL` on the CI command line, because it hides the layout in the step; DLLs in the tool's source dir    | one co-location, no untracked DLLs in the tree | the tool no longer sits next to its source                  | owner, 2026-10-12                          |
| make: 40.test-e2e.sh calls the harness Makefile through GNU make  | the Makefile is the harness's only entry point and owns the server lifecycle                                         | driving round-trip.sh from the step, because it duplicates the Makefile's start, trap and stop                         | one lifecycle                                  | one per-platform script uses make, an exception to the rule | `tests/e2e/AGENTS.md:7-9`                  |
| trap: the Makefile's trap is the one stop of the REST servers     | the owner knows the scope exactly, and start and stop stay close together                                            | stopping through `hooks/pre-exit` by pidfile, because it splits the lifecycle across two files for a timeout-kill case | one place to read                              | a step killed by timeout can leave a server on the agent    | owner, 2026-10-12                          |
| the step runs after the Go tests, inside the build step           | ADR-0013 6 and `docs/e2e.md`, "In Buildkite" place it there                                                          | a separate pipeline step, because it would rebuild and restart qdbd                                                    | no second build                                | a failing round trip fails the whole platform step          | `docs/e2e.md:361-366`                      |
| M2 closes in this unit's log commit, M3 stays not started         | the green build is M2's exit as the log defines it, and criteria are written when a milestone starts                 | starting M3 here, because its criteria are an owner decision of their own                                              | M2 closed on CI evidence                       | the next seed starts M3                                     | owner, 2026-10-12                          |
| both e2e plans are deleted here, decisions moved to `docs/e2e.md` | the round-trip plan is stale (build 125 passed) and the umbrella plan's last unit is this one; the owner confirmed   | a separate deletion unit, because it would be two commits with no code                                                 | no dangling plan                               | a longer doc tail in this unit                              | owner, 2026-10-12; `docs/AGENTS.md:95-104` |

## Knowledge

- Commit 2 (bin): the bin row lands in the Makefile comment above
  `E2ETOOL`, the README line and the AGENTS.md bullet; `.gitignore`.
- Commit 3 (the script): the make row lands at step 3's comment; the
  Windows PATH and DLL facts at steps 2 and 4; the step order in the
  header.
- Commit 4 (the dump): the dump function's doc and path notes.
- Commit 5 (the pipeline): `_build.yml`, `pipeline.py`, `pre-exit`; the
  trap row lands in the `pre-exit` comment.
- Commit 6 (AGENTS.md): the make row and the DLL fact in
  `scripts/cicd/AGENTS.md`; the e2e bullet in `.buildkite/AGENTS.md`.
- Commit 7 (`docs/e2e.md`): "In Buildkite" reconciled; the 2026-10-11
  and 2026-10-12 decision logs; the bin, make and trap rows as decision
  rows, so every Rationale row above has a permanent home.
- Commit 8 (plans deleted): nothing new; the moves of commit 7 make it
  safe.
- Commit 9 (log): M2 done, Next, Handoff, one entry; the M2 and plan
  rows land here.

## How the knowledge lands

1. Run `/doc-discipline read` before the first code commit.
2. Write every commit's comments and document rows from the
   specifications in Design and Knowledge, in the same commit as the
   code, so that every listed claim appears once, at the place the
   specification names, no claim is added without evidence, and a claim
   the code contradicts is reported in the build-stage message and not
   written.
3. A why that arises while building and is not in this plan is written
   where it is decided, with its evidence, or asked of the owner through
   the question tool before the commit that needs it.
4. After the last code commit, run
   `/doc-discipline all scripts/cicd .buildkite tests/e2e/Makefile docs/e2e.md`
   and land one small commit per finding.
5. Before the build-stage message, run
   `/doc-discipline check scripts/cicd .buildkite tests/e2e/Makefile docs/e2e.md docs/ci-e2e-plan.md`,
   which reconciles the code against this plan and reports every reason
   the plan promised that the code and documents do not state.

## Commits

1. `docs(plan): ci-e2e-plan.md, the round trip runs in every build step and closes M2` (this document)
2. `build(e2e): make e2etool builds the tool into bin next to qdb_rest`
3. `ci(cicd): 40.test-e2e.sh runs make test-round-trip against the built binary with GNU make found per platform`
4. `ci(cicd): 40.test-e2e.sh dumps the REST servers' and qdbd's logs when the round trip fails`
5. `ci(buildkite): the build step runs the e2e after the Go tests`
6. `docs(agents): .buildkite and scripts/cicd AGENTS.md say the round trip is in the build step and 40.test-e2e.sh uses GNU make`
7. `docs(e2e): "In Buildkite" matches the step, and the 2026-10-11 and 2026-10-12 decision logs join e2e.md`
8. `docs: e2e-round-trip-plan.md and e2e-v2-round-trip-plan.md deleted; facts moved to e2e.md`
9. `docs(log): the round trip runs in Buildkite; M2 closed; both e2e plans deleted`
10. `/doc-discipline all scripts/cicd .buildkite tests/e2e/Makefile docs/e2e.md`, one small commit per finding, then `/doc-discipline check scripts/cicd .buildkite tests/e2e/Makefile docs/e2e.md docs/ci-e2e-plan.md`.
11. Verify: push `sc-19567/rr-ci-e2e`, build its head in Buildkite (`.buildkite/AGENTS.md`, Facts: API-created, branch filters bypassed, the full 40-character SHA), wait for the result. Green: report the build number, and write it into commit 9's log entry if commit 9 landed before the build (a further `docs(log)` commit). Red: fix with further small commits on this branch, push, build again.

Commit 9 is written after the build is green, so the log names a build
that passed; if the build goes red and is fixed, the number is the
passing one.

## Open questions and recommendations

none. The one question of the first revision (the Handoff to M2 bullets
about `make load`) was answered "delete" in the plan review of
2026-10-12 and is settled in the Design of `docs/log.md`.
