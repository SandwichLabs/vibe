package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

const dotenvPW = "pw-168-fake-from-dotenv"

func TestDotEnv_BindingsAllowFeedsTestWithoutLeaking(t *testing.T) {
	fake := startFakePG(t)
	app := leakApp(t, nil)
	def := workspace.ConnectionDef{Name: "prod_pg", Type: "postgres", Host: "127.0.0.1", Port: fake.port(), Database: "app"}
	_, err := app.SaveConnection(SaveConnectionRequest{Scope: "shared", Def: def, IsNew: true, SignIn: "env:PGPROD"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(app.ws.Root(), ".env"), []byte("PGPROD_USER=rosa\nPGPROD_PASSWORD="+dotenvPW+"\n"), 0o600))

	st, err := app.GetDotEnvStatus()
	require.NoError(t, err)
	assert.Equal(t, "undecided", st.State)
	assert.Equal(t, []string{"PGPROD_PASSWORD", "PGPROD_USER"}, st.Names)
	require.Len(t, st.Satisfies, 1)
	raw, _ := json.Marshal(st)
	assert.NotContains(t, string(raw), dotenvPW)

	// Undecided: Test sends nothing.
	res, err := app.TestConnection(TestConnectionRequest{Scope: "shared", Def: def, SignIn: "env:PGPROD"})
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.NotContains(t, fake.all(), dotenvPW)

	// The connection's credential status explains it, names only.
	d, err := app.GetConnection("prod_pg")
	require.NoError(t, err)
	assert.Contains(t, d.Credential.Detail, "allow loading it")
	assert.NotContains(t, d.Credential.Detail, dotenvPW)

	st, err = app.AllowDotEnv()
	require.NoError(t, err)
	assert.Equal(t, "allowed", st.State)
	res, err = app.TestConnection(TestConnectionRequest{Scope: "shared", Def: def, SignIn: "env:PGPROD"})
	require.NoError(t, err)
	assert.False(t, res.OK) // the fake always refuses; the saved .env sign-in must have been used
	if res.InstallExtension == "" {
		assert.Contains(t, fake.all(), dotenvPW)
	}
	if res.Error != nil {
		assert.NotContains(t, res.Error.Message, dotenvPW)
	}

	st, err = app.DenyDotEnv()
	require.NoError(t, err)
	assert.Equal(t, "denied", st.State)
	st, err = app.RevokeDotEnv()
	require.NoError(t, err)
	assert.Equal(t, "undecided", st.State)
}
