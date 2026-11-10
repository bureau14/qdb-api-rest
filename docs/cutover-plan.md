# Cutover of the base to master and 3.14.x

Thread: rest cutover to master and 3.14.x
Status: approved

## Outcome

When the unit lands, `master` on origin carries the rewritten server. It
holds the same commits as the head of `sc-19567/rr-cutover`, and the rr
workflow branches from `master` and fast-forwards into it. `3.14.x` on
origin carries the same server plus two commits. One sets `VERSION` to
`3.14.4.dev0`, and the other vendors `qdb-api-go` from that module's own
`3.14.x` line. The old go-swagger server lives on the branch
`archive/go-swagger` and on the tags `archive/go-swagger-master` and
`archive/go-swagger-3.14.x`. The seven old pull requests against `master`
are closed. Story 20139 is Completed, and story 20145 no longer blocks
it. The branch `sc-19567/rest-rewrite` is deleted locally and on origin.

The documents in the tree say all of this as the present state. The six
`rr-*` skills name `master` as the base. The brief's Tracking section
says where the server lives. The brief's Versioning and release section
names the archive and the pin branch. `internal/AGENTS.md` says which
upstream line each release line vendors `qdb-api-go` from. The log's
Next list holds story 20143.

Left for later units:

- Story 20143, the `qdb-release` updater reading `VERSION` (in
  `qdb-release`, before the first release off the new `master`).
- The `duckdb-go-bindings` bump under story 20145, still blocked on the
  binding's next release.
- The items under "Open after the milestones" in `docs/log.md`.

## Verified facts

All of these were checked on 2026-11-10.

- Origin's `master` is at `62a726b` and `3.14.x` is at `938a0d6`. Both
  tips are "QDB-20065 - [buildkite] Link Linux builds with GCC 16 (#54)"
  (`git ls-remote origin master 3.14.x`). The merge stage's
  `--force-with-lease` values are these two shas.
- `master` and `3.14.x` differ only in the vendored `qdb-api-go` sync, in
  9 files (`git diff --stat origin/master origin/3.14.x`).
- No `archive/*` branch or tag exists on origin
  (`git ls-remote origin 'refs/heads/archive/*' 'refs/tags/archive/*'`).
- The open pull requests against `master` are #3, #13, #14, #22, #24, #25
  and #27. #51 targets `3.14.x-20260821-qdb-3.14.2`
  (`gh pr list --state open`).
- `master`'s protection allows force pushes. It does not enforce admins,
  it requires one approving review and it requires linear history. The
  owner's token is an admin on the repository, so a direct fast-forward
  or force push to `master` passes. `3.14.x` is unprotected (GitHub
  branch protection API).
- `VERSION` on the base reads `3.15.0.dev0`.
- quasardb's `3.14.x` declares `qdb_query_arrow`,
  `qdb_exp_batch_push_arrow_with_options` and
  `qdb_bulk_reader_get_data_arrow`, the Arrow calls the vendored binding
  links (`api/include/qdb/query.h` and `api/include/qdb/ts.h` at
  quasardb `3.14.x`, and the `C.qdb_*` uses in
  `vendor/github.com/bureau14/qdb-api-go/v3/*.go`).
- qdb-duck's `3.14.x` and `master` both sit on "QDB-20068 - Allow pool
  size and table cache TTL in ATTACH string (#23)". The CI download for
  `3.14.x` therefore takes the same qdb-duck package as `master`
  (qdb-duck branches API, `.buildkite/AGENTS.md:39-46`).
- The base vendors `qdb-api-go` `v3.9.1-0.20261105015135-88106da97a69`
  (`go.mod:10`, `vendor/modules.txt:48`). `88106da` is a branch commit of
  qdb-api-go #131 and is not on that module's `master`. Its tree equals
  `928c48b`, the squash commit of #131 and the head of qdb-api-go
  `master` (`git diff --stat 88106da 928c48b` prints nothing).
- qdb-api-go's `3.14.x` head is `54040c8` (#131 on that line, committed
  2026-11-05T20:13:08Z). Its tree differs from the vendored copy only in
  `README.md`, so the two lines vendor the same Go code and the same
  `go.mod` (local clone of qdb-api-go `3.14.x`, `diff -r` against
  `vendor/github.com/bureau14/qdb-api-go/v3`).
