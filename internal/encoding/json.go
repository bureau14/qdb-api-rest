package encoding

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/jsontext"
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

// ndjsonAppender returns the function that appends one JSON token to the
// builder b, the inverse of jsonCell. A number token goes into an int64
// or a float64, and a string token into a string, into a blob through
// standard base64, or into a timestamp through RFC 3339. A token of any
// other kind, and a number or text the column cannot hold, is an error
// the caller wraps with the row and the column.
func ndjsonAppender(f arrow.Field, b array.Builder) (func(jsontext.Token) error, error) {
	switch b := b.(type) {
	case *array.Int64Builder:
		return func(tok jsontext.Token) error {
			if tok.Kind() != '0' {
				return kindError(tok, "number")
			}
			// Token.Int refuses a fraction and an exponent with a syntax
			// error, so 1.0 is a fault in an int64 column and not a cast.
			v, err := tok.Int()
			b.Append(v)
			return err
		}, nil
	case *array.Float64Builder:
		return func(tok jsontext.Token) error {
			if tok.Kind() != '0' {
				return kindError(tok, "number")
			}
			v, err := tok.Float()
			b.Append(v)
			return err
		}, nil
	case *array.TimestampBuilder:
		return func(tok jsontext.Token) error {
			if tok.Kind() != '"' {
				return kindError(tok, "string")
			}
			t, err := time.Parse(time.RFC3339Nano, tok.String())
			b.Append(arrow.Timestamp(t.UnixNano()))
			return err
		}, nil
	case *array.StringBuilder:
		return func(tok jsontext.Token) error {
			if tok.Kind() != '"' {
				return kindError(tok, "string")
			}
			b.Append(tok.String())
			return nil
		}, nil
	case *array.BinaryBuilder:
		return func(tok jsontext.Token) error {
			if tok.Kind() != '"' {
				return kindError(tok, "string")
			}
			v, err := base64.StdEncoding.DecodeString(tok.String())
			b.Append(v)
			return err
		}, nil
	}
	return nil, unsupportedType(f)
}

// kindError is the error for a token of a kind the column cannot hold.
func kindError(tok jsontext.Token, want string) error {
	return fmt.Errorf("a JSON %s where a %s is expected", tok.Kind(), want)
}

// ndjsonTable accumulates the rows of one table. It holds the batch's
// schema, one builder per field with its appender, the field of each
// column name, and the rows appended so far.
type ndjsonTable struct {
	name      string
	schema    *arrow.Schema
	builders  []array.Builder
	appenders []func(jsontext.Token) error
	index     map[string]int // the schema field of each column name
	rows      int
}

// newNDJSONTable types the body's columns by the table's schema:
// $timestamp first, then the first object's names in their order. Each
// name must be a field of the schema, or the result is ErrInvalidRows. A
// table after the first must agree with the first on every field's type,
// or ErrInvalidRows names both tables. The error of schemaOf passes
// through as is.
func newNDJSONTable(name string, names []string, schemaOf model.SchemaOf, first *ndjsonTable) (*ndjsonTable, error) {
	// The batch carries the reader's types, so its fields are picked from
	// the reader's schema rather than declared here, the four steps of
	// newCSVTable:
	//
	//  1. look the table up through schemaOf, and pass its error as is;
	//  2. pick $timestamp and the first object's names from the schema by
	//     name; a name the table lacks is the body's fault;
	//  3. check that a table after the first has the same type in every
	//     field: one body is one column list, and the Arrow writer checks
	//     one table at a time, so this decoder has to check it;
	//  4. make one builder and one appender per field, and index the
	//     fields by name, because an object names its members.

	// 1. the table's schema
	schema, err := schemaOf(name)
	if err != nil {
		return nil, err
	}

	// 2. the fields, by name
	names = append([]string{"$timestamp"}, names...)
	fields := make([]arrow.Field, len(names))
	for i, n := range names {
		idx := schema.FieldIndices(n)
		if idx == nil {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrInvalidRows, name, n)
		}
		fields[i] = schema.Field(idx[0])
	}

	// 3. one column list per body
	if first != nil {
		for i, f := range fields {
			if want := first.schema.Field(i); !arrow.TypeEqual(f.Type, want.Type) {
				return nil, fmt.Errorf("%w: tables %s and %s differ in the type of column %s", ErrInvalidRows, first.name, name, f.Name)
			}
		}
	}

	// 4. the builders and the name index
	t := &ndjsonTable{name: name, schema: arrow.NewSchema(fields, nil), index: make(map[string]int, len(fields))}
	for i, f := range fields {
		b := array.NewBuilder(memory.DefaultAllocator, f.Type)
		app, err := ndjsonAppender(f, b)
		if err != nil {
			b.Release()
			t.release()
			return nil, err
		}
		t.builders = append(t.builders, b)
		t.appenders = append(t.appenders, app)
		t.index[f.Name] = i
	}
	return t, nil
}

