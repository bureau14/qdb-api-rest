// Helpers shared by encode_test.go and decode_test.go. run executes a
// query as the anonymous user, and encode runs an encoder over a batch.
// The generated tables come from internal/qdbtest/table, which also
// compares what a test read back with what it wrote.
package encoding

import (
	"bytes"
	"context"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// failer is the part of testing.TB the helpers use, so a *testing.T and
// a *rapid.T both fit.
type failer interface {
	Helper()
	Fatalf(string, ...any)
}

// run executes q as the fixture's caller (qdbtest.Caller) and fails the
// test on error. The caller releases the batch it returns.
func run(t failer, c *qdb.Cluster, q string) arrow.RecordBatch {
	t.Helper()
	name, secret := qdbtest.Caller()
	rec, err := c.Query(context.Background(), qdb.User{Username: name, SecretKey: secret}, q)
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
