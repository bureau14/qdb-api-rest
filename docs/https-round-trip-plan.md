# HTTPS round trip and the rr skills on the QuasarDB workflow

Thread: rr: round trip over HTTPS
Status: approved

## Outcome

When the unit lands, the v2 round trip runs every step on both HTTP
planes of each server. The run order is insecure over HTTP, insecure
over HTTPS, secure over HTTP and secure over HTTPS. Each run covers
login, create, list, table, ingest, the read, query and DuckDB matrix,
ADBC and JDBC, the tag steps and the metrics scrape. A TLS fault on any
HTTPS route fails `make test-round-trip` in CI. The log's open item
"The round trip drives both servers over plain HTTP" is removed.

The rr skills and the brief's Tracking section describe the QuasarDB
workflow. A unit has a Shortcut story under epic 19567, a branch
`sc-<id>/<kebab-title>` off `origin/master`, and a `QDB-<id> - <title>`
pull request reviewed by Igor. It lands by squash-merge and a
cherry-pick onto `3.14.x`. The rr stages keep the seed, the plan review
in the Inbox, the guided code review and the owner's invocation as the
gate.

Left for later: the Flight case of the stress (`docs/log.md`, Open
after the milestones), certificate verification on any plane, and an
HTTPS step in the stress.

## Verified facts

- The HTTPS listener serves the same handler as the HTTP listener
  (`cmd/qdb_rest/main.go:157`, `:170`). Its requests log
  `observe.PlaneHTTP` (`internal/httpapi/query.go:84`), so
  `QUERY_LINES` (`tests/e2e/round-trip.sh`, `QUERY_LINES`) holds on
  both planes unchanged. Checked 2026-11-16.
- Every e2e server binds HTTPS on a test port: 40092 for the insecure
  server and 40093 for the secure one (`tests/e2e/Makefile:38-43`,
  `:80-88`). No server sets `listen.tls.*`, so each serves the
  ephemeral self-signed certificate (ADR-0001, Decision 2).
- The JDBC tool already turns verification off for the same reason
  (`tests/e2e/tools/jdbc/src/main/java/com/quasardb/e2e/Main.java:48`).
- `create_table` deletes the table before it creates it, and treats a
  404 as clean (`tests/e2e/common.sh:276-284`). `remove_tag` clears the
  fixed tag `e2e_tag` first (`tests/e2e/round-trip.sh:203-217`). So two
  sequential runs on one cluster can reuse the same table and tag
  names.
- `run_cluster` takes the run's name and writes to
  `actual/round-trip/<name>/` (`tests/e2e/round-trip.sh`,
  `run_cluster`, `out=`). The name appears in every log line it
  prints.
- `request` in `common.sh` is shared by `round-trip.sh`, `stress.sh`
  and `compare-v1.sh` (`tests/e2e/*.sh`, `source .../common.sh`).
  curl ignores `-k` on an `http://` URL.
- `main` reports "$failed of 6 checks" (`round-trip.sh`, `main`).
- Every rr file that names `sc-19567/rr-`, a fast-forward or
  `--ff-only`: `.claude/skills/rr-workflow/SKILL.md`,
  `rr-plan/SKILL.md`, `rr-plan/plan-shape.md`, `rr-build/SKILL.md`,
  `rr-merge/SKILL.md`, `rr-start/SKILL.md`, `rr-start-next/SKILL.md`,
  `rr-seed/report.md`, and `docs/brief.md:16`. The dated entry in
  `docs/log.md:413` is history and is not edited.
- Story 20185 tracks this unit. Its branch is
  `sc-20185/rest-https-round-trip-and-quasardb-workflow-in-rr-skills`
  (2026-11-16).

## Design

### tests/e2e/common.sh

```bash
# request <out_dir> <name> <curl args...>: one request. Its body goes to
# <out_dir>/<name>.body, its headers to <out_dir>/<name>.headers, and its
# status to stdout. [existing contract, common.sh:194-200]
request() {
    # style: doc comment + inline notes
    # because: the existing block explains -g, --compressed and the header dump, and -k is one more flag a reader would ask about
    #
    # the existing reasons for -g, the missing --compressed and the header dump stay [common.sh:194-200]
    # -k: every e2e server serves HTTPS with the self-signed certificate it generates at start [ADR-0001, Decision 2]
    # -k: the Flight steps turn verification off for the same reason [Main.java:48]
    # -k: curl ignores it on an http:// URL, so one flag serves both planes and the stress [Rationale 4]
}
```

