# Plan: flight statement -- the statement query on the Flight listener

Status: draft

This plan answers the first Next item of `docs/log.md`, Current state,
whole: `GetFlightInfo` for a statement, `DoGet` through `query.Run`,
and the status rows of ADR-0018, Decision 13, for a running query. The
owner added one piece during the seed of 2026-10-21: the native query
result is split into bounded batches inside the query core, so every
consumer of a result reads batches of one size. It is written for an
executor who has none of that seed conversation. Everything the build
needs is here.

## 1. Outcome

When the unit lands:

- A Flight SQL client sends its query text and gets back one
  `FlightInfo` with one endpoint, no location, no schema and unknown
  totals, whose ticket carries the query text unchanged.
- The client presents the ticket on `DoGet` and receives the result as
  an Arrow IPC stream of record batches: the schema message, one
  message per batch, no trailing marker. A plain query runs on the
  native engine and a `#sql` query on the caller's DuckDB instance,
  through `query.Run`, as `POST /api/v2/query` runs them. A statement
  without a result set streams a schema with no fields and no batch.
- An engine's own error (a parse, bind or execution error of either
  engine, an unknown table) is `INVALID_ARGUMENT` with the engine's
  message. An open breaker, an unreachable cluster, a failed DuckDB
  attach and a full DuckDB instance cap are `UNAVAILABLE` with the
  error's message. A token whose credentials the secure cluster refuses
  is `UNAUTHENTICATED`. A caller that went away is `CANCELED`. A
  failure after the first message closes the stream with `INTERNAL`,
  or `CANCELED` when the caller left, and one warning line. Nothing is
  retried once the stream has started.
- The login and the statement map a cluster error to a gRPC status
  through one function.
- An open breaker and a full instance cap carry their retry-after as a
  `RetryInfo` detail on the `UNAVAILABLE` status, the gRPC twin of the
  HTTP `Retry-After` header, next to the message.
- `Cluster.Query` hands its result to the sink as slices of
  `model.BatchRows` rows, 65536, the one batch-size constant of the
  binary. The bulk reader fetches that many rows per batch through the
  same constant. Every result in the binary is a sequence of bounded
  batches, and the encoders are written for that alone: `Encoder` has
  one method, `EncodeStream`, each encoder checks the ctx once per
  batch, and the Arrow encoder writes each batch as it comes. The e2e
  tool writes a generated body as a sequence of one batch. The JSON body
  of a native result larger than one batch is an array of several
  results, one per slice, as the reader's and DuckDB's bodies are.
- One property test in `internal/flightsql` reads a generated table
  back over Flight on both engines and both clusters, and one error-row
  table pins the codes.

Left for later units, each named:

- `GetSqlInfo` on arrow-go's registered map, then the catalog commands
  on a table list core in `internal/qdb` (`docs/log.md`, Next);
- the format-equivalence property in `internal/httpapi` extended with
  a Flight SQL client, the dispatch property over generated prefixes,
  and the ADBC and JDBC smoke tests (`docs/e2e.md`, The Flight SQL
  smoke tests);
- query capture and monitoring on both planes, in M6 (`docs/log.md`,
  Handoff).

## 2. Verified facts

1. The statement RPCs are unimplemented: `flightsql.BaseServer`
   answers `UNIMPLEMENTED` for `GetFlightInfoStatement` and
   `DoGetStatement`, and the package's tests pin that
   [`vendor/github.com/apache/arrow-go/v18/arrow/flight/flightsql/server.go:316-335`,
   `internal/flightsql/flightsql_test.go:154-159,209-213`].
2. arrow-go's router decodes a `DoGet` ticket from a protobuf `Any`,
   dispatches on its type, and for a statement calls `DoGetStatement`
   with the stream's context; on success it writes the schema and each
   chunk of the answered channel through `flight.NewRecordWriter` and
   releases each chunk after writing
   [`vendor/.../flightsql/server.go:874-917`].
   `flightsql.GetStatementQueryTicket` performs that decode for a
   statement ticket alone and answers an error for any other ticket
   [`vendor/.../flightsql/server.go:83-96`]. `CreateStatementQueryTicket`
   builds the ticket bytes from a handle [`vendor/.../flightsql/types.go:79-85`].
3. `flightsql.NewFlightServer` answers a `flight.FlightServer`, which
   is the generated `FlightServiceServer` interface, whose `DoGet` is
   `DoGet(*flight.Ticket, flight.FlightService_DoGetServer) error`
   [`vendor/.../flightsql/server.go:724-726`,
   `vendor/github.com/apache/arrow-go/v18/arrow/flight/gen/flight/Flight_grpc.pb.go:367-423`].
   A struct that embeds the returned value and declares `DoGet`
   satisfies the interface, and `flight.RegisterFlightServiceServer`
   takes it [`internal/flightsql/server.go:63`].
4. `flight.Writer` embeds `*ipc.Writer`, writes one `FlightData`
   message per record batch, and `NewRecordWriter(stream, opts)` builds
   it over the server stream
   [`vendor/github.com/apache/arrow-go/v18/arrow/flight/record_batch_writer.go:62-100`].
   `ipc.Writer.Close` writes the schema when nothing was written yet
   and no end marker on a payload writer, so a result of no batches
   reaches the client as one schema message
   [`vendor/github.com/apache/arrow-go/v18/arrow/ipc/writer.go:157-171,321-335`].
   arrow-go's client reader needs that first message
   [`vendor/github.com/apache/arrow-go/v18/arrow/flight/record_batch_reader.go:155-175`].
5. `FlightInfo` carries `Schema`, `FlightDescriptor`, `Endpoint`,
   `TotalRecords` and `TotalBytes`; a `FlightEndpoint` carries a
   `Ticket` and `Location`s
   [`vendor/github.com/apache/arrow-go/v18/arrow/flight/gen/flight/Flight.pb.go:973-1226`].
   An endpoint with no location means the same server, which is how
   arrow-go's client and the stock drivers read it; `TotalRecords` and
   `TotalBytes` of -1 mean unknown [the Flight protocol's
   `Flight.proto` comments on those fields, in the same generated file].
