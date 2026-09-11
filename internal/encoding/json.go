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

// JSON encodes a record batch as one columnar result:
//
//	{"columns":[{"name":"$timestamp","type":"timestamp","data":[...]},...]}
//
// One object per column, keys in that order so a streaming reader knows
// the type before the data; no tables wrapper, one query being one
// result and the table a row came from a column like any other. A nil
// batch is {"columns":[]}. No trailing newline.
type JSON struct{}

// ContentType implements Encoder.
func (JSON) ContentType() string { return JSONContentType }

// Encode implements Encoder.
func (JSON) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	cols, err := bindColumns(rec)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if err := writeJSON(ctx, bw, cols, numRows(rec)); err != nil {
		return err
	}
	return bw.Flush()
}

// writeJSON writes the columns, each array walked once, so the body
// streams column-major over the batch.
func writeJSON(ctx context.Context, w *bufio.Writer, cols []column, rows int64) error {
	if _, err := w.WriteString(`{"columns":[`); err != nil {
		return err
	}
	for i, c := range cols {
		if i > 0 {
			if err := w.WriteByte(','); err != nil {
				return err
			}
		}
		if err := writeJSONColumn(ctx, w, c, rows); err != nil {
			return err
		}
	}
	_, err := w.WriteString("]}")
	return err
}

// writeJSONColumn writes one column object: name, type, then the data
// array of every row.
func writeJSONColumn(ctx context.Context, w *bufio.Writer, c column, rows int64) error {
	b := append(w.AvailableBuffer(), `{"name":`...)
	b = appendQuoted(b, c.name)
	b = append(b, `,"type":"`...)
	b = append(b, c.kind...)
	b = append(b, `","data":[`...)
	if _, err := w.Write(b); err != nil {
		return err
	}
	for row := int64(0); row < rows; row++ {
		if err := checkChunk(ctx, row); err != nil {
			return err
		}
		b := w.AvailableBuffer()
		if row > 0 {
			b = append(b, ',')
		}
		if _, err := w.Write(c.json(b, int(row))); err != nil {
			return err
		}
	}
	_, err := w.WriteString("]}")
	return err
}
