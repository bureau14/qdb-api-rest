// Package model is the server's neutral representation of table data.
// It holds the types, built on Arrow, that the layers hand each other
// and that none of them owns. The encoders and decoders encode and
// decode it, and the cluster binding fills and pushes it. The package
// does no I/O and knows no wire format and no C API.
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

// SchemaOf returns the schema the bulk reader answers for a whole table:
// $table, $timestamp, then the data columns, in the reader's Arrow types.
// For a table the cluster does not know, it returns the cluster's error
// as is.
type SchemaOf func(table string) (*arrow.Schema, error)

// BatchRows is the row count of every record batch the binary splits a
// result into, so every consumer reads batches of one size. It sizes the
// slices Cluster.Query hands out and the bulk reader's fetch, and a
// decoder checks the ctx once per BatchRows rows. The value was not
// measured.
const BatchRows = 65536

// EmptyBatch returns a batch with the given schema and no rows. A reader
// that would yield no batch yields this one instead, so every receiver
// sees the schema.
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
