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
// the negotiated format, as the bearer's user.
func handleReadTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The read runs inside the cluster call's sink, which holds a session
	// for as long as the client reads. Two kinds of failure come out of it
	// and they answer differently, so they are kept apart:
	//
	//  1. read the parameters; a bad time is the caller's 400 right here;
	//  2. read the table, the encoder streaming inside the sink;
	//  3. a failure of the stream: with zero bytes out it is 500, after the
	//     first byte the stream is cut and only the log hears of it;
	//  4. a failure of the call, before the sink ran and before any byte:
	//     404 for an unknown table, ADR-0010's table for the rest.

	// 1. the parameters
	o, err := readOptions(r.URL.Query())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	enc := negotiate(r.Header.Get("Accept"))
	cw := &countingWriter{w: w}
	// 2. the read. The sink keeps its own error and also returns it, so the
	// breaker and the pool hear of a fetch that found the cluster gone.
	var streamErr error
	err = qdb.ClusterFrom(ctx).Read(ctx, caller(r), r.PathValue("name"), o, func(batches qdb.Batches) error {
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
