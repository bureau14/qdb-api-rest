# Metrics series -- plan

Thread: M7 metrics series
Status: draft

## Outcome

When the unit lands, the tree has these properties:

- `log`, `metrics` and `debug.pprof` live under one `observability`
  block: `observability.log.*`, `observability.metrics.*` and
  `observability.pprof`. Their environment variables
  (`QDB_REST_OBSERVABILITY_...`), their flags (`--observability-...`),
  `examples/qdb_rest.yaml`, the brief, the log, ADR-0019 and every
  `AGENTS.md` spell them that way. The `debug` block is gone.
- `observability.metrics.duration_buckets` sets every duration
  histogram's buckets as `start` (a Go duration, `32ms` by default) and
  `count` (`10` by default), each boundary double the one before.
- `/metrics` serves, under the `metrics.namespace` prefix:
  - `budget_sessions{state}`, with `in_use` and `max`;
  - `breaker_state{state}`, with `closed`, `open` and `half_open`, at 1
    on the current state;
  - `pool_sessions{user,state}`, with `in_use`, `idle`, `dialing` and
    `closing`. `user` is present on a secure cluster and absent on an
    insecure one, where the pools are summed per state;
  - `retries_total`;
  - `http_request_duration_seconds{method,route,code}` on the data
    routes. The probes, `/metrics` and pprof are not recorded;
  - `flight_rpc_duration_seconds{method,code}`;
  - `first_byte_seconds{plane,format}` and
    `response_bytes_total{plane,format}` for the query and the table
    read on HTTP and for DoGet on Flight;
  - `query_duration_seconds{plane,engine,user}`, with `user` under the
    same rule as the pool gauges.
- The round trip asserts every series by name on both servers.
- `stress.sh budget` runs more clients than the budget against one
  table while it polls `budget_sessions{state="in_use"}`. `make
test-stress` runs it before the drain step on both clusters.

Later units take the rest. Slice C covers rows per format, qdb call
durations, and query latency per engine without a user. The green run
of the budget step on both clusters is the owner's, by hand, and M7
closes on it.

## Verified facts

- `qdb.New(cfg, now)` takes no context, and the eviction goroutine has
  none [internal/qdb/cluster.go:74-90, :728-740].
- `internal/qdb` reads nothing from the context today, and its tests
  call `Cluster` methods with `context.Background()`
  [internal/qdb/cluster_test.go:70, internal/qdb/read_test.go:80]. A
  `MetricsFrom(ctx)` inside `internal/qdb` would panic in those tests.
- `breaker.allow` changes an open breaker to half-open when the window
  has passed [internal/qdb/breaker.go:44-65], so a gauge must not call
  it.
- `Call` retries at most once, in its loop's second pass
  [internal/qdb/cluster.go:375-392].
- `Cluster.Stats` reads the budget through `budget.inUse` and
  `budget.max` and the users under `c.mu`
  [internal/qdb/cluster.go:798-809]. `takeIdle` walks `c.users` under
  `c.mu` and calls the binding's `pool.Stats()` there
  [internal/qdb/cluster.go:751-772], so that lock order is established.
  `instanceOf` takes `duckMu` before `c.mu`, so nothing may take
  `duckMu` while it holds `c.mu`.
- The binding's `SessionPoolStats` has `InUse`, `Idle`, `Dialing` and
  `Closing`, plus the cumulative `Dialed` and `Discarded`
  [vendor/github.com/bureau14/qdb-api-go/v3/session_pool.go:169-176].
- `withRequestLogging` wraps the whole mux, operational routes included
  [internal/httpapi/middleware.go:71-85, internal/httpapi/httpapi.go:104-118].
- Every data route is registered with a literal pattern of method and
  path: `POST /api/v2/query` [internal/httpapi/query.go:166],
  `GET /api/v2/tables/{name}/rows` [internal/httpapi/read.go:122],
  `POST /api/v2/rows` [internal/httpapi/rows.go:165], the table create
  and delete [internal/httpapi/tables.go:130-131], and the four auth and
  session routes [internal/httpapi/login.go:249-252].
