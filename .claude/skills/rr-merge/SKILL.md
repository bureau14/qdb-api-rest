---
name: rr-merge
description: Open the merge stage of a unit -- squash-merge the reviewed pull request onto master, cherry-pick the squash commit onto 3.14.x, ensure the builds of both lines and complete the story. Invoked by the owner after reviewing the code of a green build; the invocation is the owner's yes on the code.
disable-model-invocation: true
---

# /rr-merge -- the merge stage

Run `/rr-workflow` first.

## Entry check

Current branch: !`git branch --show-current`

Working tree (empty means clean):

!`git status --short`

Plan documents on this branch (empty means none):

!`git ls-tree --name-only HEAD docs/ | grep -- '-plan.md' || true`

Pull request of this branch (number, state, head, and the logins whose
latest review approves it):

!`gh pr view --json number,state,headRefOid,latestReviews -q '"\(.number) \(.state) \(.headRefOid) approved by: \([.latestReviews[] | select(.state == "APPROVED") | .author.login] | join(" "))"' 2>/dev/null || echo none`

Buildkite builds of this head (number, state), the `buildkite` skill's
one-liner with the slug from `/rr-workflow`, Tools:

!`bk api "pipelines/qdb-api-rest/builds?commit=$(git rev-parse HEAD)" 2>/dev/null | python3 -c 'import sys,json; print(*[(b["number"], b["state"]) for b in json.load(sys.stdin)] or ["none"])'`

The current branch is the unit's story branch, `sc-<id>/<kebab-title>`.
No tracked file is modified. The pull request is `OPEN`, its head is
this branch's head, and the approving logins include `igorniebylski`
or `edouarda`. An approval by anyone else does not count, although
GitHub's review decision reads `APPROVED` after it. The list above shows a build of this exact head in
state `passed`. The branch carries no plan document, because the build
stage deleted the unit's plan as its last commit before the push, with
the log entry. The build's state and the approval are read from the
lists, never from the conversation: a build-stage message that names a
green build is a claim, and the list is the evidence. Anything else is
a stop: another branch, modified files, no pull request, no approval,
no build of this head, a build still `running` or `scheduled`, a
`failed` build, or a plan still present. Say which, and the owner
decides. A plan still present means the build stage is incomplete, and
the fix is its deletion commit on this branch, reviewed and built,
never a commit on `master`.

## Step by step

Run `quasardb-workflow`, step 7, as one unit. Its preconditions are the
entry check above, and the owner's invocation of `/rr-merge` is its
"merge":

1. 7a: squash-merge the pull request with the subject
   `QDB-<id> - <story title> (#<n>)` and an empty body, and delete the
   branch.
2. 7b: cherry-pick the squash commit onto `3.14.x`, run the build, lint
   and the tests of the touched area, and push. A conflict in
   hand-written code aborts the pick, leaves `3.14.x` untouched and is
   reported. The owner decides.
3. 7c: ensure the builds of the squash commit on `master` and of the
   pick on `3.14.x`, both watchers in the background.
4. 7d: set the story to Completed once both lines carry the change.

Close with one line: what merged on both lines, as
`<story title>: master <squash sha>, 3.14.x <pick sha>, builds <n> and <m>`.

Incorrect: the entry check lists `docs/<slug>-plan.md`, and the merge
runs anyway because the build is green, so `master` carries a plan
that no stage may remove there. Or: the build list says `running`,
the build-stage message said green, and the merge runs on the message,
so `master` takes a head whose matrix has not finished. Or: the owner
invokes `/rr-merge` before Igor approved, and the merge runs because
the invocation is the gate, so the pull request lands unreviewed.
