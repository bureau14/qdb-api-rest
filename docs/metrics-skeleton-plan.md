# Plan: the metrics skeleton

Status: approved

The first slice of M7 (`docs/log.md`, M7 criteria): the Prometheus
registry and its exposition, the two configuration keys, the runtime
profiler, and the round trip's scrape. The request and query series,
the pool and breaker gauges and the stress's budget step are later
slices and record into the registry this plan creates.

## Outcome

When this unit lands:

- `go.mod` requires `github.com/prometheus/client_golang`, vendored.
- `metrics.enabled` (default true) and `debug.pprof` (default false)
  are configuration keys, as file keys, `QDB_REST_METRICS_ENABLED` and
  `QDB_REST_DEBUG_PPROF`, and `--metrics-enabled=BOOL` and
  `--debug-pprof=BOOL`. The parser takes a bool, and the reference
  configuration lists both.
- `internal/observe` holds `Metrics`, the process's registry with the
  build-info series and the client's Go runtime and process
  collectors, built once by main and carried in the context through
  `observe.WithMetrics` and `observe.MetricsFrom`, which panics on a
  context without one as `Logger` does.
- `GET /metrics` serves the registry's exposition on the HTTP and
  HTTPS listeners, beside the probes and outside the bearer edge and
  the compression wrapper, when `metrics.enabled` is true, and is not
  registered when it is false.
- `/debug/pprof/` serves the Go runtime profiler on the same mux when
  `debug.pprof` is true, and is not registered when it is false.
- The round trip scrapes `/metrics` on each server after every other
  step and asserts that the build-info series and the Go runtime
  series are in the body.
- ADR-0019 records the library, the carriage and the namespace.

Left for later units: every series beyond build info and the runtime
(the request and query histograms, rows and bytes per format, time to
first byte, the pool, breaker and retry state, the user label), the
query capture line on both planes, and the stress's budget step.

## Verified facts

- The tree has no Prometheus module, no `/metrics` route and no pprof:
  `go.mod` lists no `prometheus` module, `vendor/github.com/` has no
  `prometheus` directory, and no file under `internal/` or `cmd/`
  mentions `prometheus` or `pprof` (grep, 2026-10-28).
- The OpenTelemetry metric API (`go.opentelemetry.io/otel/metric
v1.44.0`) and the trace SDK are vendored transitively; the metric SDK
  and the Prometheus exporter are not (`vendor/modules.txt:174-242`,
  2026-10-28).
- `github.com/prometheus/client_golang` v1.25.0 is the latest release
  on the module proxy and downloads (`go list -m -versions`,
  2026-10-28). Its `prometheus` package offers `NewRegistry`
  (`registry.go:67`), `NewGaugeVec` and `GaugeOpts` with `Namespace`,
  `Name` and `ConstLabels`, and its `collectors` package offers
  `NewGoCollector` (`collectors/go_collector_latest.go:161`) and
  `NewProcessCollector` (`collectors/process_collector.go:49`).
  `promhttp.HandlerFor(reg, opts)` (`promhttp/http.go:226`) serves a
  registry's exposition. `collectors.NewBuildInfoCollector`
  (`collectors/collectors.go:37`) reports the Go module build info and
  not the ldflags-injected version, so it is not used.
- The process collector reads procfs on every platform but Windows and
  macOS (`prometheus/process_collector_procfsenabled.go:14`, build tag
  `!windows && !js && !wasip1 && !darwin`), and emits nothing when
  `/proc` is not mounted (`process_collector_procfsenabled.go:9-12`,
  `canCollectProcess`). Windows and macOS have their own files
  (`process_collector_windows.go`, `process_collector_darwin.go`).
  FreeBSD, in the CI matrix, does not mount procfs by default, so the
  `process_` series cannot be asserted on every platform.
- The exposition's `Content-Type` is `text/plain` with parameters
  (`promhttp/http.go:334`, 2026-10-28), so the round trip asserts the
  prefix.
- `NewHandler` takes nothing and returns the mux behind
  `withRequestLogging` (`internal/httpapi/httpapi.go:53-61`); the
  probes are registered bare on the mux (`httpapi.go:42-47`); main
  passes the one handler to both HTTP and HTTPS
  (`cmd/qdb_rest/main.go:288`, `main.go:156`, `main.go:169`).