The build rewrites the whole comment block above `request`.

### tests/e2e/round-trip.sh

```bash
# File header: the round trip drives the built binary like a client.
# Each server runs it once on each HTTP plane, over HTTP and over HTTPS,
# and every step runs on both planes. [Rationale 1, 2]
#   round-trip.sh run --insecure-http <url> --insecure-https <url> --insecure-flight <url>
#     --secure-http <url> --secure-https <url> --secure-flight <url> ... (rest unchanged)
# style: file header comment
# because: the header is the driver's map, and it states today that no step reads HTTPS
#
# each plane is a URL in the form its client takes: http:// and https:// for curl, grpc+tls:// for ADBC [round-trip.sh:15-18]
# the sentence "The HTTPS listener has no option, because no step reads it" is deleted [round-trip.sh:19-20]
# the working directory is actual/round-trip/<cluster>-<plane>/ [run_cluster out=]
```

The build rewrites the header block whole.

```bash
INSECURE_HTTPS="" SECURE_HTTPS=""   # joined to the existing globals line
usage()        # the usage string gains --insecure-https and --secure-https
parse_args()   # two case arms. Both URLs are required, as the http URLs are
    # style: doc comment alone (unchanged)
    # because: two more arms of the same option loop need no comment
```

```bash
# run_cluster <name> <url> <flight_url> <login_body_file> <qdbsh cluster
# options...>: the round trip against one HTTP plane of one server, named
# <cluster>-<plane>. The steps are unchanged. [Rationale 2]
run_cluster() {
    # style: doc comment alone (body unchanged)
    # because: only the doc comment's first sentence changes, from one server to one plane of one server
}
```

```bash
# main: runs run_cluster four times, on both planes of each server, then
# the warning and query-line steps once per server.
main() {
    # style: inline notes
    # because: the two existing reasons for the warning steps stay, and the run order needs one
    #
    # runs: insecure-http, insecure-https, then secure-http, secure-https [Rationale 2]
    # the runs are sequential, because they share table names and the tag on one cluster [Rationale 3, common.sh:276-284]
    # the warning and query-line steps read each server's log once, after both of its planes ran [round-trip.sh check_log]
    # the failure message counts 8 checks: four runs, two warning steps, two query-line steps
}
```

### tests/e2e/Makefile

```make
# The comment above ROWS ... REST_URL_SECURE gains HTTPS: each plane the driver
# reads is a URL in the form its client takes, http:// and https:// for curl.
HTTPS_URL_INSECURE ?= https://127.0.0.1:$(HTTPS_PORT)
HTTPS_URL_SECURE   ?= https://127.0.0.1:$(HTTPS_PORT_SECURE)
# test-round-trip passes --insecure-https $(HTTPS_URL_INSECURE) --secure-https $(HTTPS_URL_SECURE)
# style: the existing block comment, rewritten whole
# because: the HTTPS URLs follow the FLIGHT_URL_* pattern, so a server given by URL gives its HTTPS URL the same way [Makefile:76-78, Rationale 5]
```

### docs/e2e.md

```text
Purpose: "driven over HTTP like a client" becomes "over HTTP and HTTPS". [docs/e2e.md:15]
The v2 round trip: "runs once per cluster" becomes "runs once per cluster and HTTP plane, four runs". [docs/e2e.md:62-66]
- each run reuses the table names and the tag, because the runs are sequential and the create drops the table first
The driver: the synopsis gains both --*-https options, "http:// and https:// for curl". [docs/e2e.md:232-240]
- "The HTTPS listener has no option, because no step reads it" is deleted [docs/e2e.md:243]
- "Working files go to actual/round-trip/<cluster>-<plane>/" [docs/e2e.md:252]
- curl does not verify the certificate, as the Flight drivers do not [Rationale 4]
```

### tests/e2e/AGENTS.md

