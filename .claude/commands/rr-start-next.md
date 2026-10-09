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
comment specifications and homes name that shape from the start rather
than repaired after. The skill's vocabulary names the shapes; the plan
is still read by someone who has not loaded it, so every shape and
reason is said in plain words.

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
   changes, in the file order they will have: the signature, then its
   comment specification in prose. The specification says three things.
   The shape: a doc comment alone; a doc comment and a body overview;
   numbered steps with a comment at each; one inline comment at a named
   branch or constant; a Note on ownership, caching or I/O. The reason
   for that shape, in plain words a reader without the skill can follow
   ("a doc comment alone, since it is one type switch like
   csvAppender"; "an overview, because the object is read whole before
   anything is appended and three things can fail on the way"). A rule
   number may follow in parentheses and never replaces the reason. The
   claims each part carries, one per line, each with its evidence
   (`path:line`, a test, a Rationale row): for a doc comment, the
   contract facts; for an overview, the strategy and each step's why;
   for a step or inline comment, the guard, threshold, ordering
   constraint or rejected alternative that is local to it. The plan
   carries no comment text. The executor words the comments at build
   time, with the code in view, in the shape `/doc-discipline read`
   loads. Document rows (an `AGENTS.md` bullet, a specification
   paragraph) are specified the same way: the claims and their place,
   not the text. Tests are designed the same way.
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
   commit's comments and document rows from their specifications in
   sections 3 and 5, in the same commit as the code, so that every
   listed claim appears once, at the place the specification names, no
   claim is added without evidence, and a claim the code contradicts is
   reported in the build-stage message and not written; a why that
   arises while building and is
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
can each comment be composed from its specification alone, with every
claim traceable to its evidence, and can each of its choices be defended
from the Rationale alone? Each "no" is a gap in section 3, 4 or 5,
filled now.

Correct: the plan says `ingestCSV` gets a doc comment and a numbered
overview, because the lease spans the decode and the push and three
things can fail between them, and lists the overview's claims: the
session is held for the whole body because the writer types the columns
through it (`internal/qdb/ingest.go:266`); the batches are released on
every path because the receiver owns them. Its Rationale row says the
alternative was a lease per lookup, sunk by the one-held-session rule,
giving up a schema cache; and it asks whether the empty-string exclusion
is a decision or a limitation before writing either word.

Incorrect: the plan pastes the overview as Go text, so the wording is
frozen before the function exists and the build types in a claim the
code does not bear out; or it says "narrated (rule 3)" and nothing
else, so a reader without the skill learns neither the shape nor the
reason; or the Rationale says what was chosen and not what lost, so the
next unit tries the loser again.

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
the plan's Design and Knowledge sections specify them: the executor
composes the wording, in the shape the root `AGENTS.md`, "Code
comments", prescribes, and every claim the specification lists is
present once, in the same commit as the code. A reason that arises
while building (a rejected alternative, a
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

## Shape the unit

The report explains the unit to the owner before it lists anything,
so settle its shape before writing. From what you read, decide:

1. The outcome: what is true in the tree when the unit is done, and
   what it leaves for later.
2. The approach: the shape of the change, where it lives, what it
   reuses, what it introduces.
3. Each design choice the unit requires: the option you take, the
   alternative it beats, the reason, and what settles it. A choice a
   source settles carries that path. A choice the sources leave open is
   a proposal: you still pick, you say the pick is yours, and the plan's
   approval is what settles it. A choice that contradicts a locked
   decision, or that would make the unit materially different work, is
   not yours to make; it goes under Open questions.

A reason you do not have is not invented; the choice becomes an open
question instead.

## Report, then stop

Reply with exactly these sections, in this order. Every candidate,
every constraint and every code claim carries a path, for example
`path/to/document.md` or `path/to/file.go:42-46`; a claim without a
path is not made. Do not summarize the source-of-truth document; report
only what the unit must honor.

Each section is a markdown heading, `# Name`, on a line of its own
with a blank line before and after it. Sections carry no numbers. A
numbered list appears only where a section asks for one, and it starts
at 1.

- `# Workflow` -- one line acknowledging the rules above and the
  feature branch name you will use (`sc-19567/rr-<slug>`), not yet
  created.
- `# Position` -- at most five lines: milestone, what is in flight,
  what the log says comes next, how the unit relates to that.
- `# Candidates` -- a table, one row per candidate, in the log's
  order:
  `| candidate | source (path) | tree check (landed / blocked / ready) | verdict |`
- `# Brief` -- prose for the owner to read, at most 200 words, no
  tables, no paths: the one section whose claims the sections below
  evidence instead of carrying paths themselves. Three paragraphs:
  what the unit is and what is true when it is done; how it will be
  done, the approach and the shape of the change in plain words; why
  this unit comes now and what it leaves for later. Written for a
  reader who has not opened the sources: this is the explanation, the
  rest of the report is its evidence.
- `# Decisions` -- a numbered list, one entry per design choice from
  Shape the unit, one to three lines each: the choice, the
  alternative it beats, the reason, and `settled by <path>` or
  `proposal`. Write "none" if the unit leaves no choice to make.
- `# Size and slices` -- at most three lines: the commit count and
  whether it is inside the band, where the cut is and why there; then
  one line per remaining slice with its size, in the order they would
  follow, or "none" if the unit was not cut.
- `# Constraints` -- a table, one row per decision, constraint or
  gotcha that applies:
  `| constraint | source (path or path:line) | effect on the unit |`
- `# Code involved` -- one line per file or package in the scoped set:
  path, then what it does and how the unit touches it.
- `# Open questions` -- a numbered list of everything the unit's
  source leaves open, every stale entry found in step 3, every gap no
  document calls for, and every choice Shape the unit found was not
  yours to make. Ask; do not resolve by assumption. Write "none" if
  there are none.
- `# Proposed commits` -- numbered one-line commit subjects in the
  order you would land them. Commit 1 is always the plan:
  `docs(plan): <slug>-plan.md, <subject>`; it is the only commit the
  next go-ahead authorizes, and it is outside the size band. Then the
  build commits from step 5: the outline the plan document will
  refine, so the owner can redirect before the plan is written. The
  list closes with the Buildkite verification step (see Lifecycle);
  it is not counted as a commit.

Correct: the brief says the decoder reads the whole body into one
record batch per table before anything is pushed, so a malformed row
anywhere fails the request before the server sees a row; it names
streaming as the alternative and says why it lost; the decision entry
repeats the choice with what settles it.

Incorrect: the brief says "add the CSV decoder, 7 commits, inside the
band", and the owner learns what the decoder does from the commit
subjects.

Then wait. The owner's next go-ahead opens the plan stage for this unit
and nothing beyond it: commit 1, the plan, then the plannotator gate.
