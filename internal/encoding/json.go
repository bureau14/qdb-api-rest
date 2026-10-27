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
	"maps"
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

// jsonColumn is one column with its JSON encoding: its name, its type as
// the Arrow type's name, and the function that appends cell i as a JSON
// value, null included.
type jsonColumn struct {
	name string
	kind string
	cell func(dst []byte, i int) []byte
}

func appendNull(dst []byte) []byte {
	return append(dst, "null"...)
}

// appendQuoted appends s as a JSON string with the minimal RFC 8785
// escaping. The C API does not validate a utf8 column, so a string may
// hold invalid bytes. jsontext replaces them with U+FFFD, the error that
// reports them is dropped, and the body stays valid JSON.
func appendQuoted(dst []byte, s string) []byte {
	dst, _ = jsontext.AppendQuote(dst, s)
	return dst
}

// appendFloat appends a finite float of the given bit size as a JSON
// number, in the bytes encoding/json writes: the shortest round trip, in
// plain notation for exponents in [-6, 21). jsontext would write NaN and
// the infinities as strings. They are not JSON numbers, and NaN is the
// writer's own null for doubles, so the caller writes them as null.
func appendFloat(dst []byte, f float64, bits int) []byte {
	return jsontext.AppendFloat(dst, f, bits)
}

// appendTimestamp appends nanos since the epoch as a JSON string in
// timestampLayout.
func appendTimestamp(dst []byte, nanos int64) []byte {
	dst = append(dst, '"')
	dst = time.Unix(0, nanos).UTC().AppendFormat(dst, timestampLayout)
	return append(dst, '"')
}

// appendBase64 appends b as a JSON string in the standard alphabet with
// padding, as encoding/json encodes bytes, which every client library
// decodes without configuration.
func appendBase64(dst []byte, b []byte) []byte {
	dst = append(dst, '"')
	dst = base64.StdEncoding.AppendEncode(dst, b)
	return append(dst, '"')
}

// jsonSigned returns the appender of a signed integer column: a bare
// number, null for null.
func jsonSigned[T int8 | int16 | int32 | int64](a signed[T]) func(dst []byte, i int) []byte {
	return func(dst []byte, i int) []byte {
		if a.IsNull(i) {
			return appendNull(dst)
		}
		return strconv.AppendInt(dst, int64(a.Value(i)), 10)
	}
}

// jsonUnsigned returns the appender of an unsigned integer column: a bare
// number, null for null. A uint64 above 2^53 is a bare number too, and a
// client that parses numbers as doubles loses digits.
func jsonUnsigned[T uint8 | uint16 | uint32 | uint64](a unsigned[T]) func(dst []byte, i int) []byte {
	return func(dst []byte, i int) []byte {
		if a.IsNull(i) {
			return appendNull(dst)
		}
		return strconv.AppendUint(dst, uint64(a.Value(i)), 10)
	}
}

// jsonFloat returns the appender of a float column: null for null, NaN
// and the infinities, and otherwise the shortest text that round-trips
// at the column's own bit size. A float32 0.1 is written as 0.1. Widening
// it to float64 first was rejected, because the text would then carry
// digits the value never had (0.10000000149011612).
func jsonFloat[T float32 | float64](a floating[T]) func(dst []byte, i int) []byte {
	bits := floatBits[T]()
	return func(dst []byte, i int) []byte {
		v := float64(a.Value(i))
		if a.IsNull(i) || math.IsNaN(v) || math.IsInf(v, 0) {
			return appendNull(dst)
		}
		return appendFloat(dst, v, bits)
	}
}

