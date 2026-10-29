// Command e2etool is the Go tool of the v2 e2e round trip (docs/e2e.md,
// "The tool"). gen writes the rows the round trip ingests in every body
// format. tocsv turns a response into CSV. diff explains a failed
// comparison row by row. adbc runs one statement through arrow-adbc's
// Flight SQL driver. The tool may import any package of this repository,
// the cgo binding included (ADR-0014).
//
//	e2etool gen --rows N --seed S --out DIR [--table NAME]      write DIR/rows.{csv,ndjson,arrow,json}, print the row count
//	e2etool tocsv --format csv|json|ndjson|arrow [--table NAME] stdin to CSV on stdout
//	e2etool diff [--table NAME] GENERATED READ                  the keyed diff of a read against the generated rows on stdout
//	e2etool adbc --uri grpc+tls://HOST:PORT --query Q [--username U --secret S] [--format arrow|csv]
//	                                                            the statement's result on stdout
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"iter"
	"math"
	"os"
	"path/filepath"
	"time"

	adbcapi "github.com/apache/arrow-adbc/go/adbc"
	adbcflight "github.com/apache/arrow-adbc/go/adbc/driver/flightsql"
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// roundTripSchema is the table the round trip creates. It has one column
// of every type, named v_<type>. The symbol column's symtable is named
// after the table and the column, as the fixture names symtables. The
// driver's create body spells the same names.
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

// edgeRows builds a batch of the values a draw reaches rarely, over the
// columns of roundTripSchema. The caller releases it. The batch holds:
//
//   - the extreme integers
//   - a double that the shortest round trip writes in exponent form
//   - a string with a quote, a comma, <&> and an LF, and one with a
//     leading space
//   - a one-byte and a two-byte blob
//   - a nanosecond timestamp at each end of the range the fixture draws
//   - one all-null row
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

// concat joins the two batches row-wise under the schema of the first.
// The caller releases the result.
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

// releaser implements the part of the fixture's T that table.WithTable
// needs. It collects cleanups for the caller to run, and it panics on a
// failure.
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

// generate draws the given number of rows of the named round-trip table
// under the seed and puts the edge rows ahead of them. It returns the
// batch as an ingest body carries it, $table first. The caller releases
// it.
func generate(name string, rows, seed int) (arrow.RecordBatch, error) {
	// The fixture draws the rows, so the vocabulary and the bounds are
	// those of the property tests and are written once. The bounds are no
	// empty string, no NUL, no MinInt64 and alphanumeric symbols.
	//
	//  1. Draw the rows under the seed. rapid.Custom(...).Example(seed) is
	//     the only seeded entry rapid offers outside a test. Example runs
	//     the T's cleanups when the function returns, which would release
	//     the batch, so the function retains it first. rapid.Check under a
	//     stand-in testing.T was rejected, because it is a test harness
	//     that shrinks on failure. A draw of the tool's own was rejected,
	//     because it would copy the bounds.
	//  2. Put the edge rows ahead of the drawn ones. The round trip wants
	//     the extremes in every run, and ten thousand drawn rows reach
	//     them rarely. The tool builds them, because the fixture draws
	//     values and fixes none.
	//  3. Put the $table column in front through the fixture's WithTable,
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

// bodyFiles lists the encoder of each body format. Each name is the
// format name in the driver's matrix and the extension of the file:
// rows.csv, rows.ndjson, rows.arrow, rows.json.
var bodyFiles = []struct {
	name string
	enc  encoding.Encoder
}{
	{"csv", encoding.CSV{}},
	{"ndjson", encoding.NDJSON{}},
	{"arrow", encoding.Arrow{}},
	{"json", encoding.JSON{}},
}

// one wraps a batch in a sequence of that batch. The tool writes one
// batch, and the encoders write only sequences.
func one(rec arrow.RecordBatch) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) { yield(rec, nil) }
}

