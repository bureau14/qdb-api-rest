package encoding

import (
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// UnsupportedTypeError is the encode error for a column whose Arrow type
// has no rendering: the binding's vocabulary can grow, and the wire
// refuses loudly rather than guess.
type UnsupportedTypeError struct {
	Column string
	Type   arrow.DataType
}

func (e *UnsupportedTypeError) Error() string {
	return fmt.Sprintf("encoding: column %q has type %s, which has no rendering", e.Column, e.Type)
}

// column is one column of the batch bound to the cell vocabulary: its
// name, its wire type in QuasarDB's words (int64, double, string, blob,
// timestamp), and its cell i rendered two ways. json appends the cell as
// a JSON value, null included; text is the cell as CSV carries it, the
// empty field for null. The type switch that binds them runs once per
// column, so rendering a cell is one call.
type column struct {
	name string
	kind string
	json func(dst []byte, i int) []byte
	text func(i int) string
}

// timestampLayout is RFC 3339 in UTC with nine fixed fractional digits:
// lossless to the nanosecond, universally parsed, and fixed width writes
// faster than a trimmed one.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// appendTimestamp appends nanos since the epoch in timestampLayout. The
// binding's timestamp is naive; QuasarDB stores every timestamp in UTC.
func appendTimestamp(dst []byte, nanos int64) []byte {
	return time.Unix(0, nanos).UTC().AppendFormat(dst, timestampLayout)
}

// floatIsNull reports whether f renders as the format's null: NaN and the
// infinities are not JSON, and NaN is the writer's own null for doubles.
func floatIsNull(f float64) bool {
	return math.IsNaN(f) || math.IsInf(f, 0)
}

// appendFloat appends a finite f as a JSON number: shortest round trip,
// plain notation for exponents in [-6, 21), the same bytes encoding/json
// writes. The appender would write NaN and the infinities as strings, so
// floatIsNull guards it.
func appendFloat(dst []byte, f float64) []byte {
	return jsontext.AppendFloat(dst, f, 64)
}

// appendQuoted appends s as a JSON string with the minimal RFC 8785
// escaping. The C API does not validate a utf8 column: invalid bytes
// become U+FFFD, the error that reports them is dropped, and the body
// stays valid JSON.
func appendQuoted(dst []byte, s string) []byte {
	dst, _ = jsontext.AppendQuote(dst, s)
	return dst
}

// appendBase64 appends b in the standard alphabet with padding, as
// encoding/json renders bytes: what every client library decodes without
// configuration.
func appendBase64(dst []byte, b []byte) []byte {
	return base64.StdEncoding.AppendEncode(dst, b)
}

func appendNull(dst []byte) []byte {
	return append(dst, "null"...)
}

// bindColumn binds f's array a to the vocabulary. A symbol column arrives
// as utf8 and answers as a string; a count arrives as int64 and answers
// as one: the wire words are the binding's types.
func bindColumn(f arrow.Field, a arrow.Array) (column, error) {
	c := column{name: f.Name}
	switch a := a.(type) {
	case *array.Int64:
		c.kind = "int64"
		c.json = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return strconv.AppendInt(dst, a.Value(i), 10)
		}
		c.text = func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return strconv.FormatInt(a.Value(i), 10)
		}
	case *array.Float64:
		c.kind = "double"
		c.json = func(dst []byte, i int) []byte {
			if a.IsNull(i) || floatIsNull(a.Value(i)) {
				return appendNull(dst)
			}
			return appendFloat(dst, a.Value(i))
		}
		c.text = func(i int) string {
			if a.IsNull(i) || floatIsNull(a.Value(i)) {
				return ""
			}
			return string(appendFloat(nil, a.Value(i)))
		}
	case *array.Timestamp:
		c.kind = "timestamp"
		// The value is in the field's unit; nanoseconds from the binding.
		unit := int64(a.DataType().(*arrow.TimestampType).Unit.Multiplier())
		nanos := func(i int) int64 { return int64(a.Value(i)) * unit }
		c.json = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			dst = append(dst, '"')
			dst = appendTimestamp(dst, nanos(i))
			return append(dst, '"')
		}
		c.text = func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return string(appendTimestamp(nil, nanos(i)))
		}
	case *array.String:
		c.kind = "string"
		c.json = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return appendQuoted(dst, a.Value(i))
		}
		// The empty string is the empty field, like null: encoding/csv
		// never quotes an empty field, and CSV readers fold the two.
		c.text = a.Value
	case *array.Binary:
		c.kind = "blob"
		c.json = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			dst = append(dst, '"')
			dst = appendBase64(dst, a.Value(i))
			return append(dst, '"')
		}
		c.text = func(i int) string {
			if a.IsNull(i) {
				return ""
			}
			return string(appendBase64(nil, a.Value(i)))
		}
	default:
		return column{}, &UnsupportedTypeError{Column: f.Name, Type: f.Type}
	}
	return c, nil
}

// bindColumns binds every column of rec; a nil rec has none.
func bindColumns(rec arrow.RecordBatch) ([]column, error) {
	if rec == nil {
		return nil, nil
	}
	cols := make([]column, rec.NumCols())
	for i, f := range rec.Schema().Fields() {
		c, err := bindColumn(f, rec.Column(i))
		if err != nil {
			return nil, err
		}
		cols[i] = c
	}
	return cols, nil
}

// numRows is rec's row count; a nil rec has none.
func numRows(rec arrow.RecordBatch) int64 {
	if rec == nil {
		return 0
	}
	return rec.NumRows()
}

// checkChunk looks at the ctx at every chunk boundary, so a client that
// left is noticed within chunkRows rows.
func checkChunk(ctx context.Context, row int64) error {
	if row%chunkRows == 0 {
		return ctx.Err()
	}
	return nil
}
