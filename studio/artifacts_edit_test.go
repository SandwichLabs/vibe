package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/agents"
	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/filestore"
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
	src, err := newArtifact(ctx, store, "fresh", "a2ui")
	require.NoError(t, err)
	got, err := store.Get(ctx, "fresh")
	require.NoError(t, err, "the starter passes the structural checks")
	assert.Equal(t, "fresh", got.Manifest.Title)
	assert.Equal(t, src.Rev, got.Rev)
	assert.Equal(t, artifacts.ViewFile, src.ViewFile)

	_, err = newArtifact(ctx, store, "fresh", "a2ui")
	require.ErrorContains(t, err, "already exists")
	_, err = newArtifact(ctx, store, "Not A Name", "a2ui")
	require.Error(t, err)
	_, err = newArtifact(ctx, store, strings.Repeat("a", 65), "a2ui")
	require.Error(t, err)

	_, err = newArtifact(ctx, store, "null", "a2ui")
	require.NoError(t, err, "a name that YAML reads as null still makes a titled artifact")

	empty, err := newArtifact(ctx, store, "plain", "")
	require.NoError(t, err, "an empty kind is the live view starter")
	assert.Equal(t, starterView, empty.View)
}

// starterStores runs a check over the Mock and a FileStore in a temp dir.
func starterStores(t *testing.T) map[string]artifacts.Store {
	t.Helper()
	return map[string]artifacts.Store{"mock": &artifacts.Mock{}, "file": artifacts.NewFileStore(t.TempDir())}
}

func TestNewArtifact_EveryStarterPassesTheStoresVetOnBothStores(t *testing.T) {
	ctx := context.Background()
	for storeName, store := range starterStores(t) {
		for _, kind := range starterKinds {
			t.Run(storeName+"/"+kind, func(t *testing.T) {
				src, err := newArtifact(ctx, store, "s-"+kind, kind)
				require.NoError(t, err, "Put runs Decode, ValidateView and VetPage")
				a, err := store.Get(ctx, "s-"+kind)
				require.NoError(t, err)
				assert.Equal(t, src.Rev, a.Rev)
				assert.Empty(t, a.Error)
				require.NoError(t, artifacts.Validate(&a.Manifest))
				switch kind {
				case starterHTML, starterHTMLScript:
					require.NotNil(t, a.Page)
					require.NoError(t, artifacts.ValidateView(&a.Manifest, nil))
					_, err := artifacts.VetPage(&a.Manifest, a.Files)
					require.NoError(t, err, "the real page vet, not a copy")
					want := artifacts.PageStatic
					if kind == starterHTMLScript {
						want = artifacts.PageScripted
					}
					assert.Equal(t, want, a.Page.Kind)
					assert.Equal(t, []string{"span"}, a.Page.Queries)
					assert.Empty(t, a.Page.Capabilities, "no capabilities declared")
				case starterScript:
					assert.Equal(t, artifacts.ViewScript, a.Manifest.View)
					assert.Equal(t, starterScriptJS, string(a.View))
					_, err := artifacts.ViewHash([]byte(src.Manifest), []artifacts.ViewPart{{Path: artifacts.ScriptFile, Data: a.View}})
					require.NoError(t, err)
				default:
					require.NoError(t, artifacts.ValidateView(&a.Manifest, a.View))
				}
			})
		}
	}
}

func TestNewArtifact_StarterSourcesNameNoNetworkAddress(t *testing.T) {
	for _, kind := range starterKinds {
		st, err := starterFor("x", kind)
		require.NoError(t, err)
		all := st.manifest + st.view
		for _, f := range st.files {
			all += string(f.Data)
		}
		// The SVG namespace names a vocabulary; nothing fetches it.
		all = strings.ReplaceAll(all, "http://www.w3.org/2000/svg", "")
		for _, bad := range []string{"http://", "https://", "//cdn", "@import", "#fff", "#000"} {
			assert.NotContains(t, all, bad, kind)
		}
	}
}

func TestNewArtifact_UnknownKindAndExistingNameWriteNothing(t *testing.T) {
	ctx := context.Background()
	for storeName, store := range starterStores(t) {
		for _, kind := range []string{"pdf", "HTML", "../x", "a2ui/../..", " script"} {
			_, err := newArtifact(ctx, store, "nope", kind)
			require.Error(t, err, storeName+" "+kind)
			assert.ErrorContains(t, err, "a2ui, html, html-script, script")
		}
		list, err := store.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, list, storeName)

		first, err := newArtifact(ctx, store, "taken", "html")
		require.NoError(t, err)
		_, err = newArtifact(ctx, store, "taken", "script")
		require.ErrorContains(t, err, "already exists")
		got, err := store.Get(ctx, "taken")
		require.NoError(t, err)
		assert.Equal(t, first.Rev, got.Rev, storeName+": the first artifact is untouched")
		assert.Equal(t, artifacts.ViewHTML, got.Manifest.View)
	}
}

func TestNewArtifactBinding_StartersVetCleanAndScriptedOnesStartUndecided(t *testing.T) {
	app := newOpenApp(t)
	for _, kind := range starterKinds {
		name := "b-" + kind
		src, err := app.NewArtifact(name, kind)
		require.NoError(t, err)
		assert.Equal(t, name, src.Name)
		warnings, err := app.CheckArtifact(name)
		require.NoError(t, err)
		assert.Empty(t, warnings, kind+": the agent's read policy has no complaint")

		st, err := app.ArtifactViewState(name)
		require.NoError(t, err)
		switch kind {
		case starterHTMLScript, starterScript:
			assert.Equal(t, "undecided", st.State, kind+": opens as source with Allow, never auto-allowed")
		case starterHTML:
			assert.Equal(t, "undecided", st.State, kind)
			assert.Equal(t, artifacts.PageStatic, st.PageKind)
		default:
			assert.Equal(t, "none", st.State)
		}
	}
	html, err := app.GetArtifact("b-html-script")
	require.NoError(t, err)
	assert.Equal(t, artifacts.PageScripted, html.PageKind)
	list, err := app.ListArtifacts()
	require.NoError(t, err)
	labels := map[string]string{}
	for _, it := range list {
		labels[it.Name] = it.View
	}
	assert.Equal(t, map[string]string{"b-a2ui": "a2ui", "b-html": "html (static)", "b-html-script": "html (scripted)", "b-script": "script"}, labels)
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
