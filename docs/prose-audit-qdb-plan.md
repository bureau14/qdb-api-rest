# The prose audit of internal/qdb -- Plan

Status: draft. Scaffolding for one unit on `sc-19567/rr-prose-audit-qdb`.
Deleted when the unit lands (`docs/AGENTS.md`, Plans).

## 1. Outcome

When the unit lands:

- Every comment in `internal/qdb` (the ten Go files and the two test
  files) reads as a plain sentence under the root `AGENTS.md`, Prose.
- Every file carries the facts it carries today. No fact is added and
  none is removed. No code token changes. No function changes its
  qualification under `narrative.md`: the narrated ones keep their
  overviews and step comments, the bare ones stay bare.
- `docs/log.md` has one new entry naming this plan's deletion.
  Current state, Next, item 1 stays as written, because it already
  names the remaining audit units ("every other comment and document in
  the project, one package or document per unit").
- This plan is deleted.

Left for later units:

1. The audit of every other package, document and shell file, one per
   unit, in the order section 8 recommends.
2. The decoders, the e2e tool, the flow, the bench unit and the
   upstream filing, as `docs/log.md`, Next, lists them.

## 2. Verified facts

- The prose rule is `AGENTS.md:72-94` (root), five numbered rules and
  the read-aloud test. The comment shape is `AGENTS.md:40-70`, and
  `AGENTS.md:63` names `Session.ingest` in `internal/qdb/ingest.go` as
  the shape (2026-10-08, this session).
- Which functions qualify for a narrative is
  `.claude/skills/doc-discipline/narrative.md:25-32`, four ordered
  rules (2026-10-08).
- The package was swept once, lightly, by 65b94fa (2026-10-06, 234
  lines across 36 files): `breaker.go`, `cluster.go`, `cluster_test.go`,
  `ingest.go`, `read.go`, `read_test.go` and `table.go` each lost a
  few sentences without a verb. No per-file audit of the package has
  landed: `git log --grep 'plain sentences' -- internal/qdb` is empty
  (2026-10-08).
- The density of what the rule targets, from `grep` on 2026-10-08
  over comment lines (`;`, `--`, `never|exactly|nothing else|the
one`, `, not `): `cluster.go` 21 lines, `read.go` 13, `ingest.go` 10,
  `breaker.go` 6, `read_test.go` 5, `cluster_test.go` 4, `table.go` 2,
  `user.go` 1, `logger.go` 1, `qdb.go` 2, `budget.go` 0, `context.go` 0. These are candidates to read, not a count of edits.
- The precedent for an audit commit's shape is 9f39b4a (`model.go` and
  `query.go`), d3557ba (the encoding package) and fa1a53a
  (`common.sh`): comment lines only, facts kept, one commit per file,
  the small files together (2026-10-08, `git show`).
- The precedent for a numbered overview after the audit is
  `internal/encoding/csv.go:233-239` (d3557ba): the items of the list
  end in semicolons and the last in a full stop, and a semicolon inside
  an item that joined two clauses became a full stop (2026-10-08).
- The narrated functions of the package, by `narrative.md` rule 3,
  are `Session.ingest` (`internal/qdb/ingest.go:101-153`, numbered
  overview and five steps), `Session.Read` (`internal/qdb/read.go:147-177`,
  numbered overview and four steps), `lent`
  (`internal/qdb/read.go:115-141`, two step comments), `schemaOf`
  (`internal/qdb/read.go:71-90`, one overview comment),
  `breaker.allow` and `breaker.recordFailure`
  (`internal/qdb/breaker.go:42-62`, `:75-92`, step comments per
  branch), `feedBreaker` (`internal/qdb/cluster.go:277-287`), and
  `takeIdle` (`internal/qdb/cluster.go:404-425`, the overview in its
  doc comment). Every other function is bare under rule 1 or 2
  (2026-10-08, read whole).
