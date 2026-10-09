// Tests for the decoders. They need no cluster. For every codec, the
// round trip draws tables through the fixture, encodes them as one body
// and checks that the decoder returns the batches that were drawn; the
// fault property draws one mutation of that body and checks that the
// decoder refuses it; the interleaved property shuffles the tables'
// rows through the stream encoder. What a batch cannot express is one
// short table per format.
package encoding

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/model"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// codecs pairs every format that decodes with its encoder. A new decoder
// joins the slice and thereby the round trip.
var codecs = []struct {
	Encoder
	Decoder
}{{CSV{}, CSV{}}, {NDJSON{}, NDJSON{}}, {Arrow{}, Arrow{}}}

// specials are the two columns the reader answers in front of every
// table. The fault table builds its one-column schemas on them.
var specials = []arrow.Field{
	{Name: "$table", Type: arrow.BinaryTypes.String},
	{Name: "$timestamp", Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
}

// constant answers the same schema for every table name.
func constant(schema *arrow.Schema) model.SchemaOf {
	return func(string) (*arrow.Schema, error) { return schema, nil }
}

// TestDecodeRoundTrip checks, for every codec, that one to three tables
// sharing a column list, drawn through the fixture and encoded as one
// body, decode to the batches that were drawn, in the order the body
// first names each table and without the tables that have no rows. A
// body that is a header alone decodes to no table at all.
func TestDecodeRoundTrip(t *testing.T) {
	for _, c := range codecs {
		t.Run(c.ContentType(), func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				// 1. draw the tables; a table may have no rows
				first := table.Generate(rt)
				tables := []table.Table{first}
				for range rapid.IntRange(0, 2).Draw(rt, "more tables") {
					tables = append(tables, table.GenerateLike(rt, first))
				}
				var want []table.Table
				for _, tbl := range tables {
					if tbl.Rows() > 0 {
						want = append(want, tbl)
					}
				}

				// 2. encode every table into one body
				body := encode(rt, c, table.Body(rt, tables...))

				// 3. decode it under a lookup that answers the reader's whole-table
				// schema for any name. The tables share one column list, so one
				// schema fits all of them.
				got, err := c.Decode(context.Background(), bytes.NewReader(body), constant(table.WithTable(rt, first).Schema()))
				if err != nil {
					rt.Fatalf("decode: %v", err)
				}

				// 4. the decoder returns one batch per table that had rows, in the
				// order the body first named them, each equal to the batch drawn
				if len(got) != len(want) {
					rt.Fatalf("decoded %d tables, want %d", len(got), len(want))
				}
				for i, tb := range got {
					if tb.Table != want[i].Name || !array.RecordEqual(want[i].Batch, tb.Batch) {
						rt.Errorf("table %d decoded as %s\n%v\nwant %s\n%v", i, tb.Table, tb.Batch, want[i].Name, want[i].Batch)
					}
					tb.Batch.Release()
				}
			})
		})
	}
}

// withoutColumn is rec without column i, the caller releasing it.
func withoutColumn(rec arrow.RecordBatch, i int) arrow.RecordBatch {
	fields := slices.Delete(slices.Clone(rec.Schema().Fields()), i, i+1)
	cols := slices.Delete(slices.Clone(rec.Columns()), i, i+1)
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
}

// renamedColumn is rec with column i under name, the caller releasing
// it.
func renamedColumn(rec arrow.RecordBatch, i int, name string) arrow.RecordBatch {
	fields := slices.Clone(rec.Schema().Fields())
	fields[i].Name = name
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), rec.Columns(), rec.NumRows())
}

