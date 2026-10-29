package main

import (
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// concatColumn concatenates the given row ranges of the column, in order.
// The caller releases the result.
func concatColumn(t *rapid.T, col arrow.Array, ranges [][2]int64) arrow.Array {
	parts := make([]arrow.Array, len(ranges))
	for k, r := range ranges {
		parts[k] = array.NewSlice(col, r[0], r[1])
		defer parts[k].Release()
	}
	out, err := array.Concatenate(parts, memory.DefaultAllocator)
	if err != nil {
		t.Fatalf("concatenate: %v", err)
	}
	return out
}

// rowsOf concatenates the given row ranges of the batch, in order, into a
// new batch under its schema. The batch is released on the test's
// cleanup.
func rowsOf(t *rapid.T, rec arrow.RecordBatch, ranges ...[2]int64) arrow.RecordBatch {
	cols := make([]arrow.Array, rec.NumCols())
	var n int64
	for _, r := range ranges {
		n += r[1] - r[0]
	}
	for c := range cols {
		cols[c] = concatColumn(t, rec.Column(c), ranges)
		defer cols[c].Release()
	}
	out := array.NewRecordBatch(rec.Schema(), cols, n)
	t.Cleanup(out.Release)
	return out
}

// withCell copies the cell of row r into row i of column c and leaves
// every other cell as it is. The batch is released on the test's cleanup.
func withCell(t *rapid.T, rec arrow.RecordBatch, i, c, r int64) arrow.RecordBatch {
	n := rec.NumRows()
	cols := slices.Clone(rec.Columns())
	cols[c] = concatColumn(t, rec.Column(int(c)), [][2]int64{{0, i}, {r, r + 1}, {i + 1, n}})
	defer cols[c].Release()
	out := array.NewRecordBatch(rec.Schema(), cols, n)
	t.Cleanup(out.Release)
	return out
}

// TestDiffRows draws a batch of the round trip schema, applies one change
// to a copy, and checks that diffRows reports that change and nothing
// else. The diff runs only after a comparison has failed, so without this
// test its first run would be during an investigation.
func TestDiffRows(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Each change maps to one expected report, so the test pins the
		// classification and not only that something is found.
		//
		//  1. Draw 2 to 50 rows of the round trip schema in the decoder's
		//     layout, $timestamp first and no $table.
		//  2. Draw one change: none, drop row i, duplicate row i in place,
		//     edit one cell of row i, or swap rows i and j.
		//  3. Build the changed batch from row ranges of the drawn one, so
		//     no cell is formatted.
		//  4. Check the report against the change.

		// 1. the drawn rows
		want := table.GenerateRows(rt, roundTripSchema("e2e_csv"), rapid.IntRange(2, 50).Draw(rt, "rows")).Batch
		n := want.NumRows()
		ts := want.Column(0).(*array.Timestamp)
		i := rapid.Int64Range(0, n-1).Draw(rt, "i")

		// 2. and 3. the change and the changed batch, 4. the report
		switch rapid.SampledFrom([]string{"none", "drop", "duplicate", "edit", "swap"}).Draw(rt, "change") {
		case "none":
			if d := diffRows(want, want); !d.empty() {
				rt.Fatalf("unchanged rows: %+v", d)
			}

		case "drop":
			d := diffRows(want, rowsOf(rt, want, [2]int64{0, i}, [2]int64{i + 1, n}))
			if len(d.missing) != 1 || d.missing[0] != ts.Value(int(i)) ||
				len(d.extra) != 0 || len(d.changed) != 0 || len(d.breaks) != 0 {
				rt.Fatalf("drop of row %d: %+v", i, d)
			}

		case "duplicate":
			d := diffRows(want, rowsOf(rt, want, [2]int64{0, i + 1}, [2]int64{i, n}))
			if len(d.extra) != 1 || d.extra[0].row != int(i)+1 || !d.extra[0].duplicate ||
				len(d.missing) != 0 || len(d.changed) != 0 || len(d.breaks) != 0 {
				rt.Fatalf("duplicate of row %d: %+v", i, d)
			}

		case "edit":
			// The edit copies a cell of another row that differs from row
			// i's, and a batch in which no such cell exists is skipped.
			type cell struct{ c, r int64 }
			var cands []cell
			for c := int64(1); c < want.NumCols(); c++ {
				for r := range n {
					if !array.SliceEqual(want.Column(int(c)), i, i+1, want.Column(int(c)), r, r+1) {
						cands = append(cands, cell{c, r})
					}
				}
			}
			if len(cands) == 0 {
				rt.Skip("no cell differs from row i's")
			}
			e := rapid.SampledFrom(cands).Draw(rt, "cell")
			d := diffRows(want, withCell(rt, want, i, e.c, e.r))
			if len(d.changed) != 1 || d.changed[0].column != want.ColumnName(int(e.c)) ||
				d.changed[0].ts != ts.Value(int(i)) ||
				len(d.missing) != 0 || len(d.extra) != 0 || len(d.breaks) != 0 {
				rt.Fatalf("edit of row %d column %d: %+v", i, e.c, d)
			}

		case "swap":
			// Rows i and j trade places. An adjacent swap breaks the order
			// once, at j. Any other swap breaks it twice, at i+1, which
			// follows the larger timestamp moved to i, and at j.
			j := rapid.Int64Range(0, n-1).Filter(func(j int64) bool { return j != i }).Draw(rt, "j")
			lo, hi := min(i, j), max(i, j)
			d := diffRows(want, rowsOf(rt, want,
				[2]int64{0, lo}, [2]int64{hi, hi + 1}, [2]int64{lo + 1, hi}, [2]int64{lo, lo + 1}, [2]int64{hi + 1, n}))
			wantBreaks := []int{int(hi)}
			if hi > lo+1 {
				wantBreaks = []int{int(lo) + 1, int(hi)}
			}
			var rows []int
			for _, b := range d.breaks {
				rows = append(rows, b.row)
			}
			if !slices.Equal(rows, wantBreaks) ||
				len(d.missing) != 0 || len(d.extra) != 0 || len(d.changed) != 0 {
				rt.Fatalf("swap of rows %d and %d: breaks at %v, want %v: %+v", lo, hi, rows, wantBreaks, d)
			}
		}
	})
}
