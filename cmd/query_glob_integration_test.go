//go:build integration

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/testsupport/miniotest"
)

// A wildcard on an s3, gcs or r2 URL in `vibe query` (#138 follow-up): the CLI
// reads every such URL as a literal key, so Vibe expands the pattern itself.
func TestQueryExpandsAnS3WildcardThroughTheCLI(t *testing.T) {
	ep := miniotest.Start(t)
	miniotest.UseExtensionCache(t)

	root := newWS(t)
	t.Setenv("FLOCI_KEY_ID", miniotest.KeyID)
	t.Setenv("FLOCI_SECRET", miniotest.Secret)
	b := "s3://" + miniotest.Bucket + "/raw/"
	var all string
	run := func(args ...string) string {
		t.Helper()
		out, stderr, err := runVibe(append(args, "--workspace", root)...)
		all += out + stderr
		require.NoError(t, err, stderr)
		return out
	}
	run("conn", "add", "floci", "--type", "s3", "--prefix", b, "--region", "us-east-1", "--sign-in", "env:FLOCI",
		"--option", "endpoint="+ep, "--option", "url_style=path", "--option", "use_ssl=false")
	run("query", "COPY (SELECT 1 AS n UNION ALL SELECT 2) TO '"+b+"a.parquet'")
	run("query", "COPY (SELECT 3 AS n) TO '"+b+"b.parquet'")
	run("query", "COPY (SELECT 4 AS n) TO '"+b+"sub/c.parquet'")

	out := run("query", "SELECT sum(n) AS total, count(*) AS rows FROM read_parquet('"+b+"*.parquet')", "--format", "csv")
	assert.Contains(t, out, "6,3", "a.parquet and b.parquet (1+2+3, three rows)")
	out = run("query", "SELECT count(*) AS n FROM '"+b+"**/*.parquet'", "--format", "csv")
	assert.Contains(t, out, "4")
	out = run("query", "SELECT count(*) AS n FROM glob('"+b+"*')", "--format", "csv")
	assert.Contains(t, out, "2")

	// A pattern no connection covers is refused, and says so.
	_, stderr, err := runVibe("query", "SELECT * FROM read_parquet('s3://elsewhere/*.parquet')", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error()+stderr, "no storage connection")
	_, stderr, err = runVibe("query", "SELECT * FROM read_parquet('"+b+"none-*.parquet')", "--workspace", root)
	require.Error(t, err)
	assert.Contains(t, err.Error()+stderr, "no files found")

	assert.NotContains(t, all, miniotest.Secret)
}
