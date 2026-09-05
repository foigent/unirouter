package runtimegeneration

import "context"

type identityContextKey struct{}

// WithIdentity is set by the server admission layer after a confirmed lease.
// No HTTP header or request field can supply this process identity.
func WithIdentity(ctx context.Context, i Identity) context.Context {
	return context.WithValue(ctx, identityContextKey{}, i)
}

func FromContext(ctx context.Context) (Identity, bool) {
	i, ok := ctx.Value(identityContextKey{}).(Identity)
	return i, ok && i.Validate() == nil
}
