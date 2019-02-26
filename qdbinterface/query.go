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

	tableCount := int64(0)

	if results != nil {
		tableCount = results.TablesCount()
	}

	queryResult.Tables = make([]*models.QueryTable, tableCount)
	if tableCount != 0 {
		for tableIdx, table := range results.Tables() {
			queryTable := models.QueryTable{}
			queryTable.Name = table.Name()
			queryTable.Columns = make([]*models.QueryColumn, table.ColumnsCount())
			columns := make([]models.QueryColumn, table.ColumnsCount())

			for idx, colName := range table.ColumnsNames() {
				columns[idx].Name = colName
				columns[idx].Data = make([]interface{}, len(table.Rows()))
			}
			for rowIdx, row := range table.Rows() {
				for colIdx, col := range table.Columns(row) {
					columns[colIdx].Data[rowIdx] = col.Get().Value()
				}
			}
			for idx := range columns {
				queryTable.Columns[idx] = &columns[idx]
			}
			queryResult.Tables[tableIdx] = &queryTable
		}
	}
	return &queryResult, nil
}

// QueryData : send a query to the server
func QueryData(handle qdb.HandleType, query string) (*models.QueryResult, error) {
	if strings.HasPrefix(query, "find") {
		return runFind(handle, query)
	}
	return runQuery(handle, query)

}
