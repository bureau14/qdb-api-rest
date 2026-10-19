// The DuckDB query is pinned against the live qdbd fixture. A generated
// table, read through qdb.<name> on an instance attached as the cluster's
// user, answers the rows that were written. This is an external test
// package, because the table fixture imports internal/qdb, which imports
// internal/olap.
package olap_test

import (
	"context"
	"errors"
	"iter"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/olap"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// hostPort splits a single-node qdb:// URI into the host and the port
// an attach takes.
func hostPort(t *testing.T, uri string) (string, string) {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

// open opens an instance under the default duckdb block with c and
// closes it on t's cleanup.
func open(t *testing.T, c olap.Credentials) *olap.Instance {
	t.Helper()
	in, err := olap.Open(context.Background(), config.Default().DuckDB, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := in.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return in
}

// credentialsOf is what an instance attaches with as f's user: the host
// and port of f's URI, and on the secure cluster the user's credentials
// and the cluster public key.
func credentialsOf(t *testing.T, f cluster.Fixture) olap.Credentials {
	t.Helper()
	host, port := hostPort(t, f.Spec.URI)
	creds := olap.Credentials{Host: host, Port: port}
	if !f.Spec.Secured {
		return creds
	}
	pub, err := os.ReadFile(f.Spec.PublicKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	creds.Username, creds.SecretKey = f.User.Username, f.User.SecretKey
	creds.ClusterPublicKey = strings.TrimSpace(string(pub))
	return creds
}

// TestQueryReadsGeneratedTable: on each cluster, a generated table read
// through qdb.<name> on an instance attached as that cluster's user
// answers the rows written, and a statement DuckDB cannot parse fails
// before the sink runs.
func TestQueryReadsGeneratedTable(t *testing.T) {
	// 1. The insecure cluster runs as the anonymous user and the secure
	//    one as its test user, so the secure attach proves the secret's
	//    credentials.
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		// 2. One instance serves every iteration, because qdb-duck
		//    holds up to 100 qdbd sessions per attach and the test
		//    cluster punishes exhaustion.
		in := open(t, credentialsOf(t, f))
		rapid.Check(t, func(rt *rapid.T) {
			// 3. A generated table read back in $timestamp order. Its
			//    rows are few enough to fit in one DuckDB vector, so
			//    the result is one batch.
			tbl := table.Generate(rt)
			table.CreateAs(rt, f.Cluster, f.User, tbl)
			err := in.Query(context.Background(), tbl.SelectSQL(), func(seq iter.Seq2[arrow.RecordBatch, error]) error {
				for rec, err := range seq {
					if err != nil {
						return err
					}
					// 4. table.Check compares the batch with the rows
					//    written.
					table.Check(rt, tbl, rec)
				}
				return nil
			})
			if err != nil {
				rt.Fatal(err)
			}
		})
		// 5. A parse error arrives from Query, and the sink never
		//    runs.
		err := in.Query(context.Background(), "SELEC", func(iter.Seq2[arrow.RecordBatch, error]) error {
			return errors.New("sink ran")
		})
		if err == nil || !strings.Contains(err.Error(), "Parser Error") {
			t.Fatalf("parse error = %v", err)
		}
	})
}
