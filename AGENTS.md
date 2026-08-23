# qdb-api-rest -- Agent Instructions

## How instructions are organized in this repository

Agent instructions live in `AGENTS.md` files, maintained **hierarchically**:

- Every piece of information goes into the `AGENTS.md` of the most
  precise folder it applies to. Information that only concerns
  `docs/` belongs in `docs/AGENTS.md`, not here.
- A higher-level `AGENTS.md` gives only a _rough_ pointer to what a
  sub-folder contains and when to open its `AGENTS.md`. It does not
  repeat or summarize the sub-folder's rules.
- `CLAUDE.md` files contain exactly `@AGENTS.md` and nothing else; they
  exist only so the tooling includes `AGENTS.md`. Never put content in
  a `CLAUDE.md`.
- When adding a folder with its own conventions, add an `AGENTS.md`
  there and a one-line pointer in the parent's `AGENTS.md`.

Before working inside a folder, read its `AGENTS.md` if one exists.

## Sub-folders

| Folder                 | Contains                                                        | Open its `AGENTS.md` when                                                 |
| ---------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `docs/`                | project brief, plans, ADRs, project log (current state)         | starting any session; reading or writing any planning or design text      |
| `tests/e2e/`           | e2e harness: Makefile, helpers, legacy goldens, dataset tooling | touching tests, goldens, the dataset, or starting a REST server for tests |
| `internal/`            | Go packages: config, observe, tlsconf, httpapi, ...             | writing or reviewing any Go code; logging and test conventions live there |
| `scripts/tests/setup/` | qdb-test-setup git submodule (starts qdbd); has no `AGENTS.md`  | never edit here; update by bumping the submodule SHA                      |

Folders introduced by later milestones (`cmd/`, `scripts/cicd/`,
`.buildkite/`) get their own row here when they gain an `AGENTS.md`.
