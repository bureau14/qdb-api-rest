# M7 remaining series -- plan

Thread: M7 remaining series
Status: approved

## Outcome

When the unit lands, the tree has these properties:

- `/metrics` serves `<namespace>_rows_total{plane,format,direction}`.
  Direction `out` counts the rows of the HTTP query, the HTTP table read
  and Flight DoGet, at the three sites that add response bytes. Direction
  `in` counts the rows the ingest route pushed, under the format of its
  request body.
- `/metrics` serves `<namespace>_qdb_call_duration_seconds{operation,outcome[,user]}`.
  It times every run of an operation's closure on a leased session inside
  `Cluster.Call`. A retried read records two observations. The wait for a
  session and the breaker check stay outside. `user` is present on a
  secure cluster only. A table read is one observation, and it spans
  every batch fetch and the client's consumption of the stream.
- `Cluster.Call` takes the operation's name. The eight cluster methods
  pass `query`, `describe`, `ingest`, `read`, `create_table`,
  `remove_table`, `tables` and `table_schemas`.
- The round trip asserts both series by name on both servers.
- `docs/log.md` Next item 1 is gone. "Query latency per engine without a
  user" is covered by `query_duration_seconds{plane,engine}` and is not
  built.

Left for later: the stress's budget step green by hand on both clusters,
which is the owner's run and closes M7 (`docs/log.md`, Next 2).

## Verified facts

- `qdb.New` receives the whole configuration, `cfg.Observability`
  included [internal/qdb/cluster.go:77].
- Every caller of `qdb.New` builds its configuration from
  `config.Default()`, so the duration buckets are set in every test
  [internal/qdbtest/qdbtest.go:129, internal/httpapi/login_test.go:112,
  internal/httpapi/readiness_test.go:69, internal/flightsql/flightsql_test.go:435,
  internal/qdb/cluster_test.go:128,230].
- `observe.NewMetrics` cuts every histogram with
  `prometheus.ExponentialBuckets(b.Start.Seconds(), 2, b.Count)`
  [internal/observe/metrics.go:76-77].
- `internal/qdb` imports neither `internal/observe` nor reads metrics from
  the context, and `observe` imports only `config` and `build` of this
  module [internal/observe/metrics.go:3-14, internal/qdb/cluster.go:3-20].
  `qdb` may import `observe` without a cycle.
- `Call` leases inside `c.poolFor(u).Do(ctx, func(s qdbapi.Session) error {...})`,
  and the closure runs only on a leased session
  [internal/qdb/cluster.go:383].
- `c.now` is the clock the tests fake for the breaker
  [internal/qdb/cluster.go:74-80].
- The callers of `Call` are the eight cluster methods
  [internal/qdb/cluster.go:447,486, ingest.go:167, read.go:171,
  table.go:80,87,139,251], four test sites
  [internal/qdb/cluster_test.go:103,138,142,160] and the table fixture
  [internal/qdbtest/table/table.go:480].
- `Cluster.Read` runs the sink inside the closure, so the session is held
  while the client consumes the stream [internal/qdb/read.go:165-171].
  `Cluster.Query` runs the sink after `Call` returns
  [internal/qdb/cluster.go:428-476].
- `Authenticate` and `Probe` dial outside `Call`
  [internal/qdb/cluster.go:501-534].
- The collector is a custom `prometheus.Collector` registered once by
  main [internal/qdb/collector.go:40-118, cmd/qdb_rest/main.go:275].
- The secure test is `PublicKeyFile != ""` in both main and
  `Cluster.Secured` [cmd/qdb_rest/main.go:260, internal/qdb/cluster.go:103].
- `query.countRows` counts the rows of each pulled batch before yield
  [internal/query/query.go:63-77].
- The query and table-read handlers add bytes after the run
  [internal/httpapi/query.go:176, internal/httpapi/read.go:102], and
  DoGet after its run [internal/flightsql/statement.go:211].
- `formatOf` maps an encoder's media type to an `observe.Format`
  [internal/httpapi/query.go:53-66]. `decoderOf` parses the request's
  media type and the decoders are keyed by the same four media types
  [internal/httpapi/rows.go:28-45].
