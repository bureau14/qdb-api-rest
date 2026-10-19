// Tests for the encoders. The round trip needs a live qdbd. It
// generates a table, writes it, queries it back as a record batch,
// appends drawn columns of the DuckDB scalars, encodes that batch in
// every format, parses the output with the standard library and
// compares the result with the batch. The fixture's Check proves first
// that the queried batch is the table that was written. The other tests
// need no cluster. TestEdgeCells pins the exact bytes of values the
// fixture never generates, and the stream tests pin the stream path of
// every encoder on the same hand-built batch.
package encoding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"math"
	"math/big"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// readIPC parses an IPC stream and returns its schema, its columns with
// every batch concatenated, and the number of batches. A stream without
// a batch returns no columns.
func readIPC(t failer, stream []byte) (*arrow.Schema, []arrow.Array, int) {
	t.Helper()
	r, err := ipc.NewReader(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("ipc reader: %v", err)
	}
	defer r.Release()
	perColumn := make([][]arrow.Array, r.Schema().NumFields())
	batches := 0
	for r.Next() {
		batches++
		rec := r.RecordBatch()
		for i, col := range rec.Columns() {
			col.Retain()
			perColumn[i] = append(perColumn[i], col)
		}
	}
	if r.Err() != nil {
		t.Fatalf("ipc read: %v", r.Err())
	}
	if batches == 0 {
		return r.Schema(), nil, 0
	}
	cols := make([]arrow.Array, len(perColumn))
	for i, parts := range perColumn {
		if cols[i], err = array.Concatenate(parts, memory.DefaultAllocator); err != nil {
			t.Fatalf("concatenate column %d: %v", i, err)
		}
	}
	return r.Schema(), cols, batches
}

// cell is one parsed cell: whether it holds a value, and the value's
// text with the format's quoting removed.
type cell struct {
	valid bool
	text  string
}

// wireColumn is one column as parsed from a text format. kind is empty
// when the format carries no type.
type wireColumn struct {
	name  string
	kind  string
	cells []cell
}

func unmarshal(t failer, body []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
}

// cellOf parses a raw JSON scalar. null is an invalid cell, a string is
// unquoted, and a number keeps its own text.
func cellOf(t failer, raw json.RawMessage) cell {
	t.Helper()
	switch {
	case string(raw) == "null":
		return cell{}
	case raw[0] == '"':
		var s string
		unmarshal(t, raw, &s)
		return cell{true, s}
	default:
		return cell{true, string(raw)}
	}
}

// readJSON parses the columnar JSON body.
func readJSON(t failer, body []byte) []wireColumn {
	t.Helper()
	var doc struct {
		Columns []struct {
			Name string
			Type string
			Data []json.RawMessage
		}
	}
	unmarshal(t, body, &doc)
	cols := make([]wireColumn, len(doc.Columns))
	for i, c := range doc.Columns {
		cols[i] = wireColumn{name: c.Name, kind: c.Type}
		for _, raw := range c.Data {
			cols[i].cells = append(cols[i].cells, cellOf(t, raw))
		}
	}
	return cols
}

// readNDJSON parses one object per line into the named columns. An empty
// body has no rows.
func readNDJSON(t failer, body []byte, names []string) []wireColumn {
	t.Helper()
	cols := make([]wireColumn, len(names))
	for i, name := range names {
		cols[i].name = name
	}
	if len(body) == 0 {
		return cols
	}
	for _, line := range bytes.Split(bytes.TrimSuffix(body, []byte("\n")), []byte("\n")) {
		var row map[string]json.RawMessage
		unmarshal(t, line, &row)
		if len(row) != len(names) {
			t.Fatalf("%s: %d members, want %d", line, len(row), len(names))
		}
		for i, name := range names {
			raw, ok := row[name]
			if !ok {
				t.Fatalf("%s: no %s", line, name)
			}
			cols[i].cells = append(cols[i].cells, cellOf(t, raw))
		}
	}
	return cols
}