- The package `httpapi` does not import `internal/config`; its tests
  do (grep, 2026-10-28).
- The process context is built in main in steps: the logger
  (`main.go:252-257`), the cluster (`main.go:268`), the keychain
  (`main.go:279`), then the listeners (`main.go:288`).
- `observe.Logger` panics on a context without a logger
  (`internal/observe/observe.go:93-99`), and `WithLogger` is the
  placement (`observe.go:85`).
- `internal/build` holds `Version`, `Commit`, `BuildTime`, `BuildMode`
  and `GOAMD64` as ldflags-set variables (`internal/build/build.go`),
  and main's `starting` line logs `version`, `commit` and `build_mode`
  (`main.go:258-259`).
- `parseValue` handles `string`, `time.Duration`, `int`, `int64` and
  `[]string` and panics on any other type
  (`internal/config/config.go:285-318`); `placeholder` names the usage
  text's value (`config.go:321-331`); every flag is registered as a
  string (`config.go:412`), so a bool flag needs `=true` or `=false`.
- `decode` runs mapstructure with `WeaklyTypedInput: true`
  (`config.go:514-529`), so a bool arrives as text from every layer
  and the file layer of the fold test can put the text as it does for
  every other scalar (`config_test.go:130-140`).
- The fold test draws a value per key type in `values`
  (`config_test.go:48-65`) and falls back to strings, which `set`
  would fail to parse for a bool (`config_test.go:70-86`).
  `TestExampleIsTheDefaults` pins `examples/qdb_rest.yaml` to
  `Default()` (`config_test.go:265-273`), and the file's header says
  every key is listed at its default (`examples/qdb_rest.yaml:1-2`).
- The handler tests build their context in `newServerOn`
  (`internal/httpapi/query_test.go:65-89`) and `probe`
  (`readiness_test.go:20-32`), both over `observeContext`
  (`readiness_test.go:36-39`); `middleware_test.go` wraps
  `withRequestLogging` directly and never calls `NewHandler`.
- The round trip's steps are functions over `request` and `expect`
  (`tests/e2e/common.sh:201-218`), called from `run_cluster`
  (`tests/e2e/round-trip.sh:319-348`); a log-style assertion follows
  `check_log` (`round-trip.sh:285-303`), a fixed-string grep. The
  Makefile starts the servers with `REST_ARGS` and `REST_ARGS_SECURE`
  (`tests/e2e/Makefile:39-41`, `Makefile:81-86`), with no metrics flag,
  so the default serves the route.
- The lint config forbids the slog globals and runs sloglint with
  snake-case keys (`.golangci.yml`); no rule forbids a new import.
- ADR-0004 rule 3: `/metrics` reads in-process counters only, is not
  gated on auth or cluster state, and the configuration key is its
  only switch (`docs/adr/0004-pure-probes.md:42-44`).

## Design

### `internal/config/config.go`

```go
// Metrics switches the Prometheus exposition. [docs/brief.md:995]
type Metrics struct {
	// style: doc comment alone
	// because: one field whose help tag says what it does
	Enabled bool `yaml:"enabled" help:"serve the Prometheus exposition at GET /metrics on the HTTP listeners"`
}

// Debug switches the runtime profiler; the block exists for that one
// key, because the brief spells it debug.pprof. [docs/brief.md:806]
type Debug struct {
	// style: doc comment alone
	// because: one field whose help tag says what it does
	Pprof bool `yaml:"pprof" help:"serve the Go runtime profiler under /debug/pprof/ on the HTTP listeners"`
}

// Config gains two fields after DuckDB, in the order the blocks
// appear in examples/qdb_rest.yaml. [config.go:160-169]
type Config struct {
	// ...
	Metrics Metrics `yaml:"metrics"`
	Debug   Debug   `yaml:"debug"`
}

// Default gains Metrics{Enabled: true}, and Debug stays zero; the doc
// comment's list gains one line: the exposition on and the profiler
// off. [docs/log.md:24-37]

// parseValue gains a bool case. [config.go:285-318]
func parseValue(k key, text string) (any, error) {
	// style: doc comment alone + inline note at the bool case
	// because: one type switch, and the bool case is one more arm
	//
	// at the bool case: strconv.ParseBool, so the file, the variable and
	// the flag take the same words; the error says "want true or false"
	// in the voice of the other arms [config.go:289-306]
	// at the bool case: a flag is a string flag, so the bool is spelled
	// --metrics-enabled=false and a bare --metrics-enabled is refused by
	// the flag package for want of an argument [config.go:412]
}

// placeholder gains "BOOL" for the bool type. [config.go:321-331]
```

