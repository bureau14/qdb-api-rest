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
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
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

// fixture is one cluster of the test services: a binding to create
// tables through, the user they are created as, and the credentials an
// instance attaches with as that user.
type fixture struct {
	cluster *qdb.Cluster
	user    qdb.User
	creds   olap.Credentials
}

func insecure(t *testing.T) fixture {
	t.Helper()
	c := cluster.NewInsecure(t)
	host, port := hostPort(t, qdbtest.InsecureURI)
	return fixture{cluster: c, creds: olap.Credentials{Host: host, Port: port}}
}

func secure(t *testing.T) fixture {
	t.Helper()
	c := cluster.NewSecure(t)
	host, port := hostPort(t, qdbtest.SecureURI)
	name, key := qdbtest.SecureUser(t)
	pub, err := os.ReadFile(qdbtest.ClusterPublicKeyFile())
	if err != nil {
		t.Fatal(err)
	}
	return fixture{
		cluster: c,
		user:    qdb.User{Username: name, SecretKey: key},
		creds: olap.Credentials{
			Host: host, Port: port,
			Username: name, SecretKey: key,
			ClusterPublicKey: strings.TrimSpace(string(pub)),
		},
	}
}

// TestQueryReadsGeneratedTable: on each cluster, a generated table read
// through qdb.<name> on an instance attached as that cluster's user
// answers the rows written, and a statement DuckDB cannot parse fails
// before the sink runs.
func TestQueryReadsGeneratedTable(t *testing.T) {
	// 1. The insecure cluster runs as the anonymous user and the secure
	//    one as its test user, so the secure attach proves the secret's
	//    credentials.
	for name, fix := range map[string]func(*testing.T) fixture{"insecure": insecure, "secure": secure} {
		t.Run(name, func(t *testing.T) {
			f := fix(t)
			// 2. One instance serves every iteration, because qdb-duck
			//    holds up to 100 qdbd sessions per attach and the test
			//    cluster punishes exhaustion.
			in := open(t, f.creds)
			rapid.Check(t, func(rt *rapid.T) {
				// 3. A generated table read back in $timestamp order. Its
				//    rows are few enough to fit in one DuckDB vector, so
				//    the result is one batch.
				tbl := table.Generate(rt)
				table.CreateAs(rt, f.cluster, f.user, tbl)
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
}
