---
name: rr-start-next
description: Seed a session with the next unit of work -- survey docs, history and tree, offer three scopes of the next unit, load its context and put its design choices to the owner without starting it
disable-model-invocation: true
---

# /rr-start-next -- session seed, unit chosen for you

This skill takes no task description. You work out which unit of work
comes next, cut it into three scopes for the owner to choose from, and
then seed the session for the chosen scope exactly as `/rr-start` would
have, had the owner described that unit.

## This turn

Read and report only. You do not write code, edit documents, create
branches, or make commits.

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

Read the state as it is, not as a story: a merged unit is a
fast-forward, so its commits sit on the base with no branch and no
merge commit, and that is the workflow working, not a unit that
skipped it. The merge stage pushes the base, so a base ahead of origin
means one merge was not pushed; that is a line under Open questions,
not a reason to stop. A leftover is a feature branch in the list
above, a dirty tree, or a `docs/*-plan.md` whose unit has merged.

## Steps

1. `/rr-workflow`: the rules every later stage is held to, and the
   gates between them.
2. Choose the unit (below).
3. `/rr-seed`, with the candidate as the unit, through its context
   step only; return here before shaping it.
4. Cut three scopes and ask (below). The owner's pick is the unit.
5. The rest of `/rr-seed` for the chosen scope: shape, challenge round,
   report, then the wait for `/rr-plan`. The report's Candidates and
   Size and slices sections are present, because steps 2 and 4 ran.

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

## Cut three scopes and ask

The size of a unit is the length of its commit list, never an estimate
made before writing it, and never reached by folding or splitting
commits. So first write the candidate's whole commit list at the
granularity the workflow prescribes, from the context step. Then cut
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
