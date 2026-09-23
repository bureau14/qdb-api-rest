package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// parseTime reads a query parameter as RFC 3339 with any fraction, which
// accepts the timestamp text the encoders write; absent is the zero time.
func parseTime(q url.Values, key string) (time.Time, error) {
	s := q.Get(key)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", key, err)
	}
	return t, nil
}

// readOptions reads the query parameters of a table read: start and end
// as times, columns as a comma-separated list, absent meaning every
// column. Whether the pair makes a range is the cluster call's to judge.
func readOptions(q url.Values) (qdb.ReadOptions, error) {
	var o qdb.ReadOptions
	var err error
	if o.Start, err = parseTime(q, "start"); err != nil {
		return o, err
	}
	if o.End, err = parseTime(q, "end"); err != nil {
		return o, err
	}
	if cols := q.Get("columns"); cols != "" {
		o.Columns = strings.Split(cols, ",")
	}
	return o, nil
}

// handleReadTable streams the table the path names, batch by batch, in
// the negotiated format. The status is decided before the first byte: the
// reader opens, and the schema is known, before the sink runs; the sink
// then holds the session for as long as the client reads.
func handleReadTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	o, err := readOptions(r.URL.Query())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	enc := negotiate(r.Header.Get("Accept"))
	cw := &countingWriter{w: w}
	// The sink's error is kept apart from the call's: the call fails before
	// anything is sent and answers a status; the sink fails a stream. The
	// sink still returns it, so the breaker and the pool hear of a fetch
	// that found the cluster gone.
	var streamErr error
	err = qdb.ClusterFrom(ctx).Read(ctx, caller(r), r.PathValue("name"), o, func(batches qdb.Batches) error {
		w.Header().Set("Content-Type", enc.ContentType())
		streamErr = enc.EncodeStream(ctx, cw, batches)
		return streamErr
	})
	switch {
	case streamErr != nil:
		// Before the first byte the failure is a fetch's or this process's:
		// 500. After it, or once the caller has left, the stream is cut and
		// only the log hears of it.
		switch {
		case ctx.Err() != nil:
			observe.Logger(ctx).DebugContext(ctx, "response abandoned by the caller", slog.Int64("bytes", cw.n), observe.Err(streamErr))
		case cw.n == 0:
			writeProblem(w, http.StatusInternalServerError, streamErr.Error())
		default:
			observe.Logger(ctx).WarnContext(ctx, "response cut mid-stream", slog.Int64("bytes", cw.n), observe.Err(streamErr))
		}
	case qdb.IsTableNotFound(err):
		writeProblem(w, http.StatusNotFound, err.Error())
	case err != nil:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// registerReadRoutes serves the table reader behind the bearer
// middleware. The mux pattern fixes method and path.
func registerReadRoutes(mux *http.ServeMux) {
	mux.Handle("GET "+tablesPath+"/{name}/rows", withCompression(requireBearer(http.HandlerFunc(handleReadTable))))
}
