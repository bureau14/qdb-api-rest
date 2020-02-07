package prometheus

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/prometheus/prometheus/prompb"
	prom "github.com/prometheus/prometheus/prompb"

	qdb "github.com/bureau14/qdb-api-go"
	"github.com/prometheus/common/model"
)

// Client represents a QuasarDB Prometheus client that knows how to read and
// write Promethus requests and responses
type Client struct {
	ClusterURI string
	Handle     *qdb.HandleType
	Logger     func(string, ...interface{})
	mutex      sync.Mutex
}

// Write takes a slice prometheus Timeseries and writes them to QuasarDB
func (c *Client) Write(tses []prom.TimeSeries) error {
	for _, ts := range tses {
		var tableName string
		var labelNames []string
		var labelValues []string
		for _, label := range ts.Labels {
			if label.Name == model.MetricNameLabel {
				tableName = fmt.Sprintf("$qdb.prom.%s", label.Value)
			} else {
				labelNames = append(labelNames, label.Name)
				labelValues = append(labelValues, label.Value)
			}
		}

		if tableName == "" {
			return fmt.Errorf("Timeseries had no metric name")
		}

		handle, err := c.GetHandle()
		if err != nil {
			return fmt.Errorf("Failed to retrieve qdb handle: %s", err.Error())
		}

		err = c.EnsureTable(&ts)
		if err != nil {
			return err
		}

		var tsBatchColInfo []qdb.TsBatchColumnInfo
		for _, labelName := range labelNames {
			tsBatchColInfo = append(tsBatchColInfo, qdb.NewTsBatchColumnInfo(tableName, labelName, int64(len(ts.Samples))))
		}
		tsBatchColInfo = append(tsBatchColInfo, qdb.NewTsBatchColumnInfo(tableName, "value", int64(len(ts.Samples))))

		tsBatch, err := handle.TsBatch(tsBatchColInfo...)

		if err != nil {
			return fmt.Errorf("Failed to created qdb.TsBatch: %s", err.Error())
		}

		for _, sample := range ts.Samples {
			timestamp := model.Time(sample.Timestamp).Time()
			err = tsBatch.StartRow(timestamp)
			if err != nil {
				return err
			}
			var i int64
			for _, col := range labelValues {
				err = tsBatch.RowSetBlob(i, []byte(col))
				if err != nil {
					return err
				}
				i++
			}
			err = tsBatch.RowSetDouble(i, sample.Value)
			if err != nil {
				return err
			}
		}

		if err != nil {
			return fmt.Errorf("Failed to set TsBatch rows: %s", err.Error())
		}

		err = tsBatch.Push()
		if err != nil {
			return fmt.Errorf("Failed to flush TsBatch: %s", err.Error())
		}
	}

	return nil
}

// GetHandle caches and returns an anonymous user qdb handle
func (c *Client) GetHandle() (*qdb.HandleType, error) {
	c.mutex.Lock()
	if c.Handle == nil {
		handle, err := qdb.SetupHandle(c.ClusterURI, time.Duration(12)*time.Hour)
		if err != nil {
			return nil, err
		}
		c.Handle = &handle
	}
	c.mutex.Unlock()

	return c.Handle, nil
}

// EnsureTable ensures the prometheus metric table and required columns exist
func (c *Client) EnsureTable(ts *prom.TimeSeries) error {
	var tableName string
	var columnNames []string

	for _, label := range ts.Labels {
		labelName := label.GetName()

		if labelName == model.MetricNameLabel {
			tableName = label.GetValue()
		} else {
			columnNames = append(columnNames, labelName)
		}
	}
	sort.Strings(columnNames)

	if tableName == "" {
		return fmt.Errorf("prometheus timeseries missing metric name")
	}

	handle, err := c.GetHandle()
	if err != nil {
		return err
	}

	table := handle.Timeseries(fmt.Sprintf("$qdb.prom.%s", tableName))
	doubleCols, blobCols, _, _, _, err := table.Columns()

	// Unexpected error
	if err != nil && err != qdb.ErrAliasNotFound {
		return err
	}

	// Timeseries doesn't exist so we create it
	if err == qdb.ErrAliasNotFound {
		var colsInfo []qdb.TsColumnInfo
		for _, name := range columnNames {
			colsInfo = append(colsInfo, qdb.NewTsColumnInfo(name, qdb.TsColumnBlob))
		}
		colsInfo = append(colsInfo, qdb.NewTsColumnInfo("value", qdb.TsColumnDouble))

		err = table.Create(24*time.Hour, colsInfo...)
		return err
	}

	// Add any missing prometheus labels as blob columns
	var newColsInfo []qdb.TsColumnInfo
	for _, label := range columnNames {
		var hasColumn bool
		for _, col := range blobCols {
			if label == col.Name() {
				hasColumn = true
			}
		}
		if !hasColumn {
			newColsInfo = append(newColsInfo, qdb.NewTsColumnInfo(label, qdb.TsColumnBlob))
		}
	}

	var hasValueColumn bool
	for _, col := range doubleCols {
		if col.Name() == "value" {
			hasValueColumn = true
		}
	}
	if !hasValueColumn {
		newColsInfo = append(newColsInfo, qdb.NewTsColumnInfo("value", qdb.TsColumnDouble))
	}

	// Table already has all the columns it needs
	if len(newColsInfo) == 0 {
		return nil
	}

	err = table.InsertColumns(newColsInfo...)
	return err
}

