package encoding

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/bureau14/qdb-api-rest/internal/model"
)

const (
	// JSONContentType is the media type of the columnar JSON result.
	JSONContentType = "application/json"
	// NDJSONContentType is the media type of newline-delimited JSON, one
	// object per row.
	NDJSONContentType = "application/x-ndjson"
)

// jsonColumn is one column with its JSON encoding: its name, its type in
// QuasarDB's words (int64, double, string, blob, timestamp), and the
// function that appends cell i as a JSON value, null included.
type jsonColumn struct {
	name string
	kind string
	cell func(dst []byte, i int) []byte
}

func appendNull(dst []byte) []byte {
	return append(dst, "null"...)
}

// appendQuoted appends s as a JSON string with the minimal RFC 8785
// escaping. The C API does not validate a utf8 column: invalid bytes
// become U+FFFD, the error that reports them is dropped, and the body
// stays valid JSON.
func appendQuoted(dst []byte, s string) []byte {
	dst, _ = jsontext.AppendQuote(dst, s)
	return dst
}

// appendFloat appends a finite f as a JSON number, the bytes
// encoding/json writes: shortest round trip, plain notation for
// exponents in [-6, 21). The appender would write NaN and the infinities
// as strings; they are not JSON, and NaN is the writer's own null for
// doubles, so the caller writes them as null.
func appendFloat(dst []byte, f float64) []byte {
	return jsontext.AppendFloat(dst, f, 64)
}

// appendTimestamp appends nanos since the epoch as a JSON string in
// timestampLayout.
func appendTimestamp(dst []byte, nanos int64) []byte {
	dst = append(dst, '"')
	dst = time.Unix(0, nanos).UTC().AppendFormat(dst, timestampLayout)
	return append(dst, '"')
}

// appendBase64 appends b as a JSON string in the standard alphabet with
// padding, as encoding/json encodes bytes: what every client library
// decodes without configuration.
func appendBase64(dst []byte, b []byte) []byte {
	dst = append(dst, '"')
	dst = base64.StdEncoding.AppendEncode(dst, b)
	return append(dst, '"')
}

// jsonCell returns the type word and the appender of column a. The type
// switch runs once per column, so appending a cell is one call. The type
// words on the wire are the binding's types: a symbol column arrives as
// utf8 and is reported as a string, and a count column arrives as int64
// and is reported as int64.
func jsonCell(f arrow.Field, a arrow.Array) (jsonColumn, error) {
	c := jsonColumn{name: f.Name}
	switch a := a.(type) {
	case *array.Int64:
		c.kind = "int64"
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return strconv.AppendInt(dst, a.Value(i), 10)
		}
	case *array.Float64:
		c.kind = "double"
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) || math.IsNaN(a.Value(i)) || math.IsInf(a.Value(i), 0) {
				return appendNull(dst)
			}
			return appendFloat(dst, a.Value(i))
		}
	case *array.Timestamp:
		c.kind = "timestamp"
		nanos := nanosReader(a)
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return appendTimestamp(dst, nanos(i))
		}
	case *array.String:
		c.kind = "string"
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return appendQuoted(dst, a.Value(i))
		}
	case *array.Binary:
		c.kind = "blob"
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return appendBase64(dst, a.Value(i))
		}
	default:
		return jsonColumn{}, unsupportedType(f)
	}
	return c, nil
}

// jsonColumns returns the jsonColumn of every column of rec. A nil rec
// has none.
func jsonColumns(rec arrow.RecordBatch) ([]jsonColumn, error) {
	if rec == nil {
		return nil, nil
	}
	cols := make([]jsonColumn, rec.NumCols())
	for i, f := range rec.Schema().Fields() {
		c, err := jsonCell(f, rec.Column(i))
		if err != nil {
			return nil, err
		}
		cols[i] = c
	}
	return cols, nil
}

// The two JSON encoders append every cell straight into the buffered
// writer's spare capacity (AvailableBuffer) and hand the bytes back, so
// the hot loop allocates nothing. The buffered writer is flushed once at
// the end of Encode. That flush is internal buffering; the HTTP flush
// belongs to the handler.

// NDJSON encodes record batches as one JSON object per row, keys in
// column order, one LF-terminated line per row. A batch with no rows,
// including a nil batch, encodes as an empty body.
type NDJSON struct{}

// ContentType implements Encoder.
func (NDJSON) ContentType() string { return NDJSONContentType }

// Encode implements Encoder.
func (NDJSON) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	cols, err := jsonColumns(rec)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if err := writeNDJSON(ctx, bw, cols, numRows(rec)); err != nil {
		return err
	}
	return bw.Flush()
}