// readCSV parses the header and the records. An empty field is null.
func readCSV(t failer, body []byte) []wireColumn {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	cols := make([]wireColumn, len(records[0]))
	for i, name := range records[0] {
		cols[i].name = name
	}
	for _, rec := range records[1:] {
		for i, text := range rec {
			cols[i].cells = append(cols[i].cells, cell{text != "", text})
		}
	}
	return cols
}

// parseTimestamp parses an encoded timestamp and checks its width: RFC
// 3339 in UTC with exactly nine fractional digits.
func parseTimestamp(s string) (int64, error) {
	if len(s) != len(timestampLayout) {
		return 0, errors.New("not the fixed nine-digit form")
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	return ts.UnixNano(), err
}

// parseTimeOfDay parses hh:mm:ss.nnnnnnnnn and checks its width: nine
// fixed fractional digits. The hour may be 24. time.Parse was rejected,
// because it refuses hour 24.
func parseTimeOfDay(s string) (int64, error) {
	if len(s) != len("15:04:05.000000000") || s[2] != ':' || s[5] != ':' || s[8] != '.' {
		return 0, errors.New("not the fixed nine-digit form")
	}
	var nanos int64
	for _, f := range []struct {
		digits string
		unit   int64
	}{{s[0:2], 3600e9}, {s[3:5], 60e9}, {s[6:8], 1e9}, {s[9:], 1}} {
		v, err := strconv.ParseUint(f.digits, 10, 64)
		if err != nil {
			return 0, err
		}
		nanos += int64(v) * f.unit
	}
	return nanos, nil
}

// parseDate parses a date in dateLayout as days since the epoch.
func parseDate(s string) (arrow.Date32, error) {
	d, err := time.Parse(dateLayout, s)
	return arrow.Date32(d.Unix() / 86400), err
}

func parseInt(s string) (int64, error)     { return strconv.ParseInt(s, 10, 64) }
func parseUint(s string) (uint64, error)   { return strconv.ParseUint(s, 10, 64) }
func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
func parseText(s string) (string, error)   { return s, nil }

func parseFloat32(s string) (float32, error) {
	v, err := strconv.ParseFloat(s, 32)
	return float32(v), err
}

// asInt64 reads the values of a signed column of any width as the int64
// that parseInt returns.
func asInt64[T int8 | int16 | int32 | int64](a signed[T]) func(int) int64 {
	return func(i int) int64 { return int64(a.Value(i)) }
}

// asUint64 reads the values of an unsigned column of any width as the
// uint64 that parseUint returns.
func asUint64[T uint8 | uint16 | uint32 | uint64](a unsigned[T]) func(int) uint64 {
	return func(i int) uint64 { return uint64(a.Value(i)) }
}

// parsed returns a reader that parses the text of cell i as a V and
// fails the test when it cannot.
func parsed[V any](t failer, name string, cells []cell, parse func(string) (V, error)) func(int) V {
	return func(i int) V {
		v, err := parse(cells[i].text)
		if err != nil {
			t.Fatalf("%s row %d: %q: %v", name, i, cells[i].text, err)
		}
		return v
	}
}

// wantValid reports whether cell i of a is a value on the wire. A null
// is not, and neither is a NaN or an infinity of a float column, which
// every text format writes as null.
func wantValid(a arrow.Array, i int) bool {
	switch a := a.(type) {
	case *array.Float32:
		v := float64(a.Value(i))
		return a.IsValid(i) && !math.IsNaN(v) && !math.IsInf(v, 0)
	case *array.Float64:
		return a.IsValid(i) && !math.IsNaN(a.Value(i)) && !math.IsInf(a.Value(i), 0)
	}
	return a.IsValid(i)
}

// checkValues compares every valid cell of a parsed column with the
// value that was encoded.
func checkValues[V any](t failer, name string, want arrow.Array, value func(int) V, got func(int) V, equal func(V, V) bool) {
	t.Helper()
	for i := range want.Len() {
		if wantValid(want, i) && !equal(got(i), value(i)) {
			t.Fatalf("%s row %d: %v on the wire, %v encoded", name, i, got(i), value(i))
		}
	}
}

func same[V comparable](a, b V) bool { return a == b }

// checkCells compares one parsed column with the column that was
// encoded: the name, the wire type when the format carries one, the
// validity of every cell, and every value parsed back from its text.
func checkCells(t failer, f arrow.Field, want arrow.Array, got wireColumn) {
	t.Helper()
	if got.name != f.Name {
		t.Fatalf("column %q on the wire, %q encoded", got.name, f.Name)
	}
	if got.kind != "" && got.kind != f.Type.Name() {
		t.Fatalf("%s: type %q on the wire, want %q", f.Name, got.kind, f.Type.Name())
	}
	if len(got.cells) != want.Len() {
		t.Fatalf("%s: %d rows on the wire, %d encoded", f.Name, len(got.cells), want.Len())
	}
	for i := range want.Len() {
		if got.cells[i].valid != wantValid(want, i) {
			t.Fatalf("%s row %d: valid %v on the wire, %v encoded", f.Name, i, got.cells[i].valid, wantValid(want, i))
		}
	}
	switch a := want.(type) {
	case *array.Int8:
		checkValues(t, f.Name, a, asInt64(a), parsed(t, f.Name, got.cells, parseInt), same[int64])
	case *array.Int16:
		checkValues(t, f.Name, a, asInt64(a), parsed(t, f.Name, got.cells, parseInt), same[int64])
	case *array.Int32:
		checkValues(t, f.Name, a, asInt64(a), parsed(t, f.Name, got.cells, parseInt), same[int64])
	case *array.Int64:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseInt), same[int64])
	case *array.Uint8:
		checkValues(t, f.Name, a, asUint64(a), parsed(t, f.Name, got.cells, parseUint), same[uint64])
	case *array.Uint16:
		checkValues(t, f.Name, a, asUint64(a), parsed(t, f.Name, got.cells, parseUint), same[uint64])
	case *array.Uint32:
		checkValues(t, f.Name, a, asUint64(a), parsed(t, f.Name, got.cells, parseUint), same[uint64])
	case *array.Uint64:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseUint), same[uint64])
	case *array.Float32:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseFloat32), same[float32])
	case *array.Float64:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseFloat), same[float64])
	case *array.Boolean:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, strconv.ParseBool), same[bool])
	case *array.Decimal128:
		dt := a.DataType().(*arrow.Decimal128Type)
		parse := func(s string) (decimal128.Num, error) { return decimal128.FromString(s, dt.Precision, dt.Scale) }
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parse), same[decimal128.Num])
	case *array.Date32:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseDate), same[arrow.Date32])
	case *array.Time64:
		checkValues(t, f.Name, a, timeNanosReader(a), parsed(t, f.Name, got.cells, parseTimeOfDay), same[int64])
	case *array.Timestamp:
		checkValues(t, f.Name, a, nanosReader(a), parsed(t, f.Name, got.cells, parseTimestamp), same[int64])
	case *array.String:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseText), same[string])
	case *array.Binary:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, base64.StdEncoding.DecodeString), bytes.Equal)
	default:
		t.Fatalf("%s: unexpected column type %s", f.Name, f.Type)
	}
}

