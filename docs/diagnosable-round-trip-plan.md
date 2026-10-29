# Diagnosable round trip -- plan

Status: draft

## Outcome

When a round trip comparison fails, `compare_csv` runs a new
`e2etool diff` over the generated CSV and the read CSV. The diff
decodes both through the server's CSV decoder and the round trip
schema. It matches rows by `$timestamp` and writes a report: the row
counts, the missing timestamps, the extra timestamps, the changed cells
and the breaks in timestamp order. The full report lands next to the
body as `<name>.diff`, and its head goes to stderr. A rapid property
test pins the diff without a cluster. When the round trip fails in CI,
`scripts/cicd/40.test-e2e.sh` packs `tests/e2e/actual/round-trip` and
both REST logs into `e2e-round-trip.tar.gz` at the repository root.
The `bureau14/qdb-test-report` plugin uploads that archive to R2
through `job.artifacts`. A green run writes no archive.

Left for later units:

- The row count in the server's log (`docs/log.md`, Next 1.3). It needs
  the M7 per-query line, and no query line exists today (Verified
  facts). The query capture unit delivers it.
- The investigation of the flaky DuckDB read-back (`docs/log.md`,
  Next 2), which uses this unit's report and archive.

## Verified facts

- `compare_csv` decodes every body except the reader's raw CSV through
  `e2etool tocsv`, compares the two files with `cmp -s`, and on a
  mismatch diffs only the first 4096 bytes of each, which never changes
  its result [`tests/e2e/common.sh:308-320`].
- The round trip and the stress both call `compare_csv`
  [`tests/e2e/round-trip.sh:92,188`, `tests/e2e/stress.sh:155`].
- The CSV decoder answers one `model.TableBatch` per table that the
  body names, in first-seen order. It types each table through
  `schemaOf` [`internal/encoding/csv.go:381-460`]. It drops `$table`
  and puts `$timestamp` first, then the data columns in schema order
  [`tests/e2e/tools/e2etool/main.go:305-312`]. A body with a header and
  no rows answers no batch [`internal/encoding/csv.go:417-459`, the
  loop builds a table only on its first record].
- `toCSV` already builds the decoder's schema as `roundTripSchema` with
  `$table` in front through `table.WithTable` and a `releaser`
  [`tests/e2e/tools/e2etool/main.go:315-321`].
- The fixture draws `$timestamp` strictly ascending with a step of at
  least one nanosecond [`internal/qdbtest/table/table.go:138-147`].
  The edge rows sit on the day before the fixture's index start, so the
  concatenation keeps `$timestamp` ascending
  [`tests/e2e/tools/e2etool/main.go:51-54`]. A `$timestamp` value
  therefore names one generated row.
- The fixture draws no NaN and no infinity
  [`internal/qdbtest/table/table.go:105-106`], so exact cell equality
  through arrow's `array.SliceEqual` is sound for every column
  [`vendor/github.com/apache/arrow-go/v18/arrow/array/compare.go:357`].
- The e2e tool is part of the root module, and no `go.mod` exists under
  `tests/`. `30.test.sh` runs `go test ./...` with `-race`, so a test
  in the tool's package runs in CI on every platform
  [`scripts/cicd/30.test.sh:38`].
- The failure branch of the CI step prints the last 100 lines of each
  REST log and the qdbd logs, then exits 1. Its comment says that these
  files are not Buildkite artifacts
  [`scripts/cicd/40.test-e2e.sh:105-142`].
- The REST logs are `tests/e2e/rest.log` and `tests/e2e/rest-secure.log`
  [`tests/e2e/Makefile:43,88`]. The round trip writes its files under
  `tests/e2e/actual/round-trip/<cluster>/` and only creates the
  directory, so an earlier run's files stay until they are overwritten
  [`tests/e2e/round-trip.sh:33,361,396`].
- The test-report plugin block in `_build.yml` carries `title`,
  `job.variant` and `job.junit_input_path` only
  [`.buildkite/steps/_build.yml:40-44`]. `pipeline.py` passes it
  through unchanged [`.buildkite/pipeline.py:215-221`].