`validate` does not change: a bool has no bound.

### `internal/config/config_test.go`

```go
// values gains a bool case drawing the words ParseBool accepts in
// both spellings, so the fold is checked on 1 and TRUE as well as
// true. [config_test.go:48-65]
//
// at the bool case: no reject-side word, because a bool has no
// validation to reject it, unlike the durations and ints [config.go:639-670]
```

### `examples/qdb_rest.yaml`

```yaml
# After the duckdb block, in Config's order:

metrics:
  # Serve the Prometheus exposition at GET /metrics on the HTTP and
  # HTTPS listeners, outside authentication. The scrape reads counters
  # held in this process and never touches the cluster.
  enabled: true

debug:
  # Serve the Go runtime profiler under /debug/pprof/ on the HTTP and
  # HTTPS listeners, outside authentication, for goroutine, heap and
  # CPU profiles of a live instance. Off unless a profile is wanted.
  pprof: false
```

### `internal/observe/metrics.go`

A new file, so `observe.go` stays the logger's.

```go
// Metrics is the process's instrument registry: every series this
// binary exposes is registered on it, and GET /metrics serves it. It
// is built once by main and travels in the context like the logger.
// [ADR-0019; docs/brief.md:884]
type Metrics struct {
	// style: doc comment alone + Note: on ownership
	// because: one exported field, and the ownership is the one fact
	//
	// Note: the registry is this process's own and not the client's
	// global default registry, because the brief forbids package-level
	// mutable state and the global would make a second server in one
	// test binary register twice [docs/brief.md:884; Rationale: the
	// registry]
	Registry *prometheus.Registry
}

// namespace is the prefix of every series this binary names, so a
// host scraping several exporters does not collide. The client's own
// go_ and process_ series keep their conventional names. [ADR-0019;
// owner, 2026-10-28]
const namespace = "qdb_rest"

// NewMetrics builds the registry with the series every instance
// exposes: build info, the Go runtime and the process. Later units
// register their instruments on it. [docs/brief.md:173-175]
func NewMetrics() *Metrics {
	// style: doc comment + numbered overview + step comments
	// because: three registrations whose choices each have a reason
	//
	// 1. the build-info series is qdb_rest_build_info{version,commit,
	//    build_mode} at 1, from internal/build, with the labels main's
	//    starting line logs, so a dashboard reads what the log says
	//    [main.go:258-259; build.go]
	//    - rejected: the client's build-info collector, because it reports
	//      the Go module version and not the ldflags-injected one
	//      [collectors/collectors.go:37; Rationale: build info]
	// 2. the Go runtime collector, because the brief asks for the runtime
	//    series [docs/brief.md:175]
	// 3. the process collector, which emits nothing where it cannot read
	//    the process (FreeBSD without procfs), so no platform fails
	//    [process_collector_procfsenabled.go:9-12]
	// MustRegister is right here, because the three names are fixed and a
	// collision is a programming error [registry.go]
}

// metricsKey is the context key, unexported so only WithMetrics and
// MetricsFrom touch it. [observe.go:79-81, the logger's shape]
type metricsKey struct{}

// WithMetrics returns a context that carries the metrics. [observe.go:85]
func WithMetrics(ctx context.Context, m *Metrics) context.Context {
	// style: doc comment alone
	// because: one WithValue, the logger's shape
}

// MetricsFrom returns the metrics the context carries. A context
// without them is a programming error, and MetricsFrom panics on it, so
// a forgotten placement fails at the first scrape instead of serving
// an empty exposition. [observe.go:93-99; owner, 2026-10-28]
func MetricsFrom(ctx context.Context) *Metrics {
	// style: doc comment alone
	// because: one type assertion and a panic, the logger's shape
	//
	// at the panic: a no-op fallback was rejected, because a forgotten
	// placement in main would then serve an empty /metrics without
	// failing [Rationale: carriage]
}
```

The package comment of `observe.go` says the package owns the
operational surface: the logger and the attribute vocabulary; it gains
the registry in the same sentence.

### `internal/observe/metrics_test.go`

