---
name: rr-seed
description: Seed a session for one unit of work of the rest rewrite -- classify the sources by provenance, load the unit's context, shape it, put its design choices to the owner, and write the seed report. Loaded by rr-start and rr-start-next after they have fixed the unit; never invoked by the owner.
user-invocable: false
---

# Seed a unit

The caller has fixed the unit: a task the owner described, or the
scope the owner chose from `/rr-start-next`. This skill turns it into
the seed report and stops. It reads and reports only: no code, no
documents, no branches, no commits.

Work through the three references in this order, each read whole
before its step begins:

1. `context.md`: what the sources are, and how to load the unit's
   context from them.
2. `challenge.md`: how to shape the unit and put its choices to the
   owner.
3. `report.md`: the report, then the wait.

## What the sources are

Every document under `docs/`, every `AGENTS.md` and every plan was
written by an agent, in a session the owner reviewed at the level of a
decision, not at the level of every sentence. So a document settles a
choice only at the level of detail the owner could have reviewed as a
decision. Classify every claim the unit rests on by its provenance, and
carry the class through the steps and into the report:

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
a question for the challenge round. An `agent` claim the tree
contradicts, or that two documents state differently, is drift: the
same question, with the disagreement spelled out.

Correct: an accepted ADR's Decision says login answers a token pair,
and the log marks the refresh lifetime as an owner decision; both are
`owner`. The ADR's consequences name the config key and a sliding
expiry; the owner has not said so in those words, so both are `agent`,
and the unit that adds them asks before the plan is written.

Incorrect: the key and the expiry go into the plan as settled because
the log says the ADR "fixes the shapes".

A reason you do not have is not invented, in any step; the choice
becomes an open question instead.
