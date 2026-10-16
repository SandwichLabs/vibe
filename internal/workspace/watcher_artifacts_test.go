package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/filestore"
)

const artifactManifest = "version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\n"
const artifactView = `{"title":"T","components":[]}`

func TestClassify_Artifacts(t *testing.T) {
	k, id, ok := classify("artifacts/regional-sales/artifact.yaml")
	assert.True(t, ok)
	assert.Equal(t, KindArtifact, k)
	assert.Equal(t, "regional-sales", id)
	k, id, ok = classify("artifacts/regional-sales/view.a2ui.json")
	assert.True(t, ok)
	assert.Equal(t, KindArtifact, k)
	assert.Equal(t, "regional-sales", id)

	for _, rel := range []string{
		"artifacts/x/" + filestore.TempPrefix + "123", // WriteAtomic's temp file
		"artifacts/x/artifact.yaml.swp",
		"artifacts/x/artifact.yaml~",
		"artifacts/x/notes.md",
		"artifacts/x/view.html",
		"artifacts/.hidden/artifact.yaml",
		"artifacts/artifact.yaml",
		"artifacts/x/y/artifact.yaml",
		"artifact/x/artifact.yaml",
	} {
		_, _, ok := classify(rel)
		assert.False(t, ok, rel)
	}
}

func TestWatcher_ArtifactsAreReportedAndOwnWritesAreNot(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	w := startWatcher(t, ws)

	// The store's own writes (both files) and delete are ignored.
	a := &artifacts.Artifact{Name: "mine", View: []byte(artifactView)}
	m, err := artifacts.Decode([]byte(artifactManifest))
	require.NoError(t, err)
	a.Manifest = *m
	require.NoError(t, ws.Artifacts().Put(ctx, a))
	a.Manifest.Title = "Mine, edited"
	require.NoError(t, ws.Artifacts().Put(ctx, a))
	require.NoError(t, ws.Artifacts().Delete(ctx, "mine"))
	expectQuiet(t, w)

	// A git pull creates the folder and both files at once.
	dir := filepath.Join(root, "artifacts", "pulled")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(artifactManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "view.a2ui.json"), []byte(artifactView), 0o644))
	seen := map[string]Change{}
	for i := 0; i < 2; i++ {
		c := next(t, w)
		seen[c.Path] = c
	}
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpCreated, ID: "pulled", Path: "artifacts/pulled/artifact.yaml"}, seen["artifacts/pulled/artifact.yaml"])
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpCreated, ID: "pulled", Path: "artifacts/pulled/view.a2ui.json"}, seen["artifacts/pulled/view.a2ui.json"])

	// Edits in the new folder are watched, and so is a deletion.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(artifactManifest+"description: edited\n"), 0o644))
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpChanged, ID: "pulled", Path: "artifacts/pulled/artifact.yaml"}, next(t, w))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.md"), []byte("not an artifact file"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".artifact.yaml.swp"), []byte("x"), 0o644))
	expectQuiet(t, w)
	require.NoError(t, os.Remove(filepath.Join(dir, "view.a2ui.json")))
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpDeleted, ID: "pulled", Path: "artifacts/pulled/view.a2ui.json"}, next(t, w))
}

func TestWatcher_ArtifactsFolderCreatedLaterIsWatched(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vibe.yaml"), []byte("id: "+testID+"\n"), 0o644))
	ws, err := Open(dir, noUser(t))
	require.NoError(t, err)
	defer ws.Close()
	w := startWatcher(t, ws)

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "artifacts", "late"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifacts", "late", "artifact.yaml"), []byte(artifactManifest), 0o644))
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpCreated, ID: "late", Path: "artifacts/late/artifact.yaml"}, next(t, w))
}

func TestWatcher_SymlinkedArtifactFolderIsNotWatched(t *testing.T) {
	ws, root := newWorkspace(t)
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "artifacts"), 0o755))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "artifacts", "linked")))
	w := startWatcher(t, ws)
	require.NoError(t, os.WriteFile(filepath.Join(outside, "artifact.yaml"), []byte(artifactManifest), 0o644))
	expectQuiet(t, w)
}

func TestWorkspace_TempDirsCoverArtifacts(t *testing.T) {
	ws, root := newWorkspace(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "artifacts", "one"), 0o755))
	w, ok := ws.(*workspace)
	require.True(t, ok)
	dirs := w.tempDirs()
	assert.Contains(t, dirs, "artifacts")
	assert.Contains(t, dirs, "artifacts/one")
}

func TestWatcher_ArtifactFolderExistingAtStartIsWatched(t *testing.T) {
	ws, root := newWorkspace(t)
	dir := filepath.Join(root, "artifacts", "pre")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(artifactManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "view.a2ui.json"), []byte(artifactView), 0o644))
	w := startWatcher(t, ws)
	expectQuiet(t, w)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(artifactManifest+"description: edited\n"), 0o644))
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpChanged, ID: "pre", Path: "artifacts/pre/artifact.yaml"}, next(t, w))
}
