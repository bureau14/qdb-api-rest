# The encoders' tests become one file; "render" leaves the project -- Plan

Status: draft. Deleted when the unit lands (`docs/AGENTS.md`, Plans).

## 1. Outcome

When the unit lands:

- `internal/encoding/encode_test.go` is the counterpart of
  `decode_test.go`: one file holding the text round trip, the Arrow
  round trip, the hand-built edge batch with its byte-level pin, and
  the stream pins, reading top to bottom; `arrow_test.go`,
  `render_test.go` and `stream_test.go` are gone.
  `encoding_test.go` holds only the helpers both files share.
- The test-side body readers are named `read*`; `decode` is the
  package's word for its `Decoder` and nothing else.
- The word "render" names the encoding concept nowhere in the tree:
  not in code, comments, test names, file names, `AGENTS.md` files,
  the brief, the specifications, the live plan, the ADRs, the log, the
  e2e shell or the bench. The encoding concept is "encode"; the three
  formats that write a cell as text are "the text formats"; what Arrow
  writes is an encoding too. "Render" survives only where it does not
  mean encoding: the server-side rendered dashboard and six helpers
  that format a value for a log line, a usage text, a header or a
  fingerprint (Rationale, row 8).
- Every test proves exactly what it proved before; no test is added or
  removed.
- `docs/log.md` Current state lists the next unit, not this one; this plan is
  deleted.

Left for later units: the NDJSON and Arrow IPC decoders (`docs/log.md`,
Next), which join `codecs` in `decode_test.go` and the body formats
`table.Body` serves; the e2e tool and the flow.

## 2. Verified facts

- `decode_test.go:64` calls `encode` from `encoding_test.go:38`; the
  helper is shared by the two test files (2026-10-06, this session).
- `arrow_test.go:26` defines `decode`, `render_test.go:69,91,118` define
  `decodeJSON`, `decodeNDJSON`, `decodeCSV`; the package's own `Decoder`
  is `encoding.go:34`. Two meanings of "decode" in one package.
- `edgeBatch` (`render_test.go:266`) is used by `TestEdgeCells`
  (`render_test.go:300`) and by both stream tests (`stream_test.go:63,86`).
- `internal/encoding/testdata/rapid/` is untracked and ignored
  (`.gitignore:36-37`); renaming a test orphans nothing in git.
- Every occurrence of "render" outside vendored code, listed by
  `grep -rni render` on 2026-10-06, is enumerated in section 3 under
  "The sweep"; 69 lines across 27 files.
- The owner's words (2026-10-06, this session): "i don't like the term
  'render' for what is actually just 'encode'; do not just scope it to
  AGENTS.md, this re-wording is scoped over the entire project,
  including function renames, doc updates, file renames, etc"; ADRs and
  log entries: "Both get the swap"; the helpers outside encoding: "it is
  only about using the _concept_ of 'encoding'. so the others are fine,
  i think, unless they refer to a concept that is actually encoding".
- `internal/encoding/AGENTS.md:30` already says "the text wires cannot
  carry the empty string": "text" is the family word already in use.
- `.buildkite/AGENTS.md:42-46`: feature-branch builds are created
  through the API with `ignore_pipeline_branch_filters: true` and the
  full 40-character SHA.

## 3. Design

### `internal/encoding/encoding_test.go` (commits 5, 7)

Unchanged code. File header becomes:

```go
// The encoding tests share one fixture, a query run as the anonymous
// user and its encoding; the comparison with the generated table that
// was written is the fixture's (internal/qdbtest/table). The encoders'
// tests are encode_test.go, the decoders' decode_test.go.
```

`failer`, `run`, `encode` stay as they are, bare (narrative rule 1).

### `internal/encoding/encode_test.go` (commits 2 to 7)

Built by commit 2 as `git mv arrow_test.go encode_test.go`, then
appended to. After commit 5 it reads in this order, every helper above
its first use; after commits 6 and 7 the names and words below are
final. Each function keeps its body; only the names and comments
listed here change.

File header (commit 5):

```go
// The encoders are pinned without and with a cluster. With one: a
// generated table is read back as the binding's record batch, which the
// fixture's Check proves to be the table written, then encoded in every
// format, read back with the standard library and compared with that
// batch. Without one: what the fixture does not draw is pinned byte for
// byte on one hand-built batch, and the stream path of every encoder is
// pinned on the same batch twice.
```

Order and texts:

1. `readIPC(t failer, stream []byte) (*arrow.Schema, []arrow.Array, int)`
   (was `decode`, commit 6). Doc comment: "readIPC reads every batch of
   an IPC stream and concatenates the batches per column, returning the
   schema, the columns and the batch count. A stream with no batch has
   no columns to return." Bare body as today.
2. `cell`, `wireColumn` types. `wireColumn` doc comment (commit 7):
   "wireColumn is one column as a text format delivered it; kind is
   empty where the format carries no type."
