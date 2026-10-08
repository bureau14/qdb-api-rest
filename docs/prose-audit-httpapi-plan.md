# The prose audit of internal/httpapi -- Plan

Status: draft. Scaffolding for one unit on
`sc-19567/rr-prose-audit-httpapi`. Deleted when the unit lands
(`docs/AGENTS.md`, Plans).

## 1. Outcome

When the unit lands:

- Every comment in the ten source files of `internal/httpapi` other
  than `query.go` (`httpapi.go`, `bearer.go`, `compress.go`,
  `login.go`, `middleware.go`, `negotiate.go`, `problem.go`, `read.go`,
  `rows.go`, `tables.go`) reads as a plain sentence under the root
  `AGENTS.md`, Prose.
- Every rule in `internal/httpapi/AGENTS.md` reads as a plain
  sentence. Every rule survives. No rule is added.
- Every file carries the facts it carries today. No fact is added and
  none is removed. No code token changes. No function changes its
  qualification under `narrative.md`: `handleReadTable` and
  `handleIngestRows` keep their numbered overviews and step comments,
  `requireBearer`, `compressingWriter.Write` and `withCompression` keep
  their step comments, and the bare functions stay bare.
- `docs/log.md`, Current state, Next, item 1 names the tests of
  `internal/httpapi`, which the next slice audits, in place of the
  package, and it omits `docs/adr/README.md`, which 22c6b42 audited.
  `docs/log.md` has one new entry naming this plan's deletion.
- This plan is deleted.

Left for later units, in the order section 8 recommends:

1. The tests of `internal/httpapi`, ten files.
2. `internal/auth`, `internal/config`, `internal/observe`,
   `internal/tlsconf` and `internal/qdbtest` with its two subpackages.
3. `cmd/qdb_rest`, the root `AGENTS.md`, `docs/AGENTS.md` and
   `docs/bench.md`.
4. The ten ADRs other than 0010, 0013 and 0014.
5. `tests/e2e`: `AGENTS.md`, `README.md`, `Makefile`, `golden.sh`, and
   the bench's `AGENTS.md`, `README.md` and `Makefile`.
6. `scripts/cicd`, `.buildkite/AGENTS.md` and `examples/qdb_rest.yaml`.
7. The decoders, the e2e tool, the flow, the bench unit and the
   upstream filing, as `docs/log.md`, Next, lists them.

## 2. Verified facts

- The prose rule is `AGENTS.md:72-94` (root), five numbered rules and
  the read-aloud test. The comment shape is `AGENTS.md:40-70`
  (2026-10-08, this session).
- Which functions qualify for a narrative is
  `.claude/skills/doc-discipline/narrative.md:25-32`, four ordered
  rules. The shape of an overview and its step comments is
  `narrative.md`, "The shape" (2026-10-08).
- A negation belongs in a comment only where it records a rejected
  alternative or an invariant (`internal/AGENTS.md:16-18`, Code). This
  is the rule that decides which "never" survives (2026-10-08).
- The owner decided on 2026-10-08 that the rest of the audit is one
  unit with one commit per file (`docs/log.md`, Current state, Next,
  item 1, and the dated entry of 2026-10-08). The item names
  sixty-three files that have not landed. The seed report of this
  session cut it into seven slices and proposed this slice first; the
  owner answered "proceed" without answering the open questions, so
  section 8 carries them with recommendations.
- `internal/httpapi/query.go` was audited in 9f39b4a (2026-10-07) and
  `docs/adr/README.md` in 22c6b42 (2026-10-07). Both are listed by Next
  item 1 and are stale there (`git log --grep 'plain sentences'
--name-only`, 2026-10-08).
- The package was swept once, lightly, by 65b94fa (2026-10-06): twelve
  files of `internal/httpapi` lost a few sentences without a verb. No
  per-file audit of the package other than `query.go` has landed
  (2026-10-08).
- The density of what the rule targets, from `grep` on 2026-10-08
  over comment lines (`;`, `--`, `never|exactly|nothing else|the
one`, `, not `): `compress.go` 8 lines, `httpapi.go` 7, `bearer.go` 5,
  `login.go` 4, `read.go` 4, `rows.go` 4, `tables.go` 4,
  `middleware.go` 2, `problem.go` 2, `negotiate.go` 0.
  `internal/httpapi/AGENTS.md` carries a semicolon in most of its
  bullets. These are candidates to read, not a count of edits.
