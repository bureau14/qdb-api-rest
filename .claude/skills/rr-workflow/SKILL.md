---
name: rr-workflow
description: The workflow of the rest rewrite -- the story, the branch, the commit form, the pull request, and the four stages a unit of work moves through with the owner's invocation as the gate between them. Loaded by the seed skills and at the top of every stage skill; never invoked by the owner.
user-invocable: false
---

# The rest-rewrite workflow

## Branches and commits

A unit follows the QuasarDB workflow (the `quasardb-workflow` skill),
and the rr stages add the seed, the plan and the owner's reviews in the
Inbox to it.

- `master` is the base branch, and `3.14.x` is the current release.
  Commits reach `master` only as the squash commit of a unit's pull
  request, and the same commit is cherry-picked onto `3.14.x`.
- Each unit is one Shortcut story under epic 19567, created when the
  owner opens the plan stage. Its branch is `sc-<id>/<kebab-title>`,
  the name the shortcut skill's `sc-story.sh` prints, made from
  `origin/master` with clean tracked files.
- Each unit has a plan, `docs/<slug>-plan.md`. The seed report chooses
  the slug, and the build stage finds the plan as the one plan file the
  branch adds over `origin/master`.
- Commits on the branch are small and individual: one
  conventional-commit line, `type(scope): subject`, no body, no
  trailers, no AI attribution. Prefer many small commits over one big
  one; each commit builds and passes lint.
- The unit lands as one pull request to `master`, titled
  `QDB-<id> - <story title>`, with an empty body, and Igor
  (`igorniebylski`) reviews it. The owner reviews the plan and the code
  in the Plannotator Inbox (The Inbox, below). The code review's patch
  is `git diff origin/master...HEAD`.
- No stacked branches, and no history rewriting on `master` or
  `3.14.x`.

## Tools

The generic `buildkite` skill owns how a build is ensured for a commit
past the pipeline's branch filter, how its queued jobs are
reprioritized, how it is watched and how its state is read back. The
stage skills say what to do with a build; the skill says how, and it
takes these values from this repository:

| Parameter      | Value                                                                                 |
| -------------- | ------------------------------------------------------------------------------------- |
| Pipeline slug  | `qdb-api-rest`                                                                        |
| Org            | `quasar-1`, resolved from the repository's `.bk.yaml`; nothing to set                 |
| Branch         | the story branch, `sc-<id>/<kebab-title>`                                             |
| Commit         | the full SHA of the head, `git rev-parse HEAD`                                        |
| `MESSAGE`      | `rr-<slug>`, so the pipeline's build list reads by unit                               |
| `CHECKPOINT`   | `7200`, because the watcher runs beside the owner's code review, which can take hours |
| Control branch | `master`, when the skill says to check a failure against an unchanged base            |

The builds of a head, with their states, are read with the skill's
one-liner in "Inspecting builds and jobs". `/rr-merge` quotes it in its
entry check because the harness evaluates that check at load time.

The reviews go through the `plannotator_inbox` tool:

| Review      | Calls                                     | Values here                                                       |
| ----------- | ----------------------------------------- | ----------------------------------------------------------------- |
| Plan review | `send_message`, the plan in `attachments` | `docs/<slug>-plan.md`, one message                                |
| Code review | `get_guide_brief`, then `submit_guide`    | patch `git diff origin/master...HEAD`, the head's SHA in the body |

The `quasardb-workflow` skill owns the story, the branch, the pull
request, the squash-merge and the cherry-pick onto `3.14.x`, and it
reads the `shortcut` and `github` skills for their mechanics. The stage
skills say which of its steps a stage runs. The story goes under epic
19567, the rewrite's epic, and not under the release epic that
`quasardb-workflow` picks by default.

Correct: the build stage runs the buildkite skill's watcher with slug
`qdb-api-rest`, the feature branch, the head's SHA,
`MESSAGE=rr-flight-catalog` and `CHECKPOINT=7200`, in the background.

Correct: the build stage pushes the story branch and opens the pull
request through `quasardb-workflow`, step 4, before it ensures the
build.

Incorrect: the owner writes "merge" in the chat, and the session runs
`quasardb-workflow`, step 7, because the skill says "merge" starts it.
Only `/rr-merge` opens the merge of a unit.

## The Inbox