- `countingWriter` counts what the encoder writes, before compression,
  in the query handler and the table reader
  [internal/httpapi/query.go:54-63, :135, internal/httpapi/read.go:87].
- `negotiate` answers an `encoding.Encoder`, whose `ContentType` is one
  of the four media types [internal/httpapi/negotiate.go:22-35,
  internal/encoding/encoding.go:25-33].
- The Flight `callContext` copies the logger, the cluster and the
  keychain, but not the metrics [internal/flightsql/server.go:110-121],
  and the Flight test fixture places only a logger
  [internal/flightsql/flightsql_test.go:47].
- `accessLine` has the method, the code and the start of every RPC
  [internal/flightsql/logging.go:28-50].
- DoGet writes each batch with `w.Write(rec)` inside `query.Run`'s sink
  [internal/flightsql/statement.go:157-175].
- `query.Logged` times the call and knows the engine, but not the
  plane or the user [internal/query/query.go:83-123]. The plane exists
  only as a log attribute [internal/httpapi/middleware.go:76,
  internal/flightsql/logging.go:21]. `query.Run` has the user `u`
  [internal/query/query.go:131], and the table reader has `caller(r)`
  [internal/httpapi/read.go:93-94].
- The config key path derives the flag and the variable names
  [internal/config/config.go:285-315]. `TestExample` pins
  `examples/qdb_rest.yaml` to `Default()`
  [internal/config/config_test.go:272-277].
- `client_golang` v1.25.0 is vendored with `prometheus`, `promhttp`
  and `collectors` [vendor/modules.txt:148-156]. It offers
  `ExponentialBuckets(start, factor, count)`.
- The stress budget is two sessions [tests/e2e/Makefile:166]. The drain
  step ends its server [docs/e2e.md:353-372], so the budget step runs
  before it on the same server.

## Design

### internal/config/config.go

```go
// Log configures the process logger.
type Log struct { /* unchanged fields */ }

// DurationBuckets cuts every duration histogram: Count boundaries, the
// first at Start and each next one double the last.
// [owner, 2026-11-02]
type DurationBuckets struct {
	Start time.Duration `yaml:"start"`
	Count int           `yaml:"count"`
}

// Metrics switches the Prometheus exposition, names its series and cuts
// its histograms.
type Metrics struct {
	Enabled         bool            `yaml:"enabled"`
	Namespace       string          `yaml:"namespace"`
	DurationBuckets DurationBuckets `yaml:"duration_buckets"`
}

// Observability groups what the process tells an operator about itself:
// its log, its metrics and the runtime profiler. [owner, 2026-11-02]
type Observability struct {
	Log     Log     `yaml:"log"`
	Metrics Metrics `yaml:"metrics"`
	Pprof   bool    `yaml:"pprof"`
}
```

`Config` swaps its `Log`, `Metrics` and `Debug` fields for one
`Observability Observability yaml:"observability"`. The `Debug` type
and its comment are deleted. `vocab` keys become
`observability.log.level` and `observability.log.format`.

```go
func Default() Config {
	// style: doc comment + inline notes
	// because: two defaults are numbers a reader asks about
	//
	// at the Default doc comment's list: the observability item names JSON logs, the statement cut, the exposition on and the profiler off [config.go:202-210]
	// at DurationBuckets: 32 ms and ten buckets are the owner's choice for a gateway, and a deployment sets its own [owner, 2026-11-02]
}
```

```go
// validateMetrics requires a namespace that fits the name grammar and
// buckets that the client can build: a positive start and at least one
// bucket.
func validateMetrics(m Metrics) error {
	// style: doc comment + inline notes
	// because: the namespace reason stays at its check, and the bucket check is a bound
	//
	// at the namespace check: an empty namespace is refused because the prefix keeps the series apart from other services (ADR-0019) [config.go:719-724]
	// at the start check: positive() names the key observability.metrics.duration_buckets.start [config.go:618-624]
	// at the count check: zero buckets would build a histogram of +Inf alone [Rationale: bucket bounds]
}
```

