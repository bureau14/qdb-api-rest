package prometheus

import (
	"testing"
	"time"

	qdb "github.com/bureau14/qdb-api-go/v3"

	"github.com/prometheus/common/model"
	prom "github.com/prometheus/prometheus/prompb"
)

const clusterURI = "qdb://127.0.0.1:2836"

func getColumnsByType(table qdb.TimeseriesEntry) (map[string]qdb.TsColumnType, error) {
	colsInfo, err := table.ColumnsInfo()
	if err != nil { 
		return nil, err
	}

	cols := make(map[string]qdb.TsColumnType, len(colsInfo))
	for _, col := range colsInfo {
		cols[col.Name()] = col.Type()
	}
	return cols, nil
}

func toUtcTime(milliseconds int64) time.Time {
	sec := milliseconds / 1000
	nsec := (milliseconds - (sec * 1000)) * 1000000
	return time.Unix(sec, nsec).UTC()
}

func toMilliseconds(time time.Time) int64 {
	return time.UnixNano() / 1000000
}

func TestBuildQuasarDbQuery(t *testing.T) {
	start := time.Now()
	end := start.Add(time.Hour * 24)

	q := prom.Query{
		StartTimestampMs: toMilliseconds(start),
		EndTimestampMs:   toMilliseconds(end),
		Matchers: []*prom.LabelMatcher{
			{
				Type:  prom.LabelMatcher_EQ,
				Name:  model.MetricNameLabel,
				Value: "example_metric",
			},
			{
				Type:  prom.LabelMatcher_EQ,
				Name:  "tag_one",
				Value: "one",
			},
			{
				Type:  prom.LabelMatcher_NEQ,
				Name:  "tag_two",
				Value: "2",
			},
		},
	}

	quasarQuery, metricName, err := buildQuasarDbQuery(&q)
	if err != nil {
		t.Fatalf("Failed to build query")
	}

	if metricName != "example_metric" {
		t.Fatalf("Incorrect metric name, expected 'example_metic' and got '%s'", metricName)
	}

	expectedResult := "SELECT * FROM $qdb.prom.example_metric WHERE WHERE tag_one = 'one' AND WHERE tag_two != '2'"
	if quasarQuery != expectedResult {
		t.Fatalf("Incorrect query result, got '%s' and expected '%s'", quasarQuery, expectedResult)
	}
}

func TestWrite(t *testing.T) {
	client := Client{ClusterURI: clusterURI}
	handle, err := client.GetHandle()
	if err != nil {
		t.Fatalf("Failed to retrieve handle: %s", err.Error())
	}

	metricOneTable := handle.Timeseries("$qdb.prom.metric_one")
	metricOneTable.Remove()
	metricTwoTable := handle.Timeseries("$qdb.prom.metric_two")
	metricTwoTable.Remove()

	t1 := time.Now()
	t2 := t1.Add(1 * time.Hour)
	t3 := t2.Add(1 * time.Hour)

	tses := []prom.TimeSeries{
		{
			Labels: []prom.Label{
				{Name: model.MetricNameLabel, Value: "metric_one"},
				{Name: "label_one", Value: "1"},
			},
			Samples: []prom.Sample{
				{Timestamp: toMilliseconds(t1), Value: 1.1},
				{Timestamp: toMilliseconds(t2), Value: 1.2},
				{Timestamp: toMilliseconds(t3), Value: 1.3},
			},
		},
		{
			Labels: []prom.Label{
				{Name: model.MetricNameLabel, Value: "metric_one"},
				{Name: "label_one", Value: "one"},
				{Name: "label_two", Value: "two"},
			},
			Samples: []prom.Sample{
				{Timestamp: toMilliseconds(t1), Value: 2.1},
				{Timestamp: toMilliseconds(t2), Value: 2.2},
				{Timestamp: toMilliseconds(t3), Value: 2.3},
			},
		},
		{
			Labels: []prom.Label{
				{Name: model.MetricNameLabel, Value: "metric_two"},
				{Name: "other", Value: "otherone"},
				{Name: "other_two", Value: "othertwo"},
			},
			Samples: []prom.Sample{
				{Timestamp: toMilliseconds(t1), Value: 2.1},
				{Timestamp: toMilliseconds(t2), Value: 2.2},
				{Timestamp: toMilliseconds(t3), Value: 2.3},
			},
		},
	}

	err = client.Write(tses)
	if err != nil {
		t.Errorf("Failed somewhere: %s", err.Error())
	}

	t.Logf("Success Timeseries write")
}

