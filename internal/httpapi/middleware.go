package httpapi

import (
	"net/http"
	"time"
	"uuid"

	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// requestIDHeader is the id clients and load balancers propagate; it is
// honored inbound and always echoed back.
const requestIDHeader = "X-Request-Id"

// maxRequestIDLen bounds what an untrusted header may inject into logs.
const maxRequestIDLen = 128

// validRequestID accepts non-empty printable ASCII up to maxRequestIDLen.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}

// newRequestID mints a time-ordered id when the client sent none.
func newRequestID() string {
	return uuid.NewV7().String()
}

// requestID returns the inbound id when acceptable, else a fresh one.
func requestID(r *http.Request) string {
	if id := r.Header.Get(requestIDHeader); validRequestID(id) {
		return id
	}
	return newRequestID()
}

// responseRecorder captures status and size for the access line. Unwrap
// keeps http.ResponseController (Flush, deadlines) working through it.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (rec *responseRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *responseRecorder) Write(p []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(p)
	rec.bytes += int64(n)
	return n, err
}

func (rec *responseRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

// withRequestLogging tags the request context with its id, echoes the id,
// and emits one access line when the handler returns. Only the id rides
// on the context; lines join on it (ADR-0002).
func withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := requestID(r)
		w.Header().Set(requestIDHeader, id)
		ctx := observe.WithAttrs(r.Context(), observe.KeyRequestID, id)
		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r.WithContext(ctx))
		observe.Logger(ctx).InfoContext(ctx, "request",
			"method", r.Method, "path", r.URL.Path,
			"status", rec.status, "bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds())
	})
}
