package flightsql

import (
	"context"
	"encoding/base64"
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

// login proves a Basic credential by one dial and answers the access
// token. An empty username is the anonymous login, as the HTTP login
// treats it (ADR-0018, Decision 6).
func (s *Server) login(ctx context.Context, username, secret string) (string, error) {
	// One dial with four outcomes mapped to gRPC codes:
	//
	//  1. the user, anonymous when the username is empty;
	//  2. Authenticate, and classify what it answered;
	//  3. Issue, the access token alone.

	// 1. An anonymous user carries no secret, whatever was sent.
	u := qdb.User{}
	if username != "" {
		u = qdb.User{Username: username, SecretKey: secret}
	}
	// 2. A refusal is the caller's, UNAUTHENTICATED with the binding's
	//    message. An open breaker and an unreachable cluster are
	//    UNAVAILABLE; the breaker's retry-after is not carried, because
	//    gRPC has no Retry-After header and the statement query decides
	//    the detail text when it maps a running query's errors. A caller
	//    that went away is CANCELED.
	var open *qdb.BreakerOpenError
	err := qdb.ClusterFrom(ctx).Authenticate(ctx, u)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		return "", status.Error(codes.Canceled, ctx.Err().Error())
	case errors.Is(err, qdb.ErrCredentialsRefused):
		return "", unauthenticated(err.Error())
	case errors.As(err, &open), qdb.IsClusterUnavailable(err):
		return "", status.Error(codes.Unavailable, err.Error())
	default:
		return "", status.Error(codes.Internal, err.Error())
	}
	// 3. The access token alone, because Flight has no refresh path: a
	//    client whose token expired reconnects, as the stock drivers do
	//    (ADR-0018, Decision 6). A mint failure is this process's fault.
	g, err := auth.TokensFrom(ctx).Issue(u.Username, u.SecretKey)
	if err != nil {
		observe.Logger(ctx).ErrorContext(ctx, "minting the access token failed", observe.Err(err))
		return "", status.Error(codes.Internal, "minting the access token failed")
	}
	return g.Access, nil
}

// handshake answers a Handshake call: it decodes the Basic credential,
// logs in, and sends the access token as a Bearer in the reply's headers
// before the base RPC runs. The client reads the token from the
// authorization header, as arrow-go's client and Arrow's ADBC and JDBC
// drivers do.
func (s *Server) handshake(ctx context.Context, stream grpc.ServerStream) error {
	// Three decodes and one send whose timing matters:
	//
	//  1. a Handshake without a credential is refused, because the call
	//     exists to log in;
	//  2. decode the Basic credential as the drivers encode it;
	//  3. login;
	//  4. send the token as a header, now.

	// 1. no credential, nothing to log in with
	scheme, value, present := credential(ctx)
	if !present || !strings.EqualFold(scheme, "Basic") || value == "" {
		return unauthenticated("handshake needs a Basic credential")
	}
	// 2. The value is base64 of user:secret, raw or padded, because
	//    arrow-go's client writes it raw and the Java driver pads it.
	raw, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		raw, err = base64.StdEncoding.DecodeString(value)
	}
	if err != nil {
		return unauthenticated("invalid basic auth encoding")
	}
	username, secret, found := strings.Cut(string(raw), ":")
	if !found {
		return unauthenticated("invalid basic auth credentials")
	}
	// 3. login
	token, err := s.login(ctx, username, secret)
	if err != nil {
		return err
	}
	// 4. SendHeader and not SetHeader, because a header that is set and
	//    never flushed goes out together with the status as a
	//    trailers-only reply, and the JDBC driver reads headers only.
	if err := stream.SendHeader(metadata.Pairs(authorizationKey, "Bearer "+token)); err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return nil
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

// authStream is the auth edge of a stream: a Handshake goes to handshake,
// and every other stream runs under authenticate's ctx or not at all.
func (s *Server) authStream(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
	// The call ctx is callContext first, so handshake and authenticate
	// can read the cluster and the keychain.
	ctx := s.callContext(stream.Context())
	// A Handshake is matched by the method's suffix, as arrow-go matches
	// it. The base RPC body is a no-op, so the headers handshake sent are
	// the whole reply.
	if strings.HasSuffix(info.FullMethod, "/Handshake") {
		if err := s.handshake(ctx, stream); err != nil {
			return err
		}
		return next(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
	}
	ctx, err := s.authenticate(ctx)
	if err != nil {
		return err
	}
	return next(srv, &wrappedStream{ServerStream: stream, ctx: ctx})
}