// withNullAt is rec with slot row of column i set to null, the caller
// releasing it. The column is rebuilt from its own values, so only the
// two special columns, a string and a timestamp, are handled.
func withNullAt(t failer, rec arrow.RecordBatch, i int, row int) arrow.RecordBatch {
	t.Helper()
	b := array.NewBuilder(memory.DefaultAllocator, rec.Column(i).DataType())
	defer b.Release()
	for r := range int(rec.NumRows()) {
		if r == row || rec.Column(i).IsNull(r) {
			b.AppendNull()
			continue
		}
		switch a := rec.Column(i).(type) {
		case *array.String:
			b.(*array.StringBuilder).Append(a.Value(r))
		case *array.Timestamp:
			b.(*array.TimestampBuilder).Append(a.Value(r))
		default:
			t.Fatalf("withNullAt: column %s", rec.Column(i).DataType())
		}
	}
	col := b.NewArray()
	defer col.Release()
	cols := slices.Clone(rec.Columns())
	cols[i] = col
	return array.NewRecordBatch(rec.Schema(), cols, rec.NumRows())
}

// retyped is schema with data column i under another type: a double for
// an int64, an int64 for anything else.
func retyped(schema *arrow.Schema, i int) *arrow.Schema {
	fields := slices.Clone(schema.Fields())
	if fields[i].Type.ID() == arrow.INT64 {
		fields[i].Type = arrow.PrimitiveTypes.Float64
	} else {
		fields[i].Type = arrow.PrimitiveTypes.Int64
	}
	return arrow.NewSchema(fields, nil)
}

// fault is one mutation of a body before it is encoded, with the error
// the decoder must then answer. The mutations are of the batch and of
// the schema lookup, so one list serves every format. apply takes the
// body as table.Body builds it, $table and $timestamp in front, and the
// reader's schema of the shared column list. It returns a batch the
// caller releases, so a mutation that keeps the body retains it.
type fault struct {
	name  string
	apply func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error)
}

// errRefused stands in for the cluster's unknown-table error, which the
// decoders must pass through unwrapped so the handler can answer 404.
var errRefused = errors.New("no such table")

// faults is every mutation the property draws from, each named by the
// rule of the seam it exercises (internal/encoding/AGENTS.md, The
// seam). The data column indices count from 2, after $table and
// $timestamp. A row index is drawn when the mutation needs one. The
// last entry needs two tables with rows and is drawn only then.
var faults = []fault{
	// A body must say which table each row goes to. Without the column
	// the CSV header, the first NDJSON object and the Arrow schema all
	// name no $table.
	{"no $table", func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error) {
		return withoutColumn(body, 0), constant(schema), ErrInvalidRows
	}},
	// A body must carry the index, because the writer cannot invent one.
	{"no $timestamp", func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error) {
		return withoutColumn(body, 1), constant(schema), ErrInvalidRows
	}},
	// A column the table lacks is refused when the table is typed, so a
	// typo in a header is a fault and not a silently dropped column.
	{"unknown column", func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error) {
		i := rapid.IntRange(2, int(body.NumCols())-1).Draw(rt, "column")
		return renamedColumn(body, i, body.Schema().Field(i).Name+"_nope"), constant(schema), ErrInvalidRows
	}},
	// A row that names no table is refused in every format: CSV writes
	// the null as the empty field, NDJSON as null, Arrow as a null slot.
	{"null $table", func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error) {
		return withNullAt(rt, body, 0, rapid.IntRange(0, int(body.NumRows())-1).Draw(rt, "row")), constant(schema), ErrInvalidRows
	}},
	// The index cannot be null, and the decoder says so before the push
	// rather than leaving it to the writer.
	{"null $timestamp", func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error) {
		return withNullAt(rt, body, 1, rapid.IntRange(0, int(body.NumRows())-1).Draw(rt, "row")), constant(schema), ErrInvalidRows
	}},
	// The lookup's error passes through as is, so the body is kept and
	// the lookup is what fails.
	{"table refused", func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error) {
		body.Retain()
		return body, func(string) (*arrow.Schema, error) { return nil, errRefused }, errRefused
	}},
	// One body is one column list, types included, because the Arrow
	// writer checks one table at a time. The body is kept, and the lookup
	// answers the first table's schema for the table the body names
	// first and a retyped one for every other, so the second table seen
	// disagrees with the first.
	{"tables of differing type", func(rt *rapid.T, body arrow.RecordBatch, schema *arrow.Schema) (arrow.RecordBatch, model.SchemaOf, error) {
		body.Retain()
		first := body.Column(0).(*array.String).Value(0)
		other := retyped(schema, rapid.IntRange(2, schema.NumFields()-1).Draw(rt, "column"))
		return body, func(name string) (*arrow.Schema, error) {
			if name == first {
				return schema, nil
			}
			return other, nil
		}, ErrInvalidRows
	}},
}