// checkText compares a parsed text body with the batch that was encoded,
// column by column.
func checkText(t failer, rec arrow.RecordBatch, got []wireColumn) {
	t.Helper()
	if int64(len(got)) != rec.NumCols() {
		t.Fatalf("%d columns on the wire, %d encoded", len(got), rec.NumCols())
	}
	for i, f := range rec.Schema().Fields() {
		checkCells(t, f, rec.Column(i), got[i])
	}
}

// drawScalar draws a column of type dt and n rows, about one in eight of
// them null. Dates run from 0001-01-01 to 9999-12-31, because outside
// those years dateLayout writes a text no ISO 8601 reader parses, and a
// client that needs such dates reads them through Arrow. Times run from
// 00:00:00 to 24:00:00 inclusive, the range DuckDB answers. Floats
// include NaN and the infinities, so the property pins that every text
// format writes them as null.
func drawScalar(t *rapid.T, dt arrow.DataType, n int) arrow.Array {
	b := array.NewBuilder(memory.DefaultAllocator, dt)
	defer b.Release()
	specials := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}
	for range n {
		if rapid.IntRange(0, 7).Draw(t, "null") == 0 {
			b.AppendNull()
			continue
		}
		switch b := b.(type) {
		case *array.Int8Builder:
			b.Append(rapid.Int8().Draw(t, "int8"))
		case *array.Int16Builder:
			b.Append(rapid.Int16().Draw(t, "int16"))
		case *array.Int32Builder:
			b.Append(rapid.Int32().Draw(t, "int32"))
		case *array.Uint8Builder:
			b.Append(rapid.Uint8().Draw(t, "uint8"))
		case *array.Uint16Builder:
			b.Append(rapid.Uint16().Draw(t, "uint16"))
		case *array.Uint32Builder:
			b.Append(rapid.Uint32().Draw(t, "uint32"))
		case *array.Uint64Builder:
			b.Append(rapid.Uint64().Draw(t, "uint64"))
		case *array.Float32Builder:
			special := rapid.SampledFrom(specials).Draw(t, "special")
			b.Append(rapid.OneOf(rapid.Float32(), rapid.Just(float32(special))).Draw(t, "float32"))
		case *array.BooleanBuilder:
			b.Append(rapid.Bool().Draw(t, "bool"))
		case *array.Decimal128Builder:
			// The unscaled value has at most the precision's digits, so it
			// always fits the column.
			p := dt.(*arrow.Decimal128Type).Precision
			v, _ := new(big.Int).SetString(rapid.StringMatching(fmt.Sprintf(`-?[0-9]{1,%d}`, p)).Draw(t, "decimal"), 10)
			b.Append(decimal128.FromBigInt(v))
		case *array.Date32Builder:
			lo := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Unix() / 86400
			hi := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC).Unix() / 86400
			b.Append(arrow.Date32(rapid.Int64Range(lo, hi).Draw(t, "date32")))
		case *array.Time64Builder:
			unit := int64(dt.(*arrow.Time64Type).Unit.Multiplier())
			b.Append(arrow.Time64(rapid.Int64Range(0, 86400e9/unit).Draw(t, "time64")))
		default:
			t.Fatalf("no draw for %s", dt)
		}
	}
	return b.NewArray()
}

