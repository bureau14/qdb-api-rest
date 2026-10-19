// Package cluster is the cluster fixture. Bind binds a *qdb.Cluster to one
// cluster of the test services for a test's life, and Each runs a test once
// per cluster. It is a subpackage for the same reason as table.
// internal/qdb's own tests import qdbtest, so qdbtest itself cannot import
// internal/qdb.
package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

// Fixture is one cluster of the test services bound for a test's life:
// the cluster as data, the bound *qdb.Cluster, and the user a test acts
// as there.
type Fixture struct {
	Spec    qdbtest.Cluster
	Cluster *qdb.Cluster
	User    qdb.User
}

// Bind binds spec for t's life under the default configuration as edit
// leaves it. A nil edit keeps the defaults.
func Bind(t testing.TB, spec qdbtest.Cluster, edit func(*config.Config)) Fixture {
	t.Helper()
	cfg := spec.Config(t)
	if edit != nil {
		edit(&cfg)
	}
	return Fixture{
		Spec:    spec,
		Cluster: bind(t, cfg),
		User:    qdb.User{Username: spec.Username, SecretKey: spec.SecretKey},
	}
}

// Each runs test once per cluster of the test services, as a subtest
// named by the cluster, each with its own bound cluster.
func Each(t *testing.T, test func(t *testing.T, f Fixture)) {
	t.Helper()
	EachWith(t, nil, test)
}

// EachWith is Each under a configuration edit, for a test that needs a
// smaller limit.
func EachWith(t *testing.T, edit func(*config.Config), test func(t *testing.T, f Fixture)) {
	t.Helper()
	for _, spec := range qdbtest.Clusters(t) {
		t.Run(spec.Name, func(t *testing.T) { test(t, Bind(t, spec, edit)) })
	}
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
