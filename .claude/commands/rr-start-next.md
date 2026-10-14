---
description: Seed a session with the next unit of work -- survey docs, history and tree, offer three scopes of the next unit, load its context and put its design choices to the owner without starting it
---

# /rr-start-next -- session seed, unit chosen for you

This command takes no task description. You work out which unit of work
comes next, cut it into three scopes for the owner to choose from, and
then seed the session for the chosen scope exactly as `/rr-start` would
have, had the owner described that unit.

This command does four things: it commits you to the branch-off
workflow below, it chooses the next candidate, it loads the context
that candidate needs and lets the owner pick its scope, and it puts the
design choices the unit rests on to the owner. It ends with a report
and a wait for the owner.

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
| 2. Plan  | `/doc-discipline read`, then the branch and exactly one commit: the plan document, in the shape below          | touch code, tests, config, ADRs or any other document                                                                      | the plannotator gate on the committed plan; on approval, the plan-stage message, then a wait for the owner's go on the build    |
| 3. Build | the small commits the approved plan lists, then the doc-discipline runs and the Buildkite build the plan lists | merge; deviate from the plan without saying so; invent a reason for a comment; ask about merging before the build is green | branch name, summary, the check's report, the build number and state, the `git diff` command, and the question whether to merge |
| 4. Merge | the fast-forward merge, the branch deletion                                                                    | merge without the owner's explicit yes on the code                                                                         | one line: what merged                                                                                                           |

A go-ahead advances exactly one stage, never more, whatever its wording:
"go", "ok", "yes", "proceed", "looks good", a thumbs up. The go-ahead on
the seed report means one thing: write and commit the plan, commit 1 of
the unit. The gate on the plan is not a chat message at all; it is the
owner's decision in the plannotator review (below), and it settles the
plan, not the build: the go-ahead on the build is the owner's next
message after the plan-stage message. Open questions the
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
   - `approved` (with optional `feedback`): the owner's yes on the plan,
     and on the plan alone. Post the plan-stage message and wait; the
     build stage opens on the owner's next go-ahead in the conversation.
   - annotations: the owner's redirect. Fold every annotation into the
     plan, commit the revision as a further `docs(plan)` commit, and
     gate again from step 5. Never start code on an annotated plan.
   - `dismissed`, or a non-zero exit without a decision: stop and
     report; the owner decides in the conversation.

The plan-stage message, posted on approval, is exactly: the branch name
and the plan's path; `git show <sha>` for the plan commit; the owner's
`feedback` quoted, if any, with how the build will honor it; the open
questions the plan recorded, each with the recommendation the approval
has now settled; one line saying the build waits for the owner's go.

Correct: the owner says "go" on the seed report. You branch, read the
skill, write the plan, commit it, run the gate, wait; the decision is
`approved`; you post the message and wait; the owner says "go"; you
start commit 2.