6. grpc-go refuses a message over its default receive limit, on the
   client and on the server, and the limit is the constant those two
   files declare [`vendor/google.golang.org/grpc/clientconn.go:139`,
   `vendor/google.golang.org/grpc/server.go:61`], so a native result
   must reach the Flight writer in bounded batches.
7. `Cluster.Query` fetches the whole result as one batch on a leased
   session, returns the session, stands in an empty batch with no
   columns for a statement without a result set, runs the sink over the
   one batch and releases the batch after the sink returns; under
   `WithReadRetry` it retries once before the sink
   [`internal/qdb/cluster.go:390-432`]. `query.Run` calls it with
   `WithReadRetry` for a native query and `Cluster.Duck` for a `#sql`
   query, and every error other than the sink's own arrives before the
   sink runs [`internal/query/query.go:58-75`].
8. A DuckDB result is a sequence of batches lent until yield returns;
   go-duckdb's reader releases its reference to the current batch when
   `Next` fetches the next chunk, converts one DuckDB data chunk per
   record, and destroys the chunk right after the conversion, so the
   batch owns its buffers; the reader exposes no batch size, and a
   DuckDB chunk is one vector
   [`internal/olap/instance.go:156-220`,
   `vendor/github.com/duckdb/duckdb-go/v2/arrow.go:248-285`].
9. `arrow.RecordBatch.NewSlice` answers a batch sharing the parent's
   buffers and retaining the parent, released by the caller
   [`internal/encoding/arrow.go:36-50`, which relies on it;
   `vendor/github.com/apache/arrow-go/v18/arrow/array/record.go`,
   `simpleRecord.NewSlice`].
10. The bulk reader fetches `readBatchRows` rows per batch, 65536, a
    constant declared equal to the encoders' chunk so one fetch is one
    record batch on the Arrow wire [`internal/qdb/read.go:17-20`]. The
    encoders' `chunkRows` is 65536, the record batch size on the Arrow
    wire and the stride between ctx checks on the text wires
    [`internal/encoding/encoding.go:44-56`, `internal/encoding/AGENTS.md`,
    "Every encoder checks the ctx"]. The Arrow encoder slices every
    batch of a stream into `chunkRows` rows
    [`internal/encoding/arrow.go:34-97`], and `checkArrow` in its tests
    pins the batch count of the one-shot `Encode`
    [`internal/encoding/encode_test.go:437-462`].
11. `internal/encoding` imports `internal/model` and not
    `internal/qdb`; `internal/qdb` imports `model` and `olap`; `model`
    imports neither and holds what two packages share
    [non-test imports, session of 2026-10-21;
    `internal/model/AGENTS.md`].
12. The HTTP query handler runs `query.Run` with a sink that sets the
    content type and runs `EncodeStream`, keeps the sink's error apart
    from the run's, answers a stream failure with zero bytes out as
    500, logs a cut stream after the first byte, and maps the run's
    error through `writeClusterError` at 400
    [`internal/httpapi/query.go:106-162`]. `writeClusterError` orders
    its cases: the caller's context ended, `ErrCredentialsRefused`
    before the unreachable case because a refused attach would
    otherwise count as unreachable, `*BreakerOpenError`,
    `*InstanceLimitError`, `IsClusterUnavailable`, then the status
    passed in [`internal/httpapi/problem.go:71-104`]. The user of a
    call is derived from the claims on the ctx
    [`internal/httpapi/tables.go:76-79`].
13. The Flight login maps `Cluster.Authenticate`'s outcomes in one
    switch: a caller that went away is `CANCELED` by `ctx.Err()`, a
    refused credential `UNAUTHENTICATED`, an open breaker or an
    unreachable cluster `UNAVAILABLE` with the error's message, anything
    else `INTERNAL`; its comment says the statement decides the detail
    text [`internal/flightsql/auth.go:104-147`]. The auth interceptors
    place the claims on the ctx, zero claims for the anonymous user,
    and the stream handlers receive a `wrappedStream` whose context is
    that ctx [`internal/flightsql/auth.go:97-101,207-227`,
    `internal/flightsql/server.go:114-121`].
14. A refused credential on a data call is `qdb.ErrCredentialsRefused`
    on the secure cluster, whether a user pool, the login or a failed
    DuckDB attach dialed [`internal/AGENTS.md`, Code, the refusal bullet;
    `internal/qdb/cluster.go:164`]. A full instance cap is
    `*qdb.InstanceLimitError` [`internal/qdb/cluster.go:478-489`], a
    failed attach is `olap.ErrAttach` wrapped, which
    `IsClusterUnavailable` does not recognize
    [`internal/olap/instance.go:45-48`, `internal/httpapi/AGENTS.md`,
    "a failed attach is 503 without it"].
15. `status.Code(err)` answers `OK` for nil and `Unknown` for a plain
    error, and `status.FromContextError` maps a context error to
    `CANCELED` or `DEADLINE_EXCEEDED`
    [`vendor/google.golang.org/grpc/status/status.go:139-160`].
16. The package's tests start the server on a port of its own over the
    ctx main would give it, dial arrow-go's Flight SQL client with
    verification off, run on both clusters through `cluster.Each`, mint
    tokens directly, and `newServerOn` takes any cluster
    [`internal/flightsql/flightsql_test.go:60-131`]. An unreachable
    cluster is `qdb.New` over `qdb://127.0.0.1:1` with
    `Pool.Breaker.Failures` of one [`internal/httpapi/login_test.go:112-118`].
    arrow-go's client `Execute` answers the `FlightInfo` and `DoGet`
    answers a `flight.Reader` over the stream
    [`vendor/.../flightsql/client.go:132-135,365-372`].
