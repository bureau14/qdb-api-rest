// The encoders share one fixture: a cluster bound to the live qdbd, a
// query run as the anonymous user, and the comparison of a decoded Arrow
// column with the generated table (internal/qdbtest/table) that was
// written. Each format's own decoder lives with that format's test.
package encoding

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	qdbapi "github.com/bureau14/qdb-api-go/v3"

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

// encode runs e over rec and returns the body.
func encode(t failer, e Encoder, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.Encode(context.Background(), &buf, rec); err != nil {
		t.Fatalf("%s: %v", e.ContentType(), err)
	}
	return buf.Bytes()
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
		checkValues(t, want.Name, want.Valid, nanosOf(want.Data), nanos, same[int64])
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

// nanosOf is a timestamp column's cells as nanoseconds since the epoch.
func nanosOf(data qdbapi.ColumnData) []int64 {
	var nanos []int64
	for _, ts := range qdbapi.GetColumnDataTimestampUnsafe(data) {
		nanos = append(nanos, ts.UnixNano())
	}
	return nanos
}

// columns is tbl as its select answers it: $timestamp first, every slot
// valid, then the columns in order.
func columns(tbl table.Table) []table.Column {
	index := qdbapi.NewColumnDataTimestamp(tbl.Index)
	valid := make([]bool, len(tbl.Index))
	for i := range valid {
		valid[i] = true
	}
	return append([]table.Column{{Name: "$timestamp", Type: qdbapi.TsColumnTimestamp, Data: &index, Valid: valid}}, tbl.Columns...)
}
