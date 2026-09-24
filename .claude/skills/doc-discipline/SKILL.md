---
name: doc-discipline
description: Enforce this project's documentation discipline. Moves knowledge out of doc/ and AGENTS.md files into comments next to the code it describes while keeping it discoverable, and gives complex functions narrative comments (process overview at the top of the body, intent comments per step). Use when asked to apply or check documentation discipline, tidy docs, fix comment quality, or after a change that added non-trivial logic. Accepts a mode (all, placement, narrative), an optional "check" flag for report-only, and optional paths.
argument-hint: "[all|placement|narrative] [check] [paths...]"
---

# Documentation discipline

The policy is the "Documentation strategy" and "Code comments" sections of the
root `AGENTS.md`, plus the `AGENTS.md` of the documentation folder that section
names (`doc/` or `docs/`). Read them first. If anything here disagrees with
them, they win; say so in the report. This file and its two references carry
no policy of their own, so the same copy serves every repository.

## Arguments

Parse the arguments as words, in any order:

| Word        | Meaning                                                              |
| ----------- | -------------------------------------------------------------------- |
| `placement` | Only knowledge placement. Follow `placement.md`.                     |
| `narrative` | Only narrative comments in complex functions. Follow `narrative.md`. |
| `all`       | Both, placement first. This is the default when no mode is given.    |
| `check`     | Change nothing. Produce the report with proposed changes only.       |
| other words | Paths or globs that limit the scope. No paths means the whole repo.  |

Read only the reference file(s) the mode needs.

Scope excludes generated code, vendored code, lock files, and anything ignored
by git. When paths are given, `placement` still reads the `AGENTS.md` chain
from the root down to those paths, because that is where misplaced knowledge
about them lives, but it only moves knowledge that concerns the given paths.

## Hard limits

1. Comments and documentation files only. Never change what code does: no
   renames, no reordering, no refactors, no formatting sweeps. If code should
   change (a function that needs splitting, a misleading name, dead code),
   put it in the report.
2. Never invent rationale. A comment may only state what you verified from the
   code, the tests, the docs you are moving, or git history. If the "why" is
   unknown, write the "what" and list the gap under "Rationale unknown" in the
   report so a human can fill it in. Every "why" you intend to write names its
   evidence in the plan (step 3 of the procedure): a `file:line`, a test, the
   passage being moved, or a commit. The evidence stays in the plan; it does
   not go into the comment.
3. A wrong comment is worse than none. Verify every existing comment you touch
   against the code. Fix or delete stale ones; never leave one you know is
   wrong.
4. Do not add comments to code that does not need them. Coverage is not a goal.
5. ASCII only. Match the comment syntax, doc-comment convention and line width
   of the surrounding file.
6. Moving is cut-and-paste, not copy. After a move the knowledge exists once,
   at the most local place, and every other mention is a pointer or is gone.

## Procedure

1. Read the policy files. If the root `AGENTS.md` has no "Documentation
   strategy" section, stop and say so; there is no policy to enforce. State
   the resolved mode, `check` flag and scope in one line before continuing.
2. Large scope (more than about 30 source files): work one component directory
   at a time, leaf directories first, and finish each before starting the
   next. Independent components may be delegated to parallel subagents. Give
   each subagent exactly: the resolved mode, the `check` flag, its one
   directory, the Hard limits section, the reference file(s) for the mode, and
   the Report section. Tell it to do the work itself and not delegate further.
   Merge the returned reports section by section.
3. Plan. Following the reference file(s) for the mode, write the plan table(s)
   they define, one terse row per passage or function. In `check` mode, stop
   here: the plan plus the report is the result.
4. Execute the plan row by row. A row whose "why" has no evidence is executed
   as a "what"-only comment and listed under "Rationale unknown".
5. Verify. After editing Markdown, run `npx prettier --write` on the touched
   files. Do not run a code formatter. Review `git diff` hunk by hunk: every
   changed line in a source file must be a comment line; revert any hunk that
   changes a non-comment token and redo it by hand. Run the build or tests
   when that is cheap.
6. Report.

## Report

Keep it short; list, do not narrate.

- **Moved**: from -> to, one line each.
- **Narrated**: functions that received or had their narrative comments fixed.
- **Removed**: stale or duplicate comments and doc passages.
- **Discoverability**: `AGENTS.md` Map rows added or changed.
- **Rationale unknown**: places where the why could not be verified.
- **Needs a human**: code-level findings outside this skill's limits (split
  candidates, misleading names, dead or commented-out code, doc budget
  overruns that cannot be fixed by moving).

In `check` mode the same sections describe proposed changes, and the plan
table(s) precede them.