```go
// TestNewMetricsExposesBuildInfo gathers the registry once and asserts
// the build-info series is there with the three labels, because the
// name and the labels are the wire shape dashboards read, and the
// e2e scrape sees only the name. [Rationale: build info]
//
// at the assertion: Gather, then find the family by name and read the
// label pairs; the value is 1 [registry.go]
```

### `internal/httpapi/httpapi.go`

```go
// handleMetrics serves the registry's exposition, with the client's
// handler built over the registry the request context carries.
// [ADR-0004:42-44]
func handleMetrics(w http.ResponseWriter, r *http.Request) {
	// style: doc comment alone + inline note at the handler build
	// because: one call
	//
	// at the build: the handler is built per scrape and not once at
	// registration, because the registry travels in the context and
	// NewHandler has no context; a scrape is rare and the build is a
	// small allocation [Rationale: carriage]
	// at HandlerOpts: the zero options, so a gather error is a 500 and
	// the exposition is gzip-compressed when the scraper asks, by the
	// client's own writer, because the route sits outside withCompression
	// [promhttp/http.go:226]
}

// registerMetricsRoute serves GET /metrics beside the probes, outside
// the bearer edge and the compression wrapper, when the exposition is
// on. Off, the route is not registered and the path is the mux's 404.
// [ADR-0004:42-44; docs/log.md:24-37]
func registerMetricsRoute(mux *http.ServeMux, cfg config.Metrics) {
	// style: doc comment alone
	// because: one guarded registration
	//
	// at the guard: a registered route answering 404 itself was
	// rejected, because ADR-0004 says the key disables the endpoint and
	// nothing else switches it [Rationale: the switch]
}

// registerPprofRoutes serves the Go runtime profiler under
// /debug/pprof/ on this mux when debug.pprof is true, for goroutine,
// heap and CPU profiles of a live instance. It is not a metrics
// source. [docs/brief.md:804-808]
func registerPprofRoutes(mux *http.ServeMux, cfg config.Debug) {
	// style: doc comment alone + inline note at the registrations
	// because: five registrations behind one guard
	//
	// at the registrations: Index, Cmdline, Profile, Symbol and Trace are
	// registered by hand on this mux, because net/http/pprof's init
	// registers them on the default mux, which this binary never serves
	// [net/http/pprof; main.go:96-113]
	// at the patterns: no method prefix, because Symbol takes POST and
	// Index serves every named profile under the /debug/pprof/ prefix
	// [net/http/pprof]
}

// NewHandler returns the root handler, which serves every route behind
// the request middleware. It takes the two switches of the
// unauthenticated operational routes, because configuration does not
// travel in the context; the handlers take the logger, the cluster,
// the keychain and the metrics from the request context. [Rationale:
// the switches]
func NewHandler(metrics config.Metrics, debug config.Debug) http.Handler {
	// style: doc comment alone
	// because: a list of registrations, as today
	//
	// at the two new calls: after registerStatusRoutes, so the
	// unauthenticated routes sit together [httpapi.go:53-61]
}
```

### `internal/httpapi/metrics_test.go`

```go
// TestOperationalRoutes is one table over the two switches: /metrics
// answers 200 with text/plain and the build-info series when on and
// 404 when off, and /debug/pprof/ answers 200 when on and 404 when
// off. It runs through NewHandler with a context carrying the metrics,
// without a cluster, because neither route reads one.
// [docs/log.md:24-37]
//
// at the context: observeContext plus WithMetrics(NewMetrics()), and
// no cluster, because the probes are not requested [readiness_test.go:36-39]
// at the 404 rows: the body is the mux's plain text, as the unknown
// paths answer [httpapi AGENTS.md, Handlers]
```

The fixtures `newServerOn` and `probe` gain `observe.WithMetrics(ctx,
observe.NewMetrics())` on their context and pass
`config.Metrics{Enabled: true}` and `config.Debug{}` to `NewHandler`,
so every later unit's middleware finds the metrics without touching
the fixture again.

### `cmd/qdb_rest/main.go`

```go
// main's step 2 gains the metrics after the logger: built once and
// placed in the context, so every listener and later every package
// records into one registry. The step comment says the registry is
// built before the cluster, because the next unit's pool gauges read
// the cluster from a collector registered here. [main.go:252-257;
// Rationale: carriage]
//
// at step 5: NewHandler(cfg.Metrics, cfg.Debug), the two switches
// passed from the configuration, because the handler builder takes
// nothing from the context [httpapi.go:53]
```