```text
- "the startup warning of a secure cluster behind plain HTTP": unchanged, the check stays once per server [tests/e2e/AGENTS.md:48-50]
- one new clause in the round-trip bullet: the driver runs every step on both HTTP planes of a server, so a new step needs no plane-specific code
```

### The rr skills

Each skill file is rewritten as if the QuasarDB workflow had always
been the workflow. These are the rows of each file:

```text
rr-workflow/SKILL.md, Branches and commits:
- a unit has a Shortcut story under epic 19567, created at /rr-plan with the shortcut skill's sc-story.sh [Rationale 6, 7]
- its branch is sc-<id>/<kebab-title>, off origin/master, the name sc-story.sh prints
- commits stay small, one line each, type(scope): subject
- the unit lands as one pull request, QDB-<id> - <story title>, to master, reviewed by Igor, with an empty body
- /rr-merge squash-merges it as "<PR title> (#N)" and cherry-picks the squash commit onto 3.14.x
- the code review's patch is git diff origin/master...HEAD
rr-workflow/SKILL.md, Tools: the Branch row names the story branch, and MESSAGE stays the plan slug. The table gains the quasardb-workflow, github and shortcut skills as the owners of the story, PR, merge and cherry-pick mechanics.
rr-workflow/SKILL.md: the Correct/Incorrect pair that forbids quasardb-workflow is replaced by one where the build stage opens the PR through the quasardb-workflow skill's step 4.
rr-workflow/SKILL.md, Lifecycle table: Plan produces the story, the branch, the plan commit and its review. Build adds the PR. Merge is the squash-merge, the cherry-pick and the builds on both lines.
rr-plan/SKILL.md: step 1 creates the story and the branch. The entry check lists 'sc-*' branches.
rr-plan/plan-shape.md: the Verify step pushes the story branch and opens the PR.
rr-build/SKILL.md: the branch names the plan through its story. Push, open the PR (quasardb-workflow step 4), build and guided review run side by side. The build-stage message names the PR.
rr-merge/SKILL.md: the entry check is a passed build of this head, the PR approved by Igor or Edouard, and no plan in the tree. The steps are quasardb-workflow step 7: squash-merge, cherry-pick onto 3.14.x, push, builds on both lines, story Completed. The closing line names the squash commit and the 3.14.x commit.
rr-start/SKILL.md and rr-start-next/SKILL.md: a merged unit is one "QDB-<id> - ... (#N)" commit on master and its cherry-pick on 3.14.x. A leftover is an sc-* branch whose PR has merged, a dirty tree, or a plan on the base.
rr-seed/report.md: the Workflow line names the story branch, created at /rr-plan.
```

### docs/brief.md, Tracking

```text
- "Feature branches keep the epic's id, as sc-19567/rr-<slug>" becomes: each unit is a story of the epic, and its branch is sc-<id>/<kebab-title> [docs/brief.md:16]
```

## Rationale

