package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnAdd_DuckLakeSQLiteCatalogInObjectStorage(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runVibe("conn", "add", "store", "--type", "s3", "--prefix", "s3://acme/", "--region", "us-east-1", "--workspace", root)
	require.NoError(t, err, stderr)
	_, stderr, err = runVibe("conn", "add", "lake", "--type", "ducklake", "--path", "s3://acme/lake/catalog.db",
		"--option", "catalog=sqlite", "--option", "data_path=s3://acme/lake/data/", "--workspace", root)
	require.NoError(t, err, stderr)
	out, stderr, err := runVibe("conn", "list", "--json", "--workspace", root)
	require.NoError(t, err, stderr)
	// Resolved against the store's scope: not an issue, and nothing connected yet
	// (extension-missing where the extensions are not downloaded, as on CI).
	assert.Contains(t, out, `"name": "lake"`)
	assert.Regexp(t, `"name": "lake",[^}]*"status": "(not-connected|extension-missing)"`, out)
	assert.NotContains(t, out, "not inside the scope")

	// Read-only only: a writable one is refused.
	_, _, err = runVibe("conn", "add", "wlake", "--type", "ducklake", "--path", "s3://acme/lake/c2.db",
		"--option", "data_path=s3://acme/lake/data/", "--read-only=false", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can only be read")
}
