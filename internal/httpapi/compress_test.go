package httpapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// failer is what decompress needs of a *testing.T or a *rapid.T.
type failer interface {
	Helper()
	Fatalf(format string, args ...any)
}

// decompress decodes body in coding c with the reference decoders.
func decompress(t failer, c coding, body []byte) []byte {
	t.Helper()
	var r io.Reader
	switch c {
	case gzipCoding:
		z, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("gzip: %v", err)
		}
		r = z
	case zstdCoding:
		z, err := zstd.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("zstd: %v", err)
		}
		defer z.Close()
		r = z
	default:
		r = bytes.NewReader(body)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("%s: %v", c, err)
	}
	return out
}

// TestNegotiateCoding: the client's order decides, parameters and case
// are ignored, and everything else is identity.
func TestNegotiateCoding(t *testing.T) {
	cases := map[string]coding{
		"":                        identityCoding,
		"br":                      identityCoding,
		"*":                       identityCoding,
		"gzip":                    gzipCoding,
		"GZip;q=0.1, zstd":        gzipCoding,
		"deflate, gzip, br, zstd": gzipCoding,
		"zstd, gzip":              zstdCoding,
		"identity, gzip":          identityCoding,
		" zstd ;q=0.5 , identity": zstdCoding,
		"gzip;q=0":                gzipCoding, // q is not read
		"x-gzip":                  identityCoding,
	}
	for header, want := range cases {
		if got := negotiateCoding(header); got != want {
			t.Errorf("%q: %s, want %s", header, got, want)
		}
	}
}

// compressed runs handler behind withCompression for one Accept-Encoding.
func compressed(handler http.HandlerFunc, acceptEncoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Accept-Encoding", acceptEncoding)
	resp := httptest.NewRecorder()
	withCompression(handler).ServeHTTP(resp, req)
	return resp
}

// TestCompressionHeldStatus: a status without a body goes out as written,
// unlabelled and empty; a problem body is compressed and labelled.
func TestCompressionHeldStatus(t *testing.T) {
	empty := compressed(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }, "gzip")
	if empty.Code != http.StatusNoContent || empty.Header().Get("Content-Encoding") != "" || empty.Body.Len() != 0 {
		t.Errorf("empty body: status %d, Content-Encoding %q, %d bytes", empty.Code, empty.Header().Get("Content-Encoding"), empty.Body.Len())
	}
	if vary := empty.Header().Get("Vary"); vary != "Accept-Encoding" {
		t.Errorf("Vary = %q", vary)
	}
	for _, c := range []coding{gzipCoding, zstdCoding} {
		resp := compressed(func(w http.ResponseWriter, _ *http.Request) { writeProblem(w, http.StatusTeapot, "x") }, string(c))
		if resp.Code != http.StatusTeapot || resp.Header().Get("Content-Encoding") != string(c) {
			t.Errorf("%s: status %d, Content-Encoding %q", c, resp.Code, resp.Header().Get("Content-Encoding"))
		}
		var p problem
		if err := json.Unmarshal(decompress(t, c, resp.Body.Bytes()), &p); err != nil || p.Status != http.StatusTeapot {
			t.Errorf("%s: problem body %v (%v)", c, p, err)
		}
	}
}
