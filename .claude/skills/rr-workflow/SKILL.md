---
name: rr-workflow
description: The branch-off workflow of the rest rewrite -- the base branch, the feature branch, the commit form, and the four stages a unit of work moves through with the owner's invocation as the gate between them. Loaded by the seed skills and at the top of every stage skill; never invoked by the owner.
user-invocable: false
---

# The rest-rewrite workflow

## Branches and commits

- `sc-19567/rest-rewrite` is the base branch. Commit to it only by
  fast-forward merge from a reviewed feature branch. It is a flat,
  linear history; it stays that way.
- Every unit of work happens on a feature branch off the base, named
  `sc-19567/rr-<slug>`. Create it only when the owner opens the plan
  stage, from an up-to-date base, with a clean working tree.
- Commits on the feature branch are small and individual: one
  conventional-commit line, `type(scope): subject`, no body, no
  trailers, no AI attribution. Prefer many small commits over one big
  one; each commit builds and passes lint.
- The owner reviews by diffing the branch against the base
  (`git diff sc-19567/rest-rewrite...sc-19567/rr-<slug>`).
- No GitHub pull requests, no stacked branches, no history rewriting on
  the base.

## Lifecycle: four stages, three gates

Every unit of work moves through these stages in order. Each stage is a
skill the owner invokes; the invocation is the gate. A chat message
("go", "ok", "yes", "proceed", "looks good", a thumbs up) opens no
stage, whatever its wording: it answers a question inside the current
stage, or it is a review comment on what the current stage produced.

| Stage    | Opened by                              | You produce                                                                                                                                                                              | You never                                                                                                                  | Ends with                                                                                                                                                            |
| -------- | -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Seed  | `/rr-start <task>` or `/rr-start-next` | the seed report                                                                                                                                                                          | write, branch, commit                                                                                                      | a wait for `/rr-plan`                                                                                                                                                |
| 2. Plan  | `/rr-plan`                             | the branch and exactly one commit: the plan document                                                                                                                                     | touch code, tests, config, ADRs or any other document                                                                      | the plannotator gate on the committed plan; on approval, the plan-stage message, then a wait for `/rr-build`                                                         |
| 3. Build | `/rr-build docs/<slug>-plan.md`        | the small commits the approved plan lists, the doc-discipline runs, the plan's deletion commit, the push, the Buildkite build and the plannotator code review, the last two side by side | merge; deviate from the plan without saying so; invent a reason for a comment; ask about merging before the build is green | the build-stage message: branch, summary, the check's report, the review's decision, the build number and state, the `git diff` command, then a wait for `/rr-merge` |
| 4. Merge | `/rr-merge`                            | the fast-forward merge, the branch deletion                                                                                                                                              | merge without the owner having invoked `/rr-merge` on a green, approved build; merge a branch that still carries its plan  | one line: what merged                                                                                                                                                |

Open questions the owner left unanswered are not answered by a stage
opening either; the plan records your recommendation for each, and the
plan's approval in the plannotator gate is what settles them.

Correct: the seed report is posted and the owner writes "looks good".
You answer that you are waiting for `/rr-plan`, and do nothing else.

Incorrect: the seed report is posted, the owner writes "go", and you
create the branch and commit the plan, because "go" sounded like the
gate. Or: the plannotator gate returns `approved` and you start the
first code commit, because the plan is approved. A small change and a
settled design do not shorten the lifecycle; each gate exists so the
owner reviews one stage's output on its own.
