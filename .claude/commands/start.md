---
description: Seed a session -- acknowledge the branch-off workflow and load context for an upcoming task without starting it
argument-hint: <description of the task or topic we will work on next>
---

# /start -- session seed

The upcoming task, described by the owner (NOT to be started yet):

> $ARGUMENTS

This command does two things and nothing else: (1) commits you to the
branch-off workflow below, (2) loads the context the task will need.
You do not write code, edit documents, create branches or commits. You
end by reporting what you learned and waiting for the owner.

## Workflow (binding for the whole session)

- `sc-19567/rest-rewrite` is the base branch. Never commit to it
  directly. It is a flat, linear history; it stays that way.
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

Working tree (empty means clean):

!`git status --short`

Recent history:

!`git log --oneline -15`

Feature branches already present (a leftover here needs the owner's
decision before anything else):

!`git branch --list 'sc-19567/rr-*'`

## Load context

Read, in this order, following `docs/AGENTS.md` ("Read this first"):

1. `docs/brief.md` in full: scope, locked decisions, compatibility
   contract, project structure.
2. `docs/log.md`, the Current state block: active milestone, in flight,
   next, handoff, blocked.
3. Every `docs/*-plan.md` that covers the task, and every ADR in
   `docs/adr/` that the brief, the plan or the task touches.
4. The `AGENTS.md` of every folder the task will touch (`internal/`,
   `cmd/qdb_rest/`, `tests/e2e/`, `scripts/cicd/`, `.buildkite/`), plus
   `internal/AGENTS.md` whenever Go code is involved.
5. The code the task will touch or depend on: read the packages, their
   tests, and the call sites, top to bottom. For the vendored
   `qdb-api-go`, read the parts of the binding the task will call.
6. The history of those paths: `git log --oneline -- <paths>` and the
   diffs of the commits that shaped them, so the task continues the
   existing direction instead of restarting it.
7. Memory: any recalled note about this repository that bears on the
   task, verified against the tree before relying on it.

Look actively for: locked decisions the task must honor, constraints in
the handoff block, upstream `qdb-api-go` gaps the task will hit, what the
e2e goldens and the bench already pin, and anything in the task
description that contradicts the brief or an ADR.

## Report, then stop

Reply with, in this order, briefly:

1. One line acknowledging the workflow and the feature branch name you
   will use (`sc-19567/rr-<slug>`), not yet created.
2. Where the project is: milestone, what is in flight, what the log says
   comes next, and how the task relates to that.
3. What you read and what it means for the task: the constraints,
   decisions and gotchas that apply, each with its source document, and
   the code and tests involved.
4. Anything the task description contradicts or leaves open, as
   questions for the owner. Do not resolve them by assumption.
5. A short proposed plan of commits, as a numbered list of one-line
   commit subjects, so the owner can redirect before any work starts.

Then wait. Do not begin the task until the owner says so.
