package qdb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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

// tables lists the names of the cluster's tables on the held session, in
// the order the cluster reports them, qdbd's own $qdb. tables included.
func (s *Session) tables() ([]string, error) {
	// The list is the native query `show tables`, because the C API has no
	// enumeration call and that query is the server's own notion of a
	// table, answered in one round trip. A prefix scan over the empty
	// prefix with a metadata call per alias was rejected: N+1 round trips
	// over every entry type. The $qdb. tables are listed as the server
	// lists them, because hiding them needs a prefix rule this project
	// would own, and a client that queries $qdb.firehose finds it here.
	//
	//  1. fetch the result on the held session;
	//  2. check the result's shape;
	//  3. copy each name out before the batch goes.

	// 1. one fetch, whose batch this function owns and releases. fetch
	//    answers a nil batch for a statement without a result set, which
	//    is read as no table rather than dereferenced.
	rec, err := s.fetch("show tables")
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	defer rec.Release()
	// 2. the result is one string column named $table (qdbd 3.15.0.dev0),
	//    read by position, and a result of another shape is an error
	//    naming it, because a server change would otherwise read as an
	//    empty cluster
	if rec.NumCols() != 1 || rec.Column(0).DataType().ID() != arrow.STRING {
		return nil, fmt.Errorf("qdb: show tables answered %s, want one string column", rec.Schema())
	}
	// 3. each value is cloned, because a Go string of an Arrow array
	//    aliases the batch's buffer and the batch is released on return
	col := rec.Column(0).(*array.String)
	names := make([]string, col.Len())
	for i := range names {
		names[i] = strings.Clone(col.Value(i))
	}
	return names, nil
}

// Tables lists the names of the cluster's tables as user u, in the order
// the cluster reports them, qdbd's own $qdb. tables included. The listing
// is retried once on a retryable failure, as a read is.
func (c *Cluster) Tables(ctx context.Context, u User) ([]string, error) {
	var names []string
	// The read retry, because a listing is a read.
	err := c.Call(ctx, u, func(s *Session) error {
		var err error
		names, err = s.tables()
		return err
	}, WithReadRetry())
	if err != nil {
		return nil, err
	}
	return names, nil
}

// The metadata keys of the schema TableSchemas answers. They are this
// server's own, outside the ARROW:FLIGHT:SQL: prefix Flight SQL reserves
// for its keys. MetaSymtable is a field key on a symbol column whose value
// is the symtable name. MetaShardSize and MetaTTL are schema keys whose
// values are an integer count of milliseconds, and a TTL of 0 means
// disabled.
const (
	MetaSymtable  = "qdb:symtable"
	MetaShardSize = "qdb:shard_size"
	MetaTTL       = "qdb:ttl"
)

// TableSchema is one table of a listing: its name and the schema
// TableSchemas builds for it.
type TableSchema struct {
	Name   string
	Schema *arrow.Schema
}

// millis is d as the metadata keys carry it: an integer count of
// milliseconds.
func millis(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10)
}

// schemaOfMetadata is the schema TableSchemas answers for a table whose
// metadata is md: $timestamp, non-nullable, then the columns in order,
// nullable, in the binding's Arrow types, a symbol column's field carrying
// MetaSymtable, and the schema carrying MetaShardSize and MetaTTL in
// milliseconds.
func schemaOfMetadata(md qdbapi.TableMetadata) *arrow.Schema {
	// The shape is the table as it was created and not the reader's or a
	// query's result set, because the metadata call is the canonical
	// description of a table and a result set has a schema of its own.
	// The reader's shape with $table in front was rejected, because
	// $table is a result-set artefact no table has, and the columns alone
	// were rejected, because $timestamp is a column of every table that
	// every query returns.
	//
	//  1. $timestamp first;
	//  2. each column, nullable, in the binding's Arrow type, a symbol
	//     column's field carrying its symtable;
	//  3. the shard size and the TTL on the schema.

	// 1. the reader's special field, non-nullable, because the metadata
	//    does not list the implicit index and every row has one
	fields := make([]arrow.Field, 0, len(md.Columns)+1)
	fields = append(fields, specialFields["$timestamp"])
	// 2. dataFields builds the column fields, because the server declares
	//    no type map of its own. The symtable joins a symbol field as
	//    metadata, because it is the one column attribute the Arrow type
	//    loses: a symbol reads as a string.
	cols, _ := dataFields(md.Columns)
	for i, f := range cols {
		if c := md.Columns[i]; c.Type() == qdbapi.TsColumnSymbol {
			f.Metadata = arrow.NewMetadata([]string{MetaSymtable}, []string{c.Symtable()})
		}
		fields = append(fields, f)
	}
	// 3. integer milliseconds, the C API's unit and the unit the create
	//    body takes, with 0 meaning disabled as qdb_ttl_disabled does. Go
	//    duration strings were rejected as a second spelling of a duration
	//    next to the create body's milliseconds. The reserved
	//    ARROW:FLIGHT:SQL keys, TYPE_NAME and the rest, were rejected
	//    because nothing asks for them yet and they can be added later.
	meta := arrow.NewMetadata([]string{MetaShardSize, MetaTTL}, []string{millis(md.ShardSize), millis(md.TTL)})
	return arrow.NewSchema(fields, &meta)
}

// tableSchema answers the schema of the table called name through one
// metadata call on the held session. The binding's error for an unknown
// table passes through unchanged, so IsTableNotFound classifies it.
func (s *Session) tableSchema(name string) (*arrow.Schema, error) {
	// Metadata is the fetching call and not CachedMetadata, because a
	// listing describes the cluster as it is at the call.
	md, err := s.session.Table(name).Metadata()
	if err != nil {
		return nil, err
	}
	return schemaOfMetadata(md), nil
}

// TableSchemas lists the cluster's tables as user u with the schema of
// each, in the order the cluster reports them, qdbd's own $qdb. tables
// included, through one leased session. Any failed lookup fails the
// listing. The listing is retried once on a retryable failure, as a read
// is.
func (c *Cluster) TableSchemas(ctx context.Context, u User) ([]TableSchema, error) {
	// One Call holds the session across the listing and every lookup,
	// because one checkout per request is what the pool and the budget
	// are built around, and Query hands the session back before the names
	// are read. Tables, then one Call per name, was rejected: N+1
	// checkouts per request, each counted by the budget.
	//
	//  1. the names on the held session;
	//  2. one metadata call per name on the same session, in the names'
	//     order;
	//  3. any failure fails the listing.
	var out []TableSchema
	// A listing is a read, and a retry re-runs the whole closure on a
	// fresh session after a bad one.
	err := c.Call(ctx, u, func(s *Session) error {
		// 1. the listing Tables answers
		names, err := s.tables()
		if err != nil {
			return err
		}
		out = make([]TableSchema, 0, len(names))
		for _, name := range names {
			// 2. one round trip per name
			schema, err := s.tableSchema(name)
			// 3. a not-found included, because a table that vanishes between
			//    the two calls makes the request inconsistent, and the case
			//    is extreme. Skipping the vanished row was rejected: an
			//    errors.Is per lookup for a case that does not happen in
			//    practice.
			if err != nil {
				return err
			}
			out = append(out, TableSchema{Name: name, Schema: schema})
		}
		return nil
	}, WithReadRetry())
	if err != nil {
		return nil, err
	}
	return out, nil
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