// writeBody writes the batch to dir/<file> through the encoder, so the
// file is in the encoder's dialect and the tool formats no cell itself.
func writeBody(dir, file string, enc encoding.Encoder, rec arrow.RecordBatch) error {
	f, err := os.Create(filepath.Join(dir, file))
	if err != nil {
		return err
	}
	if err := enc.EncodeStream(context.Background(), f, one(rec)); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// writeBodies writes the batch as rows.<format> in the directory, for
// every body format. The four files carry the same batch, $table
// included, so the bodies the round trip ingests differ in format only.
// One run writes all four files. One file per run under a --format flag
// was rejected, because the driver would then draw the same batch four
// times for one table.
func writeBodies(dir string, rec arrow.RecordBatch) error {
	for _, b := range bodyFiles {
		if err := writeBody(dir, "rows."+b.name, b.enc, rec); err != nil {
			return err
		}
	}
	return nil
}

// gen is the gen subcommand. It prints the row count it wrote, edge rows
// included. The driver checks the ingest's answer against that count,
// because the tool is the only writer of the number.
func gen(args []string) error {
	// Every failure ends the command before anything is printed, so a
	// count on stdout means every file is complete.
	//
	//  1. Parse the flags. The defaults are the Makefile's, so the tool
	//     runs by hand with no options.
	//  2. Draw the batch. It is released when the function returns, after
	//     the writes that read it.
	//  3. Write every body file.
	//  4. Print the row count, which is the only line the driver reads.

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

// decoders maps each response format tocsv reads to its decoder, keyed
// by the format name in the driver's matrix. CSV is among them, because
// the query answers its columns in a different order from the table
// reader. The driver sends the query's CSV through the tool to restore
// the order.
var decoders = map[string]encoding.Decoder{
	"csv":    encoding.CSV{},
	"json":   encoding.JSON{},
	"ndjson": encoding.NDJSON{},
	"arrow":  encoding.Arrow{},
}

// toCSV decodes one response body of the named round-trip table through
// the decoder and writes it as CSV, in the column order of the generated
// file, $table first.
func toCSV(w io.Writer, r io.Reader, dec encoding.Decoder, name string) error {
	// The response is the table reader's or the query's answer for one
	// table, so the decoder answers one batch. The tool restores what the
	// decoder strips.
	//
	//  1. Build the schema the decoder types against, which is the round
	//     trip schema with $table in front. The decoder tests answer the
	//     same shape. The tool already holds the schema, with the
	//     binding's types the reader answers, so no request asks the
	//     server for it.
	//  2. Decode. A response names one table and the generated rows are
	//     never empty, so any count other than one batch is an error that
	//     names the count.
	//  3. Put $table back in front through the fixture's WithTable,
	//     because the decoder drops it and the generated CSV carries it
	//     first. Dropping $table from the expected file was rejected, so
	//     that every comparison reads the same expected file. The decoder
	//     puts $timestamp first and orders the data columns by the schema
	//     for every format. A bare SELECT * answers $timestamp, $table and
	//     then the columns in creation order. This step is therefore the
	//     whole reorder a query needs.
	//  4. Encode through the CSV encoder, so the bytes are in the dialect
	//     of rows.csv.
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
	return (encoding.CSV{}).EncodeStream(context.Background(), w, one(body))
}

// tocsv is the tocsv subcommand: stdin to CSV on stdout.
func tocsv(args []string) error {
	fs := flag.NewFlagSet("tocsv", flag.ContinueOnError)
	format := fs.String("format", "", "csv, json, ndjson or arrow")
	tableName := fs.String("table", "e2e_csv", "the table the body answers")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dec, ok := decoders[*format]
	if !ok {
		return fmt.Errorf("--format must be csv, json, ndjson or arrow, got %q", *format)
	}
	return toCSV(os.Stdout, os.Stdin, dec, *tableName)
}

// outputs is the encoder of each format adbc writes: Arrow IPC, which
// tocsv decodes, and CSV for a result the round trip's schema does not
// type.
var outputs = map[string]encoding.Encoder{
	"arrow": encoding.Arrow{},
	"csv":   encoding.CSV{},
}

// batchesOf turns the reader's batches into the sequence the encoders
// write. The error that ended the stream is the last element.
func batchesOf(rdr array.RecordReader) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		for rdr.Next() {
			if !yield(rdr.RecordBatch(), nil) {
				return
			}
		}
		if err := rdr.Err(); err != nil {
			yield(nil, err)
		}
	}
}

