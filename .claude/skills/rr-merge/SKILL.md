---
name: rr-merge
description: Open the merge stage of a unit -- fast-forward the reviewed feature branch onto the base and delete it locally and on origin. Invoked by the owner after reviewing the diff of a green build; the invocation is the owner's yes on the code.
disable-model-invocation: true
---

# /rr-merge -- the merge stage

Run `/rr-workflow` first.

## Entry check

Current branch: !`git branch --show-current`

Working tree (empty means clean):

!`git status --short`

Feature branch vs base, commits ahead / behind:

!`git rev-list --left-right --count sc-19567/rest-rewrite...HEAD`

Base vs origin, commits ahead / behind:

!`git rev-list --left-right --count sc-19567/rest-rewrite...origin/sc-19567/rest-rewrite`

Plan documents on this branch (empty means none):

!`git ls-tree --name-only HEAD docs/ | grep -- '-plan.md' || true`

Buildkite builds of this head (number, state):

!`bk api "pipelines/qdb-api-rest/builds?commit=$(git rev-parse HEAD)" 2>/dev/null | python3 -c 'import sys,json; print(*[(b["number"], b["state"]) for b in json.load(sys.stdin)] or ["none"])'`

The current branch is a `sc-19567/rr-<slug>`, the tree is clean, the
list above shows a build of this exact head in state `passed`, and
the branch carries no plan document: the build stage deleted the
unit's plan as its last commit before the push, with the log entry.
The build's state is read from the list, never from the conversation:
a build-stage message that names a green build is a claim, and the
list is the evidence. Anything else (another branch, a dirty tree, no
build of this head, a build still `running` or `scheduled`, a
`failed` build, a plan still present) is a stop; say which, and the
owner decides. A plan still present means the build
stage is incomplete, and the fix is its deletion commit on this branch,
reviewed and built, never a commit on the base.

## Step by step

1. If the base moved under the feature branch (the first count above is
   not `<n> 0`), rebase the feature branch onto the base so the merge
   stays a fast-forward, and say so in the closing line. A rebase that
   conflicts is stopped and reported; the owner decides.
2. `git checkout sc-19567/rest-rewrite && git merge --ff-only sc-19567/rr-<slug>`.
   No merge commit, ever.
3. Delete the feature branch locally and on origin, where the build
   stage pushed it:
   `git branch -d sc-19567/rr-<slug> && git push origin --delete sc-19567/rr-<slug>`.
4. `git push origin sc-19567/rest-rewrite`. The head it publishes is
   the one the entry check saw pass, so the base on origin is always
   a built head. A rejected push (origin moved) is stopped and
   reported; the owner decides.

Close with one line: what merged and that the base is pushed, as
`<slug>: <n> commits, <first sha>..<last sha>, base pushed`, plus
"rebased first" when step 1 ran.

Incorrect: the entry check lists `docs/<slug>-plan.md`, and the merge
runs anyway because the build is green, so the base carries a plan
that no stage may remove there. Or: the build list says `running`,
the build-stage message said green, and the merge runs on the
message, so the base takes a head whose matrix has not finished.
