# Plan: roundtrip read range -- the round trip reads under the span of the rows it wrote

Status: draft

This plan answers the first Next item of `docs/log.md`, Current state:
why `TestRoundtrip` in `internal/httpapi` runs for minutes. It is
written for an executor who has none of the seed conversation of
2026-10-20. Everything the build needs is here.

## 1. Outcome

When the unit lands:

- Every read in `TestRoundtrip` (`internal/httpapi/roundtrip_test.go`),
  over HTTP and in the direct oracle, passes the time range of the rows
  the test wrote: the table's own span, from the fixture. No read of the
  round trip runs under no range.
- The table fixture (`internal/qdbtest/table`) answers that span through
  one accessor, `Range`, for a table with rows and for one without.
- `TestRoundtrip` runs in seconds on each cluster, as the rest of the
  suite does. The Buildkite test step is the check.
- `internal/httpapi/AGENTS.md` says the round trip bounds its reads and
  why, `internal/AGENTS.md` says a test that reads a generated table
  bounds the read, and the Next item leaves `docs/log.md`.

Left for later, by the owner's word only (owner, 2026-10-20):

- a story or a C++ reproduce for the QuasarDB R&D team of the cost
  this plan works around. Nothing in the tree records that it is
  pending.

## 2. Verified facts

1. `TestRoundtrip` alone takes minutes against the local clusters, both
   subtests alike, and the time is linear in the rapid iteration count:
   one check takes seconds, ten checks ten times that, and the default
   hundred ten times that again [session of 2026-10-20, `go test -run
'TestRoundtrip$'` with `RAPID_CHECKS`].
2. An execution trace of ten iterations attributes nearly all of the
   wall time to `qdb_bulk_reader_fetch`, called from the binding's
   `openBulkReader` under `Cluster.Read`, and nothing of note to the
   queries, the DuckDB scans, the creates, the pushes or the deletes
   [session of 2026-10-20, `go test -trace`, `go tool trace
-pprof=syscall`].
3. The cost is one case: a bulk reader opened on a table that has no
   rows, under no range or a wide range. Measured through `Cluster.Read`
   with an empty sink on a one-day shard, an empty table under no range
   costs the same as under a century, and the cost falls in proportion
   with the range: a decade is a tenth of it, a year a hundredth, two
   days under a thousandth. The same empty table with a one-year shard
   costs under a thousandth under no range. A table with rows costs
   under a thousandth under any range, its exact span included. So the
   cost is linear in the shards the range covers, and qdbd pays it only
   when the table has no rows [session of 2026-10-20, scratch test
   through `go test -overlay`, not in the tree].
4. The binding passes no range as a null pointer, which the C API reads
   as "forever", and a range as half-open `[start, end)`
   [`vendor/github.com/bureau14/qdb-api-go/v3/reader.go:337-343,434-448`].
5. `qdb.ReadOptions` carries `Start` and `End`, and a zero range is no
   range [`internal/qdb/read.go:27-41`].
6. The HTTP read parses `start` and `end` as RFC 3339 with nanoseconds,
   and an absent one is the zero time
   [`internal/httpapi/read.go:18-41`]. `TestReadTableRange` pins a
   covering range and a range before the rows
   [`internal/httpapi/read_test.go:41-60`].
7. The fixture's index starts at `indexStart`, 2020-01-01 UTC, with a
   drawn step of one nanosecond to one hour and zero to forty rows, so
   every generated table's rows lie inside two days from that start
   [`internal/qdbtest/table/table.go:70-71,136-146,179-181`]. A nil
   `Batch` is a table of no rows [`internal/qdbtest/table/table.go:48-58`].
   The batch's first column is `$timestamp` in the binding's Arrow
   timestamp type, nanoseconds UTC
   [`internal/qdbtest/table/table.go:148-153`].
8. The round trip's reads are `checkRead` (`roundtrip_test.go:98`,
   `Cluster.Read` under `qdb.ReadOptions{}`), `checkReadFormats`
   (`roundtrip_test.go:130-160`, `readTable` over HTTP with `""` or
   `columns=` as the parameters, against `directRead`), and `directRead`
   (`read_test.go:26-38`). Step 4 reads the first table empty, one
   `checkRead` and one `checkReadFormats` without a column subset, which
   is nine reads; step 6 reads every table filled, one `checkRead` and
   one `checkReadFormats` with a column subset, which is seventeen reads
   per table [`roundtrip_test.go:378-386,404-420`].
9. The round trip creates its tables over HTTP with a one-day shard
   [`internal/httpapi/tables_test.go:54-61`], and the fixture's `Create`
   does the same [`internal/qdbtest/table/table.go:476`].
10. No other test reads a generated table under no range in a loop:
    `qdb.ReadOptions{}` appears in no other test file, and the shell
    round trip reads each table once without a range, on tables that
    always have rows [`tests/e2e/round-trip.sh:216-226`].
11. `internal/httpapi/AGENTS.md:175-177` says the empty read runs on one
    table only because an empty read through the bulk reader costs
    about a tenth of a second. The number is a measured one, which
    `docs/AGENTS.md`, "Recorded nowhere", keeps out of documents, and the
    reason stops being true once the reads are bounded.
