package qdbinterface

import (
	"fmt"
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

	tableMap := make(map[string][]*models.QueryColumn)
	tableNames := make([]string, 0, 10)

	// get table names
	for i, row := range results.Rows() {
		columns := results.Columns(row)
		b, err := columns[1].GetBlob()
		if err != nil {
			fmt.Printf("Failed to get column name. Row: %v", i)
			continue
		}
		tableNames = append(tableNames, string(b))
	}

	// initialise results container
	for _, name := range tableNames {
		tableMap[name] = make([]*models.QueryColumn, results.ColumnsCount()-1)
		for i := range tableMap[name] {
			tableMap[name][i] = &models.QueryColumn{}
		}
		for i, colName := range results.ColumnsNames() {
			switch i {
			case 0:
				tableMap[name][i].Name = colName
				tableMap[name][i].Data = make([]interface{}, 0, results.RowCount())
			case 1:
				continue
			default:
				tableMap[name][i-1].Name = colName
				tableMap[name][i-1].Data = make([]interface{}, 0, results.RowCount())
			}
		}
	}

	// set the values from the results
	for _, row := range results.Rows() {
		columns := results.Columns(row)
		b, err := columns[1].GetBlob()
		if err != nil {
			continue
		}

		name := string(b)

		for j, col := range columns {
			switch j {
			case 0:
				value := col.Get().Value()
				if col.Get().Type() == qdb.QueryResultTimestamp && value == qdb.MinTimespec() {
					tableMap[name][j].Data = append(tableMap[name][j].Data, "(void)")
				} else if col.Get().Type() == qdb.QueryResultInt64 && value == qdb.Int64Undefined() {
					tableMap[name][j].Data = append(tableMap[name][j].Data, "(undefined)")
				} else {
					tableMap[name][j].Data = append(tableMap[name][j].Data, value)
				}
			case 1:
				continue
			default:
				value := col.Get().Value()
				if col.Get().Type() == qdb.QueryResultTimestamp && value == qdb.MinTimespec() {
					tableMap[name][j-1].Data = append(tableMap[name][j-1].Data, "(void)")
				} else if col.Get().Type() == qdb.QueryResultInt64 && value == qdb.Int64Undefined() {
					tableMap[name][j-1].Data = append(tableMap[name][j-1].Data, "(undefined)")
				} else {
					tableMap[name][j-1].Data = append(tableMap[name][j-1].Data, value)
				}
			}
		}
	}

	// Set the table results
	queryResult.Tables = make([]*models.QueryTable, len(tableNames))
	for i, name := range tableNames {
		queryResult.Tables[i] = &models.QueryTable{Name: name, Columns: tableMap[name]}
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