- qdb-api-go's tag `v3.14.3` is `3bc960d` (2026-09-08), which predates
  that module's Arrow query, reader and writer APIs, so it cannot serve
  as the `3.14.x` pin.
- `go mod vendor` overwrites
  `vendor/github.com/duckdb/duckdb-go-bindings/alloc.go`, which carries
  the one hand-written patch, so every re-vendor commit reapplies it
  (`internal/AGENTS.md:207-214`).
- The name `sc-19567/rest-rewrite` appears in `.claude/skills/rr-*/SKILL.md`
  (17 lines in six files), `docs/log.md:40` and `docs/brief.md:14`, and
  nowhere else in the tree outside `vendor/`. No default branch in the
  bureau14 organisation mentions `rest-rewrite` (GitHub code search).
- Buildkite builds from its webhook on `master` and `3.14.x`, so each
  force-push starts a build of the pushed tip (`.buildkite/AGENTS.md:54-57`).

## Design

The unit changes no Go code. It edits Markdown in the tree, changes the
`qdb-api-go` pin in `go.mod`, and runs a merge procedure that replaces
`/rr-merge`'s steps for this unit alone.

### `.claude/skills/rr-workflow/SKILL.md`, Branches and commits

```
- `master` is the base branch. Commit to it only by fast-forward merge
  from a reviewed feature branch. [owner, 2026-11-10]
- The code review's patch is `git diff master...sc-19567/rr-<slug>`. [Rationale 1]
```

The remaining bullets keep their wording. "Flat, linear history" holds
on `master` too, because GitHub enforces linear history there.

### `.claude/skills/rr-workflow/SKILL.md`, Tools

```
- The control branch row reads `master`. [Rationale 1]
- The code review row's patch reads `git diff master...sc-19567/rr-<slug>`. [Rationale 1]
- The paragraph under the review table is rewritten whole from the new rule:
  - The `quasardb-workflow` and `github` skills do not apply to a unit. [owner, 2026-11-10]
  - A unit has no story of its own, no pull request and no squash-merge. [owner, 2026-11-10]
  - `/rr-merge` is the only merge. [tree: rr-merge/SKILL.md]
```

The paragraph loses its reason, "the whole rewrite is one Shortcut story
that lasts months", because story 20139 completes at the cutover. The
reason for keeping the rr workflow on `master` is the owner's choice of
2026-11-10, and the skill states that choice as a rule without inventing
a reason for it. The sentence "the base lands on `master` once, at the
end, outside these skills" is deleted. The "Incorrect" example below the
paragraph keeps its wording. The description line in the frontmatter
does not name a branch and stays.

### `.claude/skills/rr-*/SKILL.md`, the stage skills

```
- rr-start:51, rr-start-next:25, rr-plan:18: the entry check counts `master...origin/master`. [Rationale 1]
- rr-start:90: the unit's commits are `git log --oneline master..`. [Rationale 1]
- rr-build:27: plan documents added on the branch are `master...HEAD`. [Rationale 1]
- rr-build:142 and rr-build:206: the patch and the command are `git diff master...sc-19567/rr-<slug>`. [Rationale 1]
- rr-merge:21 and rr-merge:25: the counts are `master...HEAD` and `master...origin/master`. [Rationale 1]
- rr-merge step 2: `git checkout master && git merge --ff-only sc-19567/rr-<slug>`. [Rationale 1]
- rr-merge step 4: `git push origin master`, and its sentence says the base on origin is always a built head. [Rationale 1]
```

### `vendor/` and `go.mod` on `master`

```
- go.mod pins qdb-api-go at the pseudo-version of 928c48b. [internal/AGENTS.md:200-206]
- go get github.com/bureau14/qdb-api-go/v3@928c48b1b766, go mod tidy, go mod vendor.
- alloc.go's calloc patch is reapplied after go mod vendor. [internal/AGENTS.md:207-214]
- The diff touches go.mod, go.sum and vendor/modules.txt and nothing else, because the two trees are equal. [Verified facts]
```

### `internal/AGENTS.md`, Building, the `qdb-api-go` bullet

