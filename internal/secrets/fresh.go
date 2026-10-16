package secrets

import "context"

type freshKey struct{}

// WithFresh marks ctx as asking for newly issued credentials: a method
// provider that caches (aws-sso) skips its cache. The hub sets it when it
// rotates a secret that is about to expire; a stored credential ignores it.
func WithFresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, freshKey{}, true)
}

// IsFresh reports whether ctx came from WithFresh.
func IsFresh(ctx context.Context) bool {
	v, _ := ctx.Value(freshKey{}).(bool)
	return v
}
