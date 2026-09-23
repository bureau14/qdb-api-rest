// The table read is pinned against the live qdbd fixture: a generated
// table, read back batch by batch, answers the rows that were written,
// whole or as a requested subset of columns. This is the package's one
// black-box test: the table fixture imports internal/qdb, so a white-box
// test could not use it.
package qdb_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// concat is the batches joined into one the caller owns, per column.
func concat(t *rapid.T, schema *arrow.Schema, batches []arrow.RecordBatch) arrow.RecordBatch {
	t.Helper()
	cols := make([]arrow.Array, schema.NumFields())
	rows := int64(0)
	for i := range cols {
		parts := make([]arrow.Array, len(batches))
		for j, b := range batches {
			parts[j] = b.Column(i)
		}
		var err error
		if cols[i], err = array.Concatenate(parts, memory.DefaultAllocator); err != nil {
			t.Fatalf("concatenate column %d: %v", i, err)
		}
		defer cols[i].Release()
	}
	for _, b := range batches {
		rows += b.NumRows()
	}
	return array.NewRecordBatch(schema, cols, rows)
}

// read reads name under o as the anonymous user into one batch the caller
// owns, asserting that no batch exceeded BatchRows and that a batch is
// what the sequence lends: retained here, since the step releases it.
func read(t *rapid.T, c *qdb.Cluster, name string, o qdb.ReadOptions) arrow.RecordBatch {
	t.Helper()
	var batches []arrow.RecordBatch
	err := c.Read(context.Background(), qdb.User{}, name, o, func(seq qdb.Batches) error {
		for rec, err := range seq {
			if err != nil {
				return err
			}
			if rec.NumRows() > int64(o.BatchRows) {
				t.Fatalf("a batch of %d rows, at most %d requested", rec.NumRows(), o.BatchRows)
			}
			rec.Retain()
			batches = append(batches, rec)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	defer func() {
		for _, b := range batches {
			b.Release()
		}
	}()
	// An empty table is one schema-only batch, so there is always a schema.
	return concat(t, batches[0].Schema(), batches)
}

// names is the column names of rec's schema, in order.
func names(rec arrow.RecordBatch) []string {
	out := make([]string, rec.NumCols())
	for i, f := range rec.Schema().Fields() {
		out[i] = f.Name
	}
	return out
}

// TestReadAnswersRowsWritten: a generated table read back in batches of
// a drawn size answers $table, $timestamp and every column as written;
// a drawn subset of names answers exactly those, in that order. A drawn
// row count of zero is the schema-only batch.
func TestReadAnswersRowsWritten(t *testing.T) {
	c := cluster.NewInsecure(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, c, tbl)
		// A batch size below the row count is what makes the read span
		// fetches.
		o := qdb.ReadOptions{BatchRows: rapid.IntRange(1, 16).Draw(rt, "batch rows")}
		whole := read(rt, c, tbl.Name, o)
		defer whole.Release()
		all := []string{"$table", "$timestamp"}
		for _, col := range tbl.Columns {
			all = append(all, col.Name)
		}
		if got := names(whole); !slices.Equal(got, all) {
			rt.Fatalf("columns %v read back, want %v", got, all)
		}
		table.Check(rt, tbl, whole)

		// A subset in a drawn order, the specials in the draw like any name.
		o.Columns = rapid.Permutation(all).Draw(rt, "order")[:rapid.IntRange(1, len(all)).Draw(rt, "picked")]
		subset := read(rt, c, tbl.Name, o)
		defer subset.Release()
		if got := names(subset); !slices.Equal(got, o.Columns) {
			rt.Fatalf("columns %v read back, want %v", got, o.Columns)
		}
		table.Check(rt, tbl, subset)
	})
}

// TestReadDropsTrailingNUL pins a C API defect, sc-19829: the bulk
// reader's Arrow path drops one trailing NUL byte from a string cell,
// while the query answers the byte. When this test fails, the C API has
// been fixed: delete the test and let the fixture's drawText draw NUL.
func TestReadDropsTrailingNUL(t *testing.T) {
	c := cluster.NewInsecure(t)
	data := qdbapi.NewColumnDataString([]string{"x\x00"})
	tbl := table.Table{
		Name:    "qdbtest_trailing_nul",
		Columns: []table.Column{{Name: "c0", Type: qdbapi.TsColumnString, Data: &data, Valid: []bool{true}}},
		Index:   []time.Time{time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	table.Create(t, c, tbl)
	// A string value aliases the batch's buffer, which a lent batch frees
	// when the step returns, so the read copies it out.
	cell := func(rec arrow.RecordBatch) string { return strings.Clone(rec.Column(0).(*array.String).Value(0)) }
	queried, err := c.Query(context.Background(), qdb.User{}, "SELECT c0 FROM "+tbl.Name)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer queried.Release()
	var read string
	err = c.Read(context.Background(), qdb.User{}, tbl.Name, qdb.ReadOptions{Columns: []string{"c0"}}, func(seq qdb.Batches) error {
		for rec, err := range seq {
			if err != nil {
				return err
			}
			read = cell(rec)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if cell(queried) != "x\x00" || read != "x" {
		t.Fatalf("query answered %q and the read %q; sc-19829 fixed? delete this test", cell(queried), read)
	}
}
