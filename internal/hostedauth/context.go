package hostedauth

import "context"

type identityKey struct{}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey{}).(Identity)
	return identity, ok
}
