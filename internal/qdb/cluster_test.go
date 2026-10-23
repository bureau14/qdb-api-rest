// This file holds the integration tests of Cluster against the live qdbd
// fixture (internal/qdbtest): breaker, per-user cap, retry-once, a
// rejected query's session and breaker, user-pool eviction, and the
// probe as the REST API's own user, the table list, and the slicing of a
// query result, which dials nothing. Every test that dials the fixture
// runs once per cluster, as the user the fixture names there, except
// TestAuthenticate, which checks each cluster's own verdict. The tests
// against an unreachable URI dial no fixture cluster. The pool's own
// invariants and the error predicates are pinned upstream in qdb-api-go.
package qdb

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// userOf is the user a test acts as on spec.
func userOf(spec qdbtest.Cluster) User {
	return User{Username: spec.Username, SecretKey: spec.SecretKey}
}

// eachCluster runs test once per cluster of the test services, as a
// subtest named by the cluster. The cluster fixture cannot serve this
// package, because it imports internal/qdb.
func eachCluster(t *testing.T, test func(t *testing.T, spec qdbtest.Cluster)) {
	t.Helper()
	for _, spec := range qdbtest.Clusters(t) {
		t.Run(spec.Name, func(t *testing.T) { test(t, spec) })
	}
}

// closeCluster drains a cluster within a bounded context.
func closeCluster(t *testing.T, c *Cluster) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Close(ctx); err != nil {
		t.Errorf("close: %v", err)
	}
}

// anonymous names the anonymous user, which the unreachable cluster and
// the insecure cluster's login see.
var anonymous = User{}

// TestRejectedQueryReusesSession: the cluster rejects a malformed query,
// and a rejection is an answer, so the session goes back to the pool and
// the breaker counts a success.
func TestRejectedQueryReusesSession(t *testing.T) {
	eachCluster(t, func(t *testing.T, spec qdbtest.Cluster) {
		c := New(spec.Config(t), nil)
		defer closeCluster(t, c)
		u := userOf(spec)

		err := c.Query(context.Background(), u, "NOT A QUERY", func(Batches) error { return nil })
		if err == nil {
			t.Fatal("want an error for a malformed query")
		}
		if qdbapi.IsBadSession(err) {
			t.Fatalf("a malformed query should not condemn the session: %v", err)
		}
		if s := c.poolFor(u).Stats(); s.Idle != 1 {
			t.Fatalf("rejected query did not return the session: %+v", s)
		}
		if st, n := c.breaker.state, c.breaker.failures; st != breakerClosed || n != 0 {
			t.Fatalf("rejected query fed the breaker: state %d, failures %d", st, n)
		}
	})
}

// TestPerUserCapAndSharing: one user's concurrent calls never exceed the
// per-user cap, and two User values with the same name share one pool.
func TestPerUserCapAndSharing(t *testing.T) {
	eachCluster(t, func(t *testing.T, spec qdbtest.Cluster) {
		cfg := spec.Config(t)
		cfg.Pool.PerUserMax = 2
		cfg.Pool.MaxSessions = 8
		c := New(cfg, nil)
		defer closeCluster(t, c)
		u := userOf(spec)

		var wg sync.WaitGroup
		for range 6 {
			wg.Go(func() {
				// The cap is checked while the session is held, so the check runs
				// inside Call, because Query has already returned the session by
				// the time it answers.
				err := c.Call(context.Background(), u, func(s *Session) error {
					rec, err := s.fetch("SELECT 1")
					if rec != nil {
						rec.Release()
					}
					if st := c.poolFor(u).Stats(); st.InUse > 2 {
						t.Errorf("per-user cap exceeded: %+v", st)
					}
					return err
				})
				if err != nil {
					t.Errorf("query: %v", err)
				}
			})
		}
		wg.Wait()
		if s := c.Stats(); s.Users != 1 {
			t.Fatalf("one user should hold one pool, got %d", s.Users)
		}
	})
}

// TestBreakerOpensOnUnreachable: dialing an unreachable cluster fails,
// and after the threshold the breaker fails fast with a Retry-After.
func TestBreakerOpensOnUnreachable(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1" // connection refused, fast
	cfg.Pool.Breaker.Failures = 2
	cfg.Pool.Breaker.OpenFor = time.Minute
	c := New(cfg, nil)
	defer closeCluster(t, c)

	ctx := context.Background()
	noop := func(*Session) error { return nil }
	for range cfg.Pool.Breaker.Failures {
		if err := c.Call(ctx, anonymous, noop); err == nil {
			t.Fatal("want a dial error against an unreachable cluster")
		}
	}
	err := c.Call(ctx, anonymous, noop)
	var open *BreakerOpenError
	if !errors.As(err, &open) {
		t.Fatalf("breaker did not open: %v", err)
	}
	if open.RetryAfter <= 0 || open.RetryAfter > time.Minute {
		t.Fatalf("Retry-After out of range: %s", open.RetryAfter)
	}
}

