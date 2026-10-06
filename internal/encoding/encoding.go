// Package encoding turns Arrow record batches into bytes in one wire
// format, and turns a body in one wire format back into batches. An
// encoder or a decoder knows only its media type and its bytes.
package encoding

import (
	"context"
	"io"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/bureau14/qdb-api-rest/internal/model"
)

// Encoder writes record batches to w in one wire format. It does not
// release a batch. It returns the first error it meets: a write error,
// the error of an error step, or the ctx ending between chunks.
type Encoder interface {
	// ContentType is the media type the handler answers with.
	ContentType() string
	// Encode writes one batch. A nil rec is a statement without a result
	// set and encodes as zero columns and zero rows.
	Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error
	// EncodeStream writes a sequence of batches as one body. Every batch
	// shares the first batch's schema. A batch is valid only during its
	// step.
	EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error
}

// Decoder reads a body in one wire format into one batch per table. It
// is the reverse of the format's Encoder. schemaOf supplies the column
// types and is called once for each table the body names. On error the
// decoder returns no batches.
type Decoder interface {
	Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error)
}

// chunkRows is the number of rows an encoder handles between two checks
// of the ctx. On the Arrow wire it is also the size of one record batch.
// On the text wires it is only the stride between ctx checks.
const chunkRows = 65536

// checkChunk looks at the ctx at every chunk boundary, so a client that
// left is noticed within chunkRows rows.
func checkChunk(ctx context.Context, row int64) error {
	if row%chunkRows == 0 {
		return ctx.Err()
	}
	return nil
}

// numRows returns the row count of rec. A nil rec has none.
func numRows(rec arrow.RecordBatch) int64 {
	if rec == nil {
		return 0
	}
	return rec.NumRows()
}

// timestampLayout is how every text format writes a timestamp: RFC 3339
// in UTC with nine fixed fractional digits. It is lossless to the
// nanosecond, every reader parses it, and a fixed width writes faster
// than a trimmed one. The binding's timestamp carries no zone, and
// QuasarDB stores every timestamp in UTC.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// nanosReader returns the reader of a's values as nanoseconds since the
// epoch, whatever unit the field declares.
func nanosReader(a *array.Timestamp) func(i int) int64 {
	unit := int64(a.DataType().(*arrow.TimestampType).Unit.Multiplier())
	return func(i int) int64 { return int64(a.Value(i)) * unit }
}
