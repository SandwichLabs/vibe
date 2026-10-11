package session_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/session"
	"github.com/SandwichLabs/vibe/internal/session/sessiontest"
)

// Only a type that declares StorageScope exports a scope the agent policy reads
// inside; a catalog-less type that merely accepts a "scope" param exports none.
func TestOnlyStorageScopeTypesExportAScope(t *testing.T) {
	sess, err := session.New(session.WithConnectionTypes(
		sessiontest.ScopeAcceptingType{TypeName: "plain"},
		sessiontest.StorageScopedType{ScopeAcceptingType: sessiontest.ScopeAcceptingType{TypeName: "store"}},
	), session.WithExtensionDirectory(t.TempDir()))
	require.NoError(t, err)
	defer sess.Close()
	require.NoError(t, sess.Register(session.Spec{Name: "a", Type: "plain", Params: map[string]string{"scope": "file:///"}}))
	require.NoError(t, sess.Register(session.Spec{Name: "b", Type: "store", Params: map[string]string{"scope": "s3://bucket/p/"}}))
	scope, cl := map[string]string{}, map[string]bool{}
	for _, ci := range sess.ConnectionInfos() {
		scope[ci.Name], cl[ci.Name] = ci.Scope, ci.Catalogless
	}
	assert.True(t, cl["a"] && cl["b"])
	assert.Equal(t, "", scope["a"], "a catalog-less type that is not a storage type exports no scope")
	assert.Equal(t, "s3://bucket/p/", scope["b"])
}