- The two C API defects the comments cite are sc-19829 (the trailing
  NUL, `internal/qdb/read_test.go:146-149`) and sc-19830 (two
  symbol-bearing tables in one reader, `internal/AGENTS.md`, Code).
  Their sentences carry the ticket numbers and keep them.
- `docs/AGENTS.md:193-212` lists four greps that must stay clean after
  a document edit. `.buildkite/AGENTS.md:40-48`: a feature-branch build
  is created through the API with `ignore_pipeline_branch_filters:
true` and the full 40-character SHA.
- `make lint` and `go test` need `source .envrc`; the `qdb` tests dial
  the pair of `scripts/tests/setup/start-services.sh` on 2836 and 2838
  and run serially (`internal/AGENTS.md`, Building and Tests). The
  linters are `.golangci.yml`: the standard set, `forbidigo` and
  `sloglint`, with `gofumpt` and `goimports`. None judges comment
  punctuation, so a comment edit cannot fail lint.
- The owner answered the seed report with "ok" on 2026-10-08 and left
  its three open questions unanswered. Section 8 carries them with the
  recommendations the plan's approval settles.

## 3. Design

The unit changes no signature, no type and no variable. It changes
comment lines. This section says, per commit, what the executor reads
and how each sentence is judged and rewritten. The executor composes
the rewritten sentences from the rule and the file. The examples below
fix the shape and are typed in as written.

### The procedure, applied to every file

For each doc comment, overview, step comment and trailing comment, in
file order:

1. Read it aloud as a colleague explaining the code. A sentence no one
   would say is rewritten. A sentence anyone would say is kept as it is.
2. Apply the five rules of `AGENTS.md:77-94` in order. Give every
   sentence a subject and a verb. Split a sentence that carries two
   ideas. Replace a semicolon that joins clauses with a full stop.
   Replace a colon that introduces a chain of clauses with a full stop,
   and keep a colon that introduces one example or one list of nouns.
   Turn an aside set off by `--` into its own sentence. Restore a
   dropped verb. Replace a possessive that stands in for a clause with
   the clause. Remove "X, not Y" unless it records a rejected
   alternative. Remove "never", "exactly", "nothing else" and "the one"
   where they add emphasis and keep them where they carry meaning (a
   rule, an invariant, a count).
3. In a numbered overview, a semicolon that ends an item stays, as list
   punctuation, and the last item ends in a full stop
   (`internal/encoding/csv.go:233-239`). A semicolon inside an item
   that joins two clauses becomes a full stop, and the item continues
   on the same indentation.
4. Keep every fact. Add none. Keep every path, identifier, ticket
   number, date and cross-reference as it is. Keep the author's names
   for things.
5. A claim the executor cannot verify against the tree, the vendored
   binding or the tests is kept verbatim and listed in the build-stage
   message under "Rationale unknown" with its `file:line` (the skill's
   hard limits 2 and 3).
6. After the file, read it back top to bottom as a list of claims and
   check each against the code below it.

A Go statement, a string literal, a `//nolint` directive, a URL or a
path is not prose and is not touched. A trailing comment on a struct
field is prose and is audited.

Every function keeps its qualification. A narrated function's doc
comment stays the contract, its overview stays the process, and its
step comments keep their numbers and headings. A bare function stays
bare.

### `cluster.go` (commit 2)

The doc comments of `Cluster`, `New`, `ownCredentials`,
`compressionOf`, `connect`, `closeBudgeted`, `newUserPool`, `poolFor`,
`Session`, `closeAsync`, `fetch`, `WithReadRetry`, `callerLeft`,
`feedBreaker` and its two step comments, `Call`, `Query`,
`Authenticate`, `Probe`, `evict`, `takeIdle`, `evictOnce`, `Reap`,
`Close`, and the field comments of `Cluster`. The examples:

```go
// breaker is the per-cluster circuit breaker Call gates on; dials and
// calls that find the cluster unavailable feed it, the readiness probe
// never does.
```

becomes

