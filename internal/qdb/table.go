package qdb

import (
	"errors"
	"fmt"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// ErrInvalidColumn is a column no table can be created with: a type word
// outside the schema vocabulary, or a symtable where the type does not
// take one. It is the caller's error and is found before any session is
// leased.
var ErrInvalidColumn = errors.New("qdb: invalid column")

// Column is one column of a table to create, in the schema vocabulary:
// Type is one of blob, double, int64, string, symbol, timestamp, and
// Symtable names the symbol table of a symbol column and of no other.
type Column struct {
	Name     string
	Type     string
	Symtable string
}

// columnTypes maps the schema vocabulary onto the binding's column types.
var columnTypes = map[string]qdbapi.TsColumnType{
	"blob":      qdbapi.TsColumnBlob,
	"double":    qdbapi.TsColumnDouble,
	"int64":     qdbapi.TsColumnInt64,
	"string":    qdbapi.TsColumnString,
	"symbol":    qdbapi.TsColumnSymbol,
	"timestamp": qdbapi.TsColumnTimestamp,
}

// info is c as the create call takes it.
func (c Column) info() (qdbapi.TsColumnInfo, error) {
	kind, ok := columnTypes[c.Type]
	switch {
	case !ok:
		return qdbapi.TsColumnInfo{}, fmt.Errorf("%w: %s has unknown type %q", ErrInvalidColumn, c.Name, c.Type)
	case kind == qdbapi.TsColumnSymbol && c.Symtable == "":
		return qdbapi.TsColumnInfo{}, fmt.Errorf("%w: symbol column %s names no symtable", ErrInvalidColumn, c.Name)
	case kind != qdbapi.TsColumnSymbol && c.Symtable != "":
		return qdbapi.TsColumnInfo{}, fmt.Errorf("%w: %s column %s takes no symtable", ErrInvalidColumn, c.Type, c.Name)
	case kind == qdbapi.TsColumnSymbol:
		return qdbapi.NewSymbolColumnInfo(c.Name, c.Symtable), nil
	}
	return qdbapi.NewTsColumnInfo(c.Name, kind), nil
}

// columnInfos is cols as the create call takes them, or the first invalid
// column's error.
func columnInfos(cols []Column) ([]qdbapi.TsColumnInfo, error) {
	infos := make([]qdbapi.TsColumnInfo, len(cols))
	for i, c := range cols {
		info, err := c.info()
		if err != nil {
			return nil, err
		}
		infos[i] = info
	}
	return infos, nil
}
