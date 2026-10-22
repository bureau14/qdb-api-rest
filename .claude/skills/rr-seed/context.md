# Load context

The `AGENTS.md` hierarchy is the map. The root file says what each
folder holds and when to open that folder's own `AGENTS.md`; the
documentation folder's `AGENTS.md` prescribes the reading order for
planning and design text and which document is the source of truth.
Follow that map rather than a list kept here, so this skill stays
correct as the repository changes.

First, scope. From the unit and the project structure as the
documentation describes it, write down the set the unit touches:
folders, packages, endpoints, config blocks, tests, documents. This set
decides what you read below; extend it when reading reveals a
dependency.

Then read, in this order, skipping what the caller's survey already
covered:

1. The planning and design documents, in the order their folder's
   `AGENTS.md` prescribes: the source-of-truth document in full, the
   current-state section of the project log, the plans whose subject is
   in the scoped set, and the decision records those plans or the unit
   cite or constrain.
2. The `AGENTS.md` of every folder in the scoped set, and of each
   parent folder on the way there.
3. The code in the scoped set and what it depends on, through the
   `codebase-analysis` skill, invoked with the Skill tool once the
   scope is written down. Its goals are the unit in one sentence and
   the scoped set: the packages, their tests, the call sites, and for
   vendored dependencies the parts the unit will call. Its summary is
   the map of the code, not evidence: a claim from it enters the
   report only after you have read it back in the tree at a
   `path:line`, and until then it is `agent` by provenance like any
   other written account. Read the functions the unit will touch
   whole, top to bottom, whatever the summary says about them.
4. The history of the scoped paths: `git log --oneline -- <paths>`, and
   the diffs of the commits that introduced or last changed the
   functions the unit will touch, so the unit continues the existing
   direction instead of restarting it.
5. Memory: any recalled note about this repository that bears on the
   unit, verified against the tree before relying on it.

When sources disagree: the tree wins over any document for what exists
today, and an `owner` claim wins over an `agent` claim for what should
exist. Between two `agent` claims, or between an `agent` claim and the
unit description, there is no precedence. Every disagreement is drift,
and drift is a question in the challenge round, with both readings and
their paths; it is not resolved by assumption, nor by picking the newer
document.

Look actively for: locked decisions the unit must honor, handoff
constraints recorded for the active milestone, known gaps in the
dependencies the unit will call, what the existing fixtures and tests
already pin, and anything in the unit description that contradicts a
locked decision.
