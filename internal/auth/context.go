package auth

import "context"

// Tokens travels in the context next to the logger and the cluster
// (ADR-0002's pattern): main places it in the process context, the
// server hands that context to every request, and handlers read it from
// there instead of taking it through a constructor.
type tokensKey struct{}

// WithTokens returns ctx carrying t.
func WithTokens(ctx context.Context, t *Tokens) context.Context {
	return context.WithValue(ctx, tokensKey{}, t)
}

// TokensFrom returns the Tokens carried by ctx. A ctx without one means
// a caller built a fresh context instead of passing along the one it was
// given; that is a programming error and panics, as observe.Logger does.
func TokensFrom(ctx context.Context) *Tokens {
	t, ok := ctx.Value(tokensKey{}).(*Tokens)
	if !ok {
		panic("auth: no tokens in context")
	}
	return t
}