- `IngestResult.Rows` counts the pushed rows [internal/qdb/ingest.go:75-78],
  and a failed ingest returns a zero result [internal/qdb/ingest.go:172-175].
- `observe.Format` is documented as the format of a response body
  [internal/observe/observe.go:150-152].
- The owner's answers of 2026-11-04 in the thread: time on the leased
  session only; labels operation, outcome and user on a secure cluster;
  response and ingest rows with a direction label; the per-engine item is
  dropped; a table read is one observation spanning its fetches.

## Design

### internal/observe/observe.go

The `Format` doc comment is rewritten whole: it names the wire format of
a response or request body.

```go
// Format names the wire format of a response or request body. It is
// the label of the byte, first-byte and row series. [internal/observe/metrics.go]
type Format string
```

```go
// Direction says whether rows left the server in a response or arrived
// in an ingest body. It is the direction label of the row series.
type Direction string

const (
	DirectionOut Direction = "out"
	DirectionIn  Direction = "in"
)
```

The comment above the closed vocabularies names `Direction` beside
Plane, Engine, Outcome and Format [internal/observe/observe.go:107].

### internal/observe/metrics.go

```go
// Buckets returns the boundaries of every duration histogram, in
// seconds: b.Count bounds, the first at b.Start and each next double
// the last. [internal/config/config.go:171-176]
func Buckets(b config.DurationBuckets) []float64 {
	// style: doc comment alone
	// because: one call to the client's helper
}
```

```go
type Metrics struct {
	// ... rows joins the unexported instruments
	rows *prometheus.CounterVec
}
```

```go
func NewMetrics(cfg config.Metrics, perUser bool) *Metrics {
	// style: doc comment + overview + inline notes (unchanged shape)
	// because: the overview already lists the four steps
	//
	// 3. calls Buckets(cfg.DurationBuckets); the step comment keeps "in
	//    seconds, the unit of every _seconds series" [metrics.go:76]
	// 4. registers rows_total{plane,format,direction} beside the bytes
	//    counter, with help "Rows the response encoders consumed and the
	//    ingest pushed, by format." [Rationale: rows direction]
}
```

```go
// AddRows adds n rows of format f moving in direction d to the plane's
// total.
func (m *Metrics) AddRows(p Plane, f Format, d Direction, n int64) {
	// style: doc comment alone
	// because: one WithLabelValues call like AddBytes
}
```

### internal/query/query.go

`countRows` becomes `CountRows`. Its doc comment keeps its two claims,
and `Logged` calls it under the new name.

```go
// CountRows passes the batches through and adds the rows of each batch
// the consumer pulls to n. [query.go:63-64]
func CountRows(b qdb.Batches, n *int64) qdb.Batches {
	// style: doc comment + inline notes (unchanged)
	// because: the count-before-yield line stays explained [query.go:68]
}
```

### internal/httpapi/query.go

`formatOf` takes a media type, so the ingest route maps its request
type the same way.

```go
// formatOf names the wire format of a media type for the metrics.
func formatOf(mediaType string) observe.Format {
	// style: doc comment + inline notes
	// because: the default branch needs its reason
	//
	// at default: JSON, because the encoders and the decoders know only
	// the four media types [negotiate.go:11-16, rows.go:28-33]
}
```

```go
func handleQuery(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + overview + inline notes (unchanged shape)
	// because: the overview's four steps stay
	//
	// 2. the overview line names the counters: "with the encoder
	//    streaming inside the sink through counters of bytes and rows
	//    that feed the metrics" [query.go:149]
	//    streamErr = enc.EncodeStream(ctx, cw, query.CountRows(batches, &rows))
	//    after the run: AddBytes and AddRows(PlaneHTTP, f, DirectionOut, rows)
}
```

### internal/httpapi/read.go

```go
func handleReadTable(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + overview + inline notes (unchanged shape)
	// because: the overview's four steps stay
	//
	// 2. the overview line names both counters, as in handleQuery
	//    the sink wraps its batches in query.CountRows; Logged counts its
	//    own rows for the query line, and the two counts never meet
	//    [query.go:86-89]
	//    after the run: AddRows(PlaneHTTP, f, DirectionOut, rows)
}
```

### internal/httpapi/rows.go

