package encoding

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"io"
	"iter"
	"math"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
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

// EncodeStream implements Encoder: the header from the first batch, then
// every batch's rows. No batch at all is an empty body.
func (CSV) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
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
	cw.Flush()
	return cw.Error()
}
