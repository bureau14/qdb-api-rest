# Query capture -- plan

Status: draft

## Outcome

When this unit lands, every query on both planes writes one `info` line
with the message `query`. The line carries the engine, the plane, the
duration, the row count and the outcome. A `debug` line with the
message `query statement` precedes it and carries the statement text,
truncated at `log.statement_max_bytes`, where 0 keeps the text whole. A table
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

The server already logs structured lines through the standard
library's `slog`. `observe.NewLogger` builds the one process logger,
JSON or console as `log.format` says, and `main` places it in the root
context (ADR-0002). This unit adds no logger and no handler. It changes
the options `NewLogger` passes to the handler it already builds. Every
entry below says whether it is new or changed.

### internal/observe/observe.go

Three vocabularies have a fixed set of values: the plane, the engine
and the outcome. Each becomes a named string type with typed
constants and one attribute constructor, so a call site cannot write a
free string under the key, and the compiler refuses a plane where an
engine is expected. The rows, the statement, the table and the range
carry plain values and use the `slog` constructors under a shared key,
as `slog.Int64("bytes", ...)` does today. Every type lives in
`observe`, because `observe` owns the attribute vocabulary that every
package shares (`internal/AGENTS.md`, Logging, "Add a key to `observe`
before using it in a second package"). The engine words are used by
`internal/query` and `internal/httpapi`, and the plane words by
`internal/httpapi` and `internal/flightsql` [Rationale 11].

New keys go into the existing block of shared attribute keys, after
`KeySession`:

```go
const (
	// ... KeyError, KeyRequestID, KeyUser, KeySession as today
	KeyPlane   = "plane"
	KeyEngine  = "engine"
	KeyOutcome = "outcome"
	KeyRows    = "rows"
	// KeyStatement is a query's text, truncated by the process logger at
	// log.statement_max_bytes.
	KeyStatement = "statement"
	// KeyStatementBytes is the length of the whole statement, before the
	// truncation.
	KeyStatementBytes = "statement_bytes"
	KeyTable          = "table"
	KeyStart          = "start"
	KeyEnd            = "end"
)
```

New types after the keys, each with its constructor:

```go
// Plane names the listener a request came in on. [Rationale 2]
type Plane string

const (
	// PlaneHTTP is the HTTP and the HTTPS listener, which serve one mux.
	// [cmd/qdb_rest/main.go]
	PlaneHTTP   Plane = "http"
	PlaneFlight Plane = "flight"
)

// PlaneAttr is the plane under KeyPlane.
func PlaneAttr(p Plane) slog.Attr

// Engine names what ran a query. EngineBulkReader is the table reader,
// which reads a whole table through Cluster.Read and runs no statement.
// [Rationale 1]
type Engine string

const (
	EngineNative     Engine = "native"
	EngineDuckDB     Engine = "duckdb"
	EngineBulkReader Engine = "bulk reader"
)

// EngineAttr is the engine under KeyEngine.
func EngineAttr(e Engine) slog.Attr

// Outcome is how a query ended, one value per case a call can tell
// apart without the plane's status. query.Logged says when each applies.
// [Rationale 3]
type Outcome string

const (
	OutcomeOK        Outcome = "ok"
	OutcomeError     Outcome = "error"
	OutcomeCut       Outcome = "cut"
	OutcomeAbandoned Outcome = "abandoned"
)

// OutcomeAttr is the outcome under KeyOutcome.
func OutcomeAttr(o Outcome) slog.Attr
```

The three constructors are bare one-liners
(`slog.String(KeyPlane, string(p))`) and get no comment beyond the
doc line.

New, `truncateStatement`, before `NewLogger`, which calls it:

```go
// truncateStatement keeps at most max bytes of a statement and ends a
// truncated one in "...". A max of 0 keeps the statement whole.
// [Rationale 4, 5]
func truncateStatement(s string, max int) string {
	// style: doc comment + inline note at the rune back-off
	// because: one comparison and one loop, where only the back-off needs a reason
	//
	// at the back-off: the end moves back to the start of a UTF-8 rune, so a
	//   multi-byte character is never split into an invalid byte sequence that
	//   the JSON handler would print as a replacement character [Rationale 5]
	// at the back-off: the loop runs at most three times, because a UTF-8
	//   rune is at most four bytes [utf8.UTFMax]
}
```

Changed, `NewLogger`. Its name, its signature and its one caller stay
as they are. The `slog.HandlerOptions` it builds gain a `ReplaceAttr`.

```go
// NewLogger builds the process logger. The caller places it in the root
// context with WithLogger, and nothing installs it as a global. [today]
// The logger truncates every top-level KeyStatement attribute at
// cfg.StatementMaxBytes. [Rationale 5]
func NewLogger(cfg config.Log, w io.Writer) (*slog.Logger, error) {
	// style: doc comment alone, + inline note at ReplaceAttr
	// because: it assembles a handler, and only the truncation needs a reason
	//
	// at ReplaceAttr: the handler truncates the statement, so that no call
	//   site carries the length and nothing new travels in the ctx [Rationale 6]
	// at ReplaceAttr: only a top-level string attribute under KeyStatement
	//   is truncated, because no caller groups it [query.Run, this plan]
}
```

### internal/config/config.go

Changed, `config.Log`, which gains one field:

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
statement truncated at a kibibyte on debug lines". A comment at the value
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
# are truncated and end in "...". 0 keeps every statement whole.
statement_max_bytes: 1024
```

### internal/httpapi/middleware.go

Changed, `withRequestLogging`:

```go
// withRequestLogging puts the request id on the request context and on
// the response, tags the context with the plane, and logs one access line
// when the handler returns. Log lines join on the id. [today, Rationale 2]
func withRequestLogging(next http.Handler) http.Handler {
	// style: doc comment alone
	// because: glue around the handler, and nothing in the body is unclear
}
```

The body changes in one place: `observe.WithAttrs(r.Context(),
observe.KeyRequestID, id, observe.PlaneAttr(observe.PlaneHTTP))`.
`Logger.With` takes a `slog.Attr` among its arguments, so the
constructor goes straight into `WithAttrs`.

### internal/flightsql/logging.go

Changed, `accessContext`:

```go
// accessContext tags the call's context with a request id minted for
// the call and with the plane, so its log lines join on the id and name
// the plane as the HTTP lines do. [today, Rationale 2]
func (s *Server) accessContext(ctx context.Context) context.Context {
	// style: doc comment + inline note (the existing one on the minted id stays)
	// because: one expression, plus the existing rejected alternative
}
```

The body gains `observe.PlaneAttr(observe.PlaneFlight)` in the same
`WithAttrs` call.

### internal/query/query.go

The engine and outcome words are `observe`'s, so this file defines no
vocabulary. It adds `countRows` and `Logged` before `Run`, and it
changes `Run` and `Describe`'s doc comment.

New, `countRows`:

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

New, `Logged`:

```go
// Logged runs call with a sink that wraps sink, then writes one info
// line with the engine, the duration, the rows the sink pulled, the
// outcome and the attrs. It returns call's error unchanged. The line
// inherits the plane, the request id and the user from the ctx's logger.
// [Rationale 1, 2, 3, 7]
func Logged(ctx context.Context, e observe.Engine, call func(func(qdb.Batches) error) error, sink func(qdb.Batches) error, attrs ...slog.Attr) error {
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
	// 3. name the outcome. A nil error is OutcomeOK. An ended ctx is
	//    OutcomeAbandoned, whichever failed, because the caller left and no
	//    answer reached it [writeClusterError, DoGet step 4]. A sink error is
	//    OutcomeCut. Anything else is OutcomeError, which arrived before the
	//    sink ran [query.Run contract]
	//    - the ctx check comes before the sink check, because a caller who
	//      leaves mid-stream makes the sink fail too [handleQuery step 3]
	// 4. write the line at info, whatever the outcome, with the error under
	//    KeyError, because the M7 criteria put every query at info
	//    [docs/log.md, M7 criteria]
	//    - rejected: warn for a failed query, because the criteria fix the
	//      level and the access line already carries the status [Rationale 3]
}
```

Changed, `Run`:

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
	//   its length under KeyStatementBytes, and the process logger truncates
	//   the text [Rationale 6]
	// at the debug line: the text logged is the one the engine runs, with
	//   the #sql prefix stripped, because KeyEngine already names the engine
	//   [proposal, Open question 4]
	// at Logged: the engine is observe.EngineDuckDB or observe.EngineNative,
	//   as split chose
}
```

Changed, `Describe`'s doc comment gains one claim: it writes no query
line, because it runs nothing [Rationale 8].

### internal/httpapi/read.go

Changed, `handleReadTable`. It gains no step. Step 2's `Cluster.Read`
call goes through `query.Logged` with `observe.EngineBulkReader` and the
table attrs, and the overview's step 2 says so.

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

The unit has two tests [Rationale 12]. One pure property pins the
truncation. One live test pins every line the unit writes: both
engines through `Run`, the bulk reader through `Logged` over
`Cluster.Read`, each outcome, and the row count. That one test is
where `countRows` is proven, so the counter has no test of its own.
The handler and the Flight server only pass their calls through
`Logged` and `Run`. Those call sites are glue, which the e2e step
proves on both planes.

```go
// TestTruncateStatement: for any statement and any max, the truncation
// is the statement itself when max is 0 or the statement fits, and
// otherwise a valid UTF-8 prefix of at most max bytes followed by "...".
// The test dials nothing, because the truncation is pure. [Rationale 5]
func TestTruncateStatement(t *testing.T) // internal/observe/observe_test.go
	// style: doc comment + step comments
	// a rapid property (rapid.Check), as the project's pure logic is pinned
	//   [internal/AGENTS.md, Tests; internal/query/query_test.go, TestSplit]
	// draws: rapid.String() for the statement, which draws ASCII and
	//   multi-byte runes both, and rapid.IntRange(0, len+4) for max
	// checks: max 0 or len <= max -> unchanged; otherwise HasSuffix "...",
	//   the prefix is a prefix of s, len(prefix) <= max, utf8.ValidString(prefix),
	//   and len(prefix) > max-4, so the back-off drops less than one rune
```

```go
// TestQueryLines: on each cluster, every call writes one query line with
// its engine, its outcome and the rows its sink pulled. A native query, a
// #sql query and a table read are ok. A refused query is error, a sink
// that fails is cut, and a caller that leaves is abandoned. A query
// writes its statement line at debug first, and a table read writes the
// table and its range instead. [Rationale 1, 3, 7, 9, 12]
func TestQueryLines(t *testing.T) // internal/query/query_test.go, cluster.Each + rapid
	// the ctx carries a JSON logger at debug into a buffer, and the
	//   fixture's cluster through qdb.WithCluster
	// one drawn table per rapid iteration (table.Generate, table.CreateAs as
	//   f.User), and the rows below run against it in order:
	//   1. Run, native select of the table -> engine native, outcome ok,
	//      rows = the table's rows, and a debug "query statement" line before
	//      it with statement_bytes = the length of the text run
	//   2. Run, #sql select of qdb.<name> -> engine duckdb, outcome ok,
	//      rows = the table's rows, and the statement line without the prefix
	//   3. Logged with EngineBulkReader over Cluster.Read under table.Range ->
	//      outcome ok, rows = the table's rows, table, start and end set, and
	//      no statement line
	//   4. Run, a native query of an unknown table -> outcome error, rows 0,
	//      the error key set
	//   5. Run, a sink that pulls one batch and returns an error -> outcome
	//      cut, rows = that batch's rows, which is where countRows stopping
	//      early is proven
	//   6. Run, a sink that cancels the ctx and returns its error -> outcome
	//      abandoned
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
- Each plane's edge tags its logger with `observe.PlaneAttr`,
  `PlaneHTTP` or `PlaneFlight`, beside the request id, so every line
  below names its plane. The plane, the engine and the outcome are
  `observe`'s named types, and a line writes them through their
  constructors only.
- Every query writes one `query` line at info through `query.Logged`:
  engine, plane, `duration_ms`, rows, outcome and the error. `query.Run`
  writes it for both engines, and the table reader writes it with engine
  `bulk reader`, the table and the range. A `query statement` line at
  debug comes before the run with the statement, which the process
  logger truncates at `log.statement_max_bytes`. `query.Logged` says what each outcome
  means.
```

`internal/httpapi/AGENTS.md`, Middleware, the bullet on the edge
enriching: `withRequestLogging` also tags `observe.PlaneHTTP`.
Handlers, the table reader bullet: the read goes through `query.Logged`.

`internal/flightsql/AGENTS.md`, Server, the access line bullet: the ctx
is also tagged with `observe.PlaneFlight`.

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

| decision                                                                                                                                                 | why                                                                                                                                                                                                                                                                                                                                                                                             | rejected, and why                                                                                                                                                                                                                                                                                                                                                                                                                      | gained                                                              | given up                                                                               | settled by        |
| -------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ----------------- |
| 1. A table read writes the query line with engine `bulk reader`, the table and the range, and no statement                                               | the owner counts table reads as data reads to capture, and the reader has no statement                                                                                                                                                                                                                                                                                                          | engine `native`, because the line could not tell a query from a bulk read; no line, because reads would be invisible                                                                                                                                                                                                                                                                                                                   | every data read is in one place                                     | a third engine word                                                                    | owner, 2026-10-30 |
| 2. The edges tag the plane on the logger                                                                                                                 | ADR-0002: edges enrich, and the lines below only log                                                                                                                                                                                                                                                                                                                                            | a plane argument on `query.Run`, because it changes a signature both planes call and the reader would need it too                                                                                                                                                                                                                                                                                                                      | every line of a request names its plane, the access line included   | none                                                                                   | owner, 2026-10-30 |
| 3. The outcome is ok / error / cut / abandoned                                                                                                           | a dashboard counts failures by kind without parsing messages                                                                                                                                                                                                                                                                                                                                    | ok / error, because a cut stream and an engine refusal would look alike                                                                                                                                                                                                                                                                                                                                                                | four kinds countable                                                | `query.Logged` must order its checks (ctx before sink)                                 | owner, 2026-10-30 |
| 4. A config key sets the statement length, and 0 disables the truncation                                                                                 | owner's choice                                                                                                                                                                                                                                                                                                                                                                                  | a constant in the code                                                                                                                                                                                                                                                                                                                                                                                                                 | operators tune it without a build                                   | one more key, its bound and its example                                                | owner, 2026-10-30 |
| 5. The key is `log.statement_max_bytes`, default 1024, and the truncation backs off to a rune start and ends in `...`. The operation is named "truncate" | it sits with the other log keys, a kibibyte holds several lines of SQL, and an invalid UTF-8 tail would print as a replacement character. The M7 criteria say "truncated"                                                                                                                                                                                                                       | "trim", because Go's `strings.Trim` removes a set of characters from both ends and names a different operation; "cut", which the plan used with no reason and which is also the outcome word of a broken stream; a truncation on the raw byte, because the JSON line would carry a mangled character; the original length inside the text, because `statement_bytes` carries it as a number                                            | readable truncated text, a queryable length, one meaning per word   | the default is not measured                                                            | proposal          |
| 6. The process logger's `ReplaceAttr` truncates the statement                                                                                            | `NewLogger` already takes `config.Log`, so no ctx value and no argument carries the length. The server already logs through `slog`, and this changes the existing logger's options, with no second logger                                                                                                                                                                                       | the length in the ctx through a new observe value, and an argument on `query.Run`, both of which plumb a setting through code that only logs                                                                                                                                                                                                                                                                                           | one home for the truncation                                         | the truncation applies only to a top-level `KeyStatement`                              | proposal          |
| 7. `countRows` counts the rows the sink pulled                                                                                                           | the batches are lent, so the count is read before yield, and a sink that stops early pulled fewer                                                                                                                                                                                                                                                                                               | counting in each sink, because each plane would copy it                                                                                                                                                                                                                                                                                                                                                                                | one counter for both planes and the reader                          | a batch never pulled is not counted                                                    | proposal          |
| 8. `query.Describe` writes no line                                                                                                                       | it runs nothing, and `DoGet` runs the query and writes the line                                                                                                                                                                                                                                                                                                                                 | a line per describe, because a Flight client's describe and run would count as two queries                                                                                                                                                                                                                                                                                                                                             | one line per execution                                              | a describe that fails is visible only in the access line                               | proposal          |
| 9. The statement line comes before the run, at debug                                                                                                     | a crash in DuckDB kills the binary, and a line written after the run would be lost with it                                                                                                                                                                                                                                                                                                      | the statement on the info line after the run, because the criteria put it at debug and a crash would lose it                                                                                                                                                                                                                                                                                                                           | the crashing statement is in the log                                | two lines per query at debug                                                           | proposal          |
| 10. The e2e step greps fixed strings, plane then engine, per pair                                                                                        | `check_log` already greps fixed strings, and the driver knows nothing else about the log format                                                                                                                                                                                                                                                                                                 | jq over the JSON lines, rejected for `check_log` already (`tests/e2e/round-trip.sh:291-294`)                                                                                                                                                                                                                                                                                                                                           | both planes and every engine the round trip drives are proven in CI | the strings follow the JSON handler's spelling, and a key rename changes `QUERY_LINES` | proposal          |
| 11. The plane, the engine and the outcome are named string types in `observe`, with typed constants and one attribute constructor each                   | their values are a closed set, a call site then cannot write a free string under the key, and the compiler refuses one vocabulary where another is expected. `observe` owns the shared attribute vocabulary, and the engine words are used by `internal/query` and `internal/httpapi` (owner's annotation, 2026-10-30, asked where the bulk reader belongs and whether the values can be typed) | untyped string constants beside the keys, because a free string still compiles; the engine words in `internal/query`, because the bulk reader is not a query string and `query` is the wrong home for it; struct tags or reflection on a line struct, because `slog` reads no tags and the line would need a reflective encoder; a `LogValuer` line struct, because it nests the attributes under one group key and changes every grep | typed call sites, one home per vocabulary                           | three small types and three one-line constructors                                      | proposal          |
| 12. Two tests: a pure rapid property for the truncation, and one live test for every line                                                                | the owner wants no test bloat (annotation, 2026-10-30). The live test proves `countRows` through real rows and an early stop, and the call sites in the handler and the Flight server are glue the e2e step covers                                                                                                                                                                              | a pure property for `countRows`, a test per line kind, and a handler test for the reader, because each repeats what the live test checks                                                                                                                                                                                                                                                                                               | one place that pins the line                                        | `countRows` is proven only over the rows a drawn table produces                        | owner, 2026-10-30 |

## Knowledge

- Commit 2 (vocabulary): the type, constant and key comments in
  `observe.go` carry rows 1, 2, 3 and 11.
- Commit 3 (http plane): `withRequestLogging`'s doc comment carries
  row 2. `internal/httpapi/AGENTS.md`, Middleware, and ADR-0002,
  Decision, gain the plane.
- Commit 4 (flight plane): `accessContext`'s doc comment carries row 2.
  `internal/flightsql/AGENTS.md`, Server, gains the plane.
- Commit 5 (key and example): `config.Log`, `Default`'s list and the
  comment at the value carry rows 4 and 5 (not measured).
  `examples/qdb_rest.yaml` carries the operator's sentence.
- Commit 6 (truncation): `truncateStatement` carries row 5 (the
  back-off, and the name), and `NewLogger` carries row 6 (rejected: the
  ctx value and the argument).
- Commit 7 (truncation test): `TestTruncateStatement`'s doc comment.
- Commit 8 (counter): `countRows` carries row 7.
- Commit 9 (`Logged` and the info line in `Run`): `Logged`'s overview
  carries rows 3 and 7 and the ordering of step 3. `Describe`'s doc
  comment carries row 8. `internal/AGENTS.md`, Logging and metrics,
  gains the plane bullet and the query line bullet.
- Commit 10 (statement line): `Run`'s notes carry row 9 and the
  stripped prefix.
- Commit 11 (reader line): `handleReadTable`'s step 2 carries row 1 and
  the range rule. `internal/httpapi/AGENTS.md`, Handlers, gains the
  reader bullet edit.
- Commit 12 (lines test): `TestQueryLines`'s doc comment, and a sentence
  in it saying that it is where `countRows` is proven and that the call
  sites are glue the e2e step covers (row 12).
- Commit 13 (e2e): `QUERY_LINES` and `check_query_lines` carry row 10.
  `docs/e2e.md` gains step 10, and `tests/e2e/AGENTS.md` gains the
  bullet.
- Commit 15 (plan deletion): nothing moves, because every row has
  landed above. `docs/log.md` gets the entry, Next item 1 is removed,
  and the Handoff item on query capture is deleted.

## How the knowledge lands

The build stage is `/rr-build`. It writes every comment and document row
from sections 3 and 5 in the same commit as the code, and it reconciles
the code against this plan before the build-stage message. Specific to
this unit:

- Commit 13's fixed strings follow what slog's JSON handler writes for
  the tagged logger. Before writing `QUERY_LINES`, the executor reads one
  `query` line from a local server's log and copies the spelling of
  `"plane":"http"` and `"engine":"bulk reader"` from it.
- If `TestQueryLines`' abandoned row cannot be produced through a sink
  that cancels the ctx (for example, because the native engine finishes
  before the sink runs), the executor asks the owner before dropping
  the row. The executor does not drop it silently.

## Commits

1. `docs(plan): query-capture-plan.md, one line per query and per table read on both planes`
2. `feat(observe): the plane, engine and outcome types with their attributes, and the keys of the query line`
3. `feat(httpapi): withRequestLogging tags the request's logger with plane http`
4. `feat(flightsql): accessContext tags the call's logger with plane flight`
5. `feat(config): log.statement_max_bytes, 1024 by default, 0 keeps the statement whole, refused when negative`
6. `feat(observe): NewLogger truncates the statement attribute at log.statement_max_bytes`
7. `test(observe): a rapid property pins the statement truncation and the zero that disables it`
8. `feat(query): countRows counts the rows the sink pulled from the lent batches`
9. `feat(query): Logged writes one query line with engine, duration, rows and outcome, and Run uses it`
10. `feat(query): Run logs the statement at debug before the run`
11. `feat(httpapi): the table reader writes the query line with engine bulk reader, the table and the range`
12. `test(query): every query line on both clusters, for both engines, the bulk reader and each outcome`
13. `test(e2e): the round trip finds a query line per engine and plane in each server's log`
14. `/doc-discipline all` over `internal/query`, `internal/observe`,
    `internal/httpapi`, `internal/flightsql`, `internal/config` and
    `tests/e2e`, then `/doc-discipline check` against this plan. A fix
    from either lands as its own `docs(<scope>)` commit.
15. `docs(log): query-capture-plan.md deleted, facts moved to internal/AGENTS.md, internal/httpapi/AGENTS.md, internal/flightsql/AGENTS.md, ADR-0002, docs/e2e.md, tests/e2e/AGENTS.md and the comments of internal/query, internal/observe, internal/config, internal/httpapi and tests/e2e`
16. Verify: push `sc-19567/rr-query-capture`, build its head in
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