17. The table fixture draws a table, creates it as a user, answers its
    native `Select` and its DuckDB `SelectSQL`, its `Range`, and checks
    a batch column by column with `Check`
    [`internal/qdbtest/table/table.go:205-213,228-250,269,352-358`].
    The round trip compares a DuckDB Arrow body decoded, because
    DuckDB leaves the bytes under a null slot undefined
    [`internal/httpapi/AGENTS.md`, Tests].
18. ADR-0018, Decision 5, says `Cluster.Query` hands its one batch to
    the sink as a sequence of one batch so every result streams through
    `EncodeStream` and the Flight server has one `DoGet` body
    [`docs/adr/0018-flight-sql.md:46-54`]; ADR-0017, Decision 1, says
    the DuckDB result streams through `EncodeStream` one vector at a
    time [`docs/adr/0017-embedded-duckdb.md:28-32`]. `internal/AGENTS.md`
    carries the one-batch rule in its query bullet [`internal/AGENTS.md:40-52`],
    and `internal/encoding/AGENTS.md` names `chunkRows` three times
    [`internal/encoding/AGENTS.md:64-66,82-89`].
19. `Encoder` has two methods, `Encode` for one batch and `EncodeStream`
    for a sequence. Outside tests, `Encode` has two callers, both in the
    e2e tool writing a generated body [`tests/e2e/tools/e2etool/main.go:196,319`];
    the server's routes call `EncodeStream` alone
    [`internal/httpapi/query.go:142`, `internal/httpapi/read.go:78`]. The
    four `Encode` methods, the nil-batch rule of the `Encoder` doc, the
    nil-safe `numRows` and the Arrow encoder's `emptyBatch` exist for
    that path [`internal/encoding/encoding.go:24-34,58-64`,
    `internal/encoding/arrow.go:27-32,69-71`, `internal/encoding/csv.go:198-216`,
    `internal/encoding/json.go:244-254,321-331`]. The text encoders check
    the ctx inside their row loops through `checkChunk` at six sites
    [`internal/encoding/csv.go:172,425`, `internal/encoding/json.go:282,405,663,928`].
    The test helpers over `Encode` are `encode` in the encoding tests and
    `direct` in the round trip [`internal/encoding/encoding_test.go:49-56`,
    `internal/httpapi/roundtrip_test.go:52-58`].
20. The JSON stream is a top-level array with one columnar result per
    batch [`internal/encoding/json.go:334-340`, ADR-0010, Decision on the
    JSON body, `docs/adr/0010-v2-query-request-negotiation-errors.md:45`].
    A native result is one batch today, so its array has one element
    whatever its size [fact 7].
21. gRPC's structured retry-after is a `RetryInfo` message attached to a
    status as a detail: `status.WithDetails` takes it, and the message
    type lives in `google.golang.org/genproto/googleapis/rpc/errdetails`,
    a package of a module `go.mod` already requires indirectly at
    `v0.0.0-20260526163538-3dc84a4a5aaa` and whose `status` package is
    vendored; `errdetails` is in the module cache at that version and
    enters `vendor/` with the first import through `go mod vendor`
    [`vendor/google.golang.org/grpc/internal/status/status.go:134`,
    `vendor/modules.txt:166-168`, `go.mod:46`,
    `$GOMODCACHE/google.golang.org/genproto/googleapis/rpc@v0.0.0-20260526163538-3dc84a4a5aaa/errdetails/error_details.pb.go:159-165`].
    Whether the ADBC and JDBC drivers surface a status detail is not
    verified; arrow-go's client answers the status as grpc-go does.
