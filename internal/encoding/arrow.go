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
// batch in slices, the end-of-stream marker.
type Arrow struct{}

// ContentType implements Encoder.
func (Arrow) ContentType() string { return ArrowContentType }

// emptyBatch returns the batch of a statement without a result set. It
// has no fields and no rows, so its stream is a schema and the
// end-of-stream marker.
func emptyBatch() arrow.RecordBatch {
	return array.NewRecordBatch(arrow.NewSchema(nil, nil), nil, 0)
}

// writeSlices writes rec to ipcw as one record batch per batchRows rows.
// A slice shares rec's buffers.
func writeSlices(ctx context.Context, ipcw *ipc.Writer, rec arrow.RecordBatch, batchRows int64) error {
	for start := int64(0); start < rec.NumRows(); start += batchRows {
		// A client that left is noticed at the batch boundary.
		if err := ctx.Err(); err != nil {
			return err
		}
		batch := rec.NewSlice(start, min(start+batchRows, rec.NumRows()))
		err := ipcw.Write(batch)
		batch.Release()
		if err != nil {
			return err
		}
	}
	return nil
}

// writeArrow writes rec to w: the schema as rec carries it, one slice of
// batchRows rows per record batch, the end-of-stream marker.
func writeArrow(ctx context.Context, w io.Writer, rec arrow.RecordBatch, batchRows int64) error {
	if rec == nil {
		rec = emptyBatch()
		defer rec.Release()
	}
	ipcw := ipc.NewWriter(w, ipc.WithSchema(rec.Schema()))
	if err := writeSlices(ctx, ipcw, rec, batchRows); err != nil {
		return err
	}
	// Close writes the end-of-stream marker, which completes the stream
	// even with no batches.
	return ipcw.Close()
}

// Encode implements Encoder.
func (Arrow) Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error {
	return writeArrow(ctx, w, rec, chunkRows)
}

// EncodeStream implements Encoder.
func (Arrow) EncodeStream(ctx context.Context, w io.Writer, batches iter.Seq2[arrow.RecordBatch, error]) error {
	// An IPC stream carries one schema, and the schema is only known once
	// the first batch arrives, so the writer opens lazily. Every batch is
	// then written in chunkRows slices like the one-shot path.
	var ipcw *ipc.Writer
	for rec, err := range batches {
		if err != nil {
			return err
		}
		if ipcw == nil {
			ipcw = ipc.NewWriter(w, ipc.WithSchema(rec.Schema()))
		}
		if err := writeSlices(ctx, ipcw, rec, chunkRows); err != nil {
			return err
		}
	}
	// A sequence without any batch must still be a complete stream, so it
	// is written as the nil-batch encoding: a schema with no fields and
	// the marker.
	if ipcw == nil {
		return writeArrow(ctx, w, nil, chunkRows)
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
// table name and the row range. A null $table is ErrInvalidRows naming
// the row, counted from offset, the rows of the batches before rec.
func splitRuns(rec arrow.RecordBatch, table int, offset int64, f func(name string, i, j int64) error) error {
	col := rec.Column(table).(*array.String)
	for i := 0; i < col.Len(); {
		if col.IsNull(i) {
			return fmt.Errorf("%w: row %d: null $table", ErrInvalidRows, offset+int64(i)+1)
		}
		// Rows of one table are usually contiguous, so a run is one slice
		// and the common body costs one slice per table per batch. An
		// interleaved body costs one slice per row and decodes all the same.
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

// checkArrowHeader checks the stream's schema: $table is a utf8 column
// and $timestamp is present. A dictionary-encoded $table is refused,
// because no client sends one yet and the rule is one line to lift.
func checkArrowHeader(schema *arrow.Schema) (int, error) {
	idx := schema.FieldIndices("$table")
	switch {
	case idx == nil:
		return 0, fmt.Errorf("%w: schema names no $table", ErrInvalidRows)
	case !arrow.TypeEqual(schema.Field(idx[0]).Type, arrow.BinaryTypes.String):
		return 0, fmt.Errorf("%w: $table has type %s, not utf8", ErrInvalidRows, schema.Field(idx[0]).Type)
	case schema.FieldIndices("$timestamp") == nil:
		return 0, fmt.Errorf("%w: schema names no $timestamp", ErrInvalidRows)
	}
	return idx[0], nil
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
	//  2. each batch is split into runs; a table seen for the first time
	//     is typed through schemaOf against the body's schema, and every
	//     run is kept as a retained slice, because the reader releases
	//     the batch on its next step;
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
	table, err := checkArrowHeader(rd.Schema())
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
		// The callback's error is a fault of the body or the error of
		// schemaOf, each already in the form the caller classifies.
		err := splitRuns(rec, table, rows, func(name string, i, j int64) error {
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
