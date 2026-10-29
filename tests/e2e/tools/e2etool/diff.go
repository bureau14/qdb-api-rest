// The diff subcommand explains a failed round trip comparison. It sits in
// its own file beside main.go, so its property test sits beside the code
// it tests.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// timestampLayout is the CSV encoder's timestamp layout
// (internal/encoding, timestampLayout), which the package does not
// export.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// extraRow is one read row that matches no generated row. duplicate says
// that its timestamp names a generated row the read already answered.
type extraRow struct {
	row       int
	ts        arrow.Timestamp
	duplicate bool
}

// changedCell is one cell of a matched row whose values differ. The
// values are arrow's text for each cell.
type changedCell struct {
	ts        arrow.Timestamp
	column    string
	want, got string
}

// orderBreak is one read row whose timestamp is below the previous row's.
type orderBreak struct {
	row      int
	prev, ts arrow.Timestamp
}

// rowDiff is what a read answered against the generated rows, keyed by
// $timestamp. The zero value is no difference.
type rowDiff struct {
	// wantRows and gotRows are the two row counts.
	wantRows, gotRows int
	// missing holds the generated timestamps the read never answered, in
	// generated order.
	missing []arrow.Timestamp
	// extra holds the read rows whose timestamp the generated rows lack
	// or that repeat a timestamp, in read order.
	extra []extraRow
	// changed holds each cell that differs on a matched row.
	changed []changedCell
	// breaks holds each read row whose timestamp is below the previous
	// row's. A repeated timestamp is not a break, because it is already
	// an extra row, so each kind of change is reported once.
	breaks []orderBreak
}

// empty reports whether the diff found nothing.
func (d rowDiff) empty() bool {
	return d.wantRows == d.gotRows && len(d.missing) == 0 && len(d.extra) == 0 &&
		len(d.changed) == 0 && len(d.breaks) == 0
}

// diffRows compares a read with the generated rows. Both batches are in
// the decoder's layout, $timestamp first and then the data columns in
// schema order, under one schema. The generated timestamps must be
// unique, which the fixture guarantees.
func diffRows(want, got arrow.RecordBatch) rowDiff {
	// The read is walked once, in its own order. A map from timestamp to
	// generated row replaces a merge of the two sorted sides, because a
	// merge assumes the read is ordered, and a disorder is one of the
	// things the diff looks for.
	//
	//  1. Index the generated rows by timestamp. The fixture draws
	//     $timestamp strictly ascending, so a timestamp names one row.
	//  2. Walk the read rows. A row whose timestamp is below the previous
	//     row's is an order break, and the walk goes on to classify it.
	//  3. A timestamp the generated rows lack is an extra row, and so is
	//     one the walk has already matched, marked as a duplicate.
	//  4. A matched row compares each data column, and each cell that
	//     differs is recorded with arrow's text for both values.
	//  5. The generated timestamps the walk never matched are missing, in
	//     generated order.
	d := rowDiff{wantRows: int(want.NumRows()), gotRows: int(got.NumRows())}
	wantTS := want.Column(0).(*array.Timestamp)
	gotTS := got.Column(0).(*array.Timestamp)

	// 1. the generated rows by timestamp
	index := make(map[arrow.Timestamp]int, d.wantRows)
	for i := range d.wantRows {
		index[wantTS.Value(i)] = i
	}

	// 2. the read rows, in read order
	matched := make([]bool, d.wantRows)
	for j := range d.gotRows {
		ts := gotTS.Value(j)
		if j > 0 && ts < gotTS.Value(j-1) {
			d.breaks = append(d.breaks, orderBreak{row: j, prev: gotTS.Value(j - 1), ts: ts})
		}

		// 3. an unknown timestamp, or one already answered
		i, ok := index[ts]
		if !ok || matched[i] {
			d.extra = append(d.extra, extraRow{row: j, ts: ts, duplicate: ok})
			continue
		}
		matched[i] = true

		// 4. The fixture draws no NaN, so exact equality holds for
		// doubles, and SliceEqual treats two nulls as equal.
		for c := 1; c < int(want.NumCols()); c++ {
			w, g := want.Column(c), got.Column(c)
			if !array.SliceEqual(w, int64(i), int64(i+1), g, int64(j), int64(j+1)) {
				d.changed = append(d.changed, changedCell{
					ts: ts, column: want.ColumnName(c), want: w.ValueStr(i), got: g.ValueStr(j),
				})
			}
		}
	}

	// 5. the generated rows the read never answered
	for i, m := range matched {
		if !m {
			d.missing = append(d.missing, wantTS.Value(i))
		}
	}
	return d
}

