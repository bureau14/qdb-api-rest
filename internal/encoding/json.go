package encoding

import (
	"bufio"
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
)

const (
	// JSONContentType is the media type of the columnar JSON result.
	JSONContentType = "application/json"
	// NDJSONContentType is the media type of newline-delimited JSON, one
	// object per row.
	NDJSONContentType = "application/x-ndjson"
)

// The two JSON encoders append every cell straight into the buffered
// writer's spare capacity (AvailableBuffer) and hand the bytes back, so
// the hot loop allocates nothing. The buffered writer is flushed once at
// the end of Encode: internal buffering, not the HTTP flush, which is the
// handler's.

// NDJSON encodes a record batch as one JSON object per row, keys in
// column order, one LF-terminated line per row. A batch with no rows, a
// nil batch included, is an empty body.
type NDJSON struct{}

// ContentType implements Encoder.
func (NDJSON) ContentType() string { return NDJSONContentType }

// Encode implements Encoder.
func (NDJSON) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	cols, err := bindColumns(rec)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if err := writeNDJSON(ctx, bw, cols, numRows(rec)); err != nil {
		return err
	}
	return bw.Flush()
}

// writeNDJSON writes the rows. Each key is escaped once and reused with
// its colon for every row.
func writeNDJSON(ctx context.Context, w *bufio.Writer, cols []column, rows int64) error {
	keys := make([][]byte, len(cols))
	for i, c := range cols {
		keys[i] = append(appendQuoted(nil, c.name), ':')
	}
	for row := int64(0); row < rows; row++ {
		if err := checkChunk(ctx, row); err != nil {
			return err
		}
		b := append(w.AvailableBuffer(), '{')
		for i, c := range cols {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, keys[i]...)
			b = c.json(b, int(row))
		}
		b = append(b, "}\n"...)
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	return nil
}
