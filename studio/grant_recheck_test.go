package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/workspace"
)

const metricPageManifest = "version: 1\ntitle: P\nview: html\nqueries:\n  - name: big\n    sql: SELECT * FROM metric('net_revenue')\n"
const metricViewManifest = "version: 1\ntitle: V\nview: script\nqueries:\n  - name: big\n    sql: SELECT * FROM metric('net_revenue')\n"

func metricApp(t *testing.T) *App {
	t.Helper()
	app := newOpenApp(t)
	ctx := context.Background()
	require.NoError(t, app.ws.Queries().Create(ctx, &queries.Query{ID: "base", Title: "base", SQL: "SELECT 5 AS amount"}))
	setMetric(t, app, "sum(amount)")
	return app
}

func setMetric(t *testing.T, app *App, measure string) {
	t.Helper()
	m, err := semantics.DecodeMetric("net_revenue", []byte("version: 1\nname: net_revenue\nquery: base\nmeasure: "+measure+"\n"))
	require.NoError(t, err)
	require.NoError(t, app.ws.Semantics().PutMetric(context.Background(), m, semantics.WithOverwrite()))
}

func writeArtifactFiles(t *testing.T, app *App, name, manifest string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(manifest), 0o644))
	for p, s := range files {
		full := filepath.Join(dir, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(s), 0o644))
	}
}

// A static page allowed over a metric: the verdict cache (500 ms) is left ON, so
// only the pin and the re-check can stop it.
func TestGrant_PageReadingAMetricIsRefusedAtOnceWhenTheMetricChanges(t *testing.T) {
	app := metricApp(t)
	writeArtifactFiles(t, app, "pg", metricPageManifest, map[string]string{"html/index.html.tmpl": `<p>{{ range (query "big").Rows }}{{ index . 0 }}{{ end }}</p>`})
	st, err := app.ArtifactViewState("pg")
	require.NoError(t, err)
	require.Len(t, st.MetricsUsed, 1)
	_, err = app.AllowArtifactView("pg", st.Hash)
	require.NoError(t, err)
	gr, err := app.OpenArtifactPage("pg", testOrigin)
	require.NoError(t, err)
	g := app.views.lookup(gr.Token)
	require.NotNil(t, g)
	ctx := context.Background()
	rows, err := g.run(ctx, "big", nil)
	require.NoError(t, err)
	assert.EqualValues(t, 5, rows.Rows[0][0])

	// In process, as an accepted proposal or a Query Explorer save does it.
	setMetric(t, app, "sum(amount) * 2")
	require.NotNil(t, app.views.lookup(gr.Token), "the verdict cache still says allowed")
	_, err = g.run(ctx, "big", nil)
	require.Error(t, err, "but the run is pinned to the allowed revisions")
	assert.Contains(t, err.Error(), "a metric this page reads changed")

	var events []string
	app.emit = func(name string, data ...any) {
		if len(data) > 0 {
			if m, ok := data[0].(map[string]any); ok && m["kind"] == "artifactview" {
				events = append(events, name)
			}
		}
	}
	app.recheckGrants()
	assert.Nil(t, app.views.lookup(gr.Token), "the grant is dropped with no cache grace")
	assert.Equal(t, []string{"workspace:changed"}, events, "and the trust screen is told")
	state, err := app.ArtifactViewState("pg")
	require.NoError(t, err)
	assert.Equal(t, "changed", state.State)
	assert.True(t, state.DepsChanged, "only what the page reads changed, not the page")
}

func TestGrant_SavedQueryChangeAndDiskChangeAlsoEndTheGrant(t *testing.T) {
	app := metricApp(t)
	writeArtifactFiles(t, app, "pg", metricPageManifest, map[string]string{"html/index.html.tmpl": `<p>{{ query "big" }}</p>`})
	st, _ := app.ArtifactViewState("pg")
	_, err := app.AllowArtifactView("pg", st.Hash)
	require.NoError(t, err)
	gr, err := app.OpenArtifactPage("pg", testOrigin)
	require.NoError(t, err)
	g := app.views.lookup(gr.Token)
	require.NotNil(t, g)

	// The saved query changes through the Query Explorer's binding.
	q, err := app.ws.Queries().Get(context.Background(), "base")
	require.NoError(t, err)
	_, err = app.SaveQueryOn(SaveQueryRequest{ID: "base", Title: "base", SQL: "SELECT 9 AS amount", Rev: q.Rev})
	require.NoError(t, err)
	assert.Nil(t, app.views.lookup(gr.Token))
	_, err = g.run(context.Background(), "big", nil)
	require.Error(t, err)

	// A change on disk (git pull) is caught by the watcher's event.
	_, err = app.AllowArtifactView("pg", hashOfView(t, app, "pg"))
	require.NoError(t, err)
	gr2, err := app.OpenArtifactPage("pg", testOrigin)
	require.NoError(t, err)
	require.NotNil(t, app.views.lookup(gr2.Token))
	p := filepath.Join(app.ws.Root(), "semantics", "metrics", "net_revenue.yaml")
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, append(data, "# pulled\n"...), 0o644))
	app.onDiskChange(app.ws, workspace.Change{Kind: workspace.KindSemantics, Op: workspace.OpChanged, ID: "metrics/net_revenue", Path: "semantics/metrics/net_revenue.yaml"})
	assert.Nil(t, app.views.lookup(gr2.Token))
}

