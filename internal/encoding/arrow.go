package encoding

import (
	"context"
	"fmt"
	"io"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/bureau14/qdb-api-rest/internal/model"
)

// ArrowContentType is the media type of the Arrow IPC streaming format.
const ArrowContentType = "application/vnd.apache.arrow.stream"

// Arrow encodes record batches as an Arrow IPC stream: the schema, each
// batch as one record batch, the end-of-stream marker.
type Arrow struct{}

// ContentType implements Encoder.
func (Arrow) ContentType() string { return ArrowContentType }

// EncodeStream implements Encoder. The stream opens on the first batch's
// schema and carries every batch as one record batch, so the producer's
// batch size is the wire's.
func (Arrow) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	// A lazy open and two ways to end:
	//
	//  1. open the writer on the first batch;
	//  2. write each batch whole, with a ctx check per batch;
	//  3. Close writes the end-of-stream marker.
	var ipcw *ipc.Writer
	for rec, err := range batches {
		if err != nil {
			return err
		}
		// 1. an IPC stream carries one schema, and it is known only when
		//    the first batch arrives, so the writer opens here
		if ipcw == nil {
			ipcw = ipc.NewWriter(w, ipc.WithSchema(rec.Schema()))
		}
		// 2. a client that left is noticed at the batch boundary, and the
		//    batch goes out whole, because the producer already bounds a
		//    batch at model.BatchRows rows (Cluster.Query and the bulk
		//    reader) or one vector (DuckDB). Slicing here was rejected: the
		//    slice is the query core's now, and the text encoders never
		//    sliced. A batch of no rows is not written, because the schema
		//    message already carries everything it would, so a result of no
		//    rows is the schema and the marker alone.
		if err := ctx.Err(); err != nil {
			return err
		}
		if rec.NumRows() == 0 {
			continue
		}
		if err := ipcw.Write(rec); err != nil {
			return err
		}
	}
	// 3. Close writes the end-of-stream marker. A sequence of no batch
	//    cannot occur, because every producer yields at least one (the
	//    Encoder contract); if one did, the writer would be unopened, so
	//    it is opened on a schema with no fields rather than left nil.
	if ipcw == nil {
		ipcw = ipc.NewWriter(w, ipc.WithSchema(arrow.NewSchema(nil, nil)))
	}
	return ipcw.Close()
}

// arrowTable accumulates the slices of one table across the batches of a
// stream: the batch's schema in the reader's fields, the body column of
// each field, and the slices kept so far.
type arrowTable struct {
	name   string
	schema *arrow.Schema
	fields []int               // the body column of each schema field
	slices []arrow.RecordBatch // retained slices of the stream's batches
}

// newArrowTable types the body's columns by the table's schema:
// $timestamp first, then the body's data columns in their order. A name
// the table lacks, and a body column whose type differs from the
// reader's, are ErrInvalidRows. The tables of one body agree in type
// without a check between them, because every table is checked against
// the same body columns. The error of schemaOf passes through as is.
func newArrowTable(name string, body *arrow.Schema, schemaOf model.SchemaOf) (*arrowTable, error) {
	// The batch carries the reader's types, so its fields are picked from
	// the reader's schema, with the type check turned on the body instead
	// of on the previous table:
	//
	//  1. look the table up through schemaOf, and pass its error as is;
	//  2. pick $timestamp and the body's data columns from the reader's
	//     schema by name; a name the table lacks is the body's fault;
	//  3. check that the body column's type equals the reader's, which
	//     arrow.TypeEqual decides without the reader's max_width metadata;
	//  4. the batch's schema is the reader's fields in that order.

	// 1. the table's schema
	schema, err := schemaOf(name)
	if err != nil {
		return nil, err
	}

	// 2. the fields, by name: $timestamp, then the body's data columns.
	// checkArrowHeader has proven $timestamp present in the body.
	bodyFields := []int{body.FieldIndices("$timestamp")[0]}
	for i, f := range body.Fields() {
		if f.Name != "$table" && f.Name != "$timestamp" {
			bodyFields = append(bodyFields, i)
		}
	}
	fields := make([]arrow.Field, len(bodyFields))
	for k, i := range bodyFields {
		f := body.Field(i)
		idx := schema.FieldIndices(f.Name)
		if idx == nil {
			return nil, fmt.Errorf("%w: table %s has no column %s", ErrInvalidRows, name, f.Name)
		}
		// 3. the body's type is the reader's type. A cast (timestamp[us]
		// into [ns], large_utf8 into utf8) was rejected: the binding refuses
		// what the cast would take, and a conversion layer is more code
		// than the rule.
		if want := schema.Field(idx[0]); !arrow.TypeEqual(f.Type, want.Type) {
			return nil, fmt.Errorf("%w: column %s has type %s, the table's is %s", ErrInvalidRows, f.Name, f.Type, want.Type)
		}
		fields[k] = schema.Field(idx[0])
	}

	// 4. the reader's fields
	return &arrowTable{name: name, schema: arrow.NewSchema(fields, nil), fields: bodyFields}, nil
}

