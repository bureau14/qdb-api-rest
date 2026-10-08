package httpapi

import (
	"encoding/json"
	"errors"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/model"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// rowsPath is the ingest. It is the writer's door, as the table reader
// is the reader's.
const rowsPath = "/api/v2/rows"

// maxIngestBytes caps an ingest body, which is the one body that is a
// dataset rather than a line of text. 64 MiB is the owner's number.
const maxIngestBytes = 64 << 20

// decoders maps each media type the ingest accepts to its decoder.
var decoders = map[string]encoding.Decoder{
	encoding.CSVContentType: encoding.CSV{},
}

// decoderOf picks the decoder for a Content-Type by media type alone. A
// charset parameter is neither honored nor checked. The second value is
// false for a type the ingest does not accept.
func decoderOf(contentType string) (encoding.Decoder, bool) {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, false
	}
	d, ok := decoders[mt]
	return d, ok
}

// acceptedTypes lists the keys of decoders, sorted, for the 415 detail.
func acceptedTypes() string {
	return strings.Join(slices.Sorted(maps.Keys(decoders)), ", ")
}

// pushOptions reads the push options among the URL parameters as words.
// The cluster call judges them.
func pushOptions(params url.Values) qdb.PushOptions {
	o := qdb.PushOptions{
		Mode:              params.Get("push-mode"),
		DeduplicationMode: params.Get("deduplication-mode"),
	}
	if cols := params.Get("deduplication-columns"); cols != "" {
		o.DeduplicationColumns = strings.Split(cols, ",")
	}
	return o
}

// ingestResponse is the answer. It says what was written and how long
// the two halves took, in whole milliseconds.
type ingestResponse struct {
	Rows    int   `json:"rows"`
	Tables  int   `json:"tables"`
	ParseMS int64 `json:"parse_ms"`
	PushMS  int64 `json:"push_ms"`
}

// handleIngestRows pushes the body's rows to their tables in one batch
// as the bearer's user and answers the counts. The body streams into
// the decoder under its cap and is never read whole. The status is
// decided when the push has returned, because the answer is one small
// object.
func handleIngestRows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// 1. a body of a type no decoder reads gets a clear 415 instead of a
	// decode error
	dec, ok := decoderOf(r.Header.Get("Content-Type"))
	if !ok {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be one of "+acceptedTypes())
		return
	}
	// 2. the body under the ingest cap
	body := http.MaxBytesReader(w, r.Body, maxIngestBytes)
	// 3. one call, in which the decoder runs under the held session's
	// schema lookup
	decode := func(schemaOf model.SchemaOf) ([]model.TableBatch, error) {
		return dec.Decode(ctx, body, schemaOf)
	}
	res, err := qdb.ClusterFrom(ctx).Ingest(ctx, caller(r), pushOptions(r.URL.Query()), decode)
	// 4. the status by who failed
	var tooLarge *http.MaxBytesError
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ingestResponse{
			Rows:    res.Rows,
			Tables:  res.Tables,
			ParseMS: res.Parse.Milliseconds(),
			PushMS:  res.Push.Milliseconds(),
		})
	// The cap surfaces from inside the decode, so it is classified here.
	case errors.As(err, &tooLarge):
		writeProblem(w, http.StatusRequestEntityTooLarge, err.Error())
	// A body's or an option's fault is the caller's, before any push.
	case errors.Is(err, qdb.ErrInvalidPushOptions), errors.Is(err, encoding.ErrInvalidRows):
		writeProblem(w, http.StatusBadRequest, err.Error())
	// A $table the cluster does not know fails the whole request.
	case qdb.IsTableNotFound(err):
		writeProblem(w, http.StatusNotFound, err.Error())
	default:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// registerRowsRoutes serves the ingest behind the bearer middleware. The
// mux pattern fixes method and path.
func registerRowsRoutes(mux *http.ServeMux) {
	mux.Handle("POST "+rowsPath, withCompression(requireBearer(http.HandlerFunc(handleIngestRows))))
}
