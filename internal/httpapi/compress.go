package httpapi

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// coding is a content coding of the response body, the token the wire
// uses in Accept-Encoding and Content-Encoding.
type coding string

const (
	identityCoding coding = "identity"
	gzipCoding     coding = "gzip"
	zstdCoding     coding = "zstd"
)

// negotiateCoding picks the content coding for an Accept-Encoding header:
// the listed codings are read in order and the first one this server
// produces wins. An absent header, no match and identity itself all mean
// identity; the server never compresses uninvited. q weights are not
// read: a client that wants a coding names it, the rule Accept follows.
func negotiateCoding(acceptEncoding string) coding {
	for tok := range strings.SplitSeq(acceptEncoding, ",") {
		// The coding name ends at the parameters (q included), which are
		// not read; codings are case-insensitive on the wire.
		name, _, _ := strings.Cut(tok, ";")
		switch c := coding(strings.ToLower(strings.TrimSpace(name))); c {
		case gzipCoding, zstdCoding, identityCoding:
			return c
		}
	}
	return identityCoding
}

// newCompressor opens a compressor of coding c over w at the fastest
// level: a gateway pays CPU per byte on every response it compresses,
// and the bytes saved are the WAN client's gain, not this process's.
// The zstd encoder runs at concurrency one so a response spawns no
// goroutines; its options are constants, so a construction error is a
// programming error.
func newCompressor(c coding, w io.Writer) io.WriteCloser {
	switch c {
	case gzipCoding:
		z, _ := gzip.NewWriterLevel(w, gzip.BestSpeed) // BestSpeed is a valid level
		return z
	case zstdCoding:
		z, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithEncoderConcurrency(1))
		if err != nil {
			panic(err)
		}
		return z
	}
	panic("httpapi: no compressor for " + string(c))
}

// compressingWriter compresses one response body in the negotiated
// coding. The status a handler writes is held back until the first body
// byte, when Content-Encoding is settled and the compressor opens; a
// handler that writes a status and no body gets that status uncompressed
// and unlabelled, so no empty frame is ever sent. Unwrap keeps
// http.ResponseController working through it.
type compressingWriter struct {
	http.ResponseWriter
	coding coding
	status int            // the held-back status; 0 until WriteHeader
	z      io.WriteCloser // nil until the first body byte
}

func (c *compressingWriter) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
}

func (c *compressingWriter) Write(p []byte) (int, error) {
	if c.z == nil {
		// The first byte settles the headers: the body is compressed, so
		// its coding is declared and any length a handler set is wrong.
		if c.status == 0 {
			c.status = http.StatusOK
		}
		h := c.Header()
		h.Set("Content-Encoding", string(c.coding))
		h.Del("Content-Length")
		c.ResponseWriter.WriteHeader(c.status)
		c.z = newCompressor(c.coding, c.ResponseWriter)
	}
	return c.z.Write(p)
}

// Close ends the compressed stream, or releases a held-back status that
// no body followed.
func (c *compressingWriter) Close() error {
	if c.z != nil {
		return c.z.Close()
	}
	if c.status != 0 {
		c.ResponseWriter.WriteHeader(c.status)
	}
	return nil
}

func (c *compressingWriter) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

// withCompression compresses one route's responses in the coding the
// client asked for, problem bodies included; identity passes the writer
// through untouched. Every response of the route varies on the header,
// so a cache never serves one client's coding to another. Applied per
// route, never to the mux: the probes answer empty bodies with
// golden-pinned headers.
func withCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		c := negotiateCoding(r.Header.Get("Accept-Encoding"))
		if c == identityCoding {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressingWriter{ResponseWriter: w, coding: c}
		// A close error has no one left to tell: the handler has
		// returned and the status is on the wire.
		defer func() { _ = cw.Close() }()
		next.ServeHTTP(cw, r)
	})
}
