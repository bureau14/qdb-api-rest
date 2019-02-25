package qdbinterface

import (
	qdb "github.com/bureau14/qdb-api-go"
	"github.com/bureau14/qdb-api-rest/models"
)

// Find : Execute a find query on tags
func Find(handle qdb.HandleType, query string) (*models.QueryResult, error) {
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
