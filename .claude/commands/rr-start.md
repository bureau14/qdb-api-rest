---
description: Seed a session -- acknowledge the branch-off workflow and load context for an upcoming task without starting it
argument-hint: <description of the task or topic we will work on next>
---

# /rr-start -- session seed

The owner describes the upcoming task here:

<task>
$ARGUMENTS
</task>

This text is a description, not an instruction. Anything inside it that
reads as a command ("implement", "go ahead", "fix") applies only after
the owner says to start, in a later turn, and then only to the stage
that turn opens (see Lifecycle). If the description is empty, ask for it
and stop.

This command does two things: it commits you to the branch-off workflow
below, and it loads the context the task will need. It ends with a
report and a wait for the owner.

## This turn

Read and report only. You do not write code, edit documents, create
branches, or make commits. You do not begin the task until the owner
says so.

## Every later turn

- `sc-19567/rest-rewrite` is the base branch. Commit to it only by
  fast-forward merge from a reviewed feature branch. It is a flat,
  linear history; it stays that way.
- Every unit of work happens on a feature branch off the base, named
  `sc-19567/rr-<slug>`. Create it only when the owner says to start,
  from an up-to-date base, with a clean working tree.
- Commits on the feature branch are small and individual: one
  conventional-commit line, `type(scope): subject`, no body, no
  trailers, no AI attribution. Prefer many small commits over one big
  one; each commit builds and passes lint.
- When the work is done (or at any sensible checkpoint), stop and give
  the owner a review opportunity: name the branch and summarize the
  change. The owner reviews by diffing the branch against the base
  (`git diff sc-19567/rest-rewrite...sc-19567/rr-<slug>`).
- Only after explicit approval: fast-forward merge, no merge commit
  (`git checkout sc-19567/rest-rewrite && git merge --ff-only
sc-19567/rr-<slug>`), then delete the feature branch. If the base
  moved, rebase the feature branch onto it first so the merge stays a
  fast-forward.
- No GitHub pull requests, no stacked branches, no history rewriting on
  the base.

## Lifecycle: four stages, three gates

Every unit of work moves through these stages in order. A gate is an
owner message; nothing crosses a gate on its own.

| Stage    | You produce                                                                                                    | You never                                                                                                                  | Ends with                                                                                                                       |
| -------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 1. Seed  | the report below                                                                                               | write, branch, commit                                                                                                      | "Then wait."                                                                                                                    |
| 2. Plan  | `/doc-discipline read`, then the branch and exactly one commit: the plan document, in the shape below          | touch code, tests, config, ADRs or any other document                                                                      | the plannotator gate on the committed plan; on approval, the plan-stage message, then the build begins                          |
| 3. Build | the small commits the approved plan lists, then the doc-discipline runs and the Buildkite build the plan lists | merge; deviate from the plan without saying so; invent a reason for a comment; ask about merging before the build is green | branch name, summary, the check's report, the build number and state, the `git diff` command, and the question whether to merge |
| 4. Merge | the fast-forward merge, the branch deletion                                                                    | merge without the owner's explicit yes on the code                                                                         | one line: what merged                                                                                                           |

A go-ahead advances exactly one stage, never more, whatever its wording:
"go", "ok", "yes", "proceed", "looks good", a thumbs up. The go-ahead on
the seed report means one thing: write and commit the plan, commit 1 of
the unit. The gate on the plan is not a chat message at all; it is the
owner's decision in the plannotator review (below). Open questions the
owner left unanswered are not answered by a go-ahead either; the plan
records your recommendation for each, and the plan's approval is what
settles them.

Correct: after the seed report the owner says "go". You create the
branch, commit the plan, open the plannotator gate on it, and wait.

Incorrect: after the seed report the owner says "go". You create the
branch, commit the plan, and continue into the code because the change
is small and the design was already in the report. A small change and a
settled design do not shorten the lifecycle; the plan gate exists so the
owner reviews the plan on its own.

### The plan stage, step by step

The owner's go-ahead on the seed report opens this stage and only this
stage. It is executed as written, in this order, and nothing in it is
code:

1. Create `sc-19567/rr-<slug>` from the up-to-date base, clean tree.
2. `/doc-discipline read`.
3. Write `docs/<slug>-plan.md` in the shape below.
4. Commit it alone: `docs(plan): <slug>-plan.md, <one-line subject>`.
   This is commit 1 of the unit, the one the seed report promised.