func hashOfView(t *testing.T, app *App, name string) string {
	t.Helper()
	st, err := app.ArtifactViewState(name)
	require.NoError(t, err)
	return st.Hash
}

func TestGrant_ScriptViewReadingAMetricIsPinnedAndDropped(t *testing.T) {
	app := metricApp(t)
	writeArtifactFiles(t, app, "chart", metricViewManifest, map[string]string{artifacts.ScriptFile: studioScriptSource})
	st, err := app.ArtifactViewState("chart")
	require.NoError(t, err)
	require.NotEmpty(t, st.Pins)
	_, err = app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	gr, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	g := app.views.lookup(gr.Token)
	require.NotNil(t, g)
	res, err := app.runArtifactQuery(semantics.WithPins(g.ctx, g.pins), "chart", "big", nil)
	require.NoError(t, err)
	assert.EqualValues(t, 5, res.Rows[0][0])

	setMetric(t, app, "sum(amount) + 1")
	_, err = app.runArtifactQuery(semantics.WithPins(g.ctx, g.pins), "chart", "big", nil)
	require.Error(t, err, "a run with the old pins cannot read the new metric")
	assert.Contains(t, err.Error(), "changed")
	_, err = app.RunArtifactViewQuery(gr.Token, "big", nil)
	require.Error(t, err)
	app.recheckGrants()
	assert.Nil(t, app.views.lookup(gr.Token))
}

// A view that read no metric when it was allowed runs with an empty pin set, and
// a metric() that shows up in its manifest later expands nothing.
func TestGrant_ViewWithNoMetricsRunsWithEmptyStrictPins(t *testing.T) {
	app := metricApp(t)
	writeArtifactFiles(t, app, "chart", studioScriptManifest, map[string]string{artifacts.ScriptFile: studioScriptSource})
	st, err := app.ArtifactViewState("chart")
	require.NoError(t, err)
	_, err = app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	gr, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	g := app.views.lookup(gr.Token)
	require.NotNil(t, g)
	require.NotNil(t, g.pins, "an empty set, attached")
	assert.Empty(t, g.pins)
	require.NotNil(t, g.manifest, "the manifest that was hashed is held")

	// The file on disk gains a metric() query after the allow. The view's own
	// run uses the held manifest, which does not have it:
	require.NoError(t, os.WriteFile(filepath.Join(app.ws.Root(), artifacts.Dir, "chart", artifacts.ManifestFile), []byte(metricViewManifest), 0o644))
	_, err = app.RunArtifactViewQuery(gr.Token, "big", nil)
	require.Error(t, err, "the changed manifest also ended the allow")

	// And even a manifest that does carry the call cannot expand an unpinned metric.
	m, err := artifacts.Decode([]byte(metricViewManifest))
	require.NoError(t, err)
	_, err = app.runPageQuery(semantics.WithPins(context.Background(), g.pins), "chart", m, "big", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "was not read by this page when you allowed it")
}

func TestPins_AnUnpinnedMetricIsRefusedAndNoPinsMeansNoCheck(t *testing.T) {
	app := metricApp(t)
	m, err := artifacts.Decode([]byte(metricViewManifest))
	require.NoError(t, err)
	_, err = app.runPageQuery(semantics.WithPins(context.Background(), map[string]string{"metric:other": "x"}), "chart", m, "big", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "was not read by this page")
	// Not pinned at all (the person's own surfaces, the agent): unchanged.
	res, err := app.runPageQuery(context.Background(), "chart", m, "big", nil)
	require.NoError(t, err)
	assert.EqualValues(t, 5, res.Rows[0][0])
}

func TestGrant_RecheckIgnoresGrantsOfAnotherWorkspace(t *testing.T) {
	app := metricApp(t)
	writeArtifactFiles(t, app, "chart", studioScriptManifest, map[string]string{artifacts.ScriptFile: studioScriptSource})
	st, _ := app.ArtifactViewState("chart")
	_, err := app.AllowArtifactView("chart", st.Hash)
	require.NoError(t, err)
	gr, err := app.OpenArtifactView("chart", testOrigin)
	require.NoError(t, err)
	res, err := workspace.Init(filepath.Join(t.TempDir(), "other")) // another workspace, which has no such artifact
	require.NoError(t, err)
	defer res.Workspace.Close() //nolint:errcheck
	app.recheckGrantsIn(context.Background(), res.Workspace)
	assert.NotNil(t, app.views.lookup(gr.Token), "a grant of another workspace is not judged by this one")
}
