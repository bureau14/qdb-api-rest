package qdbinterface

import (
	"strings"

	qdb "github.com/bureau14/qdb-api-go"
	"github.com/bureau14/qdb-api-rest/models"
)

func runFind(handle qdb.HandleType, query string) (*models.QueryResult, error) {
	queryResult := models.QueryResult{}
	results, err := handle.Find().ExecuteString(query)
	if err != nil {
		return nil, err
	}
	tableCount := int64(0)

	if results != nil {
		tableCount = int64(len(results))
	}

	queryResult.Tables = make([]*models.QueryTable, tableCount)
	if tableCount != 0 {
		for tableIdx, table := range results {
			queryTable := models.QueryTable{}
			queryTable.Name = table
			queryTable.Columns = nil
			queryResult.Tables[tableIdx] = &queryTable
		}
	}
	return &queryResult, nil
}

func runQuery(handle qdb.HandleType, query string) (*models.QueryResult, error) {
	queryResult := models.QueryResult{}
	results, err := handle.Query(query).Execute()
	if err != nil {
		return nil, err
	}

	// initial columns
	columns := make([]*models.QueryColumn, results.ColumnsCount())
	for i := range columns {
		columns[i] = &models.QueryColumn{}
	}

	// set table column names and initialise data
	for i, colName := range results.ColumnsNames() {
		columns[i].Name = colName
		columns[i].Data = make([]interface{}, 0, results.RowCount())
	}

	// set column data
	for _, row := range results.Rows() {
		cols := results.Columns(row)

		for i, col := range cols {
			value := col.Get().Value()
			if col.Get().Type() == qdb.QueryResultTimestamp && value == qdb.MinTimespec() {
				columns[i].Data = append(columns[i].Data, "(void)")
			} else if col.Get().Type() == qdb.QueryResultInt64 && value == qdb.Int64Undefined() {
				columns[i].Data = append(columns[i].Data, "(undefined)")
			} else {
				columns[i].Data = append(columns[i].Data, value)
			}
		}
	}

	// Set the table results
	queryResult.Tables = make([]*models.QueryTable, 1)
	queryResult.Tables[0] = &models.QueryTable{Name: "", Columns: columns}

	return &queryResult, nil
}

// QueryData : send a query to the server
func QueryData(handle qdb.HandleType, query string) (*models.QueryResult, error) {
	if strings.HasPrefix(query, "find") {
		return runFind(handle, query)
	}
	return runQuery(handle, query)

}