3. `unmarshal`, `cellOf` unchanged.
4. `readJSON`, `readNDJSON`, `readCSV` (were `decodeJSON`,
   `decodeNDJSON`, `decodeCSV`, commit 6); doc comments keep their
   wording with the new name: "readJSON reads the columnar body.",
   "readNDJSON reads one object per line into the named columns; no
   rows is an empty body.", "readCSV reads the header and the records;
   the empty field is null."
5. `wordOf`, `parseTimestamp` ("parseTimestamp reads an encoded
   timestamp: RFC 3339 in UTC with exactly nine fractional digits, so
   the fixed width is pinned too.", commit 7), `parseInt`, `parseFloat`,
   `parseText`, `parsed`.
6. `checkValues` doc comment (commit 7): "checkValues compares every
   valid slot of a text column with the value encoded." `same`.
7. `checkCells` doc comment (commit 7): "checkCells compares one text
   column with the column encoded: name, wire type where carried, every
   validity bit, and every value parsed back from its text."
8. `checkText(t failer, rec arrow.RecordBatch, got []wireColumn)` (was
   `checkRendered`, commit 7): "checkText compares a text body with the
   batch encoded, column by column in order."
9. `TestTextRoundTrip` (was `TestRenderedRoundTrip`, commit 7). Doc
   comment: "TestTextRoundTrip: what the three text encoders put on the
   wire decodes to the table it was given, whatever the types, the
   nulls and the row count." Body unchanged; the three calls become
   `checkText(rt, rec, readJSON(...))` etc.
10. `TestArrowRoundTrip`: unchanged, its `decode(rt, ...)` call becomes
    `readIPC(rt, ...)` (commit 6).
11. `TestArrowNilBatch`: unchanged but for `readIPC` (commit 6).
12. `edgeBatch`: unchanged.
13. `TestEdgeCells` doc comment (commit 7): "TestEdgeCells pins the
    bytes of the edge batch in every text format: NaN and the infinity
    are null; invalid UTF-8 is U+FFFD in JSON and the raw byte in CSV;
    the leading space and the comma, quote and newline are what
    encoding/csv quotes; the empty string is the empty field."
14. `steps`, `encodeStream`, `ipcStream`: unchanged.
15. `TestStreamIsBatchesJoined`, `TestStreamErrorStep`: unchanged. The
    former `stream_test.go` header is folded into the file header above
    (commit 5); its sentence "a sequence of two batches renders as the
    two one-shot renderings joined" survives in
    `TestStreamIsBatchesJoined`'s doc comment as it already stands
    there ("two batches stream as ...").

No function in the file qualifies for a narrative (rule 2: each is a
reader, a comparer or a test whose steps the assertions state); the
existing step comments in `TestArrowRoundTrip` and `TestTextRoundTrip`
stay.

### The sweep: every "render" that means encoding (commits 8 to 12)

One row per line; "new" is the exact replacement. A row not in this
table keeps its word (Rationale, row 8).

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
| `internal/AGENTS.md:92`             | `an encoder, a wire shape or a cell rendering`                        | `an encoder, a wire shape or a cell's text`                          | 10     |
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
`.old-master/`, `tools/`, `setup/` and `.claude/` lists exactly: the
two dashboard lines of `docs/brief.md` (35, 684) and the six helpers
of Rationale row 8. The executor runs that grep and pastes its output
into the build-stage message.

### `internal/encoding/AGENTS.md`, Tests (commit 9)

Replaces the bullet at lines 89-98 whole:

```
- The encoders are one file, `encode_test.go`: one round trip per wire
  family against the live fixture, a generated table, queried once,
  encoded, read back with the standard library, compared cell by cell
  with the batch encoded, which the fixture's `Check` has proven to be
  the table written. What the table fixture does not draw is pinned
  byte for byte on one hand-built batch, and the stream path is pinned
  on the same batch, twice, as the two one-shot bodies joined, no
  cluster. The decoders are one file, `decode_test.go`: one generative
  round trip over every codec, tables drawn through the fixture,
  encoded as one body over `table.Body` and decoded back to the batches
  drawn, no cluster; the faults of a body are one table. A reader that
  parses a body in a test is `read*`; `decode` is the package's word
  for its `Decoder`. `encoding_test.go` holds what both files share.
```

### `docs/log.md` (commit 13)

Current state, Next: item 1 is removed and the list renumbered from 1.
Entries, newest first, one new entry:

```
## 2026-10-06 -- the encoders' tests are encode_test.go; encode-test-plan.md deleted

- Owner decision: "render" leaves the project; the concept is "encode",
  the three cell-writing formats are the text formats. The rules to
  `internal/encoding/AGENTS.md` (The text formats, Tests).
```

## 4. Rationale

| decision                                                                                                                                                                                                                                                                                                                                                | why                                                                                                                                                                          | rejected, and why                                                                                                                          | gained                                                     | given up                                                                                           | settled by                                            |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| 1. One `encode_test.go` holds the text round trip, the Arrow round trip, the edge pin and the stream pins                                                                                                                                                                                                                                               | the decoders already have one file; the encoders' tests mirror it                                                                                                            | three files by wire family: the layout the log asks to leave                                                                               | one place to read what the encoders prove                  | a longer file (about 520 lines)                                                                    | owner, `docs/log.md:43-47` (2026-10-06)               |
| 2. The merge is one file per commit, the first by `git mv`                                                                                                                                                                                                                                                                                              | each commit builds and passes; the owner diffs a move, not a rewrite                                                                                                         | one commit with the new file and three deletions: a diff with no move to follow                                                            | reviewable history                                         | three commits instead of one                                                                       | `.claude/commands/rr-start-next.md`, Every later turn |
| 3. `encoding_test.go` stays as the shared helper file                                                                                                                                                                                                                                                                                                   | `decode_test.go:64` calls `encode`; a decoder test that depends on the encoder test file by name reads backwards                                                             | folding the helpers into `encode_test.go`                                                                                                  | symmetry: two files of equals and one of what they share   | a third file                                                                                       | proposal                                              |
| 4. File order: readers, text round trip, Arrow round trip, edge batch, edge pin, stream pins                                                                                                                                                                                                                                                            | definitions before use; `edgeBatch` is used by the pin and both stream tests                                                                                                 | round trips first and readers below them, the order `render_test.go` has today for its parsers                                             | a reader never scrolls up                                  | none                                                                                               | `internal/AGENTS.md`, Code, book order                |
| 5. Test-side readers are `readIPC`, `readJSON`, `readNDJSON`, `readCSV`; the comparer is `checkText`                                                                                                                                                                                                                                                    | `Decoder`/`Decode` is the package's term (`encoding.go:34`); one word, one meaning                                                                                           | keeping `decode*` for the standard-library readers                                                                                         | a search for `decode` lands on the package's decoders only | none                                                                                               | proposal, applied to the file being created           |
| 6. "render" leaves the project wherever it means encoding; the concept is "encode"                                                                                                                                                                                                                                                                      | the owner: "i don't like the term 'render' for what is actually just 'encode'", scope "the entire project, including function renames, doc updates, file renames"            | keeping "render" as the word for a cell written as text and "encode" for a batch, the seed report's proposal; sunk by the owner's redirect | one word for one concept across code, tests, documents     | the cell/batch distinction is now carried by "the text formats" and by the sentence, not by a verb | owner, 2026-10-06                                     |
| 7. The family word for JSON, NDJSON and CSV is "the text formats" (the "text wires" on the wire)                                                                                                                                                                                                                                                        | the package already says "the text wires" (`internal/encoding/AGENTS.md:30`); the three write a cell as text and Arrow does not                                              | "the rendering encoders"; "the cell formats" (a new word)                                                                                  | an existing word extended, no new vocabulary               | none                                                                                               | proposal                                              |
| 8. ADRs and dated log entries get the word swap                                                                                                                                                                                                                                                                                                         | the owner: "Both get the swap"; a decision's wording changes, the decision does not                                                                                          | leaving them as written under `docs/AGENTS.md`, "append-only; supersede, never rewrite"                                                    | zero occurrences of the old word in the encoding sense     | the append-only rule is read as "no decision is rewritten", not "no word is"                       | owner, 2026-10-06                                     |
| 9. Six helpers keep "render": `versionText` (`cmd/qdb_rest/main.go:42`), `usageText` and the two `LogValue`s (`internal/config/config.go:76,129,327`), `retryAfter` (`internal/httpapi/query.go:64`), `fingerprint` (`internal/tlsconf/tlsconf.go:45`), `Err` (`internal/observe/observe.go:69`); so does the dashboard phrase (`docs/brief.md:35,684`) | the owner: "it is only about using the _concept_ of 'encoding'. so the others are fine"; each formats a value for a human-facing line or a header, none produces a wire body | a blanket sweep to zero occurrences                                                                                                        | the sweep stays about the concept                          | the word remains in the tree, outside encoding                                                     | owner, 2026-10-06                                     |
| 10. `headerFor` (`internal/auth/token.go:63`) is listed with the six                                                                                                                                                                                                                                                                                    | it writes the protected header's JSON text, formatting like the others; the JWE encoding of that header is the compact serialization elsewhere                               | reading it as encoding                                                                                                                     | consistency with row 9                                     | none                                                                                               | proposal; open question 1                             |
| 11. Test names keep "Arrow"; "Rendered" becomes "Text"                                                                                                                                                                                                                                                                                                  | row 6; `TestTextRoundTrip` pairs with `TestArrowRoundTrip` as the two wire families                                                                                          | `TestEncodeRoundTrip`, which would claim all four formats                                                                                  | the name says which family                                 | none                                                                                               | proposal                                              |

## 5. Knowledge

| commit  | lands                                                                                                                                                                                     |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2, 3, 4 | the three files' contents, moved; no new why                                                                                                                                              |
| 5       | file headers of `encode_test.go` and `encoding_test.go` (Design); row 3 (what `encoding_test.go` is for) in the `encoding_test.go` header's last sentence; row 4 in the file order itself |
| 6       | row 5 in the `readIPC` doc comment's name alone and in the `AGENTS.md` Tests bullet of commit 9 ("`decode` is the package's word for its `Decoder`")                                      |
| 7       | rows 6, 7, 11 in the renamed test and comparer and their doc comments                                                                                                                     |
| 8       | row 6 in the nine comment lines of `encoding.go`, `error.go`, `arrow.go`, `csv.go`, `json.go`                                                                                             |
| 9       | rows 1, 6, 7 in `internal/encoding/AGENTS.md`: the section name "The text formats" and the Tests bullet                                                                                   |
| 10      | row 6 in `internal/AGENTS.md`, `model.go`, `query.go`                                                                                                                                     |
| 11      | row 6 in the brief, `docs/e2e.md`, the flow plan, `common.sh`, `bench.py`                                                                                                                 |
| 12      | rows 6, 8 in the five ADRs and the 2026-09-11 log entry                                                                                                                                   |
| 13      | rows 6, 8, 9 in the new log entry (the owner decision and its scope: "render" leaves the project, the concept is "encode") and the deletion of this plan                                  |

Row 2 lands in the commit list itself. Row 9's list of survivors has
no document home; the rule is the concept ("render" never names
encoding), which the log entry states; the survivors are what the grep
of section 3 shows. Row 10 is open question 1.

## 6. How the knowledge lands

1. Run `/doc-discipline read` before commit 2.
2. Every commit carries its comments and document rows exactly as
   section 3 writes them, in the same commit as the code.
3. Commits 2 to 4 move text; commit 5 reorders and writes the two
   headers; commit 6 renames the readers; commit 7 renames the test
   and the comparer and rewrites the doc comments listed; commits 8 to
   12 are the sweep table, each file edited by hand at the lines
   listed, then `npx prettier --write` on every Markdown file touched.
4. A why that arises while building and is not here is written where
   it is decided, with its evidence, or asked of the owner through the
   question tool before the commit that needs it.
5. After commit 13: `/doc-discipline all internal/encoding`, one small
   commit per finding.
6. Before the build-stage message:
   `/doc-discipline check internal/encoding internal/AGENTS.md internal/model internal/httpapi/query.go docs docs/encode-test-plan.md`,
   and the grep of section 3; both reports quoted in the message.
7. Local verification at every commit: `source .envrc`, services up,
   `go test -p 1 ./internal/encoding/...`, `make lint`.

## 7. Commits

1. `docs(plan): encode-test-plan.md, the encoders' tests become one file and render leaves the project` (this document)
2. `test(encoding): encode_test.go opens with the Arrow round trip; arrow_test.go leaves`
3. `test(encoding): the text round trip and the edge pin join encode_test.go; render_test.go leaves`
4. `test(encoding): the stream pins join encode_test.go; stream_test.go leaves`
5. `test(encoding): encode_test.go reads top to bottom; the two headers say what each file holds`
6. `test(encoding): the body readers are read*; decode is the package's word alone`
7. `test(encoding): the text round trip and checkText replace the rendered ones`
8. `docs(encoding): the comments say encode and the text wires; render leaves the package`
9. `docs(agents): internal/encoding/AGENTS.md names encode_test.go; Rendering becomes The text formats`
10. `docs(internal): render leaves internal/AGENTS.md, model and the query handler`
11. `docs: render leaves the brief, e2e.md, the flow plan, the e2e shell and the bench`
12. `docs(adr): render leaves the ADRs and the 2026-09-11 log entry`
13. `docs(log): the encoders' tests are encode_test.go; encode-test-plan.md deleted`
14. `/doc-discipline all internal/encoding`, one small commit per finding, if any.
15. Verify: push `sc-19567/rr-encode-test`, build its head in Buildkite
    (`.buildkite/AGENTS.md`: API-created, branch-filter bypass, full
    SHA), wait for the result. Green: report the build number. Red:
    fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

1. `headerFor` (`internal/auth/token.go:63`, "renders the protected
   header") writes JSON text that the JWE then base64url-encodes. Is
   it formatting (keep "render", with the six helpers) or the encoding
   concept (becomes "writes the protected header")? Recommendation:
   keep, it is formatting; the encoding is the compact serialization.
