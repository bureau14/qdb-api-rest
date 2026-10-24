# Plan: the Flight client in the equivalence property, and the ADBC and JDBC smoke steps of the round trip

Status: approved

Unit of M5 (`docs/log.md`, Next). Branch `sc-19567/rr-flight-tests`.
This document is deleted when the unit lands (`docs/AGENTS.md`, Plans).

## Outcome

When this unit lands:

- `TestRoundtrip` (`internal/httpapi/roundtrip_test.go`) starts the Flight
  listener beside the HTTP handler and, in its step 6, reads every table
  over Flight on both engines under the refreshed HTTP access token. The
  Flight result equals the Arrow body `POST /api/v2/query` answers for the
  same text, and the native one equals the rows generated.
- The Flight client helpers live in one fixture subpackage,
  `internal/qdbtest/flight`, and `internal/flightsql/flightsql_test.go`
  reads statements through it.
- `e2etool adbc` runs one statement through arrow-adbc's Go Flight SQL
  driver and writes the result through a server encoder.
- `tests/e2e/tools/jdbc/` is a Maven project whose `pom.xml` declares
  Arrow's Flight SQL JDBC driver as a dependency and whose one class runs a
  statement and prints its row count and column names.
- The shell round trip, per table and after the ingest, reads the table
  back through ADBC and through JDBC, runs a `#sql` query of DuckDB-only
  types through each, and asserts bytes on the ADBC side and counts and
  names on the JDBC side, on both clusters.
- `docs/e2e.md`, ADR-0018 Decision 14 and the `AGENTS.md` files of
  `internal/`, `internal/flightsql`, `internal/httpapi` and `tests/e2e`
  say so, and the log's M5 criteria no longer list the dispatch property
  as outstanding.

Left for later units: the qdb-duck story for a whole `qdb://` URI (owner's
word), a FreeBSD skip of the JDBC step if the first build shows the need,
and the Buildkite run that closes M5.

## Verified facts

- `TestStatement` names the HTTP format-equivalence property extended
  with a Flight client as a later unit's
  (`internal/flightsql/flightsql_test.go:356-362`), and compares each
  engine with its own oracle: native against the generated rows, `#sql`
  against `Cluster.Duck` (`:385-395`).
- The Flight client helpers are white-box in that test file: `dial`
  (`:113-122`), `ticketOf` (`:145-155`), `readAll` (`:160-174`), `release`
  (`:177-181`), `concat` (`:185-210`), `execute` (`:214-222`), `duck`
  (`:226-247`), `sameBatch` (`:251-261`), `withHeader` (`:267-269`).
  `TestStatementRows` reads errors through `readAll` and `ticketOf`
  (`statementErr`, `:425-431`).
