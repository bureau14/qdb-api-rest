# docs/ -- Conventions

Scope of this file: everything under `docs/`. Repository-wide rules and
the hierarchy convention are in the root `AGENTS.md`.

This project is developed by LLM agents over roughly three months. Every
session starts cold. These conventions exist so that any agent can recover
scope, intent, current state, and hard-won lessons without re-deriving them
or re-reading everything.

## Read this first, in this order

1. `brief.md` -- scope, intent, locked decisions. Source of truth.
2. `log.md` -- the **Current state** block at the top: which milestone,
   what is in flight, what is next, what is blocked.
3. The plan document for whatever you are about to touch (`*-plan.md`),
   and any ADR it references.
4. `log.md` dated entries and `adr/` only as needed.

## What lives where (one writer per fact)

| Kind of information                     | Lives in                       | Update rule                                    |
| --------------------------------------- | ------------------------------ | ---------------------------------------------- |
| Scope, goals, non-goals, architecture   | `docs/brief.md`                | Rarely; only when scope or intent changes      |
| Hard design decisions                   | `docs/adr/NNNN-<slug>.md`      | Append-only; supersede, never rewrite          |
| Per-topic specification, verified facts | `docs/<topic>-plan.md`         | Edit in place; date verified facts             |
| Micro-decisions local to a topic        | decision-log table in the plan | Append rows                                    |
| Lifecycle of a document                 | `Status:` line in that doc     | Vocabulary below                               |
| **Progress** (done / in flight / next)  | `docs/log.md` Current state    | Rewrite in place; the only place progress goes |
| Post-mortems, notes to future self      | `docs/log.md` dated entries    | Append-only, dated                             |
| Cross-cutting lessons                   | `docs/log.md` dated entries    | Append-only, dated                             |

The rule that keeps this from rotting: a fact has exactly one home.
Progress never goes into the brief or the plans; verified topic facts never
go into the log (link to the plan section instead).

## Status vocabulary

The `Status:` line at the top of every document is **lifecycle**, not
progress:

- `draft` -- being written; may change shape entirely.
- `approved` -- direction agreed; implementation may or may not have
  started. Progress is in `docs/log.md`, not here.
- `implemented` -- the thing the document describes exists and matches
  the document.
- `retired` -- the thing no longer exists (e.g. the temporary bench).
  Keep the document; its lessons move to `docs/log.md` before retirement.
- ADRs use `proposed | accepted | superseded by NNNN`.

Do not add checkboxes, "TODO", or "done" markers to the brief or the plans.
If you need to record that a plan step is finished, update the Current
state block in `docs/log.md`.

## The log

`docs/log.md` has two parts:

1. **Current state** (top, rewritten in place, kept short): milestone
   table, in-flight items, next steps, blocked-on. An agent that reads
   only this block knows where to start.
2. **Entries** (below, append-only, newest first, `## YYYY-MM-DD -- title`):
   what was done, what was learned, what surprised us, what not to try
   again. Write an entry at the end of any session that changed state or
   learned something; write one _before_ retiring a document.

Entries link out (`docs/e2e-plan.md#dataset`) rather than duplicating.

## ADRs

`docs/adr/` holds decisions that were hard to make, expensive to reverse,
or that a future agent might otherwise re-litigate. Micro-decisions stay in
the plan's decision-log table. Template: `docs/adr/0000-template.md`. The
brief's "Compatibility contract", "Cluster binding", and "Flight SQL vs
gRPC" sections are pre-project decisions and stay in the brief; anything
decided from M0 onward is an ADR.

## Style

ASCII only, no emojis, `--` for dashes; `npx prettier --write` after
touching any Markdown file. Documents read top to bottom; definitions
before use.