```go
// breaker is the per-cluster circuit breaker that Call gates on. Dials
// and calls that find the cluster unavailable feed it. The readiness
// probe does not feed it.
```

("never" was emphasis.)

```go
// poolFor finds or creates the user's pool; it never replaces one that
// exists, so in-flight requests are never raced.
```

becomes

```go
// poolFor finds or creates the user's pool. It does not replace a pool
// that exists, so an in-flight request keeps the pool it leased from.
```

The doc comment of `Call` (`internal/qdb/cluster.go:289-297`) is the
longest rewrite. Its claims, in order, each as its own sentence: the
breaker gates the call; the user's pool leases a session and dials one
on demand; the pool runs f and decides the session's fate from f's
error through the binding's `IsBadSession` and `Lease.Done`; this
layer decides two things per cluster, which are the breaker, fed only
by the errors that are evidence about the cluster
(`IsClusterUnavailable`), and the opt-in retry of an idempotent read
after a retryable failure; a retry after a bad session runs on a fresh
session because the pool discarded the old one; a retry after any
other retryable failure may run on the same session. The step comment
of `feedBreaker` at `internal/qdb/cluster.go:283-285` loses its dashed
aside:

```go
// nil or an answer -- a rejected request, a refused credential, a
// failure in the caller's own Go code -- means the cluster is up,
// whatever it said.
```

becomes

```go
// A nil error or an answer means the cluster is up, whatever the
// answer said. A rejected request, a refused credential and a failure
// in the caller's own Go code are all answers.
```

### `breaker.go` (commit 3)

The doc comments of `breaker`, `allow` and its three step comments,
`recordSuccess`, `recordFailure` and its two step comments,
`BreakerOpenError` and `IsClusterUnavailable`. The example, the type
comment at `internal/qdb/breaker.go:20-25`:

```go
// breaker fails fast for a cluster that stops answering: it opens after
// threshold consecutive failures that are evidence the cluster is
// unreachable or too busy to answer, half-opens after openFor to admit one
// probe, and closes again on a success. A call that the cluster answers --
// even by rejecting the request -- counts as a success: the cluster is
// healthy, the caller was wrong.
```

becomes

```go
// breaker fails fast for a cluster that stops answering. It opens after
// threshold consecutive failures that are evidence the cluster is
// unreachable or too busy to answer. It half-opens after openFor to
// admit one probe, and it closes again on a success. A call that the
// cluster answers counts as a success, and a rejected request is an
// answer: the cluster is healthy and the caller was wrong.
```

### `ingest.go` (commit 4)

The doc comments of `ErrInvalidPushOptions`, `PushOptions`,
`writerOptions`, `IngestResult`, `Decode`, `schemaOf`, `ingest` and its
overview, and `Ingest`. The overview of `Session.ingest` keeps its
five items and their headings; the semicolons inside items 1 and 3
become full stops:

```go
	//  1. decode the body under the held session's lookup; its error is
	//     returned as is, nothing has been staged; Parse is its duration;
```

becomes

```go
	//  1. decode the body under the held session's lookup. Its error is
	//     returned as is, because nothing has been staged yet. Parse is
	//     the decode's duration;
```

The doc comment of `Ingest` keeps "An ingest is never retried", which
states the invariant (`internal/AGENTS.md`, Code: a negation records an
invariant). Its reason stays the one the code gives, the read/write
distinction of `WithReadRetry` (`internal/qdb/cluster.go:262-265`). The
sentence becomes "An ingest is never retried, because a push is not a
read."

### `read.go` (commit 5)

The doc comments of `readBatchRows`, `ReadOptions`, `readerOptions`,
`ErrUnknownColumn`, `specialFields`, `dataFields`, `schemaOf` and its
overview, `emptyBatch`, `Batches`, `lent` and its two step comments,
`Session.Read` and its overview, and `Cluster.Read`. The example, the
overview of `schemaOf` at `internal/qdb/read.go:72-74`:

```go
	// The reader's layout: without a column list, $table, $timestamp, then
	// the table's columns; with one, exactly the requested names in their
	// order, the two specials answered only when named, like any column.
```

becomes

```go
	// The schema follows the reader's layout. Without a column list it is
	// $table, $timestamp, then the table's columns. With one it is the
	// requested names in their order, and the two specials appear only
	// when named, like any column.
```

("exactly" was emphasis; the list is the count.) The `Note:` in `lent`
(`internal/qdb/read.go:120-121`) keeps its marker.

### `table.go` (commit 6)

The doc comments of `ErrInvalidColumn`, `Column`, `info`,
`columnInfos`, `CreateTable`, `RemoveTable`, `IsTableExists` and
`IsTableNotFound`. `RemoveTable` (`internal/qdb/table.go:79-80`)
reads "removes the table name as u, and nothing else: a symtable is its
own entry, which other tables may share" and becomes "removes the
table called name, as user u. It leaves the table's symtable in place,
because a symtable is its own entry and other tables may share it."
"Nothing else" carried the fact that the symtable stays; the rewrite
keeps the fact and drops the phrase.

### `user.go`, `logger.go`, `qdb.go`, `budget.go`, `context.go` (commit 7)

Five small files, one commit. `user.go:5-8` keeps "never a REST session
or a token" as the rule it states (the pool key). `logger.go:10-15`
splits at its semicolon. `qdb.go:8-9` and `:14-15` become "read
through qdb_version(). No handle is opened." `budget.go` and
`context.go` are expected to need no change. If a file needs none, the
commit subject names only the files it touches.

### `cluster_test.go` (commit 8)

The file header, the doc comments of the helpers and of every test,
and the step comments inside `TestPerUserCapAndSharing` and
`TestIdleUserPoolEvicted`. A test's doc comment keeps the form
`// TestName: <claim>.` because every test in the package uses it. The
example, `internal/qdb/cluster_test.go:176-178`:

```go
// TestAuthenticate: the secure cluster accepts its user and refuses a
// wrong secret, the refusal being an answer that leaves the breaker
// closed and no pool behind; the anonymous user passes the insecure one.
```

becomes

```go
// TestAuthenticate: the secure cluster accepts its user and refuses a
// wrong secret. The refusal is an answer, so it leaves the breaker
// closed and creates no pool. The anonymous user passes the insecure
// cluster.
```

"never exceed" in `TestPerUserCapAndSharing` states the invariant the
test pins and stays. "exactly twice" in
`TestRetryOnceOnRetryableFailure` is the count the test asserts and
stays.

### `read_test.go` (commit 9)

The file header, `oneRow` and its two step comments, `concat`, `read`
and its step comment, `names`, `TestReadAnswersRowsWritten` and its two
step comments, and `TestReadDropsTrailingNUL` and its step comment.
"exactly those, in that order" in the test's doc comment is the count
the test asserts and stays. The example, `internal/qdb/read_test.go:28-29`:

```go
// oneRow is a one-row batch for tbl, its $timestamp at 2020-01-01 and s
// in its one string column; the caller releases it.
```

becomes

```go
// oneRow builds a one-row batch for tbl, with its $timestamp at
// 2020-01-01 and s in its one string column. The caller releases it.
```

### `docs/log.md` (commit 10)

One entry, newest first:

```
## 2026-10-08 -- internal/qdb reads as plain sentences; prose-audit-qdb-plan.md deleted

- No fact moved, because the plan carried none. The remaining audit
  units stay under Current state, Next, item 1.
```

Current state, Next, is not edited. This commit deletes this plan.

### Tests

The unit adds no test. The Go commits build and lint (`make lint`,
`go build ./...`), and `go test -p 1 ./internal/qdb/...` runs with the
services up, as the proof that no token changed.

## 4. Rationale

