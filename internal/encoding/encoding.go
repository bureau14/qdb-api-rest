// Package encoding turns one query result into bytes in one wire format.
// Every encoder consumes the same Arrow record batch the query core
// returns (internal/qdb), so the formats agree by construction on what a
// row, a value and a null are. Content negotiation, status codes,
// compression, flushing and logging are the handler's; an encoder knows
// its media type and its bytes.
package encoding

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
)

// Encoder writes one record batch to w in one wire format.
type Encoder interface {
	// ContentType is the media type the handler answers with.
	ContentType() string
	// Encode writes rec to w. A nil rec is a statement that produced no
	// result set and encodes as zero columns and zero rows. Encode never
	// releases rec, which its caller owns. Encode returns the first write
	// error; ctx ending between batches ends the encoding.
	Encode(ctx context.Context, w io.Writer, rec arrow.RecordBatch) error
}
