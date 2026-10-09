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

// negotiateCoding picks the content coding for an Accept-Encoding
// header. It reads the listed codings in order, and the first one this
// server produces wins. An absent header, no match and identity itself
// all mean identity, because the server never compresses uninvited. It
// does not read q weights, because a client that wants a coding names
// it, which is the rule the Accept header follows too.
func negotiateCoding(acceptEncoding string) coding {
	for tok := range strings.SplitSeq(acceptEncoding, ",") {
		// The coding name ends at the parameters (q included), which are
		// not read. Codings are case-insensitive on the wire.
		name, _, _ := strings.Cut(tok, ";")
		switch c := coding(strings.ToLower(strings.TrimSpace(name))); c {
		case gzipCoding, zstdCoding, identityCoding:
			return c
		}
	}
	return identityCoding
}

// newCompressor opens a compressor of coding c over w at the fastest
// level, because a gateway pays CPU per byte on every response it
// compresses, and the bytes saved are the WAN client's gain, not this
// process's. The zstd encoder runs at concurrency one, so a response
// spawns no goroutines. Its options are constants, so a construction
// error is a programming error.
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
// coding. It holds the status a handler writes back until the first
// body byte, when Content-Encoding is settled and the compressor opens.
// A handler that writes a status and no body gets that status
// uncompressed and unlabelled, so no empty frame is sent. Unwrap keeps
// http.ResponseController working through it.
type compressingWriter struct {
	http.ResponseWriter
	coding coding
	status int            // the held-back status, 0 until WriteHeader
	z      io.WriteCloser // nil until the first body byte
}

func (c *compressingWriter) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
}

func (c *compressingWriter) Write(p []byte) (int, error) {
	if c.z == nil {
		// The first byte settles the headers. The body is compressed, so
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
// client asked for, problem bodies included. Identity passes the writer
// through untouched. Every response of the route varies on the header,
// so a cache does not serve one client's coding to another. It is
// applied per route and never to the mux, because the probes answer
// empty bodies.
func withCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		c := negotiateCoding(r.Header.Get("Accept-Encoding"))
		if c == identityCoding {
			next.ServeHTTP(w, r)
			return
		}
		cw := &compressingWriter{ResponseWriter: w, coding: c}
		// A close error has no one left to tell, because the handler has
		// returned and the status is on the wire.
		defer func() { _ = cw.Close() }()
		next.ServeHTTP(cw, r)
	})
}

// requestCoding reads the Content-Encoding of a request as one coding.
// An absent header and identity both mean identity. The second value is
// false for a coding this server does not read, which includes a list
// of several.
func requestCoding(contentEncoding string) (coding, bool) {
	// A coding list is refused rather than unwound, because no client
	// stacks codings on a request and unwinding is code for nobody. The
	// name is case-insensitive on the wire, like a response coding.
	name := strings.ToLower(strings.TrimSpace(contentEncoding))
	switch c := coding(name); {
	case name == "":
		return identityCoding, true
	case c == gzipCoding, c == zstdCoding, c == identityCoding:
		return c, true
	}
	return "", false
}

// newDecompressor opens a reader of coding c over r, the inverse of
// newCompressor. The gzip reader reads its header here, so a corrupt
// body can fail at the open. The zstd decoder runs at concurrency one,
// so a request spawns no goroutines, as a response spawns none, at the
// cost of a slower decode on a large body, which is not measured.
func newDecompressor(c coding, r io.Reader) (io.ReadCloser, error) {
	switch c {
	case gzipCoding:
		return gzip.NewReader(r)
	case zstdCoding:
		z, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, err
		}
		// IOReadCloser adapts the decoder, whose Close returns nothing.
		return z.IOReadCloser(), nil
	}
	panic("httpapi: no decompressor for " + string(c))
}
