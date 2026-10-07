// Package table is the generated table fixture, in building blocks that
// stack: GenerateSchema draws a table without rows, Generate draws rows
// into one as a record batch in the reader's types, GenerateLike draws
// another table of the same columns, RemoveOnCleanup removes whatever a
// table leaves behind, and Create creates the table, pushes its batch
// through the Arrow writer and removes it on the test's cleanup. Check
// and CheckColumn compare what a read answers with the batch written, by
// array equality. WithTable and Body are the batch as an ingest body
// carries it, for a test that pushes through its own door (an HTTP
// route). The blocks are functions of the test's rapid.T and take no
// options: the consumers differ only in bounds, and one set serves all.
// Rules: internal/AGENTS.md, Tests.
package table

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

// T is the slice of testing.TB the fixture needs; *testing.T and *rapid.T
// both fit.
type T interface {
	Helper()
	Fatalf(string, ...any)
	Errorf(string, ...any)
	Cleanup(func())
}

// Column is one column of a table's schema: its name, its QuasarDB
// column type and, for a symbol, its symtable.
type Column struct {
	Name     string
	Type     qdbapi.TsColumnType
	Symtable string // symbol columns only
}

// Table is a generated table: its name, its schema and its rows as one
// record batch, $timestamp first and then the columns in order, each in
// the Arrow type the binding's ArrowType answers for its column type. A
// nil Batch is a table of no rows. The fixture releases the batch on
// the test's cleanup; a test never releases it.
type Table struct {
	Name    string
	Columns []Column
	Batch   arrow.RecordBatch
}

// columnTypes is what a column's type is drawn from.
var columnTypes = []qdbapi.TsColumnType{
	qdbapi.TsColumnInt64,
	qdbapi.TsColumnDouble,
	qdbapi.TsColumnString,
	qdbapi.TsColumnSymbol,
	qdbapi.TsColumnBlob,
	qdbapi.TsColumnTimestamp,
}

// indexStart is the first index value; every index begins here.
var indexStart = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

// drawSymbol draws a symbol value: a symtable entry, in which the server
// rejects arbitrary bytes. Never empty, since the server stores the
// empty string as null.
func drawSymbol(rt *rapid.T) string {
	return rapid.StringMatching(`[a-zA-Z0-9]{1,16}`).Draw(rt, "symbol")
}

// drawString draws a string value: the characters the text wires must
// quote or escape (a space, a comma, a quote, <&>, an LF) among plain
// ones. Never empty, since the server stores the empty string as null,
// and never a NUL, which the bulk reader drops from the end of a string
// (sc-19829).
func drawString(rt *rapid.T) string {
	return rapid.StringMatching(`[a-zA-Z0-9 ,"<&>\n]{1,16}`).Draw(rt, "string")
}

// drawTime draws a timestamp value in the range the result set accepts.
func drawTime(rt *rapid.T) time.Time {
	max := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	return time.Unix(0, rapid.Int64Range(0, max).Draw(rt, "nanos")).UTC()
}

// appendValue draws one value of kind into b. The server reads a stored
// MinInt64 or NaN as null and a zero-length string or blob as null, so
// none of them is drawn: a value read back must be the value written.
func appendValue(rt *rapid.T, kind qdbapi.TsColumnType, b array.Builder) {
	switch b := b.(type) {
	case *array.Int64Builder:
		// MinInt64 is the server's null for int64.
		b.Append(rapid.Int64Range(math.MinInt64+1, math.MaxInt64).Draw(rt, "int64"))
	case *array.Float64Builder:
		// rapid.Float64 draws neither NaN, the server's null, nor an infinity.
		b.Append(rapid.Float64().Draw(rt, "double"))
	case *array.StringBuilder:
		if kind == qdbapi.TsColumnSymbol {
			b.Append(drawSymbol(rt))
		} else {
			b.Append(drawString(rt))
		}
	case *array.BinaryBuilder:
		b.Append(rapid.SliceOfN(rapid.Byte(), 1, 64).Draw(rt, "blob"))
	case *array.TimestampBuilder:
		b.Append(arrow.Timestamp(drawTime(rt).UnixNano()))
	default:
		panic(fmt.Sprintf("column type %v", kind))
	}
}

