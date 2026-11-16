# Flight SQL engine selection per connection

Thread: Flight SQL engine selection
Status: draft

## Outcome

A Flight SQL client can set the call header `engine` to `sql` or
`native`. Under `engine=sql`, query text that lacks the `#sql` prefix
runs on DuckDB, and text that has the prefix runs on DuckDB as before.
Under `engine=native`, or when the header is absent, the `#sql` prefix
alone picks the engine, as it does today. The server reads the header
when it describes or prepares a statement, and it writes `#sql ` in
front of the text that goes into the ticket or the prepared handle. So
DoGet, `query.Run` and the HTTP plane do not change. An unknown value
fails the call with INVALID_ARGUMENT.

Stock drivers set the header without code changes. The JDBC and ODBC
drivers forward a connection property they do not know as a call
header, and ADBC sets one through `adbc.flight.sql.rpc.call_header.`.
BI tools that connect through those drivers can then send DuckDB SQL
without the prefix (story 20181).

Left for later, each only if someone asks: the header on
`POST /api/v2/query`, a smoke step in the JDBC or ADBC driver that sends
the header, and a mention of the header in `GetSqlInfo`'s server name.

## Verified facts

- ADBC's Go Flight SQL driver sends every option under the prefix
  `adbc.flight.sql.rpc.call_header.` as a call header on every RPC
  (`vendor/github.com/apache/arrow-adbc/go/driver/flightsql/flightsql_driver.go:69`,
  `flightsql_database.go:271-272`).
- The JDBC and ODBC drivers forward an unknown connection property as a
  gRPC header. The source is the story's own description (owner,
  2026-11-16). Nothing in this tree checks it, because neither driver's
  source is vendored.
- The prefix rule is `split` in `internal/query/query.go:28-58`. It
  reads optional ASCII whitespace, `#sql` in any case, and one ASCII
  whitespace byte.
- The ticket that `GetFlightInfoStatement` answers is the query text
  verbatim (`internal/flightsql/statement.go:83-111`). DoGet runs the
  ticket's text through `query.Run` (`statement.go:146-236`).
- The prepared handle is the text verbatim
  (`internal/flightsql/prepared.go:14-29`).
  `GetFlightInfoPreparedStatement` passes the handle to
  `GetFlightInfoStatement` (`prepared.go:41-47`), so a handle that
  already carries `#sql ` reaches `GetFlightInfoStatement` prefixed.
- The server reads incoming metadata through
  `metadata.FromIncomingContext`, and gRPC lowercases the keys
  (`internal/flightsql/auth.go:24-41`).
- The test client adds outgoing metadata through
  `metadata.AppendToOutgoingContext` (`internal/qdbtest/flight/flight.go:45-48`).

## Design

### internal/query/query.go

`Prefixed` follows `split` and uses it.

```go
// Prefixed reports whether q starts with the #sql prefix, so Run and
// Describe send it to DuckDB. [query.go:28-58]
func Prefixed(q string) bool {
	// style: doc comment alone
	// because: it forwards to split, and the contract says everything
}
```

The package doc comment gains one claim:

```go
// - The Flight server writes the prefix itself when a connection picks
//   DuckDB through its engine header. [Rationale 3]
```

### internal/flightsql/engine.go (new)

```go
// engineKey is the call header a connection picks its engine with.
// [Rationale 1]
const engineKey = "engine"
```

```go
// withEngine answers the text the call's engine header asks for. It puts
// "#sql " in front of q when the header is sql and q has no prefix, and
// it answers q unchanged otherwise. An unknown value is
// INVALID_ARGUMENT. [Rationale 2, 3, 5]
func withEngine(ctx context.Context, q string) (string, error) {
	// style: doc comment + inline notes
	// because: two lines raise a "why": the first value wins, and the
	// value is case-insensitive
	//
	// at md.Get: the first value counts, as in credential [auth.go:36-37]
	// at the switch: the value is matched case-insensitively, because
	//   users type it in a connection URL [Rationale 6]
	// at the sql case: a prefixed text stays as it is, so a #sql query
	//   under engine=sql runs once, without a second prefix [Rationale 2]
}
```

### internal/flightsql/statement.go

`GetFlightInfoStatement` and `GetSchemaStatement` call `withEngine`
first and pass its text to `describe` or `schemaOf` and to the ticket.
In `GetFlightInfoStatement`, the comment block "The text travels
verbatim and DoGet dispatches it" is rewritten whole:

```go
	// The ticket carries the text with the engine already in it, so DoGet
	// reads no header and any instance serves the ticket. [Rationale 3]
```

The doc comment of `GetFlightInfoStatement` changes one claim, "whose
ticket is the query text" becomes "whose ticket is the query text, with
the prefix the engine header asks for".

### internal/flightsql/prepared.go

`CreatePreparedStatement` and `GetSchemaPreparedStatement` call
`withEngine` first. The handle comment is rewritten whole:

```go
	// The handle is the text with the engine already in it, so any
	// instance behind a load balancer serves the later calls. [Rationale 3]
```

`GetFlightInfoPreparedStatement` stays as it is. Its handle passes
through `withEngine` again inside `GetFlightInfoStatement`, which is a
no-op on a prefixed handle.

### internal/flightsql/flightsql_test.go

`TestStatement` gains one step, after step 6, and its overview gains
the step's line:

```go
	//  7. engine=sql: the same SQL without the prefix, through Execute and
	//     through Prepare, compared with Cluster.Duck's batches
	// at step 7: the header goes on the context the client sends,
	//   as a driver's connection property would [Verified facts]
```

`TestStatementRows` gains one row in step 1:

```go
		{"unknown engine", statementErr(t, cl, engine(s.ctx, "duckdb"), "SELECT 1")},
```

and its step 1 comment gains "an unknown engine value is
INVALID_ARGUMENT". The helper is small:

```go
// engine adds the engine header to the context the client sends.
func engine(ctx context.Context, v string) context.Context {
	// style: doc comment alone
	// because: one metadata call, like qdbflight.Bearer
}
```

### internal/flightsql/AGENTS.md, Statement

```
- A connection picks its engine with the `engine` call header, `sql` or
  `native`, matched case-insensitively. `withEngine` writes `#sql ` in
  front of an unprefixed text under `sql`, before the describe, the
  ticket and the prepared handle, so DoGet reads no header (ADR-0018,
  Decision 3). An unknown value is INVALID_ARGUMENT.
```

The bullet "GetFlightInfo for a statement ... whose ticket is the query
text verbatim" is rewritten to say the ticket is the text after
`withEngine`.

### docs/adr/0018-flight-sql.md

Decision 3 gains a paragraph, edited in place as `docs/AGENTS.md`, ADRs,
allows for an addition within the subject:

```
- On the Flight port, a connection can set the call header `engine` to
  `sql` or `native`. Under `sql`, a query without the prefix runs on
  DuckDB, and a query with it runs as before. Under `native`, or with no
  header, the prefix alone decides. The server writes the prefix into the
  ticket and the prepared handle, so Decision 9 holds. An unknown value
  is INVALID_ARGUMENT. The HTTP plane reads no header.
- The reason: BI tools generate SQL through the stock JDBC and ODBC
  drivers, which cannot add a prefix and forward a connection property
  as a header (owner, 2026-11-16).
```

The Alternatives row "A gRPC metadata header choosing the engine" is
removed, and a row is added:

```
| A header that overrides the prefix | a `#sql` query pasted under `engine=sql` would reach DuckDB with the prefix and fail (owner, 2026-11-16) |
| Reading the header again on DoGet  | a ticket would no longer say alone what it runs (owner, 2026-11-16)                                    |
```

### docs/brief.md, "One query string, two engines"

One sentence joins the paragraph:

```
- On the Flight port, a connection's `engine` header can make DuckDB the
  default for text without the prefix (ADR-0018, Decision 3).