12. The lint stack is golangci-lint `standard` plus forbidigo and
    sloglint [`.golangci.yml:5-8`]; an exported fixture method used only
    from a later commit passes lint, an unexported one does not.

## 3. Design

### `internal/qdbtest/table/table.go`, after `Rows`

```go
// Range answers the half-open span of tbl's index as the reader takes a range:
// the first $timestamp to the last plus one nanosecond. [fact 4, fact 7]
// A table with no rows answers the one-nanosecond span at the start every index begins at,
// so a read of it is bounded too. [Rationale: the empty table's span]
func (tbl Table) Range() (start, end time.Time) {
	// style: doc comment + inline note at the empty branch
	// because: two branches and one arithmetic step, and the contract is the whole story
	//
	// at the nil-batch branch: no rows were written, so there is no span to read, and the
	//   one-nanosecond span keeps the read bounded without inventing rows [Rationale: the empty table's span]
	// at the filled branch: the index is strictly ascending, so the first and last
	//   values of the $timestamp column are the span [table.go:136-146]
	// at end: plus one nanosecond, because the binding's range is half-open [fact 4]
}
```

### `internal/httpapi/read_test.go`

```go
// rangeParams renders the range of tbl as the start and end URL parameters the read takes. [fact 6]
func rangeParams(tbl table.Table) string {
	// style: doc comment alone
	// because: one formatting step, RFC 3339 with nanoseconds as parseTime reads it [read.go:23]
}
```

`readTable` and `directRead` are unchanged: the callers pass the range
in the parameters and in `qdb.ReadOptions`.

### `internal/httpapi/roundtrip_test.go`

`checkRead` (line 98): the `qdb.ReadOptions{}` becomes the table's
range. Its doc comment gains one claim:

```go
// checkRead reads tbl over the cluster under the span of its rows and compares what comes back
// with the rows generated. [fact 8]
// The read is bounded because an unbounded read of a table with no rows probes every shard
// of a forever range. [Rationale: the bounded read]
```

`checkReadFormats` (lines 130-160): each case's parameters start from
`rangeParams(tbl)` and its `qdb.ReadOptions` carries the range; the
column case appends `columns=`. The doc comment gains the same claim as
`checkRead`, and the body keeps its shape (bare, a loop over cases and
encoders).

Step 4 of `TestRoundtrip` (lines 378-386): the step comment is
rewritten whole. Its claims:

```go
// 4. read the first table empty, which answers the schema alone in every format, and the schema
//    is the one generated [unchanged]
//    - the others share the schema, so one table stands for all [unchanged, minus the cost sentence]
//    - the read is bounded like every read here, and an empty table's span is one nanosecond
//      at the index start [Rationale: the bounded read, the empty table's span]
```

The sentence "an empty read through the bulk reader is slow (about a
tenth of a second where a filled read is milliseconds)" leaves: the
number is measured, and the cost is what the range removes.

Step 6 (lines 404-420): unchanged in code, because `checkRead` and
`checkReadFormats` carry the range. The step comment is unchanged.

### `internal/httpapi/AGENTS.md`, Tests, the round-trip bullet

The last two sentences of the bullet (lines 175-177) are replaced:

```
- Every read of the round trip, over HTTP and in the direct oracle, passes the span of the
  rows the test wrote, through the fixture's Range, because the bulk reader opened on a
  table with no rows under no range probes every shard of a forever range, and the round
  trip reads its first table empty in every format. [Rationale: the bounded read]
- The empty read runs on one table only, because the tables share one schema. [unchanged reason, minus the cost]
```

### `internal/AGENTS.md`, Tests, the fixture bullet (line 222 onward)

One sentence joins the bullet, after the sentence on `Check`:

```
- A test that reads a generated table passes the table's span (table.Range) as the read's
  range, because a bulk reader opened on a table with no rows under no range probes every
  shard of a forever range, and the owner chose the bound over a qdbd story. [Rationale: the bounded read]
```

### `docs/log.md`, Current state

The Next item on `TestRoundtrip` (lines 31-34) leaves. Nothing joins
the list (owner, 2026-10-20: the reproduce for R&D is not written down
anywhere). "Last updated" becomes the landing date.

## 4. Rationale

