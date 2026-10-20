---
name: rr-build
description: Open the build stage of a unit -- land the commits an approved plan lists, with their comments and document rows, run the doc-discipline checks, push, build the branch in Buildkite while the owner reviews the code in plannotator, and post the build-stage message. Takes the plan's path. Invoked by the owner after the plan-stage message; the invocation is the gate.
disable-model-invocation: true
argument-hint: docs/<slug>-plan.md
---

# /rr-build -- the build stage

Run `/rr-workflow` first. The plan is:

<plan-path>
$ARGUMENTS
</plan-path>

This stage may run in a session that has none of the seed or plan
conversation. The plan is the whole input: read it whole before the
first commit, and when the path is empty or the file is missing, stop
and ask for it.

## Entry check

Current branch: !`git branch --show-current`

Working tree (empty means clean):

!`git status --short`

Last commits on this branch:

!`git log --oneline -5`

The current branch is the plan's `sc-19567/rr-<slug>`, the tree is
clean, and the plan's last commit is a `docs(plan)` commit. Anything
else is the owner's decision before anything else. A plan whose status
is still `draft` after the plannotator gate is fine; the gate's
decision lives in the conversation or in the owner's invocation of this
stage, not in the file.

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
it is reported in the build-stage message with both versions.

A reason that arises while building (a rejected alternative, a
threshold, an ordering constraint) and is not in the plan is written
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
each was dropped. The build-stage message quotes the check's report,
or says it was clean.

Incorrect: the plan's Rationale says the schema cache was rejected, no
comment or document says so, and the plan is deleted with that reason
in it.

## The Buildkite build and the code review

Local lint and tests cover one platform; the pipeline covers the whole
matrix, and the owner reviews the code while the pipeline runs rather
than after it. So once the checks above are clean, in this order:

1. Push `sc-19567/rr-<slug>` and create the Buildkite build for its
   head. The mechanics (API-created build, branch-filter bypass, full
   40-character SHA, reprioritizing the scheduled jobs) live in
   `.buildkite/AGENTS.md`; follow them rather than a copy kept here.
   Wait for the build in the background; do not block on it.
2. Open the code review, in the background as well, with a long
   timeout:

   ```
   plannotator review --base sc-19567/rest-rewrite --json
   ```

   It blocks until the owner decides in the browser, and the harness
   wakes you when it exits. Do nothing else while both run. Its stdout
   is one JSON record; classify it by `decision` alone:
   - `approved`: the owner's yes on the code. Notes in `message` are
     guidance, quoted in the build-stage message, not a change request.
   - `annotated`: the owner's change requests. Address every annotation
     with further small commits on this branch, push, create a new
     build for the new head, and open the review again from step 2.
     The build of the superseded head is reported as superseded, not
     waited for.
   - `dismissed`, or a non-zero exit without a decision: stop and
     report; the owner decides in the conversation.

3. Wait for the build of the reviewed head:
   - Green: the build-stage message names the build by its real
     number.
   - A red build caused by the branch is fixed with further small
     commits on the branch, pushed, built again, and reviewed again
     from step 2, because the owner has not seen the fix.
   - A red build caused by something else (an agent, an outage, a
     failure the base shows as well) is reported with the evidence,
     and the owner decides.

The stage is over only when the reviewed head is the built head, the
review is `approved` and the build is green. Local tests passing is
not the end of the stage, however small the change.

Correct: the checks are clean; you push, create build 412, open the
review; the owner annotates two comments; you fix them in two commits,
push, create build 413, open the review again; it returns `approved`;
build 413 goes green; the build-stage message names 413 and says 412
was superseded.

Incorrect: the review returns `approved` and the build-stage message is
posted while the build is still running. Or: the build goes red, you
fix it and wait for green without reopening the review, so the owner
merges a commit they never saw.

## The build-stage message

Exactly: the branch name; a summary of the change in plain words; each
deviation from the plan with the reason the code forced it; the
doc-discipline check's report, or "clean"; the review's decision with
its notes quoted, if any; the Buildkite build number and its state,
and any superseded build numbers; the command
`git diff sc-19567/rest-rewrite...sc-19567/rr-<slug>`; one line saying
the merge waits for `/rr-merge`.

Then wait. Do not ask whether to merge; the owner invokes `/rr-merge`
when they are ready.
