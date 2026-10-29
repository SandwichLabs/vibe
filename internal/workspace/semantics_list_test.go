package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/queries"
	"github.com/SandwichLabs/vibe/internal/semantics"
	"github.com/SandwichLabs/vibe/internal/semantics/proposals"
)

func TestListDefinitions_GroupsKindsAndCountsProposals(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()

	empty, err := ListDefinitions(ctx, ws)
	require.NoError(t, err)
	assert.Empty(t, empty.Entities)
	assert.Empty(t, empty.Metrics)
	assert.Empty(t, empty.Joins)
	assert.Empty(t, empty.Terms)
	assert.Zero(t, empty.OpenProposals)

	m, err := semantics.DecodeMetric("net_revenue", []byte("version: 1\nname: net_revenue\ntitle: Net revenue\nquery: base\nmeasure: sum(amount)\n"))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "semantics", "entities"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "semantics", "entities", "customer.yaml"), []byte("version: 1\nbroken: [\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "semantics", "glossary.md"), []byte("## Churn\n\nLeft.\n"), 0o644))
	p, err := proposals.Build(proposals.Draft{Kind: semantics.KindMetric, Name: "orders", Rationale: "r",
		Definition: map[string]any{"query": "base", "measure": "count(*)"}}, time.Now(), proposals.ByAgent)
	require.NoError(t, err)
	require.NoError(t, ws.Proposals().Create(ctx, p))

	got, err := ListDefinitions(ctx, ws)
	require.NoError(t, err)
	require.Len(t, got.Metrics, 1)
	assert.Equal(t, "Net revenue", got.Metrics[0].Title)
	assert.NotEmpty(t, got.Metrics[0].Rev)
	require.Len(t, got.Entities, 1, "an unreadable file still lists")
	assert.Equal(t, "customer", got.Entities[0].Name)
	assert.NotEmpty(t, got.Entities[0].Error)
	assert.Equal(t, []string{"Churn"}, got.Terms)
	assert.NotEmpty(t, got.GlossaryRev)
	assert.Equal(t, 1, got.OpenProposals)
}

func TestGetDefinition_TextDocumentAndMissingQuery(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	text := "version: 1\nname: net_revenue\nquery: base\nmeasure: sum(amount)\ndimensions: [region]\n"
	m, err := semantics.DecodeMetric("net_revenue", []byte(text))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))

	d, err := GetDefinition(ctx, ws, semantics.KindMetric, "net_revenue")
	require.NoError(t, err)
	require.NotNil(t, d.Metric)
	assert.Equal(t, []string{"region"}, d.Metric.Dimensions)
	assert.NotEmpty(t, d.Text)
	assert.True(t, d.QueryMissing)

	require.NoError(t, ws.Queries().Create(ctx, &queries.Query{ID: "base", Title: "base", SQL: "select 1 as region, 2 as amount"}))
	d, err = GetDefinition(ctx, ws, semantics.KindMetric, "net_revenue")
	require.NoError(t, err)
	assert.False(t, d.QueryMissing)

	// A broken file opens as text with its error, so it can be read and fixed.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "semantics", "joins"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "semantics", "joins", "j.yaml"), []byte("nope: ["), 0o644))
	bad, err := GetDefinition(ctx, ws, semantics.KindJoin, "j")
	require.NoError(t, err)
	assert.Nil(t, bad.Join)
	assert.NotEmpty(t, bad.Error)
	assert.Equal(t, "nope: [", bad.Text)

	_, err = GetDefinition(ctx, ws, semantics.KindEntity, "nobody")
	assert.ErrorIs(t, err, semantics.ErrNotFound)

	g, err := GetDefinition(ctx, ws, semantics.KindGlossary, "")
	require.NoError(t, err)
	require.NotNil(t, g.Glossary)
	assert.Empty(t, g.Glossary.Terms)
}
