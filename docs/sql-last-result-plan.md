# A `#sql` text of several statements answers the last result, and M5 closes

Status: draft

## Outcome

When this unit lands, the documents and the tests say what the server does
with a `#sql` text of several statements, and M5 is closed.

- ADR-0018, Decision 12, says that on both planes a `#sql` text of several
  statements runs every statement and answers the last statement's result,
  that the describe takes one statement, and why the two differ. Its
  rejected-alternatives table names the second prepare that lost.
- `internal/olap/AGENTS.md`, `internal/httpapi/AGENTS.md` and
  `internal/flightsql/AGENTS.md` carry the sentence for their component,
  and `Instance.Query`'s doc comment says what the run answers.
- One HTTP test posts two `#sql` statements to `POST /api/v2/query` and
  reads the second's result. One Flight test streams a hand-built ticket
  of two `#sql` statements and reads the second's result, beside the
  rows that pin the describe's refusal.
- `docs/log.md` marks M5 done, strikes the three Next items and carries
  no criteria for a milestone in flight.

No engine code changes. Left for later units:

- M6 Resilience. Its entry criterion, M5 closed, is met by this unit. Its
  exit criteria are written when the owner starts it.

## Verified facts

- duckdb-go's Arrow query, `Arrow.QueryContext`, calls `Conn.QueryContext`,
  which calls `prepareStmts`: it splits the text with DuckDB's extract
  call, prepares and executes every statement but the last with no
  arguments, discarding each result, and prepares the last one, whose
  rows become the Arrow reader
  (`vendor/github.com/duckdb/duckdb-go/v2/arrow.go:96-110`,
  `connection.go:81-109`, `connection.go:215-254`). Its doc comment says
  it answers "the result of the last executed statement"
  (`arrow.go:96-97`).
- `Instance.Query` hands the text to that call and streams the reader
  (`internal/olap/instance.go:190-224`), so on `POST /api/v2/query` and on
  a DoGet ticket a `#sql` text of several statements runs every statement
  and answers the last statement's result today.
- `driver.Conn.Prepare` splits the text the same way and fails when the
  count is not one, with `errMissingPrepareContext` joined to `errPrepare`
  for several statements and `errEmptyQuery` for none, neither a
  `*duckdb.Error` (`connection.go:122-136`, `errors.go:131-132`).
  `Instance.Describe` maps that failure to `ErrNotOneStatement`
  (`internal/olap/describe.go:312-325`).
- `Conn.PrepareContext` is the other prepare, and it executes every
  statement before the last to reach it (`connection.go:215-254`, the
  loop `prepareStmts` runs), so a describe cannot use it.
- duckdb-go exports no Arrow query over a prepared statement:
  `Arrow.QueryContext` takes text, and `recordReaderFromRows`, which turns
  driver rows into the Arrow reader, is unexported (`arrow.go:98`,
  `arrow.go:170`). A prepare that proves the count cannot also be the
  run.
- The Flight `GetFlightInfoStatement`, `GetSchemaStatement` and the
  prepared-statement calls describe first, so a `#sql` text of several
  statements fails there with INVALID_ARGUMENT, pinned by
  `TestStatementRows` (`internal/flightsql/flightsql_test.go:416-417`).
  `DoGet` runs the ticket's text through `query.Run` with no describe
  (`internal/flightsql/statement.go:155`), and the test's hand-built
  ticket row shows a ticket of any text reaches it
  (`flightsql_test.go:409-414`).
- The HTTP route runs the body through `query.Run` and answers 400 for
  an engine error, 200 with the stream otherwise
  (`internal/httpapi/query.go:109-163`).
- The HTTP test fixture posts a body with `s.post` and checks a JSON
  result's columns by type word and cell (`TestDuckDBScalars`,
  `internal/httpapi/duckdb_test.go:21-83`). A bare integer literal is
  int32 there.
- The Flight test fixture builds a statement ticket with
  `flightsql.CreateStatementQueryTicket` and reads a ticket whole with
  `qdbflight.ReadAll`, which answers the schema, the batches and the
  error (`flightsql_test.go:400-403`, `internal/qdbtest/flight/flight.go:68`).
- Build 175 of `qdb-api-rest` on commit c259b83, the base head, passed
  on 2026-10-25 with all eleven script jobs passed (Buildkite API,
  `pipelines/qdb-api-rest/builds/175`). The M5 exit criteria are met by
  the tree and that build.
- ADR-0018, Decision 12, says a `#sql` text is one statement because the
  owner expected one statement per invocation and could not say what a
  batch's response would be (`docs/adr/0018-flight-sql.md:106-110`), and
  the rejected table carries "Running a multi-statement `#sql` text with
  no schema" (`:175`).
- ADRs are edited in place when a decision within their subject is added
  or a detail proves wrong, with no log entry (`docs/AGENTS.md`, ADRs;
  owner, 2026-10-14 and 2026-10-19).

## Design

### `docs/adr/0018-flight-sql.md`

Decision 12 is rewritten in place; the decision's subject, prepared
statements without parameters, is unchanged.

