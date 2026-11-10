---
name: rr-build
description: Open the build stage of a unit -- land the commits an approved plan lists, with their comments and document rows, run the doc-discipline checks, push, build the branch in Buildkite while the owner reviews the code as a guided review in the Inbox, and post the build-stage message. The plan is derived from the branch; an argument overrides it. Invoked by the owner after the plan-stage message; the invocation is the gate.
disable-model-invocation: true
argument-hint: "[docs/<slug>-plan.md, only when the branch does not name it]"
---

# /rr-build -- the build stage

Run `/rr-workflow` first. An argument, when the owner gave one, is:

<plan-path>
$ARGUMENTS
</plan-path>

This stage may run in a session that has none of the seed or plan
conversation. The plan is the whole input: read it whole before the
first commit, and keep its `Thread:` name, because the code review goes
into that thread after the plan is deleted.

## The plan

The branch names the plan. On `sc-19567/rr-<slug>` the plan is
`docs/<slug>-plan.md`, and it is the one `docs/*-plan.md` the branch
adds over the base:

!`git diff --name-only --diff-filter=A master...HEAD -- 'docs/*-plan.md'`

When that list is one file, it is the plan, and an empty argument is
the normal case. When the argument names a file, it is the plan and
the list is checked against it: a disagreement is said and the owner
decides. When the list is empty or holds more than one file and the
argument is empty, stop and ask for the path.

Correct: the branch is `sc-19567/rr-flight-catalog`, the list is
`docs/flight-catalog-plan.md`, the argument is empty; that is the plan.

Incorrect: the argument is empty and you ask for the path while the
list names one file.

## Entry check

Current branch: !`git branch --show-current`

Working tree (empty means clean):

!`git status --short`

Last commits on this branch:

!`git log --oneline -5`

The current branch is the plan's `sc-19567/rr-<slug>`, the tree is
clean, the plan's last commit is a `docs(plan)` commit, and the plan's
`Status:` line says `approved`, which the plan stage sets in its own
commit when the owner approves the plan. Anything else is the owner's
decision before anything else. A plan still at `draft` means the owner
has not approved it, or the plan stage skipped the status commit; say which
you cannot tell, and the owner decides.

## The commits

Run `/doc-discipline read` before the first code commit. Then land the
plan's Commits section in order, one small commit each, each building
and passing lint. The plan is followed to the letter; a deviation the
code forces is said in the build-stage message, never made silently.

Every commit carries its comments and document rows as the plan's
Design and Knowledge sections specify them: the executor composes the
wording, in the shape the root `AGENTS.md`, "Code comments",
prescribes, and every claim the specification lists is present once, in
the same commit as the code. A claim is written as a claim, without the
plan's evidence brackets. A claim the code contradicts is not written;
it is reported in the build-stage message with both versions. Where a
commit changes code that already has comments, the blocks it touches
are rewritten whole from the new code, as the root `AGENTS.md`, "Code
comments", rule 5, prescribes. They are never patched with another
clause.

A reason that arises while building (a threshold, an ordering
constraint, or a rejected alternative a reader of that line would
propose) and is not in the plan is written
where it is decided, with its evidence. One you would have to invent
is asked through the question tool before the commit that needs it,
never written as a guess and never left out silently.

Correct: a step comment needs to say why the writer refuses an empty
push; you do not know; you ask, and the answer becomes the comment.

Incorrect: the comment says "the writer refuses an empty push for
safety", because that sounded right.

## The checks

After the last code commit, `/doc-discipline all <paths the branch
touched>` and one small commit per finding.

Before the build-stage message, `/doc-discipline check <paths the
branch touched> <plan path>`, which reconciles the code against the
plan and reports every reason the plan promised that the code and
documents do not state. Its "Unlanded from the plan" findings are
fixed with a further small commit each, or reported with the reason
each was dropped. Its "Dropped on purpose" findings are listed in the
build-stage message and get no commit. The build-stage message quotes
the check's report, or says it was clean.

Incorrect: the plan lands the rejection of the schema cache in an ADR,
the branch is pushed without that ADR, and the reason dies with the
plan. Or: every step comment names an alternative the plan rejected,
and the code reads like the planning session.

## The plan's deletion

