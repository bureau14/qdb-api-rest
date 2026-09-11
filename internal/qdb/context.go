package qdb

import "context"

// The cluster travels in the context next to the logger: main places it
// in the process context, the server hands that context to every
// request, and handlers read it from there.
type clusterKey struct{}

// WithCluster returns ctx carrying c.
func WithCluster(ctx context.Context, c *Cluster) context.Context {
	return context.WithValue(ctx, clusterKey{}, c)
}

// ClusterFrom returns the cluster carried by ctx and panics without one:
// a fresh context mid-call-chain is a programming error.
func ClusterFrom(ctx context.Context) *Cluster {
	c, ok := ctx.Value(clusterKey{}).(*Cluster)
	if !ok {
		panic("qdb: no cluster in context")
	}
	return c
}
