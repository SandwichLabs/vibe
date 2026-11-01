package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/semantics"
	"github.com/SandwichLabs/assay/internal/semantics/proposals"
)

const semanticsMetric = "version: 1\nname: net_revenue\nquery: base\nmeasure: sum(amount)\n"

func TestClassify_Semantics(t *testing.T) {
	for rel, id := range map[string]string{
		"semantics/entities/customer.yaml":      "entities/customer",
		"semantics/metrics/net_revenue.yaml":    "metrics/net_revenue",
		"semantics/joins/orders_customers.yaml": "joins/orders_customers",
		"semantics/glossary.md":                 "glossary",
	} {
		k, got, ok := classify(rel)
		assert.True(t, ok, rel)
		assert.Equal(t, KindSemantics, k, rel)
		assert.Equal(t, id, got, rel)
	}
	for _, rel := range []string{
		"semantics/metrics/.net_revenue.yaml.swp",
		"semantics/metrics/net_revenue.yaml~",
		"semantics/metrics/net_revenue.txt",
		"semantics/other/x.yaml",
		"semantics/x.yaml",
		"semantics/metrics/a/b.yaml",
		"semantics/glossary.md.swp",
		".assay-local/proposals/20261029T153012Z-metric-x-7f3a.txt",
		".assay-local/proposals/.20261029T153012Z-metric-x-7f3a.yaml.swp",
		".assay-local/other/20261029T153012Z-metric-x-7f3a.yaml",
	} {
		_, _, ok := classify(rel)
		assert.False(t, ok, rel)
	}
}

func TestWatcher_SemanticsAreReportedAndOwnWritesAreNot(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	w := startWatcher(t, ws)

	m, err := semantics.DecodeMetric("net_revenue", []byte(semanticsMetric))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))
	m.Title = "edited"
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))
	require.NoError(t, ws.Semantics().DeleteMetric(ctx, "net_revenue"))
	expectQuiet(t, w)

	// A git pull creates the folders and files at once.
	dir := filepath.Join(root, "semantics", "entities")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "customer.yaml"), []byte("x"), 0o644))
	assert.Equal(t, Change{Kind: KindSemantics, Op: OpCreated, ID: "entities/customer", Path: "semantics/entities/customer.yaml"}, next(t, w))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "customer.yaml"), []byte("y"), 0o644))
	assert.Equal(t, Change{Kind: KindSemantics, Op: OpChanged, ID: "entities/customer", Path: "semantics/entities/customer.yaml"}, next(t, w))
	require.NoError(t, os.WriteFile(filepath.Join(root, "semantics", "glossary.md"), []byte("## a\n"), 0o644))
	assert.Equal(t, Change{Kind: KindSemantics, Op: OpCreated, ID: "glossary", Path: "semantics/glossary.md"}, next(t, w))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "semantics", "joins"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "semantics", "joins", "j.yaml"), []byte("x"), 0o644))
	assert.Equal(t, Change{Kind: KindSemantics, Op: OpCreated, ID: "joins/j", Path: "semantics/joins/j.yaml"}, next(t, w))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".customer.yaml.swp"), []byte("x"), 0o644))
	expectQuiet(t, w)
}

func TestWorkspace_SemanticsStoreAndTempSweep(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	m, err := semantics.DecodeMetric("net_revenue", []byte(semanticsMetric))
	require.NoError(t, err)
	require.NoError(t, ws.Semantics().PutMetric(ctx, m))
	got, err := ws.Semantics().Metric(ctx, "net_revenue")
	require.NoError(t, err)
	assert.Equal(t, m.Rev, got.Rev)
	_, err = os.Stat(filepath.Join(root, "semantics", "metrics", "net_revenue.yaml"))
	require.NoError(t, err)

	impl, ok := ws.(*workspace)
	require.True(t, ok)
	dirs := impl.tempDirs()
	assert.Contains(t, dirs, "semantics")
	assert.Contains(t, dirs, "semantics/entities")
	assert.Contains(t, dirs, "semantics/metrics")
	assert.Contains(t, dirs, "semantics/joins")
}

func TestWatcher_ProposalsAreReportedWhenFiledElsewhereNotByOwnStore(t *testing.T) {
	ws, root := newWorkspace(t)
	ctx := context.Background()
	w := startWatcher(t, ws)

	// The workspace's own store: reported by nobody (Studio announces an
	// agent's proposal itself).
	p, err := proposals.Build(proposals.Draft{Kind: semantics.KindMetric, Name: "net_revenue", Rationale: "why",
		Definition: map[string]any{"query": "base", "measure": "sum(amount)"}}, time.Now(), proposals.ByAgent)
	require.NoError(t, err)
	require.NoError(t, ws.Proposals().Create(ctx, p))
	expectQuiet(t, w)

	// Another process files one, in a folder that may not have existed when the
	// watcher started.
	dir := filepath.Join(root, ".assay-local", "proposals")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	name := "20261029T153012Z-metric-x-7f3a.yaml"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
	got := next(t, w)
	assert.Equal(t, Change{Kind: KindProposal, Op: OpCreated, ID: "20261029T153012Z-metric-x-7f3a", Path: ".assay-local/proposals/" + name}, got)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".x.yaml.swp"), []byte("x"), 0o644))
	expectQuiet(t, w)
}
