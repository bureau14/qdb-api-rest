// The Arrow encoder is pinned by one round trip against the live qdbd
// fixture: a generated table (internal/qdbtest/table) of drawn column
// types and null density is created and pushed, read back as the
// binding's record batch, encoded with a batch size small enough that
// rows span batches, decoded with the IPC reader, and compared cell by
// cell with the table that was written.
package encoding

import (
	"bytes"
	"context"
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
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
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

// run executes q as the anonymous user and fails the test on error. The
// caller releases the batch.
func run(t failer, c *qdb.Cluster, q string) arrow.RecordBatch {
	t.Helper()
	rec, err := c.Query(context.Background(), qdb.User{}, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return rec
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

// typed asserts the decoded column's concrete Arrow array type.
func typed[T arrow.Array](t failer, name string, got arrow.Array) T {
	t.Helper()
	a, ok := got.(T)
	if !ok {
		t.Fatalf("%s: %T on the wire, want %T", name, got, a)
	}
	return a
}

// checkValues compares every valid slot of a decoded column with the
// value that was written.
func checkValues[V any](t failer, name string, valid []bool, want []V, got func(int) V, equal func(V, V) bool) {
	t.Helper()
	for i, ok := range valid {
		if ok && !equal(got(i), want[i]) {
			t.Fatalf("%s row %d: %v on the wire, %v written", name, i, got(i), want[i])
		}
	}
}

func same[V comparable](a, b V) bool { return a == b }

// checkColumn compares one decoded column with the column that was
// written: the Arrow type of the table type, every validity bit, every
// value. Null slots are compared by validity only: their value bytes
// carry no meaning on either side. A column null in every row keeps its
// table type, so it takes the same path.
func checkColumn(t failer, want table.Column, got arrow.Array) {
	t.Helper()
	if got.Len() != len(want.Valid) {
		t.Fatalf("%s: %d rows on the wire, %d written", want.Name, got.Len(), len(want.Valid))
	}
	for i, valid := range want.Valid {
		if got.IsValid(i) != valid {
			t.Fatalf("%s row %d: valid %v on the wire, %v written", want.Name, i, got.IsValid(i), valid)
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
			t.Fatalf("%s: type %s on the wire", want.Name, a.DataType())
		}
		nanos := func(i int) int64 { return int64(a.Value(i)) }
		var want64 []int64
		for _, ts := range qdbapi.GetColumnDataTimestampUnsafe(want.Data) {
			want64 = append(want64, ts.UnixNano())
		}
		checkValues(t, want.Name, want.Valid, want64, nanos, same[int64])
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

// checkIndex compares the decoded $timestamp column with the index that
// was written: nanoseconds since the epoch, every slot valid.
func checkIndex(t failer, want []time.Time, got arrow.Array) {
	t.Helper()
	a := typed[*array.Timestamp](t, "$timestamp", got)
	if a.Len() != len(want) || a.NullN() != 0 {
		t.Fatalf("$timestamp: %d rows and %d nulls on the wire, %d rows written", a.Len(), a.NullN(), len(want))
	}
	for i, ts := range want {
		if int64(a.Value(i)) != ts.UnixNano() {
			t.Fatalf("$timestamp row %d: %d on the wire, %d written", i, a.Value(i), ts.UnixNano())
		}
	}
}

// TestArrowRoundTrip: what the encoder puts on the wire decodes to the
// batch it was given, whatever the types, the nulls and the row
// count, across batch boundaries.
func TestArrowRoundTrip(t *testing.T) {
	c := newCluster(t)
	rapid.Check(t, func(rt *rapid.T) {
		tbl := table.Generate(rt)
		table.Create(rt, c, tbl)
		rec := run(rt, c, tbl.Select())
		defer rec.Release()

		// A batch size below the row count is what exercises slicing and
		// the offset rebasing of string and blob columns.
		batchRows := int64(rapid.IntRange(1, 16).Draw(rt, "batch rows"))
		var buf bytes.Buffer
		if err := writeArrow(context.Background(), &buf, rec, batchRows); err != nil {
			rt.Fatalf("encode: %v", err)
		}

		schema, cols, batches := decode(rt, buf.Bytes())
		if !schema.Equal(rec.Schema()) {
			rt.Fatalf("schema on the wire %s != %s", schema, rec.Schema())
		}
		if wantBatches := int((int64(len(tbl.Index)) + batchRows - 1) / batchRows); batches != wantBatches {
			rt.Fatalf("%d batches on the wire, want %d for %d rows of %d", batches, wantBatches, len(tbl.Index), batchRows)
		}
		if batches == 0 {
			return // no rows: the schema and the marker are the whole stream
		}
		// The select answers $timestamp first, then the columns in order,
		// rows ascending by $timestamp: the index's own order.
		checkIndex(rt, tbl.Index, cols[0])
		for i, col := range tbl.Columns {
			checkColumn(rt, col, cols[i+1])
		}
		for _, col := range cols {
			col.Release()
		}
	})
}

// TestArrowNilBatch: a statement without a result set is a complete
// stream with no fields and no batches.
func TestArrowNilBatch(t *testing.T) {
	var buf bytes.Buffer
	if err := (Arrow{}).Encode(context.Background(), &buf, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	schema, _, batches := decode(t, buf.Bytes())
	if schema.NumFields() != 0 || batches != 0 {
		t.Fatalf("%d fields and %d batches, want none", schema.NumFields(), batches)
	}
}