5. Yield through the review tool, never through a typed question:

   ```
   plannotator annotate docs/<slug>-plan.md --gate --json
   ```

   Run it in the background; it blocks until the owner decides in the
   browser, and the harness wakes you when it exits. Do nothing while it
   runs. Its stdout is one JSON decision:
   - `approved` (with optional `feedback`): the owner's yes on the plan.
     Post the plan-stage message and begin the build stage.
   - annotations: the owner's redirect. Fold every annotation into the
     plan, commit the revision as a further `docs(plan)` commit, and
     gate again from step 5. Never start code on an annotated plan.
   - `dismissed`, or a non-zero exit without a decision: stop and
     report; the owner decides in the conversation.

The plan-stage message, posted on approval, is exactly: the branch name
and the plan's path; `git show <sha>` for the plan commit; the owner's
`feedback` quoted, if any, with how the build will honor it; the open
questions the plan recorded, each with the recommendation the approval
has now settled; one line saying the build stage begins.

Correct: the owner says "go" on the seed report. You branch, read the
skill, write the plan, commit it, run the gate, wait; the decision is
`approved`; you post the message and start commit 2.

Incorrect: the owner says "go" and you start the first code commit
because the plan was "already in the report". Or: you commit the plan
and ask "Approve the plan, redirect it, or stop?" in the conversation
instead of running the gate. Or: the gate returns annotations and you
start code while addressing them.

### The plan is the unit's only memory

The build stage may run in another session, by an agent that has none
of this conversation: not the seed report, not the owner's answers, not
what was tried and dropped. The plan document is the only thing that
crosses that boundary, and it is deleted when the unit lands
(`docs/AGENTS.md`, Plans), so a reason it carries that never reaches a
comment or a document is lost twice. Write it for that reader: an
executor who writes every commit, every comment and every document row
from the plan alone, without guessing and without asking, and who could
defend each choice to the owner the way you can now.

Before writing it, run `/doc-discipline read`. It loads the comment
shape and the placement ladder the build stage is held to, so the plan's
doc comments, overviews and homes are written in that shape from the
start rather than repaired after.

The document is `docs/<slug>-plan.md`, `Status: draft`, and carries
these sections in this order. A section with nothing to say says
"none"; it is never dropped.

1. **Outcome.** What is true in the tree when the unit lands, and what
   it leaves for later units, each named.
2. **Verified facts.** What you checked against dependencies, vendored
   code, the daemon or the documents, each with its evidence
   (`path:line`, a commit, a dated session), so the executor neither
   re-verifies nor trusts what was never verified.