22. Audit of the one-batch assumption across the tree (owner's request,
    plan review of 2026-10-21): the places that assumed one batch or a
    nil batch are the `Encode` path and its helpers (fact 19), the Arrow
    encoder's slicing (fact 10), the per-row ctx stride (fact 19) and
    `Cluster.Query`'s `one` (fact 7). The three empty-result stand-ins
    (`Query`'s empty batch, `qdb.lent`'s and `olap.lent`'s) each shape
    the batch from a different schema source and stay as they are. The
    decoders read bodies of any batch count and are untouched. The HTTP
    handlers already stream a sequence and are untouched.

## 3. Design

### `internal/model/model.go`, after `SchemaOf`

```go
// BatchRows is the row count of one record batch between the layers: the
// slice Cluster.Query hands out, the bulk reader's fetch, and the stride
// between two ctx checks of a text encoder. [facts 7, 10; Rationale: one batch size]
// DuckDB's batches are smaller, one vector each, and pass through at their
// own size. [fact 8]
// Not measured. [Rationale: one batch size]
const BatchRows = 65536
```

`internal/model/AGENTS.md` gains one bullet under the existing ones:

```
- `BatchRows` is the one batch size of the binary. It lives here because
  `internal/qdb` produces batches of it and `internal/encoding` paces
  itself by it, and neither imports the other. [fact 11]
```

### `internal/qdb/cluster.go`, `one` becomes `slices`

```go
// slices is rec as a sequence of slices of at most n rows, in order. Each
// slice retains rec and is released after yield returns, so the receiver
// releases none. [fact 9; Batches doc, read.go:94-98]
// A rec of no rows is one empty slice, so the sequence is never empty. [Batches doc]
func slices(rec arrow.RecordBatch, n int64) Batches {
	// style: doc comment alone + inline note at the empty case
	// because: one loop over NewSlice, as writeSlices in internal/encoding was
	//
	// at the empty case: a rec of no rows yields rec itself once, because every
	//   receiver expects a batch and the Arrow encoder reads the first batch's
	//   schema [fact 7, step 2 of Query]
	// at the slice: the slice is released after yield returns whether the
	//   receiver broke out or not, as lent in read.go releases [read.go:107-114]
}
```

`Query`'s doc comment and overview change in two claims:

```go
// Query runs q as u and hands its result to sink as a sequence of slices of
// at most model.BatchRows rows. [Rationale: one batch size]
// ... (the rest as today: the session is back before sink runs, every error
// other than sink's own is returned before sink, a statement with no result
// set yields one empty batch with no columns, WithReadRetry) [fact 7]
func (c *Cluster) Query(...) error {
	// 3. run sink over the slices and release the batch afterwards. The slices
	//    share the batch's buffers, and each retains it, so a sink that keeps a
	//    slice keeps the buffers [fact 9]
	//    - the batch is sliced here and not in the encoder, so every consumer,
	//      HTTP and Flight, reads batches of one size and a Flight message
	//      stays under the gRPC limit [fact 6; Rationale: one batch size]
}
```

### `internal/qdb/read.go`

`readBatchRows` goes; `readerOptions` uses `model.BatchRows`, and the
`ReadOptions` doc says "A zero BatchRows means `model.BatchRows`"
[fact 10]. `slices` is white-box tested in `cluster_test.go` (section
3, Tests).

### `internal/encoding/encoding.go`

`chunkRows`, `checkChunk` and `numRows` go. The `Encoder` interface
keeps one method:

```go
// Encoder writes a sequence of record batches to w in one wire format. It
// does not release a batch. It returns the first error it meets: a write
// error, the error of an error element, or the ctx ending between batches. [fact 19; Rationale: one method]
type Encoder interface {
	// ContentType is the media type the handler answers with.
	ContentType() string
	// EncodeStream writes a sequence of batches as one body. Every batch shares
	// the first batch's schema. A batch is valid only until yield returns. The
	// sequence is never empty, because every producer stands in an empty batch
	// of its schema for a result of no rows. [fact 7, fact 8, read.go:94-98]
	EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error
}
```

The CSV, NDJSON and JSON `EncodeStream` bodies each gain one ctx check
at the top of the per-batch loop and lose their `checkChunk` calls,
with a step comment: a client that left is noticed at the next batch,
and a batch is at most `model.BatchRows` rows [fact 19; Rationale: one
batch size]. Their `Encode` methods go. The `Encode` helper of the
encoding tests and `direct` in the round trip call `EncodeStream` over
a one-element sequence, built by a three-line `one` helper in each test
file [fact 19].

`tests/e2e/tools/e2etool/main.go`: `writeBody` and the CSV write of
`tocsv` call `EncodeStream` over a one-element sequence, through one
`one` helper in that file, with a comment: the tool writes one batch,
and the encoders write sequences alone [fact 19].

### `internal/encoding/arrow.go`

`writeSlices`, `writeArrow`, `emptyBatch` and `Encode` go.
`EncodeStream` becomes:

```go
// EncodeStream implements Encoder. The stream opens on the first batch's
// schema and carries every batch as one record batch, so the producer's
// batch size is the wire's. [Rationale: one batch size]
func (Arrow) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	// style: doc comment + numbered overview + step comments
	// because: a lazy open and two ways to end
	//
	// 1. open the writer on the first batch, because an IPC stream carries one
	//    schema and it is known only when the first batch arrives [arrow.go:75-85]
	// 2. write each batch whole, with a ctx check per batch, because the
	//    producer already bounds a batch at model.BatchRows [facts 7, 8, 10]
	//    - rejected: slicing here, because the slice is the query core's now
	//      and the text encoders never sliced [Rationale: one batch size]
	// 3. Close writes the end-of-stream marker; a sequence of no batch cannot
	//    occur, because every producer yields at least one [Encoder doc]
}
```

`internal/encoding/AGENTS.md`: the first bullet's "`Encode` writes one
batch: the bodies the e2e tool and the fixture write. `EncodeStream`
writes every response the server sends" becomes "`EncodeStream` is the
one entry point; the e2e tool writes a generated body as a sequence of
one batch"; the ctx bullet says "once per batch, and a batch is at
most `model.BatchRows` rows, the binary's batch size"; the Arrow bullet
says "in the record batches the producer hands it, which `Cluster.Query`
and the bulk reader bound at `model.BatchRows` rows and DuckDB at one
vector, without in-format buffer compression", and the "nil batch
encodes as" and "writes every batch in `chunkRows` slices" sentences
go; the JSON bullet's "A nil batch encodes as `{"columns":[]}`" goes,
and its "one per batch" sentence gains "a native result larger than one
batch is several results" [facts 18, 19, 20].

### `internal/flightsql/status.go`, new file

```go
// statusOf maps a failed cluster call to the gRPC status by who failed, as
// writeClusterError maps it on HTTP. answered is the code for anything the
// engine or the cluster answered: UNAUTHENTICATED for a login and
// INVALID_ARGUMENT for a query. A nil err is nil. [ADR-0018, Decision 13; fact 12; Rationale: one mapping]
func statusOf(ctx context.Context, err error, answered codes.Code) error {
	// style: doc comment + numbered overview + step comments
	// because: a precedence of seven outcomes, each a row of ADR-0018, Decision 13
	//
	// 1. the caller's ctx ended: the context's own code, CANCELED or
	//    DEADLINE_EXCEEDED, because the failure is the caller's and nothing of
	//    the cluster's is reported [fact 13, fact 15]
	// 2. a refused credential: UNAUTHENTICATED, before the unreachable case,
	//    because a refused attach would otherwise count as unreachable [fact 12]
	// 3. an open breaker and a full instance cap: UNAVAILABLE with the error's
	//    message and a RetryInfo detail carrying RetryAfter, the gRPC twin of the
	//    Retry-After header the HTTP route sets [fact 14, fact 21; Rationale:
	//    the retry-after]
	//    - a WithDetails failure cannot happen for a well-formed duration; if it
	//      does, the status goes out without the detail [fact 21]
	// 4. an unreachable cluster and a failed attach: UNAVAILABLE with the error's
	//    message and no detail, as the HTTP route sets no header for them
	//    [fact 12, fact 14]
	// 5. anything else: answered, with the error's message, because the engine
	//    or the cluster spoke and the message is theirs [ADR-0018, Decision 13]
}
```

`statusOf` imports `google.golang.org/genproto/googleapis/rpc/errdetails`,
and the commit that adds it runs `go mod vendor`, which adds that one
package under the already vendored module [fact 21].

`login` (auth.go) keeps its overview and replaces its switch at step 2
by `statusOf(ctx, err, codes.Unauthenticated)`; its step comment says
the refusal is the caller's and the rest is `statusOf`'s rule, and the
sentence deferring the detail text to the statement goes [fact 13].

### `internal/flightsql/statement.go`, new file

```go
// GetFlightInfoStatement answers one endpoint whose ticket is the query text,
// unchanged. The info carries no schema, because the native engine does not
// know a result's columns before running the query, and no location, which
// means this server. [ADR-0018, Decision 9; fact 5]
func (s *Server) GetFlightInfoStatement(ctx context.Context, q flightsql.StatementQuery, desc *flight.FlightDescriptor) (*flight.FlightInfo, error) {
	// style: doc comment alone + inline note at the ticket
	// because: one ticket built and one message filled
	//
	// at the ticket: the text travels verbatim and DoGet dispatches it, so the
	//   #sql rule has one home, in query.Run; a mint failure is INTERNAL
	//   [owner, 2026-10-21; ADR-0018, Decision 13]
	// at the totals: -1 for records and bytes, because the server stores nothing
	//   and runs nothing here [fact 5; ADR-0018, Decision 9]
	// at the descriptor: echoed, as arrow-go's examples do [fact 5]
}
```

```go
// service is the Flight RPC service the gRPC server registers: arrow-go's
// router from Flight RPCs to Flight SQL commands, with DoGet overridden for
// a statement ticket. [fact 2, fact 3; Rationale: the DoGet override]
type service struct {
	flight.FlightServer
	s *Server
}
```

```go
// DoGet streams the result of a statement ticket, and hands every other
// ticket to arrow-go's router. [fact 2; Rationale: the DoGet override]
func (v *service) DoGet(t *flight.Ticket, stream flight.FlightService_DoGetServer) error {
	// style: doc comment + numbered overview + step comments
	// because: one decode, one run inside a sink, and two kinds of failure that
	//          answer differently, the HTTP handler's shape
	//
	// The query runs inside the sink and writes to the stream while each batch
	// is lent, so no batch is copied, retained or crossed to another goroutine
	// [facts 7, 8; Rationale: the DoGet override]
	//
	// 1. decode: GetStatementQueryTicket answers an error for any ticket that
	//    is not a statement, and that ticket is the router's [fact 2]
	// 2. the user from the claims the auth edge placed, zero claims for the
	//    anonymous user, as the HTTP handlers derive it [fact 12, fact 13]
	// 3. run through query.Run, which retries the native engine once before the
	//    sink and DuckDB never, so this method retries nothing [fact 7]
	//    the sink opens the Flight writer on the first batch's schema and writes
	//    each batch as one message; a ctx check per batch, because a client that
	//    left is noticed at the next batch [fact 4]
	//    Close after the last batch, because Close writes the schema when no
	//    batch did, so an empty result reaches the client too [fact 4]
	// 4. the stream failed: INTERNAL, or the ctx's own code when the caller
	//    left, and one warning line with the error, because the client sees
	//    only the closing status [owner, 2026-10-21; fact 12]
	// 5. the run failed before the sink: statusOf with INVALID_ARGUMENT for what
	//    an engine answered [ADR-0018, Decision 13]
}
```

`server.go`, `New`, step 3: registers `&service{FlightServer:
flightsql.NewFlightServer(s), s: s}` in place of the bare router, and
its step comment says why: the statement's `DoGet` writes inside the
sink and every other RPC stays the router's [Rationale: the DoGet
override]. The package doc comment's "every command it does not
implement answers UNIMPLEMENTED" stays true.

