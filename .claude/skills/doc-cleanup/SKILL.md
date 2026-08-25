---
name: doc-cleanup
description: Periodic maintenance of this repository's documentation -- docs/log.md, the plans, ADRs, every AGENTS.md, READMEs and code comments -- against the routing rules in docs/AGENTS.md. Use when asked to clean up, audit, tidy or de-fluff the docs, at a milestone boundary, or after a burst of log entries.
---

# doc-cleanup

Every document in this repository has one job and one set of rules,
defined in `docs/AGENTS.md` ("What lives where", "The log", "ADRs",
"Status vocabulary"). This skill does not restate those rules; it is the
procedure for enforcing them after the fact. Read `docs/AGENTS.md` in
full before starting, every time -- it is the law, this file is the
inspection routine.

The documents are forward-looking. The target state after a cleanup:
every document reads as if it had been written correctly from scratch
today, by someone who never saw the history.

## Modes

- **report** (default): produce the findings list below, change nothing.
- **apply**: the owner has confirmed the report (or explicitly asked for
  apply); make the changes, one document at a time, then run the
  finishing checks. Never apply without a report having been shown in
  the same or a previous session.

## Scope

In scope, and only these:

```
docs/brief.md  docs/*-plan.md  docs/log.md  docs/adr/*.md  docs/adr/README.md
AGENTS.md (every one)  CLAUDE.md (every one)
tests/e2e/README.md  tests/e2e/bench/README.md
comments in: cmd/ internal/ scripts/cicd/ .buildkite/*.py tests/e2e/*.sh
             tests/e2e/bench/*.py tests/e2e/bench/protocols tests/e2e/bench/servers
             Makefile tests/e2e/Makefile tests/e2e/bench/Makefile
```

Never touched: `vendor/`, `qdb/`, `scripts/tests/setup/`,
`.buildkite/tools/`, `tests/e2e/.old-master/`, anything gitignored,
golden fixtures, result files.

## Pass 0 -- inventory

```bash
find . \( -path ./vendor -o -path ./qdb -o -path ./scripts/tests/setup \
  -o -path ./.buildkite/tools -o -path ./tests/e2e/.old-master -o -name .env \) -prune \
  -o \( -name AGENTS.md -o -name CLAUDE.md -o -name README.md -o -path './docs/*.md' \) -print
```

For every `AGENTS.md` found, a sibling `CLAUDE.md` must exist containing
exactly `@AGENTS.md`; for every `CLAUDE.md`, a sibling `AGENTS.md`.
Every `AGENTS.md` except the root one must have a one-line row in its
parent's `AGENTS.md`. Every ADR file must have a row in
`docs/adr/README.md` and vice versa.

## Pass 1 -- docs/log.md

Current state block. Each of these is a finding:

- "In flight" lists things that have landed (`git log` has them).
- Any pointer into an entry ("see the 2026-.. entry", "(see .. entry)").
- Milestone-table notes that are test, lint or CI outcomes ("green",
  "N/M pass", build numbers) instead of state.
- Entry/exit criteria of the active milestone missing from the block, or
  present in a dated entry instead.
- No Handoff section, or a Handoff item whose consuming work has landed
  (verify against the code, then delete the item).
- A Handoff item that does not name the document owning its fact.
- A Next item that is not startable as written (needs a lookup first).

Entries. Grep first, then read every entry:

```bash
grep -nE '^\s*-?\s*(Landed|Verified|Lessons?|Facts verified|Decisions worth keeping|Not verified):' docs/log.md
grep -nE '\b(previously|earlier today|same day|supersedes|that entry stays|ruled out|wrong first fix|reverted)\b' docs/log.md
grep -nE '[0-9]+(\.[0-9]+)? ?(s|ms|MB|MiB|GB|GiB|B|%)\b' docs/log.md   # measured numbers
```

An entry is a finding when it exceeds three lines, contains any of the
above, restates a decision that a plan or ADR already records in full,
contains a note for a future milestone, or records no scope, decision or
milestone event at all ("scaffolding added", "planning discussed").

Resolution, per sentence, using the routing table in `docs/AGENTS.md`:

1. Name the sentence's row in the table.
2. If the row is not "log entries": grep the owning document for the
   fact. Present there -> delete the sentence. Absent -> move it there,
   rewritten as a rule or a present-tense fact, then delete it here.
3. What remains is the event and a link to its record. Never change an
   entry's date or the event it records; entries are append-only as a
   record of events, not as a record of their wording.

## Pass 2 -- brief and plans

For `docs/brief.md` and every `docs/*-plan.md`:

- `Status:` line uses the lifecycle vocabulary and is true (a plan whose
  subject is fully built and matching says `implemented`).
- Plans are ephemeral (`docs/AGENTS.md`, Plans). A plan whose work has
  landed or been abandoned is a finding: list every fact in it that
  lacks a permanent home (brief, ADR, folder `AGENTS.md`/README), the
  home each should move to, and propose deleting the plan once moved.
  Never delete a plan alone; the owner confirms.
- When a plan's deletion is proposed, every link to it outside `docs/`
  (`grep -rn '<name>-plan.md'`) is part of the finding: each needs a
  new target or the fact moved next to it.
- No progress markers: checkboxes, "TODO", "done", "pending", "once M1
  lands" phrased as status rather than as a dependency.
- Temporal contamination -- rewrite as the present-tense fact, keeping
  the date only where the convention wants one (verified facts,
  decision-log rows):

  ```bash
  grep -nE '\b(previously|no longer|used to|contrary to what|old plan|now that|as of|recently|still)\b' docs/brief.md docs/*-plan.md
  ```

  "X is Y (verified 2026-08-24, contrary to what this bullet previously
  claimed)" becomes "X is Y (verified 2026-08-24)". A superseded
  statement is replaced, never annotated.

- Measured numbers: a plan holds mechanics, not results. Any table or
  sentence with timings, byte counts or RSS figures is a finding unless
  the plan explicitly designates that section as a reference baseline
  and the owner has confirmed the exception. Flag; do not resolve alone.
- Cross-references: every "see `<doc>`, <section>" names a section
  heading that exists (`grep -n '^## '` the target). A plan never cites
  a log entry.
- Decision-log rows are append-only. A row whose "Rejected" column
  describes what the project has since adopted is a finding: propose a
  superseding row, never edit or delete the old one.
- Facts the code contradicts (a Makefile target, flag, port or file the
  plan names does not exist): verify with `ls`, `grep`, `make -n`, then
  flag with the evidence.

## Pass 3 -- ADRs

- `Status:` is `proposed | accepted | superseded by ADR-NNNN`; a
  superseded ADR names its successor and the successor names it.
- No progress, no "implemented in commit ..", no measured numbers.
- Body is never rewritten. A wrong ADR is superseded by a new one.
- ADR candidates without an ADR: grep the log, the plans and every
  `AGENTS.md` for owner decisions that "constrain code, build or CI
  until reversed" (the `docs/AGENTS.md` criterion) and have no ADR.
  Report them as candidates with the sentence that records them today;
  writing the ADR is an owner decision.

## Pass 4 -- AGENTS.md files

Rules, not history. For each file:

- Every bullet reads as a standing rule or a present-tense fact. Dates,
  "was", "used to", "we found", "after the fix", build numbers and
  ticket narratives are findings; keep the rule, drop the story.