- `internal/flightsql` imports `auth`, `build`, `config`, `observe`,
  `qdb`, `query`, `tlsconf` and, in its test, `qdbtest`, `qdbtest/cluster`
  and `qdbtest/table`. `internal/httpapi` imports neither `flightsql` nor
  is imported by it (grep of both packages' imports, 2026-10-23). A
  fixture subpackage that imported `internal/flightsql` would be a test
  import cycle for `flightsql`'s own tests, so the fixture holds the client
  side only.
- The Flight server binds its port in `New` (`internal/flightsql/server.go:76`),
  which `main` calls while building the listeners (`cmd/qdb_rest/main.go:144`)
  before any `Serve`. So when the HTTP port answers (`start_server`,
  `tests/e2e/common.sh:134-144`, waits on the REST port), the Flight port
  is bound too.
- `TestRoundtrip` step 6 (`internal/httpapi/roundtrip_test.go:412-429`)
  queries each table with `checkQuery` and `checkQueryFormats` always, and
  skips the bulk read and the DuckDB checks under `async`, because the
  bulk reader lags an async push and qdb-duck scans through it.
- `checkDuckDBFormats` compares the Arrow body decoded, by `array.RecordEqual`,
  because DuckDB leaves the bytes under a null slot undefined
  (`roundtrip_test.go:262-268`).
- `server.query` posts to `/api/v2/query` with the headers given
  (`internal/httpapi/query_test.go:76-79`); `encoders` maps the Arrow media
  type to `encoding.Arrow{}` (`internal/httpapi/negotiate.go:11-16`).
- The native result reaches the Flight wire in slices of `model.BatchRows`
  rows (`internal/qdb/cluster.go:448-455`) and the DuckDB result one vector
  per batch (`internal/olap/instance.go:158-161`), so a comparison joins
  the batches first.
- `DoGet` writes a batch of no rows to no message
  (`internal/flightsql/statement.go:110-112`).
- `e2etool` builds with `-mod=vendor` (`tests/e2e/Makefile:95-98`), so a
  new module is vendored. Its subcommands dispatch on `os.Args[1]`
  (`tests/e2e/tools/e2etool/main.go:344-366`); `tocsv` decodes an Arrow
  body typed by the round trip's schema and puts `$table` back in front
  (`main.go:279-342`; `docs/e2e.md:155-162`).
- arrow-adbc `go/adbc` v1.12.0 requires arrow-go v18.7.0, grpc v1.82.1 and
  protobuf v1.36.11 (module cache `arrow-adbc/go/adbc/@v/v1.12.0.mod:27,42`),
  all below this repository's pins (`go.mod:8,20`), so MVS keeps the pins.
  Its Flight SQL driver (`driver/flightsql`) has no cgo; the module's cgo
  lives under `pkg/`, the C wrappers (grep of the module, 2026-10-23).
- The driver's options: `adbc.OptionKeyURI` (`"uri"`) with the schemes
  `grpc+tcp://`, `grpc+tls://`, `grpc+unix://` (`flightsql_driver.go:37,132`),
  `adbc.OptionKeyUsername` and `adbc.OptionKeyPassword`
  (`flightsql_database.go:155-174`), which make `Open` run
  `AuthenticateBasicToken` only when one of them is set
  (`flightsql_database.go:463-470`), and `OptionSSLSkipVerify`
  (`"adbc.flight.sql.client_option.tls_skip_verify"`,
  `flightsql_driver.go:59`, parsed at `flightsql_database.go:119-131`).
  The API is `NewDriver(alloc).NewDatabase(opts)`, `db.Open(ctx)`,
  `conn.NewStatement()`, `stmt.SetSqlQuery(q)`, `stmt.ExecuteQuery(ctx)`
  answering an `array.RecordReader` (`flightsql_driver.go:116,181`,
  `flightsql_database.go:508`, `flightsql_connection.go:1097`,
  `flightsql_statement.go:485,521`).
- Arrow's Flight SQL JDBC driver: Maven coordinates
  `org.apache.arrow:flight-sql-jdbc-driver`, URL
  `jdbc:arrow-flight-sql://HOST:PORT/?param=value`, properties `user`,
  `password`, `useEncryption` (default true),
  `disableCertificateVerification` (default false), compatible with JDK
  17+, and the JVM needs `--add-opens=java.base/java.nio=ALL-UNNAMED`
  (arrow.apache.org/java/main/flight_sql_jdbc_driver.html, read
  2026-10-23). The newest release on Maven Central is 19.0.0
  (search.maven.org, 2026-10-23).
- The CSV encoder writes a decimal through `ToString(scale)`, a boolean
  through `strconv.FormatBool` and a date32 as `2026-06-11`
  (`internal/encoding/csv.go:95-115`); `TestDuckDBScalars` pins the cells
  `1`, `true`, `1.50` and `2026-06-11` for `1`, `true`, `1.50::DECIMAL(5,2)`
  and `DATE '2026-06-11'` (`internal/httpapi/duckdb_test.go:28-41`).
- The round trip's servers serve Flight on `40094` and `40095`
  (`tests/e2e/Makefile:36,68`). `run_cluster` calls one step function per
  read format and coding after the ingest (`tests/e2e/round-trip.sh:281-304`),
  and `duckdb_back` selects the table name as `$table` because qdb-duck's
  scan answers none (`:255-266`). The tables are dropped before create and
  left after (`:128-144`).
- A bare `SELECT *` on the native engine answers `$timestamp`, `$table`,
  then the columns in creation order (`docs/e2e.md:103-108`); the generated
  CSV has `$table` first, then `$timestamp` (`docs/e2e.md:145-147`).
- Local toolchain: Maven 3.10.0 on JDK 21 (Corretto), `mvn -version`,
  2026-10-23. Nothing in this repository verifies a JDK or Maven on the
  agents (grep of `.buildkite/`, `scripts/cicd/`, `tests/e2e/`, 2026-10-23).
- `TestSplit` (`internal/query/query_test.go:38`) is the dispatch property
  over generated prefixes the M5 criteria list.

## Design

### Commit 2: `internal/qdbtest/flight` (new package)

```go
// Package flight is the Flight client fixture: a client on a server of
// the tests with verification off, a statement read into one batch, an
// Arrow IPC body read the same way, and batch equality. It is a
// subpackage of qdbtest for the reason table and cluster are, and it
// holds no server start, because a fixture that imported
// internal/flightsql would be a test import cycle for that package's own
// tests [Verified facts, imports]
package flight
```

```go
// Dial opens arrow-go's Flight SQL client on addr with certificate
// verification off, because every test server runs on the ephemeral
// certificate, as the driver smoke steps connect [flightsql_test.go:113-122]
func Dial(t table.T, addr string) *flightsql.Client {
	// style: doc comment alone
	// because: one call and one cleanup
}
```

```go
// Bearer is ctx with one authorization header carrying token, the form
// every RPC presents [flightsql_test.go:267-269; ADR-0018 Decision 7]
func Bearer(ctx context.Context, token string) context.Context {
	// style: doc comment alone
}
```

```go
// TicketOf runs Execute for q and answers the one endpoint's ticket.
// GetFlightInfo runs nothing, so a statement's failure surfaces on DoGet
// [flightsql_test.go:143-155; ADR-0018 Decision 9]
func TicketOf(t table.T, cl *flightsql.Client, ctx context.Context, q string) *flight.Ticket {
	// style: doc comment alone
}
```

```go
// ReadAll presents ticket on DoGet and reads the stream to its end: its
// schema, its batches retained for the caller to release, and the error
// that ended it [flightsql_test.go:157-174]
func ReadAll(t table.T, cl *flightsql.Client, ctx context.Context, ticket *flight.Ticket) (*arrow.Schema, []arrow.RecordBatch, error) {
	// style: doc comment alone
}
```

```go
// Release releases every batch of recs.
func Release(recs []arrow.RecordBatch)
```

```go
// Concat joins recs, which share schema, into one batch the caller
// releases. No batch is an empty batch of schema [flightsql_test.go:183-210]
func Concat(t table.T, schema *arrow.Schema, recs []arrow.RecordBatch) arrow.RecordBatch {
	// style: doc comment alone + inline note at the empty case
	//
	// at len(parts) == 0: an empty array of the field's type, because
	// Concatenate needs at least one input [flightsql_test.go:195-198]
}
```

```go
// Execute runs q over Flight as a whole, Execute then DoGet, and answers
// the result joined into one batch the caller releases, so a comparison
// does not depend on how the server cut the stream: the native engine
// sends model.BatchRows rows per message and DuckDB one vector
// [Verified facts, batch sizes]
func Execute(t table.T, cl *flightsql.Client, ctx context.Context, q string) arrow.RecordBatch {
	// style: doc comment alone
}
```

```go
// FromIPC reads an Arrow IPC stream into one batch the caller releases,
// the shape Execute answers, so a Flight result and an HTTP Arrow body
// compare through Same [Rationale: oracle]
func FromIPC(t table.T, r io.Reader) arrow.RecordBatch {
	// style: doc comment alone
}
```

```go
// Same reports whether got and want carry equal schemas and equal
// columns by array equality, which reads no byte under a null slot,
// because DuckDB leaves those bytes undefined [flightsql_test.go:249-261;
// roundtrip_test.go:262-268]
func Same(got, want arrow.RecordBatch) bool {
	// style: doc comment alone
}
```

`internal/AGENTS.md`, Tests, gains a sentence in the fixture bullet:

```
- ... The Flight client fixture (`internal/qdbtest/flight`: `Dial`,
  `Execute`, `FromIPC`, `Same`) is a subpackage for the same reason, and
  it holds no server start, because `internal/flightsql`'s own tests
  import it. [Rationale: fixture]
```

### Commit 3: `internal/flightsql/flightsql_test.go` on the fixture

The local `dial`, `ticketOf`, `readAll`, `release`, `concat`, `execute`,
`sameBatch` and `withHeader` are removed and every call site reads
through the fixture (imported under an alias, because arrow-go's `flight`
is imported too). `duck` stays, built on `Concat`. The file header names
the fixture. `TestStatement`'s comment drops the sentence naming the
later unit (`:361-362`) and keeps the rest.

`internal/flightsql/AGENTS.md`, Tests, first bullet:

```
- One test file, `flightsql_test.go`, drives arrow-go's Flight SQL
  client through the Flight client fixture (`internal/qdbtest/flight`)
  against the server on a port of its own ... [unchanged after]
```

### Commit 4: `internal/httpapi/query_test.go`, the Flight server beside the handler

```go
// server is the root handler plus the request context main would give
// it ... and the Flight listener main would start beside it, on a port of
// its own under the same context, with a client on it, because the round
// trip proves the two planes answer the same table [Rationale: flight in
// the round trip]
type server struct {
	// ... existing fields
	flight *flight.Client   // the fixture's client on the Flight listener
}
```

```go
// newServerOn is newServer over any cluster and user, for the unreachable
// cluster. It starts the Flight listener on 127.0.0.1:0 under the
// ephemeral certificate and stops it on cleanup [flightsql_test.go:83-102]
func newServerOn(t *testing.T, c *qdb.Cluster, u qdb.User) server {
	// style: doc comment alone + inline note at the Flight start
	//
	// at flightsql.New: the same ctx as the handler's, because both planes
	// read the cluster and the keychain from it [internal/AGENTS.md, Logging]
}
```

### Commit 5: `internal/httpapi/roundtrip_test.go`, the Flight read

```go
// checkFlight reads tbl over Flight with q under the server's token and
// compares the result with the rows generated and with the Arrow body
// the query route answers for q, so the two planes answer one table
// [ADR-0018 Decision 14; Rationale: oracle]
func (s server) checkFlight(t *rapid.T, tbl table.Table, q string) {
	// style: doc comment + numbered overview + step comments
	// because: two oracles in a fixed order, and the token is the HTTP one
	//
	// The token is the refreshed HTTP access token presented as the
	// Flight bearer, because one token opens both planes
	// [docs/brief.md, Authentication, "The same token works everywhere";
	// Rationale: token]
	//
	// 1. the Flight result joined into one batch, because the engines cut
	//    the stream differently [Verified facts, batch sizes]
	// 2. the rows generated, column by column by name [table.Check]
	// 3. the Arrow body of POST /api/v2/query for q, read into one batch
	//    and compared by array equality, because every text format is
	//    already proven equal to the encoder over these batches, so Flight
	//    equals every HTTP format through this one [Rationale: oracle]
	//    - rejected: decoding every text format, because the decoders read
	//      the ingest dialect [Rationale: oracle]
}
```

Step 6 of `TestRoundtrip` gains, after `checkQueryFormats`, a call with
`tbl.Select()`, and after `checkDuckDBFormats`, a call with
`"#sql " + tbl.SelectSQL()`; the `#sql` one stays behind the `async`
skip for the reason the DuckDB checks do. The step comment gains one
sentence: both engines over Flight too, the `#sql` one skipped with the
DuckDB checks.

`internal/httpapi/AGENTS.md`, Tests, the round trip bullet:

```
- ... Step 6 reads every table over Flight too, native and `#sql`, under
  the refreshed HTTP access token, against the generated rows and the
  query route's Arrow body (`checkFlight`), so the format-equivalence
  property covers the Flight plane (ADR-0018, Decision 14). The server
  fixture starts the Flight listener beside the handler for it.
```

### Commit 6: `go.mod`, `vendor/`

`github.com/apache/arrow-adbc/go/adbc v1.12.0` required, `go mod tidy`,
`go mod vendor`. The pins of arrow-go, grpc and protobuf do not move
[Verified facts, arrow-adbc].

`internal/AGENTS.md`, Building, after the grpc bullet:

```
- arrow-adbc's Go module is vendored for `e2etool`'s ADBC smoke step and
  for nothing in the binary. Its arrow-go, grpc and protobuf requirements
  sit below this repository's pins, so a bump of arrow-go is what moves
  them, and arrow-adbc is bumped only together with arrow-go
  (`docs/e2e.md`, The Flight SQL smoke steps).
```

### Commit 7: `tests/e2e/tools/e2etool/main.go`, the `adbc` subcommand

```go
// adbc runs one statement through arrow-adbc's Flight SQL driver and
// writes the result to stdout through the encoder of --format, Arrow
// IPC by default, so the driver pipes it through tocsv as every other
// response, or CSV for a result the round trip's schema does not type.
// --username and --secret make the driver handshake; without them it
// presents no token, the anonymous user [Verified facts, driver options]
func adbc(args []string) error {
	// style: doc comment + numbered overview + step comments
	// because: a driver session has an order, and two choices need a why
	//
	// 1. the database over the URI with verification off, because the
	//    server runs on the ephemeral certificate [Verified facts, driver
	//    options; docs/e2e.md, smoke]
	// 2. username and password only when a username is given, because the
	//    driver handshakes when either is set and the insecure cluster's
	//    user is anonymous [flightsql_database.go:463-470]
	// 3. one connection, one statement, ExecuteQuery
	// 4. the reader's batches as a sequence into the encoder, the tool's
	//    rule that it formats no cell [tests/e2e/AGENTS.md]
	//    - rejected: writing CSV in the tool, because tocsv already decodes
	//      Arrow and restores $table [Rationale: adbc output]
}
```

The header comment of `main.go` (`:1-8`) and `usage` gain the subcommand.

### Commit 8: `tests/e2e/round-trip.sh`, the ADBC steps

```sh
# adbc_back <out> <flight> <user> <secret> <table> <csv>: the table over
# ADBC. e2etool adbc runs SELECT * FROM <table> through arrow-adbc's
# Flight SQL driver and writes Arrow IPC, which tocsv decodes and
# compares with the generated CSV as query_back does, so the stock
# driver's result is the rows generated, byte for byte [Rationale: smoke
# steps; e2e.md, step 5]
adbc_back() {
    # style: doc comment alone + inline note at the pipe
    # at the pipe: the exit of e2etool adbc is read through pipefail,
    # because tail would mask it [memory: pipe exit codes; set -euo pipefail]
}
```

```sh
# adbc_types <out> <flight> <user> <secret>: the #sql query of one row of
# DuckDB-only types (int32, boolean, decimal, date) over ADBC, written as
# CSV by the server's encoder and compared with the cells
# TestDuckDBScalars pins, so the driver carries a type QuasarDB does not
# have [docs/e2e.md:211-212; Verified facts, CSV scalars]
adbc_types() {
    # style: doc comment alone
}
```

`run_cluster` calls `adbc_back` and `adbc_types` once per table after the
format loop; `parse_args` gains `--insecure-flight` and `--secure-flight`
(host:port), and `main` passes each cluster's Flight address and
credentials. The Makefile passes `127.0.0.1:$(FLIGHT_PORT)` and
`127.0.0.1:$(FLIGHT_PORT_SECURE)`. The secure credentials are read from
the user security file with `jq` (already required, `docs/e2e.md:249`).

### Commit 9: `tests/e2e/tools/jdbc/`, the Maven project

`pom.xml`: groupId `net.quasardb.rest`, artifactId `e2e-jdbc-smoke`,
compiler release 17 (the driver's floor), one dependency
`org.apache.arrow:flight-sql-jdbc-driver:19.0.0`, and the shade plugin
with the main class in the manifest, so the build answers one runnable
jar `target/e2e-jdbc-smoke.jar` [Rationale: jdbc run]. The pom's
comments carry: why 19.0.0 (the newest release; the Flight SQL wire is
the same across 18 and 19), why shade (one `java -jar` per step with the
`--add-opens` the driver needs, no classpath file whose separator differs
per platform), why release 17 (the driver's floor).

`src/main/java/net/quasardb/rest/e2e/Smoke.java`:

```java
// Smoke runs one statement through Arrow's Flight SQL JDBC driver and
// prints "rows=<n>" and "columns=<name>,<name>,...", and nothing of a
// cell, because the driver compares counts and names and Java formats
// no cell [Rationale: smoke assertions]. Args: --url <jdbc url>
// [--user <u> --password <p>] --query <q>.
public final class Smoke {
    // style: doc comment + numbered overview + step comments
    // because: a JDBC session has an order, and the URL carries two
    // choices
    //
    // 1. the URL is jdbc:arrow-flight-sql://host:port/ with
    //    useEncryption=true and disableCertificateVerification=true,
    //    because the listener is TLS only on the ephemeral certificate
    //    [ADR-0018 Decision 1; Verified facts, JDBC]
    // 2. user and password as properties only when given, because the
    //    anonymous user presents none [docs/e2e.md:207-209]
    // 3. executeQuery, then the metadata's column names and a count of
    //    next(), because the ResultSet is the driver's own reading of the
    //    stream
}
```

`tests/e2e/Makefile` gains a `jdbc-smoke` target that runs
`mvn -q -B -f tests/e2e/tools/jdbc/pom.xml package` and is a prerequisite
of `test-round-trip`, which passes `--jdbc-jar <path>` to the driver. The
Makefile comment says Maven resolves and caches the driver, in its local
repository, so the harness downloads and hashes nothing itself
[Rationale: maven]. `.gitignore` gains `tests/e2e/tools/jdbc/target/`.

### Commit 10: `tests/e2e/round-trip.sh`, the JDBC steps

```sh
# jdbc_back <out> <flight> <user> <secret> <table> <rows> <csv>: the table
# over JDBC. java runs the smoke jar on SELECT * FROM <table>, and the
# driver compares rows= with the count gen printed and columns= with
# $timestamp, $table and the data columns of the generated header, the
# order a bare SELECT * answers [docs/e2e.md:103-108; Rationale: smoke
# assertions]
jdbc_back() {
    # style: doc comment alone + inline note at the java line
    # at java: --add-opens=java.base/java.nio=ALL-UNNAMED, because the
    # driver needs it on JDK 17+ [Verified facts, JDBC]
}
```

```sh
# jdbc_types <out> <flight> <user> <secret>: the #sql query of
# adbc_types over JDBC, compared as rows=1 and columns=i,b,d,day
jdbc_types() {
    # style: doc comment alone
}
```

`parse_args` gains `--jdbc-jar`; a missing java fails the step, and no
platform is skipped [Rationale: no JVM]. The `[OK]` line of `run_cluster`
names the two drivers. The e2e `README.md` usage gains the Maven and JDK
prerequisites and `make jdbc-smoke`.

### Documents that change with the commits above

`docs/e2e.md`:

- Purpose item 2 and the paragraph after it (`:20-22`, `:30-32`): the
  smoke steps are steps of the round trip; "one Java source file" becomes
  "one Maven project".
- "The v2 round trip" gains step 7: per table, after the formats, the
  ADBC read and types query, and the JDBC read and types query, with the
  assertions above; `:192-194` ("Flight SQL has no step in the round trip")
  is rewritten: Flight has no HTTP step, and the stock drivers' steps are
  the smoke steps.
- "The Flight SQL smoke tests" (`:196-237`) becomes "The Flight SQL smoke
  steps": what each driver runs, what is asserted and why the two assert
  differently, the Maven project and `java -jar`, Maven as the resolver
  and cache, the arrow-adbc pin rule, the `--add-opens`, both clusters
  and all platforms with FreeBSD decided on the first build.
- Layout gains `tools/jdbc/` and the `jdbc-smoke` target.
- Decision log (2026-10-23) rows: the Rationale rows marked "docs/e2e.md"
  below.

`docs/adr/0018-flight-sql.md`, Decision 14, edited in place: the two
smoke tests are steps of the round trip after the ingest, over its
tables, ADBC comparing bytes through the tool's encoders and JDBC counts
and names from a Maven project; the sentence "Flight has no step in the
round trip and no stress case" becomes "Flight has no HTTP step in the
round trip and no stress case". Its Consequences bullet on `vendor/`
stays true.

`tests/e2e/AGENTS.md` gains bullets: the ADBC step's output is Arrow
through tocsv or CSV through the server's encoder, never the tool's
text; the JDBC project is built by Maven only, and `java -jar` runs it;
the types query's expected cells are the ones `TestDuckDBScalars` pins,
and a change to one changes the other.

## Rationale

| decision                                                                                                                                                                   | why                                                                                                                                                           | rejected, and why                                                                                                                                                                                         | gained                                                    | given up                                                                                          | settled by        |
| -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ----------------- |
| smoke steps: the ADBC and JDBC tests are steps of the shell round trip, once per table after the ingest                                                                    | the servers, the tables and the generated rows exist by construction, and one lifecycle                                                                       | a separate target reading the round trip's leftovers, which fails alone and hides an ordering; a self-contained target filling a table through qdbsh, a second writer of test data with weaker assertions | the strongest assertion for no new data path              | the wording "Flight has no step in the round trip" in ADR-0018 and `docs/e2e.md`, edited in place | owner, 2026-10-23 |
| smoke assertions: ADBC compares bytes through the server's encoders; JDBC prints row count and column names                                                                | the tool has the encoders, and Java must not format a cell                                                                                                    | both counting only, giving up a comparison the tool has for free; Java printing CSV, a second formatter to keep in step                                                                                   | full rows on the Go side, a driver proof on the Java side | the JDBC step proves no cell                                                                      | owner, 2026-10-23 |
| oracle: the Flight result in `TestRoundtrip` is one joined batch compared by array equality with the query route's Arrow body, the native one also with the generated rows | every text format is already proven equal to the encoder over the same batches, so one comparison covers all; bytes under a null slot are undefined on DuckDB | decoding every text format back through the decoders, which read the ingest dialect and drop `$table`                                                                                                     | one comparison, no decoder path                           | nothing                                                                                           | owner, 2026-10-23 |
| fixture: dial, execute-to-one-batch, IPC read and batch equality in `internal/qdbtest/flight`; the Flight tests refactored onto it; server start per test file             | the table and cluster fixtures exist for the same reason, and a comparer has one home                                                                         | copies in the httpapi test file, two homes for one comparer                                                                                                                                               | one home                                                  | a fixture that cannot start the server, because of the test import cycle                          | owner, 2026-10-23 |
| token: the round trip presents the refreshed HTTP access token as the Flight bearer                                                                                        | one token opens both planes, and the round trip proves it                                                                                                     | a Flight Handshake per iteration, which `TestHandshake` already proves                                                                                                                                    | the cross-plane token proof                               | the round trip exercises no Handshake                                                             | proposal          |
| maven: the JDBC smoke is a Maven project with a `pom.xml` declaring the driver, built by Maven, which resolves and caches the jar                                          | the owner wants a proper JDBC project and no Go-style fetching; Maven's local repository is the cache                                                         | a Makefile download with a SHA-256 pin, the spec's earlier wording; a jar in git or in agent provisioning                                                                                                 | the Java ecosystem's own build and integrity              | Maven on every agent, unverified                                                                  | owner, 2026-10-23 |
| jdbc run: shade into one runnable jar, run with `java -jar` and `--add-opens` per step                                                                                     | one JVM per step with the flag the driver needs, and no classpath file whose separator differs on Windows                                                     | `exec:java` per step, a Maven start per table and cluster and no `--add-opens` for the in-process JVM; `dependency:build-classpath`, whose separator and paths differ on the MSYS agents                  | one command on every platform                             | a shaded jar of the driver's size in `target/`                                                    | proposal          |
| jar version: 19.0.0                                                                                                                                                        | the newest release; the Flight SQL wire is the same across 18 and 19                                                                                          | 18.3.0 for the server's arrow-go major, no functional reason                                                                                                                                              | current driver                                            | nothing                                                                                           | proposal          |
| adbc output: the subcommand writes Arrow IPC through `encoding.Arrow` and the driver pipes it through `tocsv`; `--format csv` for the types query                          | tocsv already decodes Arrow and restores `$table`, and the tool formats no cell                                                                               | a CSV writer in the subcommand duplicating tocsv's reorder                                                                                                                                                | one decode path                                           | a `--format` flag                                                                                 | proposal          |
| types query: `#sql SELECT 1 AS i, true AS b, 1.50::DECIMAL(5,2) AS d, DATE '2026-06-11' AS day`, expected cells `1,true,1.50,2026-06-11`                                   | the spec asks for a DuckDB type QuasarDB lacks, and `TestDuckDBScalars` pins these cells                                                                      | a list or struct type, which the text encoders do not write                                                                                                                                               | a known literal, pinned twice                             | the literal in shell changes with the encoder                                                     | proposal          |
| no JVM: a missing java or mvn, or a failing driver, fails the round trip everywhere; FreeBSD decided on the first build                                                    | a skip written before the failure is seen hides where it should not apply                                                                                     | a skip with a log line where java is absent                                                                                                                                                               | an honest matrix                                          | a possible red first build                                                                        | owner, 2026-10-23 |
| arrow-adbc v1.12.0, bumped only with arrow-go                                                                                                                              | its requirements sit below the pins, so nothing else moves; the spec's one-release rule                                                                       | pinning the pins to adbc's lower versions                                                                                                                                                                 | no pin moves                                              | the vendor tree grows by otel, oauth2, gcache and x/sync                                          | proposal          |
| the dispatch property criterion is already met by `TestSplit`                                                                                                              | the tree shows it                                                                                                                                             | writing another                                                                                                                                                                                           | one line in the log's criteria                            | nothing                                                                                           | tree              |

## Knowledge

- Commit 2: the package comment of `internal/qdbtest/flight` carries the
  cycle reason; `Execute` carries the batch-size reason; `Same` the
  null-slot reason; `internal/AGENTS.md` Tests the fixture row (Rationale:
  fixture).
- Commit 3: `internal/flightsql/AGENTS.md` Tests names the fixture;
  `TestStatement`'s comment loses the later-unit sentence.
- Commit 4: the `server` type and `newServerOn` comments carry why the
  Flight listener starts beside the handler (Rationale: flight in the
  round trip, as the ADR's decision).
- Commit 5: `checkFlight` carries the token reason (Rationale: token) and
  the oracle reason with its rejected alternative (Rationale: oracle);
  `internal/httpapi/AGENTS.md` Tests the round trip row; ADR-0018 Decision
  14 the Flight-in-the-property sentence is already there.
- Commit 6: `internal/AGENTS.md` Building the arrow-adbc bullet
  (Rationale: arrow-adbc).
- Commit 7: `adbc`'s overview carries the handshake rule and the output
  reason (Rationale: adbc output); `tests/e2e/AGENTS.md` the output
  bullet.
- Commit 8: `adbc_back` and `adbc_types` carry the step reasons
  (Rationale: smoke steps, smoke assertions, types query); `docs/e2e.md`
  step 7 and the smoke section; ADR-0018 Decision 14 edited; the decision
  log rows for smoke steps and smoke assertions.
- Commit 9: the pom's comments carry the jar version, shade and release
  reasons (Rationale: maven, jdbc run, jar version); `Smoke`'s overview
  the URL and anonymous reasons; the Makefile comment Maven as resolver
  and cache; `docs/e2e.md` the Maven paragraph and the decision log rows
  for maven and jdbc run; `tests/e2e/AGENTS.md` the Maven bullet;
  `README.md` the prerequisites.
- Commit 10: `jdbc_back` and `jdbc_types` carry the `--add-opens` and
  the column-order reasons; `docs/e2e.md` the no-JVM rule (Rationale: no
  JVM) and its decision log row.
- Plan deletion commit: `docs/log.md` Current state: M5 in flight lists
  the tests as landed, Next drops them, the criteria line drops the
  dispatch property as outstanding (Rationale: dispatch) and the Handoff
  stays; the log entry.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from the Design and Knowledge sections in the same commit as the code
and reconciles the code against this plan before the build-stage message.
Specific to this unit:

- Commit 9 is the first to run Maven here; if the JDBC driver's URL
  properties or the `--add-opens` flag behave differently from the docs
  page on JDK 21 locally, the pom and `Smoke` comments say what was
  observed, not what the page said.
- Commit 8's `adbc_types` expected cells are copied from
  `TestDuckDBScalars`; if the CSV encoder's output differs from the JSON
  cells (quoting), the executor pins what the encoder writes and says so
  in `tests/e2e/AGENTS.md`.
- The first Buildkite build decides the FreeBSD JDBC question; the
  finding goes to the owner before any skip is written.

## Commits

1. `docs(plan): flight-tests-plan.md, the Flight client in the equivalence property and the ADBC and JDBC smoke steps of the round trip` (this document)
2. `test(qdbtest): the flight fixture dials a Flight SQL server and reads a statement or an IPC body into one batch`
3. `refactor(flightsql): the Flight tests read statements through the flight fixture`
4. `test(httpapi): the round trip's server starts the Flight listener beside the handler`
5. `test(httpapi): the round trip reads every table over Flight on both engines, equal to the query route's Arrow body`
6. `build(vendor): arrow-adbc's Go Flight SQL driver at v1.12.0`
7. `test(e2e): e2etool adbc runs a statement through arrow-adbc's Flight SQL driver and writes it through a server encoder`
8. `test(e2e): the round trip reads each table back over ADBC and runs a #sql query of DuckDB-only types through it`
9. `test(e2e): the JDBC smoke is a Maven project that shades Arrow's Flight SQL JDBC driver into one jar`
10. `test(e2e): the round trip reads each table back over JDBC and runs the types query through it`
11. `/doc-discipline all docs/flight-tests-plan.md internal/qdbtest/flight internal/flightsql internal/httpapi tests/e2e`
12. `/doc-discipline check docs/flight-tests-plan.md`
13. `docs(log): flight-tests-plan.md deleted, facts moved to docs/e2e.md, ADR-0018, internal/AGENTS.md, internal/flightsql/AGENTS.md, internal/httpapi/AGENTS.md, tests/e2e/AGENTS.md and the comments of internal/qdbtest/flight, internal/httpapi, tests/e2e`
14. Verify: push `sc-19567/rr-flight-tests`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Maven and a JDK 17+ on the eight agents are unverified from this repository. Recommendation: the first build answers; a missing one is a red step brought to the owner, not a skip.
2. Whether the FreeBSD agents run the JDBC driver. Recommendation: decide on the first build's result, as the spec allows.
3. The qdb-duck ATTACH story stays on the owner's word, outside this unit.