- The precedent for an audit commit's shape is 546b181 through 06b19e9
  (`internal/qdb`, 2026-10-08), 9f39b4a (`query.go`) and fa1a53a
  (`common.sh`): comment lines only, facts kept, one commit per file.
  The precedent for a reworded `AGENTS.md` is cc48268
  (`internal/AGENTS.md`) and b6a5ee9 (`internal/encoding/AGENTS.md`):
  every rule kept, the bullets reworded (2026-10-08, `git show`).
- The precedent for a numbered overview after the audit is
  `internal/encoding/csv.go:233-239` (d3557ba): the items of the list
  end in semicolons and the last in a full stop, and a semicolon inside
  an item that joined two clauses became a full stop. `Session.ingest`
  (`internal/qdb/ingest.go`, a6a2f72) followed it (2026-10-08).
- The narrated functions of the package, by `narrative.md` rule 3, are
  `handleReadTable` (`internal/httpapi/read.go:49-95`, a four-item
  overview and four numbered steps), `handleIngestRows`
  (`internal/httpapi/rows.go:74-113`, four numbered steps and three
  classifying step comments), `requireBearer`
  (`internal/httpapi/bearer.go:49-86`, four step comments),
  `compressingWriter.Write` (`internal/httpapi/compress.go:81-95`, one
  step comment), `withCompression` (`internal/httpapi/compress.go:118-132`,
  one step comment), `negotiateCoding` (`:28-39`, one step comment),
  `negotiate` (`internal/httpapi/negotiate.go:22-35`, one step
  comment), `handleLogin` (`internal/httpapi/login.go:54-90`, two step
  comments) and `handleCreateTable` (`internal/httpapi/tables.go:83-109`,
  one step comment). Every other function is bare under rule 1 or 2
  (2026-10-08, read whole).
- The `AGENTS.md` of the package records rules whose code is in this
  unit's files: the error mapping (`writeClusterError`, in `query.go`,
  not touched), the create's 409 and the delete's 404
  (`tables.go:104-107`, `:119-120`), the login's 401 without a
  challenge (`login.go:73-75`), the reader's two kinds of failure
  (`read.go:78-94`), the ingest's statuses (`rows.go:90-112`), the
  coding negotiation (`compress.go:28-39`), the held-back status
  (`compress.go:81-107`) and the `Vary` header (`compress.go:120`).
  Every rule of the file was read against its code on 2026-10-08 and
  none contradicts it.
- `docs/AGENTS.md:193-212` lists four greps that must stay clean after
  a document edit. `.buildkite/AGENTS.md:40-48`: a feature-branch build
  is created through the API with `ignore_pipeline_branch_filters:
true` and the full 40-character SHA.
- `make lint` and `go test` need `source .envrc`; the `httpapi` tests
  dial the pair of `scripts/tests/setup/start-services.sh` on 2836 and
  2838 and run serially (`internal/AGENTS.md`, Building and Tests). The
  linters are `.golangci.yml`: the standard set, `forbidigo` and
  `sloglint`, with `gofumpt` and `goimports`. None judges comment
  punctuation, so a comment edit cannot fail lint.
- A red Windows job whose qdbd log ends mid-flush is the daemon's bug,
  worked on `sc-19567/rr-ci-qdbd-logs`, and a re-run of the job is the
  answer (`docs/log.md`, In flight).

## 3. Design

The unit changes no signature, no type and no variable. It changes
comment lines in Go files and prose in one Markdown file. This section
says, per commit, what the executor reads and how each sentence is
judged and rewritten. The executor composes the rewritten sentences
from the rule and the file. The examples below fix the shape and are
typed in as written.

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
4. Keep every fact. Add none. Keep every path, identifier, RFC number,
   status code and cross-reference as it is. Keep the author's names
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

### `httpapi.go` (commit 2)

The package doc, and the doc comments of `handleLiveness`,
`handleReadiness`, `registerStatusRoutes` and `NewHandler`. The
example, `internal/httpapi/httpapi.go:46-49`:

```go
// NewHandler returns the root handler: every route registered, wrapped by
// the request middleware. Handlers take what they need -- the logger, the
// cluster -- from the request context, which the server derives from the
// process context; nothing is injected here.
```

becomes

