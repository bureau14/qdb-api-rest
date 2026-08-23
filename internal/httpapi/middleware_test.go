package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// serve runs one request through withRequestLogging over handler and
// returns the response plus every JSON log record written.
func serve(t *testing.T, handler http.HandlerFunc, header string) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	ctx := observe.WithLogger(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)))
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/x", nil)
	if header != "" {
		req.Header.Set(requestIDHeader, header)
	}
	resp := httptest.NewRecorder()
	withRequestLogging(handler).ServeHTTP(resp, req)
	var records []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatal(err)
		}
		records = append(records, rec)
	}
	return resp, records
}

func TestRequestIDEchoedAndLogged(t *testing.T) {
	resp, records := serve(t, func(w http.ResponseWriter, r *http.Request) {
		observe.Logger(r.Context()).InfoContext(r.Context(), "inside")
		w.WriteHeader(http.StatusTeapot)
	}, "abc")
	if got := resp.Header().Get(requestIDHeader); got != "abc" {
		t.Errorf("echoed id = %q, want abc", got)
	}
	if len(records) != 2 {
		t.Fatalf("want handler line + access line, got %v", records)
	}
	for _, rec := range records {
		if rec[observe.KeyRequestID] != "abc" {
			t.Errorf("line lacks request id: %v", rec)
		}
	}
	if access := records[1]; access["msg"] != "request" || access["status"] != float64(http.StatusTeapot) {
		t.Errorf("access line wrong: %v", access)
	}
}

func TestRequestIDMintedWhenAbsentOrInvalid(t *testing.T) {
	noop := func(http.ResponseWriter, *http.Request) {}
	for _, header := range []string{"", strings.Repeat("x", maxRequestIDLen+1), "tab\there"} {
		resp, _ := serve(t, noop, header)
		if got := resp.Header().Get(requestIDHeader); got == "" || got == header {
			t.Errorf("header %q: minted id = %q", header, got)
		}
	}
}

func TestRecorderPreservesFlush(t *testing.T) {
	serve(t, func(w http.ResponseWriter, _ *http.Request) {
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush through recorder: %v", err)
		}
	}, "")
}
