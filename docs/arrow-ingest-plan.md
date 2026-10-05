# The Arrow ingest -- Plan

Status: draft. Scaffolding for one unit of work: `POST /api/v2/rows`
decodes its body into Arrow record batches, one per table, and pushes
them through the Go API's Arrow batch writer, so a body format is
read in `internal/encoding` and `internal/qdb` only leases, types and
pushes. Deleted when the unit lands; the rules move to the `AGENTS.md`
of `internal`, `internal/encoding` and `internal/httpapi` first
(`docs/AGENTS.md`, Plans). The owner set the direction on 2026-10-05
(the seed report's decisions, the answers to its questions and the
review of this plan, folded into the decision log below); this plan
turns it into code. The NDJSON and Arrow IPC bodies, the request
codings and the fixture's move to the Arrow writer are later units.

## What the ingest becomes

Arrow record batches are the one neutral representation of rows in this
server: the query and the bulk reader answer them, every encoder
renders them, and the Go API's `ArrowWriter` pushes them. An ingest
body is therefore decoded into batches and pushed as batches; nothing
is parsed into a QuasarDB type, and the rows cross the server once.
`internal/encoding` owns both directions of every wire format, one file
per format, behind `Encoder` and `Decoder` over batches. `internal/qdb`
owns the QuasarDB-to-Arrow type map, the lease and the push; it imports
`internal/encoding` for the neutral types (`SchemaOf`, `TableBatch`),
never the reverse, since the encoding package and its tests stay free
of cgo.

The session is held for the whole request: one `Call` leases a session
of the caller's pool, the decoder looks each table's schema up through
it the first time a row names the table, the Arrow writer pushes
through it, and the lease ends with the answer. The push is never
retried. The tables of one body share one column list, names and types
(owner, review of 2026-10-05): the header's names are typed by each
table, and a table whose types differ from the first's is refused with
the whole body.

Verified against qdb-api-go `7d49d1c` (2026-10-05), the branch head of
PR 125, vendored as `v3.9.1-0.20261005033010-7d49d1ca0c56`:

- `ArrowWriter.SetTable(name, batches...)` requires a `$timestamp`
  field of a timestamp type (any unit) or `date64` with no null slot,
  refuses a `$table` field, and accepts `int64`, `float64`,
  `timestamp`, `date64`, `utf8` and `binary` data fields, every one
  nullable (`writer_arrow.go`, `validateArrowSchema`,
  `validateArrowBatches`). The check is per table: the writer itself
  takes two tables of differing columns, so the one-column-list rule
  is this server's, checked in the decoder (the sentinel writer's own
  check, `writer.go:81`, does not apply).
- `ArrowWriter.Push(h)` skips tables with no rows and makes no C call
  when none remain; the batches are borrowed, the caller releases them
  after the push (`writer_arrow.go`, `Push`).
- `Session` is `HandleType` (`session.go:15`), so an `ArrowWriter`
  pushes through `s.session` as the sentinel writer does.
- Nulls are the validity bitmap. The server stores a zero-length string
  or blob as null whichever writer sent it, and reads a stored
  `MinInt64` or `NaN` as null; so the empty field stays null on the
  wire, the empty string stays unwritable, and a generator keeps
  avoiding the sentinels (Go API session, 2026-10-05, against
  `qdb/timeseries/none_value.hpp`).
- Server side, the Arrow push converts into the same batch-push path as
  the sentinel writer: push modes, deduplication and lazy creation are
  shared code (Go API session, 2026-10-05, `batch_table_push.cpp`).
  The push flags (write-through) are written by neither Go writer, a
  pre-existing gap outside this unit.
- Upsert without columns is refused by the writer's `Push`
  (`fillArrowTableOptions`); `PushOptions.writerOptions` refuses it
  earlier, before the lease, unchanged.
- The C API in the tree has the call (`qdb/include/qdb/ts.h:1450`); the
  local qdbd is 3.15.0.dev0.
- PR 125 is squash-merged, so `7d49d1c` never reaches upstream master.
  The owner's decision (2026-10-05): vendor the branch commit now; the
  vendored SHA is re-pointed to the squash commit before this branch
  fast-forwards into `sc-19567/rest-rewrite`.

## Design

Every function the unit adds or changes, with the doc comment it will
carry (the contract, per the root `AGENTS.md`, "Code comments") and,
where the body is more than one obvious step, the numbered overview its
body will state. Everything not named here is unchanged.

