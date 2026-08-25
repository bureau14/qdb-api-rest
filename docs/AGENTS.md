# docs/ -- Conventions

Scope of this file: everything under `docs/`. Repository-wide rules and
the hierarchy convention are in the root `AGENTS.md`.

This project is developed by LLM agents over roughly three months. Every
session starts cold. These conventions exist so that any agent can recover
scope, intent, current state, and standing constraints without re-deriving
them or re-reading everything. The documents are forward-looking: they
describe what is true and what comes next, not how we got here.

## Read this first, in this order

1. `brief.md` -- scope, intent, locked decisions. Source of truth.
2. `log.md` -- the **Current state** block at the top: which milestone,
   what is in flight, what is next, what the next milestone must honor,
   what is blocked.
3. The plan document for whatever you are about to touch (`*-plan.md`),
   the `AGENTS.md` of the folder you are about to work in, and any ADR
   either references.
4. `log.md` dated entries and `adr/` only as needed.

## What lives where (one writer per fact)

| Kind of information                                                              | Lives in                       | Update rule                                     |
| -------------------------------------------------------------------------------- | ------------------------------ | ----------------------------------------------- |
| Scope, goals, non-goals, architecture                                            | `docs/brief.md`                | Rarely; only when scope or intent changes       |
| Hard design decisions; anything that constrains code, build or CI until reversed | `docs/adr/NNNN-<slug>.md`      | Append-only; supersede, never rewrite           |
| Per-topic specification, verified facts, measurement mechanics                   | `docs/<topic>-plan.md`         | Edit in place; date verified facts              |
| Micro-decisions local to a topic                                                 | decision-log table in the plan | Append rows                                     |
| How to work in a folder; what not to try there; gotchas                          | that folder's `AGENTS.md`      | Edit in place; state as rules, never as history |
| Lifecycle of a document                                                          | `Status:` line in that doc     | Vocabulary below                                |
| Progress, milestone criteria, handoff notes for the next milestone               | `docs/log.md` Current state    | Rewrite in place; the only place progress goes  |
| Scope, decision and milestone events, dated                                      | `docs/log.md` dated entries    | Append-only; one to three lines; link out       |

Recorded nowhere, because something else already records it:

- What landed: `git log`. Commit messages are the record; do not
  transcribe them into a document.
- Test, lint and CI results: CI and the test suite.
- Measured numbers: result files (`tests/e2e/bench/results/`), never a
  document.
- Investigation narratives, hypotheses ruled out, fixes reverted: only
  the resulting fact (plan) or rule (`AGENTS.md`) survives.

The rule that keeps this from rotting: a fact has exactly one home.
Before writing a sentence into any document, name its row in the table
above. If the row is not the document you are editing, write it there
and leave at most a link behind. Progress never goes into the brief or
the plans; verified facts and lessons never go into the log.

## Status vocabulary

The `Status:` line at the top of every document is **lifecycle**, not
progress:

- `draft` -- being written; may change shape entirely.
- `approved` -- direction agreed; implementation may or may not have
  started. Progress is in `docs/log.md`, not here.
- `implemented` -- the thing the document describes exists and matches
  the document.
- `retired` -- the thing no longer exists (e.g. the temporary bench).
  Keep the document; before retiring it, move any mechanics still worth
  keeping to the plan or `AGENTS.md` that outlives it, then add a
  one-line log entry.
- ADRs use `proposed | accepted | superseded by NNNN`.

Do not add checkboxes, "TODO", or "done" markers to the brief or the plans.
If you need to record that a plan step is finished, update the Current
state block in `docs/log.md`.

## The log

`docs/log.md` has two parts.

### Current state (top, rewritten in place)

The block an agent reads instead of the history. Sections, in order:

- **Milestone table**: state and a one-line note per milestone. Entry
  and exit criteria of the active milestone live here (or in a short
  list below the table), never in a dated entry.
- **In flight**: work actually underway. Not an inventory of what has
  landed -- that is `git log`.
- **Next**: ordered, concrete, each item startable as written.
- **Handoff**: constraints and notes the next milestone must honor,
  each with the document that owns the underlying fact. Delete an item
  when the work that consumes it lands.
- **Blocked on**.

The block must be readable without opening any entry: no "see the
YYYY-MM-DD entry" pointers. If something in an entry is still needed,
it belongs in this block or in the document that owns it.

### Entries (append-only, newest first)

One entry per event that changes scope, a decision, or a milestone
state. One to three lines: what changed, and where the authoritative
record is. Template:

```
## 2026-08-24 -- e2e harness excluded from CI

- Owner decision; ADR-0003. Re-adding recipe: `.buildkite/AGENTS.md`.
```

Goes in an entry:

- a milestone started, finished, or changed scope (link the brief);
- an ADR accepted or superseded (link it);
- an owner decision that changes direction (one line, plus the plan
  section or ADR that records it);
- a document retired.

Does not go in an entry:

- `Landed:` inventories of packages, flags or features (`git log`);
- `Verified:` test, lint or CI outcomes (CI);
- `Lesson:` / `Lessons:` blocks (the folder's `AGENTS.md`, or the plan);
- investigation narratives, hypotheses ruled out, fixes reverted;
- measured numbers (result files);
- restatements of a decision already recorded in a plan or ADR;
- notes for a future milestone (Current state, Handoff);
- milestone criteria (Current state).

Before appending an entry, apply the routing table to every sentence:
if a sentence's row is not "log entries", move it to its home and keep
at most the link. An entry that would exceed three lines is carrying
something that belongs elsewhere.

## ADRs

`docs/adr/` holds decisions that were hard to make, expensive to reverse,
that constrain code, build or CI until reversed, or that a future agent
might otherwise re-litigate. Micro-decisions stay in the plan's
decision-log table. Template: `docs/adr/0000-template.md`. The brief's
"Compatibility contract", "Cluster binding", and "Flight SQL vs gRPC"
sections are pre-project decisions and stay in the brief; anything
decided from M0 onward is an ADR.

An `AGENTS.md` or plan that needs to justify a rule cites the ADR, the
brief, or a plan section -- never a log entry.

## Style

ASCII only, no emojis, `--` for dashes; `npx prettier --write` after
touching any Markdown file. Documents read top to bottom; definitions
before use.

## Maintenance

The rules above are enforced after the fact by the `doc-cleanup` skill
(`.claude/skills/doc-cleanup/SKILL.md`): run it at every milestone
boundary and whenever the log has grown by more than a few entries.