// EncodeStream implements Encoder.
func (NDJSON) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	bw := bufio.NewWriter(w)
	for rec, err := range batches {
		if err != nil {
			return err
		}
		cols, err := jsonColumns(rec)
		if err != nil {
			return err
		}
		if err := writeNDJSON(ctx, bw, cols, rec.NumRows()); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// writeNDJSON writes the rows. Each key is escaped once and reused with
// its colon for every row.
func writeNDJSON(ctx context.Context, w *bufio.Writer, cols []jsonColumn, rows int64) error {
	keys := make([][]byte, len(cols))
	for i, c := range cols {
		keys[i] = append(appendQuoted(nil, c.name), ':')
	}
	for row := int64(0); row < rows; row++ {
		if err := checkChunk(ctx, row); err != nil {
			return err
		}
		b := append(w.AvailableBuffer(), '{')
		for i, c := range cols {
			if i > 0 {
				b = append(b, ',')
			}
			b = append(b, keys[i]...)
			b = c.cell(b, int(row))
		}
		b = append(b, "}\n"...)
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	return nil
}

// JSON encodes a record batch as one columnar result:
//
//	{"columns":[{"name":"$timestamp","type":"timestamp","data":[...]},...]}
//
// There is one object per column, with the keys in that order so a
// streaming reader knows the type before the data. There is no tables
// wrapper, because one query is one result and the table a row came
// from is a column like any other. A nil batch encodes as
// {"columns":[]}. There is no trailing newline.
//
// A stream of batches encodes as a top-level array of such results, one
// per batch. The body is column-oriented within a batch and memory stays
// bounded across batches. A stream cut mid-way is invalid JSON, so a
// client cannot mistake a truncated read for a complete one.
type JSON struct{}

// ContentType implements Encoder.
func (JSON) ContentType() string { return JSONContentType }

// Encode implements Encoder.
func (JSON) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	cols, err := jsonColumns(rec)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	if err := writeJSON(ctx, bw, cols, numRows(rec)); err != nil {
		return err
	}
	return bw.Flush()
}

// EncodeStream implements Encoder.
func (JSON) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	// The body is a top-level array with one columnar result per batch.
	// Each batch is encoded exactly as Encode encodes it, between "[" and
	// "]", with a comma before every result but the first. The array
	// closes only after the last batch, so a stream cut mid-way is invalid
	// JSON and a client cannot take a truncated read for a complete one. A
	// sequence without any batch encodes as "[]".
	bw := bufio.NewWriter(w)
	if err := bw.WriteByte('['); err != nil {
		return err
	}
	first := true
	for rec, err := range batches {
		// An error step returns before the buffered writer is flushed, so a
		// failure on the first fetch leaves nothing on the wire and the
		// handler can still answer a status.
		if err != nil {
			return err
		}
		cols, err := jsonColumns(rec)
		if err != nil {
			return err
		}
		if !first {
			if err := bw.WriteByte(','); err != nil {
				return err
			}
		}
		first = false
		if err := writeJSON(ctx, bw, cols, rec.NumRows()); err != nil {
			return err
		}
	}
	if err := bw.WriteByte(']'); err != nil {
		return err
	}
	return bw.Flush()
}

// writeJSON writes the columns. Each array is walked once, so the body
// streams column-major over the batch.
func writeJSON(ctx context.Context, w *bufio.Writer, cols []jsonColumn, rows int64) error {
	if _, err := w.WriteString(`{"columns":[`); err != nil {
		return err
	}
	for i, c := range cols {
		if i > 0 {
			if err := w.WriteByte(','); err != nil {
				return err
			}
		}
		if err := writeJSONColumn(ctx, w, c, rows); err != nil {
			return err
		}
	}
	_, err := w.WriteString("]}")
	return err
}

// writeJSONColumn writes one column object: name, type, then the data
// array of every row.
func writeJSONColumn(ctx context.Context, w *bufio.Writer, c jsonColumn, rows int64) error {
	b := append(w.AvailableBuffer(), `{"name":`...)
	b = appendQuoted(b, c.name)
	b = append(b, `,"type":"`...)
	b = append(b, c.kind...)
	b = append(b, `","data":[`...)
	if _, err := w.Write(b); err != nil {
		return err
	}
	for row := int64(0); row < rows; row++ {
		if err := checkChunk(ctx, row); err != nil {
			return err
		}
		b := w.AvailableBuffer()
		if row > 0 {
			b = append(b, ',')
		}
		if _, err := w.Write(c.cell(b, int(row))); err != nil {
			return err
		}
	}
	_, err := w.WriteString("]}")
	return err
}

