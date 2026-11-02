package observe

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

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
	buckets []float64
}

// NewMetrics builds the registry with the series every instance exposes.
// perUser decides once whether per-user series carry the user label.
func NewMetrics(cfg config.Metrics, perUser bool) *Metrics {
	// 1. the build-info series
	// 2. the Go runtime and process collectors
	// 3. the duration buckets from the config
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
	b := cfg.DurationBuckets
	return &Metrics{
		Registry:  reg,
		Namespace: cfg.Namespace,
		PerUser:   perUser,
		buckets:   prometheus.ExponentialBuckets(b.Start.Seconds(), 2, b.Count),
	}
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
