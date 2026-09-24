// The CSV decoder is pinned on a hand-built batch, no cluster: what the
// encoder wrote decodes back to the same batch, table by table, and the
// body's faults are ErrInvalidRows.
package encoding

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// csvBatch is one table's rows as the decoder answers them and as CSV can
// carry them: no NaN, no infinity, no empty string, no empty blob (all
// four are the empty field on the wire, which reads back as null), one
// null per column otherwise, the int64 extremes, a float above 1e21, the
// epoch and the nanosecond before it, the characters CSV quotes, invalid
// UTF-8.
func csvBatch(t *testing.T) arrow.RecordBatch {
	t.Helper()
	mem := memory.DefaultAllocator
	valid := []bool{true, true, true, true, false}
	tb := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	tb.AppendValues([]arrow.Timestamp{0, 1, arrow.Timestamp(time.Date(2026, 6, 11, 0, 0, 0, 683000, time.UTC).UnixNano()), -1, 2}, nil)
	ib := array.NewInt64Builder(mem)
	ib.AppendValues([]int64{math.MaxInt64, math.MinInt64, 0, 42, 0}, valid)
	fb := array.NewFloat64Builder(mem)
	fb.AppendValues([]float64{-1.5, 1e21, 0.1, 3, 0}, valid)
	sb := array.NewStringBuilder(mem)
	sb.AppendValues([]string{"x", "a,b \"q\"\n", " lead", "\xff", ""}, valid)
	bb := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	bb.AppendValues([][]byte{[]byte("z"), {0x00, 0xff}, []byte("hi"), []byte("x"), nil}, valid)

	arrays := []arrow.Array{tb.NewArray(), ib.NewArray(), fb.NewArray(), sb.NewArray(), bb.NewArray()}
	fields := []arrow.Field{timestampField}
	for i, name := range []string{"i", "d", "s", "b"} {
		fields = append(fields, arrow.Field{Name: name, Type: arrays[i+1].DataType(), Nullable: true})
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(len(valid)))
	for _, a := range arrays {
		a.Release()
	}
	t.Cleanup(rec.Release)
	return rec
}

// withTable is rec as an ingest body carries it: a $table column of name
// in front.
func withTable(t *testing.T, name string, rec arrow.RecordBatch) arrow.RecordBatch {
	t.Helper()
	sb := array.NewStringBuilder(memory.DefaultAllocator)
	for range rec.NumRows() {
		sb.Append(name)
	}
	col := sb.NewArray()
	defer col.Release()
	fields := append([]arrow.Field{{Name: "$table", Type: arrow.BinaryTypes.String}}, rec.Schema().Fields()...)
	cols := append([]arrow.Array{col}, rec.Columns()...)
	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
	t.Cleanup(out.Release)
	return out
}

// schemaOf answers rec's data columns for every table name.
func schemaOf(rec arrow.RecordBatch) SchemaOf {
	return func(string) (*arrow.Schema, error) {
		return arrow.NewSchema(rec.Schema().Fields()[1:], nil), nil
	}
}

// TestCSVDecodesWhatItEncoded: the rows of two tables in one body, the
// second table's rows under the first's header, come back as two batches
// in first-seen order, each equal to what was encoded.
func TestCSVDecodesWhatItEncoded(t *testing.T) {
	rec := csvBatch(t)
	a := encode(t, CSV{}, withTable(t, "a", rec))
	b := encode(t, CSV{}, withTable(t, "b", rec))
	body := append(a, b[bytes.IndexByte(b, '\n')+1:]...)
	got, err := CSV{}.Decode(context.Background(), bytes.NewReader(body), schemaOf(rec))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 || got[0].Table != "a" || got[1].Table != "b" {
		t.Fatalf("decoded %d tables: %+v", len(got), got)
	}
	for _, tb := range got {
		if !array.RecordEqual(rec, tb.Batch) {
			t.Errorf("table %s decoded as\n%v\nwant\n%v", tb.Table, tb.Batch, rec)
		}
		tb.Batch.Release()
	}
}

// TestCSVDecodeFaults: each fault of a body is ErrInvalidRows; a table
// schemaOf refuses is that refusal, as is.
func TestCSVDecodeFaults(t *testing.T) {
	rec := csvBatch(t)
	refused := errors.New("no such table")
	for name, tc := range map[string]struct {
		body     string
		schemaOf SchemaOf
		want     error
	}{
		"no $table":        {"$timestamp,i\n", schemaOf(rec), ErrInvalidRows},
		"no $timestamp":    {"$table,i\n", schemaOf(rec), ErrInvalidRows},
		"unknown column":   {"$table,$timestamp,nope\na,1970-01-01T00:00:00Z,1\n", schemaOf(rec), ErrInvalidRows},
		"short record":     {"$table,$timestamp,i\na,1970-01-01T00:00:00Z\n", schemaOf(rec), ErrInvalidRows},
		"unparsable cell":  {"$table,$timestamp,i\na,1970-01-01T00:00:00Z,one\n", schemaOf(rec), ErrInvalidRows},
		"empty $timestamp": {"$table,$timestamp,i\na,,1\n", schemaOf(rec), ErrInvalidRows},
		"table refused":    {"$table,$timestamp,i\na,1970-01-01T00:00:00Z,1\n", func(string) (*arrow.Schema, error) { return nil, refused }, refused},
	} {
		got, err := CSV{}.Decode(context.Background(), strings.NewReader(tc.body), tc.schemaOf)
		if !errors.Is(err, tc.want) || got != nil {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}

// TestCSVDecodeHeaderOnly: a header alone is no table at all.
func TestCSVDecodeHeaderOnly(t *testing.T) {
	got, err := CSV{}.Decode(context.Background(), strings.NewReader("$table,$timestamp,i\n"), schemaOf(csvBatch(t)))
	if err != nil || len(got) != 0 {
		t.Fatalf("%v, %v", got, err)
	}
}