- The plugin's `job.artifacts` takes a list of `name` and `input_path`.
  A missing or empty input logs a warning, is recorded in
  `summary.json`, and does not fail the report
  [`~/git/qdb-test-report-buildkite-plugin/README.md:62,172`]. The
  plugin runs as a `post-command` hook
  [`~/git/qdb-test-report-buildkite-plugin/hooks/post-command`], so it
  runs after a failed command too.
- quasardb uploads a tar.gz of its query server logs through the same
  option [`~/git/quasardb/.buildkite/steps/_test.yml:36-40`].
- Nothing in this repository calls `tar`, and no document says whether
  the Windows (MSYS2) and FreeBSD agents carry it. `gunzip` is present
  on every agent, and `zstd` is not (`docs/e2e.md`, In Buildkite and
  The v2 round trip).
- No query log line exists on either plane. `query.Run` logs nothing
  [`internal/query/query.go:66-77`]. The HTTP access line carries the
  method, path, status, bytes and duration only
  [`internal/httpapi/middleware.go:72-84`], and the Flight line carries
  the method, code and duration only
  [`internal/flightsql/logging.go:23-31`].

## Design

### `tests/e2e/tools/e2etool/diff.go` (new)

The file holds the diff and its report. `main.go` gains the subcommand
and the usage line. One new file was chosen so that the property test
sits beside the code it tests (Rationale, "diff.go").

```go
// rowDiff is what a read answered against the generated rows, keyed by
// $timestamp. The zero value is no difference. [Rationale, "key"]
type rowDiff struct {
	// style: doc comment alone, one line per field
	// because: a plain record of findings
	//
	// wantRows and gotRows are the two row counts.
	// missing holds the generated timestamps the read never answered, in generated order. [Outcome]
	// extra holds the read rows whose timestamp the generated rows lack or that repeat a timestamp, in read order. [Rationale, "duplicate"]
	// changed holds each cell that differs on a matched row: timestamp, column, generated value, read value. [docs/log.md, Next 1.1]
	// breaks holds each read row whose timestamp is below the previous row's. [Rationale, "order break"]
}
```

```go
// extraRow is one read row that matches no generated row. duplicate says
// that its timestamp names a generated row the read already answered.
type extraRow struct{ /* row, ts, duplicate */ }

// changedCell is one cell of a matched row whose values differ. The
// values are arrow's text for each cell. [Rationale, "cell text"]
type changedCell struct{ /* ts, column, want, got */ }

// orderBreak is one read row whose timestamp is below the previous row's.
type orderBreak struct{ /* row, prev, ts */ }
```

```go
// empty reports whether the diff found nothing.
func (d rowDiff) empty() bool {
	// style: doc comment alone
	// because: one boolean expression over the four lists and the counts
}
```

```go
// diffRows compares a read with the generated rows. Both batches are in
// the decoder's layout, $timestamp first and then the data columns in
// schema order, under one schema. [main.go:305-312]
// The generated timestamps must be unique, which the fixture
// guarantees. [table.go:138-147]
func diffRows(want, got arrow.RecordBatch) rowDiff {
	// style: doc comment + numbered overview + step comments
	// because: a keyed comparison with four outcomes per read row, and the order of the checks decides which outcome a row gets
	//
	// The read is walked once in its own order, so an order break is seen where it happens. [Rationale, "key"]
	// A map from timestamp to generated row replaces a merge, because a merge assumes the read is ordered, and a disorder is one of the things to find. [Rationale, "key"]
	//
	// 1. Index the generated rows by timestamp. [table.go:138-147]
	// 2. Walk the read rows. A row whose timestamp is below the previous row's is an order break, and the walk goes on to classify it. [Rationale, "order break"]
	// 3. A timestamp the generated rows lack is extra, and so is one the walk has already matched, marked as a duplicate. [Rationale, "duplicate"]
	// 4. A matched row compares each data column with array.SliceEqual, which treats two nulls as equal. Each differing cell is recorded with arrow's text for both values. [compare.go:357, Rationale, "cell text"]
	// 5. The generated timestamps the walk never matched are missing, in generated order.
}
```

