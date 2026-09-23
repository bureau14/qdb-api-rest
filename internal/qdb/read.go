package qdb

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// readBatchRows is the rows per fetch when ReadOptions names none: the
// encoders' chunk, so one fetch is one record batch on the Arrow wire.
const readBatchRows = 65536

// ReadOptions narrows a table read. Columns nil reads every column, $table
// and $timestamp first; named, exactly those in that order, the specials
// only when named. Start and End are both set or both zero; the binding
// judges the pair, before any session is leased for the fetch. BatchRows
// zero is readBatchRows.
type ReadOptions struct {
	Columns    []string
	Start, End time.Time
	BatchRows  int
}

// readerOptions is o as the bulk reader takes it, over the one table
// name. A zero range is no range: the binding reads the whole table.
func (o ReadOptions) readerOptions(name string) qdbapi.ReaderOptions {
	return qdbapi.NewReaderOptions().
		WithTables([]string{name}).
		WithColumns(o.Columns).
		WithTimeRange(o.Start, o.End).
		WithBatchSize(cmp.Or(o.BatchRows, readBatchRows))
}

// ErrUnknownColumn is a read that names a column the table does not have.
// The reader itself refuses one only once it has rows to fetch; the
// schema of an empty table is answered from its metadata, which finds it
// first.
var ErrUnknownColumn = errors.New("qdb: unknown column")

// timestampType is the reader's timestamp: nanoseconds, no zone.
var timestampType = &arrow.TimestampType{Unit: arrow.Nanosecond}

// specialFields are the two columns every table has and the reader
// answers non-nullable: the table a row came from and its index.
var specialFields = map[string]arrow.Field{
	"$table":     {Name: "$table", Type: arrow.BinaryTypes.String},
	"$timestamp": {Name: "$timestamp", Type: timestampType},
}

// arrowTypes maps a column type onto the type the reader answers it in:
// a symbol reads as a string.
var arrowTypes = map[qdbapi.TsColumnType]arrow.DataType{
	qdbapi.TsColumnInt64:     arrow.PrimitiveTypes.Int64,
	qdbapi.TsColumnDouble:    arrow.PrimitiveTypes.Float64,
	qdbapi.TsColumnString:    arrow.BinaryTypes.String,
	qdbapi.TsColumnSymbol:    arrow.BinaryTypes.String,
	qdbapi.TsColumnBlob:      arrow.BinaryTypes.Binary,
	qdbapi.TsColumnTimestamp: timestampType,
}

// dataFields is the table's own columns as the reader answers them:
// nullable, in the table's order, keyed by name for the requested subset.
func dataFields(cols []qdbapi.TsColumnInfo) ([]arrow.Field, map[string]arrow.Field) {
	fields := make([]arrow.Field, len(cols))
	byName := make(map[string]arrow.Field, len(cols))
	for i, c := range cols {
		fields[i] = arrow.Field{Name: c.Name(), Type: arrowTypes[c.Type()], Nullable: true}
		byName[c.Name()] = fields[i]
	}
	return fields, byName
}

// schemaOf is the schema the reader answers for a table with cols read
// under o. An unknown requested name is ErrUnknownColumn.
func schemaOf(cols []qdbapi.TsColumnInfo, o ReadOptions) (*arrow.Schema, error) {
	// The reader's layout: without a column list, $table, $timestamp, then
	// the table's columns; with one, exactly the requested names in their
	// order, the two specials answered only when named, like any column.
	fields, byName := dataFields(cols)
	if o.Columns == nil {
		return arrow.NewSchema(append([]arrow.Field{specialFields["$table"], specialFields["$timestamp"]}, fields...), nil), nil
	}
	picked := make([]arrow.Field, len(o.Columns))
	for i, name := range o.Columns {
		f, ok := specialFields[name]
		if !ok {
			if f, ok = byName[name]; !ok {
				return nil, fmt.Errorf("%w: %s", ErrUnknownColumn, name)
			}
		}
		picked[i] = f
	}
	return arrow.NewSchema(picked, nil), nil
}

// emptyBatch is a batch of schema and no rows: what an empty table
// answers, since the reader yields nothing for one.
func emptyBatch(schema *arrow.Schema) arrow.RecordBatch {
	cols := make([]arrow.Array, schema.NumFields())
	for i, f := range schema.Fields() {
		cols[i] = array.MakeArrayOfNull(memory.DefaultAllocator, f.Type, 0)
	}
	rec := array.NewRecordBatch(schema, cols, 0)
	for _, c := range cols {
		c.Release()
	}
	return rec
}

// Batches is a table read as the caller sees it: one record batch per
// step, lent for that step and released when the step returns, so the
// receiver never releases one; an error step carries a nil batch and is
// the last. There is always at least one step.
type Batches = iter.Seq2[arrow.RecordBatch, error]

// lent turns the reader's owned batches into the lent Batches over
// schema, the schema the reader answers.
func lent(owned iter.Seq2[arrow.RecordBatch, error], schema *arrow.Schema) Batches {
	return func(yield func(arrow.RecordBatch, error) bool) {
		// The binding hands out one owned reference per batch and wants it
		// released exactly once; releasing after the step returns, whether
		// the receiver broke out or not, keeps that promise in one place.
		// Note: a value read from the batch aliases its buffers and is
		// garbage after the release; a receiver that keeps one copies it.
		yielded := false
		for rec, err := range owned {
			yielded = true
			more := yield(rec, err)
			if rec != nil {
				rec.Release()
			}
			if !more {
				return
			}
		}
		// The reader yields nothing for an empty table, not even a schema,
		// so the schema-only batch stands in and every receiver sees a step.
		if !yielded {
			rec := emptyBatch(schema)
			yield(rec, nil)
			rec.Release()
		}
	}
}

// Read reads the table name under o through the bulk reader and hands its
// batches to sink, one per fetch, the next fetched only when sink's step
// returns. An error before sink runs is the table's, the range's or a
// column's; sink's own error is returned as is.
func (s *Session) Read(name string, o ReadOptions, sink func(Batches) error) error {
	// Everything that can refuse the read runs before the sink, so a caller
	// that has to decide a status (the HTTP handler) decides it before the
	// first byte:
	//
	//  1. open the reader: the binding judges the range and the batch size,
	//     the cluster the table's existence;
	//  2. fetch the table's columns, the schema an empty table answers;
	//  3. shape that schema under o, which finds an unknown column;
	//  4. run the sink over the lent batches, the reader closing after it.

	// 1. open the reader
	rd, err := qdbapi.NewReader(s.session, o.readerOptions(name))
	if err != nil {
		return err
	}
	defer rd.Close()
	// 2. fetch the columns
	cols, err := s.session.Table(name).ColumnsInfo()
	if err != nil {
		return err
	}
	// 3. shape the schema
	schema, err := schemaOf(cols, o)
	if err != nil {
		return err
	}
	// 4. run the sink
	return sink(lent(rd.Arrow(), schema))
}

// Read reads the table name as u through the bulk reader and runs sink
// over its batches while the session is held: every fetch needs the live
// handle, so a read occupies one session of u's pool for as long as sink
// runs, and the budget bounds concurrent reads like any other call. A read
// is never retried: sink may have written to the caller.
func (c *Cluster) Read(ctx context.Context, u User, name string, o ReadOptions, sink func(Batches) error) error {
	return c.Call(ctx, u, func(s *Session) error { return s.Read(name, o, sink) })
}
