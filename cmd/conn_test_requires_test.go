package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/connections"
	"github.com/SandwichLabs/assay/internal/session"
)

const cliStorageNeedle = "cli_storage_secret_needle"

// `assay conn test lake` tries the storage connection the lake reads with
// first, and says which part failed. The environment sign-in's secret is
// never printed.
func TestConnTest_StorageFailureIsNamedFirstAndTheLakeIsNotTried(t *testing.T) {
	root := newWS(t)
	t.Setenv("CLI_ST_KEY_ID", "AKIAcli")
	t.Setenv("CLI_ST_SECRET", cliStorageNeedle)
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // no extension is installed: no network either
	_, stderr, err := runAssay("conn", "add", "store", "--type", "s3", "--prefix", "s3://acme/", "--sign-in", "env:CLI_ST",
		"--option", "endpoint=127.0.0.1:1", "--option", "use_ssl=false", "--workspace", root)
	require.NoError(t, err, stderr)
	_ = addLake(t, root, "lake", "s3://acme/lake/c.sqlite", "s3://acme/lake/data/")

	out, stderr, err := runAssay("conn", "test", "lake", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `lake: storage connection "store" failed`)
	assert.Contains(t, err.Error(), "lake was not tried")
	assert.Contains(t, err.Error(), "assay conn test store")
	for _, s := range []string{out, stderr, err.Error()} {
		assert.NotContains(t, s, cliStorageNeedle)
	}
}

func TestConnTestFailure_Wording(t *testing.T) {
	msg := connTestFailure("lake", connections.TestResult{Status: session.StatusNeedsCredentials, Error: "denied", Requirement: "store"})
	assert.Equal(t, "lake: storage connection \"store\" failed (needs-credentials): denied\n(lake was not tried; fix \"store\" with `assay conn test store`)", msg)
	assert.Equal(t, "lake: error: boom", connTestFailure("lake", connections.TestResult{Status: session.StatusError, Error: "boom"}))
}
