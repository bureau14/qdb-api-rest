package qdb

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/model"
)

// ReadOptions narrows a table read. A nil Columns reads every column,
// $table and $timestamp first. A named list reads those columns in that
// order, with the specials only when named. Start and End are both set
// or both zero. The binding judges the pair when the reader opens,
// before any fetch. A zero BatchRows means model.BatchRows.
type ReadOptions struct {
	Columns    []string
	Start, End time.Time
	BatchRows  int
}

// readerOptions converts the options into the bulk reader's options for
// a read of the table called name. A zero range is no range, so the
// binding reads the whole table.
func (o ReadOptions) readerOptions(name string) qdbapi.ReaderOptions {
	return qdbapi.NewReaderOptions().
		WithTables([]string{name}).
		WithColumns(o.Columns).
		WithTimeRange(o.Start, o.End).
		WithBatchSize(cmp.Or(o.BatchRows, model.BatchRows))
}

// ErrUnknownColumn is the read error for a column the table does not
// have. The reader itself refuses such a read only once it has rows to
// fetch. The schema of an empty table is built from the table's
// metadata, which catches the unknown column first.
var ErrUnknownColumn = errors.New("qdb: unknown column")

// specialFields are the two columns every table has and the reader
// answers non-nullable: the table a row came from and its index.
var specialFields = map[string]arrow.Field{
	"$table":     {Name: "$table", Type: qdbapi.TsColumnString.ArrowType()},
	"$timestamp": {Name: "$timestamp", Type: qdbapi.TsColumnTimestamp.ArrowType()},
}

// dataFields is the table's own columns as the reader answers them, each
// nullable and in the binding's Arrow type for its column type: as a
// list in the table's order, and as a map by name for picking a
// requested subset.
func dataFields(cols []qdbapi.TsColumnInfo) ([]arrow.Field, map[string]arrow.Field) {
	fields := make([]arrow.Field, len(cols))
	byName := make(map[string]arrow.Field, len(cols))
	for i, c := range cols {
		fields[i] = arrow.Field{Name: c.Name(), Type: c.Type().ArrowType(), Nullable: true}
		byName[c.Name()] = fields[i]
	}
	return fields, byName
}

// schemaOf returns the schema the reader gives for a table of these
// columns read under the options. An unknown requested name is
// ErrUnknownColumn.
func schemaOf(cols []qdbapi.TsColumnInfo, o ReadOptions) (*arrow.Schema, error) {
	// The schema follows the reader's layout. Without a column list it is
	// $table, $timestamp, then the table's columns. With one it is the
	// requested names in their order, and the two specials appear only
	// when named, like any column.
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

// Batches is a result as the receiver sees it: a sequence of record
// batches, each lent until yield returns and released by the sequence,
// so the receiver releases none. An error element carries a nil batch
// and is the last. The sequence is never empty.
type Batches = iter.Seq2[arrow.RecordBatch, error]

// lent turns the batches the reader owns into the lent Batches. The
// schema is the reader's, and it shapes the empty batch that an empty
// table yields.
func lent(owned iter.Seq2[arrow.RecordBatch, error], schema *arrow.Schema) Batches {
	return func(yield func(arrow.RecordBatch, error) bool) {
		// The binding hands out one owned reference per batch and wants it
		// released exactly once. Releasing after yield returns, whether the
		// receiver broke out or not, keeps that promise in one place.
		// Note: a value read from the batch aliases its buffers and is
		// garbage after the release. A receiver that keeps one copies it.
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
		// so the empty batch stands in and every receiver sees a batch.
		if !yielded {
			rec := model.EmptyBatch(schema)
			yield(rec, nil)
			rec.Release()
		}
	}
}

// Read reads the table through the bulk reader and hands its batches to
// the sink, one per fetch. The next batch is fetched only when yield
// returns. An error before the sink runs comes from the table, the range
// or a column. The sink's own error is returned unchanged.
func (s *Session) Read(name string, o ReadOptions, sink func(Batches) error) error {
	// Everything that can refuse the read runs before the sink, so a caller
	// that has to decide a status (the HTTP handler) decides it before the
	// first byte:
	//
	//  1. open the reader. The binding judges the range and the batch
	//     size, and the cluster judges the table's existence;
	//  2. fetch the table's columns, which is where an empty table's schema
	//     comes from;
	//  3. shape that schema under the options, which finds an unknown column;
	//  4. run the sink over the lent batches. The reader closes after it.

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

// Read reads the table as the user through the bulk reader and runs the
// sink over its batches while the session is held, because every fetch
// needs the live handle. A read occupies one session of the user's pool
// for as long as the sink runs, and the budget bounds concurrent reads
// like any other call. A read is never retried, because the sink may have
// written to the caller.
func (c *Cluster) Read(ctx context.Context, u User, name string, o ReadOptions, sink func(Batches) error) error {
	return c.Call(ctx, u, func(s *Session) error { return s.Read(name, o, sink) })
}
