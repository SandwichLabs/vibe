package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SandwichLabs/vibe/internal/agents"
	"github.com/SandwichLabs/vibe/internal/artifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const studioArtifactManifest = `version: 1
title: Orders
parameters:
  - name: min_qty
    type: integer
    min: 0
    default: "0"
  - name: since
    type: date
    default: today-30d
queries:
  - name: big
    sql: SELECT id, qty FROM t WHERE qty >= $min_qty ORDER BY id
  - name: fat
    sql: SELECT repeat('x', 1000000) AS s FROM range(100)
`

const studioArtifactView = `{"title":"Orders","components":[
 {"id":"n","type":"numberInput","param":"min_qty"},
 {"id":"t","type":"table","query":"big"}]}`

func writeStudioArtifact(t *testing.T, app *App, name, manifest, view string) {
	t.Helper()
	dir := filepath.Join(app.ws.Root(), artifacts.Dir, name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ManifestFile), []byte(manifest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.ViewFile), []byte(view), 0o644))
}

func appWithArtifact(t *testing.T) *App {
	t.Helper()
	app := newOpenApp(t)
	_, err := app.sess.DB().Exec("CREATE TABLE t (id INTEGER, qty INTEGER)")
	require.NoError(t, err)
	_, err = app.sess.DB().Exec("INSERT INTO t VALUES (1, 5), (2, 50), (3, 500)")
	require.NoError(t, err)
	writeStudioArtifact(t, app, "orders", studioArtifactManifest, studioArtifactView)
	return app
}

func TestArtifacts_NeedAWorkspace(t *testing.T) {
	app := NewApp(testLogger())
	app.ctx = context.Background()
	_, err := app.ListArtifacts()
	require.Error(t, err)
	_, err = app.GetArtifact("orders")
	require.Error(t, err)
	_, err = app.RunArtifactQuery("orders", "big", nil)
	require.Error(t, err)
}

func TestListArtifacts_ListsGoodAndBroken(t *testing.T) {
	app := appWithArtifact(t)
	writeStudioArtifact(t, app, "broken", "version: 1\nunknown_key: 1\n", studioArtifactView)
	list, err := app.ListArtifacts()
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "broken", list[0].Name)
	assert.NotEmpty(t, list[0].Error)
	assert.Equal(t, "orders", list[1].Name)
	assert.Equal(t, "Orders", list[1].Title)
	assert.Equal(t, "a2ui", list[1].View)
	assert.Empty(t, list[1].Error)
}

func TestGetArtifact_RendersTheValidatedPageWithDefaults(t *testing.T) {
	app := appWithArtifact(t)
	d, err := app.GetArtifact("orders")
	require.NoError(t, err)
	assert.Empty(t, d.Error)
	assert.JSONEq(t, `{"title":"Orders","components":[
 {"id":"n","type":"numberInput","param":"min_qty"},
 {"id":"t","type":"table","query":"big"}]}`, string(d.View))
	assert.EqualValues(t, 0, d.Defaults["min_qty"])
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, d.Defaults["since"])
	require.Len(t, d.Queries, 2)
	assert.Equal(t, []string{"min_qty"}, d.Queries[0].Uses)
	assert.NotEmpty(t, d.Rev)
}

func TestGetArtifact_BrokenOpensToItsError(t *testing.T) {
	app := appWithArtifact(t)
	writeStudioArtifact(t, app, "bad-view", studioArtifactManifest,
		`{"title":"T","components":[{"id":"a","type":"table","sql":"SELECT 1"}]}`)
	d, err := app.GetArtifact("bad-view")
	require.NoError(t, err)
	assert.Contains(t, d.Error, "sql")
	assert.Empty(t, d.View)

	_, err = app.GetArtifact("missing")
	require.Error(t, err)
	_, err = app.GetArtifact("../orders")
	require.Error(t, err)
}

func TestRunArtifactQuery_BindsValuesAndRefusesBadOnes(t *testing.T) {
	app := appWithArtifact(t)
	res, err := app.RunArtifactQuery("orders", "big", nil)
	require.NoError(t, err)
	assert.Len(t, res.Rows, 3)

	res, err = app.RunArtifactQuery("orders", "big", map[string]any{"min_qty": float64(50)})
	require.NoError(t, err)
	assert.Len(t, res.Rows, 2)

	for _, values := range []map[string]any{
		{"min_qty": "0; DROP TABLE t"},
		{"min_qty": float64(-1)},
		{"nope": float64(1)},
	} {
		_, err = app.RunArtifactQuery("orders", "big", values)
		require.Error(t, err, "%v", values)
	}
	_, err = app.RunArtifactQuery("orders", "missing", nil)
	require.Error(t, err)
	_, err = app.RunArtifactQuery("gone", "big", nil)
	require.Error(t, err)
	var n int
	require.NoError(t, app.sess.DB().QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 3, n)
}

// A file edited by hand to a write never runs, in any sandbox tier.
func TestRunArtifactQuery_HandEditedWriteIsRefused(t *testing.T) {
	app := appWithArtifact(t)
	writeStudioArtifact(t, app, "evil", `version: 1
