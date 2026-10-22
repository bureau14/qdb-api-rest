package flightsql

import (
	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"

	"github.com/bureau14/qdb-api-rest/internal/build"
)

// registerSqlInfo registers on b what GetSqlInfo answers: the server, its
// versions, what it does not do, and DuckDB's identifier rules (ADR-0018,
// Decision 10). It fails only for a value of a type the base server
// refuses, which no value here is.
func registerSqlInfo(b *flightsql.BaseServer) error {
	type entry struct {
		id    flightsql.SqlInfo
		value any
	}
	entries := []entry{
		// The server. The name carries both engines, because a driver shows
		// it and the query text alone says which engine runs. The version is
		// the binary's, set by the linker, and the Arrow version is the
		// arrow-go module's.
		{flightsql.SqlInfoFlightSqlServerName, "QuasarDB REST (native engine, DuckDB through #sql)"},
		{flightsql.SqlInfoFlightSqlServerVersion, build.Version},
		{flightsql.SqlInfoFlightSqlServerArrowVersion, arrow.PkgVersion},
		// The capabilities: read-only, SQL and no Substrait, no transactions
		// in either form, no cancel and no DDL, because the Flight plane runs
		// queries and nothing else (ADR-0018, Decisions 9 and 12).
		{flightsql.SqlInfoFlightSqlServerReadOnly, true},
		{flightsql.SqlInfoFlightSqlServerSql, true},
		{flightsql.SqlInfoFlightSqlServerSubstrait, false},
		{flightsql.SqlInfoFlightSqlServerTransaction, int32(flightsql.SqlTransactionNone)},
		{flightsql.SqlInfoTransactionsSupported, false},
		{flightsql.SqlInfoFlightSqlServerCancel, false},
		{flightsql.SqlInfoDDLCatalog, false},
		{flightsql.SqlInfoDDLSchema, false},
		{flightsql.SqlInfoDDLTable, false},
		// The identifiers: case-insensitive quoted or not, and the double
		// quote as the quote character. These are DuckDB's rules (its
		// documentation, Identifiers), because DuckDB is the SQL engine the
		// drivers are told about (ADR-0018, Decision 10). A keyword and
		// function list was rejected: a static copy of DuckDB's goes stale
		// with every bump.
		{flightsql.SqlInfoIdentifierCase, int32(flightsql.SqlCaseSensitivityCaseInsensitive)},
		{flightsql.SqlInfoQuotedIdentifierCase, int32(flightsql.SqlCaseSensitivityCaseInsensitive)},
		{flightsql.SqlInfoIdentifierQuoteChar, `"`},
	}
	for _, e := range entries {
		if err := b.RegisterSqlInfo(e.id, e.value); err != nil {
			return err
		}
	}
	return nil
}
