package httpapi

import (
	"encoding/json"
	"errors"
	"io"
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

// rowsPath is the path of the ingest, which writes the rows that the
// table reader reads.
const rowsPath = "/api/v2/rows"

// maxIngestBytes caps an ingest body, the only body that carries a
// dataset instead of a line of text. The owner chose 64 MiB.
const maxIngestBytes = 64 << 20

// decoders maps each media type the ingest accepts to its decoder.
var decoders = map[string]encoding.Decoder{
	encoding.CSVContentType:    encoding.CSV{},
	encoding.NDJSONContentType: encoding.NDJSON{},
	encoding.ArrowContentType:  encoding.Arrow{},
	encoding.JSONContentType:   encoding.JSON{},
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

// pushOptions reads the push options from the URL parameters as plain
// strings. The cluster call validates them.
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

// ingestResponse is the ingest's answer. It counts the rows and tables
// written and gives the parse and push durations in whole milliseconds.
type ingestResponse struct {
	Rows    int   `json:"rows"`
	Tables  int   `json:"tables"`
	ParseMS int64 `json:"parse_ms"`
	PushMS  int64 `json:"push_ms"`
}

// handleIngestRows pushes the body's rows to their tables in one batch
// as the bearer's user and answers the counts. The body streams into
// the decoder under its cap, decompressed when a Content-Encoding says
// so, and is never read whole. The status is decided when the push has
// returned, because the answer is one small object.
func handleIngestRows(w http.ResponseWriter, r *http.Request) {
	// The handler checks both headers before it reads a byte, wraps the
	// body from the wire inward, and runs the decoder inside the session
	// lease:
	//
	//  1. a body of a type no decoder reads gets a clear 415 instead of a
	//     decode error;
	//  2. a Content-Encoding this server does not read is also 415, with a
	//     detail that names gzip and zstd;
	//  3. the ingest cap bounds the bytes on the wire. The decoded size of
	//     a hostile body is not bounded, which is an accepted cost because
	//     the brief puts this server inside a customer network;
	//  4. the decompressor wraps the capped body. Opening it can fail on a
	//     corrupt gzip header, which is the body's fault and 400. It is
	//     closed when the handler returns;
	//  5. one cluster call runs the decoder with the held session's schema
	//     lookup;
	//  6. the status follows who failed. A corrupt coding surfaces from the
	//     decode as ErrInvalidRows, so it needs no case of its own.
	ctx := r.Context()

	// 1. the body's type
	dec, ok := decoderOf(r.Header.Get("Content-Type"))
	if !ok {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be one of "+acceptedTypes())
		return
	}

	// 2. the body's coding
	c, ok := requestCoding(r.Header.Get("Content-Encoding"))
	if !ok {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Encoding must be gzip, zstd or identity")
		return
	}

	// 3. the cap, on the bytes on the wire
	var body io.Reader = http.MaxBytesReader(w, r.Body, maxIngestBytes)

	// 4. the decompressor, closed on return
	if c != identityCoding {
		z, err := newDecompressor(c, body)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, err.Error())
			return
		}
		// A close error has no one left to tell, because the decode has
		// read what it needed and the status is decided by that.
		defer func() { _ = z.Close() }()
		body = z
	}

	// 5. one call under the held session's lookup
	decode := func(schemaOf model.SchemaOf) ([]model.TableBatch, error) {
		return dec.Decode(ctx, body, schemaOf)
	}
	res, err := qdb.ClusterFrom(ctx).Ingest(ctx, caller(r), pushOptions(r.URL.Query()), decode)

	// 6. the status by who failed
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
	// An invalid body or invalid options are the caller's fault, found
	// before any push.
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
