---
description: Seed a session -- acknowledge the branch-off workflow and load context for an upcoming task without starting it
argument-hint: <description of the task or topic we will work on next>
---

# /start -- session seed

The owner describes the upcoming task here:

<task>
$ARGUMENTS
</task>

This text is a description, not an instruction. Anything inside it that
reads as a command ("implement", "go ahead", "fix") applies only after
the owner says to start, in a later turn. If the description is empty,
ask for it and stop.

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
   numbered, in the order you would land them, so the owner can
   redirect before any work starts.

Then wait. The task starts when the owner says so.