title: Evil
queries:
  - name: wipe
    sql: DELETE FROM t
`, `{"title":"Evil","components":[{"id":"t","type":"table","query":"wipe"}]}`)
	for _, mode := range []string{"read-only", "read-write", "full"} {
		setAgentSandbox(t, app, mode)
		_, err := app.RunArtifactQuery("evil", "wipe", nil)
		require.Error(t, err, mode)
	}
	var n int
	require.NoError(t, app.sess.DB().QueryRow("SELECT count(*) FROM t").Scan(&n))
	assert.Equal(t, 3, n)
}

// A handler built for one workspace refuses to run after the workspace was
// replaced, so a name from the old workspace never runs the new one's artifact.
func TestRunArtifactQuery_RefusedAfterAWorkspaceSwitch(t *testing.T) {
	app := appWithArtifact(t)
	h := app.newAgentToolHandler(agents.ModeFull)
	_, _, err := h.RunArtifactQuery(context.Background(), "orders", "big", nil)
	require.NoError(t, err)

	res, err := app.OpenWorkspace(newWorkspaceDir(t, "Other"))
	require.NoError(t, err)
	require.Equal(t, "opened", res.Status)
	_, _, err = h.RunArtifactQuery(context.Background(), "orders", "big", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replaced")

	_, err = app.RunArtifactQuery("orders", "big", nil)
	require.Error(t, err)
}

// A vetted artifact read never tells the Connections rail to reload.
func TestRunArtifactQuery_DoesNotEmitCatalogChanged(t *testing.T) {
	app := appWithArtifact(t)
	var ev eventLog
	app.emit = ev.sink
	_, err := app.RunArtifactQuery("orders", "big", nil)
	require.NoError(t, err)
	_, err = app.RunArtifactQuery("orders", "big", map[string]any{"min_qty": float64(50)})
	require.NoError(t, err)
	assert.Equal(t, 0, ev.count("catalog:changed"))
}

func TestGetDashboard_CountsArtifactsByKindAndABrokenOneOnce(t *testing.T) {
	app := appWithArtifact(t) // one live view
	for name, kind := range map[string]string{"p1": "html", "p2": "html-script", "s1": "script"} {
		_, err := app.NewArtifact(name, kind)
		require.NoError(t, err)
	}
	writeStudioArtifact(t, app, "broken", "version: 1\nunknown_key: 1\n", studioArtifactView)

	d, err := app.GetDashboard()
	require.NoError(t, err)
	assert.Equal(t, ArtifactCounts{Live: 1, Page: 2, Script: 1, Broken: 1}, d.ArtifactCounts)
}

func TestCountArtifacts_AFailingStoreCountsNone(t *testing.T) {
	assert.Equal(t, ArtifactCounts{}, countArtifacts(context.Background(), &artifacts.Mock{Err: assert.AnError}))
}
