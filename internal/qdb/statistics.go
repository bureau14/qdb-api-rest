package qdb

import (
	"context"
	"errors"
	"fmt"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// NodeStats is one node's statistics as the binding reads them under
// $qdb.statistics., with the binding's JSON tags.
type NodeStats qdbapi.Statistics

// ErrNodeUnreachable means the server could not open a direct connection
// to a node.
var ErrNodeUnreachable = errors.New("qdb: node unreachable")

// nodeError marks the binding's failed direct connection as
// ErrNodeUnreachable and passes every other error through.
func nodeError(err error) error {
	// A direct connection is the binding's one statistics path that fails
	// with a plain error, and every read after it fails with an ErrorType.
	var e qdbapi.ErrorType
	if err == nil || errors.As(err, &e) {
		return err
	}
	// The sentinel is not IsClusterUnavailable, so one down node does not
	// open the breaker for every route.
	return fmt.Errorf("%w: %v", ErrNodeUnreachable, err)
}

// Statistics answers every node's statistics as the user, keyed by the
// node's address. A node it cannot connect to fails the whole call with
// ErrNodeUnreachable. It is retried once on a retryable failure, as a
// read is.
func (c *Cluster) Statistics(ctx context.Context, u User) (map[string]NodeStats, error) {
	var stats map[string]qdbapi.Statistics
	err := c.ObservedCall(ctx, u, OpStatistics, func(s *Session) error {
		var err error
		stats, err = s.session.Statistics()
		return nodeError(err)
	}, WithReadRetry())
	if err != nil {
		return nil, err
	}
	nodes := make(map[string]NodeStats, len(stats))
	for addr, st := range stats {
		nodes[addr] = NodeStats(st)
	}
	return nodes, nil
}