```

## Rationale

| decision                                                        | why                                                                                          | rejected, and why                                                                                                 | gained                                              | given up                                                        | settled by        |
| --------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- | --------------------------------------------------------------- | ----------------- |
| 1. the header is `engine`, values `sql` and `native`            | users type it in a connection URL, and `sql` reads as the `#sql` prefix                      | `qdb-engine`: longer to type. `duckdb` as the value: does not match the prefix                                    | a short property that BI users can type             | a generic name that another forwarded property could clash with | owner, 2026-11-16 |
| 2. the header sets the default, and `#sql` is a no-op under sql | queries that work today keep working                                                         | the header overrides the prefix: a pasted `#sql` query fails. A `#native` prefix: a second prefix in the contract | one rule that every existing client keeps           | under `engine=sql` the text cannot reach the native engine      | owner, 2026-11-16 |
| 3. the engine travels in the ticket and the handle as `#sql `   | the ticket says alone what it runs, so any instance serves it (ADR-0018, Decision 9)         | reading the header again at DoGet: a client that forwards a ticket without its headers runs the wrong engine      | DoGet, `query.Run` and the HTTP plane do not change | the ticket text differs from what the client sent               | owner, 2026-11-16 |
| 4. Flight SQL only, and few tests                               | only the BI drivers lack a way to add the prefix. "don't overdo it with the tests"           | the header on `POST /api/v2/query`: no HTTP client asks for it                                                    | the smallest change                                 | the two planes differ in one input                              | owner, 2026-11-16 |
| 5. an unknown value is INVALID_ARGUMENT                         | a typo would otherwise run the query on the native engine with no sign the setting was wrong | ignoring an unknown value: the failure shows up later as a native parse error                                     | a wrong setting fails at once, naming the header    | none                                                            | proposal          |
| 6. the value is matched case-insensitively                      | users type it, and the prefix itself matches in any case                                     | exact match: `engine=SQL` would fail                                                                              | the same rule as the prefix                         | none                                                            | proposal          |
| 7. the rewrite lives in `internal/flightsql`, `query` unchanged | the ticket must carry the engine anyway, so rewriting the text is enough                     | an engine argument on `query.Run` and `query.Describe`: both planes change for a Flight-only input                | the dispatcher keeps one input, the text            | `query` exports `Prefixed` for the no-op check                  | proposal          |
| 8. ADR-0018 is edited in place                                  | the prefix rule stays, and the header is an addition within the subject (`docs/AGENTS.md`)   | a new ADR superseding 0018: the decision does not change, it gains an input                                       | one ADR for the engine rule                         | none                                                            | proposal          |

## Knowledge

Commit 2 (`query`): `Prefixed`'s doc comment. The package doc comment
claim lands with commit 3, because it names the Flight behaviour.

Commit 3 (`flightsql`): `engineKey`, `withEngine` and its three inline
notes (Rationale 2, 6), the rewritten comment blocks in
`GetFlightInfoStatement` and `CreatePreparedStatement` (Rationale 3),
and the package doc comment claim in `internal/query/query.go`.

Commit 4 (tests): step 7 of `TestStatement`, the row of
`TestStatementRows`, and `engine`.

Commit 5 (ADR-0018): Rationale 1, 2, 3, 4 and 5 land in Decision 3 and
the Alternatives table.

Commit 6 (brief): the sentence in "One query string, two engines".

Commit 7 (`internal/flightsql/AGENTS.md`): the Statement bullet and the
rewritten ticket bullet.

Plan only: Rationale 7 and 8. No reader of the code would propose an
engine argument on `query.Run` once the ticket carries the prefix, and
the edit in place follows `docs/AGENTS.md`.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message.

ADR-0018 must carry the header (commit 5) before `internal/flightsql/AGENTS.md`
cites it as Decision 3 (commit 7).

## Commits

1. `docs(plan): flight-sql-engine-plan.md, a per-connection engine header on the flight plane`
2. `feat(query): Prefixed reports whether a text carries the #sql prefix`
3. `feat(flightsql): the engine header puts #sql in front of unprefixed text`
4. `test(flightsql): engine=sql runs unprefixed text on duckdb, and an unknown value is refused`
5. `docs(adr): ADR-0018 adds the engine header beside the #sql prefix`
6. `docs(brief): one query string, two engines names the engine header`
7. `docs(flightsql): the agents file describes the engine header`
8. `/doc-discipline all internal/query internal/flightsql docs/flight-sql-engine-plan.md`,
   with a commit only if it changes something:
   `docs(flightsql): comments follow the doc discipline`
9. `docs(log): flight-sql-engine-plan.md deleted, facts moved to ADR-0018, docs/brief.md and internal/flightsql/AGENTS.md`,
   with a dated entry for the owner's decision that a header picks the
   engine on the Flight port.
10. Verify: push `sc-20181/flight-sql-engine-selection`, build its head
    in Buildkite, wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, build
    again.

## Open questions and recommendations

1. A smoke step in the JDBC or ADBC driver that sends the header.
   Recommendation: none, because of the owner's note on tests. The Go
   test sends the header the way ADBC does.
2. The stack on PR #63. Recommendation: the PR targets
   `sc-20143/rest-version-registration`. After #63 is squash-merged,
   rebase with `git rebase --onto master sc-20143/rest-version-registration`
   and retarget the PR to `master`.
3. Unknown values and case (Rationale 5 and 6). Recommendation:
   INVALID_ARGUMENT, matched case-insensitively.
