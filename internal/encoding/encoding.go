// Package encoding turns Arrow record batches into bytes in one wire
// format, and turns a body in one wire format back into batches. An
// encoder or a decoder knows only its media type and its bytes.
package encoding

import (
	"context"
	"encoding/base64"
	"io"
	"iter"
	"strconv"
	"time"

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

// textAppender returns the function that parses one cell of text into
// the builder b, the inverse of the text the encoders write: an integer
// and a float through strconv, a timestamp in RFC 3339, a string as its
// own bytes, a blob through standard base64. The cell text is the same
// on the CSV and NDJSON wires, so both decoders call it. The function
// returns the strconv, time or base64 error as is, and the caller adds
// the row and the column.
func textAppender(f arrow.Field, b array.Builder) (func(string) error, error) {
	// The Builder interface carries no typed Append, so the switch on the
	// concrete builder runs once per column and the cell path is one
	// typed call, the shape arrow-go's own CSV reader takes. arrow-go's
	// AppendValueFromString was rejected: its grammar refuses the Z our
	// timestamps carry, accepts a bare integer as a timestamp, and reads
	// the text "(null)" as null.
	switch b := b.(type) {
	case *array.Int64Builder:
		return func(s string) error {
			v, err := strconv.ParseInt(s, 10, 64)
			b.Append(v)
			return err
		}, nil
	case *array.Float64Builder:
		return func(s string) error {
			v, err := strconv.ParseFloat(s, 64)
			b.Append(v)
			return err
		}, nil
	case *array.TimestampBuilder:
		return func(s string) error {
			t, err := time.Parse(time.RFC3339Nano, s)
			b.Append(arrow.Timestamp(t.UnixNano()))
			return err
		}, nil
	case *array.StringBuilder:
		return func(s string) error {
			b.Append(s)
			return nil
		}, nil
	case *array.BinaryBuilder:
		return func(s string) error {
			v, err := base64.StdEncoding.DecodeString(s)
			b.Append(v)
			return err
		}, nil
	}
	return nil, unsupportedType(f)
}
