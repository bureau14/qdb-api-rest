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
)

// CSVContentType is the media type of RFC 4180 text.
const CSVContentType = "text/csv"

// csvCell binds column a to its CSV text: the field of cell i, the
// empty field for null. Each type renders the way CSV readers expect: an
// integer and a shortest round-trip float as plain text, a timestamp in
// timestampLayout, a string as its own bytes (the writer quotes what
// needs quoting), a blob as standard base64. NaN and the infinities are
// the empty field: CSV has no null token, and NaN is the writer's own
// null for doubles. The type switch runs once per column, so a cell is
// one call; the per-cell string is the cost of the standard writer's
// interface, accepted.
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
		// The empty string is the empty field, like null: encoding/csv
		// never quotes an empty field, and CSV readers fold the two.
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

// writeCSVRows writes one record per row through the one reused record.
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
// leading space), the empty field for null. A nil batch has no columns,
// so no header, so an empty body; a batch with no rows is the header
// alone. The writer buffers on its own and reports the first write error
// at the end.
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
	// One header for the whole body, taken from the first batch since every
	// batch shares its schema; the rows of every batch follow. No batch at
	// all writes nothing, the nil-batch rule.
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
	// here; an error step above returns before the buffer is flushed.
	cw.Flush()
	return cw.Error()
}

// timestampField is the index column of every decoded batch: $timestamp
// as timestamp[ns], never null.
var timestampField = arrow.Field{Name: "$timestamp", Type: &arrow.TimestampType{Unit: arrow.Nanosecond}}

// csvAppender binds one builder to its parse of a CSV field, csvCell
// inverted: the empty field is null, otherwise the text parses as the
// column's type. Its error is the strconv or time error; the caller adds
// the row and the column.
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

// csvTable accumulates one table's rows: the batch's schema, one builder
// per field with its appender, and which CSV field feeds each.
type csvTable struct {
	name      string
	schema    *arrow.Schema
	builders  []array.Builder
	appenders []func(string) error
	fields    []int // the CSV field of each schema field
}

// newCSVTable types the header's data columns by the table's schema:
// $timestamp first, then the header's names in their order, each found
// in the table or ErrInvalidRows.
func newCSVTable(name string, h csvHeader, schemaOf SchemaOf) (*csvTable, error) {
	schema, err := schemaOf(name)
	if err != nil {
		return nil, err
	}
	fields := []arrow.Field{timestampField}
	csvFields := []int{h.timestamp}
	for i, n := range h.names {
		idx := schema.FieldIndices(n)
		if idx == nil {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrInvalidRows, name, n)
		}
		fields = append(fields, schema.Field(idx[0]))
		csvFields = append(csvFields, h.fields[i])
	}
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

// appendRecord parses one record into t's builders; an empty field is
// null, except the index, which cannot be.
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

// batch hands the rows over as one record batch, the builders emptied.
func (t *csvTable) batch() TableBatch {
	cols := make([]arrow.Array, len(t.builders))
	for i, b := range t.builders {
		cols[i] = b.NewArray()
	}
	rec := array.NewRecordBatch(t.schema, cols, int64(cols[0].Len()))
	for _, c := range cols {
		c.Release()
	}
	return TableBatch{Table: t.name, Batch: rec}
}

func (t *csvTable) release() {
	for _, b := range t.builders {
		b.Release()
	}
}

// csvHeader is the body's first record: where $table and $timestamp sit,
// and the data columns' names with the field each occupies.
type csvHeader struct {
	table, timestamp int
	names            []string
	fields           []int
}

// readCSVHeader reads the first record; $table and $timestamp are
// required, every other name is a data column.
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

// Decode implements Decoder: RFC 4180 text in this encoder's dialect, a
// header row naming $table, $timestamp and data columns in any order.
func (CSV) Decode(ctx context.Context, r io.Reader, schemaOf SchemaOf) ([]TableBatch, error) {
	// One pass over the body, records streamed into per-table builders:
	//
	//  1. the header fixes the field count and which fields are the
	//     table, the index and the data columns;
	//  2. each record goes to its table's builders; a table seen for the
	//     first time is typed through schemaOf;
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
			// Both chains stay reachable: the sentinel for the status, the
			// reader's cause for a cap the HTTP layer set on the body.
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, row, err)
		}
		t, ok := tables[rec[h.table]]
		if !ok {
			if t, err = newCSVTable(rec[h.table], h, schemaOf); err != nil {
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
	out := make([]TableBatch, len(order))
	for i, t := range order {
		out[i] = t.batch()
		t.release()
	}
	return out, nil
}
