// Package encoding turns one query result set into bytes in one wire
// format. Every encoder consumes the same Go-owned QueryResultSet the query
// core returns (internal/qdb), so the formats agree by construction on
// what a row, a value and a null are. Content negotiation, status codes,
// compression, flushing and logging are the handler's; an encoder knows
// its media type and its bytes.
package encoding

import (
	"context"
	"io"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// Encoder writes one result set to w in one wire format.
type Encoder interface {
	// ContentType is the media type the handler answers with.
	ContentType() string
	// Encode writes rs to w. A nil rs is a statement that produced no
	// result set and encodes as zero columns and zero rows. Encode returns
	// the first write error; ctx ending between batches ends the encoding.
	Encode(ctx context.Context, w io.Writer, rs *qdbapi.QueryResultSet) error
}