```go
// NewHandler returns the root handler, which is every route registered
// and wrapped by the request middleware. A handler takes the logger and
// the cluster from the request context, which the server derives from
// the process context. NewHandler injects nothing.
```

The package doc's "It never imports v1 compatibility code" states the
rule of ADR-0007 and keeps "never". `handleLiveness`'s "It is never
cluster-aware" is emphasis and becomes "It does not look at the
cluster."

### `bearer.go` (commit 3)

The doc comments of `bearerToken`, `unauthorized` and `requireBearer`,
and the four step comments of `requireBearer`. "Applied per route,
never to the mux: the probes stay unauthenticated"
(`internal/httpapi/bearer.go:48`) records a rejected alternative and
keeps its "never"; the colon becomes a full stop and the sentence
gains its subject: "It is applied per route and never to the mux,
because the probes stay unauthenticated." The example,
`internal/httpapi/bearer.go:63-64`:

```go
		// Verify distinguishes a tampered or foreign token from a genuine
		// expired one; both are 401, the detail differs.
```

becomes

```go
		// Verify tells a tampered or foreign token from a genuine expired
		// one. Both answer 401, and the detail names which.
```

"never for the data plane" (`:74-75`) states the rule a refresh token
is held to and keeps its "never".

### `compress.go` (commit 4)

The doc comments of `coding`, `negotiateCoding`, `newCompressor`,
`compressingWriter`, `Close` and `withCompression`, the step comments
of `negotiateCoding`, `Write` and `withCompression`, and the trailing
comments of the two struct fields. "the server never compresses
uninvited" (`:25`) states ADR-0012's rule and keeps its "never".
"the bytes saved are the WAN client's gain, not this process's"
(`:43`) records the trade behind `BestSpeed` and keeps its contrast.
"so no empty frame is ever sent" (`:66`) is emphasis and becomes "so
no empty frame is sent". The example, `internal/httpapi/compress.go:62-67`:

```go
// compressingWriter compresses one response body in the negotiated
// coding. The status a handler writes is held back until the first body
// byte, when Content-Encoding is settled and the compressor opens; a
// handler that writes a status and no body gets that status uncompressed
// and unlabelled, so no empty frame is ever sent. Unwrap keeps
// http.ResponseController working through it.
```

becomes

```go
// compressingWriter compresses one response body in the negotiated
// coding. It holds the status a handler writes back until the first
// body byte, when Content-Encoding is settled and the compressor opens.
// A handler that writes a status and no body gets that status
// uncompressed and unlabelled, so no empty frame is sent. Unwrap keeps
// http.ResponseController working through it.
```

### `login.go` (commit 5)

The doc comments of `loginRequest`, `LogValue`, `user`,
`tokenResponse`, `isJSON`, `handleLogin` and `registerAuthRoutes`, and
the two step comments of `handleLogin`. The example,
`internal/httpapi/login.go:50-53`:

```go
// handleLogin proves the presented credentials by one direct dial and
// answers with an access token. A refused dial is the caller's 401, an
// unreachable cluster 503; nothing is minted for credentials the cluster
// did not accept.
```

becomes

```go
// handleLogin proves the presented credentials by one direct dial and
// answers with an access token. A refused dial is the caller's 401 and
// an unreachable cluster is 503. The handler mints nothing for
// credentials the cluster did not accept.
```

### `middleware.go` (commit 6)

The doc comments of `requestIDHeader`, `maxRequestIDLen`,
`validRequestID`, `newRequestID`, `requestID`, `responseRecorder` and
`withRequestLogging`. The example, `internal/httpapi/middleware.go:67-69`:

```go
// withRequestLogging tags the request context with its id, echoes the id,
// and emits one access line when the handler returns. Only the id rides
// on the context; lines join on it.
```

becomes

```go
// withRequestLogging tags the request context with its id, echoes the
// id, and emits one access line when the handler returns. Only the id
// rides on the context, and log lines join on it.
```

### `negotiate.go` (commit 7)

The doc comments of `encoders` and `negotiate`, and the step comment
of `negotiate`. The colon chain at `internal/httpapi/negotiate.go:18-21`
becomes sentences: "negotiate picks the encoder for an Accept header.
It reads the listed media ranges in order, and the first one an
encoder matches wins. `*/*`, an absent header and no match all mean
JSON. It does not read q weights, because a client that wants a format
names it."

### `problem.go` (commit 8)