```go
// writeDiff writes the report as plain lines: the two row counts, then
// one line per missing timestamp, extra row, changed cell and order
// break. An empty diff writes one line saying that the rows are equal
// after decoding. [Rationale, "decoded equal"]
func writeDiff(w io.Writer, d rowDiff) error {
	// style: doc comment alone
	// because: one line per finding, in a fixed order
	//
	// at the timestamps: written as UTC RFC 3339 with nanoseconds, so a line can be matched by eye against rows.csv. [Rationale, "cell text"]
}
```

```go
// decodeRows decodes one CSV file of the named round trip table into
// the decoder's layout. A file with a header and no rows answers an
// empty batch under the given schema. [csv.go:417-459]
// The caller releases the batch.
func decodeRows(path, name string, empty *arrow.Schema) (arrow.RecordBatch, error) {
	// style: doc comment + numbered overview + step comments
	// because: three outcomes of the decoder (no table, one table, several) map to three answers
	//
	// 1. Decode the file through encoding.CSV with toCSV's schema, roundTripSchema with $table in front. [main.go:315-321]
	// 2. No batch is an empty read, answered as an empty batch, because a read of zero rows is a finding of the diff and not an error. [Rationale, "key"]
	// 3. More than one batch is an error that names the count, because the round trip writes one table per file. [main.go:331-333]
}
```

```go
// diff is the diff subcommand. It decodes the generated file and the
// read file, writes the report to stdout, and fails when the report is
// not empty, so a caller sees a difference in the exit status.
// [Rationale, "exit status"]
func diff(args []string) error {
	// style: doc comment + numbered overview + step comments
	// because: flag parsing, two decodes with an ordering dependency, and a report
	//
	// 1. Parse --table and the two paths, generated first.
	// 2. Decode the generated file first, because its schema types an empty read. [Rationale, "key"]
	// 3. Decode the read file.
	// 4. Write the report, then return an error when it is not empty.
}
```

`main.go` changes:

```go
// The file header gains the usage line
//	e2etool diff [--table NAME] GENERATED READ  the keyed diff of a read against the generated rows on stdout
// and names diff in its first paragraph. main's switch and usage() gain the case.
```

### `tests/e2e/tools/e2etool/diff_test.go` (new)

```go
// TestDiffRows draws a batch of the round trip schema, applies one
// change to a copy, and checks that diffRows reports that change and
// nothing else. [Rationale, "property test"]
func TestDiffRows(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: one property over five kinds of change, each with its own expected report
	//
	// 1. Draw 2 to 50 rows through table.GenerateRows on roundTripSchema, in the decoder's layout with no $table. [table.go:191]
	// 2. Draw one change: none, drop row i, duplicate row i in place, edit one cell of row i, swap rows i and j. [Rationale, "property test"]
	// 3. Build the changed batch from row ranges of the drawn one with array.Concatenate, so no cell is formatted. [main.go:107-120]
	// 4. An edit copies the cell of another row where the two differ by array.SliceEqual, and the draw is skipped when every row holds the same value. [compare.go:357]
	// 5. Check the report: none is empty, a drop is one missing timestamp, a duplicate is one extra row marked duplicate, an edit is one changed cell of that column, an adjacent swap is one break at j and any other swap is two breaks, at i+1 and at j. [Rationale, "order break"]
}
```

```go
// rowsOf concatenates the given row ranges of the batch, in order, into
// a new batch under its schema. The caller releases it.
func rowsOf(rec arrow.RecordBatch, ranges ...[2]int64) arrow.RecordBatch {
	// style: doc comment alone
	// because: one loop of slices and concatenations per column
}
```

### `tests/e2e/common.sh`, `compare_csv`

```bash
# compare_csv's comment keeps its contract and replaces the sentence on
# the first bytes:
# - On a mismatch the tool's keyed diff writes its report to <name>.diff
#   in the out directory, and the first lines go to stderr. [Rationale, "report file"]
# - The diff's exit status is ignored, because cmp decides the result
#   and the diff only explains it. [Rationale, "exit status"]
# - The diff reads the same two files cmp compared, so the reader's raw
#   CSV is decoded by the diff and not by tocsv. [common.sh:310-314]
compare_csv() {
    # ... unchanged decode and cmp ...
    #   "$tool" diff --table "$table" "$csv" "$body" > "$out/$name.diff" 2>&1 || true
    #   head -n "$DIFF_HEAD" "$out/$name.diff" >&2
    # DIFF_HEAD is a constant at the top of common.sh, 40 lines, not
    # measured, sized to show the counts and the first findings of each
    # kind without flooding the CI log. [Rationale, "report file"]
}
```

