package encoding

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
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