The doc comments of `problemContentType`, `problem` and
`writeProblem`. "the one error shape every v2 endpoint answers with"
(`:9`) names the rule of ADR-0010 and keeps "the one" as a count. The
colon chain of `problem` (`:12-17`) becomes sentences, and its "never
a Go error" records the vocabulary rule and keeps "never".

### `read.go` (commit 9)

The doc comments of `parseTime`, `readOptions`, `handleReadTable` and
`registerReadRoutes`, the overview of `handleReadTable` and its four
step comments. The overview keeps its four items and their headings.
The semicolon inside item 1 ("read the parameters; a bad time is the
caller's 400 right here") joins two clauses and becomes a full stop:

```go
	//  1. read the parameters. A bad time is the caller's 400, answered
	//     here;
```

Item 3 ("with zero bytes out it is 500, after the first byte the
stream is cut and only the log hears of it") becomes "a failure of the
stream. With zero bytes out it is 500. After the first byte the stream
is cut and only the log hears of it;". The step comment of step 2
(`:70-71`) keeps its reason, the breaker and the pool hearing of a
fetch that found the cluster gone.

### `rows.go` (commit 10)

The doc comments of `rowsPath`, `maxIngestBytes`, `decoders`,
`decoderOf`, `acceptedTypes`, `pushOptions`, `ingestResponse`,
`handleIngestRows` and `registerRowsRoutes`, and the step comments of
`handleIngestRows`. `maxIngestBytes`'s "the owner's number" (`:23`)
is the constant's provenance (root `AGENTS.md`, Code comments, rule 4)
and stays. "never read whole" (`:72`) states the streaming rule of
`internal/httpapi/AGENTS.md`, Handlers, and keeps "never". The
example, `internal/httpapi/rows.go:70-73`:

```go
// handleIngestRows pushes the body's rows to their tables in one batch
// as the bearer's user and answers the counts. The body streams into the
// decoder under its cap, never read whole; the status is decided when the
// push has returned, since the answer is one small object.
```

becomes

```go
// handleIngestRows pushes the body's rows to their tables in one batch
// as the bearer's user and answers the counts. The body streams into
// the decoder under its cap and is never read whole. The status is
// decided when the push has returned, because the answer is one small
// object.
```

### `tables.go` (commit 11)

The doc comments of `tablesPath`, `maxShardSize`, `columnRequest`,
`createTableRequest`, the two shape errors, `decodeCreateTable`,
`shard`, `columns`, `caller`, `handleCreateTable`, `handleDeleteTable`
and `registerTableRoutes`, and the step comment of `handleCreateTable`.
"reads the body's shape and nothing more" (`:44`) states the
validation rule of `internal/AGENTS.md`, Code ("Validation has one
home") and keeps its meaning as "reads the body's shape only". The
example, `internal/httpapi/tables.go:29-30`:

```go
// createTableRequest is the create body. shard_size is in milliseconds,
// the C API's unit, and has no default: a pointer tells absent from zero.
```

becomes

```go
// createTableRequest is the create body. shard_size is in milliseconds,
// the C API's unit, and it has no default. A pointer tells an absent
// shard_size from a zero one.
```

### `internal/httpapi/AGENTS.md` (commit 12)

Every bullet of Handlers, Middleware and Tests, under the same
procedure. Every rule stays, with its status codes, headers, error
names and cross-references. "Never a 200 with an error body", "a
handler that has started streaming never retries", "never inside it",
"never retried" and "the probes stay outside both" state rules and
keep their words. A bullet that carries several rules joined by
semicolons becomes several sentences in the same bullet; no bullet is
split into two, so the file's shape stays the one `internal/AGENTS.md`
points at. The example, Handlers, the second bullet:

```markdown
- Every v2 error goes through `writeProblem`, one RFC 9457 body. A
  header that belongs to the status (`WWW-Authenticate`, `Retry-After`)
  is set before the call. Never a 200 with an error body.
```

becomes

```markdown
- Every v2 error goes through `writeProblem`, which writes one RFC
  9457 body. The handler sets a header that belongs to the status
  (`WWW-Authenticate`, `Retry-After`) before the call. No handler
  answers 200 with an error body.
```

`npx prettier --write internal/httpapi/AGENTS.md` runs after the edit.

### `docs/log.md` (commit 14)

Current state, Next, item 1 loses `internal/httpapi` as a package and
gains "the tests of `internal/httpapi`" in its place; it loses
`docs/adr/README.md`. The "Last updated" line becomes the commit's
date. One dated entry is appended, newest first, in the shape of the
entry of 2026-10-08:

```markdown
## <date> -- the internal/httpapi sources and AGENTS.md read as plain sentences; prose-audit-httpapi-plan.md deleted

- No fact moved, because the plan carried none. The audit item of
  Current state, Next, names what remains.
```

`npx prettier --write docs/log.md` runs after the edit, and the four
greps of `docs/AGENTS.md:193-212` print nothing new.

## 4. Rationale

| decision                                                                                                                                                     | why                                                                                                                  | rejected, and why                                                                                                                                    | gained                                                  | given up                                             | settled by                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ---------------------------------------------------- | --------------------------------------------- |
| 1. The audit item is cut into seven slices by package and file kind, and this unit is the first                                                              | the review band is 5 to 15 commits, and a package's sources review as one change                                     | one unit of 64 commits, as the log's item reads: one review cannot hold it                                                                           | a reviewable diff                                       | seven plan documents and seven builds instead of one | `proposal` (section 8, question 1)            |
| 2. Sources and `AGENTS.md` first, the tests second                                                                                                           | the sources are the contract and the `AGENTS.md` records the rules their comments cite, so the two are read together | alphabetical halves: splits `read.go` from `read_test.go` either way and pairs unrelated files                                                       | one review reads the rules and the code that keeps them | the tests wait one slice                             | `proposal`                                    |
| 3. One commit per file, the small files included                                                                                                             | the owner's decision of 2026-10-08                                                                                   | grouping `negotiate.go` and `problem.go`, as 4894528 did for five small files of `internal/qdb`: the decision came after that commit                 | each file's diff reviews alone                          | two commits of a few lines                           | `docs/log.md`, Next, item 1                   |
| 4. A "never", "exactly", "nothing else" or "the one" stays where it records a rejected alternative, an invariant or a count, and goes where it adds emphasis | the prose rule says so, and `internal/AGENTS.md` says where a negation belongs                                       | dropping every one: loses rules ("never to the mux", "never a Go error")                                                                             | the rules stay audible                                  | a judgment per word, listed in section 3             | `AGENTS.md:84-87`; `internal/AGENTS.md:16-18` |
| 5. Overviews keep their numbering, order and claims; list items keep their trailing semicolons                                                               | the overview is the table of contents of its step comments, and the precedent kept list punctuation                  | full stops at every item end: would differ from `csv.go` and `ingest.go`, two audited overviews                                                      | one list shape across the project                       | nothing                                              | `internal/encoding/csv.go:233-239`; a6a2f72   |
| 6. No non-comment token changes; the staged diff is read hunk by hunk                                                                                        | the unit is prose, and a code change would need its own review                                                       | fixing code smells found on the way: outside the unit, reported instead                                                                              | a diff the owner reads as prose                         | a finding in the code waits for its own unit         | c71f384, section 6.3 (the approved qdb plan)  |
| 7. No fact is added, none removed, no comment is deleted                                                                                                     | the prose rule ends "The facts it carried are kept; nothing is added"                                                | dropping a comment that only paraphrases syntax: that is `/doc-discipline narrative`'s job and changes what the file says                            | a diff that is only rewording                           | noise comments survive this unit                     | `AGENTS.md:92-94`                             |
| 8. `AGENTS.md` bullets are reworded in place, none split or merged                                                                                           | the file's bullets are the rules other `AGENTS.md` files point at, by section                                        | one bullet per rule: more bullets, and the pointers from `internal/AGENTS.md` name sections, so nothing breaks, but the file grows for no new fact   | the file's shape is unchanged                           | a long bullet stays long                             | `proposal`                                    |
| 9. The log commit trims Next item 1 to what remains                                                                                                          | the item enumerates files, and a list that names landed files is a stale entry; the seed report found two            | leaving the item and deriving the remainder from `git log` at each seed (c71f384, section 8.1): that worked while the item named packages, not files | the next seed reads the item as written                 | one log edit per slice                               | `proposal` (section 8, question 3)            |
| 10. A red Windows job whose qdbd log ends mid-flush is re-run, not fixed here                                                                                | the death is the daemon's bug, worked on another branch                                                              | waiting for the fix: it needs the qdbd team                                                                                                          | the Verify step is readable                             | one re-run per such job                              | `docs/log.md`, In flight                      |

## 5. Knowledge

| commit  | lands                                                                                                                                                                                                                                                                   |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2 to 11 | the rule applied; no new why. Row 4 is visible in each kept "never" and "the one" that section 3 names. Row 5 is visible in the unchanged overview of `handleReadTable` and the unchanged step numbers of `handleIngestRows`. Row 7 is visible in the unchanged claims. |
| 12      | the rule applied to the rules; row 8 is visible in the unchanged bullet count                                                                                                                                                                                           |
| 14      | the log entry naming the plan's deletion; row 9 in the trimmed Next item                                                                                                                                                                                                |

Rows 1, 2, 3, 6 and 10 are about how this unit is built and land in
the commit list and the build-stage message, not in a document. Row 1's
cut (section 1, "Left for later units") lands in the build-stage
message for the owner, and the trimmed Next item (row 9) names what
remains, so the next seed reads the slices from the item.

## 6. How the knowledge lands

1. `/doc-discipline read` has run before this plan was written and runs
   again before commit 2 if the build stage is another session.
2. Each commit edits its file by hand, sentence by sentence, under the
   procedure of section 3. `npx prettier --write` runs on
   `internal/httpapi/AGENTS.md` in commit 12 and on `docs/log.md` in
   commit 14, and the four greps of `docs/AGENTS.md:193-212` run after
   commit 14 and print nothing new.
3. `git diff --cached` is reviewed hunk by hunk before each commit: a
   hunk that changes a non-comment token in a `.go` file is reverted
   and redone.
4. A why that arises while building is asked of the owner through the
   question tool before the commit that needs it. The audit expects
   none: it adds no reason.
5. Every sentence kept verbatim under section 3, step 5, goes on the
   "Rationale unknown" list of the build-stage message with its
   `file:line`.
6. After commit 12 and before commit 14:
   `/doc-discipline check internal/httpapi docs/prose-audit-httpapi-plan.md`.
   The report is quoted in the build-stage message. A finding is fixed
   by a further small commit on the branch before commit 14 deletes the
   plan.
7. Local verification: `source .envrc`, services up, `make lint`,
   `go build ./...`, `go test -p 1 ./internal/httpapi/...`.

## 7. Commits

1. `docs(plan): prose-audit-httpapi-plan.md, every comment in internal/httpapi and its AGENTS.md reads as a plain sentence` (this document)
2. `docs(httpapi): httpapi.go comments read as plain sentences`
3. `docs(httpapi): bearer.go comments read as plain sentences`
4. `docs(httpapi): compress.go comments read as plain sentences`
5. `docs(httpapi): login.go comments read as plain sentences`
6. `docs(httpapi): middleware.go comments read as plain sentences`
7. `docs(httpapi): negotiate.go comments read as plain sentences`
8. `docs(httpapi): problem.go comments read as plain sentences`
9. `docs(httpapi): read.go comments read as plain sentences`
10. `docs(httpapi): rows.go comments read as plain sentences`
11. `docs(httpapi): tables.go comments read as plain sentences`
12. `docs(agents): internal/httpapi/AGENTS.md reads as plain sentences`
13. `/doc-discipline check internal/httpapi docs/prose-audit-httpapi-plan.md`; one small commit per finding.
14. `docs(log): internal/httpapi sources read as plain sentences; prose-audit-httpapi-plan.md deleted`
15. Verify: push `sc-19567/rr-prose-audit-httpapi`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

1. The log records the audit as one unit (owner decision, 2026-10-08),
   and this plan is the first of seven slices. Recommendation: the
   slices, because one review cannot hold 64 commits, and each slice
   merges on its own.
2. `docs/adr/README.md` (22c6b42) and `internal/httpapi/query.go`
   (9f39b4a) are listed by Next item 1 and have landed.
   Recommendation: commit 14 strikes them.
3. Whether the log commit of each slice trims Next item 1 to what
   remains, or the item stays whole and each seed derives the remainder
   from `git log`. Recommendation: trim, because the item names files.
4. The leftover branch `sc-19567/rr-ci-qdbd-logs` is the in-flight
   investigation in its own worktree. Recommendation: no action.
5. Whether the items of a numbered overview end in semicolons or full
   stops. Recommendation: semicolons, as `csv.go` and `ingest.go` do
   (row 5).