// withScalars returns rec with one column appended per DuckDB scalar
// the table fixture cannot generate: int8, int16, int32, uint8, uint16,
// uint32, uint64, float32, one decimal128 of drawn precision and scale,
// bool, date32, time64[us] and time64[ns]. These types reach the
// encoders only through DuckDB, and the fixture writes QuasarDB tables,
// so the columns are drawn in memory. The caller releases the batch.
func withScalars(t *rapid.T, rec arrow.RecordBatch) arrow.RecordBatch {
	p := rapid.Int32Range(1, 38).Draw(t, "precision")
	types := []arrow.DataType{
		arrow.PrimitiveTypes.Int8, arrow.PrimitiveTypes.Int16, arrow.PrimitiveTypes.Int32,
		arrow.PrimitiveTypes.Uint8, arrow.PrimitiveTypes.Uint16, arrow.PrimitiveTypes.Uint32, arrow.PrimitiveTypes.Uint64,
		arrow.PrimitiveTypes.Float32,
		&arrow.Decimal128Type{Precision: p, Scale: rapid.Int32Range(0, p).Draw(t, "scale")},
		arrow.FixedWidthTypes.Boolean, arrow.FixedWidthTypes.Date32,
		arrow.FixedWidthTypes.Time64us, arrow.FixedWidthTypes.Time64ns,
	}
	fields := rec.Schema().Fields()
	arrays := append([]arrow.Array{}, rec.Columns()...)
	var drawn []arrow.Array
	for i, dt := range types {
		fields = append(fields, arrow.Field{Name: fmt.Sprintf("scalar%d", i), Type: dt, Nullable: true})
		drawn = append(drawn, drawScalar(t, dt, int(rec.NumRows())))
	}
	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), append(arrays, drawn...), rec.NumRows())
	for _, a := range drawn {
		a.Release()
	}
	return out
}

