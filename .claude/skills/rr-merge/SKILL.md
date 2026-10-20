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

The current branch is a `sc-19567/rr-<slug>` whose build-stage message
in the conversation names a green build. Without that message, or on
any other branch, or with a dirty tree, stop and say so; the owner
decides.

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
4. Push the base only if the owner asks; the base's relation to origin
   is the owner's.

Close with one line: what merged, as `<slug>: <n> commits,
<first sha>..<last sha>`, plus "rebased first" when step 1 ran.

The plan document is deleted once its work has landed, under the rules
of `docs/AGENTS.md`, Plans, and that deletion is a commit of its own on
a feature branch, never a commit here on the base. A plan still present
after the merge is named in the closing line.
