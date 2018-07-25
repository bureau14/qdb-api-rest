package qdbinterface

import (
	"fmt"
	"os"
	"time"

	qdb "github.com/bureau14/qdb-api-go"
	"github.com/bureau14/qdb-api-rest/models"
)

// QueryData : send a query to the server
func QueryData(query string) (*models.QueryResult, error) {
	queryResult := models.QueryResult{}

	uri := os.Getenv("CLUSTER_URI")
	handle, err := qdb.SetupHandle(uri, time.Duration(60*time.Second))
	if err != nil {
		return nil, err
	}
	fmt.Println("query:", query)
	results, err := handle.QueryExp(query).Execute()
	if err != nil {
		return nil, err
	}

	queryResult.Tables = make([]*models.QueryTable, results.TablesCount())
	if results.TablesCount() != 0 {
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
		fmt.Println("Sending back", len(results.Tables()[0].Rows()), "rows")
	}
	fmt.Println("-------------------------------")
	return &queryResult, nil
}
