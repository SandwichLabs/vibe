package bundle

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/notebooks"
)

func TestWithExt(t *testing.T) {
	assert.Equal(t, "/tmp/team.vibe", WithExt("/tmp/team"))
	assert.Equal(t, "/tmp/team.vibe", WithExt("/tmp/team.vibe"))
	assert.Equal(t, "/tmp/team.zip", WithExt("/tmp/team.zip"), "an extension the person typed is kept")
	assert.Equal(t, "/tmp/a.b/team.vibe", WithExt("/tmp/a.b/team"), "only the last element counts")
	assert.Equal(t, ".vibe", WithExt(""))
}

func TestManifestCounts(t *testing.T) {
	m := Manifest{Contents: []Entry{
		{Kind: KindNotebook}, {Kind: KindNotebook}, {Kind: KindQuery},
		{Kind: KindSkill}, {Kind: KindSkill}, {Kind: KindSkill},
		{Kind: KindConnections},
	}}
	assert.Equal(t, Counts{Notebooks: 2, Queries: 1, Skills: 3, Connections: 1}, m.Counts())
	assert.Equal(t, Counts{}, Manifest{}.Counts())
}

func TestWriteFile_AtomicAndNamesThePathOnFailure(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.vibe")
	require.NoError(t, WriteFile(p, []byte("one")))
	require.NoError(t, WriteFile(p, []byte("two")))
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "two", string(b))
	entries, _ := os.ReadDir(dir)
	assert.Len(t, entries, 1, "no temp file is left behind")

	bad := filepath.Join(dir, "missing-dir", "a.vibe")
	err = WriteFile(bad, []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write "+bad+": ")
}

func TestWriteNotebookFile_EnvelopeRoundTripsThroughReadNotebookFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nb.json")
	nb := notebooks.Notebook{
		SchemaVersion: notebooks.SchemaVersion, ID: "nb-1", Title: "Sales",
		Cells: []notebooks.Cell{{ID: "c1", Type: "markdown", Source: "# hi"}},
	}
	when := time.Date(2026, 10, 11, 12, 30, 0, 0, time.FixedZone("x", 3600))
	require.NoError(t, WriteNotebookFile(p, nb, when))

	b, err := os.ReadFile(p)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, "{\n  \"kind\": \"vibe/notebook\",\n  \"schemaVersion\": 2,\n  \"exportedAt\": \"2026-10-11T11:30:00Z\",\n  \"notebook\": {")
	assert.Contains(t, s, "\"title\": \"Sales\"")

	doc, err := ReadNotebookFile(p)
	require.NoError(t, err)
	inner, ok := doc["notebook"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "Sales", inner["title"])

	err = WriteNotebookFile(filepath.Join(dir, "nodir", "nb.json"), nb, when)
	assert.ErrorContains(t, err, "failed to write notebook file: ")
}
