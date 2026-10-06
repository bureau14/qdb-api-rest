# The prose audit of the files outside encoding -- Plan

Status: draft. Scaffolding for one unit on `sc-19567/rr-prose-audit-touched`.
Deleted when the unit lands (`docs/AGENTS.md`, Plans).

## 1. Outcome

When the unit lands:

- Every comment in `internal/model/model.go`, `internal/httpapi/query.go`
  and `tests/e2e/common.sh`, and every paragraph, bullet and table cell
  in `internal/AGENTS.md`, `docs/brief.md`, `docs/e2e.md`,
  `docs/e2e-v2-flow-plan.md`, ADRs 0010, 0013 and 0014 and `docs/log.md`,
  reads as a plain sentence under the root `AGENTS.md`, Prose.
- Every one of those files carries the facts it carries today. No fact
  is added and none is removed. No code token changes.
- `docs/log.md`, Current state, Next, no longer lists this audit. Its
  first item is the project-wide audit, one package or document per
  unit.
- This plan is deleted.

Left for later units:

1. The prose audit of every other comment and document in the project
   (`docs/log.md`, Next, item 1 after this unit). `tests/e2e/bench/bench.py`
   is not audited by this unit and is not queued either (Rationale, row 5).
2. The decoders, the e2e tool and the flow, as the log lists them.

## 2. Verified facts

- The prose rule is `AGENTS.md:72-98` (root), five numbered rules and the
  read-aloud test. `docs/AGENTS.md:191` points at it (2026-10-06, this
  session).
- The eleven files of the log item were touched by the encode-test unit
  only at the lines that said "render": commits 9b07d16, d1fe40f and
  680fd76 change 4, 16 and 23 lines. No commit since 5969b62 touches
  them, so the audit has not landed (2026-10-06, `git log`).
- The owner answered the seed report's questions on 2026-10-06: the
  working tree is committed; the brief stays one commit; rewording
  accepted ADRs is acceptable; `bench.py` is skipped.
- The owner's earlier decisions this audit honors: "encode", never
  "render", for the encoding concept, with `retryAfter`
  (`internal/httpapi/query.go:64`) and the dashboard sentences
  (`docs/brief.md:35`, `docs/brief.md:684`) keeping "render" because
  they format a value for a human (`docs/log.md:81-85`, 2026-10-06).
- The density of what the rule targets, counted with `grep -c` on
  2026-10-06: semicolons 37 in `internal/AGENTS.md`, 7 in `query.go`, 114
  in the brief, 74 in `e2e.md`, 42 in the flow plan, 19, 20 and 13 in the
  three ADRs, 94 in the log, 34 in `common.sh`; " -- " 46 in the brief
  and 72 in the log; `never|nothing else|exactly|the one` 24 in
  `internal/AGENTS.md` and 27 in the brief. `model.go` has none of any.
  These are candidates to read, not a count of edits: a semicolon in a
  code span, a shell statement or a Markdown table is not prose.
- The precedent for the shape of an audit commit is d3557ba (the
  encoding package) and b6a5ee9 (`internal/encoding/AGENTS.md`): comment
  and Markdown lines only, facts kept, each sentence given a subject and
  a verb.
- `docs/AGENTS.md:195-212` lists four greps that must stay clean after a
  document edit: history words in the brief and the plans (candidates),
  pointers into log entries, measured numbers in the log and the plans,
  non-ASCII.
- `.buildkite/AGENTS.md:46-53`: a feature-branch build is created through
  the API with `ignore_pipeline_branch_filters: true` and the full
  40-character SHA.
- `make lint` and `go test` need `source .envrc`; the `httpapi` tests dial
  the services of `scripts/tests/setup/start-services.sh`
  (`internal/AGENTS.md:107-111`, `internal/AGENTS.md:185-190`).

## 3. Design

The unit changes no signature, no type and no variable. It changes
comment lines and Markdown lines. This section says, per file, what the
executor reads and how each sentence is judged and rewritten. The
executor composes the rewritten sentences from the rule and the file;
the examples below fix the shape and are typed in as written.

### The procedure, applied to every file

For each comment, paragraph, bullet and table cell, in file order:

1. Read it aloud as a colleague explaining the code. A sentence no one
   would say is rewritten. A sentence anyone would say is kept as it is.
2. Apply the five rules of `AGENTS.md:72-98` in order. Give every
   sentence a subject and a verb. Split a sentence that carries two
   ideas. Replace a semicolon that joins clauses with a full stop.
   Replace a colon that introduces a chain of clauses with a full stop,
   and keep a colon that introduces one example or one list of nouns.
   Restore a dropped verb. Replace a possessive that stands in for a
   clause with the clause. Remove "X, not Y" unless it records a
   rejected alternative. Remove "never", "exactly", "nothing else" and
   "the one" where they add emphasis and keep them where they carry
   meaning (a rule, an invariant, a count).
