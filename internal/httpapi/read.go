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
	"github.com/bureau14/qdb-api-rest/internal/query"
)

// parseTime reads one URL parameter as RFC 3339 with any fraction, which
// accepts the timestamp text the encoders write. An absent parameter is
// the zero time.
func parseTime(params url.Values, key string) (time.Time, error) {
	s := params.Get(key)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", key, err)
	}
	return t, nil
}

// readOptions reads the URL parameters of a table read. It reads start
// and end as times and columns as a comma-separated list. An absent
// columns parameter means every column. The cluster call decides
// whether start and end form a range.
func readOptions(params url.Values) (qdb.ReadOptions, error) {
	var o qdb.ReadOptions
	var err error
	if o.Start, err = parseTime(params, "start"); err != nil {
		return o, err
	}
	if o.End, err = parseTime(params, "end"); err != nil {
		return o, err
	}
	if cols := params.Get("columns"); cols != "" {
		o.Columns = strings.Split(cols, ",")
	}
	return o, nil
}

// readAttrs are the attributes of a table read's query line: the table,
// and the start and the end that the read sets, in RFC 3339 and UTC as
// the parameters take them. A read without a range has no bounds to name,
// so the line carries neither.
func readAttrs(name string, o qdb.ReadOptions) []slog.Attr {
	attrs := []slog.Attr{slog.String(observe.KeyTable, name)}
	if !o.Start.IsZero() {
		attrs = append(attrs, slog.String(observe.KeyStart, o.Start.UTC().Format(time.RFC3339Nano)))
	}
	if !o.End.IsZero() {
		attrs = append(attrs, slog.String(observe.KeyEnd, o.End.UTC().Format(time.RFC3339Nano)))
	}
	return attrs
}

// handleReadTable streams the table the path names, batch by batch, in
// the negotiated format, as the bearer's user.
func handleReadTable(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The read runs inside the cluster call's sink, which holds a session
	// for as long as the client reads. A stream failure and a call failure
	// answer differently:
	//
	//  1. read the parameters. A bad time is the caller's 400, answered
	//     here;
	//  2. read the table through query.Logged, with the encoder streaming
	//     inside the sink;
	//  3. a stream failure is 500 while no byte has gone out. After the
	//     first byte the stream is cut and the failure is only logged;
	//  4. a call failure arrives before the sink runs. An unknown table is
	//     404, and writeClusterError answers the rest by ADR-0010's table.

	// 1. the parameters
	o, err := readOptions(r.URL.Query())
	if err != nil {
		writeProblem(w, http.StatusBadRequest, err.Error())
		return
	}
	enc := negotiate(r.Header.Get("Accept"))
	cw := &countingWriter{w: w}
	// 2. the read, through query.Logged, which writes the query line with
	//    engine bulk reader, the table and the range, because a table read
	//    is a data read like a query. The sink keeps its own error and also
	//    returns it, so the breaker and the pool hear of a fetch that found
	//    the cluster gone.
	name := r.PathValue("name")
	var streamErr error
	err = query.Logged(ctx, observe.EngineBulkReader, func(sink func(qdb.Batches) error) error {
		return qdb.ClusterFrom(ctx).Read(ctx, caller(r), name, o, sink)
	}, func(batches qdb.Batches) error {
		w.Header().Set("Content-Type", enc.ContentType())
		streamErr = enc.EncodeStream(ctx, cw, batches)
		return streamErr
	}, readAttrs(name, o)...)
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
