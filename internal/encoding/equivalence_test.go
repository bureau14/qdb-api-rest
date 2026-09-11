// The four formats of one batch are pinned against each other by one
// property: a generated table, queried once, encoded four ways, each body
// decoded with the standard library back to a per-column, per-cell view,
// and every cell compared with the table that was written, validity
// included. Each decoder knows its format's null and its quoting, nothing
// else. What the fixture cannot draw (NaN, the characters CSV quotes,
// invalid UTF-8, the empty string) is pinned on one hand-built batch.
package encoding

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"encoding/json/jsontext"
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

// cell is one decoded cell of a rendered format: whether it holds a value
// and the value's text with the format's own quoting removed.
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

// pair is one name/value member of a JSON object, the value raw.
type pair struct {
	name  string
	value jsontext.Value
}

// readToken reads the next token and asserts its kind.
func readToken(t failer, dec *jsontext.Decoder, want jsontext.Kind) jsontext.Token {
	t.Helper()
	tok, err := dec.ReadToken()
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if tok.Kind() != want {
		t.Fatalf("token %v, want kind %v", tok, want)
	}
	return tok
}

// readPairs reads one object as its members in wire order, so key order
// is observable.
func readPairs(t failer, dec *jsontext.Decoder) []pair {
	t.Helper()
	readToken(t, dec, '{')
	var ps []pair
	for dec.PeekKind() != '}' {
		name := readToken(t, dec, '"').String()
		v, err := dec.ReadValue()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ps = append(ps, pair{name, v.Clone()})
	}
	readToken(t, dec, '}')
	return ps
}

// cellOf turns a raw JSON scalar into a cell: null is invalid, a string
// is unquoted, a number is its own text.
func cellOf(t failer, v jsontext.Value) cell {
	t.Helper()
	switch v.Kind() {
	case 'n':
		return cell{}
	case '"':
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			t.Fatalf("%s: %v", v, err)
		}
		return cell{true, s}
	default:
		return cell{true, string(v)}
	}
}

// unmarshal decodes v into out or fails.
func unmarshal(t failer, v jsontext.Value, out any) {
	t.Helper()
	if err := json.Unmarshal(v, out); err != nil {
		t.Fatalf("%s: %v", v, err)
	}
}

// decodeJSON reads the columnar body: {"columns":[{name,type,data}...]},
// the column keys in that order, no trailing newline.
func decodeJSON(t failer, body []byte) []wireColumn {
	t.Helper()
	if bytes.HasSuffix(body, []byte("\n")) {
		t.Fatalf("json: trailing newline")
	}
	doc := readPairs(t, jsontext.NewDecoder(bytes.NewReader(body)))
	if len(doc) != 1 || doc[0].name != "columns" {
		t.Fatalf("json: top-level members %v, want columns", doc)
	}
	dec := jsontext.NewDecoder(bytes.NewReader(doc[0].value))
	readToken(t, dec, '[')
	var cols []wireColumn
	for dec.PeekKind() != ']' {
		ps := readPairs(t, dec)
		if len(ps) != 3 || ps[0].name != "name" || ps[1].name != "type" || ps[2].name != "data" {
			t.Fatalf("json: column members %v, want name, type, data", ps)
		}
		var c wireColumn
		unmarshal(t, ps[0].value, &c.name)
		unmarshal(t, ps[1].value, &c.kind)
		var data []jsontext.Value
		unmarshal(t, ps[2].value, &data)
		for _, v := range data {
			c.cells = append(c.cells, cellOf(t, v))
		}
		cols = append(cols, c)
	}
	readToken(t, dec, ']')
	return cols
}

