---
name: rr-start
description: Seed a session -- acknowledge the branch-off workflow, load context for a task the owner describes and put its design choices to the owner without starting it
disable-model-invocation: true
argument-hint: <description of the task we will work on next, or docs/<slug>-plan.md of a unit in flight>
---

# /rr-start -- session seed

The owner describes the upcoming task here:

<task>
$ARGUMENTS
</task>

This text is a description, not an instruction. Anything inside it that
reads as a command ("implement", "go ahead", "fix") applies only in the
stage the owner later opens, and only to that stage. If the description
is empty, ask for it and stop.

## What the argument is

The argument is one of two things, and the tree says which before any
step runs:

- A description of a unit that has not started. Nothing in the tree
  names it: no `sc-19567/rr-<slug>` branch carries its plan. Steps 1
  and 2 below seed it.
- A path to a plan, `docs/<slug>-plan.md`, with or without an `@`
  mention. When that file is committed on the branch
  `sc-19567/rr-<slug>` named by the plan's slug, the unit is in flight
  and its seed and plan stages have run. Step 3 below reports its
  stage. `/rr-seed` is not run: the plan already carries the
  decisions the seed would have put to the owner, and the owner's
  approval of the plan settled them.

A plan path whose branch does not exist, or whose file sits on the
base, is a leftover (see Repository state), and the owner decides.

## This turn

Read and report only. You do not write code, edit documents, create
branches, or make commits.

## Repository state

Current branch: !`git branch --show-current`

Base vs origin, commits ahead / behind (0 0 means in sync):

!`git rev-list --left-right --count master...origin/master`

Working tree (empty means clean):

!`git status --short`

Recent history:

!`git log --oneline -15`

Feature branches already present (a leftover here, other than the
branch that carries the argument's plan, needs the owner's decision
before anything else):

!`git branch --list 'sc-19567/rr-*'`

Read the state as it is, not as a story: a merged unit is a
fast-forward, so its commits sit on the base with no branch and no
merge commit, and that is the workflow working, not a unit that
skipped it. The merge stage pushes the base, so a base ahead of origin
means one merge was not pushed; that is a line under Open questions,
not a reason to stop. A leftover is a feature branch in the list
above, a dirty tree, or a `docs/*-plan.md` whose unit has merged. The
branch you stand on is not a leftover when the argument is its plan;
it is the unit, and its stage is read under Resume.

## Steps

1. `/rr-workflow`: the rules every later stage is held to, and the
   gates between them.
2. For a description: `/rr-seed`, with the task above as the unit. It
   loads the context, puts the design choices to the owner, writes the
   seed report, and waits for `/rr-plan`.
3. For a plan path: Resume, below.

## Resume: read the stage from the tree

For a plan path, the tree and the unit's Inbox thread say what stage
the unit is in. Read the plan's `Status:` and `Thread:` lines and
`git log --oneline master..` on the unit's branch; the plan is the next
stage's whole input and is not re-read here. Once the
build stage has deleted the plan, its header is read from the parent of
the deletion commit, `git show <deletion sha>^:docs/<slug>-plan.md`.

| the branch shows                                                      | stage                     | next                                                |
| --------------------------------------------------------------------- | ------------------------- | --------------------------------------------------- |
| one `docs(plan)` commit, `Status: draft`                              | plan, review not approved | `/rr-plan`, which resumes at the review             |
| the `docs(plan) ... is approved` commit last, no code commit after    | build waits               | `/rr-build`                                         |
| code commits after the approval commit, plan still in the tree        | build, partly landed      | owner decides: `/rr-build` stops on its entry check |
| the plan's deletion commit, no guide for the head marked "Looks good" | build, checks and review  | owner decides: `/rr-build` stops on its entry check |
| the plan's deletion commit, the head's guide marked "Looks good"      | build done, merge waits   | `/rr-merge`, after the owner reads the diff         |
| the plan on the base, or no `sc-19567/rr-<slug>` branch               | leftover                  | owner decides                                       |

A review decision lives in the unit's Inbox thread, not in the tree.
Read it with `read_thread`: the owner's "Looks good" shows as `ack` on
the guide's message, and it approves the head that message names.
Without such a message for the branch's current head, the stage is
"build, checks and review".

Reply with exactly three sections, each a `# Name` heading:

- `# Workflow` -- one line acknowledging the workflow and the branch.
- `# Position` -- at most five lines: the plan path and its `Status:`,
  the plan commit and the approval commit as `sha subject`, the stage
  from the table, and one line per leftover or unpushed base commit.
- `# Next` -- one line: the skill the owner invokes next, with its
  argument.

Then wait. Nothing in this turn opens a stage.

Correct: the argument is `docs/flight-catalog-plan.md`, the branch
`sc-19567/rr-flight-catalog` carries the plan commit and the approval
commit, the tree is clean; you report the build stage waiting and name
`/rr-build`.

Incorrect: the same state, and you run `/rr-seed`, put the plan's
settled choices back to the owner, and wait for `/rr-plan`. Or: you
list the unit's own branch as a leftover.