Incorrect: the owner says "go" and you start the first code commit
because the plan was "already in the report". Or: the gate returns
`approved` and you start commit 2 without the owner's go on the build. Or: you commit the plan
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
   changes, in the file order they will have. Each entry is a sketch:
   one fenced Go block holding the doc comment above the signature,
   the signature, and a body made of comments where the real comments
   will sit. The first body comment names the shape as a `style:` line
   from the vocabulary the example below fixes, followed by a
   `because:` line giving the reason for it in plain words a reader
   without the skill can follow ("one type switch like csvAppender";
   "the object is read whole before anything is appended, and three
   things fail on the way"). A rule number may follow in parentheses
   and never replaces the reason. Below it, one claim per `//` bullet,
   each ending with its evidence in brackets (`path:line`, a test, a
   Rationale row): the contract facts in the doc comment, the strategy
   and each step's why in the overview, and the guard, threshold,
   ordering constraint or rejected alternative at the step or line it
   belongs to. Where step comments will sit, the bullets are numbered
   (`// 1.`, `// 2.`). A pseudo-code statement may anchor a step
   (`schema, err := schemaOf(name)`) and is illustrative: the code
   commit follows the signature and the claims, not the sketch's
   statements. A claim is one sentence, and two claims never share a
   bullet.

   The sketch is the shape and the claims, not the wording. The
   executor rewrites every comment in the project's prose at build
   time, with the code in view, in the shape `/doc-discipline read`
   loads; it keeps every bullet as a claim and drops the evidence
   brackets, which stay in the plan. Document rows (an `AGENTS.md`
   bullet, a specification paragraph) are sketched the same way: a
   fenced block of bullets under the heading they will join. Tests are
   designed the same way.

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

Correct: each Design entry is a sketch in this form, with one of the
two styles named on the first body line:

```go
// <contract claim> [evidence]
// <contract claim> [evidence]
func <name>(<params>) (<results>) {
	// style: doc comment + numbered overview + step comments
	// because: <reason for this style in plain words>
	//
	// <strategy claim> [evidence]
	//
	// 1. <what the step achieves> because <its why> [evidence]
	// 2. <what the step achieves> because <its why> [evidence]
	//    - rejected: <alternative>, because <what sank it> [Rationale row]
	// 3. <what the step achieves> [evidence]
	//    <pseudo-code anchor, illustrative>
}
```

```go
// <contract claim> [evidence]
func <name>(<params>) (<results>) {
	// style: doc comment alone
	// because: <reason, e.g. one type switch like csvAppender>
	//
	// at <branch or constant>: <local claim> [evidence]
}
```

The style line is one of: `doc comment alone`; `doc comment + numbered
overview + step comments`; either with `+ inline note at <line>` or
`+ Note: on <ownership | caching | I/O>`. The Rationale row behind a
rejected alternative is cited at the step that rejects it; and a
question the plan cannot answer is asked before the plan is committed,
never written as a claim.

Incorrect: the plan pastes the comments as finished Go text and tells
the build to type them in, so the wording is frozen before the function
exists; or it describes the comments in prose ("a doc comment and a
numbered overview; claims: ...; ...;"), so a reviewer rebuilds the
function in their head to see what is covered; or the style line says
"narrated (rule 3)" and nothing else; or the Rationale says what was
chosen and not what lost, so the next unit tries the loser again.

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

## What the sources are

Every document under `docs/`, every `AGENTS.md` and every plan was
written by an agent, in a session the owner reviewed at the level of a
decision, not at the level of every sentence. So a document settles a
choice only at the level of detail the owner could have reviewed as a
decision. Classify every claim the unit rests on by its provenance, and
carry the class into the report:

- `owner`: the owner stated it. A log line or handoff item marked
  "owner, <date>", an "Owner decision" entry, a brief section the
  brief names as a pre-project decision, or the Decision section of an
  accepted ADR.
- `tree`: the code, a test, a fixture, a vendored dependency or the
  daemon shows it, at a `path:line`.
- `agent`: everything else. A mechanism, type, shape, config key,
  endpoint, number or sequence written into a brief section, an ADR's
  consequences, a log "Next" item or a milestone criterion for work
  that has not started is `agent` until the owner confirms it for this
  unit, however confident the sentence sounds and however many
  documents repeat it.

An `agent` claim the unit's code would build on is not settled; it is
a question for the challenge round below. An `agent` claim the tree
contradicts, or that two documents state differently, is drift: the
same question, with the disagreement spelled out.

Correct: an accepted ADR's Decision says login answers a token pair,
and the log marks the refresh lifetime as an owner decision; both are
`owner`. The ADR's consequences name the config key and a sliding
expiry; the owner has not said so in those words, so both are `agent`,
and the unit that adds them asks before the plan is written.

Incorrect: the key and the expiry go into the plan as settled because
the log says the ADR "fixes the shapes".

## Load context

The survey was broad; this pass is deep, and only for the chosen
candidate. Follow the `AGENTS.md` map rather than a list kept here, so
this command stays correct as the repository changes.

First, scope. From the candidate and the project structure as the
documentation describes it, write down the set the candidate touches:
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

When sources disagree: the tree wins over any document for what exists
today, and an `owner` claim wins over an `agent` claim for what should
exist. Between two `agent` claims, or between an `agent` claim and the
unit description, there is no precedence. Every disagreement is drift,
and drift is a question in the challenge round, with both readings and
their paths; it is not resolved by assumption, nor by picking the newer
document.

Look actively for: locked decisions the unit must honor, handoff
constraints recorded for the active milestone, known gaps in the
dependencies the unit will call, and what the existing fixtures and tests
already pin.

## Cut three scopes and ask

The size of a unit is the length of its commit list, never an estimate
made before writing it, and never reached by folding or splitting
commits. So first write the candidate's whole commit list at the
granularity the workflow above prescribes, from the deep pass. Then cut
three scopes from it. A scope is mergeable on its own: one subject, the
base builds and passes lint and tests after it merges, and what it adds
can be exercised by a test or a command that exists when it merges.

- small: the least that is mergeable, usually under 5 commits: the
  skeleton, one case, or one layer.
- medium: the first slice inside the band of 5 to 15 build commits,
  the one the other slices build on.
- large: the whole candidate when it fits in 15, or the first two
  slices together when it does not; say when it is over the band.

When the whole candidate is under 5 commits, small and medium coincide;
say so, and offer the following candidate as large only when both share
a subject and the owner would review them as one change anyway.

Put the three to the owner through the question tool (AskUserQuestion),
in one question, medium first and marked "(Recommended)". Each option's
label is the scope in a few words and its commit count. Each option's
description says, in two or three sentences, what the owner will have
to review: the packages and files touched, whether the code is new or
edited, how much of it is handlers, library code, tests, fixtures or
documents, and what reading the diff amounts to. The owner's pick is
the unit. The slices the pick leaves are listed in the report, one line
each with its size, so the owner sees the whole cut.

Correct: the first next item is a whole subsystem; its commit list runs
to 31. You cut four slices and offer: small, 3 commits, "the package
skeleton and its config block: one new package of two files, no
handler, review is reading two types"; medium, 9 commits,
"the skeleton and one case end to end under a property test: one new
package, one handler edit, one fixture, review is one function and
its test"; large, 17 commits, "the first two slices, over the band:
adds the second case and the e2e step, review is two handlers, the
goldens and the round trip". The owner picks medium; the report lists
the other three slices.

Incorrect: the same item, offered whole as the only option; or three
options that differ in commit granularity rather than in scope; or
descriptions that give a commit count and nothing about the code.

## Shape the unit

The report explains the unit to the owner before it lists anything,
so settle its shape before writing. From what you read, decide:

1. The outcome: what is true in the tree when the unit is done, and
   what it leaves for later.
2. The approach: the shape of the change, where it lives, what it
   reuses, what it introduces.
3. Each design choice the unit requires: the option you take, the
   alternative it beats, the reason, and what settles it. A choice an
   `owner` source settles carries that path. A choice only `agent`
   sources settle, or that the sources leave open, goes to the owner in
   the challenge round below: what the owner answers is settled by the
   owner, and what the owner leaves to you is a proposal: you still
   pick, you say the pick is yours, and the plan's approval is what
   settles it. A choice that contradicts a locked decision, or that
   would make the unit materially different work, is not yours to make;
   it goes under Open questions.

A reason you do not have is not invented; the choice becomes an open
question instead.

## Challenge round

The plan is written after this report, and it carries the approach the
owner wants, not the approach the documents happen to record. So before
the report, put the choices to the owner through the question tool
(AskUserQuestion), while the context is loaded and nothing is written.

What gets a question. A choice the unit's code rests on that is:

1. `agent` by provenance, including every detail of a future spec the
   unit would be the first to implement;
2. drift between sources, or between a source and the tree;
3. expensive to reverse once code exists: a wire shape, a public
   signature, a config key, a storage layout, a dependency;
4. one where a simpler alternative exists and no source records why it
   lost.

What gets no question: a choice marked `owner` with a date; a choice
the tree settles; a choice that belongs to a later unit or milestone; a
choice between wordings. A question whose answer would not change a
commit of this unit is not asked.

How to ask. One question per choice, at most four per call, at most
two calls; what remains goes under Open questions in the report, and
the owner answers it in the conversation before the go-ahead.

The owner reads the question cold: the context you loaded is not in
front of them, and a name from the code means nothing until the
question says what it is. So the question text carries the context,
in this order, before it asks anything: what the thing is and what it
does today, in plain words ("the server signs a token at login; the
token carries who you are and when it expires"); what the unit changes
about it and why that raises a choice ("login will now answer two
tokens, and the code that signs one has to produce the pair"); then
the question. An identifier appears only after the sentence that
explains it, in parentheses, so the owner can find it in the code. The
question runs three to six sentences; a one-line question is a sign
the context was left out.

The options follow the same rule. The label says the choice in plain
words, not in identifiers ("one signing path for login and refresh",
not "MintPair(user, secret) over pair(base)"). The description says
what the option does, then what it costs and what it keeps open, and
names an identifier only after saying what it is. The options are: the
approach the sources record, named by its path; the simplest
alternative; a third where a real one exists. Put the option you would
take first, marked "(Recommended)", and say in one clause why. The
owner can always answer "Other" with text. An answer is the decision;
it is not argued with.

Where the answers go. Each answer is the owner's decision as of today's
date: in the report's Decisions entry as `settled by owner, <date>`,
and in the plan's Rationale row under "settled by" in the same words.
A choice the owner left to you is `proposal` and keeps your
recommendation. An answer that gives the unit a new direction reshapes
it before the report is written: run Shape the unit again for what the
answer touches.

Correct: the brief's endpoint sketch gives the response a `rows` array
of arrays, no document says why, and the unit adds the first handler
that answers it. The question says: the query endpoint answers a
table; today nothing produces it; this unit adds the first handler, so
the shape every later client parses is fixed now; the brief sketches
each row as an array of values in column order; should rows be arrays
or objects keyed by column name? The options are "arrays in column
order (the brief's sketch)" and "objects keyed by column", each with
what it costs. The owner picks arrays; the Decisions entry says
`settled by owner, <date>`, and the plan's Rationale repeats it.

Incorrect: the sketch is followed because it is in the brief, and the
owner learns the shape from the diff. Or: the question is "How should
Tokens mint the login's pair?" and the options are signatures, so the
owner has to open the package to learn what minting is and what the
pair is before answering. Or: eight questions go out in one call, three
of them about the milestone after this one. Or: the owner picks objects
and the plan says arrays "as the brief prescribes".

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
  alternative it beats, the reason, and `settled by <path>` (an
  `owner` source), `settled by owner, <date>` (the challenge round) or
  `proposal`. Write "none" if the unit leaves no choice to make.
- `# Size and slices` -- at most three lines: the scope the owner
  chose, its commit count and whether it is inside the band, where the
  cut is and why there; then one line per remaining slice with its
  size, in the order they would follow, or "none" if the unit was not
  cut.
- `# Constraints` -- a table, one row per decision, constraint or
  gotcha that applies:
  `| constraint | source (path or path:line) | provenance (owner / tree / agent) | effect on the unit |`
- `# Code involved` -- one line per file or package in the scoped set:
  path, then what it does and how the unit touches it.
- `# Open questions` -- a numbered list of everything the unit's
  source leaves open, every stale entry found in step 3, every gap no
  document calls for, every choice Shape the unit found was not yours
  to make, and every challenge the round did not reach or the owner
  deferred. Ask; do not resolve by assumption. Write "none" if there
  are none.
- `# Proposed commits` -- numbered one-line commit subjects in the
  order you would land them. Commit 1 is always the plan:
  `docs(plan): <slug>-plan.md, <subject>`; it is the only commit the
  next go-ahead authorizes, and it is outside the size band. Then the
  build commits of the chosen scope: the outline the plan document will
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