// TestRetryOnceOnRetryableFailure: a call that always fails retryably
// is attempted exactly twice with WithReadRetry.
func TestRetryOnceOnRetryableFailure(t *testing.T) {
	eachCluster(t, func(t *testing.T, spec qdbtest.Cluster) {
		c := New(spec.Config(t), nil)
		defer closeCluster(t, c)

		attempts := 0
		err := c.Call(context.Background(), userOf(spec), func(*Session) error {
			attempts++
			return qdbapi.ErrConnectionReset // retryable
		}, WithReadRetry())
		if !errors.Is(err, qdbapi.ErrConnectionReset) {
			t.Fatalf("want the retryable error back, got %v", err)
		}
		if attempts != 2 {
			t.Fatalf("want exactly two attempts, got %d", attempts)
		}
	})
}

// TestProbeAsOwnUser: the readiness probe dials each cluster as the REST
// API's own user and runs the query. On the insecure cluster the own user
// is anonymous, because no user security file is configured.
func TestProbeAsOwnUser(t *testing.T) {
	eachCluster(t, func(t *testing.T, spec qdbtest.Cluster) {
		c := New(spec.Config(t), nil)
		defer closeCluster(t, c)

		if err := c.Probe(context.Background()); err != nil {
			t.Fatalf("probe: %v", err)
		}
	})
}

// TestAuthenticate: the secure cluster accepts its user and refuses a
// wrong secret with ErrCredentialsRefused. The refusal is an answer, so
// it leaves the breaker closed and creates no pool. The anonymous user
// passes the insecure cluster.
func TestAuthenticate(t *testing.T) {
	c := New(qdbtest.Secure(t).Config(t), nil)
	defer closeCluster(t, c)

	// 1. The secure cluster accepts the fixture's test user.
	ctx := context.Background()
	u := userOf(qdbtest.Secure(t))
	if err := c.Authenticate(ctx, u); err != nil {
		t.Fatalf("the fixture user refused: %v", err)
	}
	// 2. It refuses the same user with a wrong secret. The secret is
	//    base64 for "not-a-key", so it is well formed, and only the
	//    cluster can reject it.
	err := c.Authenticate(ctx, User{Username: u.Username, SecretKey: "bm90LWEta2V5"})
	if !errors.Is(err, ErrCredentialsRefused) || qdbapi.IsClusterUnavailable(err) {
		t.Fatalf("want a refusal from a reachable cluster, got %v", err)
	}
	// 3. Neither login left a pool behind, because Authenticate dials
	//    outside the user pools.
	if got := c.Stats(); got.Users != 0 {
		t.Fatalf("a login left %d pool(s) behind", got.Users)
	}
	// 4. The breaker stays closed, because a refusal is an answer from a
	//    cluster that is up.
	if _, ok := c.breaker.allow(); !ok {
		t.Fatal("a refused credential opened the breaker")
	}

	// 5. The insecure cluster accepts the anonymous user.
	i := New(qdbtest.Insecure().Config(t), nil)
	defer closeCluster(t, i)
	if err := i.Authenticate(ctx, anonymous); err != nil {
		t.Fatalf("anonymous refused by the insecure cluster: %v", err)
	}
}

// TestAuthenticateFailsFastWhenOpen: an unreachable cluster opens the
// breaker through logins too, and the next login fails at once.
func TestAuthenticateFailsFastWhenOpen(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.URI = "qdb://127.0.0.1:1"
	cfg.Pool.Breaker.Failures = 2
	cfg.Pool.Breaker.OpenFor = time.Minute
	c := New(cfg, nil)
	defer closeCluster(t, c)

	ctx := context.Background()
	for range cfg.Pool.Breaker.Failures {
		if err := c.Authenticate(ctx, anonymous); err == nil {
			t.Fatal("want a dial error against an unreachable cluster")
		}
	}
	var open *BreakerOpenError
	if err := c.Authenticate(ctx, anonymous); !errors.As(err, &open) {
		t.Fatalf("breaker did not open: %v", err)
	}
}

