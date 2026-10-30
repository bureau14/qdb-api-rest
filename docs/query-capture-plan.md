# Query capture -- plan

Status: draft

## Outcome

When this unit lands, every query on both planes writes one `info` line
with the message `query`. The line carries the engine, the plane, the
duration, the row count and the outcome. A `debug` line with the
message `query statement` precedes it and carries the statement text,
cut at `log.statement_max_bytes`, where 0 keeps the text whole. A table
read through `GET /api/v2/tables/{name}/rows` writes the same `info`
line with engine `bulk reader`, the table name and the time range, and
writes no statement line. Each plane's edge tags its logger with the
plane, so every line of a request names its plane, the access line
included. The e2e round trip greps each server's log for the line, once
per engine and plane the round trip drives.

The unit leaves these for later:

- The Goal 9 series on both planes, with the pool, breaker and retry
  state (`docs/log.md`, Next 2). They are the next unit, and they can
  reuse the engine and plane words this unit fixes.
- The stress's budget step (`docs/log.md`, Next 2).
- A line for `query.Describe`, which runs nothing (Rationale, row 8).

## Verified facts

- `query.Run` is the one function through which both planes run a query
  string. The HTTP query route calls it (`internal/httpapi/query.go`,
  `handleQuery`), and the Flight server's `DoGet` calls it
  (`internal/flightsql/statement.go:155`). Checked 2026-10-30.
- `query.Run` returns the sink's error unchanged, and every other error
  arrives before the sink runs (`internal/query/query.go:55-60`).
- The table reader calls `Cluster.Read` directly and not `query.Run`
  (`internal/httpapi/read.go:76`).
- The HTTP edge `withRequestLogging` tags the ctx with the request id
  alone, and its doc comment says so (`internal/httpapi/middleware.go:69-72`).
  `requireBearer` runs inside it and adds the user and session
  (`internal/httpapi/AGENTS.md`, Middleware).
- The Flight edge `accessContext` tags the call ctx with a minted
  request id (`internal/flightsql/logging.go:16-21`). It runs outside
  the auth interceptor (`internal/flightsql/AGENTS.md`, Server).
- Both access lines name the duration `duration_ms` in milliseconds
  (`internal/httpapi/middleware.go:80-83`,
  `internal/flightsql/logging.go:28-30`).
- `observe.WithAttrs(ctx, attrs ...any)` returns a ctx whose logger
  carries the attributes (`internal/observe/observe.go:104-106`).
- `observe.NewLogger(cfg config.Log, w)` builds the process logger and
  has one caller, `cmd/qdb_rest/main.go:255`
  (`internal/observe/observe.go:45-56`). It builds `slog.HandlerOptions`
  with the level alone today.
- `config.Log` has `Level` and `Format` (`internal/config/config.go:57-60`).
  `Default` sets them to info and json (`:220`). `validate` checks them
  with `oneOf` (`:746-751`). The flag and the environment variable of a
  key derive from its path (`:285`, `flagName`).
- `TestExampleIsTheDefaults` (`internal/config/config_test.go:271`) pins
  `examples/qdb_rest.yaml` to `Default`. That forces the key and its
  example into one commit.
- The round trip already greps each server's log with a fixed string
  (`tests/e2e/round-trip.sh:280-306`, `check_log`), and `main` runs that
  grep after each cluster's `run_cluster` (`:401`, `:405`).
- The e2e servers log JSON at info, because the Makefile passes no log
  flag (`tests/e2e/Makefile:39`, `REST_ARGS`) and `Default` is info and
  json.
