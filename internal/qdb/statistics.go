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

// ErrNodeNotFound means no endpoint of the cluster has the address
// asked for.
var ErrNodeNotFound = errors.New("qdb: no such node")

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

// hasEndpoint reports whether one of the endpoints has the address addr.
func hasEndpoint(endpoints []qdbapi.Endpoint, addr string) bool {
	for _, e := range endpoints {
		if e.URI() == addr {
			return true
		}
	}
	return false
}

// NodeStatistics answers the statistics of the node at addr, an
// <ip>:<port>, as the user. An addr that is no endpoint of the cluster is
// ErrNodeNotFound, and a node it cannot connect to is ErrNodeUnreachable.
// It is retried once on a retryable failure, as a read is.
func (c *Cluster) NodeStatistics(ctx context.Context, u User, addr string) (NodeStats, error) {
	// 1. read the cluster's endpoints on the leased session
	// 2. refuse an addr no endpoint has
	// 3. read the node's statistics on the same session

	var st qdbapi.Statistics
	err := c.ObservedCall(ctx, u, OpNodeStatistics, func(s *Session) error {
		endpoints, err := s.session.Cluster().Endpoints()
		if err != nil {
			return err
		}
		// 2. The check comes before the connect, so a caller cannot make the
		//    server dial an address of its choosing.
		if !hasEndpoint(endpoints, addr) {
			return fmt.Errorf("%w: %q", ErrNodeNotFound, addr)
		}
		// 3. The read shares the session of step 1, because one checkout per
		//    request is what the pool and the budget are built around.
		st, err = s.session.NodeStatistics(addr)
		return nodeError(err)
	}, WithReadRetry())
	if err != nil {
		return NodeStats{}, err
	}
	return NodeStats(st), nil
}
