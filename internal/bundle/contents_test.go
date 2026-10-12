package bundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/notebooks"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

func TestContentsOf_ListsWhatCanGoInABundle(t *testing.T) {
	ws, _ := newWS(t)
	seed(t, ws)
	ctx := context.Background()
	require.NoError(t, ws.AddConnection(workspace.ScopeShared, workspace.ConnectionDef{Name: "zeta", Type: "duckdb", Path: "data/z.duckdb"}))
	require.NoError(t, ws.AddConnection(workspace.ScopeLocal, workspace.ConnectionDef{Name: "alpha", Type: "duckdb", Path: "/etc/elsewhere.duckdb"}))

	got, err := ContentsOf(ctx, ws)
	require.NoError(t, err)
	assert.Equal(t, ws.Name(), got.WorkspaceName)
	require.Len(t, got.Notebooks, 1)
	assert.Equal(t, "Quarterly review", got.Notebooks[0].Title)
	require.Len(t, got.Queries, 1)
	assert.Equal(t, "Top customers", got.Queries[0].Title)
	require.Len(t, got.Skills, 1, "built-in skills ship with Vibe and are not exported")
	assert.Equal(t, "revenue-notes", got.Skills[0].ID)

	// Sorted by name; the workspace's own default database is left out.
	require.Len(t, got.Connections, 2)
	assert.Equal(t, "alpha", got.Connections[0].Name)
	assert.Equal(t, "this computer", got.Connections[0].Scope)
	assert.False(t, got.Connections[0].Portable, "a path outside the workspace cannot leave this computer")
	assert.NotEmpty(t, got.Connections[0].Reason)
	assert.Equal(t, "zeta", got.Connections[1].Name)
	assert.Equal(t, "team", got.Connections[1].Scope)
	assert.True(t, got.Connections[1].Portable)
}

func TestContentsOf_EmptyWorkspaceHasEmptyListsNotNil(t *testing.T) {
	ws, _ := newWS(t)
	got, err := ContentsOf(context.Background(), ws)
	require.NoError(t, err)
	assert.NotNil(t, got.Notebooks)
	assert.NotNil(t, got.Queries)
	assert.NotNil(t, got.Skills)
	assert.NotNil(t, got.Connections)
	assert.Empty(t, got.Connections)
}

func TestContentsOf_SkipsItemsThatDoNotParse(t *testing.T) {
	ws, _ := newWS(t)
	seed(t, ws)
	require.NoError(t, os.WriteFile(filepath.Join(ws.Root(), "notebooks", "broken.vnb"), []byte("{not json"), 0o600))
	got, err := ContentsOf(context.Background(), ws)
	require.NoError(t, err)
	assert.Len(t, got.Notebooks, 1, "a notebook with an error is not offered")
}

func TestReadNotebookFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}
	good := write("good.json", `{"kind":"`+notebooks.FileKind+`","notebook":{"id":"keep-out","updatedAt":"x","title":"T","schemaVersion":2,"cells":[]}}`)
	doc, err := ReadNotebookFile(good)
	require.NoError(t, err)
	nested, ok := doc["notebook"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, nested, "id", "the file's own id is dropped so import lands as a new notebook")
	assert.NotContains(t, nested, "updatedAt")
	assert.Equal(t, "T", nested["title"])

	_, err = ReadNotebookFile(filepath.Join(dir, "missing.json"))
	assert.ErrorContains(t, err, "failed to read notebook file")
	_, err = ReadNotebookFile(write("bad.json", "{"))
	assert.ErrorContains(t, err, "invalid notebook file")
	_, err = ReadNotebookFile(write("pkg.json", `{"name":"x","version":"1"}`))
	assert.ErrorContains(t, err, "not a Vibe notebook file")
	_, err = ReadNotebookFile(write("wrongkind.json", `{"kind":"other"}`))
	assert.ErrorContains(t, err, `got "other"`)

	big := filepath.Join(dir, "big.json")
	f, err := os.Create(big)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(MaxEntryBytes+1))
	require.NoError(t, f.Close())
	_, err = ReadNotebookFile(big)
	assert.ErrorContains(t, err, "exceeding the")
}
