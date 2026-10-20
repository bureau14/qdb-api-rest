# Report, then stop

Reply with exactly these sections, in this order. Every candidate,
every constraint and every code claim carries a path, for example
`path/to/document.md` or `path/to/file.go:42-46`; a claim without a
path is not made. Do not summarize the source-of-truth document; report
only what the unit must honor.

Each section is a markdown heading, `# Name`, on a line of its own
with a blank line before and after it. Sections carry no numbers. A
numbered list appears only where a section asks for one, and it starts
at 1. Two sections belong to steps only `/rr-start-next` runs; a
section whose step did not run in this seed is omitted, not written as
"none".

- `# Workflow` -- one line acknowledging the workflow and the feature
  branch name you will use (`sc-19567/rr-<slug>`), not yet created.
- `# Position` -- at most five lines: milestone, what is in flight,
  what the log says comes next, how the unit relates to that.
- `# Candidates` (Choose the unit ran) -- a table, one row per
  candidate, in the log's order:
  `| candidate | source (path) | tree check (landed / blocked / ready) | verdict |`
- `# Brief` -- prose for the owner to read, at most 200 words, no
  tables, no paths: the one section whose claims the sections below
  evidence instead of carrying paths themselves. Three paragraphs:
  what the unit is and what is true when it is done; how it will be
  done, the approach and the shape of the change in plain words; how
  it fits what is in flight, why it comes now, and what it leaves for
  later. Written for a reader who has not opened the sources: this is
  the explanation, the rest of the report is its evidence.
- `# Decisions` -- a numbered list, one entry per design choice from
  Shape the unit, one to three lines each: the choice, the
  alternative it beats, the reason, and `settled by <path>` (an
  `owner` source), `settled by owner, <date>` (the challenge round) or
  `proposal`. Write "none" if the unit leaves no choice to make.
- `# Size and slices` (Cut three scopes ran) -- at most three lines:
  the scope the owner chose, its commit count and whether it is inside
  the band, where the cut is and why there; then one line per remaining
  slice with its size, in the order they would follow, or "none" if
  the unit was not cut.
- `# Constraints` -- a table, one row per decision, constraint or
  gotcha that applies:
  `| constraint | source (path or path:line) | provenance (owner / tree / agent) | effect on the unit |`
- `# Code involved` -- one line per file or package in the scoped set:
  path, then what it does and how the unit touches it.
- `# Open questions` -- a numbered list of everything the unit's
  description or source contradicts or leaves open, every stale entry
  or gap the caller's survey found, every choice Shape the unit found
  was not yours to make, and every challenge the round did not reach
  or the owner deferred. Ask; do not resolve by assumption. Write
  "none" if there are none.
- `# Proposed commits` -- numbered one-line commit subjects in the
  order you would land them. Commit 1 is always the plan:
  `docs(plan): <slug>-plan.md, <subject>`; it is the only commit
  `/rr-plan` authorizes, and it is outside the size band. Then at most
  ten build commits, or the build commits of the chosen scope: the
  outline the plan document will refine, so the owner can redirect
  before the plan is written. The list closes with the Buildkite
  verification step; it is not counted as a commit.

Correct: the brief says the decoder reads the whole body into one
record batch per table before anything is pushed, so a malformed row
anywhere fails the request before the server sees a row; it names
streaming as the alternative and says why it lost; the decision entry
repeats the choice with what settles it.

Incorrect: the brief says "add the CSV decoder, 7 commits", and the
owner learns what the decoder does from the commit subjects.

Then wait. The owner opens the plan stage with `/rr-plan`; nothing
else does.