// writeDiff writes the report as plain lines: the two row counts, then
// one line per missing timestamp, extra row, changed cell and order
// break. An empty diff writes one
// line saying that the rows are equal after decoding, which means the
// two files differ in their bytes only.
func writeDiff(w io.Writer, d rowDiff, unit arrow.TimeUnit) error {
	// The timestamps are written in the layout of the CSV encoder, UTC
	// with nine fractional digits, so a reported timestamp can be found
	// in rows.csv with grep.
	ts := func(t arrow.Timestamp) string { return t.ToTime(unit).UTC().Format(timestampLayout) }
	var err error
	line := func(format string, args ...any) {
		if err == nil {
			_, err = fmt.Fprintf(w, format+"\n", args...)
		}
	}
	line("rows: generated %d, read %d", d.wantRows, d.gotRows)
	if d.empty() {
		line("the rows are equal after decoding")
		return err
	}
	for _, t := range d.missing {
		line("missing %s", ts(t))
	}
	for _, e := range d.extra {
		kind := "unknown"
		if e.duplicate {
			kind = "duplicate"
		}
		line("extra row %d: %s, %s", e.row, ts(e.ts), kind)
	}
	for _, c := range d.changed {
		line("changed %s %s: generated %q, read %q", ts(c.ts), c.column, c.want, c.got)
	}
	for _, b := range d.breaks {
		line("order break at read row %d: %s after %s", b.row, ts(b.ts), ts(b.prev))
	}
	return err
}

// decodeRows decodes one CSV file of the named round trip table into the
// decoder's layout. A file with a header and no rows answers an empty
// batch under the given schema, or an error when the schema is nil. The
// caller releases the batch.
func decodeRows(path, name string, empty *arrow.Schema) (arrow.RecordBatch, error) {
	// The decoder answers no batch, one batch or several, and each maps
	// to its own answer.
	//
	//  1. Decode the file through the CSV decoder with toCSV's schema,
	//     the round trip schema with $table in front.
	//  2. No batch is an empty read, answered as an empty batch, because a
	//     read of zero rows is a finding of the diff and not an error.
	//  3. More than one batch is an error that names the count, because
	//     the round trip writes one table per file.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	rel := &releaser{}
	defer rel.release()

	// 1. the decode, typed by toCSV's schema
	schema := table.WithTable(rel, roundTripSchema(name)).Schema()
	schemaOf := func(string) (*arrow.Schema, error) { return schema, nil }
	got, err := (encoding.CSV{}).Decode(context.Background(), f, schemaOf)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	// 2. an empty read
	switch len(got) {
	case 0:
		if empty == nil {
			return nil, fmt.Errorf("%s: no rows", path)
		}
		b := array.NewRecordBuilder(memory.DefaultAllocator, empty)
		defer b.Release()
		return b.NewRecordBatch(), nil
	case 1:
		return got[0].Batch, nil
	}

	// 3. several tables
	for _, b := range got {
		b.Batch.Release()
	}
	return nil, fmt.Errorf("%s: decoded %d tables, want 1", path, len(got))
}

// diff is the diff subcommand. It decodes the generated file and the
// read file, writes the report to stdout, and fails when the report is
// not empty, so a caller sees a difference in the exit status.
func diff(args []string) error {
	//  1. Parse --table and the two paths, generated first.
	//  2. Decode the generated file first, because its schema types an
	//     empty read. The generated rows are never empty.
	//  3. Decode the read file.
	//  4. Write the report, then fail when it is not empty.

	// 1. the flags and the paths
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	tableName := fs.String("table", "e2e_csv", "the table both files carry")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("diff takes the generated file and the read file")
	}

	// 2. the generated rows
	want, err := decodeRows(fs.Arg(0), *tableName, nil)
	if err != nil {
		return err
	}
	defer want.Release()

	// 3. the read rows
	got, err := decodeRows(fs.Arg(1), *tableName, want.Schema())
	if err != nil {
		return err
	}
	defer got.Release()

	// 4. the report and the status
	d := diffRows(want, got)
	unit := want.Schema().Field(0).Type.(*arrow.TimestampType).Unit
	if err := writeDiff(os.Stdout, d, unit); err != nil {
		return err
	}
	if !d.empty() {
		return fmt.Errorf("the read differs from the generated rows")
	}
	return nil
}
