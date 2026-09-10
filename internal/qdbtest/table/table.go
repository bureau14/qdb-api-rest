// Package table is the generated table fixture: a test draws a table
// (schema, index, rows) with Generate, creates it in the live qdbd
// fixture with Create, queries it through the cluster, and compares what
// came back with the Table it holds. The table is removed on the test's
// cleanup. Plan: docs/table-fixture-plan.md.
package table

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

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
// validity mask that says which slots hold a value.
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
// empty string and the nil blob. A timestamp column is dense: its null
// timespec is not settable through the writer, so its mask is all true.
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
		cd := qdbapi.NewColumnDataTimestamp(cells(valid, func() time.Time { return drawTime(rt) }, time.Time{}))
		return &cd
	default:
		panic(fmt.Sprintf("column type %v", kind))
	}
}

// generateColumn draws column i of table: its type, its mask and its
// cells.
func generateColumn(rt *rapid.T, table string, i, rows, nullPct int) Column {
	c := Column{Name: fmt.Sprintf("c%d", i), Type: rapid.SampledFrom(columnTypes).Draw(rt, "type")}
	if c.Type == qdbapi.TsColumnSymbol {
		c.Symtable = table + "_" + c.Name
	}
	if c.Type == qdbapi.TsColumnTimestamp {
		c.Valid = generateMask(rt, rows, 0)
	} else {
		c.Valid = generateMask(rt, rows, nullPct)
	}
	c.Data = generateData(rt, c.Type, c.Valid)
	return c
}

// Generate draws a table: a name, one to five columns of independently
// drawn types, a row count, and one null density for the whole table so
// that runs range from no nulls to all-null columns.
func Generate(rt *rapid.T) Table {
	name := "qdbtest_" + rapid.StringMatching(`[a-z]{16}`).Draw(rt, "table")
	rows := rapid.IntRange(0, 40).Draw(rt, "rows")
	nullPct := rapid.IntRange(0, 100).Draw(rt, "null pct")
	cols := make([]Column, rapid.IntRange(1, 5).Draw(rt, "columns"))
	for i := range cols {
		cols[i] = generateColumn(rt, name, i, rows, nullPct)
	}
	return Table{Name: name, Columns: cols, Index: generateIndex(rt, rows)}
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
	wt.SetIndex(tbl.Index)
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

// entries is every entry Create can leave behind: the table and its
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

// Create creates tbl in the cluster, pushes its rows, and removes the
// table and its symtables on t's cleanup. The cleanup is registered as
// soon as the table exists, so a failed push leaves nothing behind; a
// failed removal is reported, never fatal, so a leak is visible.
func Create(t T, c *qdb.Cluster, tbl Table) {
	t.Helper()
	err := call(c, func(s *qdb.Session) error {
		return s.CreateTable(tbl.Name, 24*time.Hour, columnInfos(tbl)...)
	})
	if err != nil {
		t.Fatalf("create %s: %v", tbl.Name, err)
	}
	t.Cleanup(func() {
		for _, name := range entries(tbl) {
			err := call(c, func(s *qdb.Session) error { return s.RemoveTable(name) })
			if err != nil && !errors.Is(err, qdbapi.ErrAliasNotFound) {
				t.Errorf("remove %s: %v", name, err)
			}
		}
	})
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
