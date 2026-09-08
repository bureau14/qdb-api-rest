package encoding

import (
	"context"
	"fmt"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// timestampNanosUTC is the Arrow type of every timestamp column: the result
// set already holds int64 nanoseconds since the Unix epoch in UTC, so the
// values buffer is used as it is and no precision is lost.
var timestampNanosUTC = &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"}

// validity wraps a result column's mask as an Arrow validity bitmap. The
// mask is already the bitmap: one bit per slot, LSB-first, set when the
// slot holds a value, padding bits clear.
func validity(m qdbapi.Mask) *memory.Buffer {
	return memory.NewBufferBytes(m.Bytes())
}

// fixedWidth views a fixed-width column as Arrow data: the validity bitmap
// and the values buffer, both the column's own memory. Null slots hold the
// binding's sentinel in the values buffer; a reader never looks at the
// bytes of a slot whose validity bit is clear, so the sentinel is not on
// the wire's meaning.
func fixedWidth(dt arrow.DataType, m qdbapi.Mask, values []byte) arrow.Array {
	data := array.NewData(dt, m.Len(), []*memory.Buffer{validity(m), memory.NewBufferBytes(values)}, nil, m.NullCount(), 0)
	defer data.Release()
	return array.MakeFromData(data)
}

// variableWidth views a string or blob column as Arrow data: the validity
// bitmap, the n+1 int32 offsets and the cell bytes, all the column's own
// memory. The column laid its cells back to back in row order with a null
// cell contributing nothing, which is exactly Arrow's offsets-and-data
// layout for Utf8 and Binary, so nothing is copied.
func variableWidth(dt arrow.DataType, m qdbapi.Mask, offsets []int32, cells []byte) arrow.Array {
	buffers := []*memory.Buffer{
		validity(m),
		memory.NewBufferBytes(arrow.Int32Traits.CastToBytes(offsets)),
		memory.NewBufferBytes(cells),
	}
	data := array.NewData(dt, m.Len(), buffers, nil, m.NullCount(), 0)
	defer data.Release()
	return array.MakeFromData(data)
}

// arrowColumn maps one result column onto an Arrow array over the column's
// own buffers. The set of column types is sealed by the binding, so a type
// outside the switch is a programming error, not an input.
func arrowColumn(c qdbapi.QueryColumn) arrow.Array {
	switch c := c.(type) {
	case *qdbapi.QueryColumnInt64:
		// A count(...) aggregate lands here too, an int64 on the wire.
		return fixedWidth(arrow.PrimitiveTypes.Int64, c.Mask, arrow.Int64Traits.CastToBytes(c.Values))
	case *qdbapi.QueryColumnDouble:
		return fixedWidth(arrow.PrimitiveTypes.Float64, c.Mask, arrow.Float64Traits.CastToBytes(c.Values))
	case *qdbapi.QueryColumnTimestamp:
		return fixedWidth(timestampNanosUTC, c.Mask, arrow.Int64Traits.CastToBytes(c.Values))
	case *qdbapi.QueryColumnString:
		// Symbols arrive as strings; no dictionary encoding.
		return variableWidth(arrow.BinaryTypes.String, c.Mask, c.Offsets(), c.Bytes())
	case *qdbapi.QueryColumnBlob:
		return variableWidth(arrow.BinaryTypes.Binary, c.Mask, c.Offsets(), c.Bytes())
	case *qdbapi.QueryColumnNull:
		// Every cell is null, so no type could be inferred: Arrow's Null
		// type carries the length and nothing else, like the column.
		return array.NewNull(c.Len())
	default:
		panic(fmt.Sprintf("encoding: unknown query column type %T", c))
	}
}

// Record builds one Arrow record over the whole result set without copying
// a value: every buffer is memory the result set owns. Every field is
// nullable whatever the rows hold, so a query's schema does not change with
// its data; names are the result's column names verbatim, duplicates
// included, which Arrow allows. A nil set is a record with no fields and no
// rows. The caller releases the record; releasing is a no-op on the
// wrapped buffers, which the garbage collector owns.
func Record(rs *qdbapi.QueryResultSet) arrow.RecordBatch {
	if rs == nil {
		return array.NewRecordBatch(arrow.NewSchema(nil, nil), nil, 0)
	}
	cols := rs.Columns()
	fields := make([]arrow.Field, len(cols))
	arrays := make([]arrow.Array, len(cols))
	for i, c := range cols {
		arrays[i] = arrowColumn(c)
		fields[i] = arrow.Field{Name: c.Name(), Type: arrays[i].DataType(), Nullable: true}
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(rs.RowCount()))
	// The record holds its own reference to each array.
	for _, a := range arrays {
		a.Release()
	}
	return rec
}

// arrowBatchRows is the number of rows per record batch on the wire. The
// C API materializes the whole result before the first byte exists, so the
// batch size bounds nothing on the server; it is the consumer's
// granularity, and a constant rather than configuration for that reason.
const arrowBatchRows = 65536

// ArrowContentType is the media type of the Arrow IPC streaming format.
const ArrowContentType = "application/vnd.apache.arrow.stream"

// Arrow encodes a result set as an Arrow IPC stream: the schema, the
// record in batches, the end-of-stream marker. The streaming format, not
// the file format: a response body has no footer to seek to.
type Arrow struct{}

// ContentType implements Encoder.
func (Arrow) ContentType() string { return ArrowContentType }

// Encode implements Encoder.
func (Arrow) Encode(ctx context.Context, w io.Writer, rs *qdbapi.QueryResultSet) error {
	return writeArrow(ctx, w, rs, arrowBatchRows)
}

// writeArrow writes rs to w in batches of batchRows rows. The record is
// built once over the whole set and sliced per batch: a slice shares every
// buffer, and only the offsets of a string or blob slice are rebased by the
// writer, a copy of batchRows int32 values, never of the cells. No
// in-format buffer compression: gzip at the HTTP layer is this milestone's
// compression, and the two are independent.
func writeArrow(ctx context.Context, w io.Writer, rs *qdbapi.QueryResultSet, batchRows int64) error {
	rec := Record(rs)
	defer rec.Release()
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