// TestDecodeFaults checks, for every codec, that a body with one drawn
// fault decodes to ErrInvalidRows and no batches, and that a table
// schemaOf refuses surfaces the error of schemaOf itself, unwrapped.
// The faults are mutations of the batch before it is encoded, so one
// list serves every format.
func TestDecodeFaults(t *testing.T) {
	for _, c := range codecs {
		t.Run(c.ContentType(), func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				// 1. draw tables sharing a column list, as the round trip does,
				// and the reader's schema of that list, which every lookup
				// answers. A body of no rows is skipped rather than failed,
				// because a header alone names no table and an NDJSON body
				// of no rows is empty, so no row-level fault can be put in it
				first := table.Generate(rt)
				tables := []table.Table{first}
				for range rapid.IntRange(0, 2).Draw(rt, "more tables") {
					tables = append(tables, table.GenerateLike(rt, first))
				}
				body := table.Body(rt, tables...)
				if body.NumRows() == 0 {
					rt.Skip("no rows")
				}
				schema := table.WithTable(rt, first).Schema()

				// 2. draw a fault that applies. The last one compares two
				// tables, so it needs two tables that have rows, because a
				// table of no rows is never typed
				named := map[string]bool{}
				for _, tbl := range tables {
					if tbl.Rows() > 0 {
						named[tbl.Name] = true
					}
				}
				applicable := faults[:len(faults)-1]
				if len(named) > 1 {
					applicable = faults
				}
				f := rapid.SampledFrom(applicable).Draw(rt, "fault")
				mutated, schemaOf, want := f.apply(rt, body, schema)
				defer mutated.Release()

				// 3. the mutated body, encoded by the codec under test, decodes
				// to the fault's error and no batches. errors.Is covers both
				// the sentinel and the lookup's own error, and got must be nil,
				// because a decoder that failed releases what it built
				got, err := c.Decode(context.Background(), bytes.NewReader(encode(rt, c, mutated)), schemaOf)
				if !errors.Is(err, want) || got != nil {
					rt.Fatalf("%s: %v, %v", f.name, got, err)
				}
			})
		})
	}
}

// concatenated is the batches joined column by column under the first
// batch's schema, the caller releasing it.
func concatenated(t failer, recs []arrow.RecordBatch) arrow.RecordBatch {
	t.Helper()
	schema := recs[0].Schema()
	cols := make([]arrow.Array, schema.NumFields())
	rows := int64(0)
	for i := range cols {
		chunks := make([]arrow.Array, len(recs))
		for j, r := range recs {
			chunks[j] = r.Column(i)
		}
		var err error
		if cols[i], err = array.Concatenate(chunks, memory.DefaultAllocator); err != nil {
			t.Fatalf("concatenate: %v", err)
		}
		defer cols[i].Release()
	}
	for _, r := range recs {
		rows += r.NumRows()
	}
	return array.NewRecordBatch(schema, cols, rows)
}

