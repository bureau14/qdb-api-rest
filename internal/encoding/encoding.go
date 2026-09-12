// Package encoding turns one query result into bytes in one wire format.
// Every encoder consumes the same Arrow record batch the query core
// returns (internal/qdb); an encoder knows its media type and its bytes.
package encoding

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// Encoder writes one record batch to w in one wire format.
type Encoder interface {
	// ContentType is the media type the handler answers with.
	ContentType() string
	// Encode writes rec to w. A nil rec is a statement that produced no
	// result set and encodes as zero columns and zero rows. Encode never
	// releases rec, which its caller owns. Encode returns the first write
	// error; ctx ending between chunks ends the encoding.
	Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error
}

// chunkRows is the run of rows an encoder handles between two looks at
// the ctx: one record batch on the Arrow wire, one stride between ctx
// checks on the rendered wires.
const chunkRows = 65536

// checkChunk looks at the ctx at every chunk boundary, so a client that
// left is noticed within chunkRows rows.
func checkChunk(ctx context.Context, row int64) error {
	if row%chunkRows == 0 {
		return ctx.Err()
	}
	return nil
}

// numRows is rec's row count; a nil rec has none.
func numRows(rec arrow.RecordBatch) int64 {
	if rec == nil {
		return 0
	}
	return rec.NumRows()
}

// timestampLayout is how every text format writes a timestamp: RFC 3339
// in UTC with nine fixed fractional digits, lossless to the nanosecond,
// parsed by every reader, and fixed width writes faster than a trimmed
// one. The binding's timestamp is naive; QuasarDB stores every timestamp
// in UTC.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// nanosReader returns the reader of a's values as nanoseconds since the
// epoch, whatever unit the field declares.
func nanosReader(a *array.Timestamp) func(i int) int64 {
	unit := int64(a.DataType().(*arrow.TimestampType).Unit.Multiplier())
	return func(i int) int64 { return int64(a.Value(i)) * unit }
}
