package encoding

import (
	"context"
	"io"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

// ArrowContentType is the media type of the Arrow IPC streaming format.
const ArrowContentType = "application/vnd.apache.arrow.stream"

// Arrow encodes record batches as an Arrow IPC stream: the schema, each
// batch in slices, the end-of-stream marker.
type Arrow struct{}

// ContentType implements Encoder.
func (Arrow) ContentType() string { return ArrowContentType }

// emptyBatch is the batch of a statement without a result set: no fields,
// no rows, so the stream is a schema and the end-of-stream marker.
func emptyBatch() arrow.RecordBatch {
	return array.NewRecordBatch(arrow.NewSchema(nil, nil), nil, 0)
}

// writeSlices writes rec to ipcw as one record batch per batchRows rows.
// A slice shares rec's buffers.
func writeSlices(ctx context.Context, ipcw *ipc.Writer, rec arrow.RecordBatch, batchRows int64) error {
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
	return nil
}

// writeArrow writes rec to w: the schema as rec carries it, one slice of
// batchRows rows per record batch, the end-of-stream marker.
func writeArrow(ctx context.Context, w io.Writer, rec arrow.RecordBatch, batchRows int64) error {
	if rec == nil {
		rec = emptyBatch()
		defer rec.Release()
	}
	ipcw := ipc.NewWriter(w, ipc.WithSchema(rec.Schema()))
	if err := writeSlices(ctx, ipcw, rec, batchRows); err != nil {
		return err
	}
	// Close writes the end-of-stream marker, which completes the stream
	// even with no batches.
	return ipcw.Close()
}

// Encode implements Encoder.
func (Arrow) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	return writeArrow(ctx, w, rec, chunkRows)
}

// EncodeStream implements Encoder: the writer opens on the first batch's
// schema, every batch is written in slices, the marker closes the stream.
// No batch at all is the empty stream.
func (Arrow) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	var ipcw *ipc.Writer
	for rec, err := range batches {
		if err != nil {
			return err
		}
		if ipcw == nil {
			ipcw = ipc.NewWriter(w, ipc.WithSchema(rec.Schema()))
		}
		if err := writeSlices(ctx, ipcw, rec, chunkRows); err != nil {
			return err
		}
	}
	if ipcw == nil {
		return writeArrow(ctx, w, nil, chunkRows)
	}
	return ipcw.Close()
}
