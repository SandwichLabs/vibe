//go:build integration

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/testsupport/miniotest"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

// The Studio query editor (#138 follow-up): a wildcard on an s3, gcs or r2 URL
// is expanded by Vibe when the statement runs, the person's SQL is unchanged,
// and an error never lists the files.
func TestRunQueryExpandsAnS3Wildcard(t *testing.T) {
	ep := miniotest.Start(t)
	miniotest.UseExtensionCache(t)
	app, _ := connApp(t)
	b := "s3://" + miniotest.Bucket + "/raw/"
	require.NoError(t, app.ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "floci", Type: "s3", Scope: b, Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}))
	_, err := app.SetConnectionCredentials(SetCredentialsRequest{Name: "floci", CredentialInput: CredentialInput{KeyID: miniotest.KeyID, Secret: miniotest.Secret}})
	require.NoError(t, err)

	run := func(q string) *QueryResult {
		t.Helper()
		res, err := app.RunQuery(RunQueryRequest{SQL: q})
		require.NoError(t, err, q)
		return res
	}
	run("COPY (SELECT 1 AS n UNION ALL SELECT 2) TO '" + b + "a.parquet'")
	run("COPY (SELECT 3 AS n) TO '" + b + "b.parquet'")
	run("COPY (SELECT 4 AS n) TO '" + b + "sub/c.parquet'")

	res := run("SELECT n FROM read_parquet('" + b + "*.parquet') ORDER BY n")
	assert.Equal(t, 3, res.RowCount)
	res = run("SELECT count(*) AS n FROM '" + b + "**/*.parquet'")
	assert.EqualValues(t, 4, res.Rows[0][0])
	res = run("-- how many?\nSELECT count(*) AS n FROM glob('" + b + "*') /* all objects here */")
	assert.EqualValues(t, 2, res.Rows[0][0])
	res = run("CREATE TABLE pulled AS SELECT * FROM read_parquet('" + b + "*.parquet')")
	res = run("SELECT count(*) FROM pulled")
	assert.EqualValues(t, 3, res.Rows[0][0])

	_, err = app.RunQuery(RunQueryRequest{SQL: "SELECT nosuch FROM read_parquet('" + b + "*.parquet')"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "a.parquet", "an error never lists the files")
	_, err = app.RunQuery(RunQueryRequest{SQL: "SELECT * FROM read_parquet('" + b + "zzz-*.parquet')"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no files found that match the pattern")
	_, err = app.RunQuery(RunQueryRequest{SQL: "SELECT * FROM read_parquet('s3://elsewhere/*.parquet')"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no storage connection")
	assert.NotContains(t, err.Error(), miniotest.Secret)
}
