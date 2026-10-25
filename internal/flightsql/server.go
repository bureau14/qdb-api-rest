// Package flightsql serves Arrow Flight SQL on its own TLS gRPC port. The
// server embeds arrow-go's base server, so every command it does not
// implement answers UNIMPLEMENTED, and it stores nothing between calls
// (ADR-0018).
package flightsql

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/apache/arrow-go/v18/arrow/flight"
	"github.com/apache/arrow-go/v18/arrow/flight/flightsql"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// Server is the Flight SQL service and the gRPC server that carries it.
type Server struct {
	flightsql.BaseServer
	// root is the process context: the logger, the cluster and the
	// keychain every call reads.
	root context.Context
	// anonymousAllowed says whether a call with no authorization header
	// runs as the anonymous user. It is the cluster's mode, read once: an
	// insecure cluster admits the anonymous user and a secure one refuses
	// it, so the rule needs no cluster lookup per call.
	anonymousAllowed bool
	grpc             *grpc.Server
	lis              net.Listener
}

// New builds the server and listens on the address with the TLS
// configuration. The process context carries the logger, the cluster and
// the keychain every call reads. The listener is TLS only (ADR-0018).
func New(ctx context.Context, addr string, tlsConfig *tls.Config) (*Server, error) {
	// The gRPC server is built directly rather than through arrow-go's
	// flight.Server helper, because the helper's shutdown is a bare
	// GracefulStop with no forced stop after the grace. The steps:
	//
	//  1. the interceptor chain, the same on the unary and the stream
	//     path;
	//  2. the TLS credentials;
	//  3. register the Flight SQL service;
	//  4. register what GetSqlInfo answers;
	//  5. listen.
	s := &Server{root: ctx, anonymousAllowed: !qdb.ClusterFrom(ctx).Secured()}
	// 1. The access line is outermost and the auth edge inside it, so a
	//    refused call is logged too. The access line therefore carries no
	//    user tag, on this plane as on HTTP.
	// 2. TLS only, with no plaintext option: the port serves the HTTPS
	//    listener's certificates (ADR-0018).
	s.grpc = grpc.NewServer(
		grpc.ChainUnaryInterceptor(s.accessUnary, s.authUnary),
		grpc.ChainStreamInterceptor(s.accessStream, s.authStream),
		grpc.Creds(credentials.NewTLS(tlsConfig)))
	// 3. The service: arrow-go's router from Flight RPCs to Flight SQL
	//    commands, with DoGet this package's own, because the statement's
	//    DoGet writes inside the sink and every other RPC stays the
	//    router's.
	flight.RegisterFlightServiceServer(s.grpc, &service{FlightServer: flightsql.NewFlightServer(s), s: s})
	// 4. arrow-go's base server serves GetSqlInfo from its registered map
	//    and answers NOT_FOUND while the map is empty. The map is per
	//    instance and has no lock, so it is filled here, before the first
	//    call, and not by a package-level init on a global.
	if err := registerSqlInfo(&s.BaseServer); err != nil {
		return nil, err
	}
	// 5. Listen now, so a port in use fails the start here, as it does
	//    for the HTTP listeners.
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s.lis = lis
	return s, nil
}

// Addr answers the bound address, for a test that listens on port zero.
func (s *Server) Addr() net.Addr { return s.lis.Addr() }

// Serve serves until Shutdown or a listener failure, answering nil after
// Shutdown.
func (s *Server) Serve() error { return s.grpc.Serve(s.lis) }

// Shutdown stops accepting calls, waits for the calls in flight until the
// context ends, and then cuts the rest.
func (s *Server) Shutdown(ctx context.Context) {
	// GracefulStop has no deadline of its own, so it runs on a goroutine
	// and the select below bounds it.
	done := make(chan struct{})
	go func() {
		s.grpc.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		// Stop cuts the calls still open when the grace ends, so shutdown
		// never outlives the harness's SIGTERM-to-SIGKILL window.
		s.grpc.Stop()
	}
}

// callContext gives a call's context the process values: the logger, the
// cluster and the keychain. Each is copied explicitly, so a reader sees
// what a handler may read.
func (s *Server) callContext(ctx context.Context) context.Context {
	// grpc-go has no BaseContext as net/http has, so the interceptors
	// splice the process values onto the call's own cancellation. A
	// merged-context type answering values from the root and cancellation
	// from the call was rejected as more code and less explicit about what
	// a handler may read.
	ctx = observe.WithLogger(ctx, observe.Logger(s.root))
	ctx = qdb.WithCluster(ctx, qdb.ClusterFrom(s.root))
	return auth.WithTokens(ctx, auth.TokensFrom(s.root))
}

// wrappedStream carries a replaced context through a stream, because
// grpc.ServerStream's Context is a method and not a field.
type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
