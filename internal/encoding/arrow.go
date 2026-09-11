package encoding

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

// arrowBatchRows is the number of rows per record batch on the wire.
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

// emptyBatch is the batch of a statement without a result set: no fields,
// no rows, so the stream is a schema and the end-of-stream marker.
func emptyBatch() arrow.RecordBatch {
	return array.NewRecordBatch(arrow.NewSchema(nil, nil), nil, 0)
}

// writeArrow writes rec to w: the schema as rec carries it, one slice of
// batchRows rows per record batch, the end-of-stream marker. A slice
// shares rec's buffers.
func writeArrow(ctx context.Context, w io.Writer, rec arrow.RecordBatch, batchRows int64) error {
	if rec == nil {
		rec = emptyBatch()
		defer rec.Release()
	}
	ipcw := ipc.NewWriter(w, ipc.WithSchema(rec.Schema()))
	for start := int64(0); start < rec.NumRows(); start += batchRows {
		// A client that left is noticed at the batch boundary.
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
	// Close writes the end-of-stream marker, which completes the stream
	// even with no batches.
	return ipcw.Close()
}
