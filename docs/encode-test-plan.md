# The encoders' tests become one file; "render" leaves the project; the encoding package reads plainly -- Plan

Status: draft. Deleted when the unit lands (`docs/AGENTS.md`, Plans).

## 1. Outcome

When the unit lands:

- `internal/encoding/encode_test.go` is the counterpart of
  `decode_test.go`. It holds the text round trip, the Arrow round trip,
  the hand-built edge batch with its byte-level pin, and the stream
  pins, in that order. `arrow_test.go`, `render_test.go` and
  `stream_test.go` are gone. `encoding_test.go` holds only the helpers
  both files share.
- The test helpers that parse a body are named `read*`. `decode` is the
  package's word for its `Decoder` and nothing else.
- The word "render" names the encoding concept nowhere in the tree: not
  in code, comments, test names, file names, `AGENTS.md` files, the
  brief, the specifications, the live plan, the ADRs, the log, the e2e
  shell or the bench. The concept is called "encode". JSON, NDJSON and
  CSV, the three formats that write a cell as text, are "the text
  formats". Arrow's output is an encoding too. "Render" survives only
  where it does not mean encoding: the server-side rendered dashboard
  and six helpers that format a value for a log line, a usage text, a
  header or a fingerprint (Rationale, rows 9 and 10).
- The root `AGENTS.md` states how comments and documents read: plain
  sentences, each with a subject and a verb. Every comment in
  `internal/encoding` and every passage of `internal/encoding/AGENTS.md`
  follows that rule.
- Every test proves exactly what it proved before. No test is added or
  removed.
- `docs/log.md` Current state lists the following units, including the
  two audit slices below, without this one. This plan is deleted.

Left for later units, in this order (Rationale, row 13):

1. The prose audit of the other files this unit touches, whole:
   `internal/AGENTS.md`, `internal/model/model.go`,
   `internal/httpapi/query.go`, `docs/brief.md`, `docs/e2e.md`,
   `docs/e2e-v2-flow-plan.md`, the ADRs 0010, 0013 and 0014,
   `docs/log.md`, `tests/e2e/common.sh` and `tests/e2e/bench/bench.py`.
2. The prose audit of the rest of the project.
3. The NDJSON and Arrow IPC decoders, the e2e tool and the flow, as the
   log lists them today.

## 2. Verified facts

- `decode_test.go:64` calls `encode` from `encoding_test.go:38`. The two
  test files share that helper (2026-10-06, this session).
- `arrow_test.go:26` defines `decode`; `render_test.go:69,91,118` define
  `decodeJSON`, `decodeNDJSON`, `decodeCSV`. The package's own `Decoder`
  is `encoding.go:34`. "Decode" has two meanings in one package.
- `edgeBatch` (`render_test.go:266`) is used by `TestEdgeCells`
  (`render_test.go:300`) and by both stream tests
  (`stream_test.go:63,86`).
- `internal/encoding/testdata/rapid/` is untracked and ignored
  (`.gitignore:36-37`). Renaming a test orphans nothing in git.
- `grep -rni render` on 2026-10-06, outside vendored code, lists 69
  lines across 27 files. Section 3, "The sweep", enumerates every line
  that means encoding.
- The owner's words (2026-10-06, this session): "i don't like the term
  'render' for what is actually just 'encode'; do not just scope it to
  AGENTS.md, this re-wording is scoped over the entire project,
  including function renames, doc updates, file renames, etc". On ADRs
  and log entries: "Both get the swap". On the helpers outside
  encoding: "it is only about using the _concept_ of 'encoding'. so the
  others are fine, i think, unless they refer to a concept that is
  actually encoding".
- The owner's annotation on the first revision of this plan
  (plannotator, 2026-10-06), on the `encoding_test.go` header "The
  encoding tests share one fixture, a query run as the anonymous user
  and its encoding; the comparison with the generated table that was
  written is the fixture's (internal/qdbtest/table). The encoders'
  tests are encode_test.go, the decoders' decode_test.go.": "this reads
  unnatural and LLM generated". Then: "in general, audit the comments
  and docs for LLM-isms that do not read naturally". On the audit's
  scope: "Files this unit touches", with the project-wide audit as the
  next log item.