### `internal/encoding`

`encoding.go`, the contract of the lookup widens to the whole table:

```go
// SchemaOf answers the schema the bulk reader answers for the table whole:
// $table, $timestamp, then the data columns, in the reader's Arrow types.
// The error of a table the cluster does not know is the cluster's, as is.
type SchemaOf func(table string) (*arrow.Schema, error)
```

`Decoder`, `TableBatch` and `ErrInvalidRows` stay as they are. The CSV
dialect, the empty-field rule and the error chain are unchanged.

`csv.go`:

- `timestampField` leaves: the index field comes from the schema, so the
  package defines no field type of its own.
- `newCSVTable` gains the first table and picks its fields from the
  schema:

```go
// newCSVTable types the header's data columns by the table's schema:
// $timestamp first, then the header's names in their order, each a field
// of the schema or ErrInvalidRows. A table after the first must agree
// with it on every field's type, or ErrInvalidRows names both tables.
func newCSVTable(name string, h csvHeader, schemaOf SchemaOf, first *csvTable) (*csvTable, error)
```

Body: 1. look the table up through schemaOf; 2. pick `$timestamp` and
the header's names from it by name, a name missing is the body's
fault; 3. a second table's fields must equal the first's in type, the
one-column-list rule of the body; 4. one builder and one appender per
field.

- `Decode` passes the first table of `order` to `newCSVTable`; its
  overview's step 2 says so. Otherwise unchanged.

### `internal/qdb`

`ingest.go` keeps `ErrInvalidPushOptions`, `PushOptions`, `pushModes`,
`deduplicationModes`, `writerOptions` and `IngestResult` (its doc
comment loses the async sentence), and gains:

```go
// Decode reads one body into one batch per table, typing each table the
// first time the body names it through schemaOf. Its error is the body's
// or the lookup's, as is; on error there are no batches.
type Decode func(schemaOf encoding.SchemaOf) ([]encoding.TableBatch, error)

// schemaOf answers the reader's whole-table schema of name through s:
// its columns looked up through the held session, shaped as a read
// without a column list. The binding's error of an unknown table passes
// as is, so IsTableNotFound classifies it.
func (s *Session) schemaOf(name string) (*arrow.Schema, error)

// PushArrow writes every table w holds in one batch. The writer pins the
// Go buffers it hands to the C API for the duration of the call.
func (s *Session) PushArrow(w *qdbapi.ArrowWriter) error

// ingest runs decode under s's schema lookup and pushes its batches once
// under opts.
func (s *Session) ingest(decode Decode, opts qdbapi.WriterOptions) (IngestResult, error)

// Ingest pushes the batches decode reads as u in one batch under o. The
// session is held for the whole body: the tables' schemas are looked up
// through it as the body names them and the push runs through it.
// Invalid options fail before a session is leased; an invalid body or an
// unknown table fails the whole request before the push. Never retried:
// a push is not a read.
func (c *Cluster) Ingest(ctx context.Context, u User, o PushOptions, decode Decode) (IngestResult, error)
```

`Session.ingest`, the narrated function, body:

1. decode the body under the held session's lookup; its error is
   returned as is, nothing has been staged; `Parse` is its duration;
2. release every batch when the function returns, on every path,
   since the receiver of a decoded batch owns it;
3. stage every batch in one `ArrowWriter` under `opts`, `SetTable` per
   table; `Rows` sums the batches' rows and `Tables` counts the
   batches with any, since the writer skips the rest and a header-only
   body answers zeros;
4. no rows: answer without a push;
5. push through the session; `Push` is the call's duration.

`Cluster.Ingest` judges `o` before the lease and runs `ingest` inside
`Call`, the shape of `IngestCSV` today. `schemaOf` is
`Table(name).ColumnsInfo()` then `schemaOf(cols, ReadOptions{})` from
`read.go`, two steps, no narrative. `PushArrow` is a forwarder, bare.

Leaving `ingest.go`: `ErrInvalidRows`, `cells` and its five
implementations, `newCells`, `header`, `readHeader`, `ingestTable`,
`typedColumns`, `newIngestTable`, `appendRecord`, `writerTable`,
`ingestCSV`, `IngestCSV`. `Session.Push` stays for the fixture.

### `internal/httpapi`

`rows.go`:

```go
// decoders maps each media type the ingest accepts to its decoder.
var decoders = map[string]encoding.Decoder{
	encoding.CSVContentType: encoding.CSV{},
}

// decoderOf picks the decoder for a Content-Type by media type alone; a
// charset parameter is neither honored nor checked. The second value is
// false for a type the ingest does not accept.
func decoderOf(contentType string) (encoding.Decoder, bool)
```

`isCSV` leaves. `handleIngestRows` keeps its doc comment and its shape;
its steps become: 1. a body of a type no decoder reads is 415, the
detail naming the types `decoders` holds; 2. the body under
`http.MaxBytesReader` at the ingest cap; 3. `Cluster.Ingest` with a
`Decode` that runs the decoder over the body under the request's ctx; 4. the status by who failed: the cap's error in the chain is 413,
`qdb.ErrInvalidPushOptions` and `encoding.ErrInvalidRows` are 400,
`qdb.IsTableNotFound` is 404, the rest `writeClusterError` at 400; the
answer unchanged, 200 with `{"rows","tables","parse_ms","push_ms"}`.

### Tests

Few tests, each over a large drawn surface (owner, 2026-10-05): the
good path of a package is one generative property, the faults one
table.

`internal/encoding`, `decode_test.go`, replaces the hand-built batch:

```go
// codecs pairs every format that decodes with its encoder; a new decoder
// joins the slice and the round trip.
var codecs = []struct {
	Encoder
	Decoder
}{{CSV{}, CSV{}}}

// genSchema draws one to six data fields of distinct names over the five
// wire types, every field nullable, behind $table and $timestamp.
func genSchema(rt *rapid.T) *arrow.Schema

// genBatch draws zero to n rows of schema without the $table column: a
// $timestamp never null, every other cell null at a drawn rate, the
// values the text wires carry losslessly (no NaN, no infinity, no empty
// string, no empty blob, which the empty field folds into null).
func genBatch(rt *rapid.T, schema *arrow.Schema, n int) arrow.RecordBatch

// withTable is rec as an ingest body carries it: a $table column of name
// in front.
func withTable(t table.T, name string, rec arrow.RecordBatch) arrow.RecordBatch

// bodyOf is the batches as one body of e: the first table's bytes whole,
// the rows of the others under its header, which is theirs too.
func bodyOf(t table.T, e Encoder, tables []TableBatch) []byte

// TestDecodeRoundTrip: for every codec, one to three drawn tables of one
// schema, encoded as one body, decode back to the batches drawn, in
// first-seen order, the empty ones absent; a header alone is no table.
func TestDecodeRoundTrip(t *testing.T)

// TestCSVDecodeFaults: each fault of a body is ErrInvalidRows, naming the
// row and the column; a table schemaOf refuses is that refusal, as is.
func TestCSVDecodeFaults(t *testing.T)
```

`TestDecodeRoundTrip`, per iteration: 1. draw the schema and one to
three table names with a batch each, zero rows allowed; 2. the body
through `bodyOf`; 3. `Decode` under a `SchemaOf` answering the schema
for every name; 4. the batches with rows come back in first-seen
order, each `array.RecordEqual` to the one drawn, and every batch is
released. `TestCSVDecodeFaults` keeps today's rows and gains "tables of
differing type", a `SchemaOf` answering an `int64` for one name and a
`float64` for the other. `csvBatch`, `TestCSVDecodesWhatItEncoded` and
`TestCSVDecodeHeaderOnly` leave, folded into the property.