// jsonCell returns the type word and the appender of a column. The type
// switch runs once per column, so appending a cell is one call. The word
// is the Arrow type's name: a symbol column arrives as utf8 and reports
// utf8, and a count arrives as int64. A type without a case is
// ErrUnsupportedType, which names the column.
func jsonCell(f arrow.Field, a arrow.Array) (jsonColumn, error) {
	c := jsonColumn{name: f.Name, kind: f.Type.Name()}
	switch a := a.(type) {
	case *array.Int8:
		c.cell = jsonSigned(a)
	case *array.Int16:
		c.cell = jsonSigned(a)
	case *array.Int32:
		c.cell = jsonSigned(a)
	case *array.Int64:
		c.cell = jsonSigned(a)
	case *array.Uint8:
		c.cell = jsonUnsigned(a)
	case *array.Uint16:
		c.cell = jsonUnsigned(a)
	case *array.Uint32:
		c.cell = jsonUnsigned(a)
	case *array.Uint64:
		c.cell = jsonUnsigned(a)
	case *array.Float32:
		c.cell = jsonFloat(a)
	case *array.Float64:
		c.cell = jsonFloat(a)
	case *array.Decimal128:
		// The exact text, scale's trailing zeros included, is written as a
		// bare number, so the wire loses no digit.
		scale := a.DataType().(*arrow.Decimal128Type).Scale
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return append(dst, a.Value(i).ToString(scale)...)
		}
	case *array.Boolean:
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return strconv.AppendBool(dst, a.Value(i))
		}
	case *array.Date32:
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			dst = append(dst, '"')
			dst = a.Value(i).ToTime().AppendFormat(dst, dateLayout)
			return append(dst, '"')
		}
	case *array.Time64:
		nanos := timeNanosReader(a)
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			dst = append(dst, '"')
			dst = appendTimeOfDay(dst, nanos(i))
			return append(dst, '"')
		}
	case *array.Timestamp:
		nanos := nanosReader(a)
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return appendTimestamp(dst, nanos(i))
		}
	case *array.String:
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return appendQuoted(dst, a.Value(i))
		}
	case *array.Binary:
		c.cell = func(dst []byte, i int) []byte {
			if a.IsNull(i) {
				return appendNull(dst)
			}
			return appendBase64(dst, a.Value(i))
		}
	// Arrow's null type, which DuckDB answers for a bare NULL, has no
	// values, so every cell is null.
	case *array.Null:
		c.cell = func(dst []byte, _ int) []byte {
			return appendNull(dst)
		}
	default:
		return jsonColumn{}, unsupportedType(f)
	}
	return c, nil
}

