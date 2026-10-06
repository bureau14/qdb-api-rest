// Package model is the server's neutral representation of table data,
// Arrow by indirection: the types the layers hand each other, which no
// one of them owns. The encoders and decoders render and read it, the
// cluster binding fills and pushes it. The package does no I/O and knows
// no wire format and no C API.
package model

import "github.com/apache/arrow-go/v18/arrow"

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
