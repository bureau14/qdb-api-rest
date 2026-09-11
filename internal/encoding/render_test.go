// The rendering encoders are pinned by one round trip against the live
// qdbd fixture: a generated table is read back as the binding's record
// batch, encoded as JSON, NDJSON and CSV, each body decoded with the
// standard library, and every cell compared with the table that was
// written, nulls included. What the fixture cannot write (NaN, the empty
// string, the characters CSV quotes, invalid UTF-8) is pinned byte for
// byte on one hand-built batch.
package encoding

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

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

// wordOf is the wire type of a table column type.
func wordOf(kind qdbapi.TsColumnType) string {
	switch kind {
	case qdbapi.TsColumnInt64:
		return "int64"
	case qdbapi.TsColumnDouble:
		return "double"
	case qdbapi.TsColumnString, qdbapi.TsColumnSymbol:
		return "string"
	case qdbapi.TsColumnBlob:
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

// checkCells compares one rendered column with the column that was
// written: name, wire type where carried, every validity bit, and every
// value parsed back from its text.
func checkCells(t failer, want table.Column, got wireColumn) {
	t.Helper()
	if got.name != want.Name {
		t.Fatalf("column %q on the wire, %q written", got.name, want.Name)
	}
	if got.kind != "" && got.kind != wordOf(want.Type) {
		t.Fatalf("%s: type %q on the wire, want %q", want.Name, got.kind, wordOf(want.Type))
	}
	if len(got.cells) != len(want.Valid) {
		t.Fatalf("%s: %d rows on the wire, %d written", want.Name, len(got.cells), len(want.Valid))
	}
	for i, valid := range want.Valid {
		if got.cells[i].valid != valid {
			t.Fatalf("%s row %d: valid %v on the wire, %v written", want.Name, i, got.cells[i].valid, valid)
		}
	}
	switch want.Type {
	case qdbapi.TsColumnInt64:
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataInt64Unsafe(want.Data), parsed(t, want.Name, got.cells, parseInt), same[int64])
	case qdbapi.TsColumnDouble:
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataDoubleUnsafe(want.Data), parsed(t, want.Name, got.cells, parseFloat), same[float64])
	case qdbapi.TsColumnTimestamp:
		checkValues(t, want.Name, want.Valid, nanosOf(want.Data), parsed(t, want.Name, got.cells, parseTimestamp), same[int64])
	case qdbapi.TsColumnString, qdbapi.TsColumnSymbol:
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataStringUnsafe(want.Data), parsed(t, want.Name, got.cells, parseText), same[string])
	case qdbapi.TsColumnBlob:
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataBlobUnsafe(want.Data), parsed(t, want.Name, got.cells, base64.StdEncoding.DecodeString), bytes.Equal)
	default:
		t.Fatalf("%s: unexpected column type %v", want.Name, want.Type)
	}
}

// checkRendered compares a rendered body with the columns that were
// written, in order.
func checkRendered(t failer, want []table.Column, got []wireColumn) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d columns on the wire, %d written", len(got), len(want))
	}
	for i, col := range want {
		checkCells(t, col, got[i])
	}
}

// TestRenderedRoundTrip: what the three rendering encoders put on the
// wire decodes to the table it was given, whatever the types, the nulls
// and the row count.
func TestRenderedRoundTrip(t *testing.T) {
	c := newCluster(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, c, tbl)
		rec := run(rt, c, tbl.Select())
		defer rec.Release()

		want := columns(tbl)
		names := make([]string, len(want))
		for i, col := range want {
			names[i] = col.Name
		}
		checkRendered(rt, want, decodeJSON(rt, encode(rt, JSON{}, rec)))
		checkRendered(rt, want, decodeNDJSON(rt, encode(rt, NDJSON{}, rec), names))
		checkRendered(rt, want, decodeCSV(rt, encode(rt, CSV{}, rec)))
	})
}

// edgeBatch is what the fixture cannot write: the int64 extremes, NaN
// and an infinity, a float above 1e21, the epoch and the nanosecond
// before it, the empty string, a string CSV must quote, a leading space,
// invalid UTF-8, the empty blob, and a null in every column.
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