```
- The bump names a commit on upstream `master` for this repository's `master`. [tree: internal/AGENTS.md:200-202]
- The bump names a commit on upstream `3.14.x` for this repository's `3.14.x`. [owner, 2026-11-10]
- The `3.14.x` line of qdb-api-go carries the same work as its `master`, because changes reach it by cherry-pick. [Verified facts, qdb-api-go 3.14.x log]
```

The bullet is rewritten whole from the new rule. The sentences about
branch commits and squash commits keep their facts.

### `docs/brief.md`, Tracking

```
- The rewrite is Shortcut epic 19567, with the URL. [tree: docs/brief.md:12-13]
- The server lives on `master` and `3.14.x`, and story 20139 tracks it. [owner, 2026-11-10]
- No pull request carried the server onto `master`, so 20139 was completed by hand. [owner, 2026-11-10]
- Feature branches keep the epic's id, `sc-19567/rr-<slug>`. [rr-workflow/SKILL.md]
- The work that lives in other repositories has one story each. [tree: docs/brief.md:17-18]
- The 20139 row reads "server implementation" without "this branch". [owner, 2026-11-10]
- The 20145 row reads "`duckdb-go-bindings` bump" without "blocks 20139". [owner, 2026-11-10]
```

"Was completed" is a fact about Shortcut and not a tense word about the
tree. The writer phrases it as the state: "20139 is completed by hand,
because no pull request carried it".

### `docs/brief.md`, Versioning and release

```
- `archive/go-swagger` holds the old go-swagger server, and the tags `archive/go-swagger-master` and `archive/go-swagger-3.14.x` mark the two tips it replaced. [owner, 2026-11-10]
- `3.14.x-20260821-qdb-3.14.2` keeps the old server for its customer, and its backports come from `archive/go-swagger`. [owner, docs/log.md:55-57]
- `3.14.x` carries the rewrite at `3.14.4.dev0`, with the new config format and the dropped endpoints in a patch line. [owner, 2026-11-10, docs/log.md:62-65]
```

### `docs/log.md`, Current state and an entry

```
- In flight: none.
- Next 1: story 20143, the `qdb-release` updater's `qdb-api-rest` entry reads and writes `VERSION` with the `qdb-nats-connector` formats in `version.py`, before the first release off `master`. [owner, docs/log.md:48-54]
- "Open after the milestones" and "Blocked on" keep their items. [tree: docs/log.md:67-100]
- Entry, 2026-11-10: `master` and `3.14.x` carry the rewrite, the old server is archived, and the rr workflow's base is `master`; `docs/brief.md`, Tracking and Versioning and release. [owner, 2026-11-10]
```

### `3.14.x` branch: `VERSION` and the `qdb-api-go` pin

`sc-19567/rr-cutover-3.14.x` is cut from the head of `sc-19567/rr-cutover`
after the plan's deletion commit, so it carries no plan.

```
- VERSION reads 3.14.4.dev0. [owner, docs/log.md:44-45]
- go get github.com/bureau14/qdb-api-go/v3@54040c8e91d0, go mod tidy, go mod vendor. [owner, 2026-11-10]
- alloc.go's calloc patch is reapplied. [internal/AGENTS.md:207-214]
- The diff touches go.mod, go.sum, vendor/modules.txt and the vendored README.md. [Verified facts]
```

### The merge stage of this unit

`/rr-merge` runs this procedure in place of its steps 1 to 4. It reads
the procedure from the plan's approval commit with `git show`, because
the plan is deleted before the merge. The skill's checks still hold for
both heads: a clean tree, no plan document, and a `passed` Buildkite
build of each exact head. Its two counts against `master` describe the
old server until step 4 and are read as unrelated histories, so step 1
of the skill, the rebase, never runs for this unit.