// TestDecodeInterleaved checks, for every codec, that the rows of two
// or three tables cut into pieces and shuffled into one body decode to
// one batch per table, in the order the body first names each, with the
// table's rows in body order. The body is the stream encoder over the
// pieces, so the Arrow body carries several record batches.
func TestDecodeInterleaved(t *testing.T) {
	for _, c := range codecs {
		t.Run(c.ContentType(), func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				// 1. draw two or three tables sharing a column list and cut each
				// table's rows into one to three pieces at drawn row indices. A
				// piece is kept twice: with the $table column, as the body
				// carries it, and without, as the decoded batch must answer it.
				// Slices share the fixture's buffers and are released on
				// cleanup, as the fixture releases its own batches
				first := table.Generate(rt)
				tables := []table.Table{first}
				for range rapid.IntRange(1, 2).Draw(rt, "more tables") {
					tables = append(tables, table.GenerateLike(rt, first))
				}
				type piece struct {
					table int
					rows  arrow.RecordBatch // the piece without $table, for the expectation
					body  arrow.RecordBatch // the piece with $table, for the body
				}
				var pieces []piece
				for k, tbl := range tables {
					// A table of no rows has no piece and must not appear in the
					// decoded tables either
					if tbl.Rows() == 0 {
						continue
					}
					with := table.WithTable(rt, tbl)
					// The cuts are distinct interior row indices, so every piece
					// has at least one row. A table of one row cannot be cut
					var cuts []int
					if tbl.Rows() > 1 {
						cuts = rapid.SliceOfNDistinct(rapid.IntRange(1, tbl.Rows()-1), 0, min(2, tbl.Rows()-1), rapid.ID[int]).Draw(rt, "cuts")
						slices.Sort(cuts)
					}
					bounds := append(append([]int{0}, cuts...), tbl.Rows())
					for i := range len(bounds) - 1 {
						lo, hi := int64(bounds[i]), int64(bounds[i+1])
						pc := piece{k, tbl.Batch.NewSlice(lo, hi), with.NewSlice(lo, hi)}
						rt.Cleanup(pc.rows.Release)
						rt.Cleanup(pc.body.Release)
						pieces = append(pieces, pc)
					}
				}
				if len(pieces) == 0 {
					rt.Skip("no rows")
				}
				// The shuffle is what interleaves the tables: two pieces of one
				// table may end up apart, with another table's piece between
				pieces = rapid.Permutation(pieces).Draw(rt, "order")

				// 2. the expectation, from the shuffled order alone: the tables
				// in the order the body first names them, and per table its
				// pieces in body order, which is the order the decoder must
				// keep the rows in
				var order []int
				byTable := map[int][]arrow.RecordBatch{}
				for _, pc := range pieces {
					if _, seen := byTable[pc.table]; !seen {
						order = append(order, pc.table)
					}
					byTable[pc.table] = append(byTable[pc.table], pc.rows)
				}

				// 3. the body is the stream encoder over the pieces, one step
				// each, which for Arrow writes one record batch per piece and
				// for the text wires one header and the rows of every piece, so
				// the decoder meets several batches and runs that span them
				bodies := make([]arrow.RecordBatch, len(pieces))
				for i, pc := range pieces {
					bodies[i] = pc.body
				}
				var buf bytes.Buffer
				if err := c.EncodeStream(context.Background(), &buf, steps(bodies, nil)); err != nil {
					rt.Fatalf("encode: %v", err)
				}
				got, err := c.Decode(context.Background(), &buf, constant(table.WithTable(rt, first).Schema()))
				if err != nil {
					rt.Fatalf("decode: %v", err)
				}

				// 4. one batch per table, in first-seen order, each equal to its
				// pieces concatenated. RecordEqual compares the columns by
				// position, and the decoded batch has $timestamp first and then
				// the data columns in the table's order, as the fixture's batch
				// has
				if len(got) != len(order) {
					rt.Fatalf("decoded %d tables, want %d", len(got), len(order))
				}
				for i, tb := range got {
					want := concatenated(rt, byTable[order[i]])
					if tb.Table != tables[order[i]].Name || !array.RecordEqual(want, tb.Batch) {
						rt.Errorf("table %d decoded as %s\n%v\nwant %s\n%v", i, tb.Table, tb.Batch, tables[order[i]].Name, want)
					}
					want.Release()
					tb.Batch.Release()
				}
			})
		})
	}
}