The owner answers questions and reviews in the Plannotator Inbox,
through the `plannotator_inbox` tool. Each unit has one thread, and
every question and review of the unit goes into it. The first message
of the unit names the thread with `thread`, in a few words saying what
the work is. The seed report and the plan's `Thread:` line record the
name, and every later message passes it. After you send something whose
answer the next step needs, end your turn. The owner's reply wakes the
session as a message headed `Plannotator Inbox: <title> (<reply id>)`,
and you answer into the thread with `reply_to` set to that id.

A question is a `:::question` block, or `:::question-multi` for a pick
of several, with its choices in natural order and your pick on a
`Recommended:` line after them, never in a label.

A review message carries the plan or the guide and no question block,
because the owner can mark a message "Looks good" only when it has no
open question. The owner can also mark only the latest message of a
thread, so while a review waits you post nothing else into that thread.
Reports during the wait, such as a watcher's exit, go to the
conversation.

| The owner                                  | Means                                      |
| ------------------------------------------ | ------------------------------------------ |
| marks the review message "Looks good"      | approved                                   |
| replies, in words or with annotations      | changes requested, whatever the words say  |
| resolves the thread without a "Looks good" | stop and report; the owner decides in chat |

The wake for a "Looks good" names the message it marks. It approves a
review only when that id is the review message's id. An earlier
approval is read back with `read_thread`, which shows `ack` on the
marked message, never from memory of having seen it.

Correct: you send the plan as one message, end your turn, and the owner
marks it "Looks good". The wake names that message's id, and you commit
the approved status.

Incorrect: a watcher exits while the code review waits, and you post its
result into the unit's thread, so the review message is no longer the
latest and the owner cannot mark it "Looks good". Or: the owner replies
"fine, go ahead" and you treat it as an approval, because it reads like
one.

## Lifecycle: four stages, three gates

Every unit of work moves through these stages in order. Each stage is a
skill the owner invokes; the invocation is the gate. A chat message
("go", "ok", "yes", "proceed", "looks good", a thumbs up) opens no
stage, whatever its wording: it answers a question inside the current
stage, or it is a review comment on what the current stage produced. A
"Looks good" on an Inbox review message approves that review, as The
Inbox says, and opens no stage either.

| Stage    | Opened by                              | You produce                                                                                                                                                               | You never                                                                                                                                                               | Ends with                                                                                                                                                                          |
| -------- | -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. Seed  | `/rr-start <task>` or `/rr-start-next` | the seed report                                                                                                                                                           | write, branch, commit                                                                                                                                                   | a wait for `/rr-plan`                                                                                                                                                              |
| 2. Plan  | `/rr-plan`                             | the story, the branch, one commit with the plan document, its Inbox review, and on approval one commit setting its status to approved                                     | touch code, tests, config, ADRs or any other document                                                                                                                   | the Inbox review of the committed plan; on approval, the plan-stage message, then a wait for `/rr-build`                                                                           |
| 3. Build | `/rr-build`                            | the small commits the approved plan lists, the doc-discipline runs, the plan's deletion commit, the push, the pull request, the Buildkite build and the Inbox code review | merge; deviate from the plan without saying so; invent a reason for a comment; ask about merging before the build is green                                              | the build-stage message: branch, pull request, summary, the check's report, the review's decision, the build number and state, the `git diff` command, then a wait for `/rr-merge` |
| 4. Merge | `/rr-merge`                            | the squash-merge, the cherry-pick onto `3.14.x`, the builds of both lines, the story completed                                                                            | merge without the owner having invoked `/rr-merge` on a green build that the owner and the pull request's reviewer approved; merge a branch that still carries its plan | one line: what merged on both lines                                                                                                                                                |

A session may begin at any stage from the tree. `/rr-start` with a
plan path reads the unit's stage and names the next gate; it opens
nothing and seeds nothing. `/rr-build` and `/rr-merge` take the tree
as their whole input and need no seed or plan conversation.

Open questions the owner left unanswered are not answered by a stage
opening either; the plan records your recommendation for each, and the
owner's "Looks good" on the plan review is what settles them.

Correct: the seed report is posted and the owner writes "looks good".
You answer that you are waiting for `/rr-plan`, and do nothing else.

Incorrect: the seed report is posted, the owner writes "go", and you
create the branch and commit the plan, because "go" sounded like the
gate. Or: the owner marks the plan review "Looks good" and you start
the first code commit, because the plan is approved. A small change and a
settled design do not shorten the lifecycle; each gate exists so the
owner reviews one stage's output on its own.