`validate` calls `oneOf` and the statement check with the new key paths.

### internal/observe/metrics.go

```go
// Metrics is the process's instrument registry and the instruments this
// binary records into. main builds it once and it travels in the
// context (ADR-0019).
type Metrics struct {
	// style: doc comment + inline notes
	// because: two exported fields are read by the cluster's collector
	//
	// Registry: the process's own registry, not the client's global one [metrics.go:18-22]
	// Namespace: the prefix the cluster's collector names its series under [ADR-0019 Decision 3]
	// PerUser: whether a per-user series carries the user label, which is true on a secure cluster [Rationale: user label]
	Registry  *prometheus.Registry
	Namespace string
	PerUser   bool
	// unexported instruments: request, rpc, firstByte, bytes, query
}
```

```go
// NewMetrics builds the registry with the series every instance exposes
// and the instruments the listeners record into. perUser decides once
// whether per-user series carry the user label.
func NewMetrics(cfg config.Metrics, perUser bool) *Metrics {
	// style: doc comment + overview + inline notes
	// because: the registrations have choices of their own, as today
	//
	// 1. the build-info series [metrics.go:44-53]
	// 2. the Go runtime and process collectors [metrics.go:55-59]
	// 3. the duration buckets from the config
	// 4. the instruments of the two planes and the query
	//
	// 3. ExponentialBuckets(start in seconds, 2, count) [owner, 2026-11-02]
	// 4. the query histogram's labels include user only when perUser [Rationale: user label]
}
```

```go
// userLabels returns the label names of a per-user series: names, plus
// user when the series carry it.
func (m *Metrics) userLabels(names ...string) []string {
	// style: doc comment alone
	// because: the one place that decides the user label for observe's instruments
}

// ObserveRequest records one data route's duration on the HTTP plane.
func (m *Metrics) ObserveRequest(method, route string, code int, d time.Duration)

// ObserveRPC records one Flight RPC's duration.
func (m *Metrics) ObserveRPC(method string, code codes.Code, d time.Duration)

// ObserveFirstByte records the time from a handler's entry to its first
// body byte or record batch.
func (m *Metrics) ObserveFirstByte(p Plane, f Format, d time.Duration)

// AddBytes adds bytes an encoder produced to the format's total.
func (m *Metrics) AddBytes(p Plane, f Format, n int64)

// ObserveQuery records one query's duration as user ran it on engine e.
// The user is dropped when series carry no user.
func (m *Metrics) ObserveQuery(p Plane, e Engine, user string, d time.Duration)
```

These five methods are bare except `ObserveQuery`, whose inline note at
the label branch says why an insecure cluster drops the user
[Rationale: user label].

### internal/observe/observe.go

```go
// Format names the wire format of a response body. It is the label of
// the byte and first-byte series.
type Format string

const (
	FormatJSON   Format = "json"
	FormatNDJSON Format = "ndjson"
	FormatCSV    Format = "csv"
	FormatArrow  Format = "arrow"
)
```

```go
// WithPlane tags ctx with the plane, both as the logger's attribute and
// as a value PlaneOf reads.
func WithPlane(ctx context.Context, p Plane) context.Context

// PlaneOf returns the plane an edge tagged ctx with. A context without
// one is a programming error, and PlaneOf panics on it, as Logger does.
func PlaneOf(ctx context.Context) Plane
```

`PlaneAttr` stays for `Logged`'s line. The comment on `Plane` says the
edges tag the plane with `WithPlane`.

### internal/qdb/breaker.go

```go
// state returns the breaker's position without admitting a call.
func (b *breaker) state() breakerState {
	// style: doc comment alone
	// because: a read under the lock with nothing to explain
}
```

