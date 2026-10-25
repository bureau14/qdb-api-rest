// The describe answers the Arrow schema of a DuckDB query without running
// it. Flight SQL asks for a result's schema before any data: GetFlightInfo,
// GetSchema and the prepare all answer it, and Arrow's JDBC driver reads no
// result without the prepare's schema. DuckDB's own Arrow conversion takes
// a column's logical type, and duckdb-go keeps that type unexported. So
// this file prepares the statement, reads each column's type as duckdb-go
// exposes it, and maps that type to Arrow as DuckDB's conversion does.
// TestDescribe keeps the map equal to the conversion.
package olap

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/duckdb/duckdb-go/v2"
)

// ErrNotOneStatement is the error of a text that holds no statement or
// more than one, because a describe prepares one statement.
var ErrNotOneStatement = errors.New("olap: a query is one statement")

// ErrParameters is the error of a statement with parameter markers,
// which nothing binds.
var ErrParameters = errors.New("olap: query parameters are not supported")

// ErrUndescribable is the error of a column whose DuckDB type has no
// Arrow mapping here. Its message names the column and the type.
var ErrUndescribable = errors.New("olap: column type cannot be described")

// typeName names a DuckDB type for an ErrUndescribable message. duckdb-go
// exports no type names, so it knows the types arrowTypeOf has no case for
// and gives any other type as its id.
func typeName(t duckdb.Type) string {
	switch t {
	case duckdb.TYPE_GEOMETRY:
		return "GEOMETRY"
	case duckdb.TYPE_BIGNUM:
		return "BIGNUM"
	case duckdb.TYPE_VARIANT:
		return "VARIANT"
	case duckdb.TYPE_ANY:
		return "ANY"
	case duckdb.TYPE_INVALID:
		return "INVALID"
	default:
		return fmt.Sprintf("type id %d", t)
	}
}

// enumIndex answers the index type of an ENUM of n values, which DuckDB
// stores the ENUM in and the Arrow dictionary uses.
func enumIndex(n int) arrow.DataType {
	// DuckDB's EnumTypeInfo::DictType picks the smallest unsigned type
	// that counts every value.
	switch {
	case n <= 0xff:
		return arrow.PrimitiveTypes.Uint8
	case n <= 0xffff:
		return arrow.PrimitiveTypes.Uint16
	default:
		return arrow.PrimitiveTypes.Uint32
	}
}