// generateArray draws n cells of kind, each null at nullPct percent.
func generateArray(rt *rapid.T, kind qdbapi.TsColumnType, n, nullPct int) arrow.Array {
	b := array.NewBuilder(memory.DefaultAllocator, kind.ArrowType())
	defer b.Release()
	for range n {
		if rapid.IntRange(0, 99).Draw(rt, "null") < nullPct {
			b.AppendNull()
			continue
		}
		appendValue(rt, kind, b)
	}
	return b.NewArray()
}

// generateIndex draws a strictly ascending $timestamp column of n rows: a
// drawn step from a fixed start, so no two rows collide.
func generateIndex(rt *rapid.T, n int) arrow.Array {
	step := time.Duration(rapid.Int64Range(1, int64(time.Hour)).Draw(rt, "index step"))
	b := array.NewTimestampBuilder(memory.DefaultAllocator, qdbapi.TsColumnTimestamp.ArrowType().(*arrow.TimestampType))
	defer b.Release()
	for i := range n {
		b.Append(arrow.Timestamp(indexStart.Add(time.Duration(i) * step).UnixNano()))
	}
	return b.NewArray()
}

// Schema is the schema of tbl's batch: $timestamp, non-nullable, then
// the columns in order, nullable, in the binding's Arrow types.
func Schema(tbl Table) *arrow.Schema {
	fields := make([]arrow.Field, 0, len(tbl.Columns)+1)
	fields = append(fields, arrow.Field{Name: "$timestamp", Type: qdbapi.TsColumnTimestamp.ArrowType()})
	for _, c := range tbl.Columns {
		fields = append(fields, arrow.Field{Name: c.Name, Type: c.Type.ArrowType(), Nullable: true})
	}
	return arrow.NewSchema(fields, nil)
}

// generateRows draws tbl's rows into its batch: a row count and one
// null density for the whole table, so runs range from no nulls to
// all-null columns. The batch is released on rt's cleanup.
func generateRows(rt *rapid.T, tbl Table) Table {
	rows := rapid.IntRange(0, 40).Draw(rt, "rows")
	nullPct := rapid.IntRange(0, 100).Draw(rt, "null pct")
	cols := make([]arrow.Array, 0, len(tbl.Columns)+1)
	cols = append(cols, generateIndex(rt, rows))
	for _, c := range tbl.Columns {
		cols = append(cols, generateArray(rt, c.Type, rows, nullPct))
	}
	tbl.Batch = array.NewRecordBatch(Schema(tbl), cols, int64(rows))
	for _, c := range cols {
		c.Release()
	}
	rt.Cleanup(tbl.Batch.Release)
	return tbl
}

// generateColumn draws column i of table without cells: its name, its
// type and, for a symbol, a symtable named after both.
func generateColumn(rt *rapid.T, table string, i int) Column {
	c := Column{Name: fmt.Sprintf("c%d", i), Type: rapid.SampledFrom(columnTypes).Draw(rt, "type")}
	if c.Type == qdbapi.TsColumnSymbol {
		c.Symtable = table + "_" + c.Name
	}
	return c
}

// GenerateSchema draws a table without rows: a name and one to five
// columns of independently drawn types.
func GenerateSchema(rt *rapid.T) Table {
	name := "qdbtest_" + rapid.StringMatching(`[a-z]{16}`).Draw(rt, "table")
	cols := make([]Column, rapid.IntRange(1, 5).Draw(rt, "columns"))
	for i := range cols {
		cols[i] = generateColumn(rt, name, i)
	}
	return Table{Name: name, Columns: cols}
}

// Generate draws a table: a schema and its rows.
func Generate(rt *rapid.T) Table {
	return generateRows(rt, GenerateSchema(rt))
}

// GenerateLike draws a table of tbl's columns under a fresh name, with
// its own rows: the same names and types, a symbol column's symtable
// named after the new table, so several tables share one column list.
func GenerateLike(rt *rapid.T, tbl Table) Table {
	like := Table{Name: "qdbtest_" + rapid.StringMatching(`[a-z]{16}`).Draw(rt, "table")}
	for _, c := range tbl.Columns {
		c.Symtable = ""
		if c.Type == qdbapi.TsColumnSymbol {
			c.Symtable = like.Name + "_" + c.Name
		}
		like.Columns = append(like.Columns, c)
	}
	return generateRows(rt, like)
}