Once the check is clean, land the plan's deletion commit, the one its
Commits section lists after the `/doc-discipline` runs: `git rm
docs/<slug>-plan.md`, the log entry `<slug>-plan.md deleted; facts
moved to <where>` and the Current state update, as `docs/AGENTS.md`,
Plans, prescribes, in one commit
`docs(log): <slug>-plan.md deleted, facts moved to <where>`. It lands
here and not in the merge stage, so the owner reviews it with the code
and the build lints it. A plan whose Commits section lacks the
deletion is an older plan; land the deletion anyway and say so in the
build-stage message. Nothing is pushed while the plan is in the tree.

Incorrect: the plan is pushed with the branch because the deletion
"belongs to the merge", and it survives on the base, where no stage
may commit.

## The Buildkite build and the code review

Local lint and tests cover one platform; the pipeline covers the whole
matrix, and the owner reviews the code while the pipeline runs rather
than after it. So once the checks above are clean, in this order:

1. With the plan deleted from the tree, push `sc-19567/rr-<slug>` and
   ensure a build of its head: the `buildkite` skill, "Ensuring and
   watching a build", with the parameters in `/rr-workflow`, Tools.
   Run the watcher in the background; do not block on it.
2. Write a guided review of the pushed head and send it, as
   `/rr-workflow`, The Inbox, says:
   - `get_guide_brief`, then the patch
     `git diff master...sc-19567/rr-<slug>`.
   - `submit_guide` with that exact patch, in the unit's thread. The
     body names the head's SHA, because the owner's "Looks good"
     approves that head and no other.

   Then end your turn. A watcher that exits wakes the session too:
   handle it as step 3 says, report it in the conversation, and end
   your turn again. The owner's answer is one of three, by The Inbox's
   table:
   - "Looks good" on the guide's message: the owner's yes on this head.
   - A reply: the owner's change requests, also when its words sound
     like a yes. Address every annotation and every point of the reply
     with further small commits on this branch, push, create a new
     build for the new head, and send a new guide for the new head from
     step 2, with `reply_to` set to the reply's id. The build of the
     superseded head is reported as superseded, not waited for.
   - The thread resolved without a "Looks good": stop and report. The
     owner decides in the conversation.

3. Wait for the build of the reviewed head. The watcher's exit says
   what comes next:
   - 0, `FINAL ... passed`: green. The build-stage message names the
     build by its real number and state, read back with the skill's
     one-liner for the builds of a commit (`/rr-workflow`, Tools),
     never from memory of having created it.
   - 1, `FINAL ... <state>` with the log tails: red. The skill's
     "Reading a failure" says which kind it is, with the control branch
     from Tools. A failure caused by the branch is fixed with further
     small commits on the branch, pushed, built again, and reviewed
     again from step 2, because the owner has not seen the fix. A
     failure caused by something else (an agent, an outage, a failure
     the control build shows as well) is reported with the evidence,
     and the owner decides.
   - 3, `STATUS ...`: the checkpoint passed with the build still
     running. Relaunch the watcher with the printed build number and
     keep waiting.
   - 5, `NOBUILD ...`: no build could be created. Stop and report; the
     stage cannot end without a build.

The stage is over only when the reviewed head is the built head, the
owner marked its guide "Looks good" and the build is green. Local tests passing is
not the end of the stage, however small the change.

Correct: the checks are clean; you push, create build 412, send the
guide; the owner replies with two annotations; you fix them in two
commits, push, create build 413, send a new guide; the owner marks it
"Looks good"; build 413 goes green; the build-stage message names 413
and says 412 was superseded.

Incorrect: the owner marks the guide "Looks good" and the build-stage
message is posted while the build is still running. Or: build 412 goes
green while the guide waits, and you post that into the unit's thread,
so the owner cannot mark the guide. Or: the build goes red, you fix it
and wait for green without sending a new guide, so the owner merges a
commit they never saw.

## The build-stage message

Exactly: the branch name; a summary of the change in plain words; each
deviation from the plan with the reason the code forced it; the
doc-discipline check's report, or "clean"; the plan's deletion commit
by its sha; the review's outcome, with the Inbox message id the owner
marked; the Buildkite build number and its state,
and any superseded build numbers; the command
`git diff master...sc-19567/rr-<slug>`; one line saying the merge waits
for `/rr-merge`.

Then wait. Do not ask whether to merge; the owner invokes `/rr-merge`
when they are ready.