3. Keep every fact. Add none. Keep every path, identifier, number,
   date and cross-reference as it is. Keep the author's names for
   things.
4. A claim the executor cannot verify against the tree is kept verbatim
   and listed in the build-stage message under "Rationale unknown".
   The rule is the skill's hard limit 2 and 3: a wrong comment is worse
   than none, and no rationale is invented.
5. After the file, read it back top to bottom as a list of claims and
   check each against the code or the other documents it names.

A sentence inside a fenced code block, a shell statement, a Go
statement, a Markdown table header, a URL or a path is not prose and is
not touched. A table cell is prose and is audited, with the cell kept
to one line where the table has one-line cells.

### `internal/model/model.go` and `internal/httpapi/query.go` (commit 2)

Both files keep every function bare or narrated as it is today. No
function changes its qualification: `handleQuery` stays narrated (rule
3 of `narrative.md`, a handler with a sequence of steps), every other
function in `query.go` stays bare (rule 1 or 2), and `model.go` has no
function. The audit rewrites the doc comments and the step comments in
place.

`model.go` is audited and expected to need no change: its three
comments already read as sentences. If so, the commit touches `query.go`
alone and its subject names `query.go` alone.

The shape, with the sentences that change:

```go
// retryAfter renders d as Retry-After wants it: whole seconds, rounded
// up so a client never comes back early.
```

becomes

```go
// retryAfter renders d as Retry-After wants it: whole seconds, rounded
// up, so a client does not come back early.
```

("never" was emphasis; "renders" stays, Rationale row 4.)

```go
// writeClusterError maps a failed cluster call onto the wire by who
// failed: an open breaker or an unreachable cluster is 503; anything
// the cluster itself answered is the caller's fault and gets the status
// answered (400 for a query, 401 for a login); a caller whose context
// has ended gets nothing at all.
```

becomes

```go
// writeClusterError maps a failed cluster call onto the wire by who
// failed. An open breaker or an unreachable cluster is 503. Anything
// the cluster itself answered is the caller's fault and gets the status
// in answered (400 for a query, 401 for a login). A caller whose
// context has ended gets no response at all.
```

The step comments of `handleQuery` are read the same way. The comment
"Before the first byte the failure is this process's, a column the
encoder cannot encode: 500. After it, or once the caller has left, the
stream is cut and only the log hears of it." becomes "Before the first
byte the failure is this process's own, such as a column the encoder
cannot encode, and the answer is 500. After the first byte, or once the
caller has left, the stream is cut and only the log hears of it."

### `internal/AGENTS.md` (commit 3)

Every bullet of Code, Building, Logging and Tests, in order. The file
keeps its sections, its bullets and its order. The Code bullets on the
result, the table read and the ingest are the long ones and each becomes
a sequence of short sentences, the way `internal/encoding/AGENTS.md`
reads after b6a5ee9. The example, for the first sentence of the ingest
bullet:

```
- An ingest is one push per request through one held session:
  `Cluster.Ingest` leases one session of the caller's pool for the whole
  body, lends the decoder a schema lookup bound to it (`Session.schemaOf`,
  the reader's whole-table schema) and pushes the decoded batches through
  it in one `ArrowWriter` call; it is never retried.
```

becomes

```
- An ingest is one push per request through one held session.
  `Cluster.Ingest` leases one session of the caller's pool for the whole
  body. It lends the decoder a schema lookup bound to that session
  (`Session.schemaOf`, which answers the reader's whole-table schema),
  and it pushes the decoded batches through the session in one
  `ArrowWriter` call. The push is not retried.
```