The field `state` is renamed `pos` so the method can be `state`. The
alternative is to name the method `position`. The executor picks
whichever reads better in the file and keeps the doc comment.

`breakerState` gains `String()`, answering `closed`, `open` and
`half_open`, the label values [owner, 2026-11-02].

### internal/qdb/cluster.go

```go
type Cluster struct {
	// ...
	// retries counts the second attempts of Call. The collector reads it.
	retries atomic.Int64
}
```

```go
func (c *Cluster) Call(...) error {
	// existing comments unchanged, plus:
	// at cc.retry = false: count the retry on the cluster, because internal/qdb reads no metrics from the context [Rationale: retry counter]
}
```

### internal/qdb/collector.go (new)

```go
// Collector exposes the cluster's session budget, breaker and user
// pools to Prometheus, read at each scrape (ADR-0004).
type Collector struct {
	// style: doc comment alone
	// because: the fields are the descriptors and the cluster
}

// NewCollector builds the collector for c, naming its series under
// namespace. perUser adds the user label to the pool series.
func NewCollector(c *Cluster, namespace string, perUser bool) *Collector

// Describe sends the four descriptors.
func (k *Collector) Describe(ch chan<- *prometheus.Desc)

// Collect sends one snapshot of the budget, the breaker, the pools and
// the retry count.
func (k *Collector) Collect(ch chan<- prometheus.Metric) {
	// style: doc comment + overview + inline notes
	// because: the walk takes a lock in an order other code fixes
	//
	// 1. the budget, in use and max
	// 2. the breaker, 1 on the current state and 0 on the others
	// 3. the pools, per user or summed, under c.mu
	// 4. the retry count
	//
	// 1. in use counts sessions whose close is in flight, so it can exceed the sum of the pools [cluster.go:213-215]
	// 2. one series per state at 0 or 1 is the Prometheus convention for an enum [owner, 2026-11-02]
	// 3. c.mu then the pool's own lock, the order takeIdle takes, and duckMu is never taken [cluster.go:751-772, :592-598]
	// 3. an insecure cluster's usernames are the caller's choice, so its pools are summed [Rationale: user label]
}
```

### cmd/qdb_rest/main.go

Step 2 builds the metrics with
`observe.NewMetrics(cfg.Observability.Metrics, cfg.Cluster.PublicKeyFile != "")`.
Step 3 registers `qdb.NewCollector(cluster, m.Namespace, m.PerUser)` on
`m.Registry` right after `qdb.New`. Both step comments are rewritten
whole from the new code, and step 2 no longer speaks of a next unit.
Step 5 passes `cfg.Observability` to `httpapi.NewHandler`.

```go
	// 2. ... the per-user flag is the cluster's security, because a user
	//    label is only bounded where the cluster checks who the user is [Rationale: user label]
	// 3. ... the collector is registered once the cluster exists, so a scrape reads it [ADR-0004]
```

### internal/httpapi

```go
// NewHandler builds the HTTP handler: the v2 routes, the probes, and
// /metrics and /debug/pprof/ when the observability block switches them
// on.
func NewHandler(cfg config.Observability) http.Handler
```

`registerMetricsRoute` takes `config.Metrics` as today, and
`registerPprofRoutes` takes the `Pprof` bool. Their comments name the
new keys.

```go
// withRequestMetrics records the duration of a data route under its
// method, its route and the status it answered.
func withRequestMetrics(pattern string, next http.Handler) http.Handler {
	// style: doc comment + inline notes
	// because: the labels come from the registration, which a reader would otherwise look for in the request
	//
	// at the split: the method and the route are the registration's pattern, so the label set is bounded [Rationale: request labels]
	// at the recorder: responseRecorder is reused for the status [middleware.go:44-66]
}
```