| decision                                                                            | why                                                                                                                                                                                 | rejected, and why                                                                                                                                                                                                                                                                                                                                                                           | gained                                                                    | given up                                                                               | settled by        |
| ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ----------------- |
| the bounded read: every read of the round trip passes the span of the rows it wrote | the test knows what it inserted, so it can say the range, and the cost is qdbd's probe of every shard of a forever range on a table with no rows (fact 3)                           | a story for R&D and no in-repo change, the standing rule for qdbd defects (`internal/AGENTS.md`, Tests): the owner chose the local bound first, with a reproduce for R&D possibly later; a larger shard in the round trip's create: it hides the cost instead of saying the range, and the shard is otherwise arbitrary; fewer empty reads in step 4: it changes what the round trip covers | the round trip runs in seconds, and the range endpoints run on every read | the unbounded read is exercised by `TestReadTableRange` and the shell round trip alone | owner, 2026-10-20 |
| the span comes from the fixture, `Table.Range`, computed from the rows              | the accessor cannot drift from the index draw, and the test writes no range of its own                                                                                              | one constant span in the test covering every generated table: it and the generator's limits agree by hand, and a change to the draw silently breaks the cover                                                                                                                                                                                                                               | one home for the span, next to the draw that defines it                   | one more fixture method                                                                | owner, 2026-10-20 |
| the empty table's span is the one-nanosecond span at `indexStart`                   | a read of a table with no rows has to be bounded too, and a span with no rows in it is any bounded span; the start every index begins at is the one point the fixture already names | a two-day constant: a second number next to the generator's limits; the zero range: unbounded, which is the cost                                                                                                                                                                                                                                                                            | an empty read that costs what a filled one costs (fact 3)                 | none                                                                                   | proposal          |
| only the Go round trip's reads are bounded                                          | it is the only test that reads generated tables under no range in a loop (fact 10)                                                                                                  | the shell round trip too: a tool and a script change for a sub-millisecond read per filled table                                                                                                                                                                                                                                                                                            | one file and one fixture method                                           | the shell round trip keeps one unbounded read per table                                | owner, 2026-10-20 |
| no number in any document or comment                                                | `docs/AGENTS.md`, "Recorded nowhere": nothing in the repository measures one (ADR-0015)                                                                                             | keeping "about a tenth of a second" in `internal/httpapi/AGENTS.md`: a measured number, and stale once the reads are bounded                                                                                                                                                                                                                                                                | documents that stay true                                                  | the magnitude lives only in this plan and dies with it                                 | `docs/AGENTS.md`  |
| nothing in the tree records the pending reproduce for R&D                           | the owner will come back to it and wants nothing written down                                                                                                                       | a conditional Next item like the qdb-duck one                                                                                                                                                                                                                                                                                                                                               | a Next list that holds only startable work                                | a fact that lives in the owner's head                                                  | owner, 2026-10-20 |

## 5. Knowledge

Commit 2, `Table.Range`:

- the doc comment: the half-open span, first to last plus one
  nanosecond (fact 4, fact 7), and the empty table's one-nanosecond
  span (Rationale: the empty table's span);
- the inline note at the nil-batch branch: the reason the empty span is
  what it is (Rationale: the empty table's span);
- the inline note at the end: plus one nanosecond because the binding's
  range is half-open (fact 4).

Commit 3, the round trip's reads:

- `rangeParams`'s doc comment: the format `parseTime` reads (fact 6);
- `checkRead` and `checkReadFormats` doc comments: the read is bounded
  because an unbounded read of a table with no rows probes every shard
  (Rationale: the bounded read);
- step 4's comment: one table stands for all because the schema is
  shared, and the bounded empty read (Rationale: the bounded read, the
  empty table's span).

Commit 4, `internal/httpapi/AGENTS.md`, Tests, the round-trip bullet:
the bounded reads and why, the one-table empty read and its surviving
reason (Rationale: the bounded read; the number row).

Commit 5, `internal/AGENTS.md`, Tests, the fixture bullet: a test that
reads a generated table passes its span, and the owner's choice of the
bound over a story (Rationale: the bounded read).

Commit 6, `docs/log.md`: the Next item leaves (Rationale: the
reproduce row). The Rationale rows on the fixture span and the shell
round trip need no home beyond the code: the accessor is the fixture's,
and the shell round trip is unchanged.

## 6. How the knowledge lands

The build stage is `/rr-build docs/roundtrip-read-range-plan.md`, which
writes every comment and document row of sections 3 and 5 in the same
commit as the code and reconciles the code against this plan before the
build-stage message.

Specific to this unit: commit 3 is where the fix is checked, by timing
`go test -run 'TestRoundtrip$' ./internal/httpapi/` once before and
once after against the local clusters, both clusters up. The result is
reported in the build-stage message as two durations, not written into
any document. If the bounded round trip is not in seconds, the executor
stops and reports; the plan's cause is then incomplete.

## 7. Commits

1. `docs(plan): roundtrip-read-range-plan.md, the round trip reads under the span of the rows it wrote` (this document)
2. `test(qdbtest): Table.Range answers the half-open span of its index`
3. `test(httpapi): the round trip's reads pass the table's range over HTTP and in the direct oracle`
4. `docs(httpapi): the Tests bullet says the round trip bounds its reads because an unbounded empty read probes every shard`
5. `docs(internal): the Tests rules say a test reading a generated table passes its span`
6. `docs(log): the TestRoundtrip item leaves the next list`
7. `/doc-discipline all docs/roundtrip-read-range-plan.md internal/qdbtest/table internal/httpapi internal/AGENTS.md internal/httpapi/AGENTS.md`, any repair as further small commits
8. Verify: push `sc-19567/rr-roundtrip-read-range`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

none