// Select is the query that answers tbl's rows as written: $timestamp
// first, then the columns in order, rows ascending by $timestamp. A bare
// SELECT * would also answer the $table column.
func (tbl Table) Select() string {
	names := make([]string, 0, len(tbl.Columns)+1)
	names = append(names, "$timestamp")
	for _, c := range tbl.Columns {
		names = append(names, c.Name)
	}
	return "SELECT " + strings.Join(names, ", ") + " FROM " + tbl.Name
}

// Rows is tbl's row count; a nil batch has none.
func (tbl Table) Rows() int {
	if tbl.Batch == nil {
		return 0
	}
	return int(tbl.Batch.NumRows())
}

// batchOf is tbl's batch, retained, or an empty batch of its schema when
// tbl has none; the caller releases it.
func batchOf(tbl Table) arrow.RecordBatch {
	if tbl.Batch != nil {
		tbl.Batch.Retain()
		return tbl.Batch
	}
	schema := Schema(tbl)
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

// tableColumn is the $table column of n rows naming tbl.
func tableColumn(tbl Table, n int) arrow.Array {
	b := array.NewStringBuilder(memory.DefaultAllocator)
	defer b.Release()
	for range n {
		b.Append(tbl.Name)
	}
	return b.NewArray()
}

// expected is the column a read of tbl answers for name, which the
// caller releases: for $table, the table name in every row; for any
// other name, the batch's column of that name; false when tbl has no
// such column.
func expected(tbl Table, name string) (arrow.Array, bool) {
	if name == "$table" {
		return tableColumn(tbl, tbl.Rows()), true
	}
	rec := batchOf(tbl)
	defer rec.Release()
	idx := rec.Schema().FieldIndices(name)
	if idx == nil {
		return nil, false
	}
	col := rec.Column(idx[0])
	col.Retain()
	return col, true
}

// CheckColumn compares one column read back under name with the column
// written: the type, every validity bit, every value. Null slots are
// compared by validity only. Array equality ignores a field's
// nullability and metadata (the reader adds max_width to string and
// blob fields), so a query result and a bulk read check alike.
func CheckColumn(t T, tbl Table, name string, got arrow.Array) {
	t.Helper()
	want, ok := expected(tbl, name)
	if !ok {
		t.Fatalf("column %q read back, never written", name)
	}
	defer want.Release()
	if !array.Equal(want, got) {
		t.Fatalf("%s: read back\n%v\nwritten\n%v", name, got, want)
	}
}

// Check compares a record batch read back with tbl, column by column by
// name, so a read that answers $table, $timestamp and the columns and one
// that answers a requested subset are checked alike.
func Check(t T, tbl Table, rec arrow.RecordBatch) {
	t.Helper()
	for i, f := range rec.Schema().Fields() {
		CheckColumn(t, tbl, f.Name, rec.Column(i))
	}
}

// WithTable is tbl's batch as an ingest body carries it: a $table column
// of the name in front. Released on t's cleanup.
func WithTable(t T, tbl Table) arrow.RecordBatch {
	t.Helper()
	rec := batchOf(tbl)
	defer rec.Release()
	table := tableColumn(tbl, tbl.Rows())
	defer table.Release()
	fields := append([]arrow.Field{{Name: "$table", Type: qdbapi.TsColumnString.ArrowType()}}, rec.Schema().Fields()...)
	cols := append([]arrow.Array{table}, rec.Columns()...)
	out := array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, rec.NumRows())
	t.Cleanup(out.Release)
	return out
}

