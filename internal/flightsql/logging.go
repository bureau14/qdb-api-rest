package flightsql

import (
	"context"
	"time"
	"uuid"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// accessContext tags the call's context with a request id minted for
// the call, so its log lines join on the id as the HTTP lines do.
func (s *Server) accessContext(ctx context.Context) context.Context {
	// The id is minted on every call and never read from the metadata.
	// Honoring an inbound id was rejected, because it would move httpapi's
	// validation of an id into observe.
	return observe.WithAttrs(s.callContext(ctx), observe.KeyRequestID, uuid.NewV7().String())
}

// accessLine writes one line per RPC with the method, the gRPC code and
// the duration, in the HTTP access line's vocabulary.
func accessLine(ctx context.Context, method string, start time.Time, err error) {
	// The code comes from status.Code, so a handler's error and a refused
	// call log the same way, and a nil error is OK.
	observe.Logger(ctx).InfoContext(ctx, "request",
		"method", method, "code", status.Code(err).String(),
		"duration_ms", time.Since(start).Milliseconds())
}

// accessUnary writes one access line per unary call.
func (s *Server) accessUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
	start := time.Now()
	ctx = s.accessContext(ctx)
	resp, err := next(ctx, req)
	accessLine(ctx, info.FullMethod, start, err)
	return resp, err
}

// accessStream writes one access line per stream, when the stream ends.
func (s *Server) accessStream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
	start := time.Now()
	ctx := s.accessContext(stream.Context())
	err := next(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
	accessLine(ctx, info.FullMethod, start, err)
	return err
}
