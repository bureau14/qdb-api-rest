// The decoders are pinned without a cluster: for every codec, tables
// drawn through the fixture go through the encoder as one body and come
// back out of the decoder as the batches drawn; the body's faults are
// one table.
package encoding

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/model"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// codecs pairs every format that decodes with its encoder; a new decoder
// joins the slice and the round trip.
var codecs = []struct {
	Encoder
	Decoder
}{{CSV{}, CSV{}}}

// specials are the two columns the reader answers in front of every
// table; the fault table builds its one-column schemas on them.
var specials = []arrow.Field{
	{Name: "$table", Type: arrow.BinaryTypes.String},
	{Name: "$timestamp", Type: &arrow.TimestampType{Unit: arrow.Nanosecond}},
}

// constant answers schema for every table name.
func constant(schema *arrow.Schema) model.SchemaOf {
	return func(string) (*arrow.Schema, error) { return schema, nil }
}

// TestDecodeRoundTrip: for every codec, one to three tables that share a
// column list are drawn through the fixture and encoded as one body; the
// decoder answers the batches that were drawn, in the order the body
// first names each table, without the tables that have no rows. A body
// that is a header alone decodes to no table at all.
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
				// schema for any name; the tables share one column list, so one
				// schema fits all of them
				got, err := c.Decode(context.Background(), bytes.NewReader(body), constant(table.WithTable(rt, first).Schema()))
				if err != nil {
					rt.Fatalf("decode: %v", err)
				}

				// 4. the decoder answers one batch per table that had rows, in the
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
