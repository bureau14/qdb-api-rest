// The stream path of every encoder is pinned on the hand-built edge batch,
// no cluster: a sequence of two batches renders as the two one-shot
// renderings joined the way the format joins them, and an error step
// surfaces its error.
package encoding

import (
	"bytes"
	"context"
	"errors"
	"iter"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
)

// steps is a sequence of recs, then err when non-nil.
func steps(recs []arrow.RecordBatch, err error) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		for _, rec := range recs {
			if !yield(rec, nil) {
				return
			}
		}
		if err != nil {
			yield(nil, err)
		}
	}
}

// encodeStream runs e's stream path over batches and returns the body.
func encodeStream(t *testing.T, e Encoder, batches iter.Seq2[arrow.RecordBatch, error]) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := e.EncodeStream(context.Background(), &buf, batches); err != nil {
		t.Fatalf("%s: %v", e.ContentType(), err)
	}
	return buf.Bytes()
}

// ipcStream is the IPC stream of recs written directly: one schema, one
// record batch each, the marker.
func ipcStream(t *testing.T, recs ...arrow.RecordBatch) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(recs[0].Schema()))
	for _, rec := range recs {
		if err := w.Write(rec); err != nil {
			t.Fatalf("ipc write: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("ipc close: %v", err)
	}
	return buf.Bytes()
}

// TestStreamIsBatchesJoined: two batches stream as the IPC stream with
// two record batches, the CSV with one header, the NDJSON lines
// appended, and the JSON array of two results.
func TestStreamIsBatchesJoined(t *testing.T) {
	rec := edgeBatch(t)
	csv := encode(t, CSV{}, rec)
	header := csv[:bytes.IndexByte(csv, '\n')+1]
	ndjson := encode(t, NDJSON{}, rec)
	json := encode(t, JSON{}, rec)
	for _, tc := range []struct {
		e    Encoder
		want []byte
	}{
		{Arrow{}, ipcStream(t, rec, rec)},
		{CSV{}, append(csv[:len(csv):len(csv)], csv[len(header):]...)},
		{NDJSON{}, append(ndjson[:len(ndjson):len(ndjson)], ndjson...)},
		{JSON{}, []byte("[" + string(json) + "," + string(json) + "]")},
	} {
		if got := encodeStream(t, tc.e, steps([]arrow.RecordBatch{rec, rec}, nil)); !bytes.Equal(got, tc.want) {
			t.Errorf("%s:\n got %q\nwant %q", tc.e.ContentType(), got, tc.want)
		}
	}
}

// TestStreamErrorStep: an error step after a batch ends every encoder's
// stream with that error.
func TestStreamErrorStep(t *testing.T) {
	rec := edgeBatch(t)
	errStep := errors.New("fetch failed")
	for _, e := range []Encoder{Arrow{}, CSV{}, NDJSON{}, JSON{}} {
		var buf bytes.Buffer
		if err := e.EncodeStream(context.Background(), &buf, steps([]arrow.RecordBatch{rec}, errStep)); !errors.Is(err, errStep) {
			t.Errorf("%s: error %v, want %v", e.ContentType(), err, errStep)
		}
	}
}
