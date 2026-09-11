package encoding

import (
	"context"
	"encoding/csv"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
)

// CSVContentType is the media type of RFC 4180 text.
const CSVContentType = "text/csv"

// CSV encodes a record batch as RFC 4180 through encoding/csv: a header
// row of the column names, LF line endings, a field quoted only when the
// standard writer's rule says so (a comma, a quote, a CR, an LF, a
// leading space), the empty field for null. A nil batch has no columns,
// so no header, so an empty body; a batch with no rows is the header
// alone. The per-row record of strings is the cost of the standard
// writer's interface, accepted.
type CSV struct{}

// ContentType implements Encoder.
func (CSV) ContentType() string { return CSVContentType }

// Encode implements Encoder.
func (CSV) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	cols, err := bindColumns(rec)
	if err != nil || len(cols) == 0 {
		return err
	}
	return writeCSV(ctx, w, cols, numRows(rec))
}

// writeCSV writes the header, then one record per row; the one record
// slice is reused. The writer buffers on its own and reports the first
// write error at the end.
func writeCSV(ctx context.Context, w io.Writer, cols []column, rows int64) error {
	cw := csv.NewWriter(w)
	record := make([]string, len(cols))
	for i, c := range cols {
		record[i] = c.name
	}
	if err := cw.Write(record); err != nil {
		return err
	}
	for row := int64(0); row < rows; row++ {
		if err := checkChunk(ctx, row); err != nil {
			return err
		}
		for i, c := range cols {
			record[i] = c.text(int(row))
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