`internal/flightsql/AGENTS.md` gains a section between Auth and Tests:

```
## Statement

- GetFlightInfo for a statement answers one endpoint whose ticket is the
  query text verbatim, with no schema, no location and unknown totals;
  it runs nothing and keeps nothing. `GetFlightInfoStatement` says why
  the text travels unchanged. [ADR-0018, Decision 9; owner, 2026-10-21]
- DoGet for a statement ticket is this package's own method on the
  registered service, and every other ticket goes to arrow-go's router.
  The query runs through `query.Run` inside the sink, and the sink writes
  each lent batch to the stream through arrow-go's Flight writer, as the
  HTTP handler encodes inside its sink. `DoGet` says why the hook with a
  channel was rejected. A statement is never retried once the stream has
  started. [Rationale: the DoGet override]
- A batch on the Flight wire is the producer's batch: `model.BatchRows`
  rows from the native engine and the bulk reader, one vector from
  DuckDB, so one message stays under gRPC's receive limit. [fact 6;
  Rationale: one batch size]
- The codes of a running query (ADR-0018, Decision 13) are `statusOf`'s,
  which the login shares: the engine's error is INVALID_ARGUMENT with
  its message, an open breaker and a full instance cap are UNAVAILABLE
  with the error's message and a `RetryInfo` detail carrying the
  retry-after, an unreachable cluster and a failed attach are
  UNAVAILABLE with the message alone, a refused credential is
  UNAUTHENTICATED, the caller's own ctx ending is its code. A failure
  after the first message closes the stream with INTERNAL, or the ctx's
  code when the caller left, and one warning line. [Rationale: one
  mapping, the retry-after; owner, 2026-10-21]
```

The Tests section's first bullet gains: "`TestStatement` draws a table
and reads it back over Flight on both engines, and `TestStatementRows`
is the error table; the statement's rows that reach no cluster run on
the insecure cluster."

### `internal/qdb/cluster_test.go`, `TestSlices`