// appendObject reads one object's members from dec, which stands after
// the object's opening brace, into the builders of t. A member outside
// the column list is an error naming it, a column the object lacks is
// null, and a null or absent $timestamp is an error, because the index
// cannot be null. The $table member is skipped, because it routed the
// row here.
func (t *ndjsonTable) appendObject(dec *jsontext.Decoder) error {
	// The members come in the object's order and the object may omit a
	// column, so the row is filled in two passes:
	//
	//  1. read members until the closing brace: a name outside the column
	//     list is the body's fault, a null member appends null, and any
	//     other member goes through its appender;
	//  2. every builder the object did not reach appends null, found by
	//     comparing the builder's length with the row count;
	//  3. the index builder must have grown by a value, because a row
	//     without a $timestamp cannot be written.

	// 1. the members the object names
	for {
		tok, err := dec.ReadToken()
		if err != nil {
			return err
		}
		if tok.Kind() == '}' {
			break
		}
		name := tok.String()
		if name == "$table" {
			if err := dec.SkipValue(); err != nil {
				return err
			}
			continue
		}
		i, ok := t.index[name]
		if !ok {
			return fmt.Errorf("column %s is not in the column list", name)
		}
		if tok, err = dec.ReadToken(); err != nil {
			return err
		}
		if tok.Kind() == 'n' {
			if i == 0 {
				return errors.New("null $timestamp")
			}
			t.builders[i].AppendNull()
			continue
		}
		if err := t.appenders[i](tok); err != nil {
			return fmt.Errorf("column %s: %w", name, err)
		}
	}
	t.rows++

	// 2. the columns the object did not name
	for i, b := range t.builders {
		if b.Len() < t.rows {
			// 3. the index cannot be null, so an absent $timestamp is the
			// body's fault
			if i == 0 {
				return errors.New("no $timestamp")
			}
			b.AppendNull()
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

// ndjsonRow is one object of the body, read whole as a value, with the
// decoder that reads its tokens. One row is read at a time, so the
// value aliases the body decoder's buffer and the row decoder is reset
// over it for every pass.
type ndjsonRow struct {
	val jsontext.Value
	rd  *bytes.Reader
	dec *jsontext.Decoder
}

func newNDJSONRow() *ndjsonRow {
	rd := bytes.NewReader(nil)
	return &ndjsonRow{rd: rd, dec: jsontext.NewDecoder(rd)}
}

// open starts a pass over the row's members. The row must be an object,
// or the error says what it is.
func (r *ndjsonRow) open() error {
	r.rd.Reset(r.val)
	r.dec.Reset(r.rd)
	tok, err := r.dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '{' {
		return fmt.Errorf("a JSON %s where an object is expected", tok.Kind())
	}
	return nil
}

// names walks the row's members and returns their names in order, the
// column list a first object fixes.
func (r *ndjsonRow) names() ([]string, error) {
	if err := r.open(); err != nil {
		return nil, err
	}
	var names []string
	for {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return nil, err
		}
		if tok.Kind() == '}' {
			return names, nil
		}
		names = append(names, tok.String())
		if err := r.dec.SkipValue(); err != nil {
			return nil, err
		}
	}
}

// table walks the row's members and returns the $table member, which
// must be a string. The second value is false when the object has none.
func (r *ndjsonRow) table() (string, bool, error) {
	if err := r.open(); err != nil {
		return "", false, err
	}
	for {
		tok, err := r.dec.ReadToken()
		if err != nil {
			return "", false, err
		}
		if tok.Kind() == '}' {
			return "", false, nil
		}
		if tok.String() != "$table" {
			if err := r.dec.SkipValue(); err != nil {
				return "", false, err
			}
			continue
		}
		if tok, err = r.dec.ReadToken(); err != nil {
			return "", false, err
		}
		if tok.Kind() != '"' {
			return "", false, kindError(tok, "string")
		}
		return tok.String(), true, nil
	}
}

// ndjsonHeader is the column list the first object fixes: the names of
// its data columns, in its order.
type ndjsonHeader struct {
	names []string
}

// readNDJSONHeader fixes the column list from the first object's member
// names. $table and $timestamp are required, and every other name is a
// data column.
func readNDJSONHeader(row *ndjsonRow) (ndjsonHeader, error) {
	names, err := row.names()
	if err != nil {
		return ndjsonHeader{}, fmt.Errorf("%w: header: %w", ErrInvalidRows, err)
	}
	h := ndjsonHeader{}
	table, timestamp := false, false
	for _, name := range names {
		switch name {
		case "$table":
			table = true
		case "$timestamp":
			timestamp = true
		default:
			h.names = append(h.names, name)
		}
	}
	switch {
	case !table:
		return ndjsonHeader{}, fmt.Errorf("%w: header names no $table", ErrInvalidRows)
	case !timestamp:
		return ndjsonHeader{}, fmt.Errorf("%w: header names no $timestamp", ErrInvalidRows)
	}
	return h, nil
}

// Decode implements Decoder. The body is one JSON object per row, in this
// encoder's dialect: the first object's keys fix the column list, $table
// and $timestamp among them, and a later object may omit a column.
func (NDJSON) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// The decoder makes one pass over the body and streams the objects
	// into per-table builders, the three steps of CSV.Decode over values:
	//
	//  1. the first object fixes the column list and says which members
	//     hold the table, the index and the data columns. It is kept as a
	//     row and appended like every later one, so the body is read
	//     once;
	//  2. each object goes to the builders of its table, which its $table
	//     member names; a table seen for the first time is typed through
	//     schemaOf and must agree with the first table's types. The row
	//     is read whole as one value, because the $table member may come
	//     after the members it routes, and the value is walked once for
	//     the table and once for the members;
	//  3. at the end every table becomes one batch, in first-seen order.
	dec := jsontext.NewDecoder(r)
	row := newNDJSONRow()
	tables := map[string]*ndjsonTable{}
	var order []*ndjsonTable
	var h ndjsonHeader
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
		val, err := dec.ReadValue()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			release()
			// Both error chains stay reachable. The sentinel decides the status,
			// and the reader's cause reveals a body-size cap the HTTP layer set.
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, n, err)
		}
		row.val = val

		// 1. the header, from the first object
		if n == 1 {
			if h, err = readNDJSONHeader(row); err != nil {
				return nil, err
			}
		}

		// 2. the object to its table's builders
		name, ok, err := row.table()
		if err == nil && !ok {
			err = errors.New("no $table")
		}
		if err != nil {
			release()
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, n, err)
		}
		t, ok := tables[name]
		if !ok {
			var first *ndjsonTable
			if len(order) > 0 {
				first = order[0]
			}
			if t, err = newNDJSONTable(name, h.names, schemaOf, first); err != nil {
				release()
				return nil, err
			}
			tables[t.name] = t
			order = append(order, t)
		}
		if err := row.open(); err != nil {
			release()
			return nil, fmt.Errorf("%w: row %d: %w", ErrInvalidRows, n, err)
		}
		if err := t.appendObject(row.dec); err != nil {
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
