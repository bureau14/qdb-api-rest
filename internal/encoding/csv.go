package encoding

import (
	"context"
	"encoding/csv"
	"io"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// CSVContentType is the media type of RFC 4180 text.
const CSVContentType = "text/csv"

// csvCell binds column a to its CSV text: the field of cell i, the
// empty field for null. The type switch runs once per column, so a cell
// is one call. The per-cell string is the cost of the standard writer's
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
			if a.IsNull(i) || floatIsNull(a.Value(i)) {
				return ""
			}
			return string(appendFloat(nil, a.Value(i)))
		}, nil
	case *array.Timestamp:
		ns := nanos(a)
		return func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return string(appendTimestamp(nil, ns(i)))
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
			return string(appendBase64(nil, a.Value(i)))
		}, nil
	}
	return nil, &UnsupportedTypeError{Column: f.Name, Type: f.Type}
}

// CSV encodes a record batch as RFC 4180 through encoding/csv: a header
// row of the column names, LF line endings, a field quoted only when the
// standard writer's rule says so (a comma, a quote, a CR, an LF, a
// leading space), the empty field for null. A nil batch has no columns,
// so no header, so an empty body; a batch with no rows is the header
// alone.
type CSV struct{}

// ContentType implements Encoder.
func (CSV) ContentType() string { return CSVContentType }

// Encode implements Encoder.
func (CSV) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	if rec == nil {
		return nil
	}
	names := make([]string, rec.NumCols())
	cells := make([]func(int) string, rec.NumCols())
	for i, f := range rec.Schema().Fields() {
		cell, err := csvCell(f, rec.Column(i))
		if err != nil {
			return err
		}
		names[i], cells[i] = f.Name, cell
	}
	return writeCSV(ctx, w, names, cells, rec.NumRows())
}

// writeCSV writes the header, then one record per row through the one
// reused record. The writer buffers on its own and reports the first
// write error at the end.
func writeCSV(ctx context.Context, w io.Writer, names []string, cells []func(int) string, rows int64) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(names); err != nil {
		return err
	}
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
	cw.Flush()
	return cw.Error()
}