// fakeClock is a manually advanced clock for eviction and lifetime tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// TestIdleUserPoolEvicted: a user pool that has held no session for
// idle_timeout is reaped away, so the map is bounded by distinct users,
// not by logins.
func TestIdleUserPoolEvicted(t *testing.T) {
	eachCluster(t, func(t *testing.T, spec qdbtest.Cluster) {
		clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
		cfg := spec.Config(t)
		cfg.Pool.IdleTimeout = time.Minute
		cfg.Pool.MaxLifetime = time.Hour
		c := New(cfg, clk.Now)
		defer closeCluster(t, c)
		u := userOf(spec)

		if err := c.Query(context.Background(), u, "SELECT 1", func(Batches) error { return nil }); err != nil {
			t.Fatalf("query: %v", err)
		}
		if s := c.Stats(); s.Users != 1 {
			t.Fatalf("want one user pool after a query, got %d", s.Users)
		}

		// Past idle_timeout the pool's own reaper closes the idle session. The
		// test runs that pass itself instead of waiting for the tick.
		clk.advance(2 * time.Minute)
		up := c.poolFor(u)
		up.Reap()
		deadline := time.Now().Add(10 * time.Second)
		for s := up.Stats(); s.Idle != 0 || s.Closing != 0; s = up.Stats() {
			if time.Now().After(deadline) {
				t.Fatalf("idle session never closed: %+v", s)
			}
			time.Sleep(2 * time.Millisecond)
		}

		// One reap marks the pool empty, and the next reap past idle_timeout
		// evicts it.
		c.Reap()
		clk.advance(2 * time.Minute)
		c.Reap()
		if s := c.Stats(); s.Users != 0 {
			t.Fatalf("idle user pool was not evicted: %d users remain", s.Users)
		}
	})
}

// TestSlices: for a batch of any row count and any n, slices yields
// ceil(rows/n) slices of at most n rows, at least one, in order, each
// equal to the parent's rows at that offset, and releases each after
// yield. It dials nothing.
func TestSlices(t *testing.T) {
	// One property with a release check:
	//
	//  1. draw rows and n, and build a one-column int64 batch of rows on
	//     a checked allocator;
	//  2. collect the slices' row counts and values through the sequence;
	//  3. compare counts and values; a batch of no rows yields one empty
	//     slice;
	//  4. the allocator holds nothing once the parent is released, which
	//     proves every slice was released, because a slice retains the
	//     parent.
	rapid.Check(t, func(t *rapid.T) {
		// 1. the batch
		rows := rapid.IntRange(0, 1000).Draw(t, "rows")
		n := rapid.Int64Range(1, 300).Draw(t, "n")
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		b := array.NewInt64Builder(mem)
		for i := range rows {
			b.Append(int64(i))
		}
		col := b.NewArray()
		b.Release()
		schema := arrow.NewSchema([]arrow.Field{{Name: "v", Type: arrow.PrimitiveTypes.Int64}}, nil)
		rec := array.NewRecordBatch(schema, []arrow.Array{col}, int64(rows))
		col.Release()

		// 2. the slices
		var counts []int64
		var values []int64
		for s, err := range slices(rec, n) {
			if err != nil {
				t.Fatal(err)
			}
			counts = append(counts, s.NumRows())
			values = append(values, s.Column(0).(*array.Int64).Int64Values()...)
		}

		// 3. ceil(rows/n) slices of at most n rows, and one for no rows
		want := (int64(rows) + n - 1) / n
		if rows == 0 {
			want = 1
		}
		if int64(len(counts)) != want {
			t.Fatalf("rows %d, n %d: want %d slices, got %v", rows, n, want, counts)
		}
		for i, c := range counts {
			if c > n || (c == 0 && rows != 0) {
				t.Fatalf("slice %d has %d rows, n is %d", i, c, n)
			}
		}
		for i, v := range values {
			if v != int64(i) {
				t.Fatalf("value %d is %d", i, v)
			}
		}

		// 4. every slice released
		rec.Release()
		mem.AssertSize(t, 0)
	})
}