// jsonColumns returns the jsonColumn of every column of the batch.
func jsonColumns(rec arrow.RecordBatch) ([]jsonColumn, error) {
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
// the end of EncodeStream. That flush empties the internal buffer, and
// the HTTP flush belongs to the handler.

// NDJSON encodes record batches as one JSON object per row, keys in
// column order, one LF-terminated line per row. A batch with no rows
// encodes as an empty body.
type NDJSON struct{}

// ContentType implements Encoder.
func (NDJSON) ContentType() string { return NDJSONContentType }

// EncodeStream implements Encoder.
func (NDJSON) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	bw := bufio.NewWriter(w)
	for rec, err := range batches {
		if err != nil {
			return err
		}
		// A client that left is noticed at the next batch. A per-row stride
		// was rejected, because a batch has at most model.BatchRows rows and
		// the stride would notice it no sooner.
		if err := ctx.Err(); err != nil {
			return err
		}
		cols, err := jsonColumns(rec)
		if err != nil {
			return err
		}
		if err := writeNDJSON(bw, cols, rec.NumRows()); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// writeNDJSON writes the rows. Each key is escaped once and reused with
// its colon for every row.
func writeNDJSON(w *bufio.Writer, cols []jsonColumn, rows int64) error {
	keys := make([][]byte, len(cols))
	for i, c := range cols {
		keys[i] = append(appendQuoted(nil, c.name), ':')
	}
	for row := int64(0); row < rows; row++ {
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
// wrapper, because one batch is one result and the table a row came
// from is a column like any other. There is no trailing newline.
//
// A stream of batches encodes as a top-level array of such results, one
// per batch, so a native result larger than one batch is several
// results. The body is column-oriented within a batch and memory stays
// bounded across batches. A stream cut mid-way is invalid JSON, so a
// client cannot mistake a truncated read for a complete one.
type JSON struct{}

// ContentType implements Encoder.
func (JSON) ContentType() string { return JSONContentType }

// EncodeStream implements Encoder.
func (JSON) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	// The body is a top-level array with one columnar result per batch,
	// between "[" and "]", with a comma before every result but the first.
	// The array closes only after the last batch, so a stream cut mid-way
	// is invalid JSON and a client cannot take a truncated read for a
	// complete one.
	bw := bufio.NewWriter(w)
	if err := bw.WriteByte('['); err != nil {
		return err
	}
	first := true
	for rec, err := range batches {
		// An error element returns before the buffered writer is flushed, so a
		// failure on the first fetch leaves nothing on the wire and the
		// handler can still answer a status.
		if err != nil {
			return err
		}
		// A client that left is noticed at the next batch. A per-row stride
		// was rejected, because a batch has at most model.BatchRows rows and
		// the stride would notice it no sooner.
		if err := ctx.Err(); err != nil {
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
		if err := writeJSON(bw, cols, rec.NumRows()); err != nil {
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
func writeJSON(w *bufio.Writer, cols []jsonColumn, rows int64) error {
	if _, err := w.WriteString(`{"columns":[`); err != nil {
		return err
	}
	for i, c := range cols {
		if i > 0 {
			if err := w.WriteByte(','); err != nil {
				return err
			}
		}
		if err := writeJSONColumn(w, c, rows); err != nil {
			return err
		}
	}
	_, err := w.WriteString("]}")
	return err
}

// writeJSONColumn writes one column object: name, type, then the data
// array of every row.
func writeJSONColumn(w *bufio.Writer, c jsonColumn, rows int64) error {
	b := append(w.AvailableBuffer(), `{"name":`...)
	b = appendQuoted(b, c.name)
	b = append(b, `,"type":"`...)
	b = append(b, c.kind...)
	b = append(b, `","data":[`...)
	if _, err := w.Write(b); err != nil {
		return err
	}
	for row := int64(0); row < rows; row++ {
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

// jsonTable accumulates the rows of one table for either JSON decoder,
// NDJSON or columnar JSON, because a cell's text is the same in both. It
// holds the batch's schema, one builder per field with its text
// appender, and whether each field reads a JSON number or a JSON string.
type jsonTable struct {
	name      string
	schema    *arrow.Schema
	builders  []array.Builder
	appenders []func(string) error
	numeric   []bool
}

// newJSONTable types the body's columns by the table's schema:
// $timestamp first, then the named data columns in the table's order. A
// name the table lacks is ErrInvalidRows. A table after the first must
// agree with the first on every field's type, or ErrInvalidRows names
// both tables. The error of schemaOf passes through as is.
func newJSONTable(name string, names map[string]bool, schemaOf model.SchemaOf, first *jsonTable) (*jsonTable, error) {
	// The batch carries the reader's types, so its fields are picked from
	// the reader's schema. The steps are those of newCSVTable:
	//
	//  1. look the table up through schemaOf, and pass its error as is;
	//  2. pick $timestamp and the named data columns from the schema, in
	//     the schema's order. A name the table lacks is the body's fault;
	//  3. check that a table after the first has the same type in every
	//     field, by name. One body is one column list, and the Arrow
	//     writer checks one table at a time, so the decoder checks it;
	//  4. make one builder and one text appender per field, and note
	//     whether the field reads a JSON number or a JSON string.
	//
	// The fields follow the table's order, because a JSON object's members
	// carry no order of their own. The columnar decoder shares this
	// function, so both decoders build a batch of one shape.

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
	t := &jsonTable{name: name, schema: arrow.NewSchema(fields, nil)}
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

// jsonText returns the cell text of a JSON value for the text appender:
// a number's literal as it stands, or a string's content unquoted. A
// value of the other kind is an error.
func jsonText(v jsontext.Value, numeric bool) (string, error) {
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

// appendCell appends a JSON value to field i. An absent (nil) or null
// value appends null, except in field 0, $timestamp, which cannot be
// null. The error names the column.
func (t *jsonTable) appendCell(i int, v jsontext.Value) error {
	// An absent member and a null member read alike, because the encoder
	// writes null as a member and a sparse NDJSON body leaves it out
	if v == nil || v.Kind() == 'n' {
		// Field 0 is $timestamp, which cannot be null, because the writer
		// would refuse the row at the push
		if i == 0 {
			return errors.New("$timestamp is null or absent")
		}
		t.builders[i].AppendNull()
		return nil
	}
	// The kind check comes first, so a string in a number column is named
	// as such rather than as a parse error on its quotes
	text, err := jsonText(v, t.numeric[i])
	if err == nil {
		err = t.appenders[i](text)
	}
	if err != nil {
		return fmt.Errorf("column %s: %w", t.schema.Field(i).Name, err)
	}
	return nil
}

// appendRow appends one object, as the map of its raw members, to the
// table's builders. A column the object lacks or names null is null, and a
// null or absent $timestamp is an error, because the index cannot be
// null. A member outside the column list is an error naming it.
func (t *jsonTable) appendRow(row map[string]jsontext.Value) error {
	// The row is checked whole and then filled column by column:
	//
	//  1. every member must be a column of the list, $table included, so
	//     a typo is a fault and not a silent drop;
	//  2. walk the column list and append each member through appendCell,
	//     because a number's literal and a string's content are the CSV
	//     cell's text.

	// 1. no member outside the list
	for name := range row {
		if name != "$table" && !t.schema.HasField(name) {
			return fmt.Errorf("column %s is not in the column list", name)
		}
	}

	// 2. the columns, in the batch's order, so a column the object does
	// not name is reached as well
	for i, f := range t.schema.Fields() {
		if err := t.appendCell(i, row[f.Name]); err != nil {
			return err
		}
	}
	return nil
}

// batch returns the rows as one record batch and empties the builders.
func (t *jsonTable) batch() model.TableBatch {
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

func (t *jsonTable) release() {
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

// jsonTableName returns the table a $table value names, which must be a
// string and not empty.
func jsonTableName(v jsontext.Value) (string, error) {
	name, err := jsonText(v, false)
	if err != nil {
		return "", fmt.Errorf("$table: %w", err)
	}
	if name == "" {
		return "", errors.New("empty $table")
	}
	return name, nil
}

// ndjsonTableOf returns the table the row's $table member names, which
// must be present, a string, and not empty.
func ndjsonTableOf(row map[string]jsontext.Value) (string, error) {
	v, ok := row["$table"]
	if !ok {
		return "", errors.New("no $table")
	}
	return jsonTableName(v)
}

// Decode implements Decoder. The body is one JSON object per row, in this
// encoder's dialect: the first object's keys fix the column list, $table
// and $timestamp among them, and a later object may omit a column.
func (NDJSON) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// The standard library reads each row into one map of raw values, so
	// the library parses and the decoder only routes. The steps are those
	// of CSV.Decode, over objects:
	//
	//  1. the first object fixes the column list: its keys, $table and
	//     $timestamp among them;
	//  2. each object goes to the builders of its table, which its $table
	//     member names. A table seen for the first time is typed through
	//     schemaOf and must agree with the first table's types;
	//  3. at the end every table becomes one batch, in first-seen order.
	dec := jsontext.NewDecoder(r)
	tables := map[string]*jsonTable{}
	var order []*jsonTable
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
			var first *jsonTable
			if len(order) > 0 {
				first = order[0]
			}
			if t, err = newJSONTable(name, names, schemaOf, first); err != nil {
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

// jsonResultColumn is one column object of a columnar result as the body
// carries it. The type member is read and not checked, because the
// reader's schema types the batch, as it does for CSV and NDJSON. Each
// cell is kept raw, so it reads as an NDJSON cell does.
type jsonResultColumn struct {
	Name string           `json:"name"`
	Type string           `json:"type"`
	Data []jsontext.Value `json:"data"`
}

// jsonResult is one columnar result as the body carries it.
type jsonResult struct {
	Columns []jsonResultColumn `json:"columns"`
}

// readJSONResult reads the decoder's next value as result n, counted
// from 1. A value the standard library cannot read as a result is
// ErrInvalidRows, which keeps the reader's cause in the chain.
func readJSONResult(dec *jsontext.Decoder, n int) (jsonResult, error) {
	// An unknown member is refused, because the default ignores it, and a
	// misspelled "columns" or "data" would then ingest nothing and
	// answer 200
	var res jsonResult
	if err := json.UnmarshalDecode(dec, &res, json.RejectUnknownMembers(true)); err != nil {
		// Both error chains stay reachable. The sentinel decides the status,
		// and the reader's cause reveals a body-size cap the HTTP layer set.
		return jsonResult{}, fmt.Errorf("%w: result %d: %w", ErrInvalidRows, n, err)
	}
	return res, nil
}

// jsonResults yields the results of a body: one bare result, or each
// result of the array every route answers. An empty array yields
// nothing. A body that is neither, a result the standard library cannot
// read, a member a result does not declare, and a value after the body
// are ErrInvalidRows, which keeps the reader's cause in the chain. The
// error of a result names it, counted from 1. The sequence ends after
// its first error.
func jsonResults(dec *jsontext.Decoder) iter.Seq2[jsonResult, error] {
	// The decoder reads one result at a time, whole, so it holds the raw
	// cells of one result and never those of the whole body:
	//
	//  1. peek the first token, because it decides the path. An object is
	//     one result, an array is a stream of results, and anything else
	//     is the body's fault;
	//  2. read the one result through readJSONResult;
	//  3. in the array, read '[', then results until the next token is
	//     ']', then the ']';
	//  4. after the body, ReadToken must answer io.EOF, because jsontext
	//     reads several top-level values without complaint and a second
	//     value would otherwise be dropped silently.
	return func(yield func(jsonResult, error) bool) {
		// 1. the shape of the body
		switch dec.PeekKind() {
		case '{':
			// 2. the one result
			res, err := readJSONResult(dec, 1)
			if !yield(res, err) || err != nil {
				return
			}
		case '[':
			// 3. the array of results
			if _, err := dec.ReadToken(); err != nil {
				yield(jsonResult{}, fmt.Errorf("%w: %w", ErrInvalidRows, err))
				return
			}
			for n := 1; dec.PeekKind() != ']'; n++ {
				res, err := readJSONResult(dec, n)
				if !yield(res, err) || err != nil {
					return
				}
			}
			if _, err := dec.ReadToken(); err != nil {
				yield(jsonResult{}, fmt.Errorf("%w: %w", ErrInvalidRows, err))
				return
			}
		default:
			// The token is read so the error names what the body holds, or
			// keeps the reader's cause when the body could not be read
			tok, err := dec.ReadToken()
			if errors.Is(err, io.EOF) {
				yield(jsonResult{}, fmt.Errorf("%w: the body is empty", ErrInvalidRows))
				return
			}
			if err != nil {
				yield(jsonResult{}, fmt.Errorf("%w: %w", ErrInvalidRows, err))
				return
			}
			yield(jsonResult{}, fmt.Errorf("%w: the body is a JSON %s, not a result", ErrInvalidRows, tok.Kind()))
			return
		}

		// 4. nothing after the body
		if _, err := dec.ReadToken(); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("a value follows the body")
			}
			yield(jsonResult{}, fmt.Errorf("%w: %w", ErrInvalidRows, err))
		}
	}
}

// readJSONHeader returns the data columns of a result by name and its
// row count. $table and $timestamp are required, a name appears once,
// and every column has the same number of cells, or the error names the
// column and the caller makes it ErrInvalidRows.
func readJSONHeader(res jsonResult) (map[string]bool, int, error) {
	names := map[string]bool{}
	rows := 0
	for i, c := range res.Columns {
		// jsontext refuses a duplicate member inside one object, and a
		// duplicate column is a repeated name across the objects of the
		// array, which it does not see
		if names[c.Name] {
			return nil, 0, fmt.Errorf("column %s appears twice", c.Name)
		}
		names[c.Name] = true
		if i == 0 {
			rows = len(c.Data)
		} else if len(c.Data) != rows {
			return nil, 0, fmt.Errorf("column %s has %d cells where column %s has %d", c.Name, len(c.Data), res.Columns[0].Name, rows)
		}
	}
	switch {
	// {"columns":[]}, a result of no columns, names no $table either, and
	// is refused like a CSV header without $table
	case !names["$table"]:
		return nil, 0, errors.New("names no $table")
	case !names["$timestamp"]:
		return nil, 0, errors.New("names no $timestamp")
	}
	delete(names, "$table")
	delete(names, "$timestamp")
	return names, rows, nil
}

// Decode implements Decoder. The body is one bare columnar result, or
// the array of results every route answers. The first result fixes the
// column list, $table and $timestamp among them, in any order, and every
// later result names the same columns. The type member of a column is
// not checked.
func (JSON) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// A result is columnar, and $table can be any of its columns, so no
	// cell can be routed before the result's $table column has been read.
	// The decoder therefore reads one result whole and then routes it row
	// by row. A decoder that appends each cell as it reads the tokens was
	// rejected, because the $table column may come last, and it would
	// parse JSON by hand where the standard library parses for NDJSON.
	//
	//  1. each result of the body comes from jsonResults;
	//  2. every result passes readJSONHeader, and the first result's
	//     names fix the column list, because one body is one column list;
	//  3. the result's columns are indexed by name once, so each row
	//     looks its cells up without a scan;
	//  4. each row goes to the builders of its table, which its $table
	//     cell names. A table seen for the first time is typed through
	//     schemaOf and must agree with the first table's types;
	//  5. the row's cell of every field of the table is appended, in the
	//     batch's order;
	//  6. at the end every table becomes one batch, in first-seen order.
	tables := map[string]*jsonTable{}
	var order []*jsonTable
	var list map[string]bool
	release := func() {
		for _, t := range order {
			t.release()
		}
	}

	row := int64(0)
	n := 0
	// 1. the results
	for res, err := range jsonResults(jsontext.NewDecoder(r)) {
		n++
		if err != nil {
			release()
			return nil, err
		}

		// 2. the column list, fixed by the first result
		names, rows, err := readJSONHeader(res)
		if err != nil {
			release()
			return nil, fmt.Errorf("%w: result %d: %w", ErrInvalidRows, n, err)
		}
		if list == nil {
			list = names
		} else if !maps.Equal(list, names) {
			release()
			return nil, fmt.Errorf("%w: result %d names other columns than result 1", ErrInvalidRows, n)
		}

		// 3. the columns by name
		data := make(map[string][]jsontext.Value, len(res.Columns))
		for _, c := range res.Columns {
			data[c.Name] = c.Data
		}

		for i := range rows {
			// The ctx is checked once per model.BatchRows rows, and the rows are
			// counted from 1 across the body, as NDJSON counts them
			row++
			if err := checkChunk(ctx, row); err != nil {
				release()
				return nil, err
			}

			// 4. the row to its table's builders. A null or empty $table is
			// the body's fault
			name, err := jsonTableName(data["$table"][i])
			if err != nil {
				release()
				return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, row, err)
			}
			t, ok := tables[name]
			if !ok {
				// The first table of the body is the type reference every
				// later table is checked against, because one body is one
				// column list. The lookup's own error passes through
				// unwrapped, so the handler can classify an unknown table as
				// 404
				var first *jsonTable
				if len(order) > 0 {
					first = order[0]
				}
				if t, err = newJSONTable(name, list, schemaOf, first); err != nil {
					release()
					return nil, err
				}
				tables[t.name] = t
				order = append(order, t)
			}

			// 5. every field's cell. Every field has a column, because
			// newJSONTable picked the fields from the column list
			for j, f := range t.schema.Fields() {
				if err := t.appendCell(j, data[f.Name][i]); err != nil {
					release()
					return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, row, err)
				}
			}
		}
	}

	// 6. one batch per table
	out := make([]model.TableBatch, len(order))
	for i, t := range order {
		out[i] = t.batch()
		t.release()
	}
	return out, nil
}