`internal/httpapi`: `TestRoundtrip` is the oracle, unchanged in shape:
each response equals the encoder over `Cluster.Read` or
`Cluster.Query`, and the direct read passes `table.Check` against the
rows generated, so the new push path is proven to write what the
sentinel fixture wrote. Step 5 reads back under every push mode
through the query and the bulk reader (the async exclusion leaves; the
server's async visibility is near instant, owner, 2026-10-05); the
`checkQuery` doc comment loses its async clause. If the CI agents'
qdbd disagrees, the build says so and the exclusion returns with the
daemon version that needs it. `TestErrorRows` keeps every ingest row,
"tables of differing type" included; the sentinel behind the
bad-request rows is `encoding.ErrInvalidRows`, which the rows do not
name.

`internal/qdb`: `Ingest` has no test of its own; the round trip drives
it. `internal/qdbtest/table` is untouched: it pushes through the
sentinel writer on purpose in this unit, as the oracle the new path is
proven against; its move to Arrow is the next unit.

Every test bounds its sessions; `go test -p 1 ./...` against the live
pair from `scripts/tests/setup/start-services.sh`.

## How the knowledge is captured

Whoever executes this plan writes every commit's comments and document
rows as the root `AGENTS.md`, "Code comments", prescribes, in the same
commit as the code: the doc comments above and the numbered overviews
of this Design section are the text, the Knowledge section below is
the evidence for every why. The `/doc-discipline` skill
(`.claude/skills/doc-discipline/SKILL.md`) is the instrument:

- The Knowledge rows are the skill's narrative plan table, decided
  before each commit: function, qualifies or bare, the rule, the
  why-evidence. A why without evidence is asked of the owner through
  the question tool before the commit, never guessed.
- After the last code commit and before the docs commits, run
  `/doc-discipline all internal/encoding internal/qdb internal/httpapi`
  so the rules of the three `AGENTS.md` files and the comments agree,
  and fix every finding in a small commit.
- Before the build-stage message, run `/doc-discipline check` over
  every path the branch touched and quote its report.
- After every Markdown edit, `npx prettier --write`; before the docs
  commit, the four greps of `docs/AGENTS.md`, Checks, over the touched
  documents.

## Knowledge

Per commit: the functions that qualify for a narrative, the why each
overview states and its evidence; the `AGENTS.md` rows and documents
the commit changes (`docs/AGENTS.md`, routing table); facts without a
home yet.

1. `refactor(encoding)`: `newCSVTable` qualifies (rule 3, a domain
   rule): the fields come from the reader's schema so the batch carries
   the reader's types (evidence: `internal/qdb/read.go:49-66`,
   `specialFields` and `arrowTypes`; `writer_arrow.go` SetTable takes
   any timestamp unit, so nothing here depends on nanoseconds); a
   second table must agree with the first in type because one body is
   one column list (evidence: the owner's decision of 2026-09-23 in
   `internal/AGENTS.md`, the ingest paragraph, and the review of
   2026-10-05). `genSchema` and `genBatch` are bare; the comment on
   `genBatch` says which values the text wires lose (evidence:
   `internal/encoding/AGENTS.md`, Rendering, CSV).
2. `feat(qdb)`: `Session.ingest` qualifies (rule 3): the lease spans
   decode and push because the lookups need a session and the push the
   same one (evidence: `internal/AGENTS.md`, the ingest paragraph;
   `read.go`, `Read`, the same shape); batches are released on every
   path because the receiver owns them (evidence: `encoding.go`,
   `TableBatch`); `Tables` counts batches with rows because the writer
   skips the rest (evidence: `writer_arrow.go`, `Push`). `Ingest`,
   `schemaOf` and `PushArrow` are bare beyond their contracts.
3. `refactor(httpapi)`: `handleIngestRows` keeps its step comments,
   renumbered as above; `decoderOf` and `decoders` are bare.
4. `refactor(qdb)`: deletions; `ingest.go`'s remaining comments are
   read back against the file (rule 6 of "Code comments").
5. `test(httpapi)`: the step comment of round-trip step 5 says every
   mode reads back through both paths. Home of the fact:
   `internal/AGENTS.md`, the ingest paragraph (commit 7).
6. `docs(agents)`: `internal/AGENTS.md`, Code: the ingest paragraph is
   rewritten: one push per request through one held session; the body
   decodes in `internal/encoding` through a schema lookup bound to the
   session; the tables of one body share one column list, names and
   types, the decoder's check; a body's failure is the whole request's,
   found before the push; rows are visible to the query and the bulk
   reader when the push returns, under every mode; `internal/qdb`
   imports `internal/encoding` for the neutral types, never the
   reverse. The sentence "The parsers of every body format live in
   `ingest.go`" leaves. `internal/AGENTS.md`, Building: one sentence
   after the branch-commit rule, that a squash-merged PR's branch
   commit is re-pointed to the squash commit before the fast-forward
   merge. `internal/encoding/AGENTS.md`, The seam: a paragraph for
   `Decoder` and `SchemaOf` (the whole-table schema, one batch per
   table, the receiver's release, the empty field as null, `$table`
   routed and never carried, the one-column-list check); Tests: the
   decode round trip is generative over drawn schemas, no cluster.
   `internal/httpapi/AGENTS.md`, Handlers, the ingest: `Content-Type`
   negotiated through `decoders`, `encoding.ErrInvalidRows` named;
   Tests: the round trip reads back under every mode through both
   paths. `docs/brief.md`, Project structure: `internal/encoding/` is
   "format encoders and decoders". `docs/e2e-v2-flow-plan.md:71`: "the
   ingest decoder" for "the ingest parser".