```go
// TestSlices: for a batch of any row count and any n, slices yields
// ceil(rows/n) slices of at most n rows, at least one, in order, each equal
// to the parent's rows at that offset, and releases each after yield. [fact 9]
func TestSlices(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: one property with a release check
	//
	// 1. draw rows and n with rapid, build a one-column int64 batch of rows [fact 17's draw style]
	// 2. collect the slices' row counts and values through the sequence
	// 3. compare counts and values; a batch of no rows yields one empty slice
	//    [slices doc]
	// 4. the parent's refcount after the sequence is what it was before, which
	//    proves every slice was released [fact 9]
}
```

It dials nothing. `checkArrow` in `internal/encoding/encode_test.go`
drops its `batchRows` argument and batch-count assertion and runs
`EncodeStream` over one batch, because the Arrow encoder writes a batch
whole, and `TestRoundTrip`'s doc drops "across Arrow batch boundaries"
[fact 10].

### `internal/flightsql/flightsql_test.go`

The two rows that expect `UNIMPLEMENTED` for `select 1` [fact 1] expect
`OK` instead: `Execute` answers an info, and `DoGet` on its ticket reads
to the end with no error.

```go
// TestStatement: for a generated table on each cluster, the native query
// read back over Flight equals the rows generated, column by column, and the
// #sql query read back over Flight equals the batches Cluster.Duck answers
// directly, decoded. [facts 16, 17; Rationale: the package's own property]
func TestStatement(t *testing.T) {
	// style: doc comment + numbered overview + step comments
	// because: two engines, one oracle each, both clusters
	//
	// 1. one server and client per cluster, a bearer from a Handshake, because
	//    the statement runs as the token's user [fact 16]
	// 2. rapid: draw and create a table as the fixture's user; the ranged
	//    Select is not needed, because a query reads no bulk reader [fact 17,
	//    internal/AGENTS.md Tests, the Range rule applies to reads]
	// 3. native: Execute(tbl.Select()), DoGet on the one endpoint's ticket,
	//    read every batch into one table, Check against tbl [fact 17]
	// 4. #sql: Execute("#sql " + tbl.SelectSQL()), read every batch, compare
	//    decoded with Cluster.Duck's batches over the same SQL, because the
	//    bytes under a null slot are undefined [fact 17]
	// 5. an empty result: Execute of a query with no result set streams one
	//    schema and no batch, and the reader reaches the end with no error
	//    [fact 4]
}
```

```go
// TestStatementRows: every way a statement fails before the stream answers
// its code with the engine's or the cluster's message. [ADR-0018, Decision 13]
func TestStatementRows(t *testing.T) {
	// style: doc comment + inline note at the unreachable row
	// because: one table of rows, as TestBearerRows is
	//
	// the rows: a native parse error and a #sql parse error are INVALID_ARGUMENT
	//   with the engine's message; a query naming an unknown table is
	//   INVALID_ARGUMENT; a DoGet on a hand-built ticket of a bad query is the
	//   same, because the ticket is the text [owner, 2026-10-21]
	// at the unreachable row: a server over qdb://127.0.0.1:1 answers
	//   UNAVAILABLE with no detail, and once the breaker opens, UNAVAILABLE with
	//   a RetryInfo detail whose delay is the breaker's [fact 16, fact 21;
	//   Rationale: the retry-after]
	// the rows run on the insecure cluster, because none reaches a secured
	//   check [internal/flightsql/AGENTS.md, Tests]
}
```

### `docs/adr/0018-flight-sql.md`, Decision 5, edited in place

The sentence "`Cluster.Query` hands its one batch to the sink as a
sequence of one batch, so every result in the binary streams through
`EncodeStream` and the Flight server has one `DoGet` body" becomes:
"`Cluster.Query` hands its result to the sink as batches of the one
batch size, as the bulk reader and DuckDB do, so every consumer, the
HTTP encoders and the Flight server's `DoGet`, reads batches of one
shape." [Rationale: one batch size; `docs/AGENTS.md`, ADRs, edited in
place when the tree proves a detail wrong]

### `internal/AGENTS.md`, Code, the query bullet

"`Cluster.Query` hands it to a sink as a sequence of that one batch"
becomes "`Cluster.Query` hands it to a sink as slices of
`model.BatchRows` rows, each retaining the batch", and "a sink that
keeps the batch retains it" becomes "a sink that keeps a slice keeps
the buffers". The read bullet's "one per fetch" stays, with "of
`model.BatchRows` rows" added. The query dispatcher bullet's "and the
Flight server will" becomes "and the Flight server's `DoGet` does".

## 4. Rationale

