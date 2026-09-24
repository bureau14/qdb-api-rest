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

| Stage    | You produce                                                                                                         | You never                                                                                                                  | Ends with                                                                                                                       |
| -------- | ------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 1. Seed  | the report below                                                                                                    | write, branch, commit                                                                                                      | "Then wait."                                                                                                                    |
| 2. Plan  | the branch and exactly one commit: the plan document                                                                | touch code, tests, config, ADRs or any other document                                                                      | the plan-stage message below, ending in the approval question                                                                   |
| 3. Build | the small commits the approved plan lists, then the doc-discipline check and the Buildkite build the plan ends with | merge; deviate from the plan without saying so; invent a reason for a comment; ask about merging before the build is green | branch name, summary, the check's report, the build number and state, the `git diff` command, and the question whether to merge |
| 4. Merge | the fast-forward merge, the branch deletion                                                                         | merge without the owner's explicit yes on the code                                                                         | one line: what merged                                                                                                           |

A go-ahead advances exactly one stage, never more, whatever its wording:
"go", "ok", "yes", "proceed", "looks good", a thumbs up. Open questions
the owner left unanswered are not answered by the go-ahead either; the
plan records your recommendation for each, and the plan's approval is
what settles them.

Correct: after the seed report the owner says "go". You create the
branch, commit the plan, post the plan-stage message, and stop.

Incorrect: after the seed report the owner says "go". You create the
branch, commit the plan, and continue into the code because the change
is small and the design was already in the report. A small change and a
settled design do not shorten the lifecycle; the plan gate exists so the
owner reviews the plan on its own.

### The plan-stage message

When the plan is committed, reply with exactly:

1. the branch name and the plan's path;
2. `git show <sha>` for the plan commit;
3. your recommendation for every open question the owner did not
   answer, one line each;
4. the literal closing line: "Approve the plan, redirect it, or stop?"

Then stop. Code begins only after the owner answers that question with
an approval.

### The plan carries the knowledge

The build stage writes the comments and documents of the unit while the
reasons are still in context, so the plan document says what they will
be. Under a **Knowledge** heading, per commit:

- the functions expected to qualify for a narrative under the root
  `AGENTS.md`, "Code comments", and the why each overview will state,
  with the evidence for it (a `file:line`, a test, a document);
- the `AGENTS.md` rows and the documents the commit changes, named by
  the routing table in `docs/AGENTS.md`;
- any fact the commit establishes that has no home yet, with the home it
  will get.

A why you cannot verify, or a function you cannot place under the rules,
is a question to the owner through the question tool, asked before the
plan is committed; the answer goes into the plan. What the owner leaves
unanswered is an open question of the plan-stage message.

Correct: the plan names `ingestCSV` as narrated, its overview stating
that the session is held for the whole body because the writer types the
columns through it, evidence `internal/qdb/ingest.go:266`; and asks
whether the empty-string exclusion is a decision or a limitation before
writing either word.

Incorrect: the plan lists commits only, and the reasons are reconstructed
from the diff at build time, or guessed.

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

Every build-stage commit carries its comments and document rows as the
Knowledge section planned them, in the shape the root `AGENTS.md`, "Code
comments", prescribes. A reason that arises while building (a rejected
alternative, a threshold, an ordering constraint) is written where it is
decided. One you would have to invent is asked through the question tool
before the commit that needs it, never written as a guess and never left
out silently.

Before the build-stage message, run `/doc-discipline check <paths the
branch touched>`. A finding is fixed with a further small commit on the
branch; the build-stage message quotes the check's report, or says it
was clean.

Correct: a step comment needs to say why the writer refuses an empty
push; you do not know; you ask, and the answer becomes the comment.

Incorrect: the comment says "the writer refuses an empty push for
safety", because that sounded right.

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
3. **Constraints** -- a table, one row per decision, constraint or
   gotcha that applies:
   `| constraint | source (path or path:line) | effect on the task |`
4. **Code involved** -- one line per file or package in the scoped set:
   path, then what it does and how the task touches it.
5. **Open questions** -- a numbered list of everything the task
   description contradicts or leaves open. Ask; do not resolve by
   assumption. Write "none" if there are none.
6. **Proposed commits** -- at most ten one-line commit subjects,
   numbered, in the order you would land them: the outline the plan
   document will refine, so the owner can redirect before the plan is
   written. The list closes with the Buildkite
   verification step (see Lifecycle); it is not counted as a commit.

Then wait. The owner's next go-ahead opens the plan stage and nothing
beyond it.
