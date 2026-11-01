//go:build integration

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/testsupport/miniotest"
)

// The case #118 was opened for, through the CLI exactly as a person types it:
// a SQLite DuckLake catalog and its Parquet files in a no-TLS, path-style
// S3-compatible store. floci signs in from FLOCI_KEY_ID / FLOCI_SECRET (nothing
// is stored), and the lake is read first, without connecting floci by hand.
func TestFlociLakeThroughTheCLI(t *testing.T) {
	ep := miniotest.Start(t)
	miniotest.BuildLake(t, ep)
	miniotest.UseExtensionCache(t)

	root := newWS(t)
	cfg := os.Getenv("ASSAY_CONFIG_DIR")
	t.Setenv("FLOCI_KEY_ID", miniotest.KeyID)
	t.Setenv("FLOCI_SECRET", miniotest.Secret)
	b := "s3://" + miniotest.Bucket + "/"
	var all string
	run := func(args ...string) string {
		t.Helper()
		out, stderr, err := runAssay(append(args, "--workspace", root)...)
		all += out + stderr
		require.NoError(t, err, stderr)
		return out
	}
	run("conn", "add", "floci", "--type", "s3", "--prefix", b, "--region", "us-east-1", "--sign-in", "env:FLOCI",
		"--option", "endpoint="+ep, "--option", "url_style=path", "--option", "use_ssl=false")
	run("conn", "add", "lake", "--type", "ducklake", "--path", b+"warehouse/ducklake/catalog.sqlite",
		"--option", "catalog=sqlite", "--option", "data_path="+b+"warehouse/ducklake/")

	list := run("conn", "list", "--json")
	assert.Regexp(t, `"name": "lake",[^}]*"status": "not-connected"`, list)
	assert.Contains(t, list, `"read_only": true`)

	out := run("query", "SELECT count(*) AS n FROM lake.warehouse.dim_students", "--format", "csv")
	assert.Contains(t, out, fmt.Sprint(miniotest.Students))

	out = run("query", "SELECT count(*) AS n FROM lake.warehouse.dim_students", "--format", "json")
	assert.Contains(t, out, fmt.Sprint(miniotest.Students))

	// A write is refused: the lake is read-only (and its catalog is a URL).
	_, stderr, err := runAssay("query", "INSERT INTO lake.warehouse.dim_students VALUES (1, 'x', 1)", "--workspace", root)
	require.Error(t, err)
	assert.NotContains(t, err.Error()+stderr, miniotest.Secret)

	// Without the variables, the lake names what it waits on.
	t.Setenv("FLOCI_SECRET", "")
	t.Setenv("FLOCI_KEY_ID", "")
	list = run("conn", "list", "--json")
	assert.Contains(t, list, `"name": "floci"`)
	assert.Contains(t, list, `"status": "needs-credentials"`)
	_, stderr, err = runAssay("query", "SELECT count(*) FROM lake.warehouse.dim_students", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `needs the "floci" connection`)

	assert.NotContains(t, all+stderr, miniotest.Secret)
	for _, d := range []string{root, cfg} {
		_ = filepath.Walk(d, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				if data, rerr := os.ReadFile(p); rerr == nil {
					assert.NotContains(t, string(data), miniotest.Secret, p)
				}
			}
			return nil
		})
	}
}
