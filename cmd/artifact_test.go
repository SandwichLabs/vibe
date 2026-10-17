package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const cliArtifactManifest = "version: 1\ntitle: Sales by region\nqueries:\n  - name: rows\n    sql: SELECT secret_column FROM secret_table\n"
const cliArtifactView = `{"title":"T","components":[{"id":"t","type":"table","query":"rows"}]}`

func seedArtifacts(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "artifacts", "sales")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(cliArtifactManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "view.a2ui.json"), []byte(cliArtifactView), 0o644))
	bad := filepath.Join(root, "artifacts", "broken")
	require.NoError(t, os.MkdirAll(bad, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(bad, "artifact.yaml"), []byte("surprise: 1\n"), 0o644))
}

func TestArtifactList_EmptyValidAndBroken(t *testing.T) {
	ws := newWS(t)
	out, stderr, err := runVibe("artifact", "list", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "No data artifacts")

	seedArtifacts(t, ws)
	out, stderr, err = runVibe("artifact", "list", "--workspace", ws)
	require.NoError(t, err, stderr)
	assert.Contains(t, out, "NAME")
	assert.Regexp(t, `sales\s+Sales by region\s+a2ui\s+-`, out)
	assert.Regexp(t, `broken\s+-\s+-\s+\S+`, out, "a broken artifact lists with its error")
	assert.NotContains(t, out, "secret_table", "the list shows what the Studio list shows: never the SQL")
	assert.NotContains(t, out, "secret_column")

	out, stderr, err = runVibe("artifact", "list", "--json", "--workspace", ws)
	require.NoError(t, err, stderr)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &rows))
	require.Len(t, rows, 2)
	assert.Equal(t, "broken", rows[0]["name"])
	assert.NotEmpty(t, rows[0]["error"])
	assert.Equal(t, map[string]any{"name": "sales", "title": "Sales by region", "view": "a2ui"}, rows[1])
}

func TestBundle_ArtifactsRoundTripThroughTheCLI(t *testing.T) {
	src := newWS(t)
	seedArtifacts(t, src)
	out := filepath.Join(t.TempDir(), "share.vibe")
	stdout, stderr, err := runVibe("export", "bundle", "-o", out, "--artifacts", "sales", "--workspace", src)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "1 data artifacts")

	dst := newWS(t)
	stdout, stderr, err = runVibe("import", out, "--workspace", dst)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "artifact/sales")
	want, _ := os.ReadFile(filepath.Join(src, "artifacts", "sales", "artifact.yaml"))
	got, err := os.ReadFile(filepath.Join(dst, "artifacts", "sales", "artifact.yaml"))
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got))
}
