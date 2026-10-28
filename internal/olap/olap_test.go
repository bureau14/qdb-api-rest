// The DuckDB tests run against the live qdbd fixture. A generated table,
// read through qdb.<name> on an instance attached as the cluster's user,
// answers the rows that were written. This is an external test package,
// because the table fixture imports internal/qdb, which imports
// internal/olap.
package olap_test

import (
	"context"
	"errors"
	"iter"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

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

// open opens an instance with the fixture's configuration and the
// credentials, and closes it when the test ends. The fixture's Config is
// the one copy of the test binding, so the olap tests open with the
// duckdb and cluster blocks every other test runs under.
func open(t *testing.T, f cluster.Fixture, c olap.Credentials) *olap.Instance {
	t.Helper()
	cfg := f.Spec.Config(t)
	in, err := olap.Open(context.Background(), cfg.DuckDB, cfg.Cluster, c)
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

// credentialsOf is what an instance attaches with as the fixture's user:
// the host and port of the cluster URI, and on the secure cluster the
// user's credentials and the cluster public key.
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
		in := open(t, f, credentialsOf(t, f))
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

// readerSchema answers the schema Query's reader answers for a query.
func readerSchema(t table.T, in *olap.Instance, q string) *arrow.Schema {
	t.Helper()
	var sc *arrow.Schema
	err := in.Query(context.Background(), q, func(seq iter.Seq2[arrow.RecordBatch, error]) error {
		for rec, err := range seq {
			if err != nil {
				return err
			}
			sc = rec.Schema()
		}
		return nil
	})
	if err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return sc
}

// enumOf is an ENUM type of n values, v0 to v<n-1>.
func enumOf(n int) string {
	vs := make([]string, n)
	for i := range vs {
		vs[i] = "'v" + strconv.Itoa(i) + "'"
	}
	return "ENUM(" + strings.Join(vs, ", ") + ")"
}

// TestDescribe: on each cluster, Describe answers the schema of Query's
// reader for one query per row of the conversion table and for a
// generated table. Two statements, a ? marker and a GEOMETRY column
// answer their errors.
func TestDescribe(t *testing.T) {
	// The map in describe.go copies DuckDB's Arrow conversion, and this test
	// keeps the two equal:
	//
	//  1. open one instance per cluster;
	//  2. describe one cast per row of the conversion table;
	//  3. describe a generated table;
	//  4. describe the texts the describe refuses.
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		// 1. one instance per cluster, opened as in
		//    TestQueryReadsGeneratedTable
		in := open(t, f, credentialsOf(t, f))
		// 2. one cast per row of the conversion table, nested types, a
		//    TIMESTAMPTZ and both ENUM index widths included. Schema.Equal
		//    compares the field metadata too.
		exprs := []string{
			"true", "1::TINYINT", "1::SMALLINT", "1::INTEGER", "1::BIGINT",
			"1::UTINYINT", "1::USMALLINT", "1::UINTEGER", "1::UBIGINT",
			"1::FLOAT", "1::DOUBLE", "1::HUGEINT", "1::UHUGEINT",
			"1::DECIMAL(4, 1)", "1::DECIMAL(18, 3)", "1::DECIMAL(38, 10)",
			"'a'::VARCHAR", "gen_random_uuid()", "'a'::BLOB", "'101'::BIT",
			"DATE '2026-01-01'", "TIME '01:02:03'", "'01:02:03+01'::TIMETZ",
			"TIMESTAMP '2026-01-01 01:02:03'", "'2026-01-01'::TIMESTAMP_S",
			"'2026-01-01'::TIMESTAMP_MS", "'2026-01-01'::TIMESTAMP_NS",
			"'2026-01-01 01:02:03+00'::TIMESTAMPTZ", "INTERVAL 1 DAY", "NULL",
			"[1, 2]", "[1, 2]::INTEGER[2]", "{'a': 1, 'b': 'x'}",
			"MAP {'k': 1}", "union_value(n := 1)::UNION(n INTEGER, s VARCHAR)",
			"'v1'::" + enumOf(3), "'v1'::" + enumOf(300),
			"[TIMESTAMPTZ '2026-01-01 00:00:00+00']",
		}
		for _, e := range exprs {
			q := "SELECT " + e + " AS c"
			got, err := in.Describe(context.Background(), q)
			if err != nil {
				t.Errorf("describe %q: %v", q, err)
				continue
			}
			if want := readerSchema(t, in, q); !got.Equal(want) {
				t.Errorf("describe %q:\n got  %v\n want %v", q, got, want)
			}
		}
		// 3. a generated table, whose columns carry the types qdb-duck reads
		//    from the cluster
		rapid.Check(t, func(rt *rapid.T) {
			tbl := table.Generate(rt)
			table.CreateAs(rt, f.Cluster, f.User, tbl)
			got, err := in.Describe(context.Background(), tbl.SelectSQL())
			if err != nil {
				rt.Fatal(err)
			}
			if want := readerSchema(rt, in, tbl.SelectSQL()); !got.Equal(want) {
				rt.Fatalf("describe:\n got  %v\n want %v", got, want)
			}
		})
		// 4. the refusals answer their errors
		refusals := []struct {
			q    string
			want error
		}{
			{"SELECT 1; SELECT 2", olap.ErrNotOneStatement},
			{"", olap.ErrNotOneStatement},
			{"SELECT ?", olap.ErrParameters},
			{"SELECT 'POINT (1 2)'::GEOMETRY AS g", olap.ErrUndescribable},
		}
		for _, r := range refusals {
			if _, err := in.Describe(context.Background(), r.q); !errors.Is(err, r.want) {
				t.Errorf("describe %q = %v, want %v", r.q, err, r.want)
			}
		}
	})
}