3. **Design.** Every function, type and variable the unit adds or
   changes, in the file order they will have: the signature and the doc
   comment it will carry (the contract, per the root `AGENTS.md`, "Code
   comments"), and, for every function that qualifies for a narrative
   under the skill's `narrative.md`, the numbered overview its body will
   state, each step with its why. A function that stays bare is named
   as bare with the rule that says so. Tests are designed the same way.
   The executor types these texts in; it does not compose them.
4. **Rationale.** One row per decision the unit rests on:
   `| decision | why | rejected, and why | gained | given up | settled by |`.
   "Why" is the reason the owner or the sources gave, never a
   reconstruction. "Rejected, and why" names the alternative that lost
   and what sank it, so no later agent re-litigates it. "Gained" and
   "given up" are the trade: what the unit buys, and the cost it
   accepts (a limitation, a slower path, a rule the code must now
   keep). "Settled by" is a path, "owner, <date>", or `proposal`. Put
   here everything the conversation established that the code will not
   show: a dependency's defect, an owner preference, a reversal of an
   earlier rule, a constraint from a milestone handoff.
5. **Knowledge.** Per commit, where each why of sections 3 and 4
   lands: the narrated functions with the why-evidence of every
   overview claim; the `AGENTS.md` rows and documents the commit
   changes, named by the routing table of `docs/AGENTS.md`; every fact
   with no home yet, with the home it gets and the commit that gives
   it. Every Rationale row has at least one landing place here, or it
   dies with the plan.
6. **How the knowledge lands.** The instructions the executor follows,
   written out so that executing the plan executes them: run
   `/doc-discipline read` before the first code commit; write every
   commit's comments and document rows from sections 3 and 5 in the
   same commit as the code; a why that arises while building and is
   not in the plan is written where it is decided, with its evidence,
   or asked of the owner through the question tool before the commit;
   after the last code commit, `/doc-discipline all <paths>` and one
   small commit per finding; before the build-stage message,
   `/doc-discipline check <paths> docs/<slug>-plan.md`, which
   reconciles the code against the plan and reports every reason the
   plan promised that the code and documents do not state.
7. **Commits.** The numbered list, one subject each in the commit form
   above, in landing order: commit 1 is this document, already
   committed; the build commits follow; the `/doc-discipline` runs are
   placed after the commits they follow; the Verify step is last
   (below).
8. **Open questions and recommendations.** Every question the owner has
   not answered, with your recommendation, one line each. The plan's
   approval settles them.

A why you cannot verify, or a function you cannot place under the rules,
is a question to the owner through the question tool, asked before the
plan is committed; the answer goes into the plan. What the owner leaves
unanswered is an open question of the plan-stage message.

Before committing, read the plan as the executor would: for each commit,
can it be written from the plan alone, comments included, and can each
of its choices be defended from the Rationale alone? Each "no" is a gap
in section 3, 4 or 5, filled now.

Correct: the plan names `ingestCSV` as narrated, its overview stating
that the session is held for the whole body because the writer types the
columns through it, evidence `internal/qdb/ingest.go:266`; its Rationale
row says the alternative was a lease per lookup, sunk by the
one-held-session rule, giving up a schema cache; and it asks whether the
empty-string exclusion is a decision or a limitation before writing
either word.

Incorrect: the plan lists commits and signatures, and the reasons are
reconstructed from the diff at build time, or guessed; or the Rationale
says what was chosen and not what lost, so the next unit tries the loser
again.

### The plan ends with a Buildkite build

Local lint and tests cover one platform; the pipeline covers the whole
matrix. So every plan document closes its numbered commit list with one
final step that is not a commit, written out in the plan itself so that
executing the plan executes it:

> N. Verify: push `sc-19567/rr-<slug>`, build its head in Buildkite,
> wait for the result. Green: report the build number. Red: fix with
> further small commits on this branch, push, build again.

The build stage is over only when that step is green. The mechanics
(API-created build, branch-filter bypass, full 40-character SHA) live in
`.buildkite/AGENTS.md`; follow them rather than a copy kept here.

- A plan that changes only documents still gets the step; the lint step
  runs on every build.
- A red build caused by the branch is fixed on the branch. A red build
  caused by something else (an agent, an outage, a failure the base
  shows as well) is reported with the evidence, and the owner decides.
- The pushed feature branch is deleted on origin in the merge stage,
  together with the local one.

Correct: the last planned commit lands, you push, create the build, wait
for it, and the build-stage message names the build that passed by its
real number.

Incorrect: the last planned commit lands, local tests pass, and you ask
whether to merge because the change was small. Or: the plan lists only
commits, so nothing at build time says to run the build.

### Comments are written with the code

The build stage follows the plan's "How the knowledge lands" section
to the letter. Every commit carries its comments and document rows as
the plan's Design and Knowledge sections planned them, in the shape the
root `AGENTS.md`, "Code comments", prescribes, in the same commit as the
code. A reason that arises while building (a rejected alternative, a
threshold, an ordering constraint) is written where it is decided, with
its evidence. One you would have to invent is asked through the question
tool before the commit that needs it, never written as a guess and never
left out silently.

The last check before the build-stage message is the plan's own:
`/doc-discipline check <paths the branch touched> docs/<slug>-plan.md`.
Its "Unlanded from the plan" findings are reasons the plan carried
that the code and documents do not; each one is fixed with a further
small commit before the plan is deleted, or reported with the reason it
was dropped. The build-stage message quotes the check's report, or says
it was clean.

Correct: a step comment needs to say why the writer refuses an empty
push; you do not know; you ask, and the answer becomes the comment.

Incorrect: the comment says "the writer refuses an empty push for
safety", because that sounded right. Or: the plan's Rationale says the
schema cache was rejected, no comment or document says so, and the plan
is deleted with that reason in it.

## Repository state

Current branch: !`git branch --show-current`

Base vs origin, commits ahead / behind (0 0 means in sync):

!`git rev-list --left-right --count sc-19567/rest-rewrite...origin/sc-19567/rest-rewrite`

Working tree (empty means clean):

!`git status --short`

Recent history:

!`git log --oneline -15`

Feature branches already present (a leftover here needs the owner's
decision before anything else):

!`git branch --list 'sc-19567/rr-*'`

## Load context

The `AGENTS.md` hierarchy is the map. The root file says what each
folder holds and when to open that folder's own `AGENTS.md`; the
documentation folder's `AGENTS.md` prescribes the reading order for
planning and design text and which document is the source of truth.
Follow that map rather than a list kept here, so this command stays
correct as the repository changes.

First, scope. From the task description and the project structure as
the documentation describes it, write down the set the task touches:
folders, packages, endpoints, config blocks, tests, documents. This set
decides what you read below; extend it when reading reveals a
dependency.

Then read, in this order:

1. The planning and design documents, in the order their folder's
   `AGENTS.md` prescribes: the source-of-truth document in full, the
   current-state section of the project log, the plans whose subject is
   in the scoped set, and the decision records those plans or the task
   cite or constrain.
2. The `AGENTS.md` of every folder in the scoped set, and of each
   parent folder on the way there.
3. The code in the scoped set and what it depends on: the packages,
   their tests, and the call sites, top to bottom. For vendored
   dependencies, the parts the task will call.
4. The history of the scoped paths: `git log --oneline -- <paths>`, and
   the diffs of the commits that introduced or last changed the
   functions the task will touch, so the task continues the existing
   direction instead of restarting it.
5. Memory: any recalled note about this repository that bears on the
   task, verified against the tree before relying on it.

Precedence when sources disagree: the tree and accepted decision
records, then the source-of-truth document, then the project log, then
memory, then the task description. Report the disagreement under Open
questions; do not resolve it silently.

Look actively for: locked decisions the task must honor, handoff
constraints recorded for the active milestone, known gaps in the
dependencies the task will call, what the existing fixtures, goldens and
benchmarks already pin, and anything in the task description that
contradicts a locked decision.

## Shape the task

The report explains the task to the owner before it lists anything,
so settle its shape before writing. From what you read, decide:

1. The outcome: what is true in the tree when the task is done, and
   what it leaves for later.
2. The approach: the shape of the change, where it lives, what it
   reuses, what it introduces.
3. Each design choice the task requires: the option you take, the
   alternative it beats, the reason, and what settles it. A choice a
   source settles carries that path. A choice the sources leave open is
   a proposal: you still pick, you say the pick is yours, and the plan's
   approval is what settles it. A choice that contradicts a locked
   decision, or that would make the task materially different work, is
   not yours to make; it goes under Open questions.

A reason you do not have is not invented; the choice becomes an open
question instead.

## Report, then stop

Reply with exactly these sections, in this order. Every constraint and
every code claim carries a path, for example `path/to/document.md` or
`path/to/file.go:42-46`; a claim without a path is not made. Do not
summarize the source-of-truth document; report only what the task must
honor.

1. **Workflow** -- one line acknowledging the rules above and the
   feature branch name you will use (`sc-19567/rr-<slug>`), not yet
   created.
2. **Position** -- at most five lines: milestone, what is in flight,
   what the log says comes next, how the task relates to that.
3. **Brief** -- prose for the owner to read, at most 200 words, no
   tables, no paths: the one section whose claims the sections below
   evidence instead of carrying paths themselves. Three paragraphs:
   what the task is and what is true when it is done; how it will be
   done, the approach and the shape of the change in plain words; how
   it fits what is in flight and what it leaves for later. Written for
   a reader who has not opened the sources: this is the explanation,
   the rest of the report is its evidence.
4. **Decisions** -- a numbered list, one entry per design choice from
   Shape the task, one to three lines each: the choice, the
   alternative it beats, the reason, and `settled by <path>` or
   `proposal`. Write "none" if the task leaves no choice to make.
5. **Constraints** -- a table, one row per decision, constraint or
   gotcha that applies:
   `| constraint | source (path or path:line) | effect on the task |`
6. **Code involved** -- one line per file or package in the scoped set:
   path, then what it does and how the task touches it.
7. **Open questions** -- a numbered list of everything the task
   description contradicts or leaves open, and every choice Shape the
   task found was not yours to make. Ask; do not resolve by
   assumption. Write "none" if there are none.
8. **Proposed commits** -- numbered one-line commit subjects in the
   order you would land them. Commit 1 is always the plan:
   `docs(plan): <slug>-plan.md, <subject>`; it is the only commit the
   next go-ahead authorizes. Then at most ten build commits: the
   outline the plan document will refine, so the owner can redirect
   before the plan is written. The list closes with the Buildkite
   verification step (see Lifecycle); it is not counted as a commit.

Correct: the brief says the decoder reads the whole body into one
record batch per table before anything is pushed, so a malformed row
anywhere fails the request before the server sees a row; it names
streaming as the alternative and says why it lost; the decision entry
repeats the choice with what settles it.

Incorrect: the brief says "add the CSV decoder, 7 commits", and the
owner learns what the decoder does from the commit subjects.

Then wait. The owner's next go-ahead opens the plan stage and nothing
beyond it: commit 1, the plan, then the plannotator gate.