// adbc is the adbc subcommand. It runs one statement through arrow-adbc's
// Flight SQL driver and writes the result to stdout through the encoder
// of --format. The default is Arrow IPC, so the e2e driver pipes the
// result through tocsv like every other response. CSV serves a result
// that the round trip's schema does not type. --username and --secret
// make the ADBC driver handshake. Without them it presents no token,
// which is the anonymous user.
func adbc(args []string) error {
	// The steps follow the order of an ADBC driver session.
	//
	//  1. Parse the flags.
	//  2. Open the database over the URI as given, which is the driver's
	//     own option. Verification is off, because the server runs on the
	//     ephemeral certificate.
	//  3. Set the username and password only when a username is given.
	//     The driver handshakes when either is set, and the insecure
	//     cluster's user is anonymous.
	//  4. Open one connection and one statement, and call ExecuteQuery.
	//  5. Feed the reader's batches as a sequence into the encoder,
	//     because the tool formats no cell. Writing CSV in the tool was
	//     rejected, because tocsv already decodes Arrow and restores
	//     $table.

	// 1. the flags
	fs := flag.NewFlagSet("adbc", flag.ContinueOnError)
	uri := fs.String("uri", "", "the Flight listener as the driver takes it, grpc+tls://host:port")
	username := fs.String("username", "", "the user to handshake as; empty is the anonymous user")
	secret := fs.String("secret", "", "the user's secret key")
	q := fs.String("query", "", "the statement")
	format := fs.String("format", "arrow", "arrow or csv")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *uri == "" || *q == "" {
		return fmt.Errorf("--uri and --query are required")
	}
	enc, ok := outputs[*format]
	if !ok {
		return fmt.Errorf("--format must be arrow or csv, got %q", *format)
	}

	// 2. The URI carries the scheme, grpc+tls because the listener is TLS
	//    only. Verification is off because the certificate is ephemeral.
	opts := map[string]string{
		adbcapi.OptionKeyURI:           *uri,
		adbcflight.OptionSSLSkipVerify: adbcapi.OptionValueEnabled,
	}

	// 3. Only a named user sets credentials, because either key makes the
	//    driver handshake, and the anonymous user presents none.
	if *username != "" {
		opts[adbcapi.OptionKeyUsername] = *username
		opts[adbcapi.OptionKeyPassword] = *secret
	}
	ctx := context.Background()
	db, err := adbcflight.NewDriver(memory.DefaultAllocator).NewDatabase(opts)
	if err != nil {
		return fmt.Errorf("adbc database: %w", err)
	}
	defer func() { _ = db.Close() }()

	// 4. one connection, one statement
	conn, err := db.Open(ctx)
	if err != nil {
		return fmt.Errorf("adbc open: %w", err)
	}
	defer func() { _ = conn.Close() }()
	stmt, err := conn.NewStatement()
	if err != nil {
		return fmt.Errorf("adbc statement: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.SetSqlQuery(*q); err != nil {
		return fmt.Errorf("adbc set query: %w", err)
	}
	rdr, _, err := stmt.ExecuteQuery(ctx)
	if err != nil {
		return fmt.Errorf("adbc execute: %w", err)
	}
	defer rdr.Release()

	// 5. the result through the server's encoder
	return enc.EncodeStream(ctx, os.Stdout, batchesOf(rdr))
}

func main() {
	usage := func() {
		fmt.Fprintln(os.Stderr, "usage: e2etool gen --rows N --seed S --out DIR [--table NAME]")
		fmt.Fprintln(os.Stderr, "       e2etool tocsv --format csv|json|ndjson|arrow [--table NAME]")
		fmt.Fprintln(os.Stderr, "       e2etool diff [--table NAME] GENERATED READ")
		fmt.Fprintln(os.Stderr, "       e2etool adbc --uri grpc+tls://HOST:PORT --query Q [--username U --secret S] [--format arrow|csv]")
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
	case "diff":
		err = diff(os.Args[2:])
	case "adbc":
		err = adbc(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2etool:", err)
		os.Exit(1)
	}
}
