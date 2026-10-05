// The decoders are pinned without a cluster: for every codec, drawn tables
// of a drawn schema go through the encoder as one body and come back out
// of the decoder as the batches drawn; the body's faults are one table.
package encoding

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// codecs pairs every format that decodes with its encoder; a new decoder
// joins the slice and the round trip.
var codecs = []struct {
	Encoder
	Decoder
}{{CSV{}, CSV{}}}

// specials are the two columns the reader answers in front of every table.
var specials = []arrow.Field{
	{Name: "$table", Type: arrow.BinaryTypes.String},
	{Name: "$timestamp", Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
}

// wireTypes are the five types the wires carry.
var wireTypes = []arrow.DataType{
	arrow.PrimitiveTypes.Int64,
	arrow.PrimitiveTypes.Float64,
	&arrow.TimestampType{Unit: arrow.Nanosecond},
	arrow.BinaryTypes.String,
	arrow.BinaryTypes.Binary,
}

// genSchema draws one to six data fields of distinct names over the five
// wire types, every field nullable, behind $table and $timestamp.
func genSchema(rt *rapid.T) *arrow.Schema {
	fields := append([]arrow.Field{}, specials...)
	for i := range rapid.IntRange(1, 6).Draw(rt, "fields") {
		fields = append(fields, arrow.Field{
			Name:     fmt.Sprintf("c%d", i),
			Type:     rapid.SampledFrom(wireTypes).Draw(rt, "type"),
			Nullable: true,
		})
	}
	return arrow.NewSchema(fields, nil)
}

// genCell draws one value of f's type into b; the values the text wires
// carry losslessly: no NaN, no infinity, no empty string, no empty blob
// (the empty field folds all four into null), and no CR, which the CSV
// reader folds into the LF after it.
func genCell(rt *rapid.T, f arrow.Field, b array.Builder) {
	switch b := b.(type) {
	case *array.Int64Builder:
		b.Append(rapid.Int64().Draw(rt, f.Name))
	case *array.Float64Builder:
		b.Append(rapid.Float64Range(-1e300, 1e300).Draw(rt, f.Name))
	case *array.TimestampBuilder:
		b.Append(arrow.Timestamp(rapid.Int64().Draw(rt, f.Name)))
	case *array.StringBuilder:
		b.Append(rapid.StringMatching(`[a-zA-Z0-9 ,"\n]{1,16}`).Draw(rt, f.Name))
	case *array.BinaryBuilder:
		b.Append(rapid.SliceOfN(rapid.Byte(), 1, 16).Draw(rt, f.Name))
	default:
		rt.Fatalf("%s: no generator for %s", f.Name, f.Type)
	}
}

// genBatch draws zero to n rows of schema without the $table column: a
// $timestamp never null, every other cell null at a drawn rate.
func genBatch(rt *rapid.T, schema *arrow.Schema, n int) arrow.RecordBatch {
	fields := schema.Fields()[1:]
	rows := rapid.IntRange(0, n).Draw(rt, "rows")
	nullPct := rapid.IntRange(0, 100).Draw(rt, "null pct")
	cols := make([]arrow.Array, len(fields))
	for i, f := range fields {
		b := array.NewBuilder(memory.DefaultAllocator, f.Type)
		for range rows {
			if i > 0 && rapid.IntRange(0, 99).Draw(rt, "null") < nullPct {
				b.AppendNull()
				continue
			}
			genCell(rt, f, b)
		}
		cols[i] = b.NewArray()
		b.Release()
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(rows))
	for _, c := range cols {
		c.Release()
	}
	rt.Cleanup(rec.Release)
	return rec
}

// withTable is rec as an ingest body carries it: a $table column of name
// in front.
func withTable(t table.T, name string, rec arrow.RecordBatch) arrow.RecordBatch {
	t.Helper()
	sb := array.NewStringBuilder(memory.DefaultAllocator)
	for range rec.NumRows() {
		sb.Append(name)
	}
	col := sb.NewArray()
	defer col.Release()
	fields := append([]arrow.Field{specials[0]}, rec.Schema().Fields()...)
	cols := append([]arrow.Array{col}, rec.Columns()...)
	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
	t.Cleanup(out.Release)
	return out
}

// bodyOf is the batches as one body of e: the first table's bytes whole,
// the rows of the others under its header, which is theirs too.
func bodyOf(t table.T, e Encoder, tables []TableBatch) []byte {
	t.Helper()
	var body []byte
	for i, tb := range tables {
		b := encode(t, e, withTable(t, tb.Table, tb.Batch))
		if i > 0 {
			b = b[bytes.IndexByte(b, '\n')+1:]
		}
		body = append(body, b...)
	}
	return body
}

// constant answers schema for every table name.
func constant(schema *arrow.Schema) SchemaOf {
	return func(string) (*arrow.Schema, error) { return schema, nil }
}

// TestDecodeRoundTrip: for every codec, one to three drawn tables of one
// schema, encoded as one body, decode back to the batches drawn, in
// first-seen order, the empty ones absent; a header alone is no table.
func TestDecodeRoundTrip(t *testing.T) {
	for _, c := range codecs {
		t.Run(c.ContentType(), func(t *testing.T) {
			rapid.Check(t, func(rt *rapid.T) {
				// 1. the schema and the tables, zero rows allowed
				schema := genSchema(rt)
				var drawn, want []TableBatch
				for i := range rapid.IntRange(1, 3).Draw(rt, "tables") {
					tb := TableBatch{Table: fmt.Sprintf("t%d", i), Batch: genBatch(rt, schema, 64)}
					drawn = append(drawn, tb)
					if tb.Batch.NumRows() > 0 {
						want = append(want, tb)
					}
				}

				// 2. one body, 3. decoded under the one schema
				got, err := c.Decode(context.Background(), bytes.NewReader(bodyOf(rt, c, drawn)), constant(schema))
				if err != nil {
					rt.Fatalf("decode: %v", err)
				}

				// 4. the batches with rows, in first-seen order, equal to the drawn
				if len(got) != len(want) {
					rt.Fatalf("decoded %d tables, want %d", len(got), len(want))
				}
				for i, tb := range got {
					if tb.Table != want[i].Table || !array.RecordEqual(want[i].Batch, tb.Batch) {
						rt.Errorf("table %d decoded as %s\n%v\nwant %s\n%v", i, tb.Table, tb.Batch, want[i].Table, want[i].Batch)
					}
					tb.Batch.Release()
				}
			})
		})
	}
}

// TestCSVDecodeFaults: each fault of a body is ErrInvalidRows, naming the
// row and the column; a table schemaOf refuses is that refusal, as is.
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
		schemaOf SchemaOf
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
