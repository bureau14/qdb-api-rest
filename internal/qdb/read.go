package qdb

import (
	"cmp"
	"context"
	"iter"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
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

// Batches is a table read as the caller sees it: one record batch per
// step, lent for that step and released when the step returns, so the
// receiver never releases one; an error step carries a nil batch and is
// the last.
type Batches = iter.Seq2[arrow.RecordBatch, error]

// lent wraps the reader's owned batches into lent ones: each is released
// once the receiver's step returns, whether it broke out or not.
func lent(owned iter.Seq2[arrow.RecordBatch, error]) Batches {
	return func(yield func(arrow.RecordBatch, error) bool) {
		for rec, err := range owned {
			more := yield(rec, err)
			if rec != nil {
				rec.Release()
			}
			if !more {
				return
			}
		}
	}
}

// Read opens the bulk reader over the table name and hands its batches to
// sink, one per fetch, the next fetched only when sink's step returns:
// memory is one batch whatever the table's size. A missing table or a bad
// range fails here, before sink runs. The reader is closed after sink
// returns, whatever it returned.
func (s *Session) Read(name string, o ReadOptions, sink func(Batches) error) error {
	rd, err := qdbapi.NewReader(s.session, o.readerOptions(name))
	if err != nil {
		return err
	}
	defer rd.Close()
	return sink(lent(rd.Arrow()))
}

// Read reads the table name as u through the bulk reader and runs sink
// over its batches while the session is held: every fetch needs the live
// handle, so a read occupies one session of u's pool for as long as sink
// runs, and the budget bounds concurrent reads like any other call. A read
// is never retried: sink may have written to the caller.
func (c *Cluster) Read(ctx context.Context, u User, name string, o ReadOptions, sink func(Batches) error) error {
	return c.Call(ctx, u, func(s *Session) error { return s.Read(name, o, sink) })
}
