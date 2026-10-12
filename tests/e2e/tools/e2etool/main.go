// Command e2etool is the Go tool of the v2 e2e round trip (docs/e2e.md,
// "The tool"). gen writes the rows the round trip ingests, in every body
// format, and tocsv turns a response into CSV. The tool may import any
// package of this repository, the cgo binding included (ADR-0014).
//
//	e2etool gen --rows N --seed S --out DIR [--table NAME]      write DIR/rows.{csv,ndjson,arrow,json}, print the row count
//	e2etool tocsv --format json|ndjson|arrow [--table NAME]     stdin to CSV on stdout
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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

// roundTripSchema is the table the round trip creates: one column of every type,
// named v_<type>, with the symbol's symtable named after the table and
// the column as the fixture names them. The driver's create body spells
// the same names.
func roundTripSchema(name string) table.Table {
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
// roundTripSchema's columns: the extreme integers, a double the shortest
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

// generate draws rows rows of the round trip's table named name under seed,
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
	//  2. put the edge rows ahead of the drawn ones, because the round trip
	//     wants the extremes in every run and ten thousand drawn rows
	//     reach them rarely. They are built here and not in the fixture,
	//     which draws values and fixes none;
	//  3. put the $table column in front through the fixture's WithTable,
	//     so the CSV header is the one the table reader answers.
	tbl := roundTripSchema(name)

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

// bodyFiles is the encoder that writes each body format's file, keyed by
// the format name the driver's matrix uses, which is also the file's
// extension: rows.csv, rows.ndjson, rows.arrow, rows.json.
var bodyFiles = []struct {
	name string
	enc  encoding.Encoder
}{
	{"csv", encoding.CSV{}},
	{"ndjson", encoding.NDJSON{}},
	{"arrow", encoding.Arrow{}},
	{"json", encoding.JSON{}},
}

// writeBody writes rec to dir/<file> through enc, so the file is the
// encoder's dialect and the tool formats no cell of its own.
func writeBody(dir, file string, enc encoding.Encoder, rec arrow.RecordBatch) error {
	f, err := os.Create(filepath.Join(dir, file))
	if err != nil {
		return err
	}
	if err := enc.Encode(context.Background(), f, rec); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// writeBodies writes rec as rows.<format> in dir for every body format.
// The four files carry the same batch, $table included, so the bodies
// the round trip ingests differ in format only.
func writeBodies(dir string, rec arrow.RecordBatch) error {
	for _, b := range bodyFiles {
		if err := writeBody(dir, "rows."+b.name, b.enc, rec); err != nil {
			return err
		}
	}
	return nil
}

// gen is the gen subcommand. It prints the row count it wrote, edge rows
// included, so the driver asserts the ingest's answer against the one
// writer of that number.
func gen(args []string) error {
	// Every failure ends the command before anything is printed, so a
	// count on stdout means every file is complete:
	//
	//  1. parse the flags. The defaults are the Makefile's, so the tool
	//     runs by hand with no options;
	//  2. draw the batch, released when the function returns, after the
	//     writes that read it;
	//  3. write every body file;
	//  4. print the row count, the one line the driver reads.

	// 1. the flags
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	rows := fs.Int("rows", 10000, "drawn rows, before the edge rows")
	seed := fs.Int("seed", 0, "draw seed")
	out := fs.String("out", ".", "directory of the rows.* files")
	tableName := fs.String("table", "e2e_csv", "the table the $table column names")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// 2. the batch, alive until after the writes
	rec, err := generate(*tableName, *rows, *seed)
	if err != nil {
		return err
	}
	defer rec.Release()

	// 3. the files
	if err := writeBodies(*out, rec); err != nil {
		return err
	}

	// 4. the count the driver reads
	fmt.Println(rec.NumRows())
	return nil
}

// decoders is the decoder of each response format tocsv reads, keyed by
// the format name the driver's matrix uses. CSV is absent, because the
// driver compares a CSV response raw.
var decoders = map[string]encoding.Decoder{
	"json":   encoding.JSON{},
	"ndjson": encoding.NDJSON{},
	"arrow":  encoding.Arrow{},
}

// toCSV decodes one response body of the round trip's table named name
// from r through dec and writes it as CSV to w, in the generated file's
// column order, $table first.
func toCSV(w io.Writer, r io.Reader, dec encoding.Decoder, name string) error {
	// The response is the table reader's or the query's answer for one
	// table, so the decoder answers one batch, and the tool restores what
	// the decoder strips:
	//
	//  1. the schema the decoder types against is the round trip schema
	//     with $table in front, the shape the decoders' tests answer. No
	//     request asks the server for it, because the tool holds the
	//     schema already, with the binding's types the reader answers;
	//  2. decode. A response names one table and the generated rows are
	//     never empty, so one batch is expected and any other count is
	//     an error naming it;
	//  3. put $table back in front through the fixture's WithTable,
	//     because the decoder drops it and the generated CSV carries it
	//     first. The decoder orders the data columns by the schema for
	//     every format, and a bare SELECT * answers them in creation
	//     order, so no reorder is needed;
	//  4. encode through the CSV encoder, so the bytes are the dialect of
	//     rows.csv.
	tbl := roundTripSchema(name)
	rel := &releaser{}
	defer rel.release()

	// 1. the schema with $table in front
	schema := table.WithTable(rel, tbl).Schema()
	schemaOf := func(string) (*arrow.Schema, error) { return schema, nil }

	// 2. one batch
	got, err := dec.Decode(context.Background(), r, schemaOf)
	if err != nil {
		return err
	}
	for _, b := range got {
		defer b.Batch.Release()
	}
	if len(got) != 1 {
		return fmt.Errorf("decoded %d tables, want 1", len(got))
	}

	// 3. $table in front
	tbl.Batch = got[0].Batch
	body := table.WithTable(rel, tbl)

	// 4. the CSV
	return (encoding.CSV{}).Encode(context.Background(), w, body)
}

// tocsv is the tocsv subcommand: stdin to CSV on stdout.
func tocsv(args []string) error {
	fs := flag.NewFlagSet("tocsv", flag.ContinueOnError)
	format := fs.String("format", "", "json, ndjson or arrow")
	tableName := fs.String("table", "e2e_csv", "the table the body answers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dec, ok := decoders[*format]
	if !ok {
		return fmt.Errorf("--format must be json, ndjson or arrow, got %q", *format)
	}
	return toCSV(os.Stdout, os.Stdin, dec, *tableName)
}

func main() {
	usage := func() {
		fmt.Fprintln(os.Stderr, "usage: e2etool gen --rows N --seed S --out DIR [--table NAME]")
		fmt.Fprintln(os.Stderr, "       e2etool tocsv --format json|ndjson|arrow [--table NAME]")
		os.Exit(2)
	}
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "gen":
		err = gen(os.Args[2:])
	case "tocsv":
		err = tocsv(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2etool:", err)
		os.Exit(1)
	}
}
