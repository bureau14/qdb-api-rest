// Package cluster is the cluster fixture: NewInsecure binds a
// *qdb.Cluster to the insecure qdbd the test services run, NewSecure to
// the secure one, for the test's life. It is a subpackage for the same reason as table: internal/qdb's own tests
// import qdbtest, so qdbtest itself cannot import internal/qdb.
package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

// NewInsecure binds a cluster to the insecure fixture, fails fast when
// it is down, and closes the cluster on t's cleanup.
func NewInsecure(t testing.TB) *qdb.Cluster {
	t.Helper()
	qdbtest.Require(t, qdbtest.InsecureURI)
	cfg := config.Default()
	cfg.Cluster.URI = qdbtest.InsecureURI
	return bind(t, cfg)
}

// NewSecure binds a cluster to the secure fixture as its test user, the
// REST API's own user for the test's life.
func NewSecure(t testing.TB) *qdb.Cluster {
	t.Helper()
	qdbtest.Require(t, qdbtest.SecureURI)
	cfg := config.Default()
	cfg.Cluster.URI = qdbtest.SecureURI
	cfg.Cluster.PublicKeyFile = qdbtest.ClusterPublicKeyFile()
	cfg.Cluster.UserSecurityFile = qdbtest.UserSecurityFile()
	return bind(t, cfg)
}

// bind builds the cluster from cfg and closes it on t's cleanup.
func bind(t testing.TB, cfg config.Config) *qdb.Cluster {
	t.Helper()
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.Close(ctx); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return c
}
