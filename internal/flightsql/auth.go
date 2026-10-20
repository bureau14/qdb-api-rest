package flightsql

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/bureau14/qdb-api-rest/internal/auth"
	"github.com/bureau14/qdb-api-rest/internal/observe"
	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// authorizationKey is the metadata key the credential travels under, the
// one Arrow's drivers and arrow-go's client write. gRPC lowercases keys.
const authorizationKey = "authorization"

// credential reads the authorization metadata of a call: its scheme, its
// value and whether a header was present at all. A header with no scheme
// answers an empty scheme, and the caller matches the scheme
// case-insensitively, as the HTTP bearer edge does.
func credential(ctx context.Context) (scheme, value string, present bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", "", false
	}
	vals := md.Get(authorizationKey)
	if len(vals) == 0 {
		return "", "", false
	}
	scheme, value, _ = strings.Cut(strings.TrimSpace(vals[0]), " ")
	return scheme, strings.TrimSpace(value), true
}

// unauthenticated is the UNAUTHENTICATED status with detail, which names
// the failure as the HTTP challenge's detail does (ADR-0018).
func unauthenticated(detail string) error {
	return status.Error(codes.Unauthenticated, detail)
}

// authenticate decides who a call runs as, from its authorization
// metadata, and answers the ctx the handler runs under: the claims placed,
// the user and the session tagged. No header is the anonymous user on an
// insecure cluster and UNAUTHENTICATED on a secure one. A present header
// must be a Bearer with a verifiable access token (ADR-0018).
func (s *Server) authenticate(ctx context.Context) (context.Context, error) {
	// Four outcomes in a precedence, each a rule of ADR-0018:
	//
	//  1. no header: anonymous on an insecure cluster, refused on a
	//     secure one;
	//  2. a header that is empty, not Bearer or has no value: refused;
	//  3. verify: an expired token and an invalid one are both refused,
	//     with the detail naming which;
	//  4. typ != access: refused;
	//  5. the claims ride the ctx, and the logger carries the user and the
	//     session, so the code below only logs.
	scheme, token, present := credential(ctx)
	var c auth.Claims
	switch {
	// 1. no header. The stock drivers send no header unless a username or
	//    a token is configured, so a zero-config client on an insecure
	//    cluster runs as the anonymous user. On a secure cluster the dial
	//    would refuse the anonymous user anyway, so the client is told to
	//    log in first. arrow-go's own basic-auth middleware was rejected
	//    because it refuses every call without a header.
	case !present:
		if qdb.ClusterFrom(ctx).Secured() {
			return nil, unauthenticated("missing bearer token")
		}
	// 2. a client that sent a header meant to authenticate, so a header
	//    that cannot is refused rather than treated as absent.
	case !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t"):
		return nil, unauthenticated("malformed bearer credentials")
	default:
		// 3. verify. Expired and invalid are told apart, as the HTTP edge
		//    tells them, because the verifier already does.
		var err error
		c, err = auth.TokensFrom(ctx).Verify(token)
		switch {
		case errors.Is(err, auth.ErrTokenExpired):
			return nil, unauthenticated("token expired")
		case err != nil:
			return nil, unauthenticated("invalid token")
		}
		// 4. a refresh token opens the refresh endpoint and nothing else
		//    (ADR-0016), so only an access token opens this plane.
		if c.Typ != "access" {
			return nil, unauthenticated("not an access token")
		}
	}
	// 5. The edge enriches and the handler does not. The anonymous ctx
	//    carries zero Claims, so ClaimsFrom never panics and a handler
	//    derives the user from the claims as the HTTP handlers do.
	ctx = auth.WithClaims(ctx, c)
	return observe.WithAttrs(ctx, observe.KeyUser, c.Username, observe.KeySession, c.SessionID), nil
}

// authUnary is the auth edge of a unary call: the call runs under
// authenticate's ctx or not at all.
func (s *Server) authUnary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
	// The call ctx is callContext first, so authenticate can read the
	// cluster and the keychain.
	ctx, err := s.authenticate(s.callContext(ctx))
	if err != nil {
		return nil, err
	}
	return next(ctx, req)
}

// authStream is the auth edge of a stream: the stream runs under
// authenticate's ctx or not at all.
func (s *Server) authStream(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, next grpc.StreamHandler) error {
	// The call ctx is callContext first, so authenticate can read the
	// cluster and the keychain.
	ctx, err := s.authenticate(s.callContext(stream.Context()))
	if err != nil {
		return err
	}
	return next(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
}
