package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/SandwichLabs/vibe/internal/hub"
	"github.com/SandwichLabs/vibe/internal/lsp"
	"github.com/SandwichLabs/vibe/internal/workspace"
	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLSPSnapshot_UnknownConnectionIsAUsageErrorAndWritesNothing(t *testing.T) {
	root := newWS(t)
	_, stderr, err := runVibe("lsp", "snapshot", "-c", "nope", "--workspace", root)
	require.Error(t, err, stderr)
	assert.Contains(t, err.Error(), `"nope"`)
	_, statErr := os.Stat(lsp.SnapshotPath(root))
	assert.True(t, os.IsNotExist(statErr), "nothing is written")
}

func TestLSPSnapshot_NamedConnectionThatFailsStillWritesTheOthersThenFails(t *testing.T) {
	root := newWS(t)
	good := filepath.Join(t.TempDir(), "good.duckdb")
	db, err := sql.Open("duckdb", good)
	require.NoError(t, err)
	_, err = db.Exec("CREATE TABLE t(a INT)")
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, stderr, err := runVibe("conn", "add", "good", "--type", "duckdb", "--path", good, "--workspace", root)
	require.NoError(t, err, stderr)
	_, stderr, err = runVibe("conn", "add", "bad", "--type", "duckdb", "--path", filepath.Join(t.TempDir(), "missing", "x.duckdb"), "--workspace", root)
	require.NoError(t, err, stderr)

	_, stderr, err = runVibe("lsp", "snapshot", "-c", "good", "-c", "bad", "--workspace", root)
	require.Error(t, err, stderr)
	assert.Contains(t, err.Error(), "bad")
	cat, lerr := lsp.Load(lsp.SnapshotPath(root))
	require.NoError(t, lerr)
	assert.NotNil(t, cat.Conn("good"), "the connection that worked was written")
}

func TestLSPSnapshot_LocalFileLeftToTheOwnerIsNotAFailure(t *testing.T) {
	root := newWS(t)
	local := filepath.Join(t.TempDir(), "mine.duckdb")
	_, stderr, err := runVibe("conn", "add", "mylocal", "--type", "duckdb", "--path", local, "--workspace", root)
	require.NoError(t, err, stderr)
	ws, err := workspace.Open(root, workspace.WithLogger(testCmdLogger()))
	require.NoError(t, err)
	defer ws.Close() //nolint:errcheck
	hold, err := hub.AcquireHub(ws, "Vibe Studio")
	require.NoError(t, err)
	defer hold.Release() //nolint:errcheck

	_, stderr, err = runVibe("lsp", "snapshot", "-c", "mylocal", "--workspace", root)
	require.NoError(t, err, stderr)
	assert.Contains(t, stderr, "left to its owner")
}