```
// 1. Fetch, and stop if origin's master is not 62a726b or 3.14.x is not 938a0d6. [Verified facts]
// 2. Push the archive: branch archive/go-swagger at 62a726b, tags archive/go-swagger-master at 62a726b and archive/go-swagger-3.14.x at 938a0d6, then read them back with ls-remote. [owner, docs/log.md:32-36]
// 3. Close #3, #13, #14, #22, #24, #25 and #27, each with one comment that names archive/go-swagger. [owner, 2026-11-10]
// 4. git push --force-with-lease=master:62a726b origin sc-19567/rr-cutover:master. [owner, docs/log.md:40-43]
// 5. git push --force-with-lease=3.14.x:938a0d6 origin sc-19567/rr-cutover-3.14.x:3.14.x. [owner, docs/log.md:44-45]
// 6. Ensure a build of each pushed tip with the buildkite skill and read both back passed. [owner, docs/log.md:46-47]
// 7. Shortcut: story 20139 to Completed, and remove the blocks link from 20145 to 20139. [owner, 2026-11-10]
// 8. Point local master and 3.14.x at origin, and delete sc-19567/rest-rewrite, sc-19567/rr-cutover and sc-19567/rr-cutover-3.14.x locally and on origin. [owner, 2026-11-10]
//
// 1. A tip that moved since the plan was approved means someone pushed to the old server, and the owner decides. [Rationale 5]
// 2. The archive is pushed before anything destructive, so every later step is recoverable from it. [owner, docs/log.md:32-36]
// 4. and 5. The lease refuses the push if origin moved after step 1. [Rationale 5]
// 6. A red build after the force-push stops the procedure, and the owner decides between a fix and restoring from the archive. [Rationale 5]
// 8. Deleting the rewrite branch comes last, so it survives until master is green. [Rationale 5]
```

The closing line reads
`cutover: master at <sha>, 3.14.x at <sha>, builds <n> and <m> passed, old server archived`.

## Rationale

| decision                                                                                               | why                                                                                                | rejected, and why                                                                                                                                                                            | gained                                                                | given up                                  | settled by        |
| ------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- | ----------------------------------------- | ----------------- |
| 1. `master` is the base of the rr workflow, and the rewrite branch is deleted                          | the owner's choice                                                                                 | keeping `sc-19567/rest-rewrite` as the base, because every merge would need a second push to `master` and the two could drift; moving to the quasardb workflow, which the owner did not take | one branch, the same gates                                            | the rewrite's own branch name             | owner, 2026-11-10 |
| 2. Every outward operation runs in the merge stage                                                     | the owner's choice                                                                                 | the build stage pushing the archive refs; the owner running the force-pushes by hand                                                                                                         | one gate covers every change to origin, after review and green builds | none                                      | owner, 2026-11-10 |
| 3. Story 20139 completes at the cutover, and 20145 stops blocking it                                   | the server is done, and the bump is a dependency update on `master`                                | leaving 20139 open until the bump lands                                                                                                                                                      | Shortcut matches the brief                                            | none                                      | owner, 2026-11-10 |
| 4. No whole-tree review pull request for Igor                                                          | the owner's Inbox review is the gate                                                               | an empty orphan branch with a pull request of the base against it, which the log offered as optional                                                                                         | no wait on a review of the whole tree                                 | a second reader of the whole tree         | owner, 2026-11-10 |
| 5. The procedure checks the two tips, pushes with a lease and deletes the rewrite branch last          | a force-push is the one step here that loses commits if it goes wrong                              | a plain `--force`, which overwrites whatever origin holds                                                                                                                                    | every destructive step has a known prior state and a recovery         | none                                      | proposal          |
| 6. `3.14.x` vendors qdb-api-go from that module's `3.14.x` line, at `54040c8`                          | the owner's note: the `3.14.x` server must not build against the binding's `master`                | the `v3.14.3` tag, which predates the Arrow APIs; keeping the `master` pin, which the owner rejected                                                                                         | each release line pins its own binding line                           | none, because the code is identical today | owner, 2026-11-10 |
| 7. `master` repins qdb-api-go from `88106da` to `928c48b`                                              | `internal/AGENTS.md:200-206` says a squash-merged branch commit is repinned to the squash commit   | leaving the branch commit, which no upstream branch carries                                                                                                                                  | the pin names a commit on upstream `master`                           | none                                      | owner, 2026-11-10 |
| 8. The `3.14.x` commits sit on their own branch, cut from the feature branch after the plan is deleted | the build stage builds and the owner reviews them before any push to `3.14.x`                      | committing straight onto `3.14.x` during the merge, which would skip review and CI                                                                                                           | the `3.14.x` tip is a built and reviewed head                         | a second branch to delete                 | proposal          |
| 9. The seven old pull requests are closed                                                              | the owner: they are irrelevant                                                                     | retargeting some of them to `archive/go-swagger`                                                                                                                                             | none to carry                                                         | the PRs' branches stay, unmerged          | owner, 2026-11-10 |
| 10. The merge stage reads this procedure from the plan's approval commit                               | the plan is deleted before the merge, and the procedure has no permanent home because it runs once | an ADR or a skill section for a one-time procedure                                                                                                                                           | nothing permanent describes a past event                              | the procedure lives only in git history   | owner, 2026-11-10 |

