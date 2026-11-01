package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnAdd_DuckLakeSQLiteCatalogInObjectStorage(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runSlaw("conn", "add", "store", "--type", "s3", "--prefix", "s3://acme/", "--region", "us-east-1", "--workspace", root)
	require.NoError(t, err, stderr)
	_, stderr, err = runSlaw("conn", "add", "lake", "--type", "ducklake", "--path", "s3://acme/lake/catalog.db",
		"--option", "catalog=sqlite", "--option", "data_path=s3://acme/lake/data/", "--workspace", root)
	require.NoError(t, err, stderr)
	out, stderr, err := runSlaw("conn", "list", "--json", "--workspace", root)
	require.NoError(t, err, stderr)
	// Resolved against the store's scope: not an issue, and nothing connected yet
	// (extension-missing where the extensions are not downloaded, as on CI).
	assert.Contains(t, out, `"name": "lake"`)
	assert.Regexp(t, `"name": "lake",[^}]*"status": "(not-connected|extension-missing)"`, out)
	assert.NotContains(t, out, "not inside the scope")

	// Read-only only: a writable one is refused.
	_, _, err = runSlaw("conn", "add", "wlake", "--type", "ducklake", "--path", "s3://acme/lake/c2.db",
		"--option", "data_path=s3://acme/lake/data/", "--read-only=false", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "can only be read")
}

func addLake(t *testing.T, root, name, path, dataPath string, extra ...string) string {
	t.Helper()
	args := []string{"conn", "add", name, "--type", "ducklake", "--path", path, "--option", "catalog=sqlite"}
	if dataPath != "" {
		args = append(args, "--option", "data_path="+dataPath)
	}
	args = append(args, extra...)
	args = append(args, "--workspace", root)
	out, stderr, err := runSlaw(args...)
	require.NoError(t, err, stderr)
	return out
}

func TestConnAdd_HintsTheMissingStorageConnection(t *testing.T) {
	root := newWS(t)
	out := addLake(t, root, "lake", "s3://lake/warehouse/catalog.sqlite", "s3://lake/warehouse/data/")
	assert.Contains(t, out, "Added ducklake connection lake")
	assert.Equal(t, 1, strings.Count(out, "No storage connection covers"), "one command for catalog and data path in one folder")
	assert.Contains(t, out, "No storage connection covers s3://lake/warehouse/. Add one: slaw conn add lake_s3 --type s3 --prefix s3://lake/warehouse/ --scope local")
	assert.Contains(t, out, "--region")
	assert.Contains(t, out, "--option endpoint=")

	// Two buckets, two commands.
	out = addLake(t, root, "lake2", "s3://one/c.sqlite", "s3://two/data/")
	assert.Equal(t, 2, strings.Count(out, "No storage connection covers"))
	assert.Contains(t, out, "--prefix s3://one/ ")
	assert.Contains(t, out, "--prefix s3://two/data/ ")

	// The lake's own scope is named.
	out = addLake(t, root, "ulake", "s3://u/c.sqlite", "s3://u/d/", "--scope", "user")
	assert.Contains(t, out, "--scope user")

	// An iceberg location.
	out, stderr, err := runSlaw("conn", "add", "ice", "--type", "iceberg", "--path", "gs://warehouse/tables/sales", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "slaw conn add warehouse_gcs --type gcs --prefix gs://warehouse/tables/ --scope local")
}

func TestConnAdd_NoHintWhenAStorageConnectionCoversIt(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runSlaw("conn", "add", "store", "--type", "s3", "--prefix", "s3://acme/", "--workspace", root)
	require.NoError(t, err, stderr)
	out := addLake(t, root, "lake", "s3://acme/lake/c.sqlite", "s3://acme/lake/data/")
	assert.NotContains(t, out, "No storage connection")
	// A local lake never gets one.
	out = addLake(t, root, "local_lake", "data/wh.ducklake", "data/wh_files")
	assert.NotContains(t, out, "No storage connection")
}

func TestConnAdd_HintNeverPrintsAnExpandedEnvValue(t *testing.T) {
	root := newWS(t)
	t.Setenv("LAKE_BUCKET", "expandedbucket")
	out := addLake(t, root, "lake", "s3://${LAKE_BUCKET}/wh/c.sqlite", "s3://${LAKE_BUCKET}/wh/d/")
	assert.NotContains(t, out, "expandedbucket")
	assert.Contains(t, out, "No storage connection covers s3://${LAKE_BUCKET}/wh/c.sqlite.")
	assert.Contains(t, out, "--prefix '<bucket URL>'")
	assert.NotRegexp(t, `(?i)password|secret|key_id`, out)
}

func TestConnAdd_HintDescribesTheDefinitionJustSaved(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runSlaw("conn", "add", "t", "--type", "iceberg", "--path", "s3://localbkt/t/", "--scope", "local", "--workspace", root)
	require.NoError(t, err, stderr)
	out, stderr, err := runSlaw("conn", "add", "t", "--type", "iceberg", "--path", "s3://userbkt/t/", "--scope", "user", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "s3://userbkt/t/")
	assert.NotContains(t, out, "localbkt")
}

func TestConnAdd_HintIsShellSafeAndComplete(t *testing.T) {
	root := newWS(t)
	out, stderr, err := runSlaw("conn", "add", "ice", "--type", "iceberg", "--path", "s3://b/$(touch$IFS/tmp/pwn)/t", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "--prefix 's3://b/$(touch$IFS/tmp/pwn)/'")

	out, stderr, err = runSlaw("conn", "add", "rt", "--type", "iceberg", "--path", "r2://rb/t/", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "--type r2 --prefix r2://rb/t/ --option 'account_id=<account id>' --scope local")

	out, stderr, err = runSlaw("conn", "add", "at", "--type", "delta", "--path", "abfss://ev@acct.dfs.core.windows.net/t/", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "--type azure --prefix abfss://ev@acct.dfs.core.windows.net/t/ --option account_name=acct --scope local")

	// A table folder without its slash: the parent is suggested, with a note.
	out, stderr, err = runSlaw("conn", "add", "gt", "--type", "delta", "--path", "gs://wh/tables/sales", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "--prefix gs://wh/tables/ ")
	assert.Contains(t, out, "trailing slash")
}

func TestConnAdd_TwoTemplatedBucketsGiveTwoHints(t *testing.T) {
	root := newWS(t)
	t.Setenv("HB_A", "aaa")
	t.Setenv("HB_B", "bbb")
	out := addLake(t, root, "lake", "s3://${HB_A}/c.sqlite", "s3://${HB_B}/d/")
	assert.Equal(t, 2, strings.Count(out, "No storage connection covers"))
	assert.NotContains(t, out, "aaa")
	assert.NotContains(t, out, "bbb")
}
