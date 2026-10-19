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

	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
	"github.com/bureau14/qdb-api-rest/internal/query"
)

// maxBodyBytes caps every request body. A QuasarDB query is a line of
// text and a login is two fields, so a cap sized for one line bounds
// what a request can make the server read.
const maxBodyBytes = 1 << 20

// readBody reads a capped request body whole. It answers 413 over the
// cap and 400 when the body cannot be read. A false result means the
// response has been written.
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

// isQueryText reports whether a Content-Type names query text, which is
// text/plain or application/sql. An absent header counts as text/plain.
// A charset parameter is neither honored nor checked. The body bytes
// reach the C API unchanged.
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
// up, so a client does not come back early.
func retryAfter(d time.Duration) string {
	return strconv.FormatInt(int64((d+time.Second-1)/time.Second), 10)
}

// writeClusterError maps a failed cluster call onto the wire by who
// failed. A credential the secure cluster refused is 401 with the
// invalid_token challenge, so the client logs in again. An open breaker
// or a full DuckDB instance cap is 503 with Retry-After, and an
// unreachable cluster is 503 without it. Anything else the cluster
// answered is the caller's fault and gets the status in answered (400
// for a query, 401 for a login). A caller whose context has ended gets no
// response at all.
func writeClusterError(ctx context.Context, w http.ResponseWriter, err error, answered int) {
	var open *qdb.BreakerOpenError
	var full *qdb.InstanceLimitError
	switch {
	case ctx.Err() != nil:
		observe.Logger(ctx).DebugContext(ctx, "request abandoned by the caller", observe.Err(err))
	// The token's credentials are what failed, so the answer is the
	// bearer edge's own for an invalid token (ADR-0010 names what it
	// beat). The case comes before the unreachable one, because a refused
	// attach would otherwise count as an unreachable cluster.
	case errors.Is(err, qdb.ErrCredentialsRefused):
		unauthorized(w, true, err.Error())
	case errors.As(err, &open):
		w.Header().Set("Retry-After", retryAfter(open.RetryAfter))
		writeProblem(w, http.StatusServiceUnavailable, err.Error())
	// The instance cap answers like the open breaker, because both say
	// that the server is full for now.
	case errors.As(err, &full):
		w.Header().Set("Retry-After", retryAfter(full.RetryAfter))
		writeProblem(w, http.StatusServiceUnavailable, err.Error())
	case qdb.IsClusterUnavailable(err):
		writeProblem(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeProblem(w, answered, err.Error())
	}
}

// handleQuery runs the body through query.Run as the bearer's user and
// streams the result in the negotiated format. A #sql body runs on
// DuckDB, and any other body on the native engine. The status is decided
// before the first byte.
func handleQuery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// The query runs inside the sink, and every error of the run arrives
	// before the sink runs, so the status is decided before the first byte.
	// Two kinds of failure come out, and they answer differently:
	//
	//  1. read the body as query text;
	//  2. run the query through query.Run, with the encoder streaming
	//     inside the sink;
	//  3. a failure of the stream. With zero bytes out it is 500, which is
	//     where a column type no encoder writes lands. After the first
	//     byte the stream is cut and only the log hears of it;
	//  4. a failure of the run, before any byte. The engine's error is the
	//     caller's 400, and writeClusterError maps the rest.

	// 1. the body. A non-text body is a clear 415 instead of an engine's
	//    parse error, and the body is capped and read whole.
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
	// 2. the query. query.Run retries the native engine once and DuckDB
	//    never, so the handler retries nothing itself.
	var streamErr error
	err := query.Run(ctx, caller(r), string(q), func(batches qdb.Batches) error {
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
	// 4. the run failed. The instance cap, a refused credential, an open
	//    breaker and an unreachable cluster answer as writeClusterError
	//    maps them, and anything else an engine answered is 400.
	case err != nil:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// registerQueryRoutes serves the v2 query endpoint behind the bearer
// middleware. The mux pattern fixes method and path.
func registerQueryRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/v2/query", withCompression(requireBearer(http.HandlerFunc(handleQuery))))
}