- `internal/encoding/AGENTS.md:30` already says "the text wires cannot
  carry the empty string". "Text" is the family word already in use.
- `.buildkite/AGENTS.md:42-46`: feature-branch builds are created
  through the API with `ignore_pipeline_branch_filters: true` and the
  full 40-character SHA.

## 3. Design

### The prose rule, root `AGENTS.md` (commit 13)

A new section after "Code comments":

```
## Prose

Comments and documents read like a colleague explaining the code, in
plain sentences. The rules:

1. Every sentence has a subject and a verb and says who does what.
   Write "the fixture compares the batch with the table it wrote", not
   "the comparison is the fixture's".
2. One idea per sentence. A colon introduces one example or one list
   of nouns, never a chain of clauses; a semicolon joins nothing.
3. No ellipsis: a sentence does not drop its verb, and a possessive
   does not stand in for a clause ("the decoders' decode_test.go").
4. No rhetoric: no "X, not Y" contrasts except to record a rejected
   alternative, no aphorisms, no mirrored clauses, no "the one", "and
   nothing else", "exactly" or "never" added for emphasis rather than
   meaning.
5. Name the thing: "the standard library parses the body", not "read
   back with the standard library"; "`Check`", not "the fixture's
   proof".

Read every comment or paragraph back as a colleague would say it
aloud. A sentence that no one would say is rewritten. The facts it
carried are kept; nothing is added.
```

`docs/AGENTS.md`, Style, gains one sentence: "Prose follows the root
`AGENTS.md`, Prose."

### `internal/encoding/encoding_test.go` (commits 5, 7)

Unchanged code. File header becomes:

```go
// Helpers shared by encode_test.go and decode_test.go. run executes a
// query as the anonymous user, and encode runs an encoder over a batch.
// The generated tables come from internal/qdbtest/table, which also
// compares what a test read back with what it wrote.
```

`failer`, `run` and `encode` keep their doc comments and stay bare
(narrative rule 1). The `failer` comment becomes "failer is the part of
testing.TB the helpers use, so a *testing.T and a *rapid.T both fit."

### `internal/encoding/encode_test.go` (commits 2 to 7, 14)

Commit 2 creates it as `git mv arrow_test.go encode_test.go`; commits 3
and 4 append the other two files. After commit 5 the file reads in the
order below, with every helper above its first use. Commits 6 and 7 set
the names and the doc comments below. Commit 14 audits what the list
leaves unchanged. Each function keeps its body.

File header (commit 5):

```go
// Tests for the encoders. The two round trips need a live qdbd. Each
// generates a table, writes it, queries it back as a record batch,
// encodes that batch in every format of its family, parses the output
// with the standard library and compares the result with the batch.
// The fixture's Check proves first that the batch is the table that was
// written. The other tests need no cluster. TestEdgeCells pins the
// exact bytes of values the fixture never generates, and the stream
// tests pin the stream path of every encoder on the same hand-built
// batch.
```

Order, names and doc comments:

1. `readIPC(t failer, stream []byte) (*arrow.Schema, []arrow.Array, int)`
   (was `decode`, commit 6). "readIPC parses an IPC stream and returns
   its schema, its columns with every batch concatenated, and the
   number of batches. A stream without a batch returns no columns."
2. `cell`: "cell is one parsed cell: whether it holds a value, and the
   value's text with the format's quoting removed." `wireColumn`
   (commit 7): "wireColumn is one column as parsed from a text format.
   kind is empty when the format carries no type."
3. `unmarshal` (bare, no comment), `cellOf`: "cellOf parses a raw JSON
   scalar. null is an invalid cell, a string is unquoted, a number
   keeps its own text."
4. `readJSON`, `readNDJSON`, `readCSV` (were `decodeJSON`,
   `decodeNDJSON`, `decodeCSV`, commit 6). "readJSON parses the columnar
   JSON body." "readNDJSON parses one object per line into the named
   columns. An empty body has no rows." "readCSV parses the header and
   the records. An empty field is null."