### `scripts/cicd/40.test-e2e.sh`

```bash
# The header's step 5 says that a failure dumps the logs and packs the
# failing run's files into one archive the test-report plugin uploads.
#
# 5a. Before the round trip, rm -f the archive, because a reused
#     checkout could otherwise upload an earlier build's archive after a
#     green run. [Rationale, "stale archive"]

# pack_e2e_failure_files packs the round trip's working files and both
# REST logs into ${BASE_DIR}/e2e-round-trip.tar.gz, the path _build.yml
# names. [Rationale, "tar.gz"]
pack_e2e_failure_files() {
    # style: doc comment + step comments
    # because: two guards keep a failed pack from hiding the round trip's failure
    #
    # 1. Collect only the paths that exist, because a failure before the
    #    servers start leaves no logs and no actual/ directory. [round-trip.sh:396, Makefile:136]
    # 2. tar -czf from tests/e2e. gzip is the coding, because every agent
    #    carries gunzip and none carries zstd. [docs/e2e.md, In Buildkite]
    # 3. A tar failure prints a line and returns 0, so the logs are still
    #    dumped and the step still exits 1. [Rationale, "tar.gz"]
}

# The failure branch calls dump_e2e_failure_logs, then
# pack_e2e_failure_files, then exits 1. The sentence "These files are
# not Buildkite artifacts" is replaced: the logs printed here are
# tails, and the archive carries the REST logs whole.
```

### `.buildkite/steps/_build.yml`

```yaml
# The test-report plugin's job block gains:
#   artifacts:
#     - name: "e2e round trip files"
#       input_path: "e2e-round-trip.tar.gz"
# with a one-line comment above it: 40.test-e2e.sh writes the archive
# only when the round trip fails, and the plugin only warns when it is
# missing. [Rationale, "tar.gz"]
```

### Document rows

`docs/e2e.md`, The tool, a new bullet after `tocsv`:

```
- `e2etool diff [--table NAME] GENERATED READ`: decodes both CSV files
  through the server's CSV decoder and the round trip schema, matches
  rows by `$timestamp`, and writes the row counts, the missing and extra
  timestamps, the changed cells and the breaks in timestamp order.
- It exits non-zero when it finds a difference.
- `compare_csv` runs it on a mismatch and keeps the report as
  `<name>.diff`.
- The cells in the report are arrow's text for each value. The report
  is read by a person and never compared.
```

`docs/e2e.md`, In Buildkite, appended to the paragraph:

```
- On a failure the step also packs `tests/e2e/actual/round-trip` and
  both REST logs into `e2e-round-trip.tar.gz`.
- The `bureau14/qdb-test-report` plugin uploads it to R2 through
  `job.artifacts`, as quasardb uploads its query server logs.
- A green run writes no archive, and the plugin only warns about the
  missing input.
```

`tests/e2e/AGENTS.md`, the round trip bullet, after the sentence on
`tocsv`:

```
- On a mismatch `compare_csv` keeps `tools/e2etool diff`'s keyed report
  as `<name>.diff` next to the body. The report prints cells through
  arrow's text, which is the one place the tool writes a cell that does
  not come from an encoder, because nothing compares the report.
```

`scripts/cicd/AGENTS.md`, Contract, the `40.test-e2e.sh` bullet:

```
- On a failed round trip it packs the round trip's files and both REST
  logs into `e2e-round-trip.tar.gz` at the repository root, and the
  test-report plugin in `.buildkite/steps/_build.yml` uploads it. The
  file name is spelled in both places, and a change to one changes the
  other.
```

## Rationale