Each data route's registration wraps its handler as
`withRequestMetrics(p, withCompression(requireBearer(...)))` with `p`
the same constant as the mux pattern. The probes, `/metrics` and pprof
stay unwrapped [owner, 2026-11-02].

```go
// formatOf names the encoder's wire format for the metrics.
func formatOf(enc encoding.Encoder) observe.Format {
	// style: doc comment + inline notes
	// at default: negotiate answers only the four encoders [negotiate.go:22-35]
}
```

```go
// countingWriter counts the bytes that reached the response, so the
// handler knows whether a problem body may still be written, and
// records the time to the first of them.
type countingWriter struct {
	w     io.Writer
	n     int64
	start time.Time
	first func(time.Duration)
}
```

`Write` calls `first` once, on the first write of a positive length.
`handleQuery` and `handleReadTable` set `start` at their entry, call
`AddBytes(PlaneHTTP, formatOf(enc), cw.n)` when the stream ends, and
their overview step 2 says the counter also feeds the metrics.

### internal/flightsql

```go
func (s *Server) callContext(ctx context.Context) context.Context {
	// the doc comment's list gains the metrics, and the body copies them
}
```

`accessContext` uses `observe.WithPlane`. `accessLine` calls
`MetricsFrom(ctx).ObserveRPC(method, status.Code(err), elapsed)`
beside the log line, and its doc comment says it records the RPC
duration too.

```go
func (v *service) DoGet(...) error {
	// existing overview, step 3 rewritten whole:
	// 3. ... the sink records the time to the first record batch and adds each batch's IPC body length as format arrow [owner, 2026-11-02]
}
```

The bytes come from a `countingStream` around `stream`, whose `Send`
adds `len(DataHeader)+len(DataBody)` of each message. Its doc comment
says that it counts the IPC messages the writer sends.

### internal/query/query.go

```go
// Logged runs call with sink and writes one query line ... as u, and
// records the duration in the query histogram under the context's plane.
func Logged(ctx context.Context, e observe.Engine, u qdb.User, call ..., sink ..., attrs ...slog.Attr) error {
	// existing comments unchanged, plus:
	// at ObserveQuery: every outcome is recorded, as every outcome logs a line [query.go:111-112]
}
```

`Run` passes `u`, and the table reader passes `caller(r)`.

### tests

```go
// TestCollector leases a session as the cluster's test user and reads
// the collector: one session in use in the user's pool, or in the
// summed pool on the insecure cluster, and one unit of the budget.
func TestCollector(t *testing.T) {
	// style: doc comment + inline notes
	// because: a property test has nothing to draw here, and the label rule is logic
	//
	// at the lease: the collect runs inside Call, so the session is held [cluster.go:369-395]
	// at the label check: the secure cluster names the user, the insecure one does not [Rationale: user label]
}
```

The fixtures that build a context for a route or a package that records
place `observe.NewMetrics(config.Default().Observability.Metrics, false)`
and a plane: `internal/query/query_test.go:176`,
`internal/httpapi/middleware_test.go:21`,
`internal/httpapi/readiness_test.go:40-41`, the server fixture of
`internal/httpapi/query_test.go`, and
`internal/flightsql/flightsql_test.go:47`.

### tests/e2e

```
# round-trip.sh, METRIC_SERIES
- the new series names join the list in the commit that asserts them
- the comment names the namespace default and says process_ is left out [round-trip.sh:330-337]
```

```
# stress.sh budget_step
- the step ingests table e2e_stress_budget, starts STRESS_CLIENTS reads through xargs -P, and polls budget_sessions{state="in_use"} until the reads end [docs/e2e.md:375-378]
- it fails when a poll reads more than budget_sessions{state="max"} [docs/e2e.md:375-378]
- it fails unless every client's body compares equal to the rows [docs/e2e.md:375-378]
- it reads the gauge by name with a fixed-string grep, as the round trip's scrape does [round-trip.sh:345-368]
- its own table name keeps it apart from the drain step's e2e_stress [stress.sh:24]
```

