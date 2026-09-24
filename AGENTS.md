# qdb-api-rest -- Agent Instructions

## How instructions are organized in this repository

Agent instructions live in `AGENTS.md` files, maintained **hierarchically**:

- Every piece of information goes into the `AGENTS.md` of the most
  precise folder it applies to. Information that only concerns
  `docs/` belongs in `docs/AGENTS.md`, not here.
- A higher-level `AGENTS.md` gives only a _rough_ pointer to what a
  sub-folder contains and when to open its `AGENTS.md`. It does not
  repeat or summarize the sub-folder's rules.
- When adding a folder with its own conventions, add an `AGENTS.md`
  there and a one-line pointer in the parent's `AGENTS.md`.

Before working inside a folder, read its `AGENTS.md` if one exists.

## Sub-folders

| Folder                 | Contains                                                       | Open its `AGENTS.md` when                                                 |
| ---------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `cmd/qdb_rest/`        | the binary: entry point, command line, build metadata          | touching flags, startup, shutdown, or the version block                   |
| `docs/`                | project brief, plans, ADRs, project log (current state)        | starting any session; reading or writing any planning or design text      |
| `tests/e2e/`           | e2e harness: Makefile, helpers, goldens, dataset tooling       | touching tests, goldens, the dataset, or starting a REST server for tests |
| `internal/`            | Go packages: config, observe, tlsconf, httpapi, ...            | writing or reviewing any Go code; logging and test conventions live there |
| `scripts/tests/setup/` | qdb-test-setup git submodule (starts qdbd); has no `AGENTS.md` | never edit here; update by bumping the submodule SHA                      |
| `scripts/cicd/`        | Buildkite step scripts (lint, build, unit tests)               | touching CI step scripts or the shared `00.common.sh` helpers             |
| `.buildkite/`          | pipeline generator, step templates, qdb-cicd-tools submodule   | touching the CI pipeline or platform matrix                               |

## Documentation strategy

Knowledge lives next to the code that implements it: a comment for one
line or one function, the folder's `AGENTS.md` for what spans files,
`docs/` only for what spans components. `docs/AGENTS.md` holds the
routing table that says which document owns which kind of fact, and the
rules of each document. Docs change in the same commit as the code they
describe; a change that makes an `AGENTS.md` wrong is incomplete.
History is git's job and appears in no document or comment.

## Code comments

Comment density tracks complexity. Trivial functions get none. A function
that captures business logic or is an algorithm is written so that its
comments alone tell the story:

1. The doc comment above the function is the contract for callers. No
   implementation detail.
2. The top of the body describes the process: the strategy, why it is
   shaped this way, and the steps, numbered when they form a sequence.
3. Each step in the code has a comment stating what it achieves or why,
   in one line where that fits. Never a paraphrase of the syntax.
4. Constants say where they came from, impossible branches say why they
   are impossible, hot or odd constructs say what was rejected.
5. Only state what is known. When the owner is in the session, an unknown
   reason, or a function you cannot place under the rules above, is a
   question to the owner through the question tool, asked before the
   commit that needs it. Never invent a reason and never drop one
   silently.
6. After writing a function's comments, read them back top to bottom as
   separate claims and check each against the code below it. Fix or
   delete any claim the code does not bear out.

`ingestCSV` in `internal/qdb/ingest.go` is the shape. This is the
primary path: new code is written this way. `/doc-discipline
[all|placement|narrative] [check] [paths...]` is the repair path; it
applies or checks all of the above after the fact, and its worked
example is in `.claude/skills/doc-discipline/narrative.md`.