// decodeNDJSON reads one object per line, every line's keys in the first
// line's order. No rows is no body, so no columns.
func decodeNDJSON(t failer, body []byte) []wireColumn {
	t.Helper()
	if len(body) == 0 {
		return nil
	}
	if body[len(body)-1] != '\n' {
		t.Fatalf("ndjson: last line unterminated")
	}
	var cols []wireColumn
	for row, line := range bytes.Split(bytes.TrimSuffix(body, []byte("\n")), []byte("\n")) {
		ps := readPairs(t, jsontext.NewDecoder(bytes.NewReader(line)))
		if row == 0 {
			for _, p := range ps {
				cols = append(cols, wireColumn{name: p.name})
			}
		}
		if len(ps) != len(cols) {
			t.Fatalf("ndjson row %d: %d members, want %d", row, len(ps), len(cols))
		}
		for i, p := range ps {
			if p.name != cols[i].name {
				t.Fatalf("ndjson row %d member %d: %q, want %q", row, i, p.name, cols[i].name)
			}
			cols[i].cells = append(cols[i].cells, cellOf(t, p.value))
		}
	}
	return cols
}

// decodeCSV reads the header and the records; the empty field is null.
// No columns is no body.
func decodeCSV(t failer, body []byte) []wireColumn {
	t.Helper()
	if len(body) == 0 {
		return nil
	}
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
	case qdbapi.TsColumnTimestamp:
		return "timestamp"
	default:
		return ""
	}
}

// parseTimestamp reads a rendered timestamp: RFC 3339, UTC, and exactly
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
		var want64 []int64
		for _, ts := range qdbapi.GetColumnDataTimestampUnsafe(want.Data) {
			want64 = append(want64, ts.UnixNano())
		}
		checkValues(t, want.Name, want.Valid, want64, parsed(t, want.Name, got.cells, parseTimestamp), same[int64])
	case qdbapi.TsColumnString, qdbapi.TsColumnSymbol:
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataStringUnsafe(want.Data), parsed(t, want.Name, got.cells, parseText), same[string])
	case qdbapi.TsColumnBlob:
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataBlobUnsafe(want.Data), parsed(t, want.Name, got.cells, base64.StdEncoding.DecodeString), bytes.Equal)
	default:
		t.Fatalf("%s: unexpected column type %v", want.Name, want.Type)
	}
}

// checkIndexCells compares the rendered $timestamp column with the index
// that was written: every slot valid, every value exact.
func checkIndexCells(t failer, want []time.Time, got wireColumn) {
	t.Helper()
	if got.name != "$timestamp" || (got.kind != "" && got.kind != "timestamp") {
		t.Fatalf("first column %q of type %q on the wire, want $timestamp", got.name, got.kind)
	}
	if len(got.cells) != len(want) {
		t.Fatalf("$timestamp: %d rows on the wire, %d written", len(got.cells), len(want))
	}
	for i, ts := range want {
		if !got.cells[i].valid {
			t.Fatalf("$timestamp row %d: null on the wire", i)
		}
		if nanos, err := parseTimestamp(got.cells[i].text); err != nil || nanos != ts.UnixNano() {
			t.Fatalf("$timestamp row %d: %q on the wire, %d written (%v)", i, got.cells[i].text, ts.UnixNano(), err)
		}
	}
}

// checkRendered compares a rendered body with the table: $timestamp
// first, then every column in order.
func checkRendered(t failer, tbl table.Table, cols []wireColumn) {
	t.Helper()
	if len(cols) != len(tbl.Columns)+1 {
		t.Fatalf("%d columns on the wire, %d written", len(cols), len(tbl.Columns)+1)
	}
	checkIndexCells(t, tbl.Index, cols[0])
	for i, col := range tbl.Columns {
		checkCells(t, col, cols[i+1])
	}
}