```go
func handleIngestRows(w http.ResponseWriter, r *http.Request) {
	// style: doc comment + overview + inline notes (unchanged shape)
	// because: the six steps stay
	//
	// 6. in the err == nil case: AddRows(PlaneHTTP, formatOf(mt),
	//    DirectionIn, res.Rows) before the response is written
	//    a failed ingest adds nothing, because nothing was pushed
	//    [ingest.go:172-175]
}
```

`decoderOf` also returns the parsed media type, so the handler maps it
with `formatOf` without parsing the header twice. Its doc comment is
rewritten whole to name the media type as its first result.

```go
// decoderOf returns the media type of contentType and its decoder. The
// charset parameter is neither honored nor checked. The last value is
// false for a type the ingest does not accept. [rows.go:35-37]
func decoderOf(contentType string) (string, encoding.Decoder, bool) {
	// style: doc comment alone
	// because: a parse and a lookup
}
```

### internal/flightsql/statement.go

```go
// (*service).DoGet: the doc comment is unchanged
	// style: the existing step 3 comment block is rewritten whole
	// because: it lists what the sink records
	//
	// 3. the block's last sentence names the rows: the sink records the
	//    time to the first record batch and counts the rows it writes,
	//    and the counting stream adds the bytes, all under format arrow
	//    rows += rec.NumRows() after w.Write(rec) succeeds
	//    after the run: m.AddRows(PlaneFlight, FormatArrow, DirectionOut, rows)
	//    an empty batch is skipped before the write and adds nothing
	//    [statement.go:187-189]
```

### internal/qdb/cluster.go

```go
type Cluster struct {
	// ... after retries:
	// calls times each run of an operation on a leased session. The
	// collector exposes it.
	calls *prometheus.HistogramVec
}
```

```go
// newCallHistogram builds the histogram of operation durations under
// the namespace, with the user label when perUser.
func newCallHistogram(cfg config.Metrics, perUser bool) *prometheus.HistogramVec {
	// style: doc comment + inline notes
	// because: the user label rule needs its reason at the branch
	//
	// at perUser: an insecure cluster accepts any username, so a user
	// label there would make the series unbounded [internal/AGENTS.md,
	// per-user series]
	// help: "Time an operation ran on a leased session, a retry
	// counted apart. A table read includes the client's consumption of
	// the stream." [Rationale: what a call measures, table read]
}
```

```go
func New(cfg config.Config, now func() time.Time) *Cluster {
	// style: doc comment alone (unchanged)
	// because: the struct literal gains one field
	//
	//    calls: newCallHistogram(cfg.Observability.Metrics, cfg.Cluster.PublicKeyFile != "")
}
```

```go
// observeCall records one run of operation op as u.
func (c *Cluster) observeCall(op string, u User, d time.Duration, err error) {
	// style: doc comment + inline notes
	// because: the outcome and the user label each need a line
	//
	// outcome: observe.OutcomeOK on nil, observe.OutcomeError otherwise
	// [Rationale: outcome vocabulary]
	// the user is appended when c.Secured(), the rule newCallHistogram
	// applied
}
```

The `Call` doc comment is rewritten whole from the new signature:

```go
// Call runs f, the operation op, on a session authenticated as u. It
// returns a *BreakerOpenError while the cluster is unreachable.
// WithReadRetry allows one retry of an idempotent read. [cluster.go:369-371]
func (c *Cluster) Call(ctx context.Context, u User, op string, f func(*Session) error, opts ...CallOption) error {
	// style: doc comment + inline notes
	// because: the existing inline notes stay, and the timing adds two
	//
	// inside the Do closure, before f: the clock starts on the leased
	// session, so the wait for a session stays out of the duration
	// [Rationale: what a call measures]
	// time.Now and not c.now, because c.now is the clock the tests fake
	// for the breaker [cluster.go:74-80]
	//    start := time.Now(); err := f(newSession(s)); c.observeCall(op, u, time.Since(start), err)
}
```

Every caller passes its operation. `Read` gets one inline line: a read
is one call, so its duration spans every fetch and the client's
consumption [read.go:165-171].

### internal/qdb/collector.go

