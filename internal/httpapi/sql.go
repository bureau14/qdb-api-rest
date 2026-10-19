package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// handleSQL runs the body as DuckDB SQL as the bearer's user and streams
// the result batch by batch in the negotiated format.
func handleSQL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The query runs inside the cluster call's sink, as a table read does.
	// DuckDB finds every error of the statement before the sink runs, so
	// the status is decided before the first byte for each of them. Two
	// kinds of failure come out of the call, and they answer differently:
	//
	//  1. read the body as query text, as POST /api/v2/query does;
	//  2. run the query, with the encoder streaming inside the sink;
	//  3. a failure of the stream. With zero bytes out it is 500, which is
	//     where a column type no encoder writes lands. After the first
	//     byte the stream is cut and only the log hears of it;
	//  4. a failure of the call, before any byte. DuckDB's error is the
	//     caller's 400, and the instance cap and a failed attach are 503.

	// 1. the body. A non-text body is a clear 415, and the body is capped
	// and read whole.
	if !isQueryText(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be text/plain or application/sql")
		return
	}
	q, ok := readBody(w, r)
	if !ok {
		return
	}
	enc := negotiate(r.Header.Get("Accept"))
	cw := &countingWriter{w: w}
	// 2. the query
	var streamErr error
	err := qdb.ClusterFrom(ctx).Duck(ctx, caller(r), string(q), func(batches qdb.Batches) error {
		w.Header().Set("Content-Type", enc.ContentType())
		streamErr = enc.EncodeStream(ctx, cw, batches)
		return streamErr
	})
	switch {
	// 3. the stream failed
	case streamErr != nil:
		switch {
		case ctx.Err() != nil:
			observe.Logger(ctx).DebugContext(ctx, "response abandoned by the caller", slog.Int64("bytes", cw.n), observe.Err(streamErr))
		case cw.n == 0:
			writeProblem(w, http.StatusInternalServerError, streamErr.Error())
		default:
			observe.Logger(ctx).WarnContext(ctx, "response cut mid-stream", slog.Int64("bytes", cw.n), observe.Err(streamErr))
		}
	// 4. the call failed
	case err != nil:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// registerSQLRoutes serves the DuckDB endpoint behind the bearer
// middleware. The mux pattern fixes method and path.
func registerSQLRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v2/sql", withCompression(requireBearer(http.HandlerFunc(handleSQL))))
}