// contains reports whether names holds name. The standard library's
// slices package is shadowed by this package's slices function.
func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// TestTables: a table created as the user appears in Tables on both
// clusters and is gone from it once removed.
func TestTables(t *testing.T) {
	eachCluster(t, func(t *testing.T, spec qdbtest.Cluster) {
		c := New(spec.Config(t), nil)
		defer closeCluster(t, c)
		u := userOf(spec)
		ctx := context.Background()

		// The name is qdbtest_ plus a fresh suffix, so a killed run is found
		// by the qdbsh sweep internal/AGENTS.md, Tests, names.
		name := fmt.Sprintf("qdbtest_tables_%d", time.Now().UnixNano())
		// One double column and a remove on cleanup, so the test needs no
		// fixture, which package qdb cannot import.
		if err := c.CreateTable(ctx, u, name, time.Hour, []Column{{Name: "v", Type: "double"}}); err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Cleanup(func() { _ = c.RemoveTable(context.Background(), u, name) })

		names, err := c.Tables(ctx, u)
		if err != nil {
			t.Fatalf("tables: %v", err)
		}
		if !contains(names, name) {
			t.Fatalf("want %s in %v", name, names)
		}
		if err := c.RemoveTable(ctx, u, name); err != nil {
			t.Fatalf("remove: %v", err)
		}
		names, err = c.Tables(ctx, u)
		if err != nil {
			t.Fatalf("tables after remove: %v", err)
		}
		if contains(names, name) {
			t.Fatalf("want %s gone from %v", name, names)
		}
	})
}

// TestTableSchemas: a table created as the user with one column of every
// type appears in TableSchemas on both clusters with $timestamp then its
// columns in the binding's Arrow types, the symtable on its symbol
// column, and the shard size and TTL it was created with, and every
// listed schema starts with $timestamp.
func TestTableSchemas(t *testing.T) {
	// Create, list, find and compare:
	//
	//  1. one table with every type word, a symtable on the symbol column,
	//     a one-hour shard, removed on cleanup;
	//  2. TableSchemas, then the entry with the table's name;
	//  3. the fields against a schema built here from the columns;
	//  4. the shard size and the TTL;
	//  5. every entry's first field.
	eachCluster(t, func(t *testing.T, spec qdbtest.Cluster) {
		c := New(spec.Config(t), nil)
		defer closeCluster(t, c)
		u := userOf(spec)
		ctx := context.Background()

		// 1. named qdbtest_ for the sweep, as TestTables names its table
		name := fmt.Sprintf("qdbtest_schemas_%d", time.Now().UnixNano())
		symtable := name + "_sym"
		cols := []Column{
			{Name: "b", Type: "blob"},
			{Name: "d", Type: "double"},
			{Name: "i", Type: "int64"},
			{Name: "s", Type: "string"},
			{Name: "y", Type: "symbol", Symtable: symtable},
			{Name: "t", Type: "timestamp"},
		}
		if err := c.CreateTable(ctx, u, name, time.Hour, cols); err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Cleanup(func() { _ = c.RemoveTable(context.Background(), u, name) })

		// 2. the listing, and the table's entry in it
		got, err := c.TableSchemas(ctx, u)
		if err != nil {
			t.Fatalf("table schemas: %v", err)
		}
		var schema *arrow.Schema
		for _, ts := range got {
			if ts.Name == name {
				schema = ts.Schema
			}
		}
		if schema == nil {
			t.Fatalf("want %s among %d schemas", name, len(got))
		}

		// 3. Schema.Equal compares the fields with their metadata, so the
		//    symtable key is checked here, and ignores the schema's own
		//    metadata, which step 4 reads
		fields := []arrow.Field{{Name: "$timestamp", Type: qdbapi.TsColumnTimestamp.ArrowType()}}
		for _, col := range cols {
			f := arrow.Field{Name: col.Name, Type: columnTypes[col.Type].ArrowType(), Nullable: true}
			if col.Type == "symbol" {
				f.Metadata = arrow.NewMetadata([]string{MetaSymtable}, []string{symtable})
			}
			fields = append(fields, f)
		}
		if want := arrow.NewSchema(fields, nil); !schema.Equal(want) {
			t.Fatalf("schema:\n got %v\nwant %v", schema, want)
		}

		// 4. the hour in milliseconds, and no TTL
		md := schema.Metadata()
		if v, ok := md.GetValue(MetaShardSize); !ok || v != "3600000" {
			t.Fatalf("%s: got %q, %v", MetaShardSize, v, ok)
		}
		if v, ok := md.GetValue(MetaTTL); !ok || v != "0" {
			t.Fatalf("%s: got %q, %v", MetaTTL, v, ok)
		}

		// 5. the $qdb. tables are described too, so every entry is a table
		//    the builder shaped
		for _, ts := range got {
			if ts.Schema.NumFields() == 0 || ts.Schema.Field(0).Name != "$timestamp" {
				t.Fatalf("%s: want $timestamp first, got %v", ts.Name, ts.Schema)
			}
		}
	})
}