5. `wordOf`: "wordOf returns the wire type word for an Arrow type."
   `parseTimestamp` (commit 7): "parseTimestamp parses an encoded
   timestamp and checks its width: RFC 3339 in UTC with exactly nine
   fractional digits." `parseInt`, `parseFloat`, `parseText` bare.
   `parsed`: "parsed returns a reader that parses the text of cell i as
   a V and fails the test when it cannot."
6. `checkValues` (commit 7): "checkValues compares every valid cell of
   a parsed column with the value that was encoded." `same` bare.
7. `checkCells` (commit 7): "checkCells compares one parsed column with
   the column that was encoded: the name, the wire type when the format
   carries one, the validity of every cell, and every value parsed back
   from its text."
8. `checkText(t failer, rec arrow.RecordBatch, got []wireColumn)` (was
   `checkRendered`, commit 7): "checkText compares a parsed text body
   with the batch that was encoded, column by column."
9. `TestTextRoundTrip` (was `TestRenderedRoundTrip`, commit 7):
   "TestTextRoundTrip checks that the output of the three text encoders
   parses back to the batch they were given, for any column types,
   nulls and row count." The step comment after the query becomes "The
   fixture's Check proves that the batch is the table written, so the
   wire is compared with the batch and only one comparer, the
   fixture's, decides what was written."