- Every file, target, flag, script, variable and section the file names
  exists. Check mechanically:

  ```bash
  grep -oE '`[^`]+`' path/to/AGENTS.md | sort -u     # then ls / grep / make -n each
  ```

- No pointer to a log entry, and no rule justified by one; justification
  cites an ADR, the brief or a plan section.
- No content duplicated between a parent and a child `AGENTS.md`: the
  parent keeps the one-line pointer, the child keeps the rule.
- No content that belongs to a different folder's `AGENTS.md`.
- "Not yet" / "when M1 lands" items: check whether it landed. Landed ->
  rewrite as the rule that now holds. Not landed -> keep, phrased as a
  dependency ("arrives with the qdb-api-go vendoring"), not a promise.
- `CLAUDE.md` content is exactly `@AGENTS.md`.

## Pass 5 -- code comments

Comments state why, as facts. Search the in-scope code:

```bash
grep -rnE '^\s*(//|#).*\b(previously|used to|was|were|reverted|fixed in|hotfix|workaround for the|temporary|TODO|FIXME|XXX|20[0-9]{2}-[0-9]{2}-[0-9]{2})\b' \
  cmd internal scripts/cicd .buildkite/pipeline.py tests/e2e/*.sh tests/e2e/bench/*.py Makefile tests/e2e/Makefile tests/e2e/bench/Makefile
```

Findings: history instead of reason; a `TODO` whose work is tracked
nowhere else (route it to `docs/log.md` Next or Handoff, then delete the
comment); a reference to an ADR, plan section, file or flag that does
not exist; a comment restating what the code says. A comment that
explains a non-obvious constraint (the Windows PATH/cgo note in
`scripts/cicd/00.common.sh`, the `Unwrap` note in
`internal/httpapi/middleware.go`) is the intended shape: keep those.

## Pass 6 -- READMEs

`tests/e2e/README.md` and `tests/e2e/bench/README.md` hold usage only.
Findings: rationale or design text (belongs in the plan; leave a link),
targets or flags that `make -n` / the script does not accept, results or
numbers.

## Pass 7 -- one writer per fact

Pick the ten facts most likely to be duplicated (ports, dataset name and
row count, golden count, compression default, `TZ=UTC`, submodule rules,
the CI exclusion, the sentinel behaviour, the static `.a` status) and
grep each across all in-scope documents:

```bash
grep -rn --include='*.md' -E '5,?613,?032|40080|40443|40493|2836|TZ=UTC|CAPI_COMPRESSION|libqdb_api\.a' docs AGENTS.md internal tests scripts .buildkite
```

More than one document _stating_ the same fact (rather than one stating
and others linking) is a finding: the owning row in the routing table
keeps it, the others get at most a link.

Pointers into the log from anywhere else are always findings -- a log
entry is one to three lines and may be condensed at any cleanup, so
nothing may depend on its wording. This grep covers every file type,
including docstrings and YAML templates that the comment-prefixed greps
above miss:

```bash
grep -rnE 'log\.md[ ,]*20[0-9]{2}-|see the 20[0-9]{2}-[0-9]{2}-[0-9]{2} entry' . \
  --exclude-dir=vendor --exclude-dir=qdb --exclude-dir=.env --exclude-dir=.git \
  --exclude-dir=tools --exclude-dir=setup --exclude-dir=.old-master
```

Resolution: cite the document that owns the decision instead (an ADR, a
plan section, or the folder's `AGENTS.md`).

## Report format

Group by document, in the order of the passes. One line per finding:

```
docs/log.md:27      Current state   pointer into an entry            delete
docs/bench-plan.md:225  plan        temporal ("previously claimed")  rewrite as fact
docs/log.md:69-78   entry           lesson, no home                  move -> .buildkite/AGENTS.md Facts
```

Columns: location, document class, rule violated (short), action
(`delete` | `move -> <home>` | `rewrite` | `supersede` | `flag: owner`).
End with the count per action and the list of `flag: owner` items --
those need a decision and are never applied silently.

## Apply

- One document at a time; for every `delete`, the grep proving the fact
  exists at its home is run first, in the same step.
- Moves land in the receiving document before the source is cut.
- ASCII only, `--` for dashes. After every Markdown edit:
  `npx prettier --write <file>`; then `LC_ALL=C grep -n '[^ -~]' <file>`
  must print nothing (BSD grep on macOS has no `-P`).
- Re-run the greps of the pass that produced the finding; they must come
  back empty for the resolved items.
- Commits only when the owner asks: one per document, one line,
  `docs(<scope>): <subject>`, where scope is `log`, `brief`, `e2e-plan`,
  `bench-plan`, `adr`, `agents`, or the folder for `AGENTS.md` files
  (`docs(buildkite): ...`).

## When to run

At every milestone boundary (before the exit sign-off entry is written),
whenever `docs/log.md` has gained more than three entries since the last
cleanup, before a plan is deleted, and on request.