// checkArrow writes rec through the Arrow encoder in batches of
// batchRows rows and compares the stream it parses back with rec: the
// schema, the number of batches, and every column.
func checkArrow(t *rapid.T, rec arrow.RecordBatch, batchRows int64) {
	var buf bytes.Buffer
	if err := writeArrow(context.Background(), &buf, rec, batchRows); err != nil {
		t.Fatalf("encode: %v", err)
	}
	schema, cols, batches := readIPC(t, buf.Bytes())
	if !schema.Equal(rec.Schema()) {
		t.Fatalf("schema on the wire %s != %s", schema, rec.Schema())
	}
	if wantBatches := int((rec.NumRows() + batchRows - 1) / batchRows); batches != wantBatches {
		t.Fatalf("%d batches on the wire, want %d for %d rows of %d", batches, wantBatches, rec.NumRows(), batchRows)
	}
	if batches == 0 {
		return // no rows: the schema and the marker are the whole stream
	}
	for i, f := range rec.Schema().Fields() {
		// ApproxEqual with no tolerance is exact, and unlike Equal it takes
		// a NaN to equal a NaN, which the drawn float32 column holds.
		if !array.ApproxEqual(rec.Column(i), cols[i], array.WithNaNsEqual(true), array.WithAbsTolerance(0)) {
			t.Fatalf("%s: on the wire\n%v\nencoded\n%v", f.Name, cols[i], rec.Column(i))
		}
	}
	for _, col := range cols {
		col.Release()
	}
}

// TestRoundTrip checks that the output of every encoder parses back to
// the batch it was given, for every type the text encoders write, any
// nulls and any row count, and across Arrow batch boundaries.
func TestRoundTrip(t *testing.T) {
	c := cluster.NewInsecure(t)
	rapid.Check(t, func(rt *rapid.T) {
		// 1. A table is generated, written and queried back. The fixture's
		// Check proves that the batch is the table written, so the wire is
		// compared with the batch and only one comparer, the fixture's,
		// decides what was written.
		tbl := table.Generate(rt)
		table.Create(rt, c, tbl)
		queried := run(rt, c, tbl.Select())
		defer queried.Release()
		table.Check(rt, tbl, queried)

		// 2. The DuckDB scalars join the queried batch as drawn columns.
		rec := withScalars(rt, queried)
		defer rec.Release()

		// 3. Every text format parses back to the batch.
		names := make([]string, rec.NumCols())
		for i, f := range rec.Schema().Fields() {
			names[i] = f.Name
		}
		checkText(rt, rec, readJSON(rt, encode(rt, JSON{}, rec)))
		checkText(rt, rec, readNDJSON(rt, encode(rt, NDJSON{}, rec), names))
		checkText(rt, rec, readCSV(rt, encode(rt, CSV{}, rec)))

		// 4. The Arrow stream parses back to the batch. A batch size below
		// the row count is what exercises slicing and the offset rebasing
		// of string and blob columns.
		checkArrow(rt, rec, int64(rapid.IntRange(1, 16).Draw(rt, "batch rows")))
	})
}