- The round trip drives the native engine over HTTP (step 5), DuckDB
  over HTTP (step 6), the bulk reader over HTTP (step 5's read) and both
  engines over Flight through ADBC and JDBC (step 7). JDBC does not run
  on FreeBSD (`docs/e2e.md`, The v2 round trip;
  `tests/e2e/round-trip.sh:271`).
- `internal/query` can import `internal/qdbtest/cluster` and
  `internal/qdbtest/table` without a cycle, because neither imports
  `internal/query`. `httpapi` imports both fixtures already
  (`internal/httpapi/read_test.go:53-57`).
- A DuckDB crash is a crash of the binary (`docs/brief.md`, Embedded
  DuckDB, Isolation is logical).

## Design

### internal/observe/observe.go

New keys go into the existing block of shared attribute keys, after
`KeySession`.

```go
const (
	// ... KeyError, KeyRequestID, KeyUser, KeySession as today
	// KeyPlane names the listener a request came in on, http or flight.
	KeyPlane = "plane"
	// KeyEngine names what ran a query: native, duckdb or bulk reader.
	KeyEngine = "engine"
	KeyRows = "rows"
	// KeyOutcome is ok, error, cut or abandoned.
	KeyOutcome = "outcome"
	// KeyStatement is a query's text, cut by the logger at
	// log.statement_max_bytes.
	KeyStatement = "statement"
	// KeyStatementBytes is the length of the whole statement, before the cut.
	KeyStatementBytes = "statement_bytes"
	KeyTable = "table"
	KeyStart = "start"
	KeyEnd = "end"
)
```

`cutStatement` comes before `NewLogger`, which calls it.

```go
// cutStatement keeps at most max bytes of a statement and marks a cut
// with a trailing "...". A max of 0 keeps the statement whole. [Rationale 4, 5]
func cutStatement(s string, max int) string {
	// style: doc comment + inline note at the rune back-off
	// because: one comparison and one loop, where only the back-off needs a reason
	//
	// at the back-off: the cut moves back to the start of a UTF-8 rune, so a
	//   multi-byte character is never split into an invalid byte sequence that
	//   the JSON handler would print as a replacement character [Rationale 5]
	// at the back-off: the loop runs at most three times, because a UTF-8
	//   rune is at most four bytes [utf8.UTFMax]
}
```

```go
// NewLogger builds the process logger. The caller places it in the root
// context with WithLogger, and nothing installs it as a global. [today]
// The logger cuts every top-level KeyStatement attribute at
// cfg.StatementMaxBytes. [Rationale 5]
func NewLogger(cfg config.Log, w io.Writer) (*slog.Logger, error) {
	// style: doc comment alone, + inline note at ReplaceAttr
	// because: it assembles a handler, and only the cut needs a reason
	//
	// at ReplaceAttr: the handler cuts the statement, so that no call site
	//   carries the length and nothing new travels in the ctx [Rationale 6]
	// at ReplaceAttr: only a top-level string attribute under KeyStatement
	//   is cut, because no caller groups it [query.Run, this plan]
}
```

### internal/config/config.go

```go
// Log selects the log output shape and how much of a query's statement
// a debug line keeps. [Rationale 4]
type Log struct {
	Level  string `yaml:"level" help:"debug | info | warn | error"`
	Format string `yaml:"format" help:"json | console"`
	StatementMaxBytes int `yaml:"statement_max_bytes" help:"bytes of a query's statement a debug line keeps; 0 keeps it whole"`
}
```

`Default` sets `StatementMaxBytes: 1024`. Its doc list gains "a query's
statement cut at a kibibyte on debug lines". A comment at the value
says that the 1024 is not measured and that a statement of that size
is several lines of SQL. [Rationale 5]

`validate` refuses a negative value right after the `log.format` check:
`log.statement_max_bytes must not be negative, got %d`. That is the
wording the other non-negative bounds use (`:647`). [internal/AGENTS.md,
Code, validation has one home]

### examples/qdb_rest.yaml

The `log:` block gains the key at its default, commented in the file's
register:

```yaml
# Bytes of a query's statement that a debug line keeps. Longer statements
# are cut and end in "...". 0 keeps every statement whole.
statement_max_bytes: 1024
```

### internal/httpapi/middleware.go

```go
// withRequestLogging puts the request id on the request context and on
// the response, tags the context with the plane, and logs one access line
// when the handler returns. Log lines join on the id. [today, Rationale 2]
func withRequestLogging(next http.Handler) http.Handler {
	// style: doc comment alone
	// because: glue around the handler, and nothing in the body is unclear
	//
	// at WithAttrs: KeyPlane is "http" on both the HTTP and the HTTPS
	//   listener, because one mux serves both [cmd/qdb_rest/main.go]
}
```

The body changes in one place: `observe.WithAttrs(r.Context(),
observe.KeyRequestID, id, observe.KeyPlane, "http")`.

### internal/flightsql/logging.go

```go
// accessContext tags the call's context with a request id minted for
// the call and with the plane, so its log lines join on the id and name
// the plane as the HTTP lines do. [today, Rationale 2]
func (s *Server) accessContext(ctx context.Context) context.Context {
	// style: doc comment + inline note (the existing one on the minted id stays)
	// because: one expression, plus the existing rejected alternative
}
```

The body gains `observe.KeyPlane, "flight"` in the same `WithAttrs`
call.

### internal/query/query.go

The engine words and the outcome words come before `split`, because
`Run` and `Logged` both use them.

```go
// The engine words of the query line. EngineBulkReader is the table
// reader's, which reaches the cluster through Cluster.Read and not
// through Run. [Rationale 1]
const (
	EngineNative     = "native"
	EngineDuckDB     = "duckdb"
	EngineBulkReader = "bulk reader"
)

// The outcome words of the query line, one per case a call can tell
// apart without the plane's status. [Rationale 3]
const (
	outcomeOK        = "ok"
	outcomeError     = "error"
	outcomeCut       = "cut"
	outcomeAbandoned = "abandoned"
)
```

```go
// countRows answers the batches unchanged and adds the rows of every
// batch the consumer pulled to n. A batch the consumer never pulled is
// not counted. [Rationale 7]
func countRows(b qdb.Batches, n *int64) qdb.Batches {
	// style: doc comment alone
	// because: one wrapping loop with no decision beyond the error pass-through
	//
	// at the yield: the row count is read before yield, because the batch
	//   is lent and may be released once yield returns [internal/AGENTS.md, Code]
	// at an error element: it passes through uncounted, because it carries
	//   no batch [qdb.Batches contract, internal/qdb/read.go:95]
}
```

```go
// Logged runs call with a sink that wraps sink, then writes one info
// line under KeyEngine with the duration, the rows the sink pulled, the
// outcome and the attrs. It returns call's error unchanged. The line
// inherits the plane, the request id and the user from the ctx's logger.
// [Rationale 1, 2, 3, 7]
func Logged(ctx context.Context, engine string, call func(func(qdb.Batches) error) error, sink func(qdb.Batches) error, attrs ...slog.Attr) error {
	// style: doc comment + numbered overview + step comments
	// because: the outcome is decided by three branches whose order matters
	//
	// The function wraps the sink to see the rows and the sink's own error,
	// runs the call, and then names the outcome from what it saw:
	//
	// 1. wrap the sink so it counts rows through countRows and keeps its own
	//    error apart from call's, because a sink failure and an engine
	//    failure are different outcomes [Rationale 3]
	// 2. run the call and time it in milliseconds, as both access lines
	//    time a request [internal/httpapi/middleware.go:83]
	// 3. name the outcome. A nil error is ok. An ended ctx is abandoned,
	//    whichever failed, because the caller left and no answer reached it
	//    [writeClusterError, DoGet step 4]. A sink error is cut. Anything
	//    else is error, which arrived before the sink ran [query.Run contract]
	//    - the ctx check comes before the sink check, because a caller who
	//      leaves mid-stream makes the sink fail too [handleQuery step 3]
	// 4. write the line at info, whatever the outcome, with the error under
	//    KeyError, because the M7 criteria put every query at info
	//    [docs/log.md, M7 criteria]
	//    - rejected: warn for a failed query, because the criteria fix the
	//      level and the access line already carries the status [Rationale 3]
}
```

```go
// Run runs a query as the user and streams the result into the sink. ...
// [today's contract, unchanged]
// Every run writes one query line through Logged, preceded by a debug
// line with the statement. [Rationale 1, 9]
func Run(ctx context.Context, u qdb.User, q string, sink func(qdb.Batches) error) error {
	// style: doc comment + overview (extended) + inline notes
	// because: the existing overview gains the two lines and why the debug
	//   line comes first
	//
	// (existing claims stay: the same lent Batches, one native read retry,
	//  DuckDB never retried)
	// at the debug line: it is written before the run, so a statement that
	//   crashes the binary inside DuckDB is in the log [docs/brief.md,
	//   Embedded DuckDB, Isolation is logical]
	// at the debug line: it carries the whole text under KeyStatement and
	//   its length under KeyStatementBytes, and the logger cuts the text
	//   [Rationale 6]
	// at the debug line: the text logged is the one the engine runs, with
	//   the #sql prefix stripped, because KeyEngine already names the engine
	//   [proposal, Open question 4]
	// at Logged: the engine word is EngineDuckDB or EngineNative, as split
	//   chose
}
```

### internal/httpapi/read.go

`handleReadTable` gains no step. Step 2's `Cluster.Read` call goes
through `query.Logged` with `EngineBulkReader` and the table attrs, and
the overview's step 2 says so.

```go
	// 2. the read, through query.Logged, which writes the query line with
	//    engine bulk reader, the table and the range [Rationale 1]. The
	//    sink keeps its own error ... (existing claim stays)
	//    - KeyStart and KeyEnd are written only when the read has a range,
	//      because a read without one has no bounds to name [Open question 3]
	//    - the times are RFC 3339 in UTC, the layout the parameters take
	//      [readOptions, parseTime]
```

### Tests

```go
// TestCutStatement: for any statement and any max, the cut is the
// statement itself when max is 0 or the statement fits, and otherwise a
// valid UTF-8 prefix of at most max bytes followed by "...". The test
// dials nothing, because the cut is pure. [Rationale 5]
func TestCutStatement(t *testing.T) // internal/observe/observe_test.go, rapid
	// style: doc comment + step comments
	// draws: a string of any runes, ASCII and multi-byte both, and max in [0, len+4]
	// checks: max 0 or len <= max -> unchanged; otherwise HasSuffix "...",
	//   the prefix is a prefix of s, len(prefix) <= max, utf8.ValidString(prefix),
	//   and len(prefix) > max-4, so the back-off drops less than one rune
```

```go
// TestCountRows: for any list of batches and any point where the consumer
// stops, the count equals the rows of the batches the consumer pulled.
// The test dials nothing. [Rationale 7]
func TestCountRows(t *testing.T) // internal/query/query_test.go, rapid
	// draws: zero to eight one-column int64 batches of zero to sixteen rows,
	//   built with arrow-go's memory allocator, and a stop index
	// checks: n == sum of NumRows over the pulled prefix, and the batches
	//   yielded are the ones given, in order
```

```go
// TestRunLine: on each cluster, a native query, a #sql query, a refused
// query, a sink that fails and a caller that leaves each write one query
// line with their engine and outcome, and the rows the sink pulled.
// [Rationale 1, 3]
func TestRunLine(t *testing.T) // internal/query/query_test.go, cluster.Each + rapid
	// the ctx carries a JSON logger at debug into a buffer, and the
	//   fixture's cluster through qdb.WithCluster
	// rows: one drawn table (table.Generate, table.CreateAs as f.User)
	//   1. native select of the table -> engine native, outcome ok, rows = table rows
	//   2. #sql select of qdb.<name> -> engine duckdb, outcome ok, rows = table rows
	//   3. a native query of an unknown table -> outcome error, rows 0, error key set
	//   4. a sink that returns an error after ranging -> outcome cut
	//   5. a sink that cancels the ctx and returns its error -> outcome abandoned
	// each row also checks that a debug "query statement" line precedes the
	//   "query" line and carries statement_bytes = len of the text run
```

```go
// TestReadTableLine: on each cluster, a table read writes one query line
// with engine bulk reader, the table name, the rows the read streamed,
// and start and end only when the request named a range. [Rationale 1]
func TestReadTableLine(t *testing.T) // internal/httpapi/read_test.go, cluster.Each + rapid
	// the server's ctx carries a JSON logger into a buffer instead of
	//   observeContext's discarding one. The executor adds that option to
	//   the server fixture, and adds no second fixture.
	// reads: the whole table, then the table under the covering range of
	//   TestReadTableRange
```

### tests/e2e/round-trip.sh

```bash
# QUERY_LINES: one fixed string per engine and plane the round trip
# drives, as the JSON handler writes them (message, then the logger's
# attrs, then the line's). Each must appear on a line of each server's
# log. [Rationale 10]
# check_query_lines <name> <log_file>: every pair of QUERY_LINES is on
# one line of the log, by two chained fixed-string greps, plane first and
# then engine. A server without a log is skipped, as check_log skips it.
```

The pairs are: (http, native), (http, duckdb), (http, bulk reader),
(flight, native) and (flight, duckdb). `main` runs `check_query_lines`
after each `check_log`, and the failure count in `die` names six
checks.

### Document rows

`internal/AGENTS.md`, Logging and metrics, gains:

```
- Each plane's edge tags its logger with `observe.KeyPlane`, `http` or
  `flight`, beside the request id, so every line below names its plane.
- Every query writes one `query` line at info through `query.Logged`:
  engine, plane, `duration_ms`, rows, outcome and the error. `query.Run`
  writes it for both engines, and the table reader writes it with engine
  `bulk reader`, the table and the range. A `query statement` line at
  debug comes before the run with the statement, which the logger cuts
  at `log.statement_max_bytes`. `query.Logged` says what each outcome
  means.
```

`internal/httpapi/AGENTS.md`, Middleware, the bullet on the edge
enriching: `withRequestLogging` also tags `observe.KeyPlane` `http`.
Handlers, the table reader bullet: the read goes through `query.Logged`.

`internal/flightsql/AGENTS.md`, Server, the access line bullet: the ctx
is also tagged with `observe.KeyPlane` `flight`.

ADR-0002, Decision, the enrichment bullet, edited in place
(`docs/AGENTS.md`, ADRs): both edges tag the plane beside the request
id.

`docs/e2e.md`, The v2 round trip, gains step 10:

```
10. The query lines. The driver greps each server's log for one query
    line per engine and plane the round trip drove: native, DuckDB and
    the bulk reader over HTTP, native and DuckDB over Flight. A server
    given as a URL has no log and the step is skipped for it.
```

`tests/e2e/AGENTS.md` gains, after the warning bullet: the driver greps
the query lines by `QUERY_LINES`, and a unit that changes the line's
message or a key changes the list in the same commit.

## Rationale

| decision                                                                                                   | why                                                                                                                                      | rejected, and why                                                                                                                                                                | gained                                                              | given up                                                                               | settled by        |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ----------------- |
| 1. A table read writes the query line with engine `bulk reader`, the table and the range, and no statement | the owner counts table reads as data reads to capture, and the reader has no statement                                                   | engine `native`, because the line could not tell a query from a bulk read; no line, because reads would be invisible                                                             | every data read is in one place                                     | a third engine word                                                                    | owner, 2026-10-30 |
| 2. The edges tag the plane on the logger                                                                   | ADR-0002: edges enrich, and the lines below only log                                                                                     | a plane argument on `query.Run`, because it changes a signature both planes call and the reader would need it too                                                                | every line of a request names its plane, the access line included   | none                                                                                   | owner, 2026-10-30 |
| 3. The outcome is ok / error / cut / abandoned                                                             | a dashboard counts failures by kind without parsing messages                                                                             | ok / error, because a cut stream and an engine refusal would look alike                                                                                                          | four kinds countable                                                | `query.Logged` must order its checks (ctx before sink)                                 | owner, 2026-10-30 |
| 4. A config key sets the statement length, and 0 disables the cut                                          | owner's choice                                                                                                                           | a constant in the code                                                                                                                                                           | operators tune it without a build                                   | one more key, its bound and its example                                                | owner, 2026-10-30 |
| 5. The key is `log.statement_max_bytes`, default 1024, a cut backs off to a rune start and ends in `...`   | it sits with the other log keys, a kibibyte holds several lines of SQL, and an invalid UTF-8 tail would print as a replacement character | a cut on the raw byte, because the JSON line would carry a mangled character; marking with the original length inside the text, because `statement_bytes` carries it as a number | readable cut text, and the length is queryable                      | the default is not measured                                                            | proposal          |
| 6. The logger's `ReplaceAttr` cuts the statement                                                           | `NewLogger` already takes `config.Log`, so no ctx value and no argument carries the length                                               | the length in the ctx through a new observe value, and an argument on `query.Run`, both of which plumb a setting through code that only logs                                     | one home for the cut                                                | the cut applies only to a top-level `KeyStatement`                                     | proposal          |
| 7. `countRows` counts the rows the sink pulled                                                             | the batches are lent, so the count is read before yield, and a sink that stops early pulled fewer                                        | counting in each sink, because each plane would copy it                                                                                                                          | one counter for both planes and the reader                          | a batch never pulled is not counted                                                    | proposal          |
| 8. `query.Describe` writes no line                                                                         | it runs nothing, and `DoGet` runs the query and writes the line                                                                          | a line per describe, because a Flight client's describe and run would count as two queries                                                                                       | one line per execution                                              | a describe that fails is visible only in the access line                               | proposal          |
| 9. The statement line comes before the run, at debug                                                       | a crash in DuckDB kills the binary, and a line written after the run would be lost with it                                               | the statement on the info line after the run, because the criteria put it at debug and a crash would lose it                                                                     | the crashing statement is in the log                                | two lines per query at debug                                                           | proposal          |
| 10. The e2e step greps fixed strings, plane then engine, per pair                                          | `check_log` already greps fixed strings, and the driver knows nothing else about the log format                                          | jq over the JSON lines, rejected for `check_log` already (`tests/e2e/round-trip.sh:291-294`)                                                                                     | both planes and every engine the round trip drives are proven in CI | the strings follow the JSON handler's spelling, and a key rename changes `QUERY_LINES` | proposal          |

## Knowledge

- Commit 2 (keys): the key comments in `observe.go` carry the
  vocabularies of rows 1 and 3 in one line each.
- Commit 3 (http plane): `withRequestLogging`'s doc comment and its note
  carry row 2. `internal/httpapi/AGENTS.md`, Middleware, and ADR-0002,
  Decision, gain the plane.
- Commit 4 (flight plane): `accessContext`'s doc comment carries row 2.
  `internal/flightsql/AGENTS.md`, Server, gains the plane.
- Commit 5 (key and example): `config.Log`, `Default`'s list and the
  comment at the value carry rows 4 and 5 (not measured).
  `examples/qdb_rest.yaml` carries the operator's sentence.
- Commit 6 (cut): `cutStatement` carries row 5 (the back-off), and
  `NewLogger` carries row 6 (rejected: the ctx value and the argument).
- Commit 7 (cut test): `TestCutStatement`'s doc comment.
- Commit 8 (counter): `countRows` carries row 7.
- Commit 9 (counter test): `TestCountRows`'s doc comment.
- Commit 10 (`Logged` and the info line in `Run`): `Logged`'s overview
  carries rows 3 and 7 and the ordering of step 3.
  `internal/AGENTS.md`, Logging and metrics, gains the plane bullet and
  the query line bullet.
- Commit 11 (statement line): `Run`'s notes carry row 9 and the stripped
  prefix.
- Commit 12 (run test): `TestRunLine`'s doc comment.
- Commit 13 (reader line): `handleReadTable`'s step 2 carries row 1 and
  the range rule. `internal/httpapi/AGENTS.md`, Handlers, gains the
  reader bullet edit.
- Commit 14 (reader test): `TestReadTableLine`'s doc comment.
- Commit 15 (e2e): `QUERY_LINES` and `check_query_lines` carry row 10.
  `docs/e2e.md` gains step 10, and `tests/e2e/AGENTS.md` gains the
  bullet.
- Row 8 lands in `query.Describe`'s doc comment in commit 10: "It writes
  no query line, because it runs nothing."
- Commit 16 (plan deletion): nothing moves, because every row has
  landed above. `docs/log.md` gets the entry, Next item 1 is removed,
  and the Handoff item on query capture is deleted.

## How the knowledge lands

The build stage is `/rr-build`. It writes every comment and document row
from sections 3 and 5 in the same commit as the code, and it reconciles
the code against this plan before the build-stage message. Specific to
this unit:

- Commit 15's fixed strings follow what slog's JSON handler writes for
  the tagged logger. Before writing `QUERY_LINES`, the executor reads one
  `query` line from a local server's log and copies the spelling of
  `"plane":"http"` and `"engine":"bulk reader"` from it.
- If `TestRunLine`'s abandoned row cannot be produced through a sink
  that cancels the ctx (for example, because the native engine finishes
  before the sink runs), the executor asks the owner before dropping
  the row. The executor does not drop it silently.

## Commits

1. `docs(plan): query-capture-plan.md, one line per query and per table read on both planes`
2. `feat(observe): keys for the plane, engine, rows, outcome, statement, table and range`
3. `feat(httpapi): withRequestLogging tags the request's logger with plane http`
4. `feat(flightsql): accessContext tags the call's logger with plane flight`
5. `feat(config): log.statement_max_bytes, 1024 by default, 0 keeps the statement whole, refused when negative`
6. `feat(observe): NewLogger cuts the statement attribute at log.statement_max_bytes`
7. `test(observe): a rapid property pins the statement cut and the zero that disables it`
8. `feat(query): countRows counts the rows the sink pulled from the lent batches`
9. `test(query): a rapid property pins countRows against drawn batches and stops`
10. `feat(query): Logged writes one query line with engine, duration, rows and outcome, and Run uses it`
11. `feat(query): Run logs the statement at debug before the run`
12. `test(query): Run's lines on both clusters for both engines, a refusal, a cut and a caller that left`
13. `feat(httpapi): the table reader writes the query line with engine bulk reader, the table and the range`
14. `test(httpapi): the table reader's query line on both clusters, with and without a range`
15. `test(e2e): the round trip finds a query line per engine and plane in each server's log`
16. `/doc-discipline all` over `internal/query`, `internal/observe`,
    `internal/httpapi`, `internal/flightsql`, `internal/config` and
    `tests/e2e`, then `/doc-discipline check` against this plan. A fix
    from either lands as its own `docs(<scope>)` commit.
17. `docs(log): query-capture-plan.md deleted, facts moved to internal/AGENTS.md, internal/httpapi/AGENTS.md, internal/flightsql/AGENTS.md, ADR-0002, docs/e2e.md, tests/e2e/AGENTS.md and the comments of internal/query, internal/observe, internal/config, internal/httpapi and tests/e2e`
18. Verify: push `sc-19567/rr-query-capture`, build its head in
    Buildkite, wait for the result. Green: report the build number. Red:
    fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Level of the reader's table and range: recommend `info`, on the
   `query` line, because both are short and carry no secret.
2. Spelling of the reader's engine word: recommend `bulk reader`
   verbatim, as the owner wrote it, with the space.
3. A read without a range: recommend leaving out `start` and `end`
   rather than writing empty values.
4. The text on the statement line: recommend the text the engine runs,
   with `#sql` stripped, because the engine key already says DuckDB.
5. The key name and default: recommend `log.statement_max_bytes` at 1024.
6. The base is one commit ahead of origin (`9e98605`, docs(log)), which
   is not pushed: recommend that `/rr-merge` pushes it with this unit.
