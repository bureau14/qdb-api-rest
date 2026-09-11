package encoding

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

// arrowBatchRows is the number of rows per record batch on the wire: the
// consumer's granularity, nothing the server bounds, so a constant.
const arrowBatchRows = 65536

// ArrowContentType is the media type of the Arrow IPC streaming format.
const ArrowContentType = "application/vnd.apache.arrow.stream"

// Arrow encodes a record batch as an Arrow IPC stream: the schema, the
// batch in slices, the end-of-stream marker.
type Arrow struct{}

// ContentType implements Encoder.
func (Arrow) ContentType() string { return ArrowContentType }

// Encode implements Encoder.
func (Arrow) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	return writeArrow(ctx, w, rec, arrowBatchRows)
}

// emptyRecord is the batch of a statement without a result set: no fields,
// no rows, so the stream is a schema and the end-of-stream marker.
func emptyRecord() arrow.RecordBatch {
	return array.NewRecordBatch(arrow.NewSchema(nil, nil), nil, 0)
}

// writeArrow writes rec to w in batches of batchRows rows. The schema goes
// on the wire as rec carries it, names, types, nullability and field
// metadata unread (ADR-0009). A slice shares every buffer of rec; only the
// offsets of a string or blob slice are rebased by the writer, a copy of
// batchRows int32 values, never of the cells. No in-format buffer
// compression: the response's compression is negotiated at the HTTP
// layer, and the two are independent.
func writeArrow(ctx context.Context, w io.Writer, rec arrow.RecordBatch, batchRows int64) error {
	if rec == nil {
		rec = emptyRecord()
		defer rec.Release()
	}
	ipcw := ipc.NewWriter(w, ipc.WithSchema(rec.Schema()))
	for start := int64(0); start < rec.NumRows(); start += batchRows {
		// A client that left is noticed at the next batch boundary, not
		// inside a write: the writer owns the bytes of one batch.
		if err := ctx.Err(); err != nil {
			return err
		}
		batch := rec.NewSlice(start, min(start+batchRows, rec.NumRows()))
		err := ipcw.Write(batch)
		batch.Release()
		if err != nil {
			return err
		}
	}
	// Close writes the end-of-stream marker; with no batches the stream is
	// the schema and the marker, still a complete stream.
	return ipcw.Close()
}