// add keeps the rows [i, j) of rec for this table, as a slice that
// shares rec's buffers and stays valid past the reader's next batch.
func (t *arrowTable) add(rec arrow.RecordBatch, i, j int64) {
	t.slices = append(t.slices, rec.NewSlice(i, j))
}

// batch concatenates the slices into one record batch under the reader's
// fields and releases the slices.
func (t *arrowTable) batch() (model.TableBatch, error) {
	defer t.release()
	cols := make([]arrow.Array, len(t.fields))
	rows := int64(0)
	for k, i := range t.fields {
		chunks := make([]arrow.Array, len(t.slices))
		for n, s := range t.slices {
			chunks[n] = s.Column(i)
		}
		// The copy is accepted: the seam hands the push one batch per table,
		// and the binding's several-batches form is not what model.TableBatch
		// carries.
		col, err := array.Concatenate(chunks, memory.DefaultAllocator)
		if err != nil {
			for _, c := range cols[:k] {
				c.Release()
			}
			return model.TableBatch{}, err
		}
		cols[k] = col
		rows = int64(col.Len())
	}
	rec := array.NewRecordBatch(t.schema, cols, rows)
	for _, c := range cols {
		c.Release()
	}
	return model.TableBatch{Table: t.name, Batch: rec}, nil
}

func (t *arrowTable) release() {
	for _, s := range t.slices {
		s.Release()
	}
	t.slices = nil
}

// splitRuns calls f once per run of equal $table values in rec, with the
// table name and the row range. A null or empty $table is ErrInvalidRows
// naming the row, counted from offset, the rows of the batches before
// rec.
func splitRuns(rec arrow.RecordBatch, table int, offset int64, f func(name string, i, j int64) error) error {
	col := rec.Column(table).(*array.String)
	for i := 0; i < col.Len(); {
		if col.IsNull(i) || col.Value(i) == "" {
			return fmt.Errorf("%w: row %d: no $table", ErrInvalidRows, offset+int64(i)+1)
		}
		// Rows of one table are usually contiguous, so a run is one slice
		// and the common body costs one slice per table per batch. An
		// interleaved body costs one slice per row and decodes all the same.
		// Per-table builders fed row by row were rejected: a copy per cell
		// where a slice copies nothing.
		name := col.Value(i)
		j := i + 1
		for j < col.Len() && !col.IsNull(j) && col.Value(j) == name {
			j++
		}
		if err := f(name, int64(i), int64(j)); err != nil {
			return err
		}
		i = j
	}
	return nil
}

// arrowHeader is where the two special columns sit in the stream's
// schema.
type arrowHeader struct {
	table, timestamp int
}

