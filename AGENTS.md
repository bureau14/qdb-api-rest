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

| Folder                 | Contains                                                             | Open its `AGENTS.md` when                                                     |
| ---------------------- | -------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `cmd/qdb_rest/`        | the binary: entry point, command line, build metadata                | touching flags, startup, shutdown, or the version block                       |
| `docs/`                | project brief, plans, ADRs, project log (current state)              | starting any session, or reading or writing any planning or design text       |
| `tests/e2e/`           | e2e harness: Makefile, helpers, goldens, dataset tooling             | touching tests, goldens, the dataset, or starting a REST server for tests     |
| `internal/`            | Go packages: config, observe, tlsconf, httpapi, ...                  | writing or reviewing any Go code, and logging and test conventions live there |
| `scripts/tests/setup/` | qdb-test-setup git submodule (starts qdbd), which has no `AGENTS.md` | never edit here, and update by bumping the submodule SHA                      |
| `third_party/`         | stub modules that `go.mod` puts in place of a dependency's modules   | touching `go.mod` replace lines, or bumping duckdb-go                         |
| `scripts/cicd/`        | Buildkite step scripts (lint, build, unit tests)                     | touching CI step scripts or the shared `00.common.sh` helpers                 |
| `.buildkite/`          | pipeline generator, step templates, qdb-cicd-tools submodule         | touching the CI pipeline or platform matrix                                   |

## Documentation strategy

Knowledge lives next to the code that implements it: a comment for one
line or one function, the folder's `AGENTS.md` for what spans files,
`docs/` only for what spans components. `docs/AGENTS.md` holds the
routing table that says which document owns which kind of fact, and the
rules of each document. Docs change in the same commit as the code they
describe, and a change that makes an `AGENTS.md` wrong is incomplete.
History is git's job and appears in no document or comment.

## Code comments

A comment says what the code cannot say for itself. Most functions need a
short doc comment and a few inline comments, and many need none at all.

1. The doc comment above a function is the contract for callers, in one to
   three sentences: what it does, what it returns and how it fails. It holds
   no implementation detail.
2. A reason sits inline at the line it explains, and it appears once. An
   inline comment is one line, two at most. Every line that would make a
   colleague stop and ask "why?" gets one, in any function, tests included.
3. A function whose steps must run in an order the code does not make
   obvious starts its body with an overview that lists the steps, one
   numbered line each. The overview carries no reasons. The reasons sit at
   the steps, and each step comment starts with the step's number.
4. Constants say where they came from, and impossible branches say why they
   are impossible. A construct that looks wrong says which alternative was
   rejected, but only where a reader of that line would propose the
   alternative. Other rejected alternatives belong in `docs/adr/` or nowhere.
5. A comment describes the code as it is. When code changes, rewrite every
   comment block the change touches from the new code, as if the code had
   always worked this way. Write the new block before rereading the old one,
   then carry over only the facts the code cannot show. A comment never
   mentions an earlier version, a plan, a review, a unit of work or the
   conversation that shaped it.
6. Delete a comment that an experienced developer in the language would not
   miss. Comments stay well under half of a function body's lines.
7. Only state what is known. When the owner is in the session, an unknown
   reason, or a function you cannot place under the rules above, is a
   question to the owner through the question tool, asked before the commit
   that needs it. Never invent a reason.
8. After writing a function's comments, read them back top to bottom as
   separate claims and check each against the code below it. Fix or delete
   any claim the code does not bear out.

`Cluster.Call` in `internal/qdb/cluster.go` is the shape. This is the
primary path: new code is written this way. `/doc-discipline
[all|placement|narrative|read] [check] [paths...] [plan.md]` is the repair
path. It applies or checks all of the above after the fact, `read` loads the
shape before code or a plan is written, and a plan path reconciles the code
against the reasons the plan promised. Its worked examples are in
`.claude/skills/doc-discipline/narrative.md`.

## Prose

Comments and documents read like a colleague explaining the code, in
plain sentences. The rules:

1. Every sentence has a subject and a verb and says who does what.
   Write "the fixture compares the batch with the table it wrote", not
   "the comparison is the fixture's".
2. One idea per sentence. A colon introduces one example or one list
   of nouns, never a chain of clauses, and a semicolon joins nothing.
3. No ellipsis: a sentence does not drop its verb, and a possessive
   does not stand in for a clause ("the decoders' decode_test.go").
4. No rhetoric: no "X, not Y" contrasts except to record a rejected
   alternative, no aphorisms, no mirrored clauses, no "the one", "and
   nothing else", "exactly" or "never" added for emphasis rather than
   meaning.
5. Name the thing: "the standard library parses the body", not "read
   back with the standard library", and "`Check`", not "the fixture's
   proof".

Read every comment or paragraph back as a colleague would say it
aloud. A sentence that no one would say is rewritten. The rewrite keeps the
facts a reader needs and drops the facts the code already shows. These
sentences came out of review, each next to what a colleague would write:

| Written                                                    | A colleague writes                                                                 |
| ---------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| The dial is the credential check.                          | Opening a session with the user's credentials is how we check them.                |
| A create is never retried, because it is not a read.       | CreateTable does not retry, because only reads are safe to run twice.              |
| DuckDB runs q as u on u's own DuckDB instance.             | Runs the query on the user's own DuckDB instance.                                  |
| A nil batch is read as no table, not dereferenced.         | A statement without a result set returns a nil batch, which means no tables exist. |
| The trace layer wraps every route; the order is the point. | Add the trace layer last, so it also wraps `/openapi.json`.                        |
