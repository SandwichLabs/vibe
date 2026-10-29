package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/semantics/proposals"
)

func fileProposal(t *testing.T, app *App, d proposals.Draft, later ...time.Duration) *proposals.Proposal {
	t.Helper()
	now := time.Now()
	for _, l := range later {
		now = now.Add(l)
	}
	p, err := proposals.Build(d, now, proposals.ByAgent)
	require.NoError(t, err)
	require.NoError(t, app.ws.Proposals().Create(context.Background(), p))
	return p
}

func netRevenueDraft(replaces string) proposals.Draft {
	return proposals.Draft{Kind: semantics.KindMetric, Name: "net_revenue", Rationale: "Refunds are excluded.", ReplacesRev: replaces,
		Definition: map[string]any{"query": "net_revenue_base", "measure": "sum(amount)", "dimensions": []any{"region"}}}
}

func TestProposals_ListGetAcceptRejectBindings(t *testing.T) {
	app := newOpenApp(t)
	ctx := context.Background()
	_, err := app.sess.DB().Exec(`CREATE TABLE sales (region VARCHAR, amount INTEGER); INSERT INTO sales VALUES ('east', 10), ('west', 5)`)
	require.NoError(t, err)
	require.NoError(t, app.ws.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Base", SQL: "SELECT region, amount FROM sales"}))

	p := fileProposal(t, app, netRevenueDraft(""))
	g := fileProposal(t, app, proposals.Draft{Kind: semantics.KindGlossary, Rationale: "r", Definition: map[string]any{"term": "ARR", "text": "x"}}, 2*time.Second)

	list, err := app.ListProposals(false)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, g.ID, list[0].ID, "newest first")

	pv, err := app.GetProposal(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "semantics/metrics/net_revenue.yaml", pv.Target)
	assert.Empty(t, pv.CurrentText)
	assert.Contains(t, pv.ProposedText, "measure: sum(amount)")
	assert.False(t, pv.Stale)

	_, err = app.RunQuery(RunQueryRequest{SQL: "SELECT * FROM metric('net_revenue')"})
	require.Error(t, err, "not a metric until accepted")
	err = app.AcceptProposal(p.ID, "", false)
	require.Error(t, err, "the binding needs the rev of the version on screen")
	assert.Contains(t, err.Error(), "needs the rev of the version you looked at")
	require.NoError(t, app.AcceptProposal(p.ID, pv.ShownRev, false))
	res, err := app.RunQuery(RunQueryRequest{SQL: "SELECT * FROM metric('net_revenue')"})
	require.NoError(t, err)
	assert.EqualValues(t, 15, res.Rows[0][0])

	require.NoError(t, app.RejectProposal(g.ID))
	list, err = app.ListProposals(false)
	require.NoError(t, err)
	assert.Empty(t, list)
	list, err = app.ListProposals(true)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	// ids are validated, never a path
	_, err = app.GetProposal("../../etc/passwd")
	require.Error(t, err)
	require.Error(t, app.AcceptProposal("../x", "x", true))
	require.Error(t, app.RejectProposal("../x"))
	assert.Error(t, app.AcceptProposal(p.ID, "", false), "already decided")
}

func TestProposals_StaleIsAConflictUntilOverwrite(t *testing.T) {
	app := newOpenApp(t)
	ctx := context.Background()
	m, err := semantics.DecodeMetric("net_revenue", []byte("version: 1\nname: net_revenue\nquery: net_revenue_base\nmeasure: sum(a)\n"))
	require.NoError(t, err)
	require.NoError(t, app.ws.Semantics().PutMetric(ctx, m))
	p := fileProposal(t, app, netRevenueDraft(m.Rev))

	m.Measure = "sum(b)"
	require.NoError(t, app.ws.Semantics().PutMetric(ctx, m))

	pv, err := app.GetProposal(p.ID)
	require.NoError(t, err)
	assert.True(t, pv.Stale)
	assert.Contains(t, pv.CurrentText, "sum(b)")

	err = app.AcceptProposal(p.ID, pv.ShownRev, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed since this was proposed")
	cur, _ := app.ws.Semantics().Metric(ctx, "net_revenue")
	assert.Equal(t, "sum(b)", cur.Measure)

	// "accept anyway" needs the rev of what was looked at, and is refused if it moved
	require.Error(t, app.AcceptProposal(p.ID, "", true))
	m.Measure = "sum(c)"
	require.NoError(t, app.ws.Semantics().PutMetric(ctx, m))
	err = app.AcceptProposal(p.ID, pv.ShownRev, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed again since you looked")
	cur, _ = app.ws.Semantics().Metric(ctx, "net_revenue")
	assert.Equal(t, "sum(c)", cur.Measure)
	pv, err = app.GetProposal(p.ID)
	require.NoError(t, err)
	require.NoError(t, app.AcceptProposal(p.ID, pv.ShownRev, true))
	cur, _ = app.ws.Semantics().Metric(ctx, "net_revenue")
	assert.Equal(t, "sum(amount)", cur.Measure)
}

func TestDefinitionBindings_ListAndGet(t *testing.T) {
	app := newOpenApp(t)
	ctx := context.Background()
	require.NoError(t, app.ws.Queries().Create(ctx, &queries.Query{ID: "net_revenue_base", Title: "Base", SQL: "SELECT 'east' AS region, 10 AS amount"}))

	empty, err := app.ListDefinitions()
	require.NoError(t, err)
	assert.Empty(t, empty.Metrics)

	p := fileProposal(t, app, netRevenueDraft(""))
	pv, err := app.GetProposal(p.ID)
	require.NoError(t, err)
	require.NoError(t, app.AcceptProposal(p.ID, pv.ShownRev, false))

	list, err := app.ListDefinitions()
	require.NoError(t, err)
	require.Len(t, list.Metrics, 1)
	assert.Equal(t, "net_revenue", list.Metrics[0].Name)
	assert.Zero(t, list.OpenProposals)

	d, err := app.GetDefinition("metric", "net_revenue")
	require.NoError(t, err)
	require.NotNil(t, d.Metric)
	assert.Equal(t, []string{"region"}, d.Metric.Dimensions)
	assert.Contains(t, d.Text, "measure: sum(amount)")
	assert.False(t, d.QueryMissing)

	_, err = app.GetDefinition("metric", "../x")
	assert.Error(t, err)
}

func TestAgentProposalAnnouncesItself(t *testing.T) {
	app := newOpenApp(t)
	var events []string
	app.emit = func(name string, _ ...any) { events = append(events, name) }
	app.notifyAgentToolChange("propose_definition", map[string]any{"proposed": true})
	assert.Contains(t, events, "semantics:proposed")
}
