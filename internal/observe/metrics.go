package observe

import (
	"context"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"google.golang.org/grpc/codes"

	"github.com/bureau14/qdb-api-rest/internal/build"
	"github.com/bureau14/qdb-api-rest/internal/config"
)

// Metrics is the process's instrument registry and the instruments this
// binary records into. main builds it once and it travels in the
// context (ADR-0019).
type Metrics struct {
	// Registry is the process's own registry and not the client's global
	// one, because a second server in one test binary would register
	// twice on the global.
	Registry *prometheus.Registry
	// Namespace is the prefix the cluster's collector names its series
	// under (ADR-0019).
	Namespace string
	// PerUser says whether a per-user series carries the user label,
	// which is true on a secure cluster.
	PerUser bool

	request   *prometheus.HistogramVec
	rpc       *prometheus.HistogramVec
	firstByte *prometheus.HistogramVec
	bytes     *prometheus.CounterVec
	rows      *prometheus.CounterVec
	query     *prometheus.HistogramVec
}

// userLabels returns the label names of a per-user series: names, plus
// user when the series carry it.
func (m *Metrics) userLabels(names ...string) []string {
	if m.PerUser {
		return append(names, "user")
	}
	return names
}

// Buckets returns the boundaries of every duration histogram, in
// seconds: b.Count bounds, the first at b.Start and each next double the
// last.
func Buckets(b config.DurationBuckets) []float64 {
	return prometheus.ExponentialBuckets(b.Start.Seconds(), 2, b.Count)
}

// NewMetrics builds the registry with the series every instance exposes
// and the instruments the listeners record into. perUser decides once
// whether per-user series carry the user label.
func NewMetrics(cfg config.Metrics, perUser bool) *Metrics {
	// 1. the build-info series
	// 2. the Go runtime and process collectors
	// 3. the duration buckets from the config
	// 4. the instruments of the two planes and the query
	reg := prometheus.NewRegistry()

	// 1. The labels are version, commit and build_mode from internal/build,
	//    the three of main's starting line, so a dashboard reads what the
	//    log says. The client's own build-info collector reports the Go
	//    module version and not the ldflags-injected one.
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: cfg.Namespace,
		Name:      "build_info",
		Help:      "The version, commit and build mode of this binary, at 1.",
	}, []string{"version", "commit", "build_mode"})
	buildInfo.WithLabelValues(build.Version, build.Commit, build.BuildMode).Set(1)

	// 2. The process collector emits nothing where it cannot read the
	//    process (FreeBSD without procfs), so no platform fails.
	//    MustRegister, because the names are fixed and a collision is a
	//    programming error.
	reg.MustRegister(buildInfo, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	// 3. In seconds, the unit of every _seconds series.
	buckets := Buckets(cfg.DurationBuckets)

	// 4. The query histogram carries the user label only when perUser.
	m := &Metrics{Registry: reg, Namespace: cfg.Namespace, PerUser: perUser}
	histogram := func(name, help string, labels ...string) *prometheus.HistogramVec {
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: cfg.Namespace, Name: name, Help: help, Buckets: buckets,
		}, labels)
	}
	m.request = histogram("http_request_duration_seconds", "Duration of a data route on the HTTP plane.", "method", "route", "code")
	m.rpc = histogram("flight_rpc_duration_seconds", "Duration of a Flight RPC.", "method", "code")
	m.firstByte = histogram("first_byte_seconds", "Time from a handler's entry to its first body byte or record batch.", "plane", "format")
	m.bytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Name:      "response_bytes_total",
		Help:      "Bytes the encoders produced for response bodies, before compression.",
	}, []string{"plane", "format"})
	m.rows = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: cfg.Namespace,
		Name:      "rows_total",
		Help:      "Rows the response encoders consumed and the ingest pushed, by format.",
	}, []string{"plane", "format", "direction"})
	m.query = histogram("query_duration_seconds", "Duration of a query.", m.userLabels("plane", "engine")...)
	reg.MustRegister(m.request, m.rpc, m.firstByte, m.bytes, m.rows, m.query)
	return m
}

// ObserveRequest records one data route's duration on the HTTP plane.
func (m *Metrics) ObserveRequest(method, route string, code int, d time.Duration) {
	m.request.WithLabelValues(method, route, strconv.Itoa(code)).Observe(d.Seconds())
}

// ObserveRPC records one Flight RPC's duration.
func (m *Metrics) ObserveRPC(method string, code codes.Code, d time.Duration) {
	m.rpc.WithLabelValues(method, code.String()).Observe(d.Seconds())
}

// ObserveFirstByte records the time from a handler's entry to its first
// body byte or record batch.
func (m *Metrics) ObserveFirstByte(p Plane, f Format, d time.Duration) {
	m.firstByte.WithLabelValues(string(p), string(f)).Observe(d.Seconds())
}

// AddBytes adds bytes an encoder produced to the format's total.
func (m *Metrics) AddBytes(p Plane, f Format, n int64) {
	m.bytes.WithLabelValues(string(p), string(f)).Add(float64(n))
}

// AddRows adds n rows of format f moving in direction d to the plane's
// total.
func (m *Metrics) AddRows(p Plane, f Format, d Direction, n int64) {
	m.rows.WithLabelValues(string(p), string(f), string(d)).Add(float64(n))
}

// ObserveQuery records one query's duration as user ran it on engine e.
// The user is dropped when series carry no user.
func (m *Metrics) ObserveQuery(p Plane, e Engine, user string, d time.Duration) {
	labels := []string{string(p), string(e)}
	// An insecure cluster accepts any username, so a user label there
	// would make the series unbounded.
	if m.PerUser {
		labels = append(labels, user)
	}
	m.query.WithLabelValues(labels...).Observe(d.Seconds())
}

// metricsKey is the context key for the metrics. It is unexported so
// only WithMetrics and MetricsFrom touch it.
type metricsKey struct{}

// WithMetrics returns a context that carries the metrics.
func WithMetrics(ctx context.Context, m *Metrics) context.Context {
	return context.WithValue(ctx, metricsKey{}, m)
}

// MetricsFrom returns the metrics the context carries. A context
// without them is a programming error, and MetricsFrom panics on it, so
// a forgotten placement fails at the first scrape instead of serving an
// empty exposition.
func MetricsFrom(ctx context.Context) *Metrics {
	m, ok := ctx.Value(metricsKey{}).(*Metrics)
	if !ok {
		// A no-op fallback was rejected, because a forgotten placement in
		// main would then serve an empty /metrics without failing.
		panic("observe: no metrics in context")
	}
	return m
}
