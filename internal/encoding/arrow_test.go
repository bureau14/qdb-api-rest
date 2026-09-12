// The Arrow encoder is pinned by one round trip against the live qdbd
// fixture: a generated table is read back as the binding's record batch,
// encoded with a batch size small enough that rows span batches, decoded
// with the IPC reader, and compared cell by cell with the table that was
// written.
package encoding

import (
	"bytes"
	"context"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"pgregory.net/rapid"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

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

// TestArrowRoundTrip: what the encoder puts on the wire decodes to the
// batch it was given, whatever the types, the nulls and the row
// count, across batch boundaries.
func TestArrowRoundTrip(t *testing.T) {
	c := cluster.New(t)
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
		for i, col := range columns(tbl) {
			checkColumn(rt, col, cols[i])
		}
		for _, col := range cols {
			col.Release()
		}
	})
}

// TestArrowNilBatch: a statement without a result set is a complete
// stream with no fields and no batches.
func TestArrowNilBatch(t *testing.T) {
	schema, _, batches := decode(t, encode(t, Arrow{}, nil))
	if schema.NumFields() != 0 || batches != 0 {
		t.Fatalf("%d fields and %d batches, want none", schema.NumFields(), batches)
	}
}
