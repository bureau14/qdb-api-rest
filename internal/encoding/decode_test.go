// Tests for the decoders. They need no cluster. For every codec, the
// round trip draws tables through the fixture, encodes them as one body
// and checks that the decoder returns the batches that were drawn. The
// faults a body can have are one table of cases.
package encoding

import (
	"bytes"
	"context"
	"errors"
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

// TestCSVDecodeFaults checks that each fault of a body is ErrInvalidRows
// naming the row and the column, and that a table schemaOf refuses
// surfaces the error of schemaOf itself, unwrapped.
func TestCSVDecodeFaults(t *testing.T) {
	typed := func(dt arrow.DataType) *arrow.Schema {
		return arrow.NewSchema(append(append([]arrow.Field{}, specials...), arrow.Field{Name: "i", Type: dt, Nullable: true}), nil)
	}
	ints := constant(typed(arrow.PrimitiveTypes.Int64))
	differing := func(name string) (*arrow.Schema, error) {
		if name == "a" {
			return typed(arrow.PrimitiveTypes.Int64), nil
		}
		return typed(arrow.PrimitiveTypes.Float64), nil
	}
	refused := errors.New("no such table")
	for name, tc := range map[string]struct {
		body     string
		schemaOf model.SchemaOf
		want     error
	}{
		"no $table":                {"$timestamp,i\n", ints, ErrInvalidRows},
		"no $timestamp":            {"$table,i\n", ints, ErrInvalidRows},
		"unknown column":           {"$table,$timestamp,nope\na,1970-01-01T00:00:00Z,1\n", ints, ErrInvalidRows},
		"short record":             {"$table,$timestamp,i\na,1970-01-01T00:00:00Z\n", ints, ErrInvalidRows},
		"unparsable cell":          {"$table,$timestamp,i\na,1970-01-01T00:00:00Z,one\n", ints, ErrInvalidRows},
		"empty $timestamp":         {"$table,$timestamp,i\na,,1\n", ints, ErrInvalidRows},
		"tables of differing type": {"$table,$timestamp,i\na,1970-01-01T00:00:00Z,1\nb,1970-01-01T00:00:00Z,1\n", differing, ErrInvalidRows},
		"table refused":            {"$table,$timestamp,i\na,1970-01-01T00:00:00Z,1\n", func(string) (*arrow.Schema, error) { return nil, refused }, refused},
	} {
		got, err := CSV{}.Decode(context.Background(), strings.NewReader(tc.body), tc.schemaOf)
		if !errors.Is(err, tc.want) || got != nil {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}

// TestNDJSONDecodeFaults checks that each fault of a body is
// ErrInvalidRows, and that a table schemaOf refuses surfaces the error
// of schemaOf itself, unwrapped. The first object fixes the column
// list, so a fault in the list is a header fault and a fault in a later
// object is a row fault.
func TestNDJSONDecodeFaults(t *testing.T) {
	typed := func(dt arrow.DataType) *arrow.Schema {
		return arrow.NewSchema(append(append([]arrow.Field{}, specials...), arrow.Field{Name: "i", Type: dt, Nullable: true}), nil)
	}
	ints := constant(typed(arrow.PrimitiveTypes.Int64))
	strs := constant(typed(arrow.BinaryTypes.String))
	differing := func(name string) (*arrow.Schema, error) {
		if name == "a" {
			return typed(arrow.PrimitiveTypes.Int64), nil
		}
		return typed(arrow.PrimitiveTypes.Float64), nil
	}
	refused := errors.New("no such table")
	row := `{"$table":"a","$timestamp":"1970-01-01T00:00:00Z","i":1}` + "\n"
	for name, tc := range map[string]struct {
		body     string
		schemaOf model.SchemaOf
		want     error
	}{
		"not an object":            {`[1]` + "\n", ints, ErrInvalidRows},
		"not json":                 {`{"$table":` + "\n", ints, ErrInvalidRows},
		"no $table":                {`{"$timestamp":"1970-01-01T00:00:00Z","i":1}` + "\n", ints, ErrInvalidRows},
		"no $timestamp":            {`{"$table":"a","i":1}` + "\n", ints, ErrInvalidRows},
		"unknown column":           {`{"$table":"a","$timestamp":"1970-01-01T00:00:00Z","nope":1}` + "\n", ints, ErrInvalidRows},
		"unknown member later":     {row + `{"$table":"a","$timestamp":"1970-01-01T00:00:00Z","nope":1}` + "\n", ints, ErrInvalidRows},
		"$table not a string":      {`{"$table":1,"$timestamp":"1970-01-01T00:00:00Z","i":1}` + "\n", ints, ErrInvalidRows},
		"null $timestamp":          {`{"$table":"a","$timestamp":null,"i":1}` + "\n", ints, ErrInvalidRows},
		"absent $timestamp later":  {row + `{"$table":"a","i":1}` + "\n", ints, ErrInvalidRows},
		"fraction in an int64":     {`{"$table":"a","$timestamp":"1970-01-01T00:00:00Z","i":1.5}` + "\n", ints, ErrInvalidRows},
		"number in a string":       {`{"$table":"a","$timestamp":"1970-01-01T00:00:00Z","i":1}` + "\n", strs, ErrInvalidRows},
		"tables of differing type": {row + `{"$table":"b","$timestamp":"1970-01-01T00:00:00Z","i":1}` + "\n", differing, ErrInvalidRows},
		"table refused":            {row, func(string) (*arrow.Schema, error) { return nil, refused }, refused},
	} {
		got, err := NDJSON{}.Decode(context.Background(), strings.NewReader(tc.body), tc.schemaOf)
		if !errors.Is(err, tc.want) || got != nil {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}

// TestNDJSONSparseRows checks that a later object may omit a column,
// which reads as null, and may name its members in any order.
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

// arrowBody writes one record batch per fill as an IPC stream over
// fields. Each fill appends the rows of one batch into the builders, one
// per field.
func arrowBody(t *testing.T, fields []arrow.Field, fills ...func(b []array.Builder)) []byte {
	t.Helper()
	schema := arrow.NewSchema(fields, nil)
	var recs []arrow.RecordBatch
	for _, fill := range fills {
		builders := make([]array.Builder, len(fields))
		for i, f := range fields {
			builders[i] = array.NewBuilder(memory.DefaultAllocator, f.Type)
			defer builders[i].Release()
		}
		fill(builders)
		cols := make([]arrow.Array, len(fields))
		for i, b := range builders {
			cols[i] = b.NewArray()
			defer cols[i].Release()
		}
		rec := array.NewRecordBatch(schema, cols, int64(cols[0].Len()))
		defer rec.Release()
		recs = append(recs, rec)
	}
	return ipcStream(t, recs...)
}

// TestArrowDecodeFaults checks that each fault of a body is
// ErrInvalidRows, and that a table schemaOf refuses surfaces the error
// of schemaOf itself, unwrapped. The schema is checked before any
// batch, so a type fault is found on the first row that names a table.
func TestArrowDecodeFaults(t *testing.T) {
	typed := func(dt arrow.DataType) *arrow.Schema {
		return arrow.NewSchema(append(append([]arrow.Field{}, specials...), arrow.Field{Name: "i", Type: dt, Nullable: true}), nil)
	}
	ints := constant(typed(arrow.PrimitiveTypes.Int64))
	refused := errors.New("no such table")
	ns := specials[1].Type
	us := &arrow.TimestampType{Unit: arrow.Microsecond}
	dict := &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Int32, ValueType: arrow.BinaryTypes.String}
	oneRow := func(table string) func(b []array.Builder) {
		return func(b []array.Builder) {
			for _, b := range b {
				switch b := b.(type) {
				case *array.StringBuilder:
					b.Append(table)
				case *array.TimestampBuilder:
					b.Append(0)
				case *array.Int64Builder:
					b.Append(1)
				case *array.Float64Builder:
					b.Append(1)
				case *array.BinaryDictionaryBuilder:
					if err := b.AppendString(table); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
	}
	none := func([]array.Builder) {}
	nullTable := func(b []array.Builder) {
		b[0].AppendNull()
		b[1].(*array.TimestampBuilder).Append(0)
		b[2].(*array.Int64Builder).Append(1)
	}
	for name, tc := range map[string]struct {
		body     []byte
		schemaOf model.SchemaOf
		want     error
	}{
		"not a stream":      {[]byte("nope"), ints, ErrInvalidRows},
		"no $table":         {arrowBody(t, []arrow.Field{specials[1], {Name: "i", Type: arrow.PrimitiveTypes.Int64}}, none), ints, ErrInvalidRows},
		"no $timestamp":     {arrowBody(t, []arrow.Field{specials[0], {Name: "i", Type: arrow.PrimitiveTypes.Int64}}, none), ints, ErrInvalidRows},
		"dictionary $table": {arrowBody(t, []arrow.Field{{Name: "$table", Type: dict}, specials[1], {Name: "i", Type: arrow.PrimitiveTypes.Int64}}, oneRow("a")), ints, ErrInvalidRows},
		"null $table":       {arrowBody(t, []arrow.Field{specials[0], specials[1], {Name: "i", Type: arrow.PrimitiveTypes.Int64}}, nullTable), ints, ErrInvalidRows},
		"unknown column":    {arrowBody(t, []arrow.Field{specials[0], specials[1], {Name: "nope", Type: arrow.PrimitiveTypes.Int64}}, oneRow("a")), ints, ErrInvalidRows},
		"another type":      {arrowBody(t, []arrow.Field{specials[0], specials[1], {Name: "i", Type: arrow.PrimitiveTypes.Float64}}, oneRow("a")), ints, ErrInvalidRows},
		"$timestamp in us":  {arrowBody(t, []arrow.Field{specials[0], {Name: "$timestamp", Type: us}, {Name: "i", Type: arrow.PrimitiveTypes.Int64}}, oneRow("a")), ints, ErrInvalidRows},
		"table refused":     {arrowBody(t, []arrow.Field{specials[0], {Name: "$timestamp", Type: ns}, {Name: "i", Type: arrow.PrimitiveTypes.Int64}}, oneRow("a")), func(string) (*arrow.Schema, error) { return nil, refused }, refused},
	} {
		got, err := Arrow{}.Decode(context.Background(), bytes.NewReader(tc.body), tc.schemaOf)
		if !errors.Is(err, tc.want) || got != nil {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}

// TestArrowInterleavedBatches checks that a stream of several batches
// whose tables interleave decodes to one batch per table, in first-seen
// order, with every row of each table in stream order.
func TestArrowInterleavedBatches(t *testing.T) {
	fields := []arrow.Field{specials[0], specials[1], {Name: "i", Type: arrow.PrimitiveTypes.Int64, Nullable: true}}
	rows := func(names ...string) func(b []array.Builder) {
		return func(b []array.Builder) {
			for k, n := range names {
				b[0].(*array.StringBuilder).Append(n)
				b[1].(*array.TimestampBuilder).Append(arrow.Timestamp(k))
				b[2].(*array.Int64Builder).Append(int64(len(n)))
			}
		}
	}
	body := arrowBody(t, fields, rows("a", "a", "bb", "a"), rows("bb", "bb"))
	got, err := Arrow{}.Decode(context.Background(), bytes.NewReader(body), constant(arrow.NewSchema(fields, nil)))
	if err != nil || len(got) != 2 {
		t.Fatalf("decode: %v, %v", got, err)
	}
	defer func() {
		for _, tb := range got {
			tb.Batch.Release()
		}
	}()
	a, bb := got[0], got[1]
	if a.Table != "a" || a.Batch.NumRows() != 3 || bb.Table != "bb" || bb.Batch.NumRows() != 3 || a.Batch.NumCols() != 2 {
		t.Fatalf("decoded %s %v, %s %v", a.Table, a.Batch, bb.Table, bb.Batch)
	}
	if i := a.Batch.Column(1).(*array.Int64); i.Value(0) != 1 || i.Value(2) != 1 {
		t.Fatalf("table a: %v", a.Batch)
	}
	if i := bb.Batch.Column(1).(*array.Int64); i.Value(0) != 2 || i.Value(2) != 2 {
		t.Fatalf("table bb: %v", bb.Batch)
	}
}