### `tests/e2e/round-trip.sh`

```bash
# METRIC_SERIES: the series every platform exposes, asserted by name
# on each server's scrape: the build-info series this binary names and
# two of the client's Go runtime series. The process_ series are not
# listed, because the client reads procfs for them and FreeBSD, in the
# matrix, does not mount it. A later unit's series joins this list with
# its commit. [Verified facts: the process collector]
METRIC_SERIES=(qdb_rest_build_info go_goroutines go_info)

# scrape_metrics <out_dir> <url>: GET /metrics is 200, text/plain, and
# the body names every series of METRIC_SERIES at the start of a line.
# Presence only, because nothing in the harness asserts a value
# (ADR-0015), and a fixed-string grep per name, as check_log greps,
# because the exposition is a text format the driver knows nothing
# else about. [round-trip.sh:285-303; docs/adr/0015]
scrape_metrics() {
    # 1. the request, no token, because the route is outside the bearer
    #    edge [ADR-0004:42-44]
    # 2. content-type by prefix, because the exposition carries version
    #    and charset parameters [promhttp/http.go:334]
    # 3. one grep -q "^name" per series, each a [FAIL] line naming the
    #    series
}

# run_cluster calls scrape_metrics once, after the format loop, so the
# scrape follows every request of the round trip, as the M7 criterion
# says, and the series of later units are counted when it runs.
# [docs/log.md:24-37]
```

### Documents

```markdown
<!-- docs/adr/0019-metrics-registry.md, from the template -->

# ADR-0019: metrics: the Prometheus client, the registry in the context, one namespace

Status: accepted. Date: 2026-10-28.

Context: the brief's Goal 9 and M7; nothing in the tree; the three
choices the owner settled on 2026-10-28 and the trade of each.

Decision:

1. prometheus/client_golang produces the exposition; one registry per
   process, built by main; the client's global registry is not used.
2. The registry travels in the context through observe.WithMetrics and
   MetricsFrom, which panics on a context without one, like the logger.
3. Every series this binary names carries the qdb_rest_ prefix; the
   client's go_ and process_ series keep their names.
4. The build-info series carries version, commit and build_mode from
   internal/build, the labels of main's starting line.

Consequences: pull only; a push to a backend goes through a scraping
agent or collector; a later OTLP reader sits beside the registry.
Every test fixture that builds a context places the metrics.

Alternatives rejected: a hand-written exposition; the OpenTelemetry
SDK with its Prometheus exporter; a no-op MetricsFrom; constructor
arguments; the qdb_ prefix and no prefix; the client's build-info
collector. Each with the one reason from the Rationale table.
```

```markdown
<!-- internal/AGENTS.md, Logging (ADR-0002) section, renamed "Logging and metrics (ADR-0002, ADR-0019)", one bullet -->

- The metrics travel in the context like the logger:
  `observe.MetricsFrom(ctx)` is the registry, placed once by main, and
  it panics on a context without one, so a test that reaches a route
  or a package that records places `observe.NewMetrics()` on its
  context. Every series this binary names carries the `qdb_rest_`
  prefix (ADR-0019).
```

```markdown
<!-- internal/httpapi/AGENTS.md, Middleware, the probes bullet grows -->

- `withRequestLogging` wraps the mux once. `requireBearer` and
  `withCompression` wrap a route. The probes, `/metrics` and
  `/debug/pprof/` stay outside both: the exposition compresses through
  the client's own writer when a scraper asks, and the profiler's
  answers are the stdlib's. The login and the refresh are compressed
  but unauthenticated.

<!-- Handlers, two bullets -->

- `GET /metrics` serves the registry the request context carries
  (`observe.MetricsFrom`), built per scrape through the client's
  handler, when `metrics.enabled` is true; off, the route is not
  registered and the path is the mux's 404 (ADR-0004, rule 3).
- `/debug/pprof/` serves the stdlib profiler's handlers, registered by
  hand on this mux, when `debug.pprof` is true, and is not registered
  when it is false. It is not a metrics source.

<!-- Tests, one bullet -->

- The two switches have one table, `TestOperationalRoutes`
  (`metrics_test.go`), run without a cluster. The handler fixtures
  place the metrics on their context, because `MetricsFrom` panics
  without them.
```

