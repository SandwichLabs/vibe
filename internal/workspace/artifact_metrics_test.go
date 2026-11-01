package workspace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/slaw/internal/artifacts"
	"github.com/SandwichLabs/slaw/internal/queries"
	"github.com/SandwichLabs/slaw/internal/semantics"
)

const (
	metricScriptManifest = "version: 1\ntitle: T\nview: script\nqueries:\n  - name: q\n    sql: SELECT * FROM metric('net_revenue', by => 'region')\n"
	metricPageManifest   = "version: 1\ntitle: P\nview: html\nqueries:\n  - name: q\n    sql: SELECT * FROM metric('net_revenue')\n  - name: other\n    sql: SELECT * FROM metric('orders')\n"
)

func putMetric(t *testing.T, ws Workspace, name, query, measure string) {
	t.Helper()
	m, err := semantics.DecodeMetric(name, []byte("version: 1\nname: "+name+"\nquery: "+query+"\nmeasure: "+measure+"\ndimensions: [region]\n"))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(context.Background(), m, semantics.WithOverwrite()))
}

func putSaved(t *testing.T, ws Workspace, id, sql string) {
	t.Helper()
	ctx := context.Background()
	q := &queries.Query{ID: id, Title: id, SQL: sql}
	if old, err := ws.Queries().Get(ctx, id); err == nil {
		q.Rev = old.Rev
		require.NoError(t, ws.Queries().Update(ctx, q))
		return
	}
	require.NoError(t, ws.Queries().Create(ctx, q))
}

func TestArtifactView_ScriptViewReadingAMetricAsksAgainWhenTheMetricOrItsQueryChanges(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	putSaved(t, ws, "base", "SELECT 'east' AS region, 1 AS amount")
	putMetric(t, ws, "net_revenue", "base", "sum(amount)")
	scriptArtifact(t, ws, "chart", metricScriptManifest, viewJS)

	st := viewState(t, ws, "chart")
	assert.Equal(t, []ArtifactMetricUse{{Name: "net_revenue", Query: "base", Measure: "sum(amount)", Dimensions: []string{"region"}}}, st.MetricsUsed)
	assert.NotEqual(t, st.ContentHash, st.Hash, "the metric files are folded into the hash")
	require.NoError(t, ws.AllowArtifactView("chart", st.Hash))
	require.Equal(t, "allowed", viewState(t, ws, "chart").State)

	// Rewriting a file with the same bytes changes nothing.
	putMetric(t, ws, "net_revenue", "base", "sum(amount)")
	assert.Equal(t, "allowed", viewState(t, ws, "chart").State)

	putMetric(t, ws, "net_revenue", "base", "sum(amount) + 1")
	after := viewState(t, ws, "chart")
	assert.Equal(t, "changed", after.State, "a changed metric file asks again")
	assert.True(t, after.DepsChanged, "and says it was the metric, not the view")
	assert.NotEmpty(t, after.Pins)
	assert.Equal(t, st.ContentHash, after.ContentHash, "the page's own files did not change")

	require.NoError(t, ws.AllowArtifactView("chart", after.Hash))
	require.Equal(t, "allowed", viewState(t, ws, "chart").State)
	putSaved(t, ws, "base", "SELECT 'east' AS region, 2 AS amount")
	assert.Equal(t, "changed", viewState(t, ws, "chart").State, "a changed saved query asks again")
}

func TestArtifactView_ViewsThatCallNoMetricKeepTheirHash(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	putSaved(t, ws, "base", "SELECT 'east' AS region, 1 AS amount")
	putMetric(t, ws, "net_revenue", "base", "sum(amount)")
	scriptArtifact(t, ws, "plain", viewScriptManifest, viewJS)

	st := viewState(t, ws, "plain")
	assert.Empty(t, st.MetricsUsed)
	assert.Equal(t, st.ContentHash, st.Hash, "no metric, no change to the hash an existing allow was made on")
	want, err := artifacts.ViewHash([]byte(viewScriptManifest), []artifacts.ViewPart{{Path: artifacts.ScriptFile, Data: []byte(viewJS)}})
	require.NoError(t, err)
	assert.Equal(t, want, st.Hash)
	require.NoError(t, ws.AllowArtifactView("plain", st.Hash))

	putMetric(t, ws, "net_revenue", "base", "sum(amount) + 1")
	putSaved(t, ws, "base", "SELECT 'west' AS region, 5 AS amount")
	assert.Equal(t, "allowed", viewState(t, ws, "plain").State)
}

