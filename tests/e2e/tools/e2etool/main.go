// Command e2etool is the Go tool of the v2 e2e flow (docs/e2e.md, "The
// tool"). gen writes the rows the flow ingests. The tool may import any
// package of this repository, the cgo binding included (ADR-0014).
//
//	e2etool gen --rows N --seed S --out DIR    write DIR/rows.csv, print the row count
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// flowSchema is the table the flow creates: one column of every type,
// named v_<type>, with the symbol's symtable named after the table and
// the column as the fixture names them. The driver's create body spells
// the same names.
func flowSchema(name string) table.Table {
	return table.Table{Name: name, Columns: []table.Column{
		{Name: "v_int64", Type: qdbapi.TsColumnInt64},
		{Name: "v_double", Type: qdbapi.TsColumnDouble},
		{Name: "v_string", Type: qdbapi.TsColumnString},
		{Name: "v_symbol", Type: qdbapi.TsColumnSymbol, Symtable: name + "_v_symbol"},
		{Name: "v_blob", Type: qdbapi.TsColumnBlob},
		{Name: "v_timestamp", Type: qdbapi.TsColumnTimestamp},
	}}
}

// edgeStart is the index of the first edge row. The edge rows sit on the
// day before the fixture's index start, so a concatenation ahead of the
// drawn rows keeps $timestamp ascending.
var edgeStart = time.Date(2019, 12, 31, 0, 0, 0, 0, time.UTC)

// nanos is a timestamp cell.
func nanos(t time.Time) arrow.Timestamp { return arrow.Timestamp(t.UnixNano()) }

// edgeRows is the batch of the values a draw reaches rarely, for
// flowSchema's columns: the extreme integers, a double the shortest
// round trip writes in exponent form, the awkward string with a quote,
// a comma, <&> and an LF, a leading space, a one-byte and a two-byte
// blob, a nanosecond timestamp at each end of the range the fixture
// draws, and one all-null row. The caller releases it.
func edgeRows(schema *arrow.Schema) arrow.RecordBatch {
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()
	index := b.Field(0).(*array.TimestampBuilder)
	i64 := b.Field(1).(*array.Int64Builder)
	f64 := b.Field(2).(*array.Float64Builder)
	str := b.Field(3).(*array.StringBuilder)
	sym := b.Field(4).(*array.StringBuilder)
	blob := b.Field(5).(*array.BinaryBuilder)
	ts := b.Field(6).(*array.TimestampBuilder)

	index.Append(nanos(edgeStart))
	i64.Append(math.MaxInt64)
	f64.Append(1e21)
	str.Append("a,b \"q\" <&>\n")
	sym.Append("edge")
	blob.Append([]byte{0, 255})
	ts.Append(nanos(time.Date(1970, 1, 1, 0, 0, 0, 1, time.UTC)))

	index.Append(nanos(edgeStart.Add(12 * time.Hour)))
	// MinInt64 is the server's null for int64, so the lowest value a
	// column can hold is one above it.
	i64.Append(math.MinInt64 + 1)
	f64.Append(0.1)
	str.Append(" lead")
	sym.Append("Edge2")
	blob.Append([]byte("x"))
	ts.Append(nanos(time.Date(2099, 12, 31, 23, 59, 59, 999999999, time.UTC)))

	// The empty field stands for null on the CSV wire, so one null in
	// every type proves the ingest reads the empty field back as null.
	index.Append(nanos(edgeStart.Add(24*time.Hour - time.Nanosecond)))
	for _, f := range []array.Builder{i64, f64, str, sym, blob, ts} {
		f.AppendNull()
	}
	return b.NewRecordBatch()
}

// concat joins first and second row-wise under first's schema. The
// caller releases the result.
func concat(first, second arrow.RecordBatch) (arrow.RecordBatch, error) {
	cols := make([]arrow.Array, first.NumCols())
	for i := range cols {
		c, err := array.Concatenate([]arrow.Array{first.Column(i), second.Column(i)}, memory.DefaultAllocator)
		if err != nil {
			return nil, fmt.Errorf("concatenate %s: %w", first.Schema().Field(i).Name, err)
		}
		defer c.Release()
		cols[i] = c
	}
	return array.NewRecordBatch(first.Schema(), cols, first.NumRows()+second.NumRows()), nil
}

