package qdb

import (
	"errors"
	"testing"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// TestColumnInfos: the six type words map onto the binding's types, a
// symbol carries its symtable, and a column outside the vocabulary or on
// the wrong side of the symtable rule is ErrInvalidColumn.
func TestColumnInfos(t *testing.T) {
	infos, err := columnInfos([]Column{
		{Name: "a", Type: "blob"},
		{Name: "b", Type: "double"},
		{Name: "c", Type: "int64"},
		{Name: "d", Type: "string"},
		{Name: "e", Type: "symbol", Symtable: "syms"},
		{Name: "f", Type: "timestamp"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []qdbapi.TsColumnType{
		qdbapi.TsColumnBlob, qdbapi.TsColumnDouble, qdbapi.TsColumnInt64,
		qdbapi.TsColumnString, qdbapi.TsColumnSymbol, qdbapi.TsColumnTimestamp,
	}
	for i, info := range infos {
		if info.Type() != want[i] {
			t.Errorf("%s: type %v, want %v", info.Name(), info.Type(), want[i])
		}
	}
	if got := infos[4].Symtable(); got != "syms" {
		t.Errorf("symtable = %q", got)
	}
	invalid := map[string]Column{
		"unknown type":            {Name: "x", Type: "count"},
		"symbol without symtable": {Name: "x", Type: "symbol"},
		"symtable on a string":    {Name: "x", Type: "string", Symtable: "syms"},
	}
	for name, c := range invalid {
		if _, err := columnInfos([]Column{c}); !errors.Is(err, ErrInvalidColumn) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
