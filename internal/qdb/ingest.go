package qdb

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
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

// ErrInvalidRows is a body no push can be built from: a header without
// $table or $timestamp, a header name a table does not have, two tables of
// one body with differently typed columns, a record of the wrong width, a
// field that does not parse, or an empty $timestamp. It is the caller's
// error and fails the whole request; nothing is pushed.
var ErrInvalidRows = errors.New("qdb: invalid rows")

// IngestResult is what one push wrote and how long its two halves took:
// Parse from the first byte read to the end of the body, Push the batch
// push call itself, which under the async mode returns before the rows
// are readable.
type IngestResult struct {
	Rows, Tables int
	Parse, Push  time.Duration
}

// cells is one column's parsed values on their way to the writer: append
// reads one field, data hands the column over. A parser exists per type,
// the CSV encoder's rendering inverted; the empty field is the type's null
// sentinel, since the writer knows no other null.
type cells interface {
	append(field string) error
	data() qdbapi.ColumnData
}

type int64Cells struct{ xs []int64 }

func (c *int64Cells) append(field string) error {
	if field == "" {
		c.xs = append(c.xs, math.MinInt64)
		return nil
	}
	v, err := strconv.ParseInt(field, 10, 64)
	c.xs = append(c.xs, v)
	return err
}

func (c *int64Cells) data() qdbapi.ColumnData {
	d := qdbapi.NewColumnDataInt64(c.xs)
	return &d
}

type doubleCells struct{ xs []float64 }

func (c *doubleCells) append(field string) error {
	if field == "" {
		c.xs = append(c.xs, math.NaN())
		return nil
	}
	v, err := strconv.ParseFloat(field, 64)
	c.xs = append(c.xs, v)
	return err
}

func (c *doubleCells) data() qdbapi.ColumnData {
	d := qdbapi.NewColumnDataDouble(c.xs)
	return &d
}

type timestampCells struct{ xs []time.Time }

func (c *timestampCells) append(field string) error {
	if field == "" {
		c.xs = append(c.xs, qdbapi.NullTime())
		return nil
	}
	v, err := time.Parse(time.RFC3339Nano, field)
	c.xs = append(c.xs, v)
	return err
}

func (c *timestampCells) data() qdbapi.ColumnData {
	d := qdbapi.NewColumnDataTimestamp(c.xs)
	return &d
}

// stringCells carry string and symbol columns alike; the binding writes
// both as strings.
type stringCells struct{ xs []string }

func (c *stringCells) append(field string) error {
	c.xs = append(c.xs, field)
	return nil
}

func (c *stringCells) data() qdbapi.ColumnData {
	d := qdbapi.NewColumnDataString(c.xs)
	return &d
}

type blobCells struct{ xs [][]byte }

func (c *blobCells) append(field string) error {
	if field == "" {
		c.xs = append(c.xs, nil)
		return nil
	}
	v, err := base64.StdEncoding.DecodeString(field)
	c.xs = append(c.xs, v)
	return err
}

func (c *blobCells) data() qdbapi.ColumnData {
	d := qdbapi.NewColumnDataBlob(c.xs)
	return &d
}

// newCells is the parser of a column of type kind.
func newCells(kind qdbapi.TsColumnType) cells {
	switch kind {
	case qdbapi.TsColumnInt64:
		return &int64Cells{}
	case qdbapi.TsColumnDouble:
		return &doubleCells{}
	case qdbapi.TsColumnTimestamp:
		return &timestampCells{}
	case qdbapi.TsColumnString, qdbapi.TsColumnSymbol:
		return &stringCells{}
	case qdbapi.TsColumnBlob:
		return &blobCells{}
	}
	panic(fmt.Sprintf("qdb: column type %v has no parser", kind))
}

// header is the body's first record read: where $table and $timestamp
// sit, and the data columns' names in their order.
type header struct {
	table, timestamp int
	names            []string
	fields           []int // the field of each data column
}

// readHeader reads the first record and finds the two required columns;
// every other name is a data column, typed later by the table it is
// pushed to.
func readHeader(rd *csv.Reader) (header, error) {
	rec, err := rd.Read()
	if err != nil {
		return header{}, fmt.Errorf("%w: header: %v", ErrInvalidRows, err)
	}
	h := header{table: -1, timestamp: -1}
	for i, name := range rec {
		switch name {
		case "$table":
			h.table = i
		case "$timestamp":
			h.timestamp = i
		default:
			h.names = append(h.names, name)
			h.fields = append(h.fields, i)
		}
	}
	switch {
	case h.table < 0:
		return header{}, fmt.Errorf("%w: header names no $table", ErrInvalidRows)
	case h.timestamp < 0:
		return header{}, fmt.Errorf("%w: header names no $timestamp", ErrInvalidRows)
	}
	return h, nil
}

// ingestTable is one table's rows as they accumulate: its typed columns
// (the header's names, typed by the table), its index and its cells.
type ingestTable struct {
	name    string
	columns []qdbapi.WriterColumn
	index   []time.Time
	cells   []cells
}

