package v1

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
)

// v1Tables is the old server's query answer.
type v1Tables struct {
	Tables []v1Table `json:"tables"`
}

// v1Table is one table, its keys in the old server's order: its columns,
// nil for a find, then its name, empty for a query.
type v1Table struct {
	Columns []v1Column `json:"columns"`
	Name    string     `json:"name,omitempty"`
}

// v1Column is one column, its keys in the old server's order.
type v1Column struct {
	Data []any  `json:"data"`
	Name string `json:"name,omitempty"`
	Type string `json:"type,omitempty"`
}

// v1Word answers the old server's word for an Arrow type: blob, double,
// int64, string or timestamp, and the Arrow type's name for any other.
func v1Word(t arrow.DataType) string {
	switch t.ID() {
	case arrow.BINARY:
		return "blob"
	case arrow.FLOAT64:
		return "double"
	case arrow.INT64:
		return "int64"
	case arrow.STRING:
		return "string"
	case arrow.TIMESTAMP:
		return "timestamp"
	default:
		// Only DuckDB answers another type, and no old client sends #sql.
		return t.Name()
	}
}

// v1Cell answers the function that turns cell i of the column into the
// value encoding/json writes as the old server wrote it. A null is nil,
// and so are NaN and the infinities. A type outside the old server's five
// answers the v2 JSON cell as raw JSON.
func v1Cell(f arrow.Field, a arrow.Array) (func(i int) any, error) {
	var cell func(i int) any
	switch a := a.(type) {
	case *array.Binary:
		cell = func(i int) any { return bytes.Clone(a.Value(i)) }
	case *array.Float64:
		cell = func(i int) any {
			// encoding/json cannot write NaN or an infinity.
			if v := a.Value(i); !math.IsNaN(v) && !math.IsInf(v, 0) {
				return v
			}
			return nil
		}
	case *array.Int64:
		cell = func(i int) any { return a.Value(i) }
	case *array.String:
		cell = func(i int) any { return strings.Clone(a.Value(i)) }
	case *array.Timestamp:
		unit := a.DataType().(*arrow.TimestampType).Unit
		// ToTime answers UTC, whatever zone the process runs in.
		cell = func(i int) any { return a.Value(i).ToTime(unit) }
	default:
		// encoding.JSONCell writes the cell, so a DuckDB type reads as it
		// does on v2.
		js, err := encoding.JSONCell(f, a)
		if err != nil {
			return nil, err
		}
		cell = func(i int) any { return json.RawMessage(js(nil, i)) }
	}
	return func(i int) any {
		if a.IsNull(i) {
			return nil
		}
		return cell(i)
	}, nil
}

// readV1Tables reads an Arrow IPC stream into the old server's body: no
// table for no rows, and otherwise one table without a name holding every
// batch's rows. A column's type is its word when a cell is not null, and
// none when every cell is.
func readV1Tables(r io.Reader) (v1Tables, error) {
	// 1. open the stream and take the columns from its schema
	// 2. append every batch's cells to the columns, noting each column that holds a value
	// 3. answer no table for no rows, else one table with each column's word

	// 1. A stream of no batches still carries its columns.
	rd, err := ipc.NewReader(r)
	if err != nil {
		return v1Tables{}, err
	}
	defer rd.Release()
	fields := rd.Schema().Fields()
	cols := make([]v1Column, len(fields))
	valued := make([]bool, len(fields))
	for i, f := range fields {
		cols[i] = v1Column{Data: []any{}, Name: f.Name}
	}

	// 2. The cells are copied into Go values, because the reader releases
	//    each batch at Next.
	var rows int64
	for rd.Next() {
		rec := rd.RecordBatch()
		for i, f := range fields {
			a := rec.Column(i)
			cell, err := v1Cell(f, a)
			if err != nil {
				return v1Tables{}, err
			}
			for row := range a.Len() {
				cols[i].Data = append(cols[i].Data, cell(row))
			}
			// NullN counts every cell of Arrow's null type, whose IsNull
			// answers false for want of a validity bitmap.
			valued[i] = valued[i] || a.NullN() < a.Len()
		}
		rows += rec.NumRows()
	}
	if err := rd.Err(); err != nil {
		return v1Tables{}, err
	}

	// 3. The old server answered no table for no rows, and a statement
	//    without a result set has none.
	if rows == 0 {
		return v1Tables{Tables: []v1Table{}}, nil
	}
	for i, f := range fields {
		// One non-null cell is enough, because an Arrow column has one type.
		cols[i].Type = "none"
		if valued[i] {
			cols[i].Type = v1Word(f.Type)
		}
	}
	return v1Tables{Tables: []v1Table{{Columns: cols}}}, nil
}