```
# Makefile, test-stress
- STRESS_CLIENTS ?= 6, three times the budget, not measured
- per cluster: start the server, run the budget step, then the drain step, which ends the server
```

### Documents

```
# docs/e2e.md, step 9 and the stress
- the scrape lists the series families by name
- the budget step names budget_sessions{state="in_use"} and the client count
- the stress layout row drops "with the observability milestone"

# internal/AGENTS.md, Logging and metrics
- the cluster's collector reads the budget, the breaker, the pools and the retry count at scrape time, and internal/qdb reads no metrics from the context
- a per-user series carries user on a secure cluster only, decided once by NewMetrics
- the plane is a context value set by the edges with WithPlane

# docs/brief.md, M7 and Observability and logging; docs/log.md, M7 criteria
- the keys are spelled under observability
- a user label sits on the pool gauges and the query histogram, on a secure cluster

# docs/adr/0019-metrics-registry.md
- Decision 3 names observability.metrics.namespace (in place, the key moved)
```

## Rationale

| decision                                                                     | why                                                                                                                                                                | rejected, and why                                                                                                     | gained                                                | given up                                                       | settled by                                  |
| ---------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------- | -------------------------------------------------------------- | ------------------------------------------- |
| One `observability` block for log, metrics and pprof                         | The owner wants the operator's view of the process in one place.                                                                                                   | Keeping `log`, `metrics` and `debug` apart, which the owner turned down                                               | One place to look                                     | Every flag and variable of those keys changes name             | owner, 2026-11-02                           |
| pprof as `observability.pprof`                                               | The `debug` block existed for one key.                                                                                                                             | `observability.debug.pprof`, a block of one key                                                                       | One level less                                        | none                                                           | proposal                                    |
| Buckets as start and count, doubling, shared by every duration series        | The latencies a customer cares about differ per deployment, and the first byte is the same kind of duration as the query.                                          | The client's defaults (5 ms to 10 s), a fixed layout, a list of boundaries                                            | One knob per deployment                               | The default tops out at 16.4 s, and a minute needs `count: 12` | owner, 2026-11-02                           |
| Bucket bounds: start positive, count at least one                            | The client cannot build a histogram from them otherwise.                                                                                                           | Letting the client panic at startup                                                                                   | The key is named at startup                           | none                                                           | `internal/AGENTS.md`, Code                  |
| Durations in seconds, `_seconds`                                             | It is the Prometheus convention, and Grafana reads the unit from the suffix.                                                                                       | Milliseconds named `_milliseconds` or `_ms`                                                                           | Standard dashboards work                              | none                                                           | owner, 2026-11-02                           |
| The state as one series with a `state` label                                 | It is the Prometheus convention for an enum, and a query sums over the states.                                                                                     | One series per measure                                                                                                | One name per thing                                    | A wider label set                                              | owner, 2026-11-02                           |
| Request durations on the data routes only                                    | Storing metrics costs the user, and probe traffic would dilute the series.                                                                                         | Every route, which mirrors the access line                                                                            | Smaller series                                        | A broken probe shows only in the access line                   | owner, 2026-11-02                           |
| Request labels from the registration's pattern                               | The pattern bounds the label set, and the registration already holds it.                                                                                           | The raw path, whose `{name}` is unbounded. Reading `r.Pattern` in the outer middleware, which needs an exclusion list | No exclusion list                                     | One wrapper per registration                                   | proposal                                    |
| Separate HTTP and Flight duration series                                     | Flight has no route, and one series takes one label set.                                                                                                           | One series with a plane label and an empty route                                                                      | Each label set is complete                            | Two names                                                      | proposal                                    |
| Bytes count what the encoder produced, with a format label                   | It answers "how much data per format", and the counter exists.                                                                                                     | Wire bytes with a coding label, or both                                                                               | The existing counter                                  | The compressed size is in the access line only                 | owner, 2026-11-02                           |
| First byte at the first body write or record batch, from the handler's entry | No handler flushes, so the socket's first byte is not observable.                                                                                                  | The socket's first byte                                                                                               | A measurable point                                    | Bearer time before the handler is outside it                   | owner, 2026-11-02 (point); proposal (start) |
| Short format names in an `observe.Format` vocabulary                         | The owner's answer named `format=arrow`, and the plane, engine and outcome are closed vocabularies there.                                                          | The media type as the label value                                                                                     | Short label values                                    | One switch in httpapi                                          | proposal                                    |
| A per-user query histogram, never pruned                                     | The owner wants per-user query latency, and a deployment has a finite number of users.                                                                             | Per-user latency in the logs only, or a user label on every histogram                                                 | Per-user view at about 12 series per user and engine  | The brief's "user label on the pool gauges only"               | owner, 2026-11-02                           |
| The user label only on a secure cluster                                      | On an insecure cluster the caller chooses the username, so the label would be unbounded. NewMetrics decides it once from the cluster's key.                        | The label everywhere, or an `if` at each record site                                                                  | One decision point                                    | An insecure deployment has no per-user view                    | owner, 2026-11-02                           |
| Pool, budget, breaker and retries read by a collector at scrape              | A scrape reads in-process state (ADR-0004), an evicted pool leaves the series, and `internal/qdb` reads no metrics from the context, which its tests do not place. | Gauges pushed at every lease, or `MetricsFrom` in `Call`                                                              | No double bookkeeping, and the tests stay as they are | The retry count lives on the cluster as an atomic              | proposal                                    |
| The plane as a context value beside its attribute                            | `Logged` needs it for a label, and the edges already tag it.                                                                                                       | A plane parameter on `query.Run` and `Logged`                                                                         | Callers stay as they are                              | One more context value                                         | proposal                                    |
| `Logged` takes the user                                                      | The query histogram needs the user, and the callers have it.                                                                                                       | Reading the user back from the logger                                                                                 | Explicit                                              | One more parameter                                             | proposal                                    |
| The pool's `Dialed` and `Discarded` counts are not exposed                   | The brief asks for occupancy, breaker state and retries.                                                                                                           | Two more series per user                                                                                              | Smaller exposition                                    | Dial churn is not visible                                      | proposal                                    |
| The budget step before the drain, on its own table                           | The drain ends the server, and both steps ingest.                                                                                                                  | A second server per cluster                                                                                           | One server start per cluster                          | The steps run in a fixed order                                 | proposal                                    |

