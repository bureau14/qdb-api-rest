// The encoders share one fixture: a query run as the anonymous user and
// its encoding; the comparison with the generated table that was written
// is the fixture's (internal/qdbtest/table). Each format's own decoder
// lives with that format's test.
package encoding

import (
	"bytes"
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// failer is the slice of testing.TB the helpers need, so *testing.T and
// *rapid.T both fit.
type failer interface {
	Helper()
	Fatalf(string, ...any)
}

// run executes q as the anonymous user and fails the test on error. The
// caller releases the batch.
func run(t failer, c *qdb.Cluster, q string) arrow.RecordBatch {
	t.Helper()
	rec, err := c.Query(context.Background(), qdb.User{}, q)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return rec
}

// encode runs e over rec and returns the body.
func encode(t failer, e Encoder, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.Encode(context.Background(), &buf, rec); err != nil {
		t.Fatalf("%s: %v", e.ContentType(), err)
	}
	return buf.Bytes()
}