```markdown
<!-- cmd/qdb_rest/AGENTS.md, two bullets -->

- A bool key is spelled with its value on the command line
  (`--metrics-enabled=false`), because every flag is a string flag of
  the stdlib package, and a bare `--metrics-enabled` is refused for
  want of an argument.
- main builds the metrics registry once, right after the logger, and
  places it in the process context (`observe.WithMetrics`), so every
  listener records into one registry, and it passes `metrics.enabled`
  and `debug.pprof` to `httpapi.NewHandler`, because configuration
  does not travel in the context.
```

```markdown
<!-- docs/e2e.md, The v2 round trip, step 9 -->

9. The scrape. `GET /metrics` on each server, after every other step,
   answers 200 with `text/plain` and a body naming the build-info
   series and the Go runtime series (`METRIC_SERIES` in the driver,
   presence only, never a value). The `process_` series are not
   asserted, because the client reads procfs for them and FreeBSD
   does not mount it. A later unit's series joins the list with its
   commit.

<!-- Decision log (2026-10-28), one row -->

| The scrape asserts series names by grep, after the format loop | the exposition is text the driver knows nothing else about, and the M7 criterion says after its requests | a value assertion (ADR-0015); a scrape before the loop, which would miss the request series of later units |
```

```markdown
<!-- tests/e2e/AGENTS.md, one bullet after the warning-line bullet -->

- The driver scrapes `/metrics` on each server after the format loop
  and greps each name of `METRIC_SERIES` at the start of a line. A
  unit that adds a series adds its name there in the same commit, and
  nothing asserts a value.
```

```markdown
<!-- tests/e2e/README.md, the round trip's list gains one line -->

- the `/metrics` scrape: the series names, presence only.
```

```markdown
<!-- docs/brief.md: no change. Goal 9, Observability and logging, Project structure and M7 already say what this unit builds. -->
```

```markdown
<!-- docs/log.md, the deletion commit: M7 "in progress", In flight: none, Next unchanged, the entry
"metrics-skeleton-plan.md deleted; facts moved to ADR-0019, internal/AGENTS.md, internal/httpapi/AGENTS.md, cmd/qdb_rest/AGENTS.md, docs/e2e.md, tests/e2e/AGENTS.md and the comments of internal/observe, internal/httpapi, internal/config, cmd/qdb_rest and tests/e2e" -->
```

## Rationale

