// The Arrow encoder is pinned by one round trip against the live qdbd
// fixture: a generated table is read back as the binding's record batch,
// encoded with a batch size small enough that rows span batches, decoded
// with the IPC reader, and compared column by column with the batch
// encoded, which the fixture's Check has proven to be the table written.
package encoding

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"iter"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// decode reads every batch of an IPC stream and concatenates the batches
// per column, returning the schema, the columns and the batch count. A
// stream with no batch has no columns to return.
func decode(t failer, stream []byte) (*arrow.Schema, []arrow.Array, int) {
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

// TestArrowRoundTrip: what the encoder puts on the wire decodes to the
// batch it was given, whatever the types, the nulls and the row
// count, across batch boundaries.
func TestArrowRoundTrip(t *testing.T) {
	c := cluster.NewInsecure(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, c, tbl)
		rec := run(rt, c, tbl.Select())
		defer rec.Release()
		// The batch is the table written; the wire is then checked against
		// the batch, so one comparer, the fixture's, decides what was written.
		table.Check(rt, tbl, rec)

		// A batch size below the row count is what exercises slicing and
		// the offset rebasing of string and blob columns.
		batchRows := int64(rapid.IntRange(1, 16).Draw(rt, "batch rows"))
		var buf bytes.Buffer
		if err := writeArrow(context.Background(), &buf, rec, batchRows); err != nil {
			rt.Fatalf("encode: %v", err)
		}

		schema, cols, batches := decode(rt, buf.Bytes())
		if !schema.Equal(rec.Schema()) {
			rt.Fatalf("schema on the wire %s != %s", schema, rec.Schema())
		}
		if wantBatches := int((rec.NumRows() + batchRows - 1) / batchRows); batches != wantBatches {
			rt.Fatalf("%d batches on the wire, want %d for %d rows of %d", batches, wantBatches, rec.NumRows(), batchRows)
		}
		if batches == 0 {
			return // no rows: the schema and the marker are the whole stream
		}
		for i, f := range rec.Schema().Fields() {
			if !array.Equal(rec.Column(i), cols[i]) {
				rt.Fatalf("%s: on the wire\n%v\nencoded\n%v", f.Name, cols[i], rec.Column(i))
			}
		}
		for _, col := range cols {
			col.Release()
		}
	})
}

// TestArrowNilBatch: a statement without a result set is a complete
// stream with no fields and no batches.
func TestArrowNilBatch(t *testing.T) {
	schema, _, batches := decode(t, encode(t, Arrow{}, nil))
	if schema.NumFields() != 0 || batches != 0 {
		t.Fatalf("%d fields and %d batches, want none", schema.NumFields(), batches)
	}
}

// cell is one decoded cell: whether it holds a value, and the value's
// text with the format's own quoting removed.
type cell struct {
	valid bool
	text  string
}

// wireColumn is one column as a rendered format delivered it; kind is
// empty where the format carries no type.
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

// cellOf reads a raw JSON scalar: null is invalid, a string is unquoted,
// a number is its own text.
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

// decodeJSON reads the columnar body.
func decodeJSON(t failer, body []byte) []wireColumn {
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

// decodeNDJSON reads one object per line into the named columns; no rows
// is an empty body.
func decodeNDJSON(t failer, body []byte, names []string) []wireColumn {
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

// decodeCSV reads the header and the records; the empty field is null.
func decodeCSV(t failer, body []byte) []wireColumn {
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

// wordOf is the wire type word of a column's Arrow type.
func wordOf(dt arrow.DataType) string {
	switch dt.ID() {
	case arrow.INT64:
		return "int64"
	case arrow.FLOAT64:
		return "double"
	case arrow.STRING:
		return "string"
	case arrow.BINARY:
		return "blob"
	default:
		return "timestamp"
	}
}

// parseTimestamp reads a rendered timestamp: RFC 3339 in UTC with exactly
// nine fractional digits, so the fixed width is pinned too.
func parseTimestamp(s string) (int64, error) {
	if len(s) != len(timestampLayout) {
		return 0, errors.New("not the fixed nine-digit form")
	}
	ts, err := time.Parse(time.RFC3339Nano, s)
	return ts.UnixNano(), err
}

func parseInt(s string) (int64, error)     { return strconv.ParseInt(s, 10, 64) }
func parseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }
func parseText(s string) (string, error)   { return s, nil }

// parsed is the cell reader that parses a valid cell's text as V.
func parsed[V any](t failer, name string, cells []cell, parse func(string) (V, error)) func(int) V {
	return func(i int) V {
		v, err := parse(cells[i].text)
		if err != nil {
			t.Fatalf("%s row %d: %q: %v", name, i, cells[i].text, err)
		}
		return v
	}
}

// checkValues compares every valid slot of a rendered column with the
// value encoded.
func checkValues[V any](t failer, name string, want arrow.Array, value func(int) V, got func(int) V, equal func(V, V) bool) {
	t.Helper()
	for i := range want.Len() {
		if want.IsValid(i) && !equal(got(i), value(i)) {
			t.Fatalf("%s row %d: %v on the wire, %v encoded", name, i, got(i), value(i))
		}
	}
}

func same[V comparable](a, b V) bool { return a == b }

// checkCells compares one rendered column with the column encoded: name,
// wire type where carried, every validity bit, and every value parsed
// back from its text.
func checkCells(t failer, f arrow.Field, want arrow.Array, got wireColumn) {
	t.Helper()
	if got.name != f.Name {
		t.Fatalf("column %q on the wire, %q encoded", got.name, f.Name)
	}
	if got.kind != "" && got.kind != wordOf(f.Type) {
		t.Fatalf("%s: type %q on the wire, want %q", f.Name, got.kind, wordOf(f.Type))
	}
	if len(got.cells) != want.Len() {
		t.Fatalf("%s: %d rows on the wire, %d encoded", f.Name, len(got.cells), want.Len())
	}
	for i := range want.Len() {
		if got.cells[i].valid != want.IsValid(i) {
			t.Fatalf("%s row %d: valid %v on the wire, %v encoded", f.Name, i, got.cells[i].valid, want.IsValid(i))
		}
	}
	switch a := want.(type) {
	case *array.Int64:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseInt), same[int64])
	case *array.Float64:
		checkValues(t, f.Name, a, a.Value, parsed(t, f.Name, got.cells, parseFloat), same[float64])
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

// checkRendered compares a rendered body with the batch encoded, column
// by column in order.
func checkRendered(t failer, rec arrow.RecordBatch, got []wireColumn) {
	t.Helper()
	if int64(len(got)) != rec.NumCols() {
		t.Fatalf("%d columns on the wire, %d encoded", len(got), rec.NumCols())
	}
	for i, f := range rec.Schema().Fields() {
		checkCells(t, f, rec.Column(i), got[i])
	}
}

// TestRenderedRoundTrip: what the three rendering encoders put on the
// wire decodes to the table it was given, whatever the types, the nulls
// and the row count.
func TestRenderedRoundTrip(t *testing.T) {
	c := cluster.NewInsecure(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, c, tbl)
		rec := run(rt, c, tbl.Select())
		defer rec.Release()

		// The batch is the table written; the wire is then checked against
		// the batch, so one comparer, the fixture's, decides what was written.
		table.Check(rt, tbl, rec)
		names := make([]string, rec.NumCols())
		for i, f := range rec.Schema().Fields() {
			names[i] = f.Name
		}
		checkRendered(rt, rec, decodeJSON(rt, encode(rt, JSON{}, rec)))
		checkRendered(rt, rec, decodeNDJSON(rt, encode(rt, NDJSON{}, rec), names))
		checkRendered(rt, rec, decodeCSV(rt, encode(rt, CSV{}, rec)))
	})
}