// arrowTypeOf answers the Arrow type DuckDB's Arrow conversion gives a
// column of the type, with tz as the zone of a TIMESTAMP_TZ. A type the
// map has no case for is ErrUndescribable.
func arrowTypeOf(t duckdb.TypeInfo, tz string) (arrow.DataType, error) {
	// The cases follow SetArrowFormat in arrow_converter.cpp of DuckDB
	// 1.5.4, the version qdb-duck pins, under its default client
	// properties: regular offsets, no views, lossless conversion off and
	// output version 1.0. The nested types recurse.
	//
	// A LIMIT 0 run through the reader was rejected, because SHOW,
	// DESCRIBE, PRAGMA and SET cannot be a subquery. A duckdb-go change
	// that exports the logical type was rejected, because it waits on an
	// outside project.
	switch t.InternalType() {
	case duckdb.TYPE_BOOLEAN:
		return arrow.FixedWidthTypes.Boolean, nil
	case duckdb.TYPE_TINYINT:
		return arrow.PrimitiveTypes.Int8, nil
	case duckdb.TYPE_SMALLINT:
		return arrow.PrimitiveTypes.Int16, nil
	case duckdb.TYPE_INTEGER:
		return arrow.PrimitiveTypes.Int32, nil
	case duckdb.TYPE_BIGINT:
		return arrow.PrimitiveTypes.Int64, nil
	case duckdb.TYPE_UTINYINT:
		return arrow.PrimitiveTypes.Uint8, nil
	case duckdb.TYPE_USMALLINT:
		return arrow.PrimitiveTypes.Uint16, nil
	case duckdb.TYPE_UINTEGER:
		return arrow.PrimitiveTypes.Uint32, nil
	case duckdb.TYPE_UBIGINT:
		return arrow.PrimitiveTypes.Uint64, nil
	case duckdb.TYPE_FLOAT:
		return arrow.PrimitiveTypes.Float32, nil
	case duckdb.TYPE_DOUBLE:
		return arrow.PrimitiveTypes.Float64, nil
	// decimal128(38, 0), because lossless conversion is off
	case duckdb.TYPE_HUGEINT, duckdb.TYPE_UHUGEINT:
		return &arrow.Decimal128Type{Precision: 38, Scale: 0}, nil
	// always 128 bits, because output version 1.0 is below 1.5, where
	// DuckDB starts to narrow a decimal to its physical width
	case duckdb.TYPE_DECIMAL:
		d := t.Details().(*duckdb.DecimalDetails)
		return &arrow.Decimal128Type{Precision: int32(d.Width), Scale: int32(d.Scale)}, nil
	case duckdb.TYPE_VARCHAR, duckdb.TYPE_UUID:
		return arrow.BinaryTypes.String, nil
	case duckdb.TYPE_BLOB, duckdb.TYPE_BIT:
		return arrow.BinaryTypes.Binary, nil
	case duckdb.TYPE_DATE:
		return arrow.FixedWidthTypes.Date32, nil
	case duckdb.TYPE_TIME, duckdb.TYPE_TIME_TZ:
		return arrow.FixedWidthTypes.Time64us, nil
	case duckdb.TYPE_TIMESTAMP:
		return &arrow.TimestampType{Unit: arrow.Microsecond}, nil
	case duckdb.TYPE_TIMESTAMP_S:
		return &arrow.TimestampType{Unit: arrow.Second}, nil
	case duckdb.TYPE_TIMESTAMP_MS:
		return &arrow.TimestampType{Unit: arrow.Millisecond}, nil
	case duckdb.TYPE_TIMESTAMP_NS:
		return &arrow.TimestampType{Unit: arrow.Nanosecond}, nil
	case duckdb.TYPE_TIMESTAMP_TZ:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: tz}, nil
	case duckdb.TYPE_INTERVAL:
		return arrow.FixedWidthTypes.MonthDayNanoInterval, nil
	// Unreachable: duckdb-go has no TypeInfo for SQLNULL, so columnsOf
	// types a top-level one and refuses a nested one. The case mirrors
	// the conversion.
	case duckdb.TYPE_SQLNULL:
		return arrow.Null, nil
	// the child is named l
	case duckdb.TYPE_LIST:
		d := t.Details().(*duckdb.ListDetails)
		c, err := arrowTypeOf(d.Child, tz)
		if err != nil {
			return nil, err
		}
		return arrow.ListOfField(arrow.Field{Name: "l", Type: c, Nullable: true}), nil
	// the child has no name
	case duckdb.TYPE_ARRAY:
		d := t.Details().(*duckdb.ArrayDetails)
		c, err := arrowTypeOf(d.Child, tz)
		if err != nil {
			return nil, err
		}
		return arrow.FixedSizeListOfField(int32(d.Size), arrow.Field{Type: c, Nullable: true}), nil
	case duckdb.TYPE_STRUCT:
		d := t.Details().(*duckdb.StructDetails)
		fields := make([]arrow.Field, len(d.Entries))
		for i, e := range d.Entries {
			c, err := arrowTypeOf(e.Info(), tz)
			if err != nil {
				return nil, err
			}
			fields[i] = arrow.Field{Name: e.Name(), Type: c, Nullable: true}
		}
		return arrow.StructOf(fields...), nil
	// arrow-go's import builds a map through MapOf from the key and value
	// types, so MapOf answers the reader's type, non-nullable key included.
	case duckdb.TYPE_MAP:
		d := t.Details().(*duckdb.MapDetails)
		k, err := arrowTypeOf(d.Key, tz)
		if err != nil {
			return nil, err
		}
		v, err := arrowTypeOf(d.Value, tz)
		if err != nil {
			return nil, err
		}
		return arrow.MapOf(k, v), nil
	// a sparse union whose type codes are the members' positions
	case duckdb.TYPE_UNION:
		d := t.Details().(*duckdb.UnionDetails)
		fields := make([]arrow.Field, len(d.Members))
		codes := make([]arrow.UnionTypeCode, len(d.Members))
		for i, m := range d.Members {
			c, err := arrowTypeOf(m.Type, tz)
			if err != nil {
				return nil, err
			}
			fields[i] = arrow.Field{Name: m.Name, Type: c, Nullable: true}
			codes[i] = arrow.UnionTypeCode(i)
		}
		return arrow.SparseUnionOf(fields, codes), nil
	// an unordered dictionary of utf8 values
	case duckdb.TYPE_ENUM:
		d := t.Details().(*duckdb.EnumDetails)
		return &arrow.DictionaryType{IndexType: enumIndex(len(d.Values)), ValueType: arrow.BinaryTypes.String}, nil
	// An extension or a newer type has no case, because a guess would
	// promise a client a type the stream does not carry.
	default:
		return nil, fmt.Errorf("%w: DuckDB %s", ErrUndescribable, typeName(t.InternalType()))
	}
}

// column is one column of a prepared statement: its name and its type,
// nil for a bare SQLNULL, which duckdb-go answers no TypeInfo for.
type column struct {
	name string
	info duckdb.TypeInfo
}

