package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/secrets"
)

func TestViewDefinition_WinnerShadowAndErrors(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	team := pgConn("pg")
	team.Host = "team.internal"
	require.NoError(t, ws.AddConnection(ScopeShared, team))
	mine := pgConn("pg")
	mine.Host = "mine.internal"
	require.NoError(t, ws.AddConnection(ScopeLocal, mine))

	win, err := ViewDefinition(ws, "pg", "")
	require.NoError(t, err)
	assert.Equal(t, ScopeLocal, win.Scope)
	assert.Equal(t, "mine.internal", win.Def.Host)
	assert.Equal(t, []Scope{ScopeShared}, win.Shadows)
	assert.Empty(t, win.ShadowedBy)
	assert.Equal(t, Fingerprint(win.Def), win.Fingerprint)

	// Naming the winner's own scope is the winner.
	same, err := ViewDefinition(ws, "pg", "local")
	require.NoError(t, err)
	assert.Equal(t, win, same)

	// The shadowed Team copy: its own def, overridden by the local scope, no shadows.
	sh, err := ViewDefinition(ws, "pg", "shared")
	require.NoError(t, err)
	assert.Equal(t, ScopeShared, sh.Scope)
	assert.Equal(t, "team.internal", sh.Def.Host)
	assert.Equal(t, ScopeLocal, sh.ShadowedBy)
	assert.Empty(t, sh.Shadows)
	assert.Equal(t, Fingerprint(sh.Def), sh.Fingerprint)
	assert.Equal(t, win.SignIn, sh.SignIn)

	_, err = ViewDefinition(ws, "nope", "")
	var unknown *UnknownConnectionError
	require.ErrorAs(t, err, &unknown)
	assert.EqualError(t, err, `There is no connection called "nope".`)

	_, err = ViewDefinition(ws, "pg", "elsewhere")
	var bad *InvalidScopeError
	require.ErrorAs(t, err, &bad)

	_, err = ViewDefinition(ws, "pg", "user")
	var noCopy *NoScopeCopyError
	require.ErrorAs(t, err, &noCopy)
	assert.Equal(t, "user", noCopy.Scope)
}

func TestClearCredential_OneFieldVersusAll(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "rosa", Password: "pw_needle"}, false))

	// One field: the password goes, the username stays.
	require.NoError(t, ClearCredential(ctx, ws, "pg", "password"))
	st, err := ws.CredentialStatus(ctx, "pg")
	require.NoError(t, err)
	assert.False(t, st.SecretSet)
	assert.Equal(t, "rosa", st.Username)

	// All: nothing left.
	require.NoError(t, ClearCredential(ctx, ws, "pg", ""))
	st, err = ws.CredentialStatus(ctx, "pg")
	require.NoError(t, err)
	assert.False(t, st.SecretSet)
	assert.Empty(t, st.Username)

	// A field with nothing saved says so in plain words.
	err = ClearCredential(ctx, ws, "pg", "password")
	assert.EqualError(t, err, "There is nothing saved to clear for this connection.")

	// An unknown connection, with a field, is named; without one the workspace answers.
	err = ClearCredential(ctx, ws, "ghost", "password")
	assert.EqualError(t, err, `There is no connection called "ghost".`)
	assert.Error(t, ClearCredential(ctx, ws, "ghost", ""))
}

func TestExtensionNeeds(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	nobody := ExtensionNeeds(nil)
	names := make([]string, 0, len(nobody))
	for i, n := range nobody {
		names = append(names, n.Name)
		assert.NotNil(t, n.NeededBy, n.Name)
		assert.Empty(t, n.NeededBy, n.Name)
		if i > 0 {
			assert.Less(t, nobody[i-1].Name, n.Name, "sorted, each once")
		}
	}
	assert.ElementsMatch(t, []string{"postgres", "mysql", "httpfs", "azure", "sqlite", "ducklake", "iceberg", "avro", "delta", "motherduck"}, names)

	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("zeta")))
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("alpha")))
	needed := map[string][]string{}
	for _, n := range ExtensionNeeds(ws) {
		needed[n.Name] = n.NeededBy
	}
	assert.Equal(t, []string{"alpha", "zeta"}, needed["postgres"], "sorted")
	assert.Empty(t, needed["mysql"])
}

func TestExtensionNeedsFollowsTheLakeSpec(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "plain", Type: "ducklake", Path: "data/p.ducklake", Options: map[string]string{"data_path": "data/p"}}))
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "sq", Type: "ducklake", Path: "data/s.db", Options: map[string]string{"catalog": "sqlite", "data_path": "data/s"}}))
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "store", Type: "s3", Scope: "s3://bucket/", Region: "us-east-1"}))
	require.NoError(t, ws.AddConnection(ScopeLocal, urlLake("sqlite")))
	needed := map[string][]string{}
	for _, n := range ExtensionNeeds(ws) {
		needed[n.Name] = n.NeededBy
	}
	assert.Equal(t, []string{"lake", "plain", "sq"}, needed["ducklake"])
	assert.Equal(t, []string{"lake", "sq"}, needed["sqlite"])
	assert.Equal(t, []string{"lake", "store"}, needed["httpfs"])
}

func TestExtensionNeedsExpandsEnvReferences(t *testing.T) {
	t.Setenv("VIBE_TEST_LAKE_URL", "s3://bucket/lake/c.db")
	ws, _, _ := memWorkspace(t, nil)
	require.NoError(t, ws.AddConnection(ScopeLocal, ConnectionDef{Name: "elake", Type: "ducklake", Path: "${VIBE_TEST_LAKE_URL}", ReadOnly: boolPtr(true), Options: map[string]string{"data_path": "data/f"}}))
	needed := map[string][]string{}
	for _, n := range ExtensionNeeds(ws) {
		needed[n.Name] = n.NeededBy
	}
	assert.Equal(t, []string{"elake"}, needed["httpfs"])
}
