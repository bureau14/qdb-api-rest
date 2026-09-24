package httpapi

import (
	"encoding/json"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// rowsPath is the ingest: the writer's door, as the table reader is the
// reader's.
const rowsPath = "/api/v2/rows"

// maxIngestBytes caps an ingest body, the one body that is a dataset
// rather than a line of text: 64 MiB, the owner's number.
const maxIngestBytes = 64 << 20

// isCSV reports whether a Content-Type names text/csv; a charset
// parameter is neither honored nor checked.
func isCSV(contentType string) bool {
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && mt == encoding.CSVContentType
}

// pushOptions reads the push options among the URL parameters as words;
// the cluster call judges them.
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

// ingestResponse is the answer: what was written and how long the two
// halves took, in whole milliseconds.
type ingestResponse struct {
	Rows    int   `json:"rows"`
	Tables  int   `json:"tables"`
	ParseMS int64 `json:"parse_ms"`
	PushMS  int64 `json:"push_ms"`
}

// handleIngestRows pushes the body's rows to their tables in one batch
// as the bearer's user and answers the counts. The body streams into the
// parser under its cap, never read whole; the status is decided when the
// push has returned, since the answer is one small object.
func handleIngestRows(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// A non-CSV body is a clear 415 instead of a parse error.
	if !isCSV(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be text/csv")
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxIngestBytes)
	res, err := qdb.ClusterFrom(ctx).IngestCSV(ctx, caller(r), body, pushOptions(r.URL.Query()))
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
	// The cap surfaces from inside the parse, so it is classified here.
	case errors.As(err, &tooLarge):
		writeProblem(w, http.StatusRequestEntityTooLarge, err.Error())
	// A body's or an option's fault is the caller's, before any push.
	case errors.Is(err, qdb.ErrInvalidPushOptions), errors.Is(err, qdb.ErrInvalidRows):
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
