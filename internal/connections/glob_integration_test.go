//go:build integration

package connections_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/engine"
	"github.com/SandwichLabs/assay/internal/secrets"
	"github.com/SandwichLabs/assay/internal/session"
	"github.com/SandwichLabs/assay/internal/storageglob"
	"github.com/SandwichLabs/assay/internal/workspace"
)

// Wildcards on s3, gcs and r2 (#138 follow-up) against a real S3-compatible
// store: Assay lists the matching objects in a private session and the hub reads
// the list as literal keys.
func TestS3WildcardsAreExpandedOnMinIO(t *testing.T) {
	ep := startMinIO(t)
	evil := startEvil(t)
	ctx := context.Background()
	ws, _ := memWS(t)
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{
		Name: "lake", Type: "s3", Scope: "s3://lake/data/", Region: "us-east-1",
		Options: map[string]string{"endpoint": ep, "url_style": "path", "use_ssl": "false"}}))
	require.NoError(t, ws.SetCredentials(ctx, "lake", secrets.Credential{KeyID: "minioadmin", Secret: minioSecret}, false))
	sess, issues, err := workspace.OpenSession(ctx, ws, nil, workspace.WithSessionOptions(session.WithExtensionDirectory(extDir(t))))
	require.NoError(t, err)
	require.Empty(t, issues)
	defer sess.Close() //nolint:errcheck
	eng := engine.NewManager(sess)

	run := func(q string) [][]any {
		t.Helper()
		rs, err := eng.Execute(ctx, q, nil)
		require.NoError(t, err, q)
		return rs.Rows
	}
	// Objects: two parquet files and a csv at the top, one parquet under sub/,
	// a Hive-style layout, and an object whose NAME is a redirect attempt.
	run("COPY (SELECT 1 AS id, 'a' AS v UNION ALL SELECT 2, 'b') TO 's3://lake/data/a.parquet'")
	run("COPY (SELECT 3 AS id, 'c' AS v) TO 's3://lake/data/b.parquet'")
	run("COPY (SELECT 4 AS id, 'd' AS v UNION ALL SELECT 5, 'e') TO 's3://lake/data/sub/c.parquet'")
	run("COPY (SELECT 6 AS id, 'f' AS v) TO 's3://lake/data/part/year=2024/p.parquet'")
	run("COPY (SELECT 7 AS id, 'g' AS v) TO 's3://lake/data/part/year=2025/p.parquet'")
	run("COPY (SELECT 8 AS id, 'h' AS v) TO 's3://lake/data/t.csv' (FORMAT csv, HEADER)")
	t.Run("star", func(t *testing.T) {
		assert.EqualValues(t, 3, run("SELECT count(*) FROM read_parquet('s3://lake/data/*.parquet')")[0][0], "a(2) + b(1), not sub/")
		_, err := eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://lake/data/[ab].parquet')", nil)
		require.Error(t, err, "[ is no wildcard: it is part of a name that does not exist")
	})
	t.Run("double star", func(t *testing.T) {
		assert.EqualValues(t, 7, run("SELECT count(*) FROM read_parquet('s3://lake/data/**/*.parquet')")[0][0], "3 + sub 2 + 2 partitions")
		assert.EqualValues(t, 6, run("SELECT count(*) FROM glob('s3://lake/data/**')")[0][0], "every object: 5 parquet and the csv")
	})
	t.Run("glob", func(t *testing.T) {
		rows := run("SELECT file FROM glob('s3://lake/data/*.parquet') ORDER BY file")
		require.Len(t, rows, 2)
		assert.Equal(t, "s3://lake/data/a.parquet", rows[0][0])
		assert.Equal(t, "s3://lake/data/b.parquet", rows[1][0])
	})
	t.Run("FROM pattern", func(t *testing.T) {
		assert.EqualValues(t, 3, run("SELECT count(*) FROM 's3://lake/data/*.parquet'")[0][0])
		assert.EqualValues(t, 1, run("SELECT count(*) FROM 's3://lake/data/*.csv'")[0][0])
		assert.EqualValues(t, 4, run("SELECT count(*) FROM 's3://lake/data/sub/*.parquet' AS s JOIN 's3://lake/data/a.parquet' a ON a.id < s.id")[0][0])
	})
	t.Run("hive partitioning", func(t *testing.T) {
		rows := run("SELECT year, count(*) FROM read_parquet('s3://lake/data/part/*/*.parquet', hive_partitioning = true) GROUP BY year ORDER BY year")
		require.Len(t, rows, 2)
		assert.EqualValues(t, 2024, rows[0][0])
	})
	t.Run("in a table, with a join and a list", func(t *testing.T) {
		run("CREATE TABLE pulled AS SELECT * FROM read_parquet(['s3://lake/data/*.parquet', 's3://lake/data/sub/*.parquet'])")
		assert.EqualValues(t, 5, run("SELECT count(*) FROM pulled")[0][0])
		run("INSERT INTO pulled SELECT id, v FROM 's3://lake/data/part/*/*.parquet'")
		assert.EqualValues(t, 7, run("SELECT count(*) FROM pulled")[0][0])
	})
	t.Run("COPY of an expanded read to a local file", func(t *testing.T) {
		out := t.TempDir() + "/out.csv"
		run("COPY (SELECT * FROM read_parquet('s3://lake/data/sub/*.parquet')) TO '" + out + "'")
	})
	t.Run("nothing matches", func(t *testing.T) {
		_, err := eng.Execute(ctx, "SELECT * FROM read_parquet('s3://lake/data/zzz-*.parquet')", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `no files found that match the pattern "s3://lake/data/zzz-*.parquet"`)
	})
	t.Run("a pattern outside the scope is not listed", func(t *testing.T) {
		_, err := eng.Execute(ctx, "SELECT * FROM read_parquet('s3://lake/*.parquet')", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no storage connection")
	})
	t.Run("the cap", func(t *testing.T) {
		lister, ok := sess.(storageglob.Lister)
		require.True(t, ok)
		_, _, err := storageglob.Expand(ctx, "SELECT * FROM read_parquet('s3://lake/data/**/*.parquet')", storageglob.Options{Lister: lister, Max: 3})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "more than 3 objects")
		out, rw, err := storageglob.Expand(ctx, "SELECT * FROM read_parquet('s3://lake/data/**/*.parquet')", storageglob.Options{Lister: lister, Max: 5})
		require.NoError(t, err)
		assert.Equal(t, 5, rw.Keys)
		assert.Equal(t, 5, strings.Count(out, "s3://lake/data/"))
	})
	t.Run("a stale listing is not kept", func(t *testing.T) {
		before := run("SELECT count(*) FROM 's3://lake/data/*.csv'")[0][0]
		run("COPY (SELECT 10 AS id, 'j' AS v) TO 's3://lake/data/t2.csv' (FORMAT csv, HEADER)")
		after := run("SELECT count(*) FROM 's3://lake/data/*.csv'")[0][0]
		assert.EqualValues(t, 1, before)
		assert.EqualValues(t, 2, after, "the list is taken each time the statement runs")
	})

	t.Run("an object named like a redirect is refused by wildcards, readable by its exact name", func(t *testing.T) {
		hostile := "s3://lake/data/hostile/h.parquet?s3_endpoint=" + evil.Host
		run("COPY (SELECT 9 AS id, 'i' AS v) TO " + session.QuoteLiteral(hostile) + " (FORMAT parquet)")
		_, err := eng.Execute(ctx, "SELECT count(*) FROM read_parquet('s3://lake/data/hostile/*')", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "1 of the objects")
		_, err = eng.Execute(ctx, "SELECT count(*) FROM glob('s3://lake/data/**')", nil)
		require.Error(t, err, "a wildcard that would list it is refused")
		assert.EqualValues(t, 1, run("SELECT count(*) FROM read_parquet(" + session.QuoteLiteral(hostile) + ")")[0][0], "the exact name is a literal key on the connection's own endpoint")
	})

	assert.Empty(t, evil.seen(), "the object named ?s3_endpoint=... was read by name; the host it named saw nothing")

	// Test's listing of the whole scope still counts it.
	// (the engine above is the only reader; nothing was sent to the wrong key either)
	var on string
	require.NoError(t, sess.DB().QueryRow("SELECT CAST(current_setting('s3_url_compatibility_mode') AS VARCHAR)").Scan(&on))
	assert.Equal(t, "true", on, "the hub is still sealed after all of it")
}
