//go:build integration

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/testsupport/miniotest"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// A storage connection that is ready has no catalog: the Tables view's groups
// list it as ok with no tables, not as an error ("is storage, not a database"),
// which hid every other connection's tables behind a "can't list" page.
func TestListCatalogGroupsDoesNotListAStorageConnection(t *testing.T) {
	miniotest.UseExtensionCache(t)
	app, _ := connApp(t)
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "floci", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1",
		Options: map[string]string{"endpoint": "127.0.0.1:1", "url_style": "path", "use_ssl": "false"}}))
	_, err := app.SetConnectionCredentials(SetCredentialsRequest{Name: "floci", CredentialInput: CredentialInput{KeyID: "k", Secret: "s"}})
	require.NoError(t, err)

	groups, err := app.ListCatalogGroups()
	require.NoError(t, err)
	var found bool
	for _, g := range groups {
		if g.Connection == "floci" {
			found = true
			assert.Equal(t, "ok", g.Status, g.Detail)
			assert.Empty(t, g.Tables)
		}
	}
	assert.True(t, found)
}