| decision                                                                                             | why                                                                                                                                                                                                       | rejected, and why                                                                                                                                    | gained                                                          | given up                                                                         | settled by                                                               |
| ---------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| 1. `internal/qdb` is the first package of the project-wide audit                                     | the audits so far followed the dependency graph outward from `internal/encoding` and `internal/model`; `internal/qdb` imports both and `internal/httpapi` imports it; it holds the shape (`AGENTS.md:63`) | auditing by density first (`internal/config/config.go`, `docs/bench.md` carry more candidates): density says nothing about what a reader meets first | the reference package reads the way the rule asks               | the densest files wait                                                           | proposal                                                                 |
| 2. One commit per file, the five small files together, non-test files before tests                   | the owner reviews one file's prose at a time; the earlier audits landed that way (9f39b4a, d3557ba, fa1a53a)                                                                                              | one sweep commit: a prose diff with no seam to review at                                                                                             | a reviewable diff per file                                      | nine commits for one subject                                                     | proposal; precedent 9f39b4a                                              |
| 3. Comment lines only; no code token changes                                                         | the unit is an audit of prose; a code change in the same diff hides among reworded lines; the skill's hard limit 1                                                                                        | fixing code smells met on the way                                                                                                                    | a diff that is prose by construction, checked hunk by hunk      | a code finding waits for its own unit, reported in the build-stage message       | proposal; precedent d3557ba                                              |
| 4. A claim that cannot be verified is kept verbatim and reported                                     | the rule keeps every fact and adds none (`AGENTS.md:92-94`); a reworded claim that was wrong becomes a confident wrong claim                                                                              | rewording it anyway, trusting the author                                                                                                             | no fact is altered by the audit                                 | some sentences stay unnatural until the owner answers                            | `AGENTS.md:54-58`; the skill's hard limits 2 and 3                       |
| 5. Emphasis words leave; rule words stay                                                             | rule 4 of the prose section removes "never", "exactly", "nothing else" and "the one" where they are emphasis, and keeps them where they carry meaning                                                     | removing every occurrence: "an ingest is never retried" and "the pool key is never a token" are invariants the code keeps                            | each remaining "never" states a rule                            | the executor judges each occurrence, section 3 naming the ones that stay         | `AGENTS.md:84-87`                                                        |
| 6. No function changes its qualification                                                             | the audit changes wording; the shape of every function was decided when it was written, and `Session.ingest` is the reference shape (`AGENTS.md:63`)                                                      | re-running `narrative.md` over the package in the same unit: a shape change hides among reworded lines, as a code change would                       | the diff is wording only                                        | a function that should be narrated or made bare waits for a narrative unit       | proposal; precedent `prose-audit-touched-plan.md`, Design (git 9653c1b^) |
| 7. An item-ending semicolon in a numbered overview stays; one that joins clauses inside an item goes | the audited encoding package keeps item-ending semicolons as list punctuation (`internal/encoding/csv.go:233-239`, d3557ba); rule 2 targets a semicolon that joins clauses                                | a full stop after every item: it breaks the one-list shape every narrated function in the project uses                                               | the overviews of `ingest` and `Read` keep the shape of the rest | none                                                                             | `internal/encoding/csv.go:233-239`                                       |
| 8. The rapid `.fail` files under `testdata` are untouched                                            | they are data the property tests replay, not prose                                                                                                                                                        | none                                                                                                                                                 | none                                                            | none                                                                             | proposal                                                                 |
| 9. `docs/log.md`, Next, item 1 is not edited                                                         | the item already says "every other comment and document in the project, one package or document per unit"; what landed is `git log` (`docs/AGENTS.md`, "Recorded nowhere")                                | listing the audited packages in the log: an inventory of what landed, which the routing table forbids                                                | the log carries no progress inventory                           | the next seed derives the remaining units from `git log --grep`, as this one did | `docs/AGENTS.md`, "Recorded nowhere"                                     |

## 5. Knowledge