// columnsOf answers the columns of a prepared statement. A bare SQLNULL
// column has no info, and a column of a type duckdb-go does not model is
// ErrUndescribable.
func columnsOf(st *duckdb.Stmt) ([]column, error) {
	n, err := st.ColumnCount()
	if err != nil {
		return nil, err
	}
	cols := make([]column, n)
	for i := range cols {
		if cols[i].name, err = st.ColumnName(i); err != nil {
			return nil, err
		}
		// duckdb-go has no TypeInfo for SQLNULL, so the type id finds that
		// column first.
		id, err := st.ColumnType(i)
		if err != nil {
			return nil, err
		}
		if id == duckdb.TYPE_SQLNULL {
			continue
		}
		if cols[i].info, err = st.ColumnTypeInfo(i); err != nil {
			return nil, fmt.Errorf("%w: column %q: %w", ErrUndescribable, cols[i].name, err)
		}
	}
	return cols, nil
}

// hasZone reports whether a type is a TIMESTAMP_TZ or holds one at any
// depth.
func hasZone(t duckdb.TypeInfo) bool {
	if t == nil {
		return false
	}
	switch d := t.Details().(type) {
	case *duckdb.ListDetails:
		return hasZone(d.Child)
	case *duckdb.ArrayDetails:
		return hasZone(d.Child)
	case *duckdb.MapDetails:
		return hasZone(d.Key) || hasZone(d.Value)
	case *duckdb.StructDetails:
		for _, e := range d.Entries {
			if hasZone(e.Info()) {
				return true
			}
		}
		return false
	case *duckdb.UnionDetails:
		for _, m := range d.Members {
			if hasZone(m.Type) {
				return true
			}
		}
		return false
	}
	return t.InternalType() == duckdb.TYPE_TIMESTAMP_TZ
}

// zoneOf answers the connection's TimeZone setting, which DuckDB's Arrow
// conversion puts on a TIMESTAMP_TZ.
func zoneOf(ctx context.Context, conn driver.Conn) (string, error) {
	rows, err := conn.(driver.QueryerContext).QueryContext(ctx, "SELECT current_setting('TimeZone')", nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()
	v := make([]driver.Value, 1)
	if err := rows.Next(v); err != nil {
		return "", err
	}
	tz, ok := v[0].(string)
	if !ok {
		return "", fmt.Errorf("olap: TimeZone is %T", v[0])
	}
	return tz, nil
}

// Describe answers the Arrow schema Query would stream for a query,
// without running it. The query is one statement without parameter
// markers. The errors are ErrNotOneStatement, ErrParameters,
// ErrUndescribable, and DuckDB's own parse and bind errors.
func (i *Instance) Describe(ctx context.Context, q string) (*arrow.Schema, error) {
	// The statement is prepared and never executed, on a connection that
	// lives for one describe as Query's does:
	//
	//  1. open a connection of its own;
	//  2. prepare the query as one statement;
	//  3. refuse parameter markers;
	//  4. read each column's name and type;
	//  5. read the TimeZone when a column holds a TIMESTAMP_TZ;
	//  6. map each column to its field.

	// 1. open a connection, closed whatever happened
	conn, err := i.connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	// 2. prepare through driver.Conn's Prepare, and close the statement
	//    whatever happened. PrepareContext was rejected, because it
	//    executes every statement before the last. A *duckdb.Error is
	//    DuckDB's parse or bind error. Any other error is duckdb-go
	//    refusing a text of several statements or of none.
	prepared, err := conn.Prepare(q)
	if err != nil {
		var de *duckdb.Error
		if errors.As(err, &de) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrNotOneStatement, err)
	}
	defer func() { _ = prepared.Close() }()
	st := prepared.(*duckdb.Stmt)
	// 3. refuse parameter markers, because nothing binds a value
	if st.NumInput() > 0 {
		return nil, ErrParameters
	}
	// 4. read the columns
	cols, err := columnsOf(st)
	if err != nil {
		return nil, err
	}
	// 5. read the TimeZone only when a column needs it, because the reader
	//    puts that setting on a TIMESTAMP_TZ
	var tz string
	for _, c := range cols {
		if hasZone(c.info) {
			if tz, err = zoneOf(ctx, conn); err != nil {
				return nil, err
			}
			break
		}
	}
	// 6. map the columns. Every field is nullable, as DuckDB's conversion
	//    makes it, and a bare SQLNULL column is the null type.
	fields := make([]arrow.Field, len(cols))
	for n, c := range cols {
		dt := arrow.DataType(arrow.Null)
		if c.info != nil {
			if dt, err = arrowTypeOf(c.info, tz); err != nil {
				return nil, fmt.Errorf("column %q: %w", c.name, err)
			}
		}
		fields[n] = arrow.Field{Name: c.name, Type: dt, Nullable: true}
	}
	return arrow.NewSchema(fields, nil), nil
}
