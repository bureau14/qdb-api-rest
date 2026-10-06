package encoding

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/bureau14/qdb-api-rest/internal/model"
)

// CSVContentType is the media type of RFC 4180 text.
const CSVContentType = "text/csv"

// csvCell returns the function that writes cell i of column a as CSV
// text, with the empty field for null. Each type is written the way CSV
// readers expect: an integer and a shortest round-trip float as plain
// text, a timestamp in timestampLayout, a string as its own bytes (the
// writer quotes what needs quoting), a blob as standard base64. NaN and
// the infinities become the empty field, because CSV has no null token
// and NaN is the writer's own null for doubles. The type switch runs
// once per column, so writing a cell is one call. The standard writer's
// interface costs one string allocation per cell, which is accepted.
func csvCell(f arrow.Field, a arrow.Array) (func(i int) string, error) {
	switch a := a.(type) {
	case *array.Int64:
		return func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return strconv.FormatInt(a.Value(i), 10)
		}, nil
	case *array.Float64:
		return func(i int) string {
			if a.IsNull(i) || math.IsNaN(a.Value(i)) || math.IsInf(a.Value(i), 0) {
				return ""
			}
			return strconv.FormatFloat(a.Value(i), 'g', -1, 64)
		}, nil
	case *array.Timestamp:
		nanos := nanosReader(a)
		return func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return time.Unix(0, nanos(i)).UTC().Format(timestampLayout)
		}, nil
	case *array.String:
		// The empty string becomes the empty field, like null. encoding/csv
		// does not quote an empty field, and CSV readers treat the two alike.
		return func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return a.Value(i)
		}, nil
	case *array.Binary:
		return func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return base64.StdEncoding.EncodeToString(a.Value(i))
		}, nil
	}
	return nil, unsupportedType(f)
}

// csvColumns binds every column of rec: the header names and the cells.
func csvColumns(rec arrow.RecordBatch) ([]string, []func(int) string, error) {
	names := make([]string, rec.NumCols())
	cells := make([]func(int) string, rec.NumCols())
	for i, f := range rec.Schema().Fields() {
		cell, err := csvCell(f, rec.Column(i))
		if err != nil {
			return nil, nil, err
		}
		names[i], cells[i] = f.Name, cell
	}
	return names, cells, nil
}