// ndjsonTable accumulates the rows of one table. It holds the batch's
// schema, one builder per field with its text appender, and whether
// each field reads a JSON number or a JSON string.
type ndjsonTable struct {
	name      string
	schema    *arrow.Schema
	builders  []array.Builder
	appenders []func(string) error
	numeric   []bool
}

// newNDJSONTable types the body's columns by the table's schema:
// $timestamp first, then the data columns the first object names, in
// the table's order, because a JSON object's members carry no order of
// their own. A name the table lacks is ErrInvalidRows. A table after the
// first must agree with the first on every field's type, or
// ErrInvalidRows names both tables. The error of schemaOf passes
// through as is.
func newNDJSONTable(name string, names map[string]bool, schemaOf model.SchemaOf, first *ndjsonTable) (*ndjsonTable, error) {
	// The batch carries the reader's types, so its fields are picked from
	// the reader's schema rather than declared here, the four steps of
	// newCSVTable:
	//
	//  1. look the table up through schemaOf, and pass its error as is;
	//  2. pick $timestamp and the named data columns from the schema, in
	//     the schema's order; a name the table lacks is the body's fault;
	//  3. check that a table after the first has the same type in every
	//     field, by name: one body is one column list, and the Arrow
	//     writer checks one table at a time, so this decoder has to check
	//     it;
	//  4. make one builder and one text appender per field, and note the
	//     JSON kind the field reads.

	// 1. the table's schema
	schema, err := schemaOf(name)
	if err != nil {
		return nil, err
	}

	// 2. the fields, in the schema's order
	for n := range names {
		if schema.FieldIndices(n) == nil {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrInvalidRows, name, n)
		}
	}
	var fields []arrow.Field
	for _, f := range schema.Fields() {
		if f.Name == "$timestamp" || names[f.Name] {
			fields = append(fields, f)
		}
	}

	// 3. one column list per body
	if first != nil {
		for _, f := range fields {
			if want := first.schema.Field(first.schema.FieldIndices(f.Name)[0]); !arrow.TypeEqual(f.Type, want.Type) {
				return nil, fmt.Errorf("%w: tables %s and %s differ in the type of column %s", ErrInvalidRows, first.name, name, f.Name)
			}
		}
	}

	// 4. the builders and the kinds
	t := &ndjsonTable{name: name, schema: arrow.NewSchema(fields, nil)}
	for _, f := range fields {
		b := array.NewBuilder(memory.DefaultAllocator, f.Type)
		app, err := textAppender(f, b)
		if err != nil {
			b.Release()
			t.release()
			return nil, err
		}
		t.builders = append(t.builders, b)
		t.appenders = append(t.appenders, app)
		t.numeric = append(t.numeric, f.Type.ID() == arrow.INT64 || f.Type.ID() == arrow.FLOAT64)
	}
	return t, nil
}

// ndjsonText returns the cell text of a JSON value for the text
// appender: a number's literal as it stands, or a string's content
// unquoted. A value of the other kind is an error.
func ndjsonText(v jsontext.Value, numeric bool) (string, error) {
	switch {
	case numeric && v.Kind() == '0':
		return string(v), nil
	case !numeric && v.Kind() == '"':
		var s string
		err := json.Unmarshal(v, &s)
		return s, err
	case numeric:
		return "", fmt.Errorf("a JSON %s where a number is expected", v.Kind())
	}
	return "", fmt.Errorf("a JSON %s where a string is expected", v.Kind())
}

// appendRow appends one object, as the map of its raw members, to the
// builders of t. A column the object lacks or names null is null, and a
// null or absent $timestamp is an error, because the index cannot be
// null. A member outside the column list is an error naming it.
func (t *ndjsonTable) appendRow(row map[string]jsontext.Value) error {
	// The row is checked whole and then filled column by column:
	//
	//  1. every member must be a column of the list, $table included, so
	//     a typo is a fault and not a silent drop;
	//  2. walk the column list: an absent or null member appends null,
	//     except in the index; any other value's text goes through the
	//     text appender, because a number's literal and a string's
	//     content are the CSV cell's text.

	// 1. no member outside the list
	for name := range row {
		if name != "$table" && !t.schema.HasField(name) {
			return fmt.Errorf("column %s is not in the column list", name)
		}
	}

	// 2. the columns, in the batch's order, so a column the object does
	// not name is reached as well
	for i, f := range t.schema.Fields() {
		// An absent member and a null member read alike, because the
		// encoder writes null as a member and a sparse body leaves it out
		v, ok := row[f.Name]
		if !ok || v.Kind() == 'n' {
			// Field 0 is $timestamp, which cannot be null, because the
			// writer would refuse the row at the push
			if i == 0 {
				return errors.New("$timestamp is null or absent")
			}
			t.builders[i].AppendNull()
			continue
		}
		// The kind check comes first, so a string in a number column is
		// named as such rather than as a parse error on its quotes
		text, err := ndjsonText(v, t.numeric[i])
		if err == nil {
			err = t.appenders[i](text)
		}
		if err != nil {
			return fmt.Errorf("column %s: %w", f.Name, err)
		}
	}
	return nil
}

