package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/filestore"
)

type fakeVet struct{ err error }

func (f fakeVet) VetArtifact(context.Context, *artifacts.Manifest) error { return f.err }

func TestSaveArtifact_StructuralErrorsRefuseAndNothingIsWritten(t *testing.T) {
	store := &artifacts.Mock{}
	ctx := context.Background()
	for name, c := range map[string][2]string{
		"bad yaml":         {"title: [oops", studioArtifactView},
		"unknown key":      {studioArtifactManifest + "bogus: 1\n", studioArtifactView},
		"view needs query": {studioArtifactManifest, `{"title":"x","components":[{"id":"t","type":"table","query":"nope"}]}`},
		"view not json":    {studioArtifactManifest, `{`},
	} {
		_, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "orders", c[0], c[1], "")
		require.ErrorIs(t, err, artifacts.ErrInvalid, name)
	}
	_, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "../evil", studioArtifactManifest, studioArtifactView, "")
	require.Error(t, err)
	list, _ := store.List(ctx)
	assert.Empty(t, list)
}

func TestSaveArtifact_VetFailureSavesWithWarningsAndTheReason(t *testing.T) {
	store := &artifacts.Mock{}
	ctx := context.Background()
	vetErr := &agents.ArtifactVetError{Problems: []string{`query "big": writes are not allowed`}}
	res, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{err: vetErr}}, "orders", studioArtifactManifest, studioArtifactView, "")
	require.NoError(t, err)
	assert.Equal(t, vetErr.Problems, res.Warnings)
	src, err := store.Source(ctx, "orders")
	require.NoError(t, err)
	assert.Equal(t, res.Rev, src.Rev, "saved despite the warning")

	clean, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "orders", studioArtifactManifest, studioArtifactView, res.Rev)
	require.NoError(t, err)
	assert.NotNil(t, clean.Warnings)
	assert.Empty(t, clean.Warnings)

	other, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{err: assert.AnError}}, "orders", studioArtifactManifest, studioArtifactView, clean.Rev)
	require.NoError(t, err)
	require.Len(t, other.Warnings, 1)
	assert.NotContains(t, other.Warnings[0], assert.AnError.Error(), "an unexpected vet error is not shown")
}

func TestSaveArtifact_StaleRevConflictsWhicheverFileChanged(t *testing.T) {
	root := t.TempDir()
	store := artifacts.NewFileStore(root)
	ctx := context.Background()
	first, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "orders", studioArtifactManifest, studioArtifactView, "")
	require.NoError(t, err)

	for _, file := range []string{artifacts.ManifestFile, artifacts.ViewFile} {
		path := filepath.Join(root, artifacts.Dir, "orders", file)
		was, err := os.ReadFile(path)
		require.NoError(t, err)
		suffix := "\n"
		if file == artifacts.ManifestFile {
			suffix = "# outside\n"
		}
		require.NoError(t, os.WriteFile(path, append(append([]byte{}, was...), suffix...), 0o644))
		_, err = saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "orders", studioArtifactManifest, studioArtifactView, first.Rev)
		require.ErrorIs(t, err, filestore.ErrConflict, file)
		require.NoError(t, os.WriteFile(path, was, 0o644))
	}
	_, err = saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "orders", studioArtifactManifest, studioArtifactView, first.Rev)
	require.NoError(t, err, "put back, the original revision matches again")
}

func TestSaveArtifact_KeepsTheManifestAsTyped(t *testing.T) {
	store := &artifacts.Mock{}
	ctx := context.Background()
	typed := "# my note\n" + studioArtifactManifest
	_, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "orders", typed, studioArtifactView, "")
	require.NoError(t, err)
	src, err := getArtifactSource(ctx, store, "orders")
	require.NoError(t, err)
	assert.Equal(t, typed, src.Manifest)
}

func TestNewArtifact_StarterRunsCleanAndAnExistingNameIsRefused(t *testing.T) {
	store := &artifacts.Mock{}
	ctx := context.Background()
	src, err := newArtifact(ctx, store, "fresh")
	require.NoError(t, err)
	got, err := store.Get(ctx, "fresh")
	require.NoError(t, err, "the starter passes the structural checks")
	assert.Equal(t, "fresh", got.Manifest.Title)
	assert.Equal(t, src.Rev, got.Rev)

	_, err = newArtifact(ctx, store, "fresh")
	require.ErrorContains(t, err, "already exists")
	_, err = newArtifact(ctx, store, "Not A Name")
	require.Error(t, err)
	_, err = newArtifact(ctx, store, strings.Repeat("a", 65))
	require.Error(t, err)
}