## Knowledge

- Commit 2: the `Observability` doc comment, the `Default` list
  item, and the help tags. The key spelling in `docs/brief.md`,
  `docs/log.md`, ADR-0019 Decision 3, `internal/AGENTS.md`,
  `internal/httpapi/AGENTS.md` and `cmd/qdb_rest/AGENTS.md`. The pprof
  row is `plan only`, because no reader of the key proposes a block.
- Commit 3: the `DurationBuckets` doc comment, the `Default` inline note
  (the owner's choice), and the bucket checks in `validateMetrics`.
- Commit 4: the `Metrics` and `NewMetrics` comments and the
  `userLabels` doc comment. The user-label rule goes to
  `internal/AGENTS.md`, Logging and metrics, in commit 8, where the
  collector first applies it.
- Commit 5: the `WithPlane` and `PlaneOf` doc comments. The plane row
  goes to `internal/AGENTS.md` (Edges enrich).
- Commits 6-8: the breaker's `state` doc comment, the `retries` field
  comment, and the `Collector` overview with its lock-order note. The
  collector row goes to `internal/AGENTS.md`.
- Commit 9: the step comments in `main`.
- Commits 11-13: `METRIC_SERIES`, the `stress.sh` header and its budget
  step comments, the Makefile parameter comment, `docs/e2e.md` step 9
  and the budget step, and `tests/e2e/AGENTS.md` if it names the steps.
- Commits 14-19: the instrument doc comments, `withRequestMetrics`,
  `formatOf`, `countingWriter`, `callContext`, `accessLine`, DoGet
  step 3 and `countingStream`. The request-label row lands at
  `withRequestMetrics`.
- Commit 20: the `Logged` doc comment and its inline note.
- Commit 21: `docs/brief.md` M7, and `docs/log.md` M7 criteria.
- `plan only`: the rejected milliseconds, the rejected `_ms`, the
  rejected wire bytes, and the reasons the owner gave in the thread.

## How the knowledge lands

The build stage is `/rr-build`. It writes every comment and document
row from sections 3 and 5 in the same commit as the code, and it
reconciles the code against this plan before the build-stage message.
Specific to this unit: commit 2 renames keys that `docs/log.md`'s M7
criteria quote, so the log line is rewritten there, and commit 21 then
changes only the user-label sentence.

## Commits

1. docs(plan): metrics-series-plan.md, the observability block, the pool, budget, breaker and retry state, request latency, first byte and bytes on both planes, and the per-user query histogram
2. refactor(config): the observability block holds log, metrics and pprof, and every reader, flag, document and the reference config follow
3. feat(config): observability.metrics.duration_buckets doubles from start for count buckets, 32ms and 10 by default
4. feat(observe): Metrics carries the namespace, the per-user flag and the duration buckets
5. feat(observe): WithPlane tags the plane as a context value beside its attribute, and both edges use it
6. feat(qdb): the breaker answers its state without admitting a call
7. feat(qdb): Call counts each retry on the cluster
8. feat(qdb): a collector exposes budget_sessions, breaker_state, pool_sessions and retries_total at scrape time
9. feat(cmd): main registers the cluster collector and builds the metrics with the cluster's security
10. test(qdb): the collector reports a leased session in its pool and in the budget, on both clusters
11. test(e2e): the round trip asserts the budget, breaker, pool and retry series
12. test(e2e): stress.sh budget polls budget_sessions in use while more clients than the budget read one table
13. build(e2e): test-stress runs the budget step before the drain, and docs/e2e.md names the series
14. feat(observe): the request, rpc, first-byte, bytes and query instruments with their record methods
15. feat(httpapi): the data routes record request duration by method, route and code
16. feat(httpapi): the query and table-read handlers record first byte and bytes per format
17. feat(flightsql): callContext copies the metrics, and the test fixture places them
18. feat(flightsql): the access interceptors record rpc duration by method and code
19. feat(flightsql): DoGet records the first batch and its bytes as format arrow
20. feat(query): Logged takes the user and records the query duration by plane, engine and user
21. docs(brief): the m7 user label covers the pool gauges and the query histogram on a secure cluster, and the log criteria follow
22. test(e2e): the round trip asserts the request, first-byte, bytes and query series
23. `/doc-discipline all internal cmd tests/e2e docs/metrics-series-plan.md`, its fixes committed as `docs(<scope>): ...`
24. docs(log): metrics-series-plan.md deleted, facts moved to internal/AGENTS.md, docs/e2e.md, docs/brief.md, ADR-0019 and the comments of internal/config, internal/observe, internal/qdb, internal/httpapi, internal/flightsql, internal/query, cmd/qdb_rest and tests/e2e
25. Verify: push `sc-19567/rr-metrics-series`, build its head in
    Buildkite, and wait for the result. Green: report the build number.
    Red: fix with further small commits on this branch, push, and build
    again.

## Open questions and recommendations

1. Where does the pprof switch live? Recommendation: `observability.pprof`.
2. Should the pool's cumulative dial and discard counts be exposed?
   Recommendation: not in this unit.
3. Where does the first-byte clock start? Recommendation: at the
   handler's entry. The bearer check before it is cheap.
4. The budget step's green run on both clusters is the owner's, by
   hand, and M7 closes on it.
