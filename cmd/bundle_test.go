package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/bundle"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/workspace"
)

func seedBundleWS(t *testing.T, root string) {
	t.Helper()
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close()
	_, err = ws.Notebooks().Create(context.Background(), "Quarterly review")
	require.NoError(t, err)
	require.NoError(t, ws.Queries().Create(context.Background(), &queries.Query{Title: "Top customers", SQL: "SELECT 1"}))
}

func TestBundle_ExportImportRoundTripAndConflicts(t *testing.T) {
	src := newWS(t)
	seedBundleWS(t, src)
	out := filepath.Join(t.TempDir(), "share.slaw")

	stdout, stderr, err := runSlaw("export", "bundle", "-o", out, "--workspace", src)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Exported 1 notebooks, 1 queries, 0 skills")
	assert.Contains(t, stdout, "no data and no credentials")

	_, _, err = runSlaw("export", "bundle", "-o", out, "--workspace", src)
	require.Error(t, err, "an existing file is not replaced without --force")

	dst := newWS(t)
	stdout, stderr, err = runSlaw("import", "bundle", out, "--dry-run", "--workspace", dst)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Dry run")
	_, statErr := os.Stat(filepath.Join(dst, "notebooks", "quarterly-review.slnb"))
	assert.True(t, os.IsNotExist(statErr))

	// `slaw import <file.slaw>` routes to the same path.
	stdout, stderr, err = runSlaw("import", out, "--workspace", dst)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "notebook/quarterly-review")
	assert.FileExists(t, filepath.Join(dst, "notebooks", "quarterly-review.slnb"))

	stdout, _, err = runSlaw("import", "bundle", out, "--workspace", dst)
	require.NoError(t, err)
	assert.Contains(t, stdout, "renamed as quarterly-review-2")
	stdout, _, err = runSlaw("import", "bundle", out, "--on-conflict", "skip", "--workspace", dst)
	require.NoError(t, err)
	assert.Contains(t, stdout, "skipped")
	_, _, err = runSlaw("import", "bundle", out, "--on-conflict", "merge", "--workspace", dst)
	require.Error(t, err)
}

func TestBundle_SelectionAndEncryptionViaEnv(t *testing.T) {
	src := newWS(t)
	seedBundleWS(t, src)
	out := filepath.Join(t.TempDir(), "enc.slaw")

	// Not a terminal and no env var: refused, nothing written.
	_, _, err := runSlaw("export", "bundle", "--encrypt", "--notebooks", "quarterly-review", "-o", out, "--workspace", src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), bundlePassphraseEnv)
	assert.NoFileExists(t, out)

	t.Setenv(bundlePassphraseEnv, "open sesame")
	stdout, stderr, err := runSlaw("export", "bundle", "--encrypt", "--notebooks", "quarterly-review", "-o", out, "--workspace", src)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "1 notebooks, 0 queries")
	assert.Contains(t, stdout, "encrypted")
	data, err := os.ReadFile(out)
	require.NoError(t, err)
	info, err := bundle.Peek(data)
	require.NoError(t, err)
	assert.True(t, info.Encrypted)

	dst := newWS(t)
	t.Setenv(bundlePassphraseEnv, "nope")
	_, _, err = runSlaw("import", "bundle", out, "--workspace", dst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong passphrase")
	t.Setenv(bundlePassphraseEnv, "open sesame")
	_, stderr, err = runSlaw("import", "bundle", out, "--workspace", dst)
	require.NoError(t, err, stderr)
	assert.FileExists(t, filepath.Join(dst, "notebooks", "quarterly-review.slnb"))
	assert.NoFileExists(t, filepath.Join(dst, "queries", "top-customers.sql"))
}

func TestBundle_ConnectionsNeedAnExplicitFlag(t *testing.T) {
	src := newWS(t)
	seedBundleWS(t, src)
	_, stderr, err := runSlaw("conn", "add", "wh", "--type", "postgres", "--host", "db.internal", "--database", "d", "--scope", "shared", "--workspace", src)
	require.NoError(t, err, stderr)
	out := filepath.Join(t.TempDir(), "c.slaw")
	_, stderr, err = runSlaw("export", "bundle", "--with-connections", "-o", out, "--workspace", src)
	require.NoError(t, err, stderr)

	dst := newWS(t)
	_, stderr, err = runSlaw("import", "bundle", out, "--workspace", dst)
	require.NoError(t, err)
	assert.Contains(t, stderr, "--with-connections")
	ws, err := workspace.Open(dst, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	assert.Empty(t, ws.Local().Connections)
	_ = ws.Close()

	_, stderr, err = runSlaw("import", "bundle", out, "--with-connections", "all", "--workspace", dst)
	require.NoError(t, err, stderr)
	ws, err = workspace.Open(dst, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close()
	require.Len(t, ws.Local().Connections, 1)
	assert.Equal(t, "wh", ws.Local().Connections[0].Name)
}

func TestNotebook_ExportImportThroughTheSharedPath(t *testing.T) {
	src := newWS(t)
	seedBundleWS(t, src)
	out := filepath.Join(t.TempDir(), "qr.slnb")
	stdout, stderr, err := runSlaw("export", "notebook", "quarterly-review", "-o", out, "--workspace", src)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "Exported notebook quarterly-review")
	_, _, err = runSlaw("export", "notebook", "quarterly-review", "-o", out, "--workspace", src)
	require.Error(t, err, "no clobbering without --force")
	_, _, err = runSlaw("export", "notebook", "../etc", "--workspace", src)
	require.Error(t, err)

	dst := newWS(t)
	stdout, stderr, err = runSlaw("import", "notebook", out, "--workspace", dst)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "notebook/qr")
	assert.FileExists(t, filepath.Join(dst, "notebooks", "qr.slnb"))
	a, _ := os.ReadFile(filepath.Join(src, "notebooks", "quarterly-review.slnb"))
	b, _ := os.ReadFile(filepath.Join(dst, "notebooks", "qr.slnb"))
	assert.Equal(t, string(a), string(b))
	stdout, _, err = runSlaw("import", "notebook", out, "--workspace", dst)
	require.NoError(t, err)
	assert.Contains(t, stdout, "renamed as qr-2")
	stdout, _, err = runSlaw("import", "notebook", out, "--on-conflict", "skip", "--workspace", dst)
	require.NoError(t, err)
	assert.Contains(t, stdout, "skipped")
	bad := writeFile(t, t.TempDir(), "bad.slnb", "nope")
	_, _, err = runSlaw("import", "notebook", bad, "--workspace", dst)
	require.Error(t, err)
}

func TestBundle_ImportPreviewShowsConnectionDetailsAndRisks(t *testing.T) {
	src := newWS(t)
	seedBundleWS(t, src)
	_, stderr, err := runSlaw("conn", "add", "lake", "--type", "s3", "--prefix", "s3://acme-prod/", "--region", "us-east-1", "--scope", "shared", "--workspace", src)
	require.NoError(t, err, stderr)
	out := filepath.Join(t.TempDir(), "c.slaw")
	_, stderr, err = runSlaw("export", "bundle", "--with-connections", "-o", out, "--workspace", src)
	require.NoError(t, err, stderr)
	dst := newWS(t)
	_, stderr, err = runSlaw("import", "bundle", out, "--workspace", dst)
	require.NoError(t, err)
	assert.Contains(t, stderr, "connection lake (s3)")
	assert.Contains(t, stderr, "region us-east-1")
	assert.Contains(t, stderr, "WARNING: Its storage scope is a whole bucket")
	assert.Contains(t, stderr, "read-only")
}