```
12. **Prepared statements without parameters.**
- the handle, the close and the FlightInfo sentences stay as they are
- a `#sql` text may hold several statements; on both planes every
  statement runs and the response is the last statement's result, the
  convention of DuckDB's C API that duckdb-go's Arrow query follows
  [arrow.go:96-110, connection.go:215-254]
- the response format carries one result and the engine answers one,
  which is the owner's reason the rule stands (owner, 2026-10-26)
  [Rationale row 1]
- the describe takes one statement, because the only prepare that runs
  nothing refuses several, and PrepareContext would execute the
  earlier ones [connection.go:122-136, :215-254]
- so a text of several statements fails GetFlightInfo, GetSchema and
  Prepare with INVALID_ARGUMENT, and a DoGet ticket of it streams the
  last result; the divergence is accepted (owner, 2026-10-26)
  [Rationale row 2]
- the parameter-marker sentence and the UNIMPLEMENTED list stay
```

The rejected-alternatives row "Running a multi-statement `#sql` text with
no schema" is replaced:

```
| A second prepare in `Instance.Query` that refuses a `#sql` text of
  several statements on both planes | every `#sql` query would parse and
  bind twice, for an edge case, and the response carries one result
  either way (owner, 2026-10-26) |
```

### `internal/olap/instance.go`

```go
// Query runs a DuckDB query on a connection of its own and hands the result
// to the sink as lent batches. [existing contract claims stay]
// A text of several statements runs every statement and answers the last
// statement's result, as DuckDB's C API does [arrow.go:96-110]
func (i *Instance) Query(ctx context.Context, q string, sink func(iter.Seq2[arrow.RecordBatch, error]) error) error {
	// style: doc comment + numbered overview + step comments, unchanged
	// because: the body is not touched; the new claim is a contract claim
	//
	// at step 2, one added claim: the binding runs the earlier statements
	// inside this call and discards their results, so their errors
	// arrive here too [connection.go:215-254]
}
```

### `internal/olap/AGENTS.md`

Under the describe bullet (line 31):

```
- A query of several statements runs every statement and answers the
  last statement's result, the convention of DuckDB's C API that
  duckdb-go's Arrow query follows; a describe takes one statement,
  because the binding's prepare that runs nothing refuses several
  (ADR-0018, Decision 12). `Instance.Query` and `Instance.Describe`
  carry the two claims.
```

### `internal/httpapi/duckdb_test.go`

```go
// TestDuckDBLastResult: on each cluster, a #sql body of two statements
// answers 200 with the second statement's result alone, so the response
// carries one result as ADR-0018, Decision 12, says [Rationale row 1].
func TestDuckDBLastResult(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: three steps whose order tells the story: post, status, body
	//
	// 1. post "#sql SELECT 1 AS a; SELECT 2 AS b" as the fixture's user
	//    [duckdb_test.go:46, s.post]
	// 2. the answer is 200 in JSON, because every statement ran and the
	//    engine answered one result [query.go:139-163]
	// 3. the body is one result of one column, b, int32, cell 2, and no
	//    column a, because the first statement's result was discarded
	//    [connection.go:240-252]
}
```

### `internal/httpapi/AGENTS.md`

In the `#sql` body bullet (line 89), after "DuckDB's parse, bind and
execution errors arrive before the sink and are 400.":

```
A body of several statements runs every statement and answers the last
statement's result (ADR-0018, Decision 12); `TestDuckDBLastResult` pins
it.
```

Under Tests, beside the `TestDuckDBScalars` sentence:

```
`TestDuckDBLastResult` posts two statements and reads the second's
result.
```

### `internal/flightsql/flightsql_test.go`

A step inside `TestStatementRows`, after the error rows and before the
cluster rows, or a test of its own if the rows' loop shape does not fit;
the executor picks and says which in the commit.

```go
// 1b. a hand-built ticket of two #sql statements streams the second's
//     result, because DoGet runs the ticket's text with no describe
//     [statement.go:155] and the engine answers the last statement
//     [connection.go:215-254]; the describe rows above refuse the same
//     text, which is the divergence ADR-0018, Decision 12, accepts
//     [Rationale row 2]
//     - build the ticket with flightsql.CreateStatementQueryTicket over
//       "#sql SELECT 1 AS a; SELECT 2 AS b" [flightsql_test.go:400]
//     - ReadAll answers no error, a schema with one field b, and batches
//       whose one row holds 2 [flight.go:68]
```

### `internal/flightsql/AGENTS.md`

In the Statement section, the DoGet bullet (line 70), one sentence:

```
A ticket of several `#sql` statements streams the last statement's
result, because the run does not describe (ADR-0018, Decision 12).
```

In the Prepared section, the "No parameters" bullet (line 97) gains:

```
The describe takes one statement, because the binding's prepare that
runs nothing refuses several, so a `#sql` text of several statements
fails Execute and Prepare and streams through a hand-built ticket; the
rows of `TestStatementRows` pin both.
```

### `docs/log.md`

Current state, in the deletion commit:

```
- Last updated: 2026-10-26
- M5 row: done, "closed 2026-10-26"
- In flight: "Nothing."
- Next: the three items removed; the block says "Nothing." or is
  replaced by the M6 line the owner gives, if any