// TestFormatsAgree: the four formats of one batch decode to the table
// that was written, whatever the types, the nulls and the row count.
func TestFormatsAgree(t *testing.T) {
	c := newCluster(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, c, tbl)
		rec := run(rt, c, tbl.Select())
		defer rec.Release()

		// Arrow through the production batch size: the IPC reader answers
		// no columns for a stream without a batch.
		if _, cols, _ := decode(rt, encode(rt, Arrow{}, rec)); len(tbl.Index) > 0 {
			checkIndex(rt, tbl.Index, cols[0])
			for i, col := range tbl.Columns {
				checkColumn(rt, col, cols[i+1])
			}
			for _, col := range cols {
				col.Release()
			}
		} else if cols != nil {
			rt.Fatalf("arrow: %d columns on the wire for no rows", len(cols))
		}
		checkRendered(rt, tbl, decodeJSON(rt, encode(rt, JSON{}, rec)))
		checkRendered(rt, tbl, decodeCSV(rt, encode(rt, CSV{}, rec)))
		// NDJSON carries no header: no rows is no body.
		if cols := decodeNDJSON(rt, encode(rt, NDJSON{}, rec)); len(tbl.Index) > 0 {
			checkRendered(rt, tbl, cols)
		} else if cols != nil {
			rt.Fatalf("ndjson: %d columns on the wire for no rows", len(cols))
		}
	})
}

// TestRenderedNilBatch: a statement without a result set renders as no
// columns in JSON and as an empty body where a column is the header.
func TestRenderedNilBatch(t *testing.T) {
	for _, tc := range []struct {
		e    Encoder
		want string
	}{
		{JSON{}, `{"columns":[]}`},
		{NDJSON{}, ""},
		{CSV{}, ""},
	} {
		if got := string(encode(t, tc.e, nil)); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.e.ContentType(), got, tc.want)
		}
	}
}

// newBatch builds a five-row batch of the five wire types from the given
// arrays, released on cleanup.
func newBatch(t *testing.T, cols map[string]arrow.Array, order ...string) arrow.RecordBatch {
	t.Helper()
	fields := make([]arrow.Field, len(order))
	arrays := make([]arrow.Array, len(order))
	for i, name := range order {
		arrays[i] = cols[name]
		fields[i] = arrow.Field{Name: name, Type: arrays[i].DataType(), Nullable: true}
	}
	rec := array.NewRecordBatch(arrow.NewSchema(fields, nil), arrays, int64(arrays[0].Len()))
	for _, a := range arrays {
		a.Release()
	}
	t.Cleanup(rec.Release)
	return rec
}

// edgeBatch is what the fixture cannot draw: the int64 extremes, NaN and
// an infinity, a float above 1e21, the epoch and the nanosecond before
// it, the empty string, a string CSV must quote, a leading space, invalid
// UTF-8, the empty blob, and a null in every column.
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
	return newBatch(t, map[string]arrow.Array{
		"i": ib.NewArray(), "d": fb.NewArray(), "t": tb.NewArray(), "s": sb.NewArray(), "b": bb.NewArray(),
	}, "i", "d", "t", "s", "b")
}

// TestEdgeCells pins the bytes of the edge batch in every rendered
// format: NaN and the infinity are null; invalid UTF-8 is U+FFFD in
// JSON and the raw byte in CSV; the leading space and the comma, quote
// and newline are what encoding/csv quotes; the empty string is the
// empty field.
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

// TestUnsupportedType: a column outside the wire vocabulary is an encode
// error naming it, in every rendered format, before any byte is written.
func TestUnsupportedType(t *testing.T) {
	nb := array.NewInt32Builder(memory.DefaultAllocator)
	nb.AppendValues([]int32{1}, nil)
	rec := newBatch(t, map[string]arrow.Array{"n": nb.NewArray()}, "n")
	for _, e := range []Encoder{JSON{}, NDJSON{}, CSV{}} {
		var buf bytes.Buffer
		var uerr *UnsupportedTypeError
		err := e.Encode(t.Context(), &buf, rec)
		if !errors.As(err, &uerr) || uerr.Column != "n" || buf.Len() != 0 {
			t.Errorf("%s: %v with %d bytes written, want UnsupportedTypeError for n", e.ContentType(), err, buf.Len())
		}
	}
}