7. `docs(log)`: Current state, Next 1 becomes the fixture's move to the
   Arrow writer (`internal/qdbtest/table` holds a batch, pushes through
   `ArrowWriter`, renders CSV through the encoder, checks by batch
   equality) followed by the NDJSON and Arrow IPC decoders; one entry
   for the unit; this plan deleted.

Facts with no home until commit 7: the one-column-list check being the
decoder's, and the squash-merge re-vendor rule; both land in
`internal/AGENTS.md` as said above.

## Commits

1. `refactor(encoding): the decoder takes the table's read schema, picks its fields by name and refuses a table whose types differ from the first's`
2. `test(encoding): the decode round trip draws schemas and tables for every codec; the faults stay one table`
3. `feat(qdb): Ingest leases one session, lends the decoder its schema lookup and pushes the batches through the Arrow writer`
4. `refactor(httpapi): the ingest decodes through the body's decoder and pushes through Cluster.Ingest`
5. `refactor(qdb): IngestCSV and the cell parsers leave`
6. `test(httpapi): the round trip reads back under every push mode through the query and the bulk reader`
7. `docs(agents): the body formats decode and encode in encoding; the push is qdb's`
8. `docs(log): the ingest decodes to Arrow; arrow-ingest-plan.md deleted`
9. Verify: push `sc-19567/rr-ingest-csv`, build its head in Buildkite,
   wait for the result. Green: report the build number. Red: fix with
   further small commits on this branch, push, build again.

Between 6 and 7: `/doc-discipline all` over the three packages, a
finding one more small commit. Before the build-stage message:
`/doc-discipline check`. Before the fast-forward merge, in the merge
stage: re-point `go.mod` at the squash commit of PR 125, `go mod tidy`,
`go mod vendor`, one `build(deps)` commit, a green build.

Every commit builds and passes `make lint`; the Go tests run against
the live pair.

## Open questions and recommendations

None; the owner settled the seed report's questions and the review's
findings on 2026-10-05.

## Decision log (2026-10-05)

| Decision                                                  | Why                                                                                              | Rejected                                                        |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------------ | --------------------------------------------------------------- |
| Arrow record batches are the neutral representation       | the reader answers them, the encoders take them, the Arrow writer pushes them; no second copy    | a Go cell model per type in `internal/qdb`                      |
| Decoders live in `internal/encoding` next to the encoders | a decoder needs no QuasarDB type; both directions of a format in one file                        | parsers in `ingest.go` (the rule of 2026-09-23, reversed)       |
| `internal/qdb` imports `internal/encoding`                | the neutral types stay out of cgo; encoding's tests run without a cluster                        | `TableBatch` and `SchemaOf` owned by `internal/qdb`             |
| One `Ingest` call, the decoder a callback under the lease | schema lookups and the push need one session; the one-held-session rule stands                   | a lease per lookup and one for the push; a schema cache         |
| `SchemaOf` answers the reader's whole-table schema        | the wire schema is the binding's; one definition of `$timestamp` and the type map (`read.go`)    | the decoder defining its own index field (`timestampField`)     |
| One column list per body, types included, the decoder's   | one kind of table per batch is how ingestion is done (owner); the Arrow writer checks per table  | accepting tables of differing types since the writer takes them |
| Vendor the PR commit now, re-point before the merge       | progress over waiting on a merge date (owner)                                                    | waiting for the squash commit                                   |
| The fixture keeps the sentinel writer in this unit        | it is the oracle the new push path is proven against; moving both at once makes a failure opaque | moving the fixture to Arrow here                                |
| `async` reads back through both paths                     | the server's async visibility is near instant (owner)                                            | keeping the query-only read-back                                |
| One generative round trip per package, one fault table    | few tests over a large drawn surface (owner); a new codec joins a slice, not a test              | a hand-built batch per decoder; a test per fault                |
| The CSV wire and the handler's answer unchanged           | the contract is the handler's; the pivot is internal                                             | a new response shape                                            |
