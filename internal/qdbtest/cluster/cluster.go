// Package cluster is the cluster fixture: New binds a *qdb.Cluster to the
// insecure qdbd the test services run, for the test's life. It is a
// subpackage for the same reason as table: internal/qdb's own tests
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

// New binds a cluster to the insecure fixture, fails fast when it is
// down, and closes the cluster on t's cleanup.
func New(t testing.TB) *qdb.Cluster {
	t.Helper()
	qdbtest.Require(t, qdbtest.InsecureURI)
	cfg := config.Default()
	cfg.Cluster.URI = qdbtest.InsecureURI
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
