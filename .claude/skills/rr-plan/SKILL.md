---
name: rr-plan
description: Open the plan stage of the current unit -- create the feature branch, write and commit the plan document as the unit's first commit, and put it to the owner in one Inbox review. Invoked by the owner after the seed report; the invocation is the gate.
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

When the current branch is already the unit's `sc-19567/rr-<slug>`,
its last commit is the plan's `docs(plan)` commit and the plan says
`Status: draft`, the branch and the plan exist and the owner has not
approved it: skip to step 5. A seed report is not required in that
case; the plan is the input. Read the plan's thread with `read_thread`
first. When the thread's latest message is this plan's review and
shows `ack`, the owner has approved it: commit the status. When it is
this plan's review without an answer, end your turn and wait. Else
send the plan.

## Step by step

1. Create `sc-19567/rr-<slug>` from the up-to-date base, clean tree.
2. `/doc-discipline read`.
3. Read `plan-shape.md` and write `docs/<slug>-plan.md` in that shape,
   with the seed report's thread name on its `Thread:` line.
4. Commit it alone: `docs(plan): <slug>-plan.md, <one-line subject>`.
   This is commit 1 of the unit, the one the seed report promised.
5. Put the committed plan to the owner in one Inbox message, with
   `send_message` in the unit's thread, as `/rr-workflow`, The Inbox,
   says. `attachments` holds `docs/<slug>-plan.md`, and the body says
   in one line what the unit delivers and names the plan commit's sha.
   The message carries no question block, so the owner can mark it
   "Looks good". Then end your turn.

   The owner's answer is one of three, by The Inbox's table:
   - "Looks good" on this message: the owner's yes on the plan, and on
     the plan alone. Change the plan's `Status:` line from `draft` to
     `approved` and commit that alone as
     `docs(plan): <slug>-plan.md is approved`, so a session seeded
     fresh from the tree reads the plan as approved instead of a draft
     still under review. Then post the plan-stage message and wait.
     The build stage opens when the owner invokes `/rr-build`.
   - A reply: the owner's redirect, also when its words sound like a
     yes. Fold every annotation and every point of the reply into the
     plan, commit the revision as a further `docs(plan)` commit, and
     send the plan again from step 5, with `reply_to` set to the
     reply's id. Never start code on a plan the owner redirected.
   - The thread resolved without a "Looks good": stop and report. The
     owner decides in the conversation.

The plan-stage message, posted on approval, is exactly: the branch name
and the plan's path; `git show <sha>` for the plan commit and the
sha of the approval commit; the open questions the plan recorded,
each with the recommendation the approval has now settled; one line
saying the build waits for `/rr-build`. The message goes to the
conversation, not to the Inbox thread.

Correct: you branch, read the skill, write the plan, commit it, send
it to the Inbox and end your turn. The owner marks it "Looks good"; you
set the status, commit it, post the message and wait; the owner invokes
`/rr-build`; commit 2 starts there.

Incorrect: the owner marks the plan "Looks good" and you start commit 2
because the plan is approved. Or: the owner marks it "Looks good", you
post the message, and the plan in the tree still says `Status: draft`,
so the next seed reads it as unreviewed. Or: you add an "Approve the
plan?" question to the message, so the owner cannot mark it "Looks
good". Or: the owner replies with annotations and you start code while
addressing them.

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
