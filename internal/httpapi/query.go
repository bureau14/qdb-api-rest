package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// maxBodyBytes caps every request body: a QuasarDB query is a line of
// text and a login is two fields, and one line bounds what a request can
// make the server read.
const maxBodyBytes = 1 << 20

// readBody reads a capped request body whole, answering 413 over the cap
// and 400 when it cannot be read; false means the response is written.
func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeProblem(w, http.StatusRequestEntityTooLarge, err.Error())
		return nil, false
	case err != nil:
		writeProblem(w, http.StatusBadRequest, "unreadable body: "+err.Error())
		return nil, false
	}
	return body, true
}

// isQueryText reports whether a Content-Type names query text: text/plain
// or application/sql, absent counting as text/plain. A charset parameter
// is neither honored nor checked; the body bytes reach the C API
// unchanged.
func isQueryText(contentType string) bool {
	if contentType == "" {
		return true
	}
	mt, _, err := mime.ParseMediaType(contentType)
	return err == nil && (mt == "text/plain" || mt == "application/sql")
}

// countingWriter counts the bytes that reached the response, so the
// handler knows whether a problem body may still be written.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// retryAfter renders d as Retry-After wants it: whole seconds, rounded
// up so a client never comes back early.
func retryAfter(d time.Duration) string {
	return strconv.FormatInt(int64((d+time.Second-1)/time.Second), 10)
}

// writeClusterError maps a failed cluster call onto the wire by who
// failed: the breaker open or the cluster unreachable is 503, anything
// the cluster answered is the caller's problem at answered (400 for a
// query, 401 for a login), and a caller whose context has ended gets
// nothing at all.
func writeClusterError(ctx context.Context, w http.ResponseWriter, err error, answered int) {
	var open *qdb.BreakerOpenError
	switch {
	case ctx.Err() != nil:
		observe.Logger(ctx).DebugContext(ctx, "request abandoned by the caller", observe.Err(err))
	case errors.As(err, &open):
		w.Header().Set("Retry-After", retryAfter(open.RetryAfter))
		writeProblem(w, http.StatusServiceUnavailable, err.Error())
	case qdb.IsClusterUnavailable(err):
		writeProblem(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeProblem(w, answered, err.Error())
	}
}

// handleQuery runs the body as a query and streams the batch in the
// negotiated format. The status is decided before the first byte: the
// batch is materialized before anything is written, so every cluster
// error is known up front.
func handleQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// A non-text body is a clear 415 instead of a cluster parse error.
	if !isQueryText(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be text/plain or application/sql")
		return
	}
	// The body is capped and read whole; it is never inspected.
	q, ok := readBody(w, r)
	if !ok {
		return
	}
	enc := negotiate(r.Header.Get("Accept"))
	// The caller is whoever the bearer middleware verified.
	c := auth.ClaimsFrom(ctx)
	u := qdb.User{Username: c.Username, SecretKey: c.SecretKey}
	// A read is idempotent and nothing has been sent, so one retry on a
	// fresh session is safe.
	rec, err := qdb.ClusterFrom(ctx).Query(ctx, u, string(q), qdb.WithReadRetry())
	if err != nil {
		writeClusterError(ctx, w, err, http.StatusBadRequest)
		return
	}
	// A statement without a result set is a nil batch, which every
	// encoder renders as empty; only a real batch has buffers to free.
	if rec != nil {
		defer rec.Release()
	}
	w.Header().Set("Content-Type", enc.ContentType())
	cw := &countingWriter{w: w}
	if err := enc.Encode(ctx, cw, rec); err != nil {
		// Before the first byte the failure is this process's, a column
		// the encoder cannot render: 500. After it, or once the caller
		// has left, the stream is cut and only the log hears of it.
		switch {
		case ctx.Err() != nil:
			observe.Logger(ctx).DebugContext(ctx, "response abandoned by the caller", slog.Int64("bytes", cw.n), observe.Err(err))
		case cw.n == 0:
			writeProblem(w, http.StatusInternalServerError, err.Error())
		default:
			observe.Logger(ctx).WarnContext(ctx, "response cut mid-stream", slog.Int64("bytes", cw.n), observe.Err(err))
		}
	}
}

// registerQueryRoutes serves the v2 query endpoint behind the bearer
// middleware. The mux pattern fixes method and path.
func registerQueryRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v2/query", withCompression(requireBearer(http.HandlerFunc(handleQuery))))
}
