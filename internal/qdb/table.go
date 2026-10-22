package qdb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// ErrInvalidColumn is the create error for a column no table can be
// created with: a type word outside the schema vocabulary, or a symtable
// on a type that does not take one. It is the caller's error and is
// found before any session is leased.
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

// CreateTable creates the table called name, as user u, with cols after
// the implied $timestamp column and a shard size of shard. An invalid
// column is reported before a session is leased. A create is never
// retried, because it is not a read.
func (c *Cluster) CreateTable(ctx context.Context, u User, name string, shard time.Duration, cols []Column) error {
	infos, err := columnInfos(cols)
	if err != nil {
		return err
	}
	return c.Call(ctx, u, func(s *Session) error { return s.CreateTable(name, shard, infos...) })
}

// RemoveTable removes the table called name, as user u. It leaves the
// table's symtable in place, because a symtable is its own entry and
// other tables may share it.
func (c *Cluster) RemoveTable(ctx context.Context, u User, name string) error {
	return c.Call(ctx, u, func(s *Session) error { return s.RemoveTable(name) })
}

// Tables lists the names of the cluster's tables as user u, in the order
// the cluster reports them, qdbd's own $qdb. tables included. The listing
// is retried once on a retryable failure, as a read is.
func (c *Cluster) Tables(ctx context.Context, u User) ([]string, error) {
	// The list is the native query `show tables`, because the C API has no
	// enumeration call and that query is the server's own notion of a
	// table, answered in one round trip. A prefix scan over the empty
	// prefix with a metadata call per alias was rejected: it is N+1 round
	// trips over every entry type, and a new Session method for PrefixGet.
	// The $qdb. tables are listed as the server lists them, because hiding
	// them needs a prefix rule this project would own, and a client that
	// queries $qdb.firehose finds it in the list.
	//
	//  1. run the query under the read retry;
	//  2. inside the sink, copy each value of the one string column out.
	var names []string
	// 1. the read retry, because a listing is a read
	err := c.Query(ctx, u, "show tables", func(batches Batches) error {
		for rec, err := range batches {
			if err != nil {
				return err
			}
			// 2. the result is one string column named $table (qdbd
			//    3.15.0.dev0). It is read by position, and a result of
			//    another shape is an error naming it, because a server change
			//    would otherwise read as an empty cluster. Each value is
			//    cloned, because a Go string of an Arrow array aliases the
			//    batch's buffer and Query releases the batch when the sink
			//    returns.
			if rec.NumCols() != 1 || rec.Column(0).DataType().ID() != arrow.STRING {
				return fmt.Errorf("qdb: show tables answered %s, want one string column", rec.Schema())
			}
			col := rec.Column(0).(*array.String)
			for i := range col.Len() {
				names = append(names, strings.Clone(col.Value(i)))
			}
		}
		return nil
	}, WithReadRetry())
	if err != nil {
		return nil, err
	}
	return names, nil
}

// IsTableExists reports whether err means the cluster refused a create
// because the name is taken. The HTTP layer maps it to 409.
func IsTableExists(err error) bool {
	return errors.Is(err, qdbapi.ErrAliasAlreadyExists)
}

// IsTableNotFound reports whether err means the cluster knows no entry
// of that name. The HTTP layer maps it to 404 where the name is the
// resource.
func IsTableNotFound(err error) bool {
	return errors.Is(err, qdbapi.ErrAliasNotFound)
}
