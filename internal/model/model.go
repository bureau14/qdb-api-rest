// Package model is the server's neutral representation of table data.
// It is Arrow by indirection: the types the layers hand each other,
// which none of them owns. The encoders and decoders encode and decode
// it, and the cluster binding fills and pushes it. The package does no
// I/O and knows no wire format and no C API.
package model

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// TableBatch is one table's rows: $timestamp first, then the data columns
// the body carried, in the body's order. The receiver owns the batch and
// releases it once.
type TableBatch struct {
	Table string
	Batch arrow.RecordBatch
}

// SchemaOf answers the schema the bulk reader answers for the table whole:
// $table, $timestamp, then the data columns, in the reader's Arrow types.
// For a table the cluster does not know, the cluster's error passes
// through as is.
type SchemaOf func(table string) (*arrow.Schema, error)

// EmptyBatch is a batch of schema and no rows. A reader that yields no
// batch answers it as its one element, so every receiver sees the schema.
// The caller owns the batch and releases it once.
func EmptyBatch(schema *arrow.Schema) arrow.RecordBatch {
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