| decision                                                                                                                | why                                                                                                                                  | rejected, and why                                                                                                                                                           | gained                                               | given up                                                                                    | settled by                                  |
| ----------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------- |
| key: rows are matched by `$timestamp` and compared after decoding                                                       | the generator draws `$timestamp` strictly ascending, so a key names one row, and decoding hides dialect differences between encoders | a line diff of the CSV text, because one missing row shifts every later line and the 4096-byte diff shows nothing after the cut                                             | every row of a failing run is classified             | a read whose `$timestamp` cells are themselves wrong shows as one missing and one extra row | `docs/log.md`, Next 1.1 (owner, 2026-10-29) |
| tar.gz: the failing run's files travel as one tar.gz, written only on failure, uploaded by the plugin's `job.artifacts` | a green build uploads nothing, and quasardb uses the same pattern                                                                    | uploading the directory as is, because every green build would upload about a thousand objects; an archive written by e2etool, because it is Go code for a stock tool's job | the files of a red run can be downloaded             | a dependency on `tar` on all eight agents, proven on the first red run                      | owner, 2026-10-29                           |
| logs: the archive carries `rest.log` and `rest-secure.log` whole                                                        | the step prints only the last 100 lines                                                                                              | the round trip's files alone                                                                                                                                                | every stream-cut and abandon line of the failing run | none                                                                                        | owner, 2026-10-29                           |
| property test: one rapid property test of `diffRows`                                                                    | the diff runs only after a failure, so without a test its first run would be during the investigation                                | no test                                                                                                                                                                     | a broken diff fails `go test` on every platform      | one test file                                                                               | owner, 2026-10-29                           |
| diff.go: the diff lives in a new file beside `main.go`                                                                  | the property test sits beside the code it tests                                                                                      | growing `main.go`, because the diff and its types are a unit of their own                                                                                                   | `main.go` stays the subcommands and the generator    | the tool is two files                                                                       | proposal                                    |
| pure function: `diffRows` takes two decoded batches, and `diff` does the files                                          | the property test draws batches and needs no files and no cluster                                                                    | a function that reads the files, because the test would then write CSV first                                                                                                | the test checks the comparison alone                 | none                                                                                        | proposal                                    |
| order break: a break is a timestamp below the previous row's                                                            | a duplicate is then reported once, as extra, and a swap as one or two breaks                                                         | a timestamp at or below the previous, because a duplicate would then be both extra and a break                                                                              | one finding per kind of change, which the test pins  | none                                                                                        | proposal                                    |
| duplicate: a repeated timestamp is an extra row marked duplicate                                                        | the Next item names duplicates as extra rows apart from unknown ones                                                                 | a separate list, because both are rows the read added                                                                                                                       | one list of added rows                               | none                                                                                        | `docs/log.md`, Next 1.1 (owner, 2026-10-29) |
| cell text: the report prints cells through arrow's text and timestamps as UTC RFC 3339 with nanoseconds                 | the report is read by a person and never compared, and arrow's text needs no code of the tool                                        | the CSV encoder per cell, because it writes a whole batch and quotes for a file; the tool's own formatting, because the tool formats no cell (`tests/e2e/AGENTS.md`)        | readable lines with no formatting code               | the rule that the tool writes no cell of its own gets one stated exception                  | proposal                                    |
| decoded equal: an empty diff says that the rows are equal after decoding                                                | `cmp` failed, so an empty diff means the bytes differ in dialect only, which is itself a finding                                     | printing nothing, because silence reads as a tool failure                                                                                                                   | the two kinds of failure are told apart              | none                                                                                        | proposal                                    |
| report file: the full report goes to `<name>.diff`, and its first 40 lines go to stderr                                 | a mismatch of the whole table would put tens of thousands of lines into the CI log, and the archive keeps the full report            | everything to stderr; the counts alone, because they do not say which rows                                                                                                  | a short CI log, and the full report in the archive   | the CI log shows the first findings only                                                    | proposal                                    |
| exit status: `diff` exits non-zero on a difference, and `compare_csv` ignores it                                        | `cmp` decides the comparison, and the diff only explains it, so a diff that fails to decode cannot turn a failure into a pass        | `compare_csv` deciding on the diff's status, because the comparison is byte for byte (ADR-0014)                                                                             | the tool is usable by hand as a test                 | none                                                                                        | proposal                                    |
| stale archive: the step removes the archive before the round trip                                                       | a reused checkout would otherwise upload an earlier build's archive after a green run                                                | relying on a clean checkout, because no document says the agents clean the workspace                                                                                        | the archive in R2 is always this build's             | none                                                                                        | proposal                                    |
| tolerant pack: the pack collects only existing paths and a tar failure does not end the step                            | the logs must still be dumped and the step must still fail with the round trip's status                                              | `set -e` aborting on a tar failure, because a missing `tar` would hide the dump                                                                                             | the failure report is never lost to the archive      | a missing `tar` shows only as a warning line                                                | proposal                                    |

