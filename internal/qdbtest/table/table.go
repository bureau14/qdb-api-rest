// Package table is the generated table fixture, in building blocks that
// stack: GenerateSchema draws a table without rows, Generate draws rows
// into one, RemoveOnCleanup removes whatever a table leaves behind, and
// Create creates the table, pushes its rows and removes it on the test's
// cleanup. Check and CheckColumn compare what a read answers with the
// table written. A test that creates or pushes through its own door (an HTTP
// route) takes the blocks below Create; every test compares what came
// back with the Table it holds. Rules: internal/AGENTS.md, Tests.
package table

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// T is the slice of testing.TB the fixture needs; *testing.T and *rapid.T
// both fit.
type T interface {
	Helper()
	Fatalf(string, ...any)
	Errorf(string, ...any)
	Cleanup(func())
}

// Column is one generated column: its cells as the writer's own
// ColumnData, with the type's null sentinel in every null slot, and the
// validity mask that says which slots hold a value. A schema's column
// has neither.
type Column struct {
	Name     string
	Type     qdbapi.TsColumnType
	Symtable string // symbol columns only
	Data     qdbapi.ColumnData
	Valid    []bool
}

// Table is a generated table: its schema, its $timestamp index and its
// rows, row i being Index[i] and Columns[j].Data cell i.
type Table struct {
	Name    string
	Columns []Column
	Index   []time.Time
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

// generateIndex draws a strictly ascending $timestamp index of n rows: a
// drawn step from a fixed start, so no two rows collide.
func generateIndex(rt *rapid.T, n int) []time.Time {
	step := time.Duration(rapid.Int64Range(1, int64(time.Hour)).Draw(rt, "index step"))
	idx := make([]time.Time, n)
	for i := range idx {
		idx[i] = indexStart.Add(time.Duration(i) * step)
	}
	return idx
}

// generateMask draws the validity mask of n cells at nullPct percent
// nulls.
func generateMask(rt *rapid.T, n, nullPct int) []bool {
	valid := make([]bool, n)
	for i := range valid {
		valid[i] = rapid.IntRange(0, 99).Draw(rt, "null") >= nullPct
	}
	return valid
}

// cells draws one value per valid slot and puts null in the others.
func cells[V any](valid []bool, draw func() V, null V) []V {
	xs := make([]V, len(valid))
	for i, ok := range valid {
		if ok {
			xs[i] = draw()
		} else {
			xs[i] = null
		}
	}
	return xs
}

// drawText draws a string or symbol value: the two share one generator,
// since the binding carries both as strings and symbol values become
// symtable entries the server rejects arbitrary bytes in. The empty string
// is the writer's null sentinel, so a value is never empty.
func drawText(rt *rapid.T) string {
	return rapid.StringMatching(`[a-zA-Z0-9]{1,16}`).Draw(rt, "text")
}

// drawTime draws a timestamp value in the range the result set accepts.
func drawTime(rt *rapid.T) time.Time {
	max := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	return time.Unix(0, rapid.Int64Range(0, max).Draw(rt, "nanos")).UTC()
}

// generateData draws the cells of one column of type kind under valid,
// with the writer's null sentinel in every null slot: MinInt64, NaN, the
// empty string, the nil blob and NullTime.
func generateData(rt *rapid.T, kind qdbapi.TsColumnType, valid []bool) qdbapi.ColumnData {
	switch kind {
	case qdbapi.TsColumnInt64:
		draw := func() int64 { return rapid.Int64Range(math.MinInt64+1, math.MaxInt64).Draw(rt, "int64") }
		cd := qdbapi.NewColumnDataInt64(cells(valid, draw, math.MinInt64))
		return &cd
	case qdbapi.TsColumnDouble:
		draw := func() float64 { return rapid.Float64().Draw(rt, "double") }
		cd := qdbapi.NewColumnDataDouble(cells(valid, draw, math.NaN()))
		return &cd
	case qdbapi.TsColumnString, qdbapi.TsColumnSymbol:
		cd := qdbapi.NewColumnDataString(cells(valid, func() string { return drawText(rt) }, ""))
		return &cd
	case qdbapi.TsColumnBlob:
		draw := func() []byte { return rapid.SliceOfN(rapid.Byte(), 1, 64).Draw(rt, "blob") }
		cd := qdbapi.NewColumnDataBlob(cells(valid, draw, nil))
		return &cd
	case qdbapi.TsColumnTimestamp:
		cd := qdbapi.NewColumnDataTimestamp(cells(valid, func() time.Time { return drawTime(rt) }, qdbapi.NullTime()))
		return &cd
	default:
		panic(fmt.Sprintf("column type %v", kind))
	}
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

// Generate draws a table: a schema, a row count, and one null density for
// the whole table so that runs range from no nulls to all-null columns.
func Generate(rt *rapid.T) Table {
	tbl := GenerateSchema(rt)
	rows := rapid.IntRange(0, 40).Draw(rt, "rows")
	nullPct := rapid.IntRange(0, 100).Draw(rt, "null pct")
	for i := range tbl.Columns {
		c := &tbl.Columns[i]
		c.Valid = generateMask(rt, rows, nullPct)
		c.Data = generateData(rt, c.Type, c.Valid)
	}
	tbl.Index = generateIndex(rt, rows)
	return tbl
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

// allValid is n valid slots.
func allValid(n int) []bool {
	valid := make([]bool, n)
	for i := range valid {
		valid[i] = true
	}
	return valid
}

// Columns is tbl as its Select answers it: $timestamp first, every slot
// valid, then the columns in order.
func Columns(tbl Table) []Column {
	index := qdbapi.NewColumnDataTimestamp(tbl.Index)
	return append([]Column{{Name: "$timestamp", Type: qdbapi.TsColumnTimestamp, Data: &index, Valid: allValid(len(tbl.Index))}}, tbl.Columns...)
}

// ColumnOf is the column a read of tbl answers under name: $table is the
// name in every row, $timestamp the index, anything else tbl's column of
// that name; false when tbl has none.
func ColumnOf(tbl Table, name string) (Column, bool) {
	if name == "$table" {
		names := make([]string, len(tbl.Index))
		for i := range names {
			names[i] = tbl.Name
		}
		data := qdbapi.NewColumnDataString(names)
		return Column{Name: name, Type: qdbapi.TsColumnString, Data: &data, Valid: allValid(len(tbl.Index))}, true
	}
	for _, c := range Columns(tbl) {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

// timestampLayout is the text every rendered format writes a timestamp
// in, RFC 3339 in UTC with nine fixed fractional digits
// (internal/encoding); the ingest parses it back.
const timestampLayout = "2006-01-02T15:04:05.000000000Z"

// csvCell is column c's cell i as the CSV encoder renders it: the empty
// field for null, an integer and a shortest round-trip float as text, a
// timestamp in timestampLayout, a string as itself, a blob as base64.
func csvCell(c Column, i int) string {
	if !c.Valid[i] {
		return ""
	}
	switch c.Type {
	case qdbapi.TsColumnInt64:
		return strconv.FormatInt(qdbapi.GetColumnDataInt64Unsafe(c.Data)[i], 10)
	case qdbapi.TsColumnDouble:
		return strconv.FormatFloat(qdbapi.GetColumnDataDoubleUnsafe(c.Data)[i], 'g', -1, 64)
	case qdbapi.TsColumnTimestamp:
		return qdbapi.GetColumnDataTimestampUnsafe(c.Data)[i].UTC().Format(timestampLayout)
	case qdbapi.TsColumnString, qdbapi.TsColumnSymbol:
		return qdbapi.GetColumnDataStringUnsafe(c.Data)[i]
	case qdbapi.TsColumnBlob:
		return base64.StdEncoding.EncodeToString(qdbapi.GetColumnDataBlobUnsafe(c.Data)[i])
	}
	panic(fmt.Sprintf("column type %v", c.Type))
}

// CSV renders tbl's rows in the CSV encoder's dialect (encoding/csv RFC
// 4180, a header row, LF): $table, $timestamp, then the columns in order,
// one row per index entry. It is what an ingest body of tbl looks like,
// and what the reader answers for it.
func CSV(tbl Table) []byte {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	cols := Columns(tbl)
	names := []string{"$table"}
	for _, c := range cols {
		names = append(names, c.Name)
	}
	_ = w.Write(names)
	record := make([]string, len(names))
	for i := range tbl.Index {
		record[0] = tbl.Name
		for j, c := range cols {
			record[j+1] = csvCell(c, i)
		}
		_ = w.Write(record)
	}
	w.Flush()
	return buf.Bytes()
}

// typed asserts the Arrow array's concrete type.
func typed[A arrow.Array](t T, name string, got arrow.Array) A {
	t.Helper()
	a, ok := got.(A)
	if !ok {
		t.Fatalf("%s: %T read back, want %T", name, got, a)
	}
	return a
}

// checkValues compares every valid slot of a column read back with the
// value that was written.
func checkValues[V any](t T, name string, valid []bool, want []V, got func(int) V, equal func(V, V) bool) {
	t.Helper()
	for i, ok := range valid {
		if ok && !equal(got(i), want[i]) {
			t.Fatalf("%s row %d: %v read back, %v written", name, i, got(i), want[i])
		}
	}
}

func same[V comparable](a, b V) bool { return a == b }

// nanos is a timestamp column's cells as nanoseconds since the epoch.
func nanos(data qdbapi.ColumnData) []int64 {
	var ns []int64
	for _, ts := range qdbapi.GetColumnDataTimestampUnsafe(data) {
		ns = append(ns, ts.UnixNano())
	}
	return ns
}

// CheckColumn compares one Arrow column read back with the column that
// was written: the Arrow type of the table type, every validity bit,
// every value. Null slots are compared by validity only: their value
// bytes carry no meaning on either side. A column null in every row keeps
// its table type, so it takes the same path.
func CheckColumn(t T, want Column, got arrow.Array) {
	t.Helper()
	if got.Len() != len(want.Valid) {
		t.Fatalf("%s: %d rows read back, %d written", want.Name, got.Len(), len(want.Valid))
	}
	for i, valid := range want.Valid {
		if got.IsValid(i) != valid {
			t.Fatalf("%s row %d: valid %v read back, %v written", want.Name, i, got.IsValid(i), valid)
		}
	}
	switch want.Type {
	case qdbapi.TsColumnInt64:
		a := typed[*array.Int64](t, want.Name, got)
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataInt64Unsafe(want.Data), a.Value, same[int64])
	case qdbapi.TsColumnDouble:
		a := typed[*array.Float64](t, want.Name, got)
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataDoubleUnsafe(want.Data), a.Value, same[float64])
	case qdbapi.TsColumnTimestamp:
		a := typed[*array.Timestamp](t, want.Name, got)
		if dt := a.DataType().(*arrow.TimestampType); dt.Unit != arrow.Nanosecond || dt.TimeZone != "" {
			t.Fatalf("%s: type %s read back", want.Name, a.DataType())
		}
		checkValues(t, want.Name, want.Valid, nanos(want.Data), func(i int) int64 { return int64(a.Value(i)) }, same[int64])
	case qdbapi.TsColumnString, qdbapi.TsColumnSymbol:
		a := typed[*array.String](t, want.Name, got)
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataStringUnsafe(want.Data), a.Value, same[string])
	case qdbapi.TsColumnBlob:
		a := typed[*array.Binary](t, want.Name, got)
		checkValues(t, want.Name, want.Valid, qdbapi.GetColumnDataBlobUnsafe(want.Data), a.Value, bytes.Equal)
	default:
		t.Fatalf("%s: unexpected column type %v", want.Name, want.Type)
	}
}

// Check compares a record batch read back with tbl, column by column by
// name, so a read that answers $table, $timestamp and the columns and one
// that answers a requested subset are checked alike.
func Check(t T, tbl Table, rec arrow.RecordBatch) {
	t.Helper()
	for i, f := range rec.Schema().Fields() {
		want, ok := ColumnOf(tbl, f.Name)
		if !ok {
			t.Fatalf("column %q read back, never written", f.Name)
		}
		CheckColumn(t, want, rec.Column(i))
	}
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

// writerOf builds the one-table writer that pushes tbl: fast push, no
// deduplication.
func writerOf(tbl Table) (*qdbapi.Writer, error) {
	cols := make([]qdbapi.WriterColumn, len(tbl.Columns))
	for i, c := range tbl.Columns {
		cols[i] = qdbapi.WriterColumn{ColumnName: c.Name, ColumnType: c.Type}
	}
	wt, err := qdbapi.NewWriterTable(tbl.Name, cols)
	if err != nil {
		return nil, err
	}
	if err := wt.SetIndex(tbl.Index); err != nil {
		return nil, err
	}
	for i, c := range tbl.Columns {
		if err := wt.SetData(i, c.Data); err != nil {
			return nil, err
		}
	}
	w := qdbapi.NewWriter(qdbapi.NewWriterOptions().WithFastPush())
	if err := w.SetTable(wt); err != nil {
		return nil, err
	}
	return &w, nil
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

// call runs f as the anonymous user.
func call(c *qdb.Cluster, f func(*qdb.Session) error) error {
	return c.Call(context.Background(), qdb.User{}, f)
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

// Create creates tbl in the cluster, pushes its rows, and removes it on
// t's cleanup. The cleanup is registered as soon as the table exists, so
// a failed push leaves nothing behind.
func Create(t T, c *qdb.Cluster, tbl Table) {
	t.Helper()
	err := call(c, func(s *qdb.Session) error {
		return s.CreateTable(tbl.Name, 24*time.Hour, columnInfos(tbl)...)
	})
	if err != nil {
		t.Fatalf("create %s: %v", tbl.Name, err)
	}
	RemoveOnCleanup(t, c, tbl)
	if len(tbl.Index) == 0 {
		return // nothing to push
	}
	w, err := writerOf(tbl)
	if err != nil {
		t.Fatalf("writer for %s: %v", tbl.Name, err)
	}
	if err := call(c, func(s *qdb.Session) error { return s.Push(w) }); err != nil {
		t.Fatalf("push %s: %v", tbl.Name, err)
	}
}