| decision                                                                              | why                                                                                                                                                                                                                         | rejected, and why                                                                                                                                                                                                                                                                               | gained                                                                         | given up                                                                                      | settled by        |
| ------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------- | ----------------- |
| the library: prometheus/client_golang                                                 | the brief asks for a Prometheus exposition and nothing about push; every series of Goal 9 maps onto one of its types                                                                                                        | a hand-written exposition, because histograms, the runtime series and the text format would be written and kept here; the OpenTelemetry SDK with its Prometheus exporter, because it vendors client_golang underneath plus two modules and a views-and-readers layer the brief does not ask for | the registry, the instruments, the runtime and process collectors, the handler | pull only: a push to CloudWatch or another backend goes through a scraping agent or collector | owner, 2026-10-28 |
| carriage: in the context, panicking when absent                                       | the tree carries the logger, the cluster and the keychain that way, and `internal/AGENTS.md` says nothing is injected through a constructor that the context already carries; a missing placement fails at the first scrape | a no-op when absent, because a forgotten placement in main would serve an empty exposition without failing; constructor arguments, because they break the carriage rule and thread a value through `Call`                                                                                       | one placement in main reaches every package                                    | every test fixture that builds a context places the metrics                                   | owner, 2026-10-28 |
| the namespace `qdb_rest_`                                                             | one namespace per binary, so a host scraping several exporters does not collide; the product is the QuasarDB REST API in every communication                                                                                | `qdb_`, because it collides with qdbd or another component on the same Prometheus; no prefix, because `http_requests_total` collides with every Go service                                                                                                                                      | unambiguous series names                                                       | longer names                                                                                  | owner, 2026-10-28 |
| the registry: a fresh `prometheus.NewRegistry`                                        | the brief forbids package-level mutable state, and the client's global registry would make two servers in one test binary register twice                                                                                    | the default registry and `promhttp.Handler()`                                                                                                                                                                                                                                                   | a registry per `NewMetrics`                                                    | the client's global convenience functions (`promauto`) are not used                           | proposal          |
| the switch: an unregistered route when `metrics.enabled` is false                     | ADR-0004 says the key disables the endpoint and nothing else switches it                                                                                                                                                    | a registered route answering 404 itself, because it adds a handler to say what the mux already says                                                                                                                                                                                             | the mux's plain 404, as every unknown path                                     | none                                                                                          | proposal          |
| the switches as `NewHandler` arguments, the two config blocks                         | configuration does not travel in the context, and `NewHandler` takes nothing today; the blocks name themselves                                                                                                              | two bare bools, because the call site would not say which is which; the whole `config.Config`, because the handler reads two keys                                                                                                                                                               | explicit at the call site                                                      | `httpapi` imports `internal/config`                                                           | proposal          |
| the exposition handler built per scrape                                               | the registry is in the context and `NewHandler` has no context                                                                                                                                                              | a handler built once at registration, because it needs the registry at build time, which the carriage decision rules out                                                                                                                                                                        | consistent carriage                                                            | one small allocation per scrape                                                               | proposal          |
| the pprof handlers registered by hand on this mux                                     | `net/http/pprof`'s init registers on the default mux, which this binary never serves                                                                                                                                        | serving the default mux, because the binary's mux is the one behind the middleware                                                                                                                                                                                                              | the profiler behind the switch and on both listeners                           | five registrations instead of one import                                                      | proposal          |
| the build-info series from `internal/build`, labels `version`, `commit`, `build_mode` | the labels are main's starting line, so a dashboard reads what the log says                                                                                                                                                 | the client's build-info collector, because it reports the Go module version and not the ldflags-injected one; `build_time` and `goamd64` as labels, because the starting line does not carry them and a label set is a wire shape                                                               | one series per instance                                                        | the build time and the arch level are not scrapable                                           | proposal          |
| the scrape asserts names, after the format loop, no `process_` series                 | ADR-0015 forbids value assertions; the M7 criterion says after its requests; the process collector reads procfs and FreeBSD does not mount it                                                                               | a value assertion; a scrape before the loop; the `process_` series with FreeBSD excused, because a platform exclusion for a series the harness does not need adds a branch                                                                                                                      | one step, every platform                                                       | the process series are unasserted                                                             | proposal          |
| ADR-0019 records the three owner choices                                              | `docs/AGENTS.md`: a dependency and a carriage rule constrain code until reversed                                                                                                                                            | the choices in `AGENTS.md` bullets alone, because an ADR is where a later agent looks before re-litigating a dependency                                                                                                                                                                         | one home                                                                       | one more ADR                                                                                  | proposal          |

## Knowledge

Per commit, where each why lands.

- Commit 2 (deps): nothing to narrate; `go.mod` and `vendor/` carry
  the version.
- Commit 3 (config): `parseValue`'s bool arm (the same words from every
  layer; the `=false` spelling), `placeholder`'s BOOL, the two block
  doc comments, `Default`'s list, `values`' bool arm in the test, the
  two blocks of `examples/qdb_rest.yaml` with their comments, and the
  bool-flag bullet of `cmd/qdb_rest/AGENTS.md`.
- Commit 4 (observe): `Metrics` with its ownership note, `namespace`,
  `NewMetrics`'s three steps with the rejected build-info collector,
  `MetricsFrom`'s panic and the rejected no-op, the package comment,
  `TestNewMetricsExposesBuildInfo`; ADR-0019 (the library, the
  carriage, the namespace, the build-info labels, the consequences and
  the six rejected alternatives); the `internal/AGENTS.md` bullet.
- Commit 5 (the route): `handleMetrics` (per-scrape build, zero
  options), `registerMetricsRoute` (the unregistered route),
  `NewHandler`'s doc comment (the switches as arguments), the fixture
  placements, `TestOperationalRoutes`' metrics rows; the
  `internal/httpapi/AGENTS.md` Middleware, Handlers and Tests bullets.
- Commit 6 (pprof): `registerPprofRoutes` (by hand on this mux, no
  method prefix), the pprof rows of `TestOperationalRoutes`; the
  Handlers bullet.
- Commit 7 (main): step 2's metrics line and its why (before the
  cluster, for the next unit's collector), step 5's two switches; the
  `cmd/qdb_rest/AGENTS.md` bullet.