"Never" stays where it states a rule the code keeps ("`qdb-api-go` is
never patched in `vendor/`", "never `context.Background()`") and leaves
where it is emphasis.

### `docs/brief.md` (commit 4)

The whole brief, section by section, one commit (owner, 2026-10-06).
The Vision, Strategic context and Why sections are the brief's own
voice and are the richest in " -- " asides and "X, not Y" contrasts.
An aside becomes its own sentence. A contrast that records a rejected
alternative stays (the Flight SQL section, the Cluster binding section);
a contrast for emphasis goes. The tables (Ecosystem, Deviations, the
endpoint sketch block) keep their cells as they are unless a cell is a
sentence that no one would say. The two dashboard sentences keep
"render" (`docs/brief.md:35`, `docs/brief.md:684`). The `Status:` line
stays. The example:

```
Performance is the headline requirement: the server streams everything,
so response memory is bounded regardless of result size and
time-to-first-byte is independent of result size wherever the underlying
client API allows it.
```

becomes

```
Performance is the headline requirement. The server streams everything,
so response memory is bounded regardless of result size, and the time
to first byte is independent of result size wherever the underlying
client API allows it.
```

### `docs/e2e.md` (commit 5) and `docs/e2e-v2-flow-plan.md` (commit 6)

The whole of each, decision-log tables included. A table cell stays one
line. A dated verification ("verified 2026-08-19") stays with its date.
The example, from the flow plan's comparator:

```
The CSV path is thus proven against a source the encoders never touched,
and the other three are proven equal to it.
```

becomes

```
The CSV path is thus proven against a source the encoders did not
touch, and the other three formats are proven equal to it.
```

### ADR-0010, ADR-0013, ADR-0014 (commits 7, 8, 9)

The whole of each. The `Status:` and `Date:` lines, the numbering of the
decisions and the rows of the alternatives tables stay, so that every
cross-reference of the form "ADR-0013 5" still lands. A decision's
wording changes; the decision does not (owner, 2026-10-06, extending the
word-swap decision of `docs/log.md:81-85`). The example, ADR-0010,
decision 5:

```
The status is always decided before the first byte: the batch is
materialized before anything is written, so every cluster error is
known up front. A mid-stream failure is a cut stream, logged, never a
status.
```

becomes

```
The status is decided before the first byte. The batch is materialized
before anything is written, so every cluster error is known up front.
A failure in the middle of the stream cuts the stream and is logged.
It is not a status.
```

### `docs/log.md` (commits 10 and 12)

Commit 10 audits the Current state block and every dated entry. An
entry keeps its date, its heading and its links. Its sentences are
reworded only. The example, the 2026-09-02 entry on the sentinels:

```
- Owner decision: the session budget is the whole overload mechanism;
  no admission layer, no 429.
```

becomes

```
- Owner decision: the session budget is the whole overload mechanism.
  There is no admission layer and no 429.
```

Commit 12 rewrites Current state, Next: item 1 leaves, the items
renumber, and the new item 1 reads:

```
1. The prose audit (root `AGENTS.md`, Prose) of every other comment and
   document in the project, one package or document per unit.
   `tests/e2e/bench/bench.py` is left out: the bench retires once the
   rewrite beats the old server (`docs/brief.md`, Testing doctrine).
```

Commit 12 adds one entry, newest first:

```
## 2026-10-06 -- the files the encode-test unit touched read as plain sentences; prose-audit-touched-plan.md deleted

- Owner decisions: accepted ADRs and dated entries are reworded with
  their decisions unchanged; the bench is outside the audit. No fact
  moved; the plan carried none.
```

### `tests/e2e/common.sh` (commit 11)

The file header and every function comment. The comment syntax, the
line width and the `# ---- section` banners stay. A `Usage:` line
stays. The example:

```
# qdbsh writes qdbsh.log* into its log directory (default: cwd). Keep that
# noise out of the tree; every qdbsh call goes through this wrapper.
```

becomes

```
# qdbsh writes qdbsh.log* into its log directory, which defaults to the
# working directory. This wrapper keeps that noise out of the tree, so
# every qdbsh call goes through it.
```

`bash -n tests/e2e/common.sh` passes after the commit.

### Tests

The unit adds no test. The Go commits build and lint (`make lint`,
`go build ./...`), and `go test -p 1 ./internal/httpapi/... ./internal/model/...`
runs with the services up, as the proof that no token changed.

## 4. Rationale

| decision                                                                                                              | why                                                                                                                                                          | rejected, and why                                                                                 | gained                                                                   | given up                                                                                                                   | settled by                            |
| --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| 1. One commit per file, the two small Go files together, in the order Go, `internal/AGENTS.md`, documents, shell, log | the owner reviews one document's prose at a time; the encode-test unit landed its sweeps the same way (9b07d16, d1fe40f, 680fd76)                            | one sweep commit: a 2,000-line prose diff with no seam to review at                               | a reviewable diff per document                                           | eleven commits for one subject                                                                                             | proposal                              |
| 2. Comment and Markdown lines only; no code token changes                                                             | the unit is an audit of prose; a code change in the same diff hides among reworded lines; the skill's hard limit 1                                           | fixing code smells met on the way                                                                 | a diff that is prose by construction, checked by `git diff` hunk by hunk | a code finding waits for its own unit, reported in the build-stage message                                                 | proposal; precedent d3557ba           |
| 3. A claim that cannot be verified is kept verbatim and reported                                                      | the rule keeps every fact and adds none (`AGENTS.md:96-98`); a reworded claim that was wrong becomes a confident wrong claim                                 | rewording it anyway, trusting the author                                                          | no fact is altered by the audit                                          | some sentences stay unnatural until the owner answers                                                                      | proposal; skill hard limits 2 and 3   |
| 4. `retryAfter` and the dashboard sentences keep "render"                                                             | the owner: the decision is about the concept of encoding; the survivors format a value for a human                                                           | a blanket sweep                                                                                   | the owner's decision stands                                              | none                                                                                                                       | `docs/log.md:81-85`, owner 2026-10-06 |
| 5. `tests/e2e/bench/bench.py` is skipped and not queued                                                               | the owner: "skip" (2026-10-06); the bench is temporary and retires once the rewrite beats the old server (`docs/brief.md:790-793`)                           | auditing it, as the log item listed                                                               | one commit less; no prose work on a tool that retires                    | its docstrings stay as they are                                                                                            | owner, 2026-10-06                     |
| 6. The brief is one commit                                                                                            | the owner: "that's acceptable" (2026-10-06)                                                                                                                  | one commit per top-level section of the brief                                                     | one document, one diff                                                   | a large diff to review                                                                                                     | owner, 2026-10-06                     |
| 7. Accepted ADRs and dated log entries are reworded, decisions and dates unchanged                                    | the owner: "rewording accepted ADRs is ok" (2026-10-06); the precedent is the word swap of 680fd76; the wording of a decision changes, the decision does not | stopping the audit at the ADRs' and entries' boundaries, per `docs/AGENTS.md:27-28` read strictly | the whole set of files reads the same way                                | the append-only rule now reads "facts and decisions append-only, wording editable", recorded in the log entry of commit 12 | owner, 2026-10-06                     |
| 8. Emphasis words leave; rule words stay                                                                              | rule 4 of the prose section: "never", "exactly", "nothing else" are removed where added for emphasis rather than meaning                                     | removing every occurrence                                                                         | each remaining "never" states a rule the code keeps                      | the executor judges each occurrence                                                                                        | `AGENTS.md:88-91`                     |

## 5. Knowledge

| commit  | lands                                                                                                                 |
| ------- | --------------------------------------------------------------------------------------------------------------------- |
| 2 to 11 | the rule applied; no new why. Row 4 is visible in the two kept "render" lines. Row 8 is visible in each kept "never". |
| 12      | rows 5 and 7 in the new log entry and in the new Next item 1; the deletion of this plan                               |

Rows 1, 2, 3 and 6 are about how this unit is built and land in the
commit list and the build-stage message, not in a document. Row 7
changes how `docs/AGENTS.md:27-28` and `docs/AGENTS.md:138` are read;
the log entry of commit 12 records the owner decision, and
`docs/AGENTS.md` is not edited by this unit because its rule ("the
decision is appended, never rewritten") was and stays about decisions,
not about their wording.

## 6. How the knowledge lands

1. `/doc-discipline read` has run before this plan was written and runs
   again before commit 2 if the build stage is another session.
2. Each commit edits one file (commit 2: two files) by hand, sentence by
   sentence, under the procedure of section 3. `npx prettier --write`
   runs on every Markdown file a commit touches. The four greps of
   `docs/AGENTS.md:195-212` run after every document commit and print
   nothing new.
3. `git diff --cached` is reviewed hunk by hunk before each commit: a
   hunk that changes a non-comment token in a `.go` or `.sh` file is
   reverted and redone.
4. A why that arises while building is asked of the owner through the
   question tool before the commit that needs it. The audit expects
   none: it adds no reason.
5. Every sentence kept verbatim under section 3, step 4, goes on the
   "Rationale unknown" list of the build-stage message with its
   `file:line`.
6. After commit 12: `/doc-discipline check internal/model internal/httpapi/query.go internal/AGENTS.md docs tests/e2e/common.sh docs/prose-audit-touched-plan.md`.
   The report is quoted in the build-stage message. A finding is fixed
   by a further small commit on the branch.
7. Local verification: `source .envrc`, services up, `make lint`,
   `go test -p 1 ./internal/httpapi/... ./internal/model/...`,
   `bash -n tests/e2e/common.sh`.

## 7. Commits

1. `docs(plan): prose-audit-touched-plan.md, the files outside encoding read as plain sentences` (this document)
2. `docs(internal): model.go and query.go read as plain sentences`
3. `docs(agents): internal/AGENTS.md reads as plain sentences`
4. `docs(brief): the brief reads as plain sentences`
5. `docs(e2e): e2e.md reads as plain sentences`
6. `docs(plan): e2e-v2-flow-plan.md reads as plain sentences`
7. `docs(adr): ADR-0010 reads as plain sentences`
8. `docs(adr): ADR-0013 reads as plain sentences`
9. `docs(adr): ADR-0014 reads as plain sentences`
10. `docs(log): the log reads as plain sentences`
11. `docs(tests): common.sh comments read as plain sentences`
12. `docs(log): the audit of the encode-test files is done, bench.py excepted; the project-wide audit is next; prose-audit-touched-plan.md deleted`
13. `/doc-discipline check` over the paths of step 6 above; one small commit per finding.
14. Verify: push `sc-19567/rr-prose-audit-touched`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

none