| decision                                                                                                                                                                                                               | why                                                                                                                                                                                             | rejected, and why                                                                                                                                                                                                                                                                                                                            | gained                                                                                                       | given up                                                                                                                                                                                                                                                                                                                                                                                                                           | settled by                   |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| The DoGet override: `DoGet` is this package's method for a statement ticket, and the sink writes to the stream while each batch is lent                                                                                | the plain mechanism: one function, no goroutine, no channel, the HTTP handler's shape; both engines lend batches and the override honors the lending as it is                                   | arrow-go's `DoGetStatement` hook, which answers a schema and a channel the router drains: correct without a copy, because batches are refcounted and a Retain keeps one alive, but it needs a producer goroutine, a schema handoff before the first chunk, ctx-aware sends, and the error-before-first-byte split carried across the channel | one function a cold reader checks against the HTTP handler                                                   | a depth-one overlap between DuckDB's chunk conversion and gRPC serialization that the hook's goroutine would give; the ticket decode is called here as well as in the router                                                                                                                                                                                                                                                       | owner, 2026-10-21            |
| The ticket is the query text verbatim, dispatched at `DoGet`                                                                                                                                                           | the prefix rule has one home, `query.Run`, and a readable ticket                                                                                                                                | splitting the prefix at `GetFlightInfo` and encoding engine plus text: a ticket format of this server's own, and the split exported                                                                                                                                                                                                          | one rule, one home                                                                                           | a client may craft a ticket without `GetFlightInfo`, which is harmless because the user is the `DoGet` call's token                                                                                                                                                                                                                                                                                                                | owner, 2026-10-21            |
| One mapping, `statusOf`, for the login and the statement                                                                                                                                                               | the rows are ADR-0018, Decision 13's, stated once; the login's comment already deferred the detail text to the statement                                                                        | two switches side by side: the rows stated twice, a new row landing in both                                                                                                                                                                                                                                                                  | one place a new code lands                                                                                   | nothing                                                                                                                                                                                                                                                                                                                                                                                                                            | owner, 2026-10-21            |
| The retry-after of an open breaker and a full instance cap travels as a `RetryInfo` status detail next to the message                                                                                                  | the owner does not want the retry-after lost on the Flight plane, and `RetryInfo` is gRPC's standard carrier, the twin of the HTTP header                                                       | the message alone: a client parses the value out of prose, and the Flight plane carries less than HTTP                                                                                                                                                                                                                                       | the same information on both planes, in each plane's own form                                                | one vendored package from an already required module, and whether the ADBC and JDBC drivers surface the detail is unverified                                                                                                                                                                                                                                                                                                       | owner, 2026-10-21            |
| `Encoder` has one method, `EncodeStream`                                                                                                                                                                               | every result is a bounded sequence, so a one-batch entry point is a second way to write the same bytes, with a nil-batch rule and two helpers of its own                                        | keeping `Encode` as a convenience for the e2e tool: four methods and their rules kept for two callers that can wrap a batch in a one-element sequence                                                                                                                                                                                        | about a hundred lines gone, one path per encoder, the nil-batch rule gone                                    | the e2e tool and two test helpers wrap a batch themselves                                                                                                                                                                                                                                                                                                                                                                          | owner, 2026-10-21            |
| Each encoder checks the ctx once per batch                                                                                                                                                                             | a batch is at most `model.BatchRows` rows, which was the stride of the per-row check, so a check per batch notices a departed client as soon                                                    | the per-row stride kept: a second expression of the batch size inside six row loops                                                                                                                                                                                                                                                          | `checkChunk` and six call sites gone                                                                         | a DuckDB batch of one vector is checked more often than before, which costs one `ctx.Err` per vector                                                                                                                                                                                                                                                                                                                               | proposal                     |
| INTERNAL for a failure after the first message, of either kind, and the ctx's code when the caller left                                                                                                                | the client has already received data, so the closing status says the server failed, and Decision 13 names INTERNAL for an encoding failure                                                      | INVALID_ARGUMENT for a DuckDB reader error mid-stream: the engine spoke, but the client cannot act on it after data arrived                                                                                                                                                                                                                  | one rule for a cut stream                                                                                    | the engine's mid-stream message reaches the client as INTERNAL's detail, not as its own code                                                                                                                                                                                                                                                                                                                                       | owner, 2026-10-21            |
| One batch size, `model.BatchRows`, 65536: `Cluster.Query` slices the native result, the bulk reader fetches it, the text encoders pace by it, and the Arrow encoder and the Flight writer write each batch as it comes | the owner wants every consumer of a result to read batches of one shape, and the Flight message must stay under gRPC's limit; `readBatchRows` and `chunkRows` already declared themselves equal | leaving the native result as one batch with the slicing in the encoder, shared with the Flight writer through one generalization: two producers of batch size and a loop the Flight path has to borrow; 2048 to match DuckDB: 32 times the messages per native result                                                                        | one constant, no slicing loop in any consumer, the IPC stream of a sliced batch is byte-identical to today's | no memory saved on the native path, because the C API materializes the result whole before the first byte; a slice of a batch of no rows is the batch itself; the JSON body of a native result over one batch becomes an array of several results, one per slice, as ADR-0010's "one per batch" already says, and a client that read only the first element of a large native result would miss rows (accepted, owner, 2026-10-21) | owner, 2026-10-21            |
| The constant lives in `internal/model`                                                                                                                                                                                 | `internal/encoding` and `internal/qdb` import `model` and not each other, and `model` holds what two packages share                                                                             | `internal/qdb`, the owner's first thought: `encoding` would have to import `qdb`                                                                                                                                                                                                                                                             | the import graph unchanged                                                                                   | nothing                                                                                                                                                                                                                                                                                                                                                                                                                            | owner, 2026-10-21            |
| The tests are the package's own: one property over a generated table on both engines and both clusters, and one error-row table                                                                                        | the project's way: one generative test plus one row table, and the Flight client in the HTTP equivalence property is the later unit's                                                           | pulling the HTTP equivalence extension forward: httpapi tests starting a Flight server per round trip, and the third Next item folded in                                                                                                                                                                                                     | the statement pinned now, the later unit untouched                                                           | the native and `#sql` results are compared with two oracles, the table and `Cluster.Duck`, not with each other                                                                                                                                                                                                                                                                                                                     | owner, 2026-10-21            |
| `GetFlightInfo` echoes the descriptor, answers no location and totals of -1                                                                                                                                            | the server stores nothing and runs nothing there, and an empty location means this server to the stock drivers                                                                                  | running the query for the totals: the query twice                                                                                                                                                                                                                                                                                            | a cheap `GetFlightInfo`                                                                                      | drivers see no row count before `DoGet`                                                                                                                                                                                                                                                                                                                                                                                            | ADR-0018, Decision 9; fact 5 |

## 5. Knowledge

- Commit 2 (`model.BatchRows`): the constant's comment carries "one
  batch size" and "not measured"; `internal/model/AGENTS.md` carries
  the home rule; `internal/encoding/AGENTS.md`'s ctx bullet and Arrow
  bullet are rewritten; `internal/qdb/read.go`'s `ReadOptions` doc and
  `readerOptions` name the constant.