| decision                                                                | why                                                                                               | rejected, and why                                                                                                                                  | gained                               | given up                                             | settled by                                        |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ | ---------------------------------------------------- | ------------------------------------------------- |
| 1. Every step runs on both HTTP planes                                  | the owner wants the full suite on HTTPS                                                           | reads only (medium), because the write steps would go untested over TLS                                                                            | one parametrized suite               | about twice the CI time of the round trip            | owner, 2026-11-16                                 |
| 2. One `run_cluster` call per plane                                     | the owner noted it is less code than looping planes inside the steps                              | a plane loop inside `run_cluster`, which touches every step's call                                                                                 | no step changes                      | ADBC and JDBC run twice per server                   | owner, 2026-11-16                                 |
| 3. Same table and tag names on both planes                              | sequential runs, a create that deletes the table first, and a tag that is removed first           | names per plane (`e2e_<fmt>_<plane>`), which would change the create, list and tag steps                                                           | no naming change                     | the HTTP run's tables are gone after the HTTPS run   | proposal                                          |
| 4. curl runs with `-k` in `request`                                     | the servers serve an ephemeral self-signed certificate, and the Flight steps do not verify either | a test certificate with `--cacert`, which the owner did not take. Passing `-k` only for HTTPS URLs, which needs a scheme test in the shared helper | no fixture                           | the configured-pair path stays untested end to end   | owner, 2026-11-16 (`-k`), proposal (in `request`) |
| 5. `HTTPS_URL_*` variables like `FLIGHT_URL_*`                          | a server given by URL needs a way to give its HTTPS URL                                           | deriving it from the HTTP URL, which cannot know the port                                                                                          | the same pattern as Flight           | two more Makefile variables                          | proposal                                          |
| 6. The rr skills describe the QuasarDB workflow                         | the owner chose it for this unit and asked for the skills to be corrected in it                   | keeping the rr fast-forward process, which no merge since PR #60 followed                                                                          | skills match the history             | none                                                 | owner, 2026-11-16                                 |
| 7. Stories go under epic 19567                                          | the owner picked "a story under epic 19567" for this unit                                         | the release epic that `sc-release-epic.sh` picks                                                                                                   | the rewrite's work stays in one epic | the skills override quasardb-workflow's default epic | proposal                                          |
| 8. Merge needs Igor's or Edouard's approval and the owner's `/rr-merge` | quasardb-workflow makes approval the reviewer's job, and the rr gate is the owner's invocation    | `/rr-merge` alone, which merges without the PR review                                                                                              | both reviews hold                    | a merge can wait on Igor                             | proposal                                          |

## Knowledge

- Commit 2: Rationale 4 lands as inline comments at `request`.
- Commit 3: Rationale 5 lands in the Makefile's block comment. The
  driver's header and `docs/e2e.md`, The driver, get the new options.
- Commit 4: Rationale 1, 2 and 3 land in `main`'s inline comments, in
  `docs/e2e.md`, The v2 round trip, and in `tests/e2e/AGENTS.md`.
- Commits 6 to 10: Rationale 6, 7 and 8 land in the rr skills, which
  own the workflow (`rr-workflow` for 6 and 8, `rr-plan` for 7).
- Commit 11: Rationale 6 lands in `docs/brief.md`, Tracking.
- Plan only: the owner's choice of large over medium, and the seed's
  commit count.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message. Commits 6
to 10 change the stage skills while this unit is in flight. The rest
of this unit follows the QuasarDB workflow, as the owner decided on
2026-11-16, whichever skill text is loaded when a stage opens.

## Commits

1. `docs(plan): https-round-trip-plan.md, every round trip step runs over http and https`
2. `test(e2e): request skips certificate verification for the self-signed listeners`
3. `test(e2e): the driver takes each server's https url from the makefile`
4. `test(e2e): the round trip runs on the http and the https plane of each server`
5. `docs(log): the https item leaves the open list`
6. `docs(skills): rr-workflow follows the quasardb workflow`
7. `docs(skills): rr-plan creates the story and its branch`
8. `docs(skills): rr-build opens the pull request beside the build`
9. `docs(skills): rr-merge squash-merges and cherry-picks onto 3.14.x`
10. `docs(skills): rr-start, rr-start-next and rr-seed name the story branch`
11. `docs(brief): the tracking section names the story branch`
12. `/doc-discipline all tests/e2e docs/https-round-trip-plan.md`, with
    a `docs(e2e)` commit only if it finds something.
13. `docs(log): https-round-trip-plan.md deleted, facts moved to tests/e2e, docs/e2e.md and the rr skills`
14. Verify: push the branch, open the pull request
    `QDB-20185 - [rest] HTTPS round trip and QuasarDB workflow in rr skills`,
    build its head in Buildkite, and wait for the result. Green: report
    the build number. Red: fix with further small commits on this
    branch, push, build again.

## Open questions and recommendations

1. Same table names on both planes (Rationale 3). Recommended: same
   names.
2. `-k` in the shared `request` (Rationale 4). Recommended: in
   `request`.
3. Stories under epic 19567, not the release epic (Rationale 7).
   Recommended: epic 19567.
4. `/rr-merge` waits for Igor's or Edouard's approval (Rationale 8).
   Recommended: wait for it.
5. The seed listed 14 build commits, and this plan lists 10. The
   `docs/e2e.md` edits ride with the code they describe, as the root
   `AGENTS.md` requires. Recommended: 10.
