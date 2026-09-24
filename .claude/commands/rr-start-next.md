---
description: Seed a session with the next unit of work -- survey docs, history and tree, pick one reviewable 5-15 commit unit, load its context without starting it
---

# /rr-start-next -- session seed, unit chosen for you

This command takes no task description. You work out which unit of work
comes next, cut it to a reviewable size, and then seed the session for
it exactly as `/rr-start` would have, had the owner described that unit.

This command does three things: it commits you to the branch-off
workflow below, it chooses the next unit, and it loads the context that
unit will need. It ends with a report and a wait for the owner.

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

!`git log --oneline -30`

Feature branches already present (a leftover here needs the owner's
decision before anything else):

!`git branch --list 'sc-19567/rr-*'`

## Choose the unit

Work through these steps in order; each one feeds the next.

1. Survey. The `AGENTS.md` hierarchy is the map: the documentation
   folder's `AGENTS.md` prescribes the reading order and names the
   source of truth. Read the source-of-truth document in full, the
   current-state section of the project log in full, and every live
   plan. Read `git log` far enough back to cover everything that landed
   since that current-state section was last rewritten.
2. List candidates. A candidate is work a document already calls for:
   work in flight, the log's next items, an unmet exit criterion of the
   active milestone, the remaining work of a live plan. Every candidate
   carries its source path. Work no document calls for is not a
   candidate; a gap you notice goes under Open questions.
3. Check each candidate against the tree, not against the documents:
   has it already landed (`git log`, the files themselves)? Are the
   things it builds on present? Does the log list it as blocked? Does
   it need the owner's hands or an outside party rather than commits?
   A candidate the documents still list but the tree shows as landed is
   a stale entry: report it, do not pick it.
4. Pick. Work in flight comes before anything else; after that, the
   first candidate in the log's own order that survived step 3. The
   log's order is the owner's priority. Size never reorders it.
5. Size. Write the unit's commit list at the granularity the workflow
   above prescribes. The size is the length of that list, not an
   estimate made before writing it. The band is 5 to 15 build-stage
   commits; the plan commit is not counted.
   - More than 15: cut the unit into slices and take the first. A slice
     is mergeable on its own: one subject, the base builds and passes
     lint and tests after it merges, and what it adds can be exercised
     by a test or a command that exists when it merges. The first slice
     is the one the others build on. Size the slice the same way. List
     every remaining slice, one line each, so the owner sees the whole
     cut.
   - Fewer than 5: propose it as it is and say so. Add the following
     candidate only when both share a subject and the owner would
     review them as one change anyway.
   - Never reach the band by changing commit granularity: no folding
     commits together to get under 15, no splitting them to reach 5.

Correct: the first next item is a whole subsystem; its commit list runs
to 31. You cut four slices, propose the first at 9 commits (the
skeleton and one case end to end, under test), and list the other
three with their sizes.

Incorrect: the same item, proposed whole with 15 commits of several
hundred lines each; or passed over for the second item because that
one happens to fit the band.

## Load context

The survey was broad; this pass is deep, and only for the chosen unit.
Follow the `AGENTS.md` map rather than a list kept here, so this command
stays correct as the repository changes.

First, scope. From the unit and the project structure as the
documentation describes it, write down the set the unit touches:
folders, packages, endpoints, config blocks, tests, documents. This set
decides what you read below; extend it when reading reveals a
dependency.

Then read, in this order:

1. The planning and design documents the survey did not already cover:
   the plans whose subject is in the scoped set, and the decision
   records those plans or the unit's source cite or constrain.
2. The `AGENTS.md` of every folder in the scoped set, and of each
   parent folder on the way there.
3. The code in the scoped set and what it depends on: the packages,
   their tests, and the call sites, top to bottom. For vendored
   dependencies, the parts the unit will call.
4. The history of the scoped paths: `git log --oneline -- <paths>`, and
   the diffs of the commits that introduced or last changed the
   functions the unit will touch, so the unit continues the existing
   direction instead of restarting it.
5. Memory: any recalled note about this repository that bears on the
   unit, verified against the tree before relying on it.

Precedence when sources disagree: the tree and accepted decision
records, then the source-of-truth document, then the project log, then
memory. Report the disagreement under Open questions; do not resolve it
silently.

Look actively for: locked decisions the unit must honor, handoff
constraints recorded for the active milestone, known gaps in the
dependencies the unit will call, and what the existing fixtures, goldens
and benchmarks already pin.

If the deep pass changes the commit list, size the unit again (step 5)
before reporting.

## Report, then stop

Reply with exactly these sections, in this order. Every candidate,
every constraint and every code claim carries a path, for example
`path/to/document.md` or `path/to/file.go:42-46`; a claim without a
path is not made. Do not summarize the source-of-truth document; report
only what the unit must honor.

1. **Workflow** -- one line acknowledging the rules above and the
   feature branch name you will use (`sc-19567/rr-<slug>`), not yet
   created.
2. **Position** -- at most five lines: milestone, what is in flight,
   what the log says comes next, how the unit relates to that.
3. **Candidates** -- a table, one row per candidate, in the log's
   order:
   `| candidate | source (path) | tree check (landed / blocked / ready) | verdict |`
4. **The unit** -- at most six lines: what it is, why this one, where
   the cut is and why there, the commit count and whether it is inside
   the band, and what is true when it is done.
5. **Remaining slices** -- one line per slice with its size, in the
   order they would follow. Write "none" if the unit was not cut.
6. **Constraints** -- a table, one row per decision, constraint or
   gotcha that applies:
   `| constraint | source (path or path:line) | effect on the unit |`
7. **Code involved** -- one line per file or package in the scoped set:
   path, then what it does and how the unit touches it.
8. **Open questions** -- a numbered list of everything the unit's
   source leaves open, every stale entry found in step 3, and every gap
   no document calls for. Ask; do not resolve by assumption. Write
   "none" if there are none.
9. **Proposed commits** -- the commit list from step 5, numbered
   one-line subjects in the order you would land them: the outline the
   plan document will refine, so the owner can redirect before the plan
   is written. The list closes with the Buildkite
   verification step (see Lifecycle); it is not counted as a commit.

Then wait. The owner's next go-ahead opens the plan stage for this unit
and nothing beyond it.
