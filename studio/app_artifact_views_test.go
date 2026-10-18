package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
)

const studioScriptManifest = "version: 1\ntitle: Chart\nview: script\nqueries:\n  - name: big\n    sql: SELECT 1 AS x\n"
const studioScriptSource = "function draw(ctx) { return '<svg xmlns=\"http://www.w3.org/2000/svg\"/>'; }\n"

func writeStudioScript(t *testing.T, app *App, name, js string) {
	t.Helper()
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(studioScriptManifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ScriptFile), []byte(js), 0o644))
}

func TestArtifactViews_NeedAWorkspace(t *testing.T) {
	app := NewApp(testLogger())
	_, err := app.ArtifactViewState("chart")
	require.Error(t, err)
	_, err = app.AllowArtifactView("chart", "h")
	require.Error(t, err)
	_, err = app.ListArtifactViewDecisions()
	require.Error(t, err)
}

func TestArtifactViews_ScriptShowsAsSourceThenAllowDenyRevoke(t *testing.T) {
	app := newOpenApp(t)
	writeStudioScript(t, app, "chart", studioScriptSource)

	d, err := app.GetArtifact("chart")
	require.NoError(t, err)
	assert.Empty(t, d.Error)
	assert.Equal(t, "script", d.ViewKind)
	assert.Empty(t, d.View, "a script view has no A2UI page")
	list, err := app.ListArtifacts()
	require.NoError(t, err)
	assert.Equal(t, "script", list[0].View)
	src, err := app.GetArtifactSource("chart")
	require.NoError(t, err)
	assert.Equal(t, "view.js", src.ViewFile)
	assert.Equal(t, studioScriptSource, src.View)

	st, err := app.ArtifactViewState("chart")
	require.NoError(t, err)
	assert.Equal(t, "undecided", st.State)
	assert.Equal(t, studioScriptSource, st.Source)

	// A stale hash is refused (the files changed after they were shown).
	writeStudioScript(t, app, "chart", studioScriptSource+"// edit\n")
	_, err = app.AllowArtifactView("chart", st.Hash)
	require.Error(t, err)

	st, err = app.ArtifactViewState("chart")
	require.NoError(t, err)
	got, err := app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	assert.Equal(t, "allowed", got.State)

	writeStudioScript(t, app, "chart", studioScriptSource+"// another\n")
	st, _ = app.ArtifactViewState("chart")
	assert.Equal(t, "changed", st.State)

	decisions, err := app.ListArtifactViewDecisions()
	require.NoError(t, err)
	require.Len(t, decisions, 1)
	assert.Equal(t, "changed", decisions[0].State)

	got, err = app.DenyArtifactView("chart")
	require.NoError(t, err)
	assert.Equal(t, "denied", got.State)
	got, err = app.RevokeArtifactView("chart")
	require.NoError(t, err)
	assert.Equal(t, "undecided", got.State)
}

func TestArtifactViews_SourceTabCanSwitchTheViewKind(t *testing.T) {
	app := appWithArtifact(t)
	src, err := app.GetArtifactSource("orders")
	require.NoError(t, err)
	assert.Equal(t, "view.a2ui.json", src.ViewFile)
	manifest := src.Manifest + "view: script\n"
	res, err := app.SaveArtifact("orders", manifest, studioScriptSource, src.Rev)
	require.NoError(t, err, "%+v", res)
	again, err := app.GetArtifactSource("orders")
	require.NoError(t, err)
	assert.Equal(t, "view.js", again.ViewFile)
	assert.NoFileExists(t, filepath.Join(app.ws.Root(), "artifacts", "orders", "view.a2ui.json"))
}