// edgeBatch is one hand-built batch of what the fixture does not draw
// (the int64 extremes, NaN and an infinity, a float above 1e21, the epoch
// and the nanosecond before it, the empty string, invalid UTF-8, the
// empty blob, a null in every column) and of what the byte-level pin
// wants exactly (a string that CSV must quote, a leading space).
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

// TestEdgeCells pins the bytes of the edge batch in every rendered
// format: NaN and the infinity are null; invalid UTF-8 is U+FFFD in JSON
// and the raw byte in CSV; the leading space and the comma, quote and
// newline are what encoding/csv quotes; the empty string is the empty
// field.
func TestEdgeCells(t *testing.T) {
	rec := edgeBatch(t)
	for _, tc := range []struct {
		e    Encoder
		want string
	}{
		{JSON{}, `{"columns":[` +
			`{"name":"i","type":"int64","data":[9223372036854775807,-9223372036854775808,0,42,null]},` +
			`{"name":"d","type":"double","data":[null,null,1e+21,0.1,null]},` +
			`{"name":"t","type":"timestamp","data":["1970-01-01T00:00:00.000000000Z","1970-01-01T00:00:00.000000001Z","2026-06-11T00:00:00.000683000Z","1969-12-31T23:59:59.999999999Z",null]},` +
			`{"name":"s","type":"string","data":["","a,b \"q\"\n"," lead","` + "\xef\xbf\xbd" + `",null]},` +
			`{"name":"b","type":"blob","data":["","AP8=","aGk=","eA==",null]}]}`},
		{NDJSON{}, `{"i":9223372036854775807,"d":null,"t":"1970-01-01T00:00:00.000000000Z","s":"","b":""}` + "\n" +
			`{"i":-9223372036854775808,"d":null,"t":"1970-01-01T00:00:00.000000001Z","s":"a,b \"q\"\n","b":"AP8="}` + "\n" +
			`{"i":0,"d":1e+21,"t":"2026-06-11T00:00:00.000683000Z","s":" lead","b":"aGk="}` + "\n" +
			`{"i":42,"d":0.1,"t":"1969-12-31T23:59:59.999999999Z","s":"` + "\xef\xbf\xbd" + `","b":"eA=="}` + "\n" +
			`{"i":null,"d":null,"t":null,"s":null,"b":null}` + "\n"},
		{CSV{}, "i,d,t,s,b\n" +
			"9223372036854775807,,1970-01-01T00:00:00.000000000Z,,\n" +
			"-9223372036854775808,,1970-01-01T00:00:00.000000001Z,\"a,b \"\"q\"\"\n\",AP8=\n" +
			"0,1e+21,2026-06-11T00:00:00.000683000Z,\" lead\",aGk=\n" +
			"42,0.1,1969-12-31T23:59:59.999999999Z,\xff,eA==\n" +
			",,,,\n"},
	} {
		if got := string(encode(t, tc.e, rec)); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.e.ContentType(), got, tc.want)
		}
	}
}

// steps is a sequence of recs, then err when non-nil.
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

// encodeStream runs e's stream path over batches and returns the body.
func encodeStream(t *testing.T, e Encoder, batches iter.Seq2[arrow.RecordBatch, error]) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.EncodeStream(context.Background(), &buf, batches); err != nil {
		t.Fatalf("%s: %v", e.ContentType(), err)
	}
	return buf.Bytes()
}

// ipcStream is the IPC stream of recs written directly: one schema, one
// record batch each, the marker.
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

// TestStreamIsBatchesJoined: two batches stream as the IPC stream with
// two record batches, the CSV with one header, the NDJSON lines
// appended, and the JSON array of two results.
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

// TestStreamErrorStep: an error step after a batch ends every encoder's
// stream with that error.
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