## Knowledge

- Commit 2: `diff.go` doc comments and the overview of `diffRows`,
  `decodeRows` and `diff` carry Rationale rows key, duplicate, pure
  function, decoded equal, exit status and cell text. The `main.go`
  header gains the usage line. `docs/e2e.md`, The tool, gains the diff
  bullet, which carries cell text and exit status.
- Commit 3: `diffRows` steps 2 and 4 carry order break and cell text.
- Commit 4: `TestDiffRows` carries property test and order break.
  `diff.go`'s placement beside the test is stated in the `diff.go`
  file header, which carries diff.go.
- Commit 5: `compare_csv`'s comment carries report file and exit
  status. `tests/e2e/AGENTS.md` gains the `.diff` sentence and the
  cell text exception.
- Commit 6: `pack_e2e_failure_files` and the header of
  `40.test-e2e.sh` carry tar.gz, logs, stale archive and tolerant
  pack. `docs/e2e.md`, In Buildkite, and `scripts/cicd/AGENTS.md`
  gain their rows.
- Commit 7: the comment above the plugin's `artifacts` entry carries
  tar.gz.
- Facts with no home yet: none. The verified facts about the plugin
  land in `docs/e2e.md`, In Buildkite (commit 6).

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit: none.

## Commits

1. `docs(plan): diagnosable-round-trip-plan.md, a keyed csv diff in e2etool and the failing round trip's files as a ci artifact`
2. `feat(e2e): e2etool diff reports the row counts and the missing and extra timestamps of a read against the generated rows`
3. `feat(e2e): e2etool diff reports the changed cells and the breaks in timestamp order`
4. `test(e2e): a rapid property test pins that diffRows reports exactly the change made to a drawn batch`
5. `test(e2e): compare_csv keeps e2etool diff's report next to the body on a mismatch and prints its head`
6. `ci(cicd): 40.test-e2e.sh packs the round trip's files and both rest logs into one tar.gz when the round trip fails`
7. `ci(buildkite): the test report plugin uploads the round trip archive through job.artifacts`
8. `/doc-discipline all tests/e2e scripts/cicd .buildkite/steps docs/e2e.md`, committed as `docs(...)` fixes if it changes anything.
9. `/doc-discipline check docs/diagnosable-round-trip-plan.md`, with the findings fixed in further commits.
10. `docs(log): diagnosable-round-trip-plan.md deleted, facts moved to docs/e2e.md, tests/e2e/AGENTS.md, scripts/cicd/AGENTS.md and the comments of e2etool, common.sh, 40.test-e2e.sh and _build.yml`. The commit also rewrites Next in Current state: item 1 keeps only the row count, which waits on query capture.
11. Verify: push `sc-19567/rr-diagnosable-round-trip`, build its head in
    Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again.

## Open questions and recommendations

1. The log's Next 1.3 assumes an M7 query log line that does not exist.
   Recommendation: the deletion commit lists M7 query capture as Next 1,
   with the row count as its purpose for the investigation, and the
   investigation as Next 2.
2. The log's Next lists none of M7's other exit criteria (the per-plane
   series, the pool and breaker gauges, the stress's budget step).
   Recommendation: they follow the investigation in the order of the M7
   criteria, and the deletion commit lists them.
3. A green build never runs the pack, so `tar` on the Windows and
   FreeBSD agents stays unproven. Recommendation: the build stage pushes
   one commit that makes `compare_csv` fail, builds it, checks that the
   archive is in R2 on all eight platforms, then pushes its revert and
   builds again.
4. The table reader bypasses `query.Run` (`internal/httpapi/read.go:76`).
   Recommendation: the query capture unit decides whether "every query"
   includes table reads. This unit does not touch it.