// Body joins the tables into the one batch an ingest body carries: every
// table's WithTable batch, concatenated. The tables must share a column
// list. The batch is released on t's cleanup; an encoder run over it
// writes a body of that format.
func Body(t T, tables ...Table) arrow.RecordBatch {
	t.Helper()
	// The tables become one batch so that a body of any format is one run
	// of its encoder, with no header to strip and no lines to join:
	//
	//  1. build each table's WithTable batch; the batches share a schema
	//     because one body is one column list;
	//  2. concatenate the batches column by column;
	//  3. assemble the result under the first batch's schema and release
	//     it on cleanup.

	// 1. one batch per table
	parts := make([]arrow.RecordBatch, len(tables))
	for i, tbl := range tables {
		parts[i] = WithTable(t, tbl)
	}

	// 2. concatenate column by column
	schema := parts[0].Schema()
	cols := make([]arrow.Array, schema.NumFields())
	rows := int64(0)
	for i := range cols {
		chunks := make([]arrow.Array, len(parts))
		for j, p := range parts {
			chunks[j] = p.Column(i)
		}
		var err error
		if cols[i], err = array.Concatenate(chunks, memory.DefaultAllocator); err != nil {
			t.Fatalf("concatenate %s: %v", schema.Field(i).Name, err)
		}
		defer cols[i].Release()
	}
	for _, p := range parts {
		rows += p.NumRows()
	}

	// 3. the result, under the first batch's schema
	out := array.NewRecordBatch(schema, cols, rows)
	t.Cleanup(out.Release)
	return out
}

// columnInfos is tbl's schema as the create call takes it.
func columnInfos(tbl Table) []qdbapi.TsColumnInfo {
	infos := make([]qdbapi.TsColumnInfo, len(tbl.Columns))
	for i, c := range tbl.Columns {
		if c.Type == qdbapi.TsColumnSymbol {
			infos[i] = qdbapi.NewSymbolColumnInfo(c.Name, c.Symtable)
		} else {
			infos[i] = qdbapi.NewTsColumnInfo(c.Name, c.Type)
		}
	}
	return infos
}

// entries is every entry tbl can leave behind: the table and its
// symtables. A symtable exists only once a symbol value has been pushed
// into it, so removing one that was never filled answers alias-not-found,
// which is the state the cleanup wants.
func entries(tbl Table) []string {
	names := []string{tbl.Name}
	for _, c := range tbl.Columns {
		if c.Symtable != "" {
			names = append(names, c.Symtable)
		}
	}
	return names
}

// call runs f as the fixture's caller (qdbtest.Caller).
func call(c *qdb.Cluster, f func(*qdb.Session) error) error {
	name, secret := qdbtest.Caller()
	return c.Call(context.Background(), qdb.User{Username: name, SecretKey: secret}, f)
}

// RemoveOnCleanup removes tbl and its symtables on t's cleanup, however
// the table came to exist, tolerating what is already gone. A failed
// removal is reported, never fatal, so a leak is visible.
func RemoveOnCleanup(t T, c *qdb.Cluster, tbl Table) {
	t.Cleanup(func() {
		for _, name := range entries(tbl) {
			err := call(c, func(s *qdb.Session) error { return s.RemoveTable(name) })
			if err != nil && !errors.Is(err, qdbapi.ErrAliasNotFound) {
				t.Errorf("remove %s: %v", name, err)
			}
		}
	})
}

// Create creates tbl in the cluster, pushes its batch through the Arrow
// writer, and removes it on t's cleanup. The cleanup is registered as
// soon as the table exists, so a failed push leaves nothing behind.
func Create(t T, c *qdb.Cluster, tbl Table) {
	t.Helper()
	// The table exists before anything can fail, and its removal is
	// registered before anything is pushed:
	//
	//  1. create the table;
	//  2. register the removal, so a failed push leaves nothing behind;
	//  3. no rows: return, so no session is leased for a push the writer
	//     would skip anyway;
	//  4. stage the batch in one fast-push writer and push through one
	//     session.

	// 1. create
	err := call(c, func(s *qdb.Session) error {
		return s.CreateTable(tbl.Name, 24*time.Hour, columnInfos(tbl)...)
	})
	if err != nil {
		t.Fatalf("create %s: %v", tbl.Name, err)
	}

	// 2. the removal, before any push
	RemoveOnCleanup(t, c, tbl)

	// 3. nothing to push: the writer skips a table of no rows, so no
	// session is leased for it
	if tbl.Rows() == 0 {
		return
	}

	// 4. one writer, one push
	w := qdbapi.NewArrowWriter(qdbapi.NewWriterOptions().WithFastPush())
	if err := w.SetTable(tbl.Name, tbl.Batch); err != nil {
		t.Fatalf("writer for %s: %v", tbl.Name, err)
	}
	if err := call(c, func(s *qdb.Session) error { return s.PushArrow(&w) }); err != nil {
		t.Fatalf("push %s: %v", tbl.Name, err)
	}
}