- Commit 8 (e2e): `METRIC_SERIES` (why no `process_` series),
  `scrape_metrics` (presence only, the grep, the prefix match, no
  token), the `run_cluster` call (after the loop); `docs/e2e.md` step
  9 and its decision-log row, `tests/e2e/AGENTS.md`, `tests/e2e/README.md`.
- The deletion commit: `docs/log.md` Current state (M7 in progress)
  and the entry.

Every Rationale row lands: the library, the carriage, the namespace
and build info in ADR-0019 and `NewMetrics`; the registry in
`Metrics`'s note; the switch in `registerMetricsRoute`; the switches
in `NewHandler`; the per-scrape build in `handleMetrics`; pprof in
`registerPprofRoutes`; the scrape in `METRIC_SERIES`, `scrape_metrics`
and `docs/e2e.md`; the ADR in `docs/AGENTS.md`'s own rule.

## How the knowledge lands

The build stage is `/rr-build`, which writes every comment and
document row from the Design and Knowledge sections in the same commit
as the code and reconciles the code against this plan before the
build-stage message.

Specific to this unit:

- Commit 2 runs `go get github.com/prometheus/client_golang@v1.25.0`,
  `go mod tidy` and `go mod vendor`, and checks that `go.mod`'s
  `arrow-go`, `grpc` and `protobuf` pins did not move; if `tidy` moves
  one, the executor stops and asks, because `internal/AGENTS.md`
  (Building) says arrow-go is what moves those.
- Commit 4's `TestNewMetricsExposesBuildInfo` reads `build.Version`
  as "dev" under `go test`; the assertion is on the label names and
  the value, not on "dev".
- Commit 8's `METRIC_SERIES` names `go_goroutines` and `go_info`; the
  executor checks both names in the client's Go collector
  (`prometheus/go_collector.go`) before writing the list.
- The lint runs under `GOTOOLCHAIN=go1.27.0 make lint` if the machine's
  Go is newer than the pinned toolchain (memory note
  `lint-toolchain-go1270`, verified before the first lint).

## Commits

1. `docs(plan): metrics-skeleton-plan.md, the Prometheus registry in the context, /metrics and pprof behind their keys, and the round trip's scrape` (this document)
2. `build(deps): vendor prometheus/client_golang`
3. `feat(config): metrics.enabled and debug.pprof as bool keys, and the parser takes a bool`
4. `feat(observe): Metrics holds the registry with the build info and Go runtime collectors and travels in the context`
5. `feat(httpapi): GET /metrics serves the registry outside the bearer edge when metrics.enabled`
6. `feat(httpapi): /debug/pprof/ serves the runtime profiler when debug.pprof`
7. `feat(cmd): main builds the metrics, places them in the context and passes the switches to the handler`
8. `test(e2e): the round trip scrapes /metrics on each server and asserts the series`
9. `/doc-discipline all internal/observe internal/httpapi internal/config cmd/qdb_rest tests/e2e docs/metrics-skeleton-plan.md`, then `/doc-discipline check` against the plan; findings land as further small commits.
10. `docs(log): metrics-skeleton-plan.md deleted, facts moved to ADR-0019, internal/AGENTS.md, internal/httpapi/AGENTS.md, cmd/qdb_rest/AGENTS.md, docs/e2e.md, tests/e2e/AGENTS.md and the comments of internal/observe, internal/httpapi, internal/config, cmd/qdb_rest and tests/e2e`
11. Verify: push `sc-19567/rr-metrics-skeleton`, build its head in Buildkite, wait for the result. Green: report the build number. Red: fix with further small commits on this branch, push, build again.

## Open questions and recommendations

1. Which series the round trip asserts. Recommendation: the build-info
   series and two Go runtime series, no `process_` series, because
   FreeBSD does not mount procfs and a platform exclusion for an
   unneeded series adds a branch.
2. The build-info labels. Recommendation: `version`, `commit` and
   `build_mode`, the starting line's three, with `build_time` and
   `goamd64` left out, because a label set is a wire shape and the
   log line is the precedent.
3. The log's first Next item, the heap fault investigation, is not a
   unit of commits and needs a Linux amd64 host. Recommendation: it
   stays where it is, and the owner runs it outside this workflow or
   moves it when M7 is done.
