package flightsql

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bureau14/qdb-api-rest/internal/query"
)

// engineKey is the call header a connection picks its engine with. The
// JDBC and ODBC drivers send a connection property they do not know as a
// header of that name, and ADBC sends one through its call-header option.
const engineKey = "engine"

// withEngine answers the text the call's engine header asks for. It puts
// "#sql " in front of q when the header is sql and q has no prefix, and
// answers q unchanged otherwise. An unknown value is INVALID_ARGUMENT.
func withEngine(ctx context.Context, q string) (string, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	vals := md.Get(engineKey)
	if len(vals) == 0 {
		return q, nil
	}
	// The first value counts, as for the authorization header. The value
	// matches in any case, because users type it in a connection URL.
	switch v := vals[0]; {
	case strings.EqualFold(v, "native"):
		return q, nil
	case strings.EqualFold(v, "sql"):
		// A text that already carries the prefix stays as it is, so it
		// does not reach DuckDB with a second one.
		if query.Prefixed(q) {
			return q, nil
		}
		return "#sql " + q, nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "engine header: unknown value %q, want sql or native", v)
	}
}
