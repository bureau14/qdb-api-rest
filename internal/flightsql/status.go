package flightsql

import (
	"context"
	"errors"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// unavailableAfter is UNAVAILABLE with the error's message and a
// RetryInfo detail carrying the delay, the gRPC twin of the Retry-After
// header the HTTP route sets.
func unavailableAfter(err error, after time.Duration) error {
	st := status.New(codes.Unavailable, err.Error())
	// WithDetails fails only for a message it cannot pack, which a
	// well-formed duration never is. If it did fail, the status goes out
	// without the detail.
	if with, derr := st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(after)}); derr == nil {
		st = with
	}
	return st.Err()
}

// statusOf maps a failed cluster call to a gRPC status by who failed, as
// writeClusterError does on HTTP. answered is the code for anything the
// engine or the cluster answered: UNAUTHENTICATED for a login and
// INVALID_ARGUMENT for a query. A nil error maps to nil.
func statusOf(ctx context.Context, err error, answered codes.Code) error {
	// The outcomes in order of precedence, each a row of ADR-0018, Decision
	// 13, stated once for the login and the statement. Two switches side by
	// side were rejected, because each row would be stated twice.
	//  1. the caller's context ended: the context's own code;
	//  2. a refused credential: UNAUTHENTICATED;
	//  3. an open breaker and a full instance cap: UNAVAILABLE with a
	//     RetryInfo detail;
	//  4. an unreachable cluster and a failed attach: UNAVAILABLE with the
	//     message alone;
	//  5. anything else: answered, with the error's message.
	if err == nil {
		return nil
	}
	var open *qdb.BreakerOpenError
	var full *qdb.InstanceLimitError
	switch {
	// 1. CANCELED or DEADLINE_EXCEEDED, because the failure is the caller's
	//    and nothing of the cluster's is reported
	case ctx.Err() != nil:
		return status.FromContextError(ctx.Err()).Err()
	// 2. before the unreachable case, because a refused attach would
	//    otherwise count as an unreachable cluster
	case errors.Is(err, qdb.ErrCredentialsRefused):
		return status.Error(codes.Unauthenticated, err.Error())
	// 3. the delay travels as a RetryInfo detail next to the message, as
	//    HTTP carries a Retry-After header. The message alone was rejected,
	//    because a client would parse the value out of prose. Whether the
	//    ADBC and JDBC drivers surface the detail is not verified.
	//    arrow-go's client answers it as grpc-go does.
	case errors.As(err, &open):
		return unavailableAfter(err, open.RetryAfter)
	case errors.As(err, &full):
		return unavailableAfter(err, full.RetryAfter)
	// 4. no detail, as the HTTP route sets no header for them. A failed
	//    attach is inside IsClusterUnavailable.
	case qdb.IsClusterUnavailable(err):
		return status.Error(codes.Unavailable, err.Error())
	// 5. the engine or the cluster answered, and the message is theirs
	default:
		return status.Error(answered, err.Error())
	}
}