// edgeBatch builds one batch of the values the fixture does not generate:
// the int64 extremes, NaN and an infinity, a float above 1e21, the epoch
// and the nanosecond before it, the empty string, invalid UTF-8, the
// empty blob, and a null in every column. It also holds the values the
// byte-level pin needs exactly: a string that CSV must quote and a
// leading space.
func edgeBatch(t *testing.T) arrow.RecordBatch {
	t.Helper()
	mem := memory.DefaultAllocator
	valid := []bool{true, true, true, true, false}
	ib := array.NewInt64Builder(mem)
	ib.AppendValues([]int64{math.MaxInt64, math.MinInt64, 0, 42, 0}, valid)
	fb := array.NewFloat64Builder(mem)
	fb.AppendValues([]float64{math.NaN(), math.Inf(1), 1e21, 0.1, 0}, valid)
	tb := array.NewTimestampBuilder(mem, &arrow.TimestampType{Unit: arrow.Nanosecond})
	tb.AppendValues([]arrow.Timestamp{0, 1, arrow.Timestamp(time.Date(2026, 6, 11, 0, 0, 0, 683000, time.UTC).UnixNano()), -1, 0}, valid)
	sb := array.NewStringBuilder(mem)
	sb.AppendValues([]string{"", "a,b \"q\"\n", " lead", "\xff", ""}, valid)
	bb := array.NewBinaryBuilder(mem, arrow.BinaryTypes.Binary)
	bb.AppendValues([][]byte{{}, {0x00, 0xff}, []byte("hi"), []byte("x"), nil}, valid)

	arrays := []arrow.Array{ib.NewArray(), fb.NewArray(), tb.NewArray(), sb.NewArray(), bb.NewArray()}
	fields := make([]arrow.Field, len(arrays))
	for i, name := range []string{"i", "d", "t", "s", "b"} {
		fields[i] = arrow.Field{Name: name, Type: arrays[i].DataType(), Nullable: true}
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(len(valid)))
	for _, a := range arrays {
		a.Release()
	}
	t.Cleanup(rec.Release)
	return rec
}

// emptyIPCStream is the IPC stream the ipc package writes for a schema
// with no fields and no batches.
func emptyIPCStream(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := ipc.NewWriter(&buf, ipc.WithSchema(arrow.NewSchema(nil, nil))).Close(); err != nil {
		t.Fatalf("ipc close: %v", err)
	}
	return buf.Bytes()
}

// TestEdgeCells pins the exact bytes of the edge batch in every text
// format, and of a nil batch in every format. NaN and the infinity
// become null. Invalid UTF-8 becomes U+FFFD in JSON and stays a raw byte
// in CSV. CSV quotes the leading space and the field that holds a comma,
// a quote and a newline. The empty string becomes the empty field. A nil
// batch, which a statement without a result set answers, is a complete
// body: an empty result in JSON, no bytes in NDJSON and CSV, and in
// Arrow a stream of a schema with no fields and no batches.
func TestEdgeCells(t *testing.T) {
	rec := edgeBatch(t)
	for _, tc := range []struct {
		e    Encoder
		rec  arrow.RecordBatch
		want string
	}{
		{JSON{}, rec, `{"columns":[` +
			`{"name":"i","type":"int64","data":[9223372036854775807,-9223372036854775808,0,42,null]},` +
			`{"name":"d","type":"float64","data":[null,null,1e+21,0.1,null]},` +
			`{"name":"t","type":"timestamp","data":["1970-01-01T00:00:00.000000000Z","1970-01-01T00:00:00.000000001Z","2026-06-11T00:00:00.000683000Z","1969-12-31T23:59:59.999999999Z",null]},` +
			`{"name":"s","type":"utf8","data":["","a,b \"q\"\n"," lead","` + "\xef\xbf\xbd" + `",null]},` +
			`{"name":"b","type":"binary","data":["","AP8=","aGk=","eA==",null]}]}`},
		{NDJSON{}, rec, `{"i":9223372036854775807,"d":null,"t":"1970-01-01T00:00:00.000000000Z","s":"","b":""}` + "\n" +
			`{"i":-9223372036854775808,"d":null,"t":"1970-01-01T00:00:00.000000001Z","s":"a,b \"q\"\n","b":"AP8="}` + "\n" +
			`{"i":0,"d":1e+21,"t":"2026-06-11T00:00:00.000683000Z","s":" lead","b":"aGk="}` + "\n" +
			`{"i":42,"d":0.1,"t":"1969-12-31T23:59:59.999999999Z","s":"` + "\xef\xbf\xbd" + `","b":"eA=="}` + "\n" +
			`{"i":null,"d":null,"t":null,"s":null,"b":null}` + "\n"},
		{CSV{}, rec, "i,d,t,s,b\n" +
			"9223372036854775807,,1970-01-01T00:00:00.000000000Z,,\n" +
			"-9223372036854775808,,1970-01-01T00:00:00.000000001Z,\"a,b \"\"q\"\"\n\",AP8=\n" +
			"0,1e+21,2026-06-11T00:00:00.000683000Z,\" lead\",aGk=\n" +
			"42,0.1,1969-12-31T23:59:59.999999999Z,\xff,eA==\n" +
			",,,,\n"},
		{JSON{}, nil, `{"columns":[]}`},
		{NDJSON{}, nil, ""},
		{CSV{}, nil, ""},
		{Arrow{}, nil, string(emptyIPCStream(t))},
	} {
		if got := string(encode(t, tc.e, tc.rec)); got != tc.want {
			t.Errorf("%s, %d rows:\n got %q\nwant %q", tc.e.ContentType(), numRows(tc.rec), got, tc.want)
		}
	}
}

