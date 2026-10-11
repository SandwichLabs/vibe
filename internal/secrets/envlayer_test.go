package secrets

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverWithEnvLayer(t *testing.T) {
	base := NewResolver(WithKeychain(NewMemoryStore()), WithEnv(func(k string) string {
		return map[string]string{"MOTHERDUCK_USER": "from-base"}[k]
	}))
	layer := map[string]string{"MOTHERDUCK_TOKEN": "md-test-not-a-real-token"}
	hint := ""
	derived := base.WithEnvLayer(func(k string) (string, bool) { v, ok := layer[k]; return v, ok }, func(string) string { return hint })

	ref := Ref{Scope: ScopeWorkspace, Name: "md"}
	cred, err := derived.Resolve(context.Background(), ref, "env:MOTHERDUCK", Binding{Type: "motherduck"})
	require.NoError(t, err)
	assert.Equal(t, "md-test-not-a-real-token", cred.Token)
	assert.Equal(t, "from-base", cred.User, "falls back to the resolver's own environment")

	// The base resolver does not see the layer; they share one redactor.
	_, err = base.Resolve(context.Background(), ref, "env:OTHER", Binding{})
	require.Error(t, err)
	assert.Same(t, base.Redactor(), derived.Redactor())
	assert.NotContains(t, base.Redactor().Redact("x md-test-not-a-real-token y"), "md-test-not-a-real-token")

	// The hint rides on the not-set message and the status detail.
	delete(layer, "MOTHERDUCK_TOKEN")
	hint = "A .env is denied."
	_, err = derived.Resolve(context.Background(), ref, "env:NOPE", Binding{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "A .env is denied.")
	st := derived.Status(context.Background(), ref, "env:NOPE", Binding{})
	assert.Contains(t, st.Detail, "A .env is denied.")
}