10. `TestArrowRoundTrip`: "TestArrowRoundTrip checks that the Arrow
    encoder's output parses back to the batch it was given, for any
    types, nulls and row count, and across batch boundaries." Its
    `decode` call becomes `readIPC` (commit 6); its first step comment
    becomes the same sentence as in item 9; its second stays ("A batch
    size below the row count is what exercises slicing and the offset
    rebasing of string and blob columns.").
11. `TestArrowNilBatch`: "TestArrowNilBatch checks that a statement
    without a result set encodes as a complete stream with no fields
    and no batches." `readIPC` (commit 6).
12. `edgeBatch`: "edgeBatch builds one batch of the values the fixture
    does not generate: the int64 extremes, NaN and an infinity, a float
    above 1e21, the epoch and the nanosecond before it, the empty
    string, invalid UTF-8, the empty blob, and a null in every column.
    It also holds the values the byte-level pin needs exactly: a string
    that CSV must quote and a leading space."
13. `TestEdgeCells` (commit 7): "TestEdgeCells pins the exact bytes of
    the edge batch in every text format. NaN and the infinity become
    null. Invalid UTF-8 becomes U+FFFD in JSON and stays a raw byte in
    CSV. CSV quotes the leading space and the field that holds a
    comma, a quote and a newline. The empty string becomes the empty
    field."
14. `steps`: "steps yields every batch in recs, then err when it is not
    nil." `encodeStream`: "encodeStream runs the stream path of e over
    batches and returns the body." `ipcStream`: "ipcStream writes recs
    directly as an IPC stream: one schema, one record batch each, and
    the end marker."
15. `TestStreamIsBatchesJoined`: "TestStreamIsBatchesJoined checks that
    two batches stream as the IPC stream with two record batches, the
    CSV with one header, the NDJSON lines appended, and a JSON array of
    two results." `TestStreamErrorStep`: "TestStreamErrorStep checks
    that an error step after a batch ends every encoder's stream with
    that error."

No function in the file qualifies for a narrative (rule 2: each is a
parser, a comparer or a test whose assertions state its steps).

### The sweep: every "render" that means encoding (commits 8 to 12)

One row per line; "new" is the exact replacement. A line not in this
table keeps its word (Rationale, rows 9 and 10).

| file:line                           | old                                                                   | new                                                                  | commit |
| ----------------------------------- | --------------------------------------------------------------------- | -------------------------------------------------------------------- | ------ |
| `internal/encoding/encoding.go:40`  | `checks on the rendered wires`                                        | `checks on the text wires`                                           | 8      |
| `internal/encoding/error.go:11`     | `has no rendering`                                                    | `has no encoding`                                                    | 8      |
| `internal/encoding/arrow.go:86`     | `the nil-batch rendering`                                             | `the nil-batch encoding`                                             | 8      |
| `internal/encoding/csv.go:26`       | `Each type renders the way CSV readers expect`                        | `Each type is written the way CSV readers expect`                    | 8      |
| `internal/encoding/json.go:26`      | `bound to its JSON rendering`                                         | `bound to its JSON encoding`                                         | 8      |
| `internal/encoding/json.go:52`      | `so the caller renders them as null`                                  | `so the caller writes them as null`                                  | 8      |
| `internal/encoding/json.go:66`      | `as encoding/json renders bytes`                                      | `as encoding/json encodes bytes`                                     | 8      |
| `internal/encoding/json.go:74`      | `binds column a to its JSON rendering`                                | `binds column a to its JSON encoding`                                | 8      |
| `internal/encoding/json.go:251`     | `each batch is rendered exactly as Encode renders it`                 | `each batch is encoded exactly as Encode encodes it`                 | 8      |
| `internal/encoding/AGENTS.md:13`    | `each batch rendered before the next is pulled`                       | `each batch encoded before the next is pulled`                       | 9      |
| `internal/encoding/AGENTS.md:35`    | `ctx checks on the rendered wires`                                    | `ctx checks on the text wires`                                       | 9      |
| `internal/encoding/AGENTS.md:52`    | `## Rendering`                                                        | `## The text formats`                                                | 9      |
| `internal/encoding/AGENTS.md:55`    | `must know a type to render a cell. Each format renders every type`   | `must know a type to write a cell. Each format writes every type`    | 9      |
| `internal/encoding/AGENTS.md:89-98` | the Tests bullet                                                      | the Tests bullet below                                               | 9      |
| `internal/AGENTS.md:92`             | `an encoder, a wire shape or a cell rendering`                        | `an encoder, a wire shape or the text of a cell`                     | 10     |
| `internal/model/model.go:3`         | `The encoders and decoders render and read it`                        | `The encoders and decoders encode and decode it`                     | 10     |
| `internal/httpapi/query.go:118`     | `which every encoder renders as empty`                                | `which every encoder encodes as empty`                               | 10     |
| `internal/httpapi/query.go:126`     | `a column the encoder cannot render`                                  | `a column the encoder cannot encode`                                 | 10     |
| `docs/brief.md:724`                 | `renders cells through the jsontext appenders`                        | `writes cells through the jsontext appenders`                        | 11     |
| `docs/e2e.md:50`                    | `the v1 JSON renders timestamps in the server's local`                | `the v1 JSON encodes timestamps in the server's local`               | 11     |
| `docs/e2e.md:137`                   | `Rendering)`                                                          | `The text formats)`                                                  | 11     |
| `docs/e2e.md:160`                   | `renders null and the empty string alike`                             | `writes null and the empty string alike`                             | 11     |
| `docs/e2e.md:173`                   | `since it renders as null`                                            | `since it encodes as null`                                           | 11     |
| `docs/e2e.md:290`                   | `The binding's wrapError rendering is identical`                      | `The binding's wrapError message is identical`                       | 11     |
| `docs/e2e.md:429`                   | `for every rendered format` / `a renderer per format in shell`        | `for every text format` / `an encoder per format in shell`           | 11     |
| `docs/e2e-v2-flow-plan.md:42,70`    | `Rendering)`                                                          | `The text formats)`                                                  | 11     |
| `docs/e2e-v2-flow-plan.md:62`       | `rendered through internal/encoding's CSV`                            | `encoded through internal/encoding's CSV`                            | 11     |
| `docs/e2e-v2-flow-plan.md:64`       | `applied to every rendered format`                                    | `applied to every text format`                                       | 11     |
| `docs/e2e-v2-flow-plan.md:69`       | `CSV renders null and the empty string`                               | `CSV writes null and the empty string`                               | 11     |
| `docs/e2e-v2-flow-plan.md:83`       | `(it renders as null and`                                             | `(it encodes as null and`                                            | 11     |
| `docs/e2e-v2-flow-plan.md:172`      | as `docs/e2e.md:429`                                                  | as `docs/e2e.md:429`                                                 | 11     |
| `tests/e2e/common.sh:63`            | `The v1 JSON renders timestamps`                                      | `The v1 JSON encodes timestamps`                                     | 11     |
| `tests/e2e/bench/bench.py:406`      | `the v1 JSON renders server-local time`                               | `the v1 JSON encodes server-local time`                              | 11     |
| `docs/adr/0010-...:73`              | `a column the encoder cannot render`                                  | `a column the encoder cannot encode`                                 | 12     |
| `docs/adr/0013-...:58`              | `renders the batches through the CSV encoder`                         | `encodes the batches through the CSV encoder`                        | 12     |
| `docs/adr/0013-...:107`             | `a second rendering needs a second audited golden` / `a renderer bug` | `a second encoding needs a second audited golden` / `an encoder bug` | 12     |
| `docs/adr/0014-...:36`              | `rendered through the`                                                | `encoded through the`                                                | 12     |
| `docs/adr/0014-...:38`              | `every rendered format`                                               | `every text format`                                                  | 12     |
| `docs/adr/0014-...:46`              | `decodes the rendered formats to CSV`                                 | `decodes the text formats to CSV`                                    | 12     |
| `docs/adr/0014-...:70`              | `CSV renders null`                                                    | `CSV writes null`                                                    | 12     |
| `docs/adr/0014-...:86`              | `A renderer per format in the harness` / `a second rendering of JSON` | `An encoder per format in the harness` / `a second encoding of JSON` | 12     |
| `docs/log.md:231`                   | `with the rendering encoders landed`                                  | `with the text encoders landed`                                      | 12     |
| `docs/log.md:233`                   | `The rendering rules to`                                              | `The text-format rules to`                                           | 12     |

After commit 12, `grep -rni render` outside vendored code, `qdb/`,
`.old-master/`, `tools/`, `setup/` and `.claude/` lists exactly the two
dashboard lines of `docs/brief.md` (35 and 684) and the seven helpers
of Rationale rows 9 and 10. The executor runs that grep and pastes its
output into the build-stage message.

### `internal/encoding/AGENTS.md`, Tests (commit 9)

Replaces the bullet at lines 89-98 whole:

```
- The encoders' tests are in `encode_test.go`. Each wire family has one
  round trip against the live fixture: the test generates a table,
  queries it once, encodes the result, parses the output with the
  standard library and compares every cell with the batch it encoded.
  The fixture's `Check` has proven that batch to be the table written.
  Values the fixture does not generate are pinned byte for byte on one
  hand-built batch, and the stream path is pinned on that batch twice,
  as the two one-shot bodies joined; neither needs a cluster. The
  decoders' tests are in `decode_test.go`: one generative round trip
  over every codec draws tables through the fixture, encodes them as
  one body over `table.Body` and decodes them back to the batches it
  drew, without a cluster; the faults of a body are one table of cases.
  A test helper that parses a body is named `read*`, because `decode`
  is the package's word for its `Decoder`. `encoding_test.go` holds the
  helpers both files share.
```

### The prose audit of `internal/encoding` (commits 14 and 15)

Commit 14 covers every comment in `encoding.go`, `error.go`, `arrow.go`,
`csv.go`, `json.go`, `encode_test.go`, `encoding_test.go` and
`decode_test.go` that the items above do not already rewrite. Commit 15
covers every passage of `internal/encoding/AGENTS.md` that commit 9 does
not already rewrite. The executor applies the rule of commit 13 to each
comment and paragraph in turn: read it aloud, rewrite what no one would
say, keep every fact, add none. A comment whose facts the executor
cannot verify against the code is left as it is and reported under
"Rationale unknown" in the build-stage message. The rewrites are
comment and Markdown lines only; no code token changes.

### `docs/log.md` (commit 16)

Current state, Next: item 1 is replaced by two items, then the list
continues as it stands today:

```
1. The prose audit (root `AGENTS.md`, Prose) of the files the
   encode-test unit touched outside `internal/encoding`, whole:
   `internal/AGENTS.md`, `internal/model/model.go`,
   `internal/httpapi/query.go`, `docs/brief.md`, `docs/e2e.md`,
   `docs/e2e-v2-flow-plan.md`, ADRs 0010, 0013 and 0014, `docs/log.md`,
   `tests/e2e/common.sh`, `tests/e2e/bench/bench.py`.
2. The prose audit of every other comment and document in the project,
   one package or document per unit.
```

Entries, newest first, one new entry:

```
## 2026-10-06 -- the encoders' tests are encode_test.go; encode-test-plan.md deleted

- Owner decisions: the project says "encode", never "render", for the
  encoding concept, and JSON, NDJSON and CSV are the text formats
  (`internal/encoding/AGENTS.md`); comments and documents read as plain
  sentences (root `AGENTS.md`, Prose).
```

## 4. Rationale

| decision                                                                                                                                                                                                                                                                                                                                                | why                                                                                                                                                                                                                                         | rejected, and why                                                                                                                          | gained                                                                      | given up                                                                    | settled by                                            |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------- | --------------------------------------------------------------------------- | ----------------------------------------------------- |
| 1. One `encode_test.go` holds the text round trip, the Arrow round trip, the edge pin and the stream pins                                                                                                                                                                                                                                               | the decoders already have one file; the encoders' tests mirror it                                                                                                                                                                           | three files by wire family, the layout the log asks to leave                                                                               | one place to read what the encoders prove                                   | a longer file (about 520 lines)                                             | owner, `docs/log.md:43-47` (2026-10-06)               |
| 2. The merge is one file per commit, the first by `git mv`                                                                                                                                                                                                                                                                                              | each commit builds and passes; the owner diffs a move, not a rewrite                                                                                                                                                                        | one commit with the new file and three deletions, which git cannot show as a move                                                          | reviewable history                                                          | three commits instead of one                                                | `.claude/commands/rr-start-next.md`, Every later turn |
| 3. `encoding_test.go` stays as the shared helper file                                                                                                                                                                                                                                                                                                   | `decode_test.go:64` calls `encode`; a decoder test that depends on the encoder test file by name reads backwards                                                                                                                            | folding the helpers into `encode_test.go`                                                                                                  | two files of equals and one of what they share                              | a third file                                                                | proposal                                              |
| 4. File order: parsers, text round trip, Arrow round trip, edge batch, edge pin, stream pins                                                                                                                                                                                                                                                            | definitions before use; the pin and both stream tests use `edgeBatch`                                                                                                                                                                       | round trips first and parsers below them, the order `render_test.go` has today                                                             | a reader never scrolls up                                                   | none                                                                        | `internal/AGENTS.md`, Code, book order                |
| 5. Test helpers that parse a body are `readIPC`, `readJSON`, `readNDJSON`, `readCSV`; the comparer is `checkText`                                                                                                                                                                                                                                       | `Decoder`/`Decode` is the package's term (`encoding.go:34`); one word, one meaning                                                                                                                                                          | keeping `decode*` for the standard-library parsers                                                                                         | a search for `decode` lands on the package's decoders only                  | none                                                                        | proposal, applied to the file being created           |
| 6. "render" leaves the project wherever it means encoding; the concept is "encode"                                                                                                                                                                                                                                                                      | the owner: "i don't like the term 'render' for what is actually just 'encode'", scope "the entire project, including function renames, doc updates, file renames"                                                                           | keeping "render" as the word for a cell written as text and "encode" for a batch, the seed report's proposal; sunk by the owner's redirect | one word for one concept across code, tests, documents                      | the sentence, not a verb, now distinguishes a cell from a batch             | owner, 2026-10-06                                     |
| 7. The family word for JSON, NDJSON and CSV is "the text formats" (the "text wires" on the wire)                                                                                                                                                                                                                                                        | the package already says "the text wires" (`internal/encoding/AGENTS.md:30`); the three write a cell as text and Arrow does not                                                                                                             | "the rendering encoders"; "the cell formats", a new word                                                                                   | an existing word extended, no new vocabulary                                | none                                                                        | proposal                                              |
| 8. ADRs and dated log entries get the word swap                                                                                                                                                                                                                                                                                                         | the owner: "Both get the swap"; the wording of a decision changes, the decision does not                                                                                                                                                    | leaving them as written under `docs/AGENTS.md`, "append-only; supersede, never rewrite"                                                    | zero occurrences of the old word in the encoding sense                      | the append-only rule now means "no decision is rewritten", not "no word is" | owner, 2026-10-06                                     |
| 9. Six helpers keep "render": `versionText` (`cmd/qdb_rest/main.go:42`), `usageText` and the two `LogValue`s (`internal/config/config.go:76,129,327`), `retryAfter` (`internal/httpapi/query.go:64`), `fingerprint` (`internal/tlsconf/tlsconf.go:45`), `Err` (`internal/observe/observe.go:69`); so does the dashboard phrase (`docs/brief.md:35,684`) | the owner: "it is only about using the _concept_ of 'encoding'. so the others are fine"; each formats a value for a human-facing line or a header, none produces a wire body                                                                | a blanket sweep to zero occurrences                                                                                                        | the sweep stays about the concept                                           | the word remains in the tree, outside encoding                              | owner, 2026-10-06                                     |
| 10. `headerFor` (`internal/auth/token.go:63`) is listed with the six                                                                                                                                                                                                                                                                                    | it writes the protected header's JSON text, formatting like the others; the JWE encoding of that header is the compact serialization elsewhere                                                                                              | reading it as encoding                                                                                                                     | consistency with row 9                                                      | none                                                                        | proposal; open question 1                             |
| 11. Test names keep "Arrow"; "Rendered" becomes "Text"                                                                                                                                                                                                                                                                                                  | row 6; `TestTextRoundTrip` pairs with `TestArrowRoundTrip` as the two wire families                                                                                                                                                         | `TestEncodeRoundTrip`, which would claim all four formats                                                                                  | the name says which family                                                  | none                                                                        | proposal                                              |
| 12. The prose rule lives in the root `AGENTS.md`, a section of its own after "Code comments"; `docs/AGENTS.md` Style points at it                                                                                                                                                                                                                       | the rule covers code comments and documents alike, so it spans components (root `AGENTS.md`, Documentation strategy); the owner's annotation named a code comment and the follow-up "comments and docs"                                     | `docs/AGENTS.md` Style alone, which governs only `docs/`                                                                                   | one home, reachable from the root                                           | none                                                                        | proposal                                              |
| 13. This unit audits `internal/encoding` whole; the other files it touches and the rest of the project are the next two log items                                                                                                                                                                                                                       | the owner chose "Files this unit touches"; auditing `docs/brief.md`, `docs/e2e.md` and the ADRs whole adds a commit per document and puts the unit past the band of 15, so the cut falls at the package boundary, the first mergeable slice | the whole-file audit of every touched document in this unit (over the band); the plan's own texts only (less than the owner asked)         | a unit inside the band, with the encoding package consistent when it merges | the other touched files read plainly only after the next unit               | proposal; open question 2                             |
| 14. The rule's patterns come from the annotated header and from the texts this unit rewrites                                                                                                                                                                                                                                                            | the annotated header stands in for a clause with a possessive ("is the fixture's"), chains clauses with a semicolon, and drops a verb ("the decoders' decode_test.go"); the owner called it "unnatural and LLM generated"                   | a longer style guide                                                                                                                       | a rule the executor can apply mechanically, with one example per pattern    | patterns not yet seen are not listed; the read-aloud test catches them      | owner's annotation, 2026-10-06                        |

## 5. Knowledge

| commit  | lands                                                                                                                                                                            |
| ------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2, 3, 4 | the three files' contents, moved; no new why                                                                                                                                     |
| 5       | the file headers of `encode_test.go` and `encoding_test.go` (Design); row 3 in the first sentence of the `encoding_test.go` header; row 4 in the file order itself               |
| 6       | row 5 in the names of the parsers and in the `AGENTS.md` Tests bullet of commit 9 ("because `decode` is the package's word for its `Decoder`")                                   |
| 7       | rows 6, 7, 11 in the renamed test and comparer and their doc comments                                                                                                            |
| 8       | row 6 in the nine comment lines of `encoding.go`, `error.go`, `arrow.go`, `csv.go`, `json.go`                                                                                    |
| 9       | rows 1, 6, 7 in `internal/encoding/AGENTS.md`: the section name "The text formats" and the Tests bullet                                                                          |
| 10      | row 6 in `internal/AGENTS.md`, `model.go`, `query.go`                                                                                                                            |
| 11      | row 6 in the brief, `docs/e2e.md`, the flow plan, `common.sh`, `bench.py`                                                                                                        |
| 12      | rows 6, 8 in the five ADRs and the 2026-09-11 log entry                                                                                                                          |
| 13      | rows 12, 14 in the root `AGENTS.md`, Prose, and the pointer in `docs/AGENTS.md`, Style                                                                                           |
| 14, 15  | the rule of commit 13 applied; no new why                                                                                                                                        |
| 16      | rows 6, 8, 9, 12 in the new log entry (the owner decisions: "encode" never "render", the text formats, plain prose); row 13 in the two new Next items; the deletion of this plan |

Row 2 lands in the commit list itself. Row 9's list of survivors has no
document home: the rule is the concept, which the log entry states, and
the survivors are what the grep of section 3 shows. Row 10 is open
question 1. Row 13 is open question 2.

## 6. How the knowledge lands

1. Run `/doc-discipline read` before commit 2.
2. Every commit carries its comments and document rows exactly as
   section 3 writes them, in the same commit as the code.
3. Commits 2 to 4 move text; commit 5 reorders and writes the two
   headers; commit 6 renames the parsers; commit 7 renames the test and
   the comparer and writes the doc comments listed; commits 8 to 12 are
   the sweep table, each file edited by hand at the lines listed;
   commit 13 adds the prose rule; commits 14 and 15 apply it to the
   encoding package. `npx prettier --write` runs on every Markdown file
   a commit touches.
4. A why that arises while building and is not here is written where
   it is decided, with its evidence, or asked of the owner through the
   question tool before the commit that needs it.
5. After commit 16: `/doc-discipline all internal/encoding`, one small
   commit per finding.
6. Before the build-stage message:
   `/doc-discipline check internal/encoding internal/AGENTS.md internal/model internal/httpapi/query.go docs AGENTS.md docs/encode-test-plan.md`,
   and the grep of section 3; both reports quoted in the message, with
   the "Rationale unknown" list of commit 14.
7. Local verification at every commit: `source .envrc`, services up,
   `go test -p 1 ./internal/encoding/...`, `make lint`.

## 7. Commits

1. `docs(plan): encode-test-plan.md, the encoders' tests become one file and render leaves the project` (this document, revised by `docs(plan)` commits)
2. `test(encoding): encode_test.go opens with the Arrow round trip; arrow_test.go leaves`
3. `test(encoding): the text round trip and the edge pin join encode_test.go; render_test.go leaves`
4. `test(encoding): the stream pins join encode_test.go; stream_test.go leaves`
5. `test(encoding): encode_test.go reads top to bottom; the two headers say what each file holds`
6. `test(encoding): the body parsers are read*; decode is the package's word alone`
7. `test(encoding): the text round trip and checkText replace the rendered ones`
8. `docs(encoding): the comments say encode and the text wires; render leaves the package`
9. `docs(agents): internal/encoding/AGENTS.md names encode_test.go; Rendering becomes The text formats`
10. `docs(internal): render leaves internal/AGENTS.md, model and the query handler`
11. `docs: render leaves the brief, e2e.md, the flow plan, the e2e shell and the bench`
12. `docs(adr): render leaves the ADRs and the 2026-09-11 log entry`
13. `docs(agents): the root AGENTS.md says how comments and documents read`
14. `docs(encoding): every comment in the package reads as a plain sentence`
15. `docs(agents): internal/encoding/AGENTS.md reads as plain sentences`
16. `docs(log): the encoders' tests are encode_test.go; the prose audits are next; encode-test-plan.md deleted`
17. `/doc-discipline all internal/encoding`, one small commit per finding, if any.
18. Verify: push `sc-19567/rr-encode-test`, build its head in Buildkite
    (`.buildkite/AGENTS.md`: API-created, branch-filter bypass, full
    SHA), wait for the result. Green: report the build number. Red: fix
    with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

1. `headerFor` (`internal/auth/token.go:63`, "renders the protected
   header") writes JSON text that the JWE then base64url-encodes. Is it
   formatting (keep "render", with the six helpers) or the encoding
   concept (becomes "writes the protected header")? Recommendation:
   keep; it is formatting, and the encoding is the compact
   serialization.
2. The owner chose the audit scope "Files this unit touches". Auditing
   the touched documents whole puts the unit past the band, so this
   plan audits `internal/encoding` whole and lists the other touched
   files as the next log item (Rationale, row 13). Recommendation:
   accept the cut; the next unit starts from the log item at once.
