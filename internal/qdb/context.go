package qdb

import "context"

// The cluster travels in the context next to the logger (ADR-0002's
// pattern): main places it in the process context, the server hands that
// context to every request, and handlers read it from there instead of
// taking it through a constructor.
type clusterKey struct{}

// WithCluster returns ctx carrying c.
func WithCluster(ctx context.Context, c *Cluster) context.Context {
	return context.WithValue(ctx, clusterKey{}, c)
}

// ClusterFrom returns the cluster carried by ctx. A ctx without one is a
// programming error (a context.Background() or TODO() mid-call-chain) and
// panics, as observe.Logger does: fail fast rather than serve without a
// cluster.
func ClusterFrom(ctx context.Context) *Cluster {
	c, ok := ctx.Value(clusterKey{}).(*Cluster)
	if !ok {
		panic("qdb: no cluster in context")
	}
	return c
}
