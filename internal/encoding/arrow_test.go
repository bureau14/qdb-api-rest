// The Arrow encoder is pinned by one round trip against the live qdbd
// fixture (internal/qdbtest): generated rows of every column type and null
// density go into a table, come back as a result set, are encoded with a
// batch size small enough that rows span batches, decoded with the IPC
// reader, and compared with the result set value by value. The C API and
// the binding are not under test; the type map, the buffers and the
// batching are.
package encoding

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// failer is the slice of testing.TB the helpers need, so *testing.T and
// *rapid.T both fit.
type failer interface {
	Helper()
	Fatalf(string, ...any)
}

// newCluster binds a cluster to the insecure fixture for the test's life.
func newCluster(t *testing.T) *qdb.Cluster {
	t.Helper()
	qdbtest.Require(t, qdbtest.InsecureURI)
	cfg := config.Default()
	cfg.Cluster.URI = qdbtest.InsecureURI
	c := qdb.New(cfg, nil)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := c.Close(ctx); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return c
}

// run executes q as the anonymous user and fails the test on error.
func run(t failer, c *qdb.Cluster, q string) *qdbapi.QueryResultSet {
	t.Helper()
	rs, err := c.Query(context.Background(), qdb.User{}, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return rs
}

// cell is one generated value of one column, or a null.
type cell struct {
	null    bool
	literal string // the query-language literal of the value
}

// literalOf renders a value the way the query language reads it. Text
// values keep to a charset the language needs no escaping for, so the
// generator never has to know its quoting rules.
func literalOf(rt *rapid.T, kind string) string {
	switch kind {
	case "INT64":
		// MinInt64 is the binding's null sentinel and reads back as null.
		return strconv.FormatInt(rapid.Int64Range(-1<<63+1, 1<<63-1).Draw(rt, "int64"), 10)
	case "DOUBLE":
		return strconv.FormatFloat(rapid.Float64Range(-1e9, 1e9).Draw(rt, "double"), 'f', -1, 64)
	case "TIMESTAMP":
		nanos := rapid.Int64Range(0, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()).Draw(rt, "nanos")
		return time.Unix(0, nanos).UTC().Format("2006-01-02T15:04:05.000000000Z")
	case "STRING", "SYMBOL", "BLOB":
		// Empty is a value, not a null; the mask alone tells them apart.
		return "'" + rapid.StringMatching(`[a-zA-Z0-9 ,.<>&"=:/-]{0,12}`).Draw(rt, kind) + "'"
	default:
		panic("unknown kind " + kind)
	}
}

// columns is the generated table's schema after $timestamp, in order.
var columns = []struct{ name, kind, ddl string }{
	{"i", "INT64", "INT64"},
	{"d", "DOUBLE", "DOUBLE"},
	{"s", "STRING", "STRING"},
	{"y", "SYMBOL", "SYMBOL(encoding_arrow_sym)"},
	{"b", "BLOB", "BLOB"},
	{"t", "TIMESTAMP", "TIMESTAMP"},
}

// rowsGen draws a table's rows: a null density per run, so that runs
// range from no nulls to all-null columns (the Null type on the wire).
func rowsGen(rt *rapid.T) [][]cell {
	n := rapid.IntRange(0, 40).Draw(rt, "rows")
	nullPct := rapid.IntRange(0, 100).Draw(rt, "null pct")
	rows := make([][]cell, n)
	for r := range rows {
		rows[r] = make([]cell, len(columns))
		for i, col := range columns {
			if rapid.IntRange(0, 99).Draw(rt, "null") < nullPct {
				rows[r][i] = cell{null: true}
				continue
			}
			rows[r][i] = cell{literal: literalOf(rt, col.kind)}
		}
	}
	return rows
}

// loadTable recreates the table and inserts rows, one INSERT per row with
// NULL literals, which is what the fixture seed does too.
func loadTable(t failer, c *qdb.Cluster, table string, rows [][]cell) {
	t.Helper()
	_, _ = c.Query(context.Background(), qdb.User{}, "DROP TABLE "+table) // absent on the first run
	var ddl []string
	for _, col := range columns {
		ddl = append(ddl, col.name+" "+col.ddl)
	}
	run(t, c, fmt.Sprintf("CREATE TABLE %s ($timestamp TIMESTAMP, %s)", table, strings.Join(ddl, ", ")))
	var names []string
	for _, col := range columns {
		names = append(names, col.name)
	}
	for r, row := range rows {
		// Distinct $timestamp per row keeps every row, whatever the values.
		values := []string{time.Unix(int64(r), 0).UTC().Format("2006-01-02T15:04:05Z")}
		for _, v := range row {
			if v.null {
				values = append(values, "NULL")
			} else {
				values = append(values, v.literal)
			}
		}
		run(t, c, fmt.Sprintf("INSERT INTO %s ($timestamp, %s) VALUES (%s)", table, strings.Join(names, ", "), strings.Join(values, ", ")))
	}
}

// decode reads every batch of an IPC stream and concatenates it per column,
// returning the schema, the columns and the batch count. A stream with no
// batch has no columns to return.
func decode(t failer, stream []byte) (*arrow.Schema, []arrow.Array, int) {
	t.Helper()
	r, err := ipc.NewReader(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("ipc reader: %v", err)
	}
	defer r.Release()
	perColumn := make([][]arrow.Array, r.Schema().NumFields())
	batches := 0
	for r.Next() {
		batches++
		rec := r.RecordBatch()
		for i, col := range rec.Columns() {
			col.Retain()
			perColumn[i] = append(perColumn[i], col)
		}
	}
	if r.Err() != nil {
		t.Fatalf("ipc read: %v", r.Err())
	}
	if batches == 0 {
		return r.Schema(), nil, 0
	}
	cols := make([]arrow.Array, len(perColumn))
	for i, parts := range perColumn {
		if cols[i], err = array.Concatenate(parts, memory.DefaultAllocator); err != nil {
			t.Fatalf("concatenate column %d: %v", i, err)
		}
	}
	return r.Schema(), cols, batches
}

// checkColumn compares one decoded column with the result column it came
// from: the Arrow type of the type map, every validity bit, every value.
func checkColumn(t failer, want qdbapi.QueryColumn, got arrow.Array) {
	t.Helper()
	if got.Len() != want.Len() {
		t.Fatalf("%s: %d rows on the wire, %d in the result", want.Name(), got.Len(), want.Len())
	}
	// The Null type has no validity bitmap: its type is the statement that
	// every slot is null, and a reader answers IsValid from the type.
	if _, ok := want.(*qdbapi.QueryColumnNull); ok {
		typed[*array.Null](t, want.Name(), got)
		return
	}
	for i := range want.Len() {
		if got.IsValid(i) != want.Valid().IsValid(i) {
			t.Fatalf("%s row %d: valid %v on the wire, %v in the result", want.Name(), i, got.IsValid(i), want.Valid().IsValid(i))
		}
	}
	// Null slots are compared by validity only: their value bytes carry no
	// meaning on either side.
	valid := func(i int) bool { return want.Valid().IsValid(i) }
	switch want := want.(type) {
	case *qdbapi.QueryColumnInt64:
		a := typed[*array.Int64](t, want.Name(), got)
		for i := range want.Values {
			if valid(i) && a.Value(i) != want.Values[i] {
				t.Fatalf("%s row %d: %d != %d", want.Name(), i, a.Value(i), want.Values[i])
			}
		}
	case *qdbapi.QueryColumnDouble:
		a := typed[*array.Float64](t, want.Name(), got)
		for i := range want.Values {
			if valid(i) && a.Value(i) != want.Values[i] {
				t.Fatalf("%s row %d: %g != %g", want.Name(), i, a.Value(i), want.Values[i])
			}
		}
	case *qdbapi.QueryColumnTimestamp:
		a := typed[*array.Timestamp](t, want.Name(), got)
		if !arrow.TypeEqual(a.DataType(), timestampNanosUTC) {
			t.Fatalf("%s: type %s on the wire", want.Name(), a.DataType())
		}
		for i := range want.Values {
			if valid(i) && int64(a.Value(i)) != want.Values[i] {
				t.Fatalf("%s row %d: %d != %d", want.Name(), i, a.Value(i), want.Values[i])
			}
		}
	case *qdbapi.QueryColumnString:
		a := typed[*array.String](t, want.Name(), got)
		for i := range want.Values {
			if valid(i) && a.Value(i) != want.Values[i] {
				t.Fatalf("%s row %d: %q != %q", want.Name(), i, a.Value(i), want.Values[i])
			}
		}
	case *qdbapi.QueryColumnBlob:
		a := typed[*array.Binary](t, want.Name(), got)
		for i := range want.Values {
			if valid(i) && !bytes.Equal(a.Value(i), want.Values[i]) {
				t.Fatalf("%s row %d: %q != %q", want.Name(), i, a.Value(i), want.Values[i])
			}
		}
	default:
		t.Fatalf("%s: unexpected result column %T", want.Name(), want)
	}
}

// typed asserts the decoded column's concrete Arrow array type.
func typed[T arrow.Array](t failer, name string, got arrow.Array) T {
	t.Helper()
	a, ok := got.(T)
	if !ok {
		t.Fatalf("%s: %T on the wire, want %T", name, got, a)
	}
	return a
}

// TestArrowRoundTrip: what the encoder puts on the wire decodes to the
// result set it was given, whatever the types, the nulls and the row
// count, across batch boundaries.
func TestArrowRoundTrip(t *testing.T) {
	c := newCluster(t)
	const table = "encoding_arrow_roundtrip"
	rapid.Check(t, func(rt *rapid.T) {
		rows := rowsGen(rt)
		loadTable(rt, c, table, rows)
		rs := run(rt, c, "SELECT * FROM "+table)

		// A batch size below the row count is what exercises slicing and
		// the offset rebasing of string and blob columns.
		batchRows := int64(rapid.IntRange(1, 16).Draw(rt, "batch rows"))
		var buf bytes.Buffer
		if err := writeArrow(context.Background(), &buf, rs, batchRows); err != nil {
			rt.Fatalf("encode: %v", err)
		}

		schema, cols, batches := decode(rt, buf.Bytes())
		want := Record(rs)
		defer want.Release()
		if !schema.Equal(want.Schema()) {
			rt.Fatalf("schema on the wire %s != %s", schema, want.Schema())
		}
		if wantBatches := int((int64(rs.RowCount()) + batchRows - 1) / batchRows); batches != wantBatches {
			rt.Fatalf("%d batches on the wire, want %d for %d rows of %d", batches, wantBatches, rs.RowCount(), batchRows)
		}
		if batches == 0 {
			return // no rows: the schema and the marker are the whole stream
		}
		for i, col := range rs.Columns() {
			checkColumn(rt, col, cols[i])
			cols[i].Release()
		}
	})
}

// TestArrowNilResultSet: a statement without a result set is a complete
// stream with no fields and no batches.
func TestArrowNilResultSet(t *testing.T) {
	var buf bytes.Buffer
	if err := (Arrow{}).Encode(context.Background(), &buf, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	schema, _, batches := decode(t, buf.Bytes())
	if schema.NumFields() != 0 || batches != 0 {
		t.Fatalf("%d fields and %d batches, want none", schema.NumFields(), batches)
	}
}