| commit | lands                                                                                                                                                                                                          |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2 to 9 | the rule applied; no new why. Row 5 is visible in each kept "never", "exactly" and "nothing else". Row 7 is visible in the overviews of `ingest` and `Read`. Row 6 is visible in the unchanged step numbering. |
| 10     | the log entry naming the plan's deletion; row 9 in the unchanged Next item                                                                                                                                     |

Rows 1, 2, 3, 4 and 8 are about how this unit is built and land in the
commit list and the build-stage message, not in a document. Row 1's
recommendation for the order of the remaining units (section 8,
question 1) lands in the build-stage message for the owner, and the
next seed derives it again from the tree; the log's Next item does not
carry an order, by row 9.

## 6. How the knowledge lands

1. `/doc-discipline read` has run before this plan was written and runs
   again before commit 2 if the build stage is another session.
2. Each commit edits its file (commit 7: five files) by hand, sentence
   by sentence, under the procedure of section 3. `npx prettier --write`
   runs on `docs/log.md` in commit 10, and the four greps of
   `docs/AGENTS.md:193-212` run after it and print nothing new.
3. `git diff --cached` is reviewed hunk by hunk before each commit: a
   hunk that changes a non-comment token in a `.go` file is reverted and
   redone.
4. A why that arises while building is asked of the owner through the
   question tool before the commit that needs it. The audit expects
   none: it adds no reason.
5. Every sentence kept verbatim under section 3, step 5, goes on the
   "Rationale unknown" list of the build-stage message with its
   `file:line`.
6. After commit 9 and before commit 10:
   `/doc-discipline check internal/qdb docs/prose-audit-qdb-plan.md`.
   The report is quoted in the build-stage message. A finding is fixed
   by a further small commit on the branch before commit 10 deletes the
   plan.
7. Local verification: `source .envrc`, services up, `make lint`,
   `go build ./...`, `go test -p 1 ./internal/qdb/...`.

## 7. Commits

1. `docs(plan): prose-audit-qdb-plan.md, every comment in internal/qdb reads as a plain sentence` (this document)
2. `docs(qdb): cluster.go comments read as plain sentences`
3. `docs(qdb): breaker.go comments read as plain sentences`
4. `docs(qdb): ingest.go comments read as plain sentences`
5. `docs(qdb): read.go comments read as plain sentences`
6. `docs(qdb): table.go comments read as plain sentences`
7. `docs(qdb): user.go, logger.go, qdb.go, budget.go and context.go read as plain sentences`
8. `docs(qdb): cluster_test.go comments read as plain sentences`
9. `docs(qdb): read_test.go comments read as plain sentences`
10. `/doc-discipline check internal/qdb docs/prose-audit-qdb-plan.md`; one small commit per finding.
11. `docs(log): internal/qdb reads as plain sentences; prose-audit-qdb-plan.md deleted`
12. Verify: push `sc-19567/rr-prose-audit-qdb`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

1. The order of the remaining audit units is written nowhere.
   Recommendation: `internal/httpapi` (code, then its `AGENTS.md`),
   then `internal/auth`, `internal/config`, `internal/observe`,
   `internal/tlsconf`, `internal/qdbtest`, then `cmd/qdb_rest`, then
   the root `AGENTS.md` and `docs/AGENTS.md`, then `docs/bench.md`,
   the eight remaining ADRs and `docs/adr/README.md`, then `tests/e2e`
   (its `AGENTS.md`, `README.md`, `Makefile`, `golden.sh` and the
   bench's `AGENTS.md`, `README.md` and `Makefile`), then
   `scripts/cicd` and `.buildkite/AGENTS.md`, then
   `examples/qdb_rest.yaml`. The order is not recorded in the log (row
   9); each seed derives the next unit from `git log`.
2. `examples/qdb_rest.yaml` carries operator-facing comments, terse by
   an earlier owner preference. Recommendation: audit them under the
   rule in their own unit, keeping them terse.
3. The leftover branch `sc-19567/rr-ci-qdbd-logs` is accounted for by
   `docs/log.md`, In flight, and lives in its own worktree.
   Recommendation: it needs no decision for this unit.