// Read takes a Prometheus read request, fetches the corresponding data from the
// client's configured QuasarDB server daemon and returns Promethus read response
func (c *Client) Read(req *prom.ReadRequest) (*prom.ReadResponse, error) {
	handle, err := c.GetHandle()
	if err != nil {
		return nil, err
	}
	labelsToSeries := map[string]*prom.TimeSeries{}

	for _, query := range req.Queries {
		qdbQuery, name, err := buildQuasarDbQuery(query)
		if err != nil {
			c.Logger("Failed to build query: %+v", *query)
			return nil, err
		}

		q := handle.Query(qdbQuery)
		table, err := q.Execute()
		if err != nil {
			c.Logger("Failed to execute query: %s", qdbQuery)
			return nil, err
		}
		defer handle.Release(unsafe.Pointer(table))

		// query only ever has one table result
		colNames := table.ColumnsNames()

		for _, row := range table.Rows() {
			columns := table.Columns(row)
			var (
				time   int64
				value  float64
				labels = make(map[string]string)
			)

			// Read row values
			for i, colName := range colNames {
				if colName == "$timestamp" {
					timestamp, err := columns[i].GetTimestamp()
					if err != nil {
						return nil, err
					}
					time = timestamp.UnixNano() / 1000000
				} else if colName == "value" {
					double, err := columns[i].GetDouble()
					if err != nil {
						return nil, err
					}
					value = double
				} else if colName != "$table" {
					blob, err := columns[i].GetBlob()
					if err == nil {
						labels[colName] = string(blob)
					}
				}
			}

			key := promTimeSeriesKey(name, labels)
			ts, ok := labelsToSeries[key]

			// Initialise timeseries if it doesn't exist yet
			if !ok {
				labelPairs := make([]prom.Label, 0, len(labels)+1)
				labelPairs = append(labelPairs, prom.Label{
					Name:  model.MetricNameLabel,
					Value: name,
				})

				for k, v := range labels {
					labelPairs = append(labelPairs, prom.Label{
						Name:  k,
						Value: v,
					})
				}

				ts = &prompb.TimeSeries{
					Labels:  labelPairs,
					Samples: make([]prom.Sample, 0, 100),
				}
				labelsToSeries[key] = ts
			}

			// Append samples
			ts.Samples = append(ts.Samples, prom.Sample{
				Timestamp: time,
				Value:     value,
			})
		}
	}

	resp := prom.ReadResponse{
		Results: []*prom.QueryResult{
			{
				Timeseries: make([]*prom.TimeSeries, 0, len(labelsToSeries)),
			},
		},
	}

	// Build result
	for _, ts := range labelsToSeries {
		resp.Results[0].Timeseries = append(resp.Results[0].Timeseries, ts)
	}

	return &resp, nil
}

// buildQuasarDbQuery constructs the required QuasarDB SQL from a prometheus query
func buildQuasarDbQuery(q *prom.Query) (string, string, error) {
	var query string
	var metricName string
	var tableName string
	var whereConditions = make([]string, 0, len(q.Matchers)-1)

	for _, matcher := range q.Matchers {
		if matcher.Name == model.MetricNameLabel {
			switch matcher.Type {
			case prom.LabelMatcher_EQ:
				metricName = matcher.Value
				tableName = fmt.Sprintf("$qdb.prom.%s", matcher.Value)
			case prom.LabelMatcher_NEQ:
				return query, metricName, fmt.Errorf("unsupported matcher NOT EQ on metric name")
			default:
				return query, metricName, fmt.Errorf("unknown metric type %v", matcher.Type)
			}
		} else {
			switch matcher.Type {
			case prom.LabelMatcher_EQ:
				condition := fmt.Sprintf("%s = '%s'", matcher.Name, matcher.Value)
				whereConditions = append(whereConditions, condition)
			case prom.LabelMatcher_NEQ:
				condition := fmt.Sprintf("%s != '%s'", matcher.Name, matcher.Value)
				whereConditions = append(whereConditions, condition)
			default:
				return query, metricName, fmt.Errorf("unknown metric type %v", matcher.Type)
			}
		}
	}

	if metricName == "" {
		return query, metricName, fmt.Errorf("queries without a metric name are not supported")
	}

	var where string
	if len(whereConditions) > 0 {
		where = fmt.Sprintf(" WHERE %s ", strings.Join(whereConditions, " AND "))
	}

	query = fmt.Sprintf("SELECT * FROM %s%s", tableName, where)

	return query, metricName, nil
}

// Helper methods

func newTimeFromUnixNs(timestamp int64) time.Time {
	return model.Time(timestamp).Time()
}

// Creates a key unique to a set label key-value pairs
func promTimeSeriesKey(metric string, labels map[string]string) string {
	// 0xff cannot occur in valid UTF-8 sequences, so use it as a separator.
	separator := "\xff"

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	pairs := make([]string, 0, len(labels))
	for _, k := range keys {
		pairs = append(pairs, k+separator+labels[k])
	}

	return metric + separator + strings.Join(pairs, separator)
}