// typedColumns is the header's data columns typed by the table's schema;
// a name the table does not have is ErrInvalidRows.
func (s *Session) typedColumns(name string, h header) ([]qdbapi.WriterColumn, error) {
	infos, err := s.session.Table(name).ColumnsInfo()
	if err != nil {
		return nil, err
	}
	types := make(map[string]qdbapi.TsColumnType, len(infos))
	for _, info := range infos {
		types[info.Name()] = info.Type()
	}
	cols := make([]qdbapi.WriterColumn, len(h.names))
	for i, n := range h.names {
		kind, ok := types[n]
		if !ok {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrInvalidRows, name, n)
		}
		cols[i] = qdbapi.WriterColumn{ColumnName: n, ColumnType: kind}
	}
	return cols, nil
}

// newIngestTable looks the table up and types the header by it. Every
// table of one body must agree with the first on the columns' types (the
// names are the header's already), the writer's own rule for one push.
func (s *Session) newIngestTable(name string, h header, first *ingestTable) (*ingestTable, error) {
	cols, err := s.typedColumns(name, h)
	if err != nil {
		return nil, err
	}
	if first != nil {
		for i, c := range cols {
			if c != first.columns[i] {
				return nil, fmt.Errorf("%w: tables %s and %s differ in the type of column %s", ErrInvalidRows, first.name, name, c.ColumnName)
			}
		}
	}
	t := &ingestTable{name: name, columns: cols, cells: make([]cells, len(cols))}
	for i, c := range cols {
		t.cells[i] = newCells(c.ColumnType)
	}
	return t, nil
}

// appendRecord parses one record into t: the index first, which cannot be
// null, then every data column. Its error names the column; the caller
// adds the row and the sentinel.
func (t *ingestTable) appendRecord(rec []string, h header) error {
	if rec[h.timestamp] == "" {
		return errors.New("empty $timestamp")
	}
	ts, err := time.Parse(time.RFC3339Nano, rec[h.timestamp])
	if err != nil {
		return fmt.Errorf("$timestamp: %w", err)
	}
	t.index = append(t.index, ts)
	for i, f := range h.fields {
		if err := t.cells[i].append(rec[f]); err != nil {
			return fmt.Errorf("column %s: %w", h.names[i], err)
		}
	}
	return nil
}

// writerTable is t as the writer takes it.
func (t *ingestTable) writerTable() (qdbapi.WriterTable, error) {
	wt, err := qdbapi.NewWriterTable(t.name, t.columns)
	if err != nil {
		return wt, err
	}
	if err := wt.SetIndex(t.index); err != nil {
		return wt, err
	}
	for i, c := range t.cells {
		if err := wt.SetData(i, c.data()); err != nil {
			return wt, err
		}
	}
	return wt, nil
}

// ingestCSV parses body and pushes it once through s.
func (s *Session) ingestCSV(body io.Reader, opts qdbapi.WriterOptions) (IngestResult, error) {
	// One pass over the body, then one push, all through the one session
	// the caller holds:
	//
	//  1. read the header: $table and $timestamp required, the rest data
	//     columns, untyped until a table is seen;
	//  2. stream the records, each into its table's columns; a table seen
	//     for the first time is looked up through the session and typed,
	//     and must agree with the first table's types;
	//  3. no rows: answer without a push, since the writer refuses an
	//     empty one;
	//  4. build one writer table per table, in first-seen order, and push
	//     them in one batch under the caller's options.
	var res IngestResult
	start := time.Now()
	rd := csv.NewReader(body)
	rd.ReuseRecord = true

	// 1. the header
	h, err := readHeader(rd)
	if err != nil {
		return res, err
	}

	// 2. the records
	tables := map[string]*ingestTable{}
	var order []*ingestTable
	for row := 1; ; row++ {
		rec, err := rd.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return res, fmt.Errorf("%w: row %d: %v", ErrInvalidRows, row, err)
		}
		name := rec[h.table]
		t, ok := tables[name]
		if !ok {
			var first *ingestTable
			if len(order) > 0 {
				first = order[0]
			}
			if t, err = s.newIngestTable(name, h, first); err != nil {
				return res, err
			}
			tables[name] = t
			order = append(order, t)
		}
		if err := t.appendRecord(rec, h); err != nil {
			return res, fmt.Errorf("%w: row %d: %v", ErrInvalidRows, row, err)
		}
		res.Rows++
	}
	res.Parse = time.Since(start)

	// 3. nothing to push
	if res.Rows == 0 {
		return res, nil
	}

	// 4. one writer, one push
	w := qdbapi.NewWriter(opts)
	for _, t := range order {
		wt, err := t.writerTable()
		if err != nil {
			return res, err
		}
		if err := w.SetTable(wt); err != nil {
			return res, err
		}
	}
	res.Tables = len(order)
	start = time.Now()
	err = s.Push(&w)
	res.Push = time.Since(start)
	return res, err
}

// IngestCSV parses body, RFC 4180 text in the CSV encoder's dialect with a
// $table column routing each row and a $timestamp column as the index,
// and pushes every row as u in one batch under o. The session is held for
// the whole body: the tables' columns are looked up through it as they
// appear and the push runs through it. Invalid options fail before a
// session is leased; an invalid body or an unknown table fails the whole
// request before the push. Never retried: a push is not a read.
func (c *Cluster) IngestCSV(ctx context.Context, u User, body io.Reader, o PushOptions) (IngestResult, error) {
	opts, err := o.writerOptions()
	if err != nil {
		return IngestResult{}, err
	}
	var res IngestResult
	err = c.Call(ctx, u, func(s *Session) error {
		var err error
		res, err = s.ingestCSV(body, opts)
		return err
	})
	if err != nil {
		return IngestResult{}, err
	}
	return res, nil
}
