// Package encoding turns one query result into bytes in one wire format.
// Every encoder consumes the same Arrow record batch the query core
// returns (internal/qdb); an encoder knows its media type and its bytes.
package encoding

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
)

// chunkRows is the run of rows an encoder handles between two looks at
// the ctx: one record batch on the Arrow wire, one stride between ctx
// checks on the rendered wires.
const chunkRows = 65536

// Encoder writes one record batch to w in one wire format.
type Encoder interface {
	// ContentType is the media type the handler answers with.
	ContentType() string
	// Encode writes rec to w. A nil rec is a statement that produced no
	// result set and encodes as zero columns and zero rows. Encode never
	// releases rec, which its caller owns. Encode returns the first write
	// error; ctx ending between chunks ends the encoding.
	Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error
}