- Commit 3 (`Cluster.Query` slices): `slices`' doc and notes carry the
  release and the empty case; `Query`'s step 3 carries why the slice is
  the core's and the gRPC limit; `internal/AGENTS.md`'s query, read and
  dispatcher bullets; ADR-0018, Decision 5, edited in place;
  `TestSlices` pins the property.
- Commit 4 (the Arrow encoder): `EncodeStream`'s overview carries the
  lazy open, the per-batch write and the rejected slice; `checkArrow`
  and `TestRoundTrip`'s doc lose the batch-boundary claim.
- Commit 5 (one method): the `Encoder` doc carries the one entry point
  and the never-empty sequence; each text encoder's per-batch ctx check
  carries its step comment; the e2e tool's `one` helper carries why the
  tool wraps; `internal/encoding/AGENTS.md`'s first, ctx and JSON
  bullets are rewritten, the JSON bullet carrying the several-results
  consequence.
- Commit 6 (`statusOf`): its overview carries the seven outcomes and
  the `RetryInfo` detail; `login`'s step 2 points at it; the `AGENTS.md`
  Auth bullet on the login's codes points at `statusOf`; `vendor/` gains
  `errdetails`.
- Commit 7 (`GetFlightInfoStatement`): its notes carry the verbatim
  ticket, the totals and the descriptor; the `AGENTS.md` Statement
  section's first bullet.
- Commit 8 (`DoGet`): `service`'s doc and `DoGet`'s overview carry the
  override, the rejected hook, the per-batch write and Close, the
  two failure kinds; `New`'s step 3 says what is registered; the
  `AGENTS.md` Statement section's second, third and fourth bullets;
  the two test rows that flip.
- Commit 9 (`TestStatement`): the test's overview carries the two
  oracles and the empty result; the `AGENTS.md` Tests bullet.
- Commit 10 (`TestStatementRows`): the rows and the unreachable note;
  the `AGENTS.md` Tests bullet.
- Commit 13 (the plan's deletion): `docs/log.md` Current state: the
  statement query leaves Next and In flight names it in one line, and
  the older entries that mention the statement query are untouched,
  because entries are append-only.
- Every Rationale row lands in: the DoGet override in `DoGet` and the
  Statement section; the verbatim ticket in `GetFlightInfoStatement`
  and the Statement section; one mapping in `statusOf` and the Auth
  bullet; the retry-after in `statusOf` step 3 and the Statement
  section; INTERNAL after the first message in `DoGet` step 4 and the
  Statement section; one batch size and the JSON consequence in
  `BatchRows`, `Query` step 3, `EncodeStream` step 2, ADR-0018,
  `internal/AGENTS.md` and `internal/encoding/AGENTS.md`; the
  constant's home in `internal/model/AGENTS.md`; the package's own
  tests in `TestStatement`'s doc and the Tests bullet; the
  `GetFlightInfo` shape in its notes; one method in the `Encoder` doc
  and the encoding `AGENTS.md`'s first bullet; the per-batch ctx check
  in each text encoder's step comment and the ctx bullet.

## 6. How the knowledge lands

The build stage is `/rr-build docs/flight-statement-plan.md`, which
writes every comment and document row of sections 3 and 5 in the same
commit as the code and reconciles the code against this plan before
the build-stage message.

Specific to this unit:

- Commit 3 lands before commit 4, because the Arrow encoder may drop
  its slicing only once the query core slices; between the two the
  encoder slices an already-sliced batch, which is harmless.
- Commit 5 changes the e2e tool, the encoding tests and the round trip's
  oracle in the same commit as the interface, because the tool and the
  tests must build at every commit.
- Commit 6 runs `go mod vendor` after the `errdetails` import and
  commits the vendored package with the code, because the build is
  `-mod=vendor`.
- Commit 8 flips the two existing test rows in the same commit, because
  `select 1` answers `OK` from that commit on and the tests must pass at
  every commit.
- If `GetStatementQueryTicket` turns out to accept a ticket of another
  command type (fact 2 says it does not), `DoGet` checks the `Any`'s
  type URL instead, and the executor asks the owner before adding a
  second decode.
- If arrow-go's client reader fails on a stream of one schema message
  and no batch (fact 4 says it does not), the executor asks the owner
  before writing an empty batch, because that changes the wire.

## 7. Commits

1. `docs(plan): flight-statement-plan.md, the statement query on the Flight listener` (this document)
2. `refactor(model): BatchRows is the one batch size of the binary, moved from internal/encoding and internal/qdb`
3. `feat(qdb): Cluster.Query hands its result to the sink as slices of model.BatchRows rows`
4. `refactor(encoding): the Arrow encoder writes each batch as it comes, with no slicing loop of its own`
5. `refactor(encoding): Encoder has one method, EncodeStream, each encoder checks the ctx once per batch, and the e2e tool writes a body as a sequence of one batch`
6. `refactor(flightsql): statusOf maps a cluster error to a gRPC status with a RetryInfo detail, and login calls it`
7. `feat(flightsql): GetFlightInfoStatement answers one endpoint whose ticket is the query text`
8. `feat(flightsql): DoGet runs a statement ticket through query.Run and writes each batch to the stream`
9. `test(flightsql): a generated table reads back over Flight on both engines and both clusters`
10. `test(flightsql): the statement's error rows answer their codes`
11. `/doc-discipline all internal/model internal/qdb internal/encoding internal/flightsql tests/e2e/tools/e2etool docs/flight-statement-plan.md`, fixes as further small commits
12. `/doc-discipline check docs/flight-statement-plan.md`, findings fixed before the next commit
13. `docs(log): flight-statement-plan.md deleted, facts moved to internal/flightsql/AGENTS.md, internal/AGENTS.md, internal/encoding/AGENTS.md, internal/model/AGENTS.md, ADR-0018 and the comments`
14. Verify: push `sc-19567/rr-flight-statement`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## 8. Open questions and recommendations

none