// writeCSVRows writes one CSV record per row, reusing a single record
// slice for all of them.
func writeCSVRows(ctx context.Context, cw *csv.Writer, cells []func(int) string, rows int64) error {
	record := make([]string, len(cells))
	for row := int64(0); row < rows; row++ {
		if err := checkChunk(ctx, row); err != nil {
			return err
		}
		for i, cell := range cells {
			record[i] = cell(int(row))
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	return nil
}

// CSV encodes record batches as RFC 4180 through encoding/csv: a header
// row of the column names, LF line endings, a field quoted only when the
// standard writer's rule says so (a comma, a quote, a CR, an LF, a
// leading space), the empty field for null. A nil batch has no columns
// and therefore no header, so its body is empty. A batch with no rows is
// the header alone. The writer buffers on its own and reports the first
// write error at the end.
type CSV struct{}

// ContentType implements Encoder.
func (CSV) ContentType() string { return CSVContentType }

// Encode implements Encoder.
func (CSV) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	if rec == nil {
		return nil
	}
	names, cells, err := csvColumns(rec)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(names); err != nil {
		return err
	}
	if err := writeCSVRows(ctx, cw, cells, rec.NumRows()); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

// EncodeStream implements Encoder.
func (CSV) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	// The body has one header, taken from the first batch, since every
	// batch shares its schema. The rows of every batch follow. A sequence
	// without any batch writes nothing, as the nil-batch rule says.
	cw := csv.NewWriter(w)
	first := true
	for rec, err := range batches {
		if err != nil {
			return err
		}
		names, cells, err := csvColumns(rec)
		if err != nil {
			return err
		}
		if first {
			if err := cw.Write(names); err != nil {
				return err
			}
			first = false
		}
		if err := writeCSVRows(ctx, cw, cells, rec.NumRows()); err != nil {
			return err
		}
	}
	// The standard writer buffers and reports its first write error only
	// here. An error step above returns before the buffer is flushed.
	cw.Flush()
	return cw.Error()
}

// csvAppender returns the function that parses one CSV field into the
// builder b, the inverse of csvCell. The empty field appends null, and
// any other text is parsed as the column's type. The function returns
// the strconv or time error as is; the caller adds the row and the
// column.
func csvAppender(f arrow.Field, b array.Builder) (func(field string) error, error) {
	switch b := b.(type) {
	case *array.Int64Builder:
		return func(s string) error {
			v, err := strconv.ParseInt(s, 10, 64)
			b.Append(v)
			return err
		}, nil
	case *array.Float64Builder:
		return func(s string) error {
			v, err := strconv.ParseFloat(s, 64)
			b.Append(v)
			return err
		}, nil
	case *array.TimestampBuilder:
		return func(s string) error {
			t, err := time.Parse(time.RFC3339Nano, s)
			b.Append(arrow.Timestamp(t.UnixNano()))
			return err
		}, nil
	case *array.StringBuilder:
		return func(s string) error {
			b.Append(s)
			return nil
		}, nil
	case *array.BinaryBuilder:
		return func(s string) error {
			v, err := base64.StdEncoding.DecodeString(s)
			b.Append(v)
			return err
		}, nil
	}
	return nil, unsupportedType(f)
}

// csvTable accumulates the rows of one table. It holds the batch's
// schema, one builder per field with its appender, and the CSV field
// that feeds each builder.
type csvTable struct {
	name      string
	schema    *arrow.Schema
	builders  []array.Builder
	appenders []func(string) error
	fields    []int // the CSV field of each schema field
}

// newCSVTable types the header's data columns by the table's schema:
// $timestamp first, then the header's names in their order. Each name
// must be a field of the schema, or the result is ErrInvalidRows. A
// table after the first must agree with the first on every field's type,
// or ErrInvalidRows names both tables.
func newCSVTable(name string, h csvHeader, schemaOf model.SchemaOf, first *csvTable) (*csvTable, error) {
	// The batch carries the reader's types, so its fields are picked from
	// the reader's schema rather than declared here:
	//
	//  1. look the table up through schemaOf, and pass its error as is;
	//  2. pick $timestamp and the header's names from the schema by name;
	//     a name the table lacks is the body's fault;
	//  3. check that a table after the first has the same type in every
	//     field: one body is one column list, and the Arrow writer checks
	//     one table at a time, so this decoder has to check it;
	//  4. make one builder and one appender per field.

	// 1. the table's schema
	schema, err := schemaOf(name)
	if err != nil {
		return nil, err
	}

	// 2. the fields, by name
	names := append([]string{"$timestamp"}, h.names...)
	csvFields := append([]int{h.timestamp}, h.fields...)
	fields := make([]arrow.Field, len(names))
	for i, n := range names {
		idx := schema.FieldIndices(n)
		if idx == nil {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrInvalidRows, name, n)
		}
		fields[i] = schema.Field(idx[0])
	}

	// 3. one column list per body
	if first != nil {
		for i, f := range fields {
			if want := first.schema.Field(i); !arrow.TypeEqual(f.Type, want.Type) {
				return nil, fmt.Errorf("%w: tables %s and %s differ in the type of column %s", ErrInvalidRows, first.name, name, f.Name)
			}
		}
	}

	// 4. the builders
	t := &csvTable{name: name, schema: arrow.NewSchema(fields, nil), fields: csvFields}
	for _, f := range fields {
		b := array.NewBuilder(memory.DefaultAllocator, f.Type)
		app, err := csvAppender(f, b)
		if err != nil {
			b.Release()
			t.release()
			return nil, err
		}
		t.builders = append(t.builders, b)
		t.appenders = append(t.appenders, app)
	}
	return t, nil
}