// steps yields every batch in recs, then err when it is not nil.
func steps(recs []arrow.RecordBatch, err error) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		for _, rec := range recs {
			if !yield(rec, nil) {
				return
			}
		}
		if err != nil {
			yield(nil, err)
		}
	}
}

// encodeStream runs the stream path of e over batches and returns the
// body.
func encodeStream(t *testing.T, e Encoder, batches iter.Seq2[arrow.RecordBatch, error]) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.EncodeStream(context.Background(), &buf, batches); err != nil {
		t.Fatalf("%s: %v", e.ContentType(), err)
	}
	return buf.Bytes()
}

// ipcStream writes recs directly as an IPC stream: one schema, one
// record batch each, and the end marker.
func ipcStream(t *testing.T, recs ...arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(recs[0].Schema()))
	for _, rec := range recs {
		if err := w.Write(rec); err != nil {
			t.Fatalf("ipc write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("ipc close: %v", err)
	}
	return buf.Bytes()
}

// TestStreamIsBatchesJoined checks that two batches stream as the IPC
// stream with two record batches, the CSV with one header, the NDJSON
// lines appended, and a JSON array of two results.
func TestStreamIsBatchesJoined(t *testing.T) {
	rec := edgeBatch(t)
	csv := encode(t, CSV{}, rec)
	header := csv[:bytes.IndexByte(csv, '\n')+1]
	ndjson := encode(t, NDJSON{}, rec)
	json := encode(t, JSON{}, rec)
	for _, tc := range []struct {
		e    Encoder
		want []byte
	}{
		{Arrow{}, ipcStream(t, rec, rec)},
		{CSV{}, append(csv[:len(csv):len(csv)], csv[len(header):]...)},
		{NDJSON{}, append(ndjson[:len(ndjson):len(ndjson)], ndjson...)},
		{JSON{}, []byte("[" + string(json) + "," + string(json) + "]")},
	} {
		if got := encodeStream(t, tc.e, steps([]arrow.RecordBatch{rec, rec}, nil)); !bytes.Equal(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", tc.e.ContentType(), got, tc.want)
		}
	}
}

// TestStreamErrorStep checks that an error step after a batch ends every
// encoder's stream with that error.
func TestStreamErrorStep(t *testing.T) {
	rec := edgeBatch(t)
	errStep := errors.New("fetch failed")
	for _, e := range []Encoder{Arrow{}, CSV{}, NDJSON{}, JSON{}} {
		var buf bytes.Buffer
		if err := e.EncodeStream(context.Background(), &buf, steps([]arrow.RecordBatch{rec}, errStep)); !errors.Is(err, errStep) {
			t.Errorf("%s: error %v, want %v", e.ContentType(), err, errStep)
		}
	}
}
