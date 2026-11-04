package qdb

import (
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"github.com/prometheus/client_golang/prometheus"
)

// Collector exposes the cluster's session budget, breaker, user pools
// and retry count to Prometheus, read at each scrape (ADR-0004), and the
// call histogram the cluster records into.
type Collector struct {
	c       *Cluster
	perUser bool
	budget  *prometheus.Desc
	breaker *prometheus.Desc
	pools   *prometheus.Desc
	retries *prometheus.Desc
}

// NewCollector builds the collector for c, naming its series under
// namespace. perUser adds the user label to the pool series.
func NewCollector(c *Cluster, namespace string, perUser bool) *Collector {
	poolLabels := []string{"state"}
	if perUser {
		poolLabels = []string{"user", "state"}
	}
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc(prometheus.BuildFQName(namespace, "", name), help, labels, nil)
	}
	return &Collector{
		c:       c,
		perUser: perUser,
		budget:  desc("budget_sessions", "Sessions of the process-wide budget: in use, and the maximum.", "state"),
		breaker: desc("breaker_state", "The circuit breaker's state, at 1 on the current one and 0 on the others.", "state"),
		pools:   desc("pool_sessions", "Sessions of the user pools by state.", poolLabels...),
		retries: desc("retries_total", "Second attempts of an idempotent read after a retryable failure."),
	}
}

// Describe sends the four descriptors and the call histogram's.
func (k *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- k.budget
	ch <- k.breaker
	ch <- k.pools
	ch <- k.retries
	k.c.calls.Describe(ch)
}

// poolStats returns each user's pool statistics, keyed by username.
func (k *Collector) poolStats() map[string]qdbapi.SessionPoolStats {
	// c.mu then the pool's own lock, the order takeIdle takes. duckMu is
	// never taken, because instanceOf takes it before c.mu.
	k.c.mu.Lock()
	defer k.c.mu.Unlock()
	out := make(map[string]qdbapi.SessionPoolStats, len(k.c.users))
	for name, up := range k.c.users {
		out[name] = up.pool.Stats()
	}
	return out
}

// sumPools adds the pools' statistics into one, state by state.
func sumPools(byUser map[string]qdbapi.SessionPoolStats) qdbapi.SessionPoolStats {
	var sum qdbapi.SessionPoolStats
	for _, s := range byUser {
		sum.InUse += s.InUse
		sum.Idle += s.Idle
		sum.Dialing += s.Dialing
		sum.Closing += s.Closing
	}
	return sum
}

// Collect sends one snapshot of the budget, the breaker, the pools and
// the retry count, and the call histogram.
func (k *Collector) Collect(ch chan<- prometheus.Metric) {
	// 1. the budget, in use and max
	// 2. the breaker, 1 on the current state and 0 on the others
	// 3. the pools, per user or summed, read under c.mu
	// 4. the retry count
	// 5. the call histogram, which the cluster records into between scrapes
	gauge := func(d *prometheus.Desc, v int, labels ...string) {
		ch <- prometheus.MustNewConstMetric(d, prometheus.GaugeValue, float64(v), labels...)
	}

	// 1. In use counts a session until its close returns, so it can
	//    exceed what the pools report while an evicted pool closes.
	gauge(k.budget, k.c.budget.inUse(), "in_use")
	gauge(k.budget, k.c.budget.max(), "max")

	// 2. One series per state at 0 or 1 is the Prometheus convention for
	//    an enum.
	current := k.c.breaker.state()
	for _, s := range []breakerState{breakerClosed, breakerOpen, breakerHalfOpen} {
		on := 0
		if s == current {
			on = 1
		}
		gauge(k.breaker, on, s.String())
	}

	// 3. An insecure cluster's usernames are the caller's choice, so its
	//    pools are summed and the series stay bounded.
	pool := func(s qdbapi.SessionPoolStats, user ...string) {
		gauge(k.pools, s.InUse, append(user, "in_use")...)
		gauge(k.pools, s.Idle, append(user, "idle")...)
		gauge(k.pools, s.Dialing, append(user, "dialing")...)
		gauge(k.pools, s.Closing, append(user, "closing")...)
	}
	byUser := k.poolStats()
	if k.perUser {
		for name, s := range byUser {
			pool(s, name)
		}
	} else {
		pool(sumPools(byUser))
	}

	// 4. The retry count only grows, so it is a counter and not a gauge.
	ch <- prometheus.MustNewConstMetric(k.retries, prometheus.CounterValue, float64(k.c.retries.Load()))

	k.c.calls.Collect(ch)
}