// appendRecord parses one record into the builders of t. An empty field
// is null, except in the index, which cannot be null.
func (t *csvTable) appendRecord(rec []string) error {
	for i, f := range t.fields {
		s := rec[f]
		if s == "" {
			if i == 0 {
				return errors.New("empty $timestamp")
			}
			t.builders[i].AppendNull()
			continue
		}
		if err := t.appenders[i](s); err != nil {
			return fmt.Errorf("column %s: %w", t.schema.Field(i).Name, err)
		}
	}
	return nil
}

// batch returns the rows as one record batch and empties the builders.
func (t *csvTable) batch() model.TableBatch {
	cols := make([]arrow.Array, len(t.builders))
	for i, b := range t.builders {
		cols[i] = b.NewArray()
	}
	rec := array.NewRecordBatch(t.schema, cols, int64(cols[0].Len()))
	for _, c := range cols {
		c.Release()
	}
	return model.TableBatch{Table: t.name, Batch: rec}
}

func (t *csvTable) release() {
	for _, b := range t.builders {
		b.Release()
	}
}

// csvHeader is the parsed first record of a body: the fields where
// $table and $timestamp sit, and the name and field of every data
// column.
type csvHeader struct {
	table, timestamp int
	names            []string
	fields           []int
}

// readCSVHeader reads the first record. $table and $timestamp are
// required, and every other name is a data column.
func readCSVHeader(rd *csv.Reader) (csvHeader, error) {
	rec, err := rd.Read()
	if err != nil {
		return csvHeader{}, fmt.Errorf("%w: header: %w", ErrInvalidRows, err)
	}
	h := csvHeader{table: -1, timestamp: -1}
	for i, name := range rec {
		switch name {
		case "$table":
			h.table = i
		case "$timestamp":
			h.timestamp = i
		default:
			h.names = append(h.names, name)
			h.fields = append(h.fields, i)
		}
	}
	switch {
	case h.table < 0:
		return csvHeader{}, fmt.Errorf("%w: header names no $table", ErrInvalidRows)
	case h.timestamp < 0:
		return csvHeader{}, fmt.Errorf("%w: header names no $timestamp", ErrInvalidRows)
	}
	return h, nil
}

// Decode implements Decoder. The body is RFC 4180 text in this encoder's
// dialect, with a header row that names $table, $timestamp and the data
// columns in any order.
func (CSV) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// The decoder makes one pass over the body and streams the records
	// into per-table builders:
	//
	//  1. the header fixes the field count and says which fields hold the
	//     table, the index and the data columns;
	//  2. each record goes to the builders of its table; a table seen for
	//     the first time is typed through schemaOf and must agree with
	//     the first table's types;
	//  3. at the end every table becomes one batch, in first-seen order.
	rd := csv.NewReader(r)
	rd.ReuseRecord = true
	tables := map[string]*csvTable{}
	var order []*csvTable
	release := func() {
		for _, t := range order {
			t.release()
		}
	}

	// 1. the header
	h, err := readCSVHeader(rd)
	if err != nil {
		return nil, err
	}

	// 2. the records
	for row := int64(1); ; row++ {
		if err := checkChunk(ctx, row); err != nil {
			release()
			return nil, err
		}
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			release()
			// Both error chains stay reachable. The sentinel decides the status,
			// and the reader's cause reveals a body-size cap the HTTP layer set.
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, row, err)
		}
		t, ok := tables[rec[h.table]]
		if !ok {
			var first *csvTable
			if len(order) > 0 {
				first = order[0]
			}
			if t, err = newCSVTable(rec[h.table], h, schemaOf, first); err != nil {
				release()
				return nil, err
			}
			tables[t.name] = t
			order = append(order, t)
		}
		if err := t.appendRecord(rec); err != nil {
			release()
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, row, err)
		}
	}

	// 3. one batch per table
	out := make([]model.TableBatch, len(order))
	for i, t := range order {
		out[i] = t.batch()
		t.release()
	}
	return out, nil
}
