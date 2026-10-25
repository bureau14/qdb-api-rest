// Helpers shared by encode_test.go and decode_test.go. run executes a
// query as a user, and encode runs an encoder over a batch.
// The generated tables come from internal/qdbtest/table, which also
// compares what a test read back with what it wrote.
package encoding

import (
	"bytes"
	"context"
	"iter"

	"github.com/apache/arrow-go/v18/arrow"
	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

func init() { qdbapi.SetLogger(&qdbapi.NilLogger{}) }

// failer is the part of testing.TB the helpers use, so a *testing.T and
// a *rapid.T both fit.
type failer interface {
	Helper()
	Fatalf(string, ...any)
}

// run executes a query as the given user and fails the test on error.
// It retains the batch it returns, because the sequence releases its
// batch once the sink returns, and the caller releases it.
func run(t failer, c *qdb.Cluster, u qdb.User, q string) arrow.RecordBatch {
	t.Helper()
	var out arrow.RecordBatch
	err := c.Query(context.Background(), u, q, func(seq qdb.Batches) error {
		for rec, err := range seq {
			if err != nil {
				return err
			}
			rec.Retain()
			out = rec
		}
		return nil
	})
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return out
}

// one returns a sequence of the one batch.
func one(rec arrow.RecordBatch) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) { yield(rec, nil) }
}

// encode runs the encoder over a sequence of the one batch and returns
// the body.
func encode(t failer, e Encoder, rec arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.EncodeStream(context.Background(), &buf, one(rec)); err != nil {
		t.Fatalf("%s: %v", e.ContentType(), err)
	}
	return buf.Bytes()
}