// TestDecodeFormatFaults checks the faults a batch cannot express, one
// short table per format: bytes that are not the format, a fraction in
// an int64 column, a value of the wrong JSON kind, a dictionary-encoded
// $table.
func TestDecodeFormatFaults(t *testing.T) {
	// Every case decodes under one schema of one int64 column, which is
	// enough for a fault of the bytes or of a value's kind
	ints := constant(arrow.NewSchema(append(append([]arrow.Field{}, specials...), arrow.Field{Name: "i", Type: arrow.PrimitiveTypes.Int64, Nullable: true}), nil))

	// The one Arrow body is built by hand, because no encoder of this
	// package writes a dictionary-encoded column: a dictionary $table over
	// one row, which the decoder refuses before any batch is read
	dict := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	tb := array.NewDictionaryBuilder(memory.DefaultAllocator, dict).(*array.BinaryDictionaryBuilder)
	defer tb.Release()
	if err := tb.AppendString("a"); err != nil {
		t.Fatal(err)
	}
	ts := array.NewTimestampBuilder(memory.DefaultAllocator, specials[1].Type.(*arrow.TimestampType))
	defer ts.Release()
	ts.Append(0)
	cols := []arrow.Array{tb.NewArray(), ts.NewArray()}
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "$table", Type: dict}, specials[1]}, nil), cols, 1)
	for _, c := range cols {
		c.Release()
	}
	defer rec.Release()
	dictionaryTable := ipcStream(t, rec)

	for name, tc := range map[string]struct {
		dec  Decoder
		body []byte
	}{
		// an int64 column takes an integer literal only, so 1.5 is a fault
		// and not a cast, on both text wires
		"csv: fraction in an int64":    {CSV{}, []byte("$table,$timestamp,i\na,1970-01-01T00:00:00Z,1.5\n")},
		"ndjson: fraction in an int64": {NDJSON{}, []byte(`{"$table":"a","$timestamp":"1970-01-01T00:00:00Z","i":1.5}` + "\n")},
		// the standard library's parse error surfaces as the body's fault
		"ndjson: not json": {NDJSON{}, []byte(`{"$table":` + "\n")},
		// a row is an object; an array cannot be read into a map of members
		"ndjson: not an object": {NDJSON{}, []byte(`[1]` + "\n")},
		// $table routes the row, so it must be a string, and a timestamp is
		// written as a string, so a number cannot be one
		"ndjson: $table not a string": {NDJSON{}, []byte(`{"$table":1,"$timestamp":"1970-01-01T00:00:00Z","i":1}` + "\n")},
		"ndjson: number in a string":  {NDJSON{}, []byte(`{"$table":"a","$timestamp":0,"i":1}` + "\n")},
		// the IPC reader refuses what is not a stream when it reads the schema
		"arrow: not a stream": {Arrow{}, []byte("nope")},
		// $table must be a plain utf8 column; the dictionary form is refused
		// rather than decoded (internal/encoding/AGENTS.md, The seam)
		"arrow: dictionary $table": {Arrow{}, dictionaryTable},
	} {
		got, err := tc.dec.Decode(context.Background(), bytes.NewReader(tc.body), ints)
		if !errors.Is(err, ErrInvalidRows) || got != nil {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}

// TestNDJSONSparseRows checks that a later object may omit a column,
// which reads as null, and may name its members in any order. The body
// is written by hand, because no encoder writes a sparse object.
func TestNDJSONSparseRows(t *testing.T) {
	schema := arrow.NewSchema(append(append([]arrow.Field{}, specials...),
		arrow.Field{Name: "i", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		arrow.Field{Name: "s", Type: arrow.BinaryTypes.String, Nullable: true}), nil)
	body := `{"$table":"a","$timestamp":"1970-01-01T00:00:00Z","i":1,"s":"x"}` + "\n" +
		`{"s":"y","$timestamp":"1970-01-01T00:00:01Z","$table":"a"}` + "\n" +
		`{"$timestamp":"1970-01-01T00:00:02Z","$table":"a","i":3}` + "\n"
	got, err := NDJSON{}.Decode(context.Background(), strings.NewReader(body), constant(schema))
	if err != nil || len(got) != 1 {
		t.Fatalf("decode: %v, %v", got, err)
	}
	defer got[0].Batch.Release()
	rec := got[0].Batch
	if rec.NumRows() != 3 || rec.NumCols() != 3 {
		t.Fatalf("decoded %v", rec)
	}
	i, s := rec.Column(1).(*array.Int64), rec.Column(2).(*array.String)
	ok := i.Value(0) == 1 && i.IsNull(1) && i.Value(2) == 3 && s.Value(0) == "x" && s.Value(1) == "y" && s.IsNull(2)
	if !ok {
		t.Fatalf("decoded %v", rec)
	}
}
