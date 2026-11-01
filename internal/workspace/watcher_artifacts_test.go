package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/artifacts"
	"github.com/SandwichLabs/assay/internal/filestore"
)

const artifactManifest = "version: 1\ntitle: T\nqueries:\n  - name: q\n    sql: SELECT 1\n"
const artifactView = `{"title":"T","components":[{"id":"root","type":"container"}]}`

func TestClassify_Artifacts(t *testing.T) {
	k, id, ok := classify("artifacts/regional-sales/artifact.yaml")
	assert.True(t, ok)
	assert.Equal(t, KindArtifact, k)
	assert.Equal(t, "regional-sales", id)
	k, id, ok = classify("artifacts/regional-sales/view.a2ui.json")
	assert.True(t, ok)
	assert.Equal(t, KindArtifact, k)
	assert.Equal(t, "regional-sales", id)
	k, id, ok = classify("artifacts/regional-sales/view.js")
	assert.True(t, ok, "a custom view's script is the artifact's file too")
	assert.Equal(t, KindArtifact, k)
	assert.Equal(t, "regional-sales", id)

	for _, rel := range []string{
		"artifacts/x/" + filestore.TempPrefix + "123", // WriteAtomic's temp file
		"artifacts/x/artifact.yaml.swp",
		"artifacts/x/artifact.yaml~",
		"artifacts/x/notes.md",
		"artifacts/x/view.html",
		"artifacts/x/view.js.swp",
		"artifacts/x/view.mjs",
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
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assay.yaml"), []byte("id: "+testID+"\n"), 0o644))
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

func TestClassify_ArtifactPageFiles(t *testing.T) {
	for _, rel := range []string{
		"artifacts/p/html/index.html.tmpl",
		"artifacts/p/html/assets/style.css",
		"artifacts/p/html/assets/img/logo.svg",
	} {
		k, id, ok := classify(rel)
		assert.True(t, ok, rel)
		assert.Equal(t, KindArtifact, k)
		assert.Equal(t, "p", id)
	}
	for _, rel := range []string{
		"artifacts/p/html",                      // the folder itself
		"artifacts/p/html/.index.html.tmpl.swp", // an editor's swap file
		"artifacts/p/html/index.html.tmpl~",     // a backup
		"artifacts/p/html/assets/.hidden.json",  // a dotfile
		"artifacts/p/html/.git/config",          // a hidden folder
		"artifacts/p/html/a/b/c/d/e.json",       // deeper than the layout
		"artifacts/.p/html/index.html.tmpl",     // a hidden artifact folder
		"artifacts/p/htm/index.html.tmpl",       // not the page folder
		"skills/p/html/index.html.tmpl",         // not artifacts
		"artifacts/p/html/" + filestore.TempPrefix + "x",
	} {
		_, _, ok := classify(rel)
		assert.False(t, ok, rel)
	}
}

func TestWatcher_ArtifactPageFilesAreReported(t *testing.T) {
	ws, root := newWorkspace(t)
	w := startWatcher(t, ws)

	// A git pull drops the artifact folder with a page inside, nested folders too.
	dir := filepath.Join(root, "artifacts", "page")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "html", "assets", "img"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "artifact.yaml"), []byte(artifactManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "index.html.tmpl"), []byte("<p>x</p>"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "assets", "img", "a.json"), []byte("{}"), 0o644))
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		c := next(t, w)
		assert.Equal(t, KindArtifact, c.Kind)
		assert.Equal(t, "page", c.ID)
		seen[c.Path] = true
	}
	assert.True(t, seen["artifacts/page/html/index.html.tmpl"])
	assert.True(t, seen["artifacts/page/html/assets/img/a.json"])

	// Edits at every depth are watched; swap files and dotfiles are not.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "assets", "img", "a.json"), []byte(`{"a":1}`), 0o644))
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpChanged, ID: "page", Path: "artifacts/page/html/assets/img/a.json"}, next(t, w))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", ".index.html.tmpl.swp"), []byte("x"), 0o644))
	expectQuiet(t, w)

	// A folder made later under html/ is watched too.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "html", "assets", "later"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "html", "assets", "later", "b.json"), []byte("{}"), 0o644))
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpCreated, ID: "page", Path: "artifacts/page/html/assets/later/b.json"}, next(t, w))
}

func TestWatcher_ArtifactPageExistingAtStartIsWatched(t *testing.T) {
	ws, root := newWorkspace(t)
	dir := filepath.Join(root, "artifacts", "pre", "html", "assets")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "artifacts", "pre", "artifact.yaml"), []byte(artifactManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s.css"), []byte("p{}"), 0o644))
	w := startWatcher(t, ws)
	expectQuiet(t, w)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "s.css"), []byte("p{color:red}"), 0o644))
	assert.Equal(t, Change{Kind: KindArtifact, Op: OpChanged, ID: "pre", Path: "artifacts/pre/html/assets/s.css"}, next(t, w))
}

func TestWatcher_OwnPageWritesAreIgnored(t *testing.T) {
	ws, _ := newWorkspace(t)
	ctx := context.Background()
	w := startWatcher(t, ws)
	m, err := artifacts.Decode([]byte(artifactManifest + "view: html\n"))
	require.NoError(t, err)
	a := &artifacts.Artifact{Name: "mine", Manifest: *m, Files: []artifacts.PageFile{
		{Path: "index.html.tmpl", Data: []byte(`<p>{{ query "q" }}</p>`)},
		{Path: "assets/s.css", Data: []byte("p{}")},
	}}
	require.NoError(t, ws.Artifacts().Put(ctx, a))
	a.Files[1].Data = []byte("p{color:red}")
	require.NoError(t, ws.Artifacts().Put(ctx, a))
	require.NoError(t, ws.Artifacts().Delete(ctx, "mine"))
	expectQuiet(t, w)
}

func TestWorkspace_TempDirsCoverArtifactPages(t *testing.T) {
	ws, root := newWorkspace(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "artifacts", "one", "html", "assets", "img"), 0o755))
	w, ok := ws.(*workspace)
	require.True(t, ok)
	dirs := w.tempDirs()
	assert.Contains(t, dirs, "artifacts/one/html")
	assert.Contains(t, dirs, "artifacts/one/html/assets")
	assert.Contains(t, dirs, "artifacts/one/html/assets/img")
}
