---
name: rr-plan
description: Open the plan stage of the current unit -- create the feature branch, write and commit the plan document as the unit's first commit, and yield it to the owner through the plannotator gate. Invoked by the owner after the seed report; the invocation is the gate.
disable-model-invocation: true
---

# /rr-plan -- the plan stage

Run `/rr-workflow` first. This stage opens on this invocation and only
this invocation, and nothing in it is code.

## Entry check

Current branch: !`git branch --show-current`

Base vs origin, commits ahead / behind (0 0 means in sync):

!`git rev-list --left-right --count sc-19567/rest-rewrite...origin/sc-19567/rest-rewrite`

Working tree (empty means clean):

!`git status --short`

Feature branches already present:

!`git branch --list 'sc-19567/rr-*'`

A seed report for this unit is in the conversation; its Decisions,
Open questions and Proposed commits are the inputs here. Without one,
stop and say so. A dirty tree, a base behind origin, or a leftover
feature branch is the owner's decision before anything else.

## Step by step

1. Create `sc-19567/rr-<slug>` from the up-to-date base, clean tree.
2. `/doc-discipline read`.
3. Read `plan-shape.md` and write `docs/<slug>-plan.md` in that shape.
4. Commit it alone: `docs(plan): <slug>-plan.md, <one-line subject>`.
   This is commit 1 of the unit, the one the seed report promised.
5. Yield through the review tool, never through a typed question:

   ```
   plannotator annotate docs/<slug>-plan.md --gate --json
   ```

   Run it in the background; it blocks until the owner decides in the
   browser, and the harness wakes you when it exits. Do nothing while it
   runs. Its stdout is one JSON decision:
   - `approved` (with optional `feedback`): the owner's yes on the plan,
     and on the plan alone. Change the plan's `Status:` line from
     `draft` to `approved` and commit that alone as
     `docs(plan): <slug>-plan.md is approved`, so a session seeded
     fresh from the tree reads the plan as approved instead of a
     draft still under review. Then post the plan-stage message and
     wait; the build stage opens when the owner invokes `/rr-build`.
   - annotations: the owner's redirect. Fold every annotation into the
     plan, commit the revision as a further `docs(plan)` commit, and
     gate again from step 5. Never start code on an annotated plan.
   - `dismissed`, or a non-zero exit without a decision: stop and
     report; the owner decides in the conversation.

The plan-stage message, posted on approval, is exactly: the branch name
and the plan's path; `git show <sha>` for the plan commit and the
sha of the approval commit; the owner's
`feedback` quoted, if any, with how the build will honor it; the open
questions the plan recorded, each with the recommendation the approval
has now settled; one line saying the build waits for
`/rr-build docs/<slug>-plan.md`.

Correct: you branch, read the skill, write the plan, commit it, run the
gate, wait; the decision is `approved`; you set the status, commit it,
post the message and wait;
the owner invokes `/rr-build`; commit 2 starts there.

Incorrect: the gate returns `approved` and you start commit 2 because
the plan is approved. Or: the gate returns `approved`, you post the
message, and the plan in the tree still says `Status: draft`, so the
next seed reads it as unreviewed. Or: you commit the plan and ask "Approve the plan,
redirect it, or stop?" in the conversation instead of running the gate.
Or: the gate returns annotations and you start code while addressing
them.

## Before committing the plan

A why you cannot verify, or a function you cannot place under the
comment rules, is a question to the owner through the question tool,
asked before the plan is committed; the answer goes into the plan. What
the owner leaves unanswered is an open question of the plan-stage
message.

Read the plan as the executor would: for each commit, can each comment
be composed from its specification alone, with every claim traceable to
its evidence, and can each of its choices be defended from the
Rationale alone? Each "no" is a gap in the Design, Rationale or
Knowledge section, filled now.
