package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/connections"
	"github.com/SandwichLabs/assay/internal/secrets"
	"github.com/SandwichLabs/assay/internal/session"
)

const trNeedle = "storage_secret_needle_trq"

func storeConn(name string) ConnectionDef {
	return ConnectionDef{Name: name, Type: "s3", Scope: "s3://lake/", Region: "us-east-1",
		Options: map[string]string{"endpoint": "127.0.0.1:1", "url_style": "path", "use_ssl": "false"}}
}

func lakeConn(name string) ConnectionDef {
	return ConnectionDef{Name: name, Type: "ducklake", Path: "s3://lake/cat.ducklake", Options: map[string]string{"data_path": "s3://lake/data/"}}
}

// trWorkspace has a lake in the local scope and a storage connection "store"
// covering it.
func trWorkspace(t *testing.T, env map[string]string) Workspace {
	t.Helper()
	ws, _, _ := memWorkspace(t, env)
	require.NoError(t, ws.AddConnection(ScopeLocal, storeConn("store")))
	require.NoError(t, ws.AddConnection(ScopeLocal, lakeConn("sales")))
	return ws
}

func trTest(t *testing.T, ws Workspace) (connections.TestResult, error) {
	t.Helper()
	rc := savedRC(t, ws, "sales")
	require.Equal(t, []string{"store"}, rc.Requires)
	return TestWithRequirements(context.Background(), ws, rc, "", secrets.Credential{}, connections.WithTestExtensionDir(t.TempDir()))
}

func TestTestWithRequirements_StorageWithoutCredentialsIsTheFailureAndTheLakeIsNotTried(t *testing.T) {
	ws := trWorkspace(t, nil)
	res, err := trTest(t, ws)
	require.NoError(t, err)
	require.False(t, res.OK)
	assert.Equal(t, "store", res.Requirement)
	assert.Equal(t, session.StatusNeedsCredentials, res.Status)
	assert.NotContains(t, res.Detail, "object storage", "the lake was not read")
}

func TestTestWithRequirements_UsesTheStorageConnectionsOwnSavedCredential(t *testing.T) {
	ws := trWorkspace(t, nil)
	require.NoError(t, ws.SetCredentials(context.Background(), "store", secrets.Credential{KeyID: "AKIAtr", Secret: trNeedle}, false))
	res, err := trTest(t, ws)
	require.NoError(t, err)
	require.False(t, res.OK)
	// The credential was found and used: the storage attach then stops at the
	// (empty) extension directory, and says so without the secret.
	assert.Equal(t, "store", res.Requirement)
	assert.Equal(t, session.StatusExtensionMissing, res.Status)
	for _, s := range []string{res.Error, res.Message, res.Detail} {
		assert.NotContains(t, s, trNeedle)
	}
}

func TestTestWithRequirements_AnEnvSignInThatNoLongerMatchesIsWithheld(t *testing.T) {
	ws := trWorkspace(t, map[string]string{"ST_KEY_ID": "AKIAenv", "ST_SECRET": trNeedle})
	require.NoError(t, ws.SetSignIn("store", "env:ST"))
	// A pull re-points the storage connection: the environment secret is not
	// sent to the new endpoint.
	up := storeConn("store")
	up.Options = map[string]string{"endpoint": "attacker.example", "url_style": "path"}
	require.NoError(t, ws.UpdateConnection(ScopeLocal, up))

	res, err := trTest(t, ws)
	require.NoError(t, err)
	require.False(t, res.OK)
	assert.Equal(t, "store", res.Requirement)
	assert.Equal(t, connections.CodeServerChanged, res.Code)
	assert.Equal(t, connections.ServerChangedMessage, res.Message)
	assert.Equal(t, session.StatusNeedsCredentials, res.Status)
	for _, s := range []string{res.Error, res.Message, res.Detail} {
		assert.NotContains(t, s, trNeedle)
	}
}

func TestTestWithRequirements_ALakeWhoseOwnSignInChangedRunsNoTest(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	require.NoError(t, ws.SetCredentials(ctx, "pg", secrets.Credential{User: "rosa", Password: tcNeedle}, false))
	edited := pgConn("pg")
	edited.Host = "other.internal"
	res, err := TestWithRequirements(ctx, ws, resolved(t, ws, edited), "", secrets.Credential{})
	var fp *secrets.ErrFingerprintChanged
	require.ErrorAs(t, err, &fp)
	assert.Equal(t, connections.TestResult{}, res)
}

func TestTestWithRequirements_AMissingCredentialIsReportedWithoutDialling(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	require.NoError(t, ws.AddConnection(ScopeShared, pgConn("pg")))
	res, err := TestWithRequirements(context.Background(), ws, savedRC(t, ws, "pg"), "", secrets.Credential{})
	var nc *secrets.ErrNeedsCredentials
	require.ErrorAs(t, err, &nc)
	assert.False(t, res.OK)
	assert.Equal(t, session.StatusNeedsCredentials, res.Status)
	assert.Equal(t, connections.CodeBadSignin, res.Code)
}