// batch returns the rows as one record batch and empties the builders.
func (t *ndjsonTable) batch() model.TableBatch {
	cols := make([]arrow.Array, len(t.builders))
	for i, b := range t.builders {
		cols[i] = b.NewArray()
	}
	rec := array.NewRecordBatch(t.schema, cols, int64(cols[0].Len()))
	for _, c := range cols {
		c.Release()
	}
	return model.TableBatch{Table: t.name, Batch: rec}
}

func (t *ndjsonTable) release() {
	for _, b := range t.builders {
		b.Release()
	}
}

// readNDJSONHeader fixes the column list from the first object's member
// names. $table and $timestamp are required, and every other name is a
// data column.
func readNDJSONHeader(row map[string]jsontext.Value) (map[string]bool, error) {
	names := map[string]bool{}
	for name := range row {
		switch name {
		case "$table", "$timestamp":
		default:
			names[name] = true
		}
	}
	switch {
	case row["$table"] == nil:
		return nil, fmt.Errorf("%w: header names no $table", ErrInvalidRows)
	case row["$timestamp"] == nil:
		return nil, fmt.Errorf("%w: header names no $timestamp", ErrInvalidRows)
	}
	return names, nil
}

// ndjsonTableOf returns the table the row's $table member names, which
// must be present, a string, and not empty.
func ndjsonTableOf(row map[string]jsontext.Value) (string, error) {
	v, ok := row["$table"]
	if !ok {
		return "", errors.New("no $table")
	}
	name, err := ndjsonText(v, false)
	if err != nil {
		return "", fmt.Errorf("$table: %w", err)
	}
	if name == "" {
		return "", errors.New("empty $table")
	}
	return name, nil
}

// Decode implements Decoder. The body is one JSON object per row, in this
// encoder's dialect: the first object's keys fix the column list, $table
// and $timestamp among them, and a later object may omit a column.
func (NDJSON) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// Each row is read as one map of raw values through the standard
	// library, so the library parses and this decoder only routes, the
	// three steps of CSV.Decode over objects:
	//
	//  1. the first object fixes the column list: its keys, $table and
	//     $timestamp among them;
	//  2. each object goes to the builders of its table, which its $table
	//     member names; a table seen for the first time is typed through
	//     schemaOf and must agree with the first table's types;
	//  3. at the end every table becomes one batch, in first-seen order.
	dec := jsontext.NewDecoder(r)
	tables := map[string]*ndjsonTable{}
	var order []*ndjsonTable
	var names map[string]bool
	release := func() {
		for _, t := range order {
			t.release()
		}
	}

	for n := int64(1); ; n++ {
		if err := checkChunk(ctx, n); err != nil {
			release()
			return nil, err
		}
		var row map[string]jsontext.Value
		err := json.UnmarshalDecode(dec, &row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			release()
			// Both error chains stay reachable. The sentinel decides the status,
			// and the reader's cause reveals a body-size cap the HTTP layer set.
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, n, err)
		}

		// 1. the header, from the first object
		if n == 1 {
			if names, err = readNDJSONHeader(row); err != nil {
				return nil, err
			}
		}

		// 2. the object to its table's builders. A row that names no
		// table, or names it with anything but a string, is the body's
		// fault
		name, err := ndjsonTableOf(row)
		if err != nil {
			release()
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, n, err)
		}
		t, ok := tables[name]
		if !ok {
			// The first table of the body is the type reference every
			// later table is checked against, because one body is one
			// column list. The lookup's own error passes through unwrapped,
			// so the handler can classify an unknown table as 404
			var first *ndjsonTable
			if len(order) > 0 {
				first = order[0]
			}
			if t, err = newNDJSONTable(name, names, schemaOf, first); err != nil {
				release()
				return nil, err
			}
			tables[t.name] = t
			order = append(order, t)
		}
		if err := t.appendRow(row); err != nil {
			release()
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, n, err)
		}
	}

	// 3. one batch per table
	out := make([]model.TableBatch, len(order))
	for i, t := range order {
		out[i] = t.batch()
		t.release()
	}
	return out, nil
}
