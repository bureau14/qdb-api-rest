package encoding

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// The text of a cell's value, shared by the rendering encoders. Each
// encoder binds these to its own null and quoting in its own file.

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

// timestampLayout is RFC 3339 in UTC with nine fixed fractional digits:
// lossless to the nanosecond, universally parsed, and fixed width writes
// faster than a trimmed one.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// appendTimestamp appends nanos since the epoch in timestampLayout. The
// binding's timestamp is naive; QuasarDB stores every timestamp in UTC.
func appendTimestamp(dst []byte, nanos int64) []byte {
	return time.Unix(0, nanos).UTC().AppendFormat(dst, timestampLayout)
}

// nanos returns the reader of a's values as nanoseconds since the epoch,
// whatever unit the field declares.
func nanos(a *array.Timestamp) func(i int) int64 {
	unit := int64(a.DataType().(*arrow.TimestampType).Unit.Multiplier())
	return func(i int) int64 { return int64(a.Value(i)) * unit }
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

// appendBase64 appends b in the standard alphabet with padding, as
// encoding/json renders bytes: what every client library decodes without
// configuration.
func appendBase64(dst []byte, b []byte) []byte {
	return base64.StdEncoding.AppendEncode(dst, b)
}
