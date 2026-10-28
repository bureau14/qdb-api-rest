package observe

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/bureau14/qdb-api-rest/internal/build"
)

// Metrics is the process's instrument registry: every series this
// binary exposes is registered on it, and GET /metrics serves it. It is
// built once by main and travels in the context like the logger
// (ADR-0019).
type Metrics struct {
	// Note: the registry is this process's own and not the client's
	// global default registry, because the brief forbids package-level
	// mutable state, and the global would make a second server in one
	// test binary register twice.
	Registry *prometheus.Registry
}

// namespace is the prefix of every series this binary names, so a host
// scraping several exporters does not collide. The client's own go_ and
// process_ series keep their conventional names (ADR-0019).
const namespace = "qdb_rest"

// NewMetrics builds the registry with the series every instance
// exposes: build info, the Go runtime and the process. Later units
// register their instruments on it.
func NewMetrics() *Metrics {
	// Three registrations, each with a choice of its own:
	//
	//  1. the build-info series, qdb_rest_build_info at 1, labelled with
	//     what main's starting line logs, so a dashboard reads what the
	//     log says;
	//  2. the Go runtime collector;
	//  3. the process collector.
	//
	// MustRegister, because the three names are fixed and a collision is
	// a programming error.
	reg := prometheus.NewRegistry()

	// 1. The labels are version, commit and build_mode from internal/build,
	//    the three of main's starting line. The client's own build-info
	//    collector was rejected, because it reports the Go module version
	//    and not the ldflags-injected one.
	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "build_info",
		Help:      "The version, commit and build mode of this binary, at 1.",
	}, []string{"version", "commit", "build_mode"})
	buildInfo.WithLabelValues(build.Version, build.Commit, build.BuildMode).Set(1)

	// 2. The Go runtime collector, because the brief asks for the runtime
	//    series.
	// 3. The process collector, which emits nothing where it cannot read
	//    the process (FreeBSD without procfs), so no platform fails.
	reg.MustRegister(buildInfo, collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &Metrics{Registry: reg}
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
