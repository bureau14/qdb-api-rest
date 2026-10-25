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
// more than one. A describe prepares exactly one statement.
var ErrNotOneStatement = errors.New("olap: a query is one statement")

// ErrParameters is the error of a statement with parameter markers,
// which nothing binds.
var ErrParameters = errors.New("olap: query parameters are not supported")

// ErrUndescribable is the error of a column whose DuckDB type the
// describe does not map to Arrow. It names the column and the type.
var ErrUndescribable = errors.New("olap: column type cannot be described")

// typeName names t for an ErrUndescribable message. It names the types
// that fall outside arrowTypeOf's cases, and any other type by its id,
// because duckdb-go exports no name of a type.
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

// enumIndex answers the index type DuckDB stores an ENUM of n values in,
// which is the dictionary's index type on the Arrow side.
func enumIndex(n int) arrow.DataType {
	// DuckDB's EnumTypeInfo picks the smallest unsigned type that counts
	// every value.
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
// column of t, under the default client properties, with tz as the
// zone of a TIMESTAMP_TZ. A type outside the conversion's own switch is
// ErrUndescribable.
func arrowTypeOf(t duckdb.TypeInfo, tz string) (arrow.DataType, error) {
	// The cases follow SetArrowFormat in arrow_converter.cpp of DuckDB
	// 1.5.4, the version qdb-duck pins, case by case, under the defaults of
	// its client properties: regular
	// offsets, no list or string views, lossless conversion off and output
	// version 1.0. The nested types recurse. TestDescribe keeps the cases
	// equal to what the reader answers.
	//
	// The map exists because duckdb-go keeps a column's logical type
	// unexported, so DuckDB's own conversion cannot be asked. A LIMIT 0 run
	// through the reader was rejected, because it fails on a statement
	// that cannot be a subquery (SHOW, DESCRIBE, PRAGMA, SET). A duckdb-go
	// change that exports the logical type was rejected, because it waits
	// on an outside project.
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
	// DuckDB marks the entries struct and its key not nullable. arrow-go's
	// import builds the map through MapOf from the key and value types
	// alone, so MapOf here answers the type the reader does.
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
	// a dictionary of utf8 values whose index type follows the number of
	// values, unordered
	case duckdb.TYPE_ENUM:
		d := t.Details().(*duckdb.EnumDetails)
		return &arrow.DictionaryType{IndexType: enumIndex(len(d.Values)), ValueType: arrow.BinaryTypes.String}, nil
	// An extension or newer type has no case, and a guess would promise a
	// client a type the stream does not carry.
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

// columnsOf answers the columns of st. A column whose type duckdb-go
// does not model is ErrUndescribable, naming the column.
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
		// duckdb-go refuses a TypeInfo for SQLNULL, so the type id finds
		// that column first.
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

// hasZone reports whether t is a TIMESTAMP_TZ or holds one at any depth.
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

// zoneOf answers the TimeZone setting of conn, the zone DuckDB's Arrow
// conversion tags a TIMESTAMP_TZ with.
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

// Describe answers the Arrow schema of q's result without running q, in
// the types Query streams for the same q. q is one statement without
// parameter markers. A text of no statement or several is
// ErrNotOneStatement, a statement with parameters is ErrParameters, a
// column type outside the map is ErrUndescribable, and a parse or bind
// error is DuckDB's.
func (i *Instance) Describe(ctx context.Context, q string) (*arrow.Schema, error) {
	// The statement is prepared and never executed, and the connection
	// lives for one describe, as Query's does:
	//
	//  1. open a connection of its own;
	//  2. prepare q as one statement;
	//  3. refuse parameter markers;
	//  4. read the TimeZone when a column holds a TIMESTAMP_TZ;
	//  5. map each column to its field.

	// 1. open a connection, closed with the statement whatever happened
	conn, err := i.connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	// 2. driver.Conn's Prepare, because PrepareContext executes every
	//    statement before the last. Its error is a *duckdb.Error for
	//    DuckDB's own parse and bind errors, and any other error is
	//    duckdb-go's refusal of a text of several statements or of none.
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
	// 3. NumInput above zero is ErrParameters, because nothing binds a
	//    value
	if st.NumInput() > 0 {
		return nil, ErrParameters
	}
	cols, err := columnsOf(st)
	if err != nil {
		return nil, err
	}
	// 4. the TimeZone on the same connection, only when a column needs it,
	//    because the reader tags a TIMESTAMP_TZ with that setting
	var tz string
	for _, c := range cols {
		if hasZone(c.info) {
			if tz, err = zoneOf(ctx, conn); err != nil {
				return nil, err
			}
			break
		}
	}
	// 5. each field nullable, as DuckDB's Arrow conversion makes every
	//    column, and named and typed as the statement says
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
