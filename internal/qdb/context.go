package qdb

import "context"

// context plumbing: the cluster travels next to the logger (ADR-0002's
// pattern), so handlers read it from the request context instead of
// taking it through a constructor.
type clusterKey struct{}

// WithCluster returns ctx carrying c.
func WithCluster(ctx context.Context, c *Cluster) context.Context {
	return context.WithValue(ctx, clusterKey{}, c)
}

// ClusterFrom returns the cluster carried by ctx, or nil.
func ClusterFrom(ctx context.Context) *Cluster {
	c, _ := ctx.Value(clusterKey{}).(*Cluster)
	return c
}