## Knowledge

- Commit 2 (rr-workflow): Rationale 1 lands as the base bullet, and the
  rule "a unit has no story, no pull request, no squash-merge" lands in
  the Tools paragraph.
- Commit 3 (stage skills): Rationale 1 lands in each command. It needs
  no prose of its own.
- Commit 4 (vendor on `master`): Rationale 7 needs no new text, because
  `internal/AGENTS.md:200-206` already states the rule.
- Commit 5 (`internal/AGENTS.md`): Rationale 6 lands as the
  per-release-line rule in the `qdb-api-go` bullet.
- Commit 6 (brief, Tracking): Rationale 3 lands in the paragraph and the
  20145 row.
- Commit 7 (brief, Versioning and release): the archive refs, the pin
  branch's backports and the `3.14.x` version line land as bullets.
  They span the repository, so the brief is their home by
  `docs/AGENTS.md`'s routing table, row 1.
- Commit 8 (log): the Next list and the dated entry.
- Commits 12 and 13 (`3.14.x`): Rationale 6 lands through commit 5's
  rule. The commits carry no prose.
- `plan only`: Rationales 2, 4, 5, 8, 9 and 10, and the merge procedure.
  They describe how this unit runs once and constrain no later code.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message.

Specific to this unit:

- Commits 4 and 13 run `go mod vendor`. After each one, `git diff --stat`
  must list only the files the Design names, plus `alloc.go` back at its
  patched content. Any other file is a stop to report.
- The `/rr-build` the owner invokes loads the skill text of the base, so
  its own commands name `sc-19567/rest-rewrite` while commits 2 and 3
  are being written. The review patch is
  `git diff sc-19567/rest-rewrite...sc-19567/rr-cutover`, and the second
  patch is `git diff sc-19567/rr-cutover...sc-19567/rr-cutover-3.14.x`.
  Both go into one guide.

## Commits

1. `docs(plan): cutover-plan.md, the base moves to master and 3.14.x and the old server is archived`
2. `docs(skills): the rr workflow names master as its base`
3. `docs(skills): the rr stage skills check, diff and merge against master`
4. `build(vendor): qdb-api-go at 928c48b, the squash commit of 88106da on its master`
5. `docs(internal): each release line vendors qdb-api-go from the same upstream line`
6. `docs(brief): the tracking section says the server lives on master and 3.14.x, and 20145 blocks nothing`
7. `docs(brief): versioning and release names the archive of the old server and the pin branch's backports`
8. `docs(log): the cutover replaces next 1, and story 20143 is next`
9. `/doc-discipline placement check` on the touched Markdown files, with this plan. A finding is fixed in a further small commit before step 10.
10. `docs(log): cutover-plan.md deleted, facts moved to docs/brief.md, internal/AGENTS.md and the rr skills`
11. Verify: push `sc-19567/rr-cutover`, build its head in Buildkite,
    wait for the result. Green: report the build number. Red: fix with
    further small commits on this branch, push, build again.
12. On `sc-19567/rr-cutover-3.14.x`, cut from the head of step 10: `chore(version): 3.14.x is 3.14.4.dev0`
13. `build(vendor): qdb-api-go at 54040c8 from its 3.14.x line`
14. Verify: push `sc-19567/rr-cutover-3.14.x`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again. A fix on `sc-19567/rr-cutover` cuts the `3.14.x` branch again
    from its new head.

## Open questions and recommendations

1. Later changes reach `3.14.x` only when the owner asks for them in a
   unit. No document states a cherry-pick rule. Recommendation: record
   none in this unit.