func TestArtifactView_AMissingMetricIsListedAndTheDayItAppearsAsksAgain(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, ws, "chart", metricScriptManifest, viewJS)
	st := viewState(t, ws, "chart")
	assert.Equal(t, []ArtifactMetricUse{{Name: "net_revenue", Missing: true}}, st.MetricsUsed)
	require.NoError(t, ws.AllowArtifactView("chart", st.Hash))

	putSaved(t, ws, "base", "SELECT 'east' AS region, 1 AS amount")
	putMetric(t, ws, "net_revenue", "base", "sum(amount)")
	assert.Equal(t, "changed", viewState(t, ws, "chart").State)
}

func TestArtifactView_PageHashFoldsOnlyTheMetricsAStaticPageUses(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	putSaved(t, ws, "base", "SELECT 'east' AS region, 1 AS amount")
	putMetric(t, ws, "net_revenue", "base", "sum(amount)")
	putMetric(t, ws, "orders", "base", "count(*)")
	// A static page's templates use query q only: orders (query "other") is
	// reachable by no template, so it is not part of what the page reads.
	pageArtifactManifest(t, ws, "pg", metricPageManifest, artifacts.PageFile{Path: "index.html.tmpl", Data: []byte(`<p>{{ query "q" }}</p>`)})

	st := viewState(t, ws, "pg")
	require.Equal(t, "html", st.Kind)
	assert.Equal(t, []ArtifactMetricUse{{Name: "net_revenue", Query: "base", Measure: "sum(amount)", Dimensions: []string{"region"}}}, st.MetricsUsed)
	require.NoError(t, ws.AllowArtifactView("pg", st.Hash))

	putMetric(t, ws, "orders", "base", "count(amount)")
	assert.Equal(t, "allowed", viewState(t, ws, "pg").State, "a metric no template reads does not ask")
	putMetric(t, ws, "net_revenue", "base", "avg(amount)")
	assert.Equal(t, "changed", viewState(t, ws, "pg").State)

	d := ws.ArtifactViewDecisions()
	require.Len(t, d, 1)
	assert.Equal(t, []string{"net_revenue"}, []string{d[0].MetricsUsed[0].Name})
}

func TestArtifactView_AScriptedPageReadsEveryMetricOfEveryQuery(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	putSaved(t, ws, "base", "SELECT 'east' AS region, 1 AS amount")
	putMetric(t, ws, "net_revenue", "base", "sum(amount)")
	putMetric(t, ws, "orders", "base", "count(*)")
	pageArtifactManifest(t, ws, "pg", metricPageManifest,
		artifacts.PageFile{Path: "index.html.tmpl", Data: []byte(`<p>{{ query "q" }}</p><script src="app.js"></script>`)},
		artifacts.PageFile{Path: "app.js", Data: []byte(`slaw.run("other")`)})
	st := viewState(t, ws, "pg")
	require.Equal(t, "scripted", st.PageKind)
	require.Len(t, st.MetricsUsed, 2)
	assert.Equal(t, "count(*)", st.MetricsUsed[1].Measure)
}

// pageArtifactManifest writes a view: html artifact with its own manifest.
func pageArtifactManifest(t *testing.T, ws Workspace, name, manifest string, files ...artifacts.PageFile) {
	t.Helper()
	a := &artifacts.Artifact{Name: name, ManifestText: []byte(manifest), Files: files}
	require.NoError(t, ws.Artifacts().Put(context.Background(), a, artifacts.Overwrite()))
}

func TestArtifactView_AnEditToTheViewItselfIsNotDepsChanged(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	putSaved(t, ws, "base", "SELECT 'east' AS region, 1 AS amount")
	putMetric(t, ws, "net_revenue", "base", "sum(amount)")
	scriptArtifact(t, ws, "chart", metricScriptManifest, viewJS)
	require.NoError(t, ws.AllowArtifactView("chart", viewState(t, ws, "chart").Hash))
	scriptArtifact(t, ws, "chart", metricScriptManifest, viewJS+"// edit\n")
	st := viewState(t, ws, "chart")
	assert.Equal(t, "changed", st.State)
	assert.False(t, st.DepsChanged)
}

func TestArtifactView_AllowsMadeBeforeNotesExistStillWork(t *testing.T) {
	ws, _, _ := memWorkspace(t, nil)
	scriptArtifact(t, ws, "plain", viewScriptManifest, viewJS)
	st := viewState(t, ws, "plain")
	require.NoError(t, ws.AllowArtifactView("plain", st.Hash))
	assert.Empty(t, impl(t, ws).vdec.Note(impl(t, ws).viewKey("plain")), "no metric, no note: the decision is exactly what it was")
	assert.Equal(t, "allowed", viewState(t, ws, "plain").State)
}