```go
// Describe sends the four descriptors and the call histogram's.
// Collect sends one snapshot of the budget, the breaker, the pools and
// the retry count, and the call histogram.
	// style: Collect keeps its overview, with a step 5
	// because: the order of the steps is the order of the series
	//
	// 5. the call histogram, which the cluster records into between
	//    scrapes
	//    k.c.calls.Collect(ch)
```

### internal/qdb/cluster_test.go, internal/qdbtest/table/table.go

The four test sites pass `"test"` and the fixture's `call` passes
`"fixture"`. No comment: the label is never read in a test.

### internal/AGENTS.md, Logging and metrics

The cluster's collector bullet is rewritten whole:

```
- The cluster's collector (`qdb.Collector`) reads the budget, the
  breaker, the user pools and the retry count at scrape time, and
  forwards the call histogram the cluster records into. `Call` times
  each run of an operation on its leased session, by operation and
  outcome. Main registers the collector once the cluster exists, and
  `internal/qdb` reads no metrics from the context.
```

The per-user bullet names the call histogram beside the pool gauges.

### internal/httpapi/AGENTS.md

The byte-counter bullet [internal/httpapi/AGENTS.md:127-132] gains:

```
- the query and table-read sinks wrap their batches in
  `query.CountRows`, and the handler adds the rows to the format's row
  total under direction `out`; the ingest adds its pushed rows under the
  request's format and direction `in`
```

### internal/flightsql/AGENTS.md

The call-context bullet stays. No new row: DoGet's counting is
described at its step comment.

### tests/e2e/round-trip.sh, docs/e2e.md

`qdb_rest_rows_total` and `qdb_rest_qdb_call_duration_seconds` join
`METRIC_SERIES` after `qdb_rest_response_bytes_total`, and the list in
`docs/e2e.md` step 9 names `rows_total` and `qdb_call_duration_seconds`.

## Rationale

| decision                                                      | why                                                                                           | rejected, and why                                                                                                                             | gained                                                  | given up                                              | settled by        |
| ------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ----------------------------------------------------- | ----------------- |
| what a call measures: the closure on the leased session       | it is the one duration no other series shows; the budget wait shows in the pool gauges        | the whole call, which `query_duration_seconds` already shows for queries; one observation per attempt with the lease, which mixes the wait in | qdbd and binding time alone                             | a retried read is two observations                    | owner, 2026-11-04 |
| a table read is one observation                               | `Read` runs every fetch and the sink inside one closure                                       | one observation per fetch, which would need timing inside the bulk reader                                                                     | no change below `Call`                                  | the read's duration includes the client's consumption | owner, 2026-11-04 |
| call labels: operation, outcome, user on a secure cluster     | the rule of the pool gauges and the query histogram                                           | operation alone; operation and outcome                                                                                                        | slow operations per user, timeouts apart from successes | series grow with users                                | owner, 2026-11-04 |
| outcome vocabulary: ok and error, from `observe`              | `Call` already returns before timing when the caller left, and a cgo call finishes either way | an `abandoned` value                                                                                                                          | the query line's words reused                           | none                                                  | owner, 2026-11-04 |
| rows counted out and in with a direction label                | the owner wants ingest rows too                                                               | response rows only                                                                                                                            | one series for both directions                          | a wider label set                                     | owner, 2026-11-04 |
| the per-engine query item is dropped                          | `query_duration_seconds{plane,engine}` sums over `user`                                       | a second histogram without user                                                                                                               | no duplicate series                                     | none                                                  | owner, 2026-11-04 |
| the cluster owns the histogram, the collector forwards it     | `internal/qdb` reads no metrics from the context [internal/AGENTS.md]                         | a histogram in `observe` recorded through the context, which needs the metrics in every `internal/qdb` test                                   | the rule stays                                          | a second home for a histogram                         | proposal          |
| `observe.Buckets` shared by both packages                     | one cut for every duration histogram [config.go:171]                                          | the formula copied into `internal/qdb`                                                                                                        | one formula                                             | `qdb` imports `observe`                               | proposal          |
| `time.Now` in `Call`                                          | `c.now` is the breaker's fake clock in tests                                                  | `c.now`                                                                                                                                       | real durations in tests                                 | none                                                  | proposal          |
| the operation as a plain string argument                      | eight fixed callers and one fixture                                                           | a named `Operation` type with constants                                                                                                       | a smaller diff                                          | no compile-time check on the label                    | proposal          |
| the series names `rows_total` and `qdb_call_duration_seconds` | they read beside `response_bytes_total` and `query_duration_seconds`                          | `call_duration_seconds`, which does not say whose call                                                                                        | the subject is in the name                              | none                                                  | proposal          |
| `decoderOf` answers the media type                            | the handler maps it without parsing twice                                                     | a second `mime.ParseMediaType` in the handler                                                                                                 | one parse                                               | a wider return                                        | proposal          |