// checkArrowHeader checks the stream's schema: $table is a utf8 column
// and $timestamp is present. A dictionary-encoded $table is refused,
// because no client sends one yet and the rule is one line to lift.
func checkArrowHeader(schema *arrow.Schema) (arrowHeader, error) {
	table, timestamp := schema.FieldIndices("$table"), schema.FieldIndices("$timestamp")
	switch {
	case table == nil:
		return arrowHeader{}, fmt.Errorf("%w: schema names no $table", ErrInvalidRows)
	case !arrow.TypeEqual(schema.Field(table[0]).Type, arrow.BinaryTypes.String):
		return arrowHeader{}, fmt.Errorf("%w: $table has type %s, not utf8", ErrInvalidRows, schema.Field(table[0]).Type)
	case timestamp == nil:
		return arrowHeader{}, fmt.Errorf("%w: schema names no $timestamp", ErrInvalidRows)
	}
	return arrowHeader{table: table[0], timestamp: timestamp[0]}, nil
}

// checkIndex refuses a null $timestamp in rec, naming the row counted
// from offset, because the index cannot be null and the binding would
// only say so at the push. The null count is kept by the array, so a
// batch without nulls costs one read.
func checkIndex(rec arrow.RecordBatch, timestamp int, offset int64) error {
	col := rec.Column(timestamp)
	if col.NullN() == 0 {
		return nil
	}
	for i := range col.Len() {
		if col.IsNull(i) {
			return fmt.Errorf("%w: row %d: null $timestamp", ErrInvalidRows, offset+int64(i)+1)
		}
	}
	return nil
}

// Decode implements Decoder. The body is an Arrow IPC stream in this
// encoder's dialect: one schema naming $table, $timestamp and the data
// columns in any order, in the reader's types, and any number of record
// batches.
func (Arrow) Decode(ctx context.Context, r io.Reader, schemaOf model.SchemaOf) ([]model.TableBatch, error) {
	// The stream is read batch by batch and each batch is sliced into
	// runs of one table, so a body of any batch count decodes with one
	// copy per table, the three steps of CSV.Decode over batches:
	//
	//  1. open the reader, which reads the schema; a body that is not a
	//     stream, or whose schema names no $table or $timestamp, is the
	//     body's fault;
	//  2. each batch is checked for a null index and split into runs; a
	//     table seen for the first time
	//     is typed through schemaOf against the body's schema, and every
	//     run is kept as a retained slice, because the reader releases
	//     the batch when it reads the next;
	//  3. at the end every table becomes one batch, in first-seen order.
	tables := map[string]*arrowTable{}
	var order []*arrowTable
	release := func() {
		for _, t := range order {
			t.release()
		}
	}

	// 1. the reader and the schema
	rd, err := ipc.NewReader(r)
	if err != nil {
		// Both error chains stay reachable. The sentinel decides the status,
		// and the reader's cause reveals a body-size cap the HTTP layer set.
		return nil, fmt.Errorf("%w: stream: %w", ErrInvalidRows, err)
	}
	defer rd.Release()
	h, err := checkArrowHeader(rd.Schema())
	if err != nil {
		return nil, err
	}

	// 2. the batches, each split into runs
	rows := int64(0)
	for rd.Next() {
		// A client that left is noticed at the batch boundary, as the
		// encoder notices it.
		if err := ctx.Err(); err != nil {
			release()
			return nil, err
		}
		rec := rd.RecordBatch()
		if err := checkIndex(rec, h.timestamp, rows); err != nil {
			release()
			return nil, err
		}
		// The callback's error is a fault of the body or the error of
		// schemaOf, each already in the form the caller classifies.
		err := splitRuns(rec, h.table, rows, func(name string, i, j int64) error {
			t, ok := tables[name]
			if !ok {
				var err error
				if t, err = newArrowTable(name, rd.Schema(), schemaOf); err != nil {
					return err
				}
				tables[name] = t
				order = append(order, t)
			}
			t.add(rec, i, j)
			return nil
		})
		if err != nil {
			release()
			return nil, err
		}
		rows += rec.NumRows()
	}
	if err := rd.Err(); err != nil {
		release()
		return nil, fmt.Errorf("%w: batch at row %d: %w", ErrInvalidRows, rows+1, err)
	}

	// 3. one batch per table
	out := make([]model.TableBatch, 0, len(order))
	for _, t := range order {
		b, err := t.batch()
		if err != nil {
			for _, o := range out {
				o.Batch.Release()
			}
			release()
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}