func TestEnsureMetricEmptyTable(t *testing.T) {
	c := Client{ClusterURI: clusterURI}
	handle, err := c.GetHandle()
	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	table := handle.Timeseries("$qdb.prom.test_metric")
	_ = table.Remove()

	ts := prom.TimeSeries{
		Labels: []prom.Label{
			{Name: model.MetricNameLabel, Value: "test_metric"},
			{Name: "test_tag", Value: "hello"},
		},
	}

	err = c.EnsureTable(ts)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	cols, err := getColumnsByType(table)
	if err != nil {
		if err == qdb.ErrAliasNotFound {
			t.Fatalf("Failed to create table")
		}
		t.Fatalf("Error: %s", err)
	}

	if cols["test_tag"] != qdb.TsColumnBlob {
		t.Fatalf("Failed to create blob column test_tag")
	}

	if cols["value"] != qdb.TsColumnDouble {
		t.Fatalf("Failed to create double column value")
	}

	t.Logf("Success")
}

func TestEnsureMetricExistingTable(t *testing.T) {
	c := Client{ClusterURI: clusterURI}
	handle, err := c.GetHandle()
	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	table := handle.Timeseries("$qdb.prom.test_metric")
	_ = table.Remove()

	err = table.Create(24*time.Hour,
		qdb.NewTsColumnInfo("tag_one", qdb.TsColumnBlob),
		qdb.NewTsColumnInfo("tag_two", qdb.TsColumnBlob),
		qdb.NewTsColumnInfo("tag_three", qdb.TsColumnBlob),
		qdb.NewTsColumnInfo("value", qdb.TsColumnDouble),
	)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	ts := prom.TimeSeries{
		Labels: []prom.Label{
			{Name: model.MetricNameLabel, Value: "test_metric"},
			{Name: "tag_one", Value: "one"},
			{Name: "tag_two", Value: "two"},
			{Name: "tag_three", Value: "three"},
		},
	}

	err = c.EnsureTable(ts)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	cols, err := getColumnsByType(table)
	if err != nil {
		if err == qdb.ErrAliasNotFound {
			t.Fatalf("Failed to create table")
		}
		t.Fatalf("Error: %s", err)
	}

	expected := map[string]qdb.TsColumnType{
		"tag_one":   qdb.TsColumnBlob,
		"tag_two":   qdb.TsColumnBlob,
		"tag_three": qdb.TsColumnBlob,
		"value":     qdb.TsColumnDouble,
	}

	for name, typ := range expected {
		if cols[name] != typ {
			t.Fatalf("Missing or incorrect column %s", name)
		}
	}

	if len(cols) != 4 {
		t.Fatalf("Unexpected column count: got %d, want 4", len(cols))
	}

	t.Logf("Success")
}

func TestEnsureMetricExistingTableMissingColumns(t *testing.T) {
	c := Client{ClusterURI: clusterURI}
	handle, err := c.GetHandle()
	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	table := handle.Timeseries("$qdb.prom.test_metric")
	table.Remove()
	err = table.Create(24*time.Hour,
		qdb.NewTsColumnInfo("tag_two", qdb.TsColumnBlob),
	)
	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	ts := prom.TimeSeries{
		Labels: []prom.Label{
			prom.Label{Name: model.MetricNameLabel, Value: "test_metric"},
			prom.Label{Name: "tag_one", Value: "one"},
			prom.Label{Name: "tag_two", Value: "two"},
			prom.Label{Name: "tag_three", Value: "three"},
		},
	}

	err = c.EnsureTable(ts)

	if err != nil {
		t.Fatalf("Error: %s", err)
	}

	doubleCols, blobCols, _, _, _, err := table.Columns()
	if err != nil {
		if err == qdb.ErrAliasNotFound {
			t.Fatalf("Failed to create table")
		}
		t.Fatalf("Error: %s", err)
	}

	var colCount int
	for _, label := range []string{"tag_one", "tag_two", "tag_three"} {
		var hasColumn bool
		for _, col := range blobCols {
			if col.Name() == label {
				hasColumn = true
			}
		}
		if hasColumn {
			colCount++
		}
	}

	var hasColumn bool
	for _, col := range doubleCols {
		if col.Name() == "value" {
			hasColumn = true
		}
	}
	if hasColumn {
		colCount++
	}

	if colCount != 4 {
		t.Fatalf("Failed to create all columns")
	}

	t.Logf("Success")
}