// releaser is the slice of the fixture's T the body shape needs: it
// collects cleanups for the caller to run, and a failure is fatal.
type releaser struct{ cleanups []func() }

func (*releaser) Helper()                           {}
func (*releaser) Fatalf(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }
func (*releaser) Errorf(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }
func (r *releaser) Cleanup(f func())                { r.cleanups = append(r.cleanups, f) }
func (r *releaser) release() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
}

// generate draws rows rows of the flow's table named name under seed,
// puts the edge rows ahead of them, and returns the batch as an ingest
// body carries it, $table first. The caller releases it.
func generate(name string, rows, seed int) (arrow.RecordBatch, error) {
	// The draw is the fixture's, so the vocabulary and the bounds (no
	// empty string, no NUL, no MinInt64, alphanumeric symbols) are the
	// property tests' and are written once:
	//
	//  1. draw the rows under the seed. rapid.Custom(...).Example(seed)
	//     is rapid's one seeded entry outside a test (rapid.Check under a
	//     stand-in testing.T was rejected, because it is a test harness
	//     that shrinks on failure, and a draw of the tool's own because
	//     it would copy the bounds); Example runs the T's cleanups when
	//     the function returns, which would release the batch, so the
	//     function retains it first;
	//  2. put the edge rows ahead of the drawn ones, because the flow
	//     wants the extremes in every run and ten thousand drawn rows
	//     reach them rarely. They are built here and not in the fixture,
	//     which draws values and fixes none;
	//  3. put the $table column in front through the fixture's WithTable,
	//     so the CSV header is the one the table reader answers.
	tbl := flowSchema(name)

	// 1. the seeded draw, retained past the T's cleanup
	drawn := rapid.Custom(func(rt *rapid.T) arrow.RecordBatch {
		rec := table.GenerateRows(rt, tbl, rows).Batch
		rec.Retain()
		return rec
	}).Example(seed)
	defer drawn.Release()

	// 2. the edge rows first
	edges := edgeRows(drawn.Schema())
	defer edges.Release()
	all, err := concat(edges, drawn)
	if err != nil {
		return nil, err
	}
	defer all.Release()

	// 3. $table in front. The fixture releases its batch through the
	// releaser, so the result is retained for the caller.
	tbl.Batch = all
	r := &releaser{}
	body := table.WithTable(r, tbl)
	body.Retain()
	r.release()
	return body, nil
}

// writeCSV writes rec as rows.csv in dir through the package's CSV
// encoder, so the file is the encoder's dialect and the tool formats no
// cell of its own.
func writeCSV(dir string, rec arrow.RecordBatch) error {
	f, err := os.Create(filepath.Join(dir, "rows.csv"))
	if err != nil {
		return err
	}
	if err := (encoding.CSV{}).Encode(context.Background(), f, rec); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// gen is the gen subcommand. It prints the row count it wrote, edge rows
// included, so the driver asserts the ingest's answer against the one
// writer of that number.
func gen(args []string) error {
	// Every failure ends the command before anything is printed, so a
	// count on stdout means the file is complete:
	//
	//  1. parse the flags. The defaults are the Makefile's, so the tool
	//     runs by hand with no options;
	//  2. draw the batch, released when the function returns, after the
	//     write that reads it;
	//  3. write rows.csv;
	//  4. print the row count, the one line the driver reads.

	// 1. the flags
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	rows := fs.Int("rows", 10000, "drawn rows, before the edge rows")
	seed := fs.Int("seed", 0, "draw seed")
	out := fs.String("out", ".", "directory of rows.csv")
	tableName := fs.String("table", "e2e_csv", "the table the $table column names")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// 2. the batch, alive until after the write
	rec, err := generate(*tableName, *rows, *seed)
	if err != nil {
		return err
	}
	defer rec.Release()

	// 3. the file
	if err := writeCSV(*out, rec); err != nil {
		return err
	}

	// 4. the count the driver reads
	fmt.Println(rec.NumRows())
	return nil
}

func main() {
	if len(os.Args) < 2 || os.Args[1] != "gen" {
		fmt.Fprintln(os.Stderr, "usage: e2etool gen --rows N --seed S --out DIR [--table NAME]")
		os.Exit(2)
	}
	if err := gen(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "e2etool:", err)
		os.Exit(1)
	}
}
