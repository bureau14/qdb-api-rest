package qdb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/model"
)

// ErrInvalidPushOptions is a push no writer can be built for: a mode word
// outside the vocabulary, or deduplication columns without a mode and a
// mode without columns. It is the caller's error and is found before any
// session is leased.
var ErrInvalidPushOptions = errors.New("qdb: invalid push options")

// PushOptions tune one batch push, in the words the HTTP surface uses.
// Mode is transactional, fast or async, the empty word meaning fast.
// DeduplicationMode is drop or upsert, the empty word meaning none; either
// mode needs DeduplicationColumns, which say what a duplicate is, while
// the mode says what happens to one.
type PushOptions struct {
	Mode                 string
	DeduplicationMode    string
	DeduplicationColumns []string
}

// pushModes maps the mode vocabulary onto the binding's push modes.
var pushModes = map[string]qdbapi.WriterPushMode{
	"":              qdbapi.WriterPushModeFast,
	"fast":          qdbapi.WriterPushModeFast,
	"transactional": qdbapi.WriterPushModeTransactional,
	"async":         qdbapi.WriterPushModeAsync,
}

// deduplicationModes maps the deduplication vocabulary onto the binding's.
var deduplicationModes = map[string]qdbapi.WriterDeduplicationMode{
	"":       qdbapi.WriterDeduplicationModeDisabled,
	"drop":   qdbapi.WriterDeduplicationModeDrop,
	"upsert": qdbapi.WriterDeduplicationModeUpsert,
}

// writerOptions is o as the writer takes it, or the first invalid word's
// error. The columns and the mode come together: the binding would
// deduplicate on every column for a bare drop, a silent widening this
// surface refuses.
func (o PushOptions) writerOptions() (qdbapi.WriterOptions, error) {
	mode, ok := pushModes[o.Mode]
	if !ok {
		return qdbapi.WriterOptions{}, fmt.Errorf("%w: unknown push mode %q", ErrInvalidPushOptions, o.Mode)
	}
	dedup, ok := deduplicationModes[o.DeduplicationMode]
	switch {
	case !ok:
		return qdbapi.WriterOptions{}, fmt.Errorf("%w: unknown deduplication mode %q", ErrInvalidPushOptions, o.DeduplicationMode)
	case dedup != qdbapi.WriterDeduplicationModeDisabled && len(o.DeduplicationColumns) == 0:
		return qdbapi.WriterOptions{}, fmt.Errorf("%w: deduplication mode %s names no columns", ErrInvalidPushOptions, o.DeduplicationMode)
	case dedup == qdbapi.WriterDeduplicationModeDisabled && len(o.DeduplicationColumns) > 0:
		return qdbapi.WriterOptions{}, fmt.Errorf("%w: deduplication columns without a mode", ErrInvalidPushOptions)
	}
	opts := qdbapi.NewWriterOptions().WithPushMode(mode).WithDeduplicationMode(dedup)
	if dedup != qdbapi.WriterDeduplicationModeDisabled {
		opts = opts.EnableDropDuplicatesOn(o.DeduplicationColumns)
	}
	return opts, nil
}

// IngestResult is what one push wrote and how long its two halves took:
// Parse from the first byte read to the end of the body, Push the batch
// push call itself.
type IngestResult struct {
	Rows, Tables int
	Parse, Push  time.Duration
}

// Decode reads one body into one batch per table, typing each table the
// first time the body names it through schemaOf. Its error is the body's
// or the lookup's, as is; on error there are no batches.
type Decode func(schemaOf model.SchemaOf) ([]model.TableBatch, error)

// schemaOf answers the reader's whole-table schema of name through s:
// its columns looked up through the held session, shaped as a read
// without a column list. The binding's error of an unknown table passes
// as is, so IsTableNotFound classifies it.
func (s *Session) schemaOf(name string) (*arrow.Schema, error) {
	cols, err := s.session.Table(name).ColumnsInfo()
	if err != nil {
		return nil, err
	}
	return schemaOf(cols, ReadOptions{})
}

// ingest runs decode under s's schema lookup and pushes its batches once
// under opts.
func (s *Session) ingest(decode Decode, opts qdbapi.WriterOptions) (IngestResult, error) {
	// The lease spans the decode and the push, since the lookups need a
	// session and the push the same one:
	//
	//  1. decode the body under the held session's lookup; its error is
	//     returned as is, nothing has been staged; Parse is its duration;
	//  2. release every batch when the function returns, on every path,
	//     since the receiver of a decoded batch owns it;
	//  3. stage every batch in one writer; Rows sums the batches' rows and
	//     Tables counts the batches with any, since the writer skips the
	//     rest and a header-only body answers zeros;
	//  4. no rows: answer without a push;
	//  5. push through the session; Push is the call's duration.
	var res IngestResult

	// 1. decode
	start := time.Now()
	batches, err := decode(s.schemaOf)
	if err != nil {
		return res, err
	}
	res.Parse = time.Since(start)

	// 2. the batches are ours to release
	defer func() {
		for _, b := range batches {
			b.Batch.Release()
		}
	}()

	// 3. stage
	w := qdbapi.NewArrowWriter(opts)
	for _, b := range batches {
		if err := w.SetTable(b.Table, b.Batch); err != nil {
			return res, err
		}
		if rows := int(b.Batch.NumRows()); rows > 0 {
			res.Rows += rows
			res.Tables++
		}
	}

	// 4. nothing to push
	if res.Rows == 0 {
		return res, nil
	}

	// 5. one push
	start = time.Now()
	err = s.PushArrow(&w)
	res.Push = time.Since(start)
	return res, err
}

// Ingest pushes the batches decode reads as u in one batch under o. The
// session is held for the whole body: the tables' schemas are looked up
// through it as the body names them and the push runs through it.
// Invalid options fail before a session is leased; an invalid body or an
// unknown table fails the whole request before the push. Never retried:
// a push is not a read.
func (c *Cluster) Ingest(ctx context.Context, u User, o PushOptions, decode Decode) (IngestResult, error) {
	opts, err := o.writerOptions()
	if err != nil {
		return IngestResult{}, err
	}
	var res IngestResult
	err = c.Call(ctx, u, func(s *Session) error {
		var err error
		res, err = s.ingest(decode, opts)
		return err
	})
	if err != nil {
		return IngestResult{}, err
	}
	return res, nil
}