func TestDeleteArtifact_NeedsTheRevisionSeenAndRefusesAnOutsideEdit(t *testing.T) {
	store := &artifacts.Mock{}
	ctx := context.Background()
	res, err := saveArtifact(ctx, artifactEnv{store: store, vet: fakeVet{}}, "orders", studioArtifactManifest, studioArtifactView, "")
	require.NoError(t, err)

	require.Error(t, deleteArtifact(ctx, store, "orders", ""), "a delete that does not say what it saw is refused")
	require.ErrorIs(t, deleteArtifact(ctx, store, "orders", "stale"), filestore.ErrConflict)
	_, err = store.Get(ctx, "orders")
	require.NoError(t, err)
	require.NoError(t, deleteArtifact(ctx, store, "orders", res.Rev))
	_, err = store.Get(ctx, "orders")
	require.ErrorIs(t, err, artifacts.ErrNotFound)
	require.Error(t, deleteArtifact(ctx, store, "../x", "r"))
}

func TestArtifactBindings_SaveVetsThroughThePolicyAndDeleteRemovesTheFolder(t *testing.T) {
	app := appWithArtifact(t)
	src, err := app.GetArtifactSource("orders")
	require.NoError(t, err)

	write := strings.Replace(studioArtifactManifest, "SELECT id, qty FROM t WHERE qty >= $min_qty ORDER BY id", "DELETE FROM t WHERE qty >= $min_qty", 1)
	res, err := app.SaveArtifact("orders", write, studioArtifactView, src.Rev)
	require.NoError(t, err, "a policy objection does not refuse the save")
	require.Len(t, res.Warnings, 1)
	assert.Contains(t, res.Warnings[0], `query "big"`)

	warnings, err := app.CheckArtifact("orders")
	require.NoError(t, err)
	assert.Equal(t, res.Warnings, warnings)

	_, err = app.SaveArtifact("orders", write, studioArtifactView, src.Rev)
	require.ErrorContains(t, err, "conflict", "the old revision is stale now")

	require.Error(t, app.DeleteArtifact("orders", src.Rev))
	require.NoError(t, app.DeleteArtifact("orders", res.Rev))
	_, err = os.Stat(filepath.Join(app.ws.Root(), artifacts.Dir, "orders"))
	assert.True(t, os.IsNotExist(err), "both files and the folder are gone")
}

func TestArtifactBindings_SourceOfABrokenArtifactCanBeFixed(t *testing.T) {
	app := newOpenApp(t)
	writeStudioArtifact(t, app, "broken", "title: [oops", studioArtifactView)
	src, err := app.GetArtifactSource("broken")
	require.NoError(t, err)
	assert.Equal(t, "title: [oops", src.Manifest)
	res, err := app.SaveArtifact("broken", studioArtifactManifest, studioArtifactView, src.Rev)
	require.NoError(t, err)
	assert.NotEmpty(t, res.Rev)
	d, err := app.GetArtifact("broken")
	require.NoError(t, err)
	assert.Empty(t, d.Error)
}

func TestVetWarnings_ScrubTheWorkspacePath(t *testing.T) {
	root := "/home/someone/ws"
	env := artifactEnv{root: root, vet: fakeVet{err: &agents.ArtifactVetError{Problems: []string{`query "q": "/etc/x" is outside the workspace's data/ folder (` + root + `/data)`}}}}
	got := vetWarnings(context.Background(), env, &artifacts.Manifest{})
	require.Len(t, got, 1)
	assert.NotContains(t, got[0], root)
	assert.Contains(t, got[0], "<workspace>/data")
}

func TestWorkspaceIdentityIsCheckedByVetAndRun(t *testing.T) {
	app := appWithArtifact(t)
	h, _, err := app.readOnlyRunner()
	require.NoError(t, err)
	m, err := artifacts.Decode([]byte(studioArtifactManifest))
	require.NoError(t, err)

	require.NoError(t, h.VetArtifact(context.Background(), m), "same workspace: vets")
	_, _, err = h.RunArtifactQuery(context.Background(), "orders", "big", nil)
	require.NoError(t, err)

	ws := app.ws
	app.ws = nil // the workspace was closed or replaced
	defer func() { app.ws = ws }()
	require.ErrorContains(t, h.VetArtifact(context.Background(), m), "closed or replaced")
	_, _, err = h.RunArtifactQuery(context.Background(), "orders", "big", nil)
	require.ErrorContains(t, err, "closed or replaced")
	w := vetWarnings(context.Background(), artifactEnv{vet: h}, m)
	require.Len(t, w, 1)
	assert.Contains(t, w[0], "could not be checked")
}