- the "M5 criteria" paragraph removed, because criteria live in the
  block only for the active milestone [docs/AGENTS.md, Current state]
- Handoff to M6 and M7: unchanged
- one entry, 2026-10-26: M5 closed; sql-last-result-plan.md deleted,
  facts moved to ADR-0018 (Decision 12) and the AGENTS.md files of
  internal/olap, internal/httpapi and internal/flightsql
```

## Rationale

| decision                                                                                                                                  | why                                                                                                                                                                     | rejected, and why                                                                                                                                                                                                                      | gained                                     | given up                                                                                      | settled by                                                    |
| ----------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------ | --------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| A `#sql` text of several statements runs every statement and answers the last statement's result, on the HTTP route and on a DoGet ticket | the objection to a batch was that the response format carries one result; duckdb-go already answers one, DuckDB's convention, so the format is never asked to carry two | a second prepare in `Instance.Query` refusing several statements on both planes: every `#sql` query would parse and bind twice, a bind on a qdb-attached table may read the cluster's catalog, for an edge case users learn not to hit | no engine change, no cost per query        | the earlier statements run with their results discarded, so a client sees nothing of them     | owner, 2026-10-26                                             |
| The describe keeps refusing a text of several statements                                                                                  | `driver.Conn.Prepare` is the only prepare that runs nothing and it refuses several; `PrepareContext` executes the earlier ones                                          | describing through `PrepareContext`: a describe would execute statements                                                                                                                                                               | the Flight describe stays side-effect free | the two planes differ on this edge: Execute and Prepare refuse what a raw ticket and HTTP run | tree (`connection.go:122-136`, `:215-254`); owner, 2026-10-26 |
| The rule is recorded by editing ADR-0018 Decision 12 and one rejected row in place                                                        | the decision's subject is unchanged and the ADR's stated reason proved narrower than the owner's                                                                        | a new ADR superseding 0018: the decision did not change, a detail of its reasoning did                                                                                                                                                 | one ADR for Flight SQL                     | none                                                                                          | `docs/AGENTS.md`, ADRs (owner, 2026-10-14 and 2026-10-19)     |
| The HTTP pin is a test of its own in `duckdb_test.go`, not an error-table row                                                             | it is a 200 with a body to read, and the error table holds failures                                                                                                     | a row in `TestErrorRows` with status 200: the table reads no body                                                                                                                                                                      | the body's columns are checked             | one more test function                                                                        | proposal                                                      |
| The log commit closes M5 and strikes the three Next items, M6 stays not started                                                           | build 175 on the base head passed every job; the qdb-duck story item is dropped on the owner's word                                                                     | closing M5 in a commit of its own on the base: no stage commits to the base                                                                                                                                                            | the log reads current                      | nothing                                                                                       | owner, 2026-10-26                                             |

## Knowledge

- Commit 2: ADR-0018, Decision 12 and the rejected row (Rationale rows
  1, 2 and the second prepare).
- Commit 3: `Instance.Query` doc comment and its step 2 claim;
  `internal/olap/AGENTS.md` (Rationale rows 1 and 2, at the engine).
- Commit 4: `TestDuckDBLastResult` doc comment and steps;
  `internal/httpapi/AGENTS.md` Handlers and Tests (Rationale rows 1, 4).
- Commit 5: `TestStatementRows` step 1b; `internal/flightsql/AGENTS.md`
  Statement and Prepared (Rationale row 2).
- Commit 6: `docs/log.md` Current state and one entry (Rationale row 5).
  Rationale row 3 lands nowhere but the ADR edit itself, which is its
  home by `docs/AGENTS.md`.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the
code and reconciles the code against this plan before the build-stage
message. Specific to this unit: commit 4 and commit 5 assert the body
of a two-statement run, so if either answers something other than one
column `b` holding 2, the executor stops and asks the owner before
changing a claim, because the ADR sentence of commit 2 rests on it.

## Commits

1. `docs(plan): sql-last-result-plan.md, a #sql text of several statements answers the last result on both planes, and M5 closes`
2. `docs(adr): ADR-0018 Decision 12 says a #sql text runs every statement and answers the last result, and why the describe takes one`
3. `docs(olap): Instance.Query and AGENTS.md say a text of several statements runs them all and answers the last`
4. `test(httpapi): two #sql statements on the query route answer the second's result, and AGENTS.md says so`
5. `test(flightsql): a hand-built ticket of two #sql statements streams the last result, and AGENTS.md says why the describe refuses it`
6. `/doc-discipline all docs/adr/0018-flight-sql.md internal/olap internal/httpapi internal/flightsql docs/sql-last-result-plan.md`, then `/doc-discipline all check` on the same paths; fixes land as further small commits.
7. `docs(log): sql-last-result-plan.md deleted, M5 closed, the three Next items struck`
8. Verify: push `sc-19567/rr-sql-last-result`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Whether the Next block of the log carries an M6 line once M5 closes. Recommendation: "Nothing." until the owner starts M6 with its criteria.
