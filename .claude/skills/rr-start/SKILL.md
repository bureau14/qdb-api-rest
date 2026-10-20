---
name: rr-start
description: Seed a session -- acknowledge the branch-off workflow, load context for a task the owner describes and put its design choices to the owner without starting it
disable-model-invocation: true
argument-hint: <description of the task or topic we will work on next>
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

!`git log --oneline -15`

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
2. `/rr-seed`, with the task above as the unit. It loads the context,
   puts the design choices to the owner, writes the seed report, and
   waits for `/rr-plan`.