## Knowledge

- Commit 2 (observe): `Direction` and `AddRows` doc comments; the
  `Format` doc comment; `NewMetrics` step 4. Row "rows counted out and
  in" lands in the `rows_total` help text and the `Direction` comment.
- Commit 3 (query): `CountRows` keeps its doc comment. Nothing new.
- Commit 4 (httpapi query and read): handler overview step 2 in both;
  `internal/httpapi/AGENTS.md` byte-counter bullet, the `out` half.
- Commit 5 (flightsql): DoGet step 3 block.
- Commit 6 (httpapi ingest): `formatOf` and `decoderOf` doc comments;
  ingest step 6 line; the AGENTS bullet's `in` half.
- Commit 7 (observe buckets): `Buckets` doc comment; `NewMetrics` step 3.
- Commit 8 (qdb operation): `Call` doc comment; the `Read` inline line
  (row "a table read is one observation").
- Commit 9 (qdb timing): `calls` field comment, `newCallHistogram` and
  `observeCall` comments, the two inline lines in `Call` (rows "what a
  call measures" and "`time.Now`"); the help text carries the table-read
  row.
- Commit 10 (collector): `Describe` and `Collect` comments;
  `internal/AGENTS.md` collector and per-user bullets (rows "the cluster
  owns the histogram" and "call labels").
- Commit 11 (e2e): `METRIC_SERIES` and `docs/e2e.md` step 9.
- `plan only`: "outcome vocabulary" (no reader of `observeCall` proposes
  `abandoned` once `Call` returns early on a caller who left), "the
  per-engine query item is dropped", "the operation as a plain string",
  "series names", "`decoderOf` answers the media type", "`observe.Buckets`
  shared".

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and document
row from sections 3 and 5 in the same commit as the code and reconciles
the code against this plan before the build-stage message. Specific to
this unit: commit 9 uses `observe.OutcomeOK` and `observe.OutcomeError`
[internal/observe/observe.go:139-144], and it rewrites the `Outcome` doc
comment, which says "how a query ended", whole: an outcome is how a query
or a cluster operation ended.

## Commits

1. docs(plan): m7-remaining-series-plan.md, rows per format and qdb call durations
2. feat(observe): rows_total counts rows by plane, format and direction
3. refactor(query): CountRows is exported, so the handlers count the rows they encode
4. feat(httpapi): the query and table-read handlers add their response rows per format
5. feat(flightsql): DoGet adds its rows as format arrow
6. feat(httpapi): the ingest route adds its pushed rows by request format as direction in
7. refactor(observe): Buckets cuts every duration histogram, so internal/qdb cuts its own the same way
8. refactor(qdb): Call takes the operation each cluster method names
9. feat(qdb): the cluster times each operation on its leased session by operation, outcome and user
10. feat(qdb): the collector exposes qdb_call_duration_seconds
11. test(e2e): the round trip asserts rows_total and qdb_call_duration_seconds
12. `/doc-discipline all check internal/observe internal/query internal/httpapi internal/flightsql internal/qdb docs/m7-remaining-series-plan.md`, findings fixed in further small commits
13. docs(log): m7-remaining-series-plan.md deleted, facts moved to the comments of internal/observe, internal/query, internal/httpapi, internal/flightsql and internal/qdb, internal/AGENTS.md, internal/httpapi/AGENTS.md and docs/e2e.md; Current state drops Next item 1
14. Verify: push `sc-19567/rr-m7-remaining-series`, build its head in
    Buildkite, wait for the result. Green: report the build number. Red:
    fix with further small commits on this branch, push, build again.

## Open questions and recommendations

none
