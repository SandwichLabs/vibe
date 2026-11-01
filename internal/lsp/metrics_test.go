package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/assay/internal/semantics"
)

func metricFixture() *Catalog {
	c := fixture()
	c.Metrics = []MetricDoc{
		{Name: "net_revenue", Title: "Net revenue", Description: "Revenue after refunds.", Dimensions: []string{"region", "month"}, Query: "net_revenue_base"},
		{Name: "orders", Dimensions: []string{"channel"}, Query: "orders_base"},
	}
	return c
}

func TestComplete_MetricNamesAndDimensions(t *testing.T) {
	cat := metricFixture()
	assert.ElementsMatch(t, []string{"net_revenue", "orders"}, labels(at("SELECT * FROM metric('|", cat)))
	assert.Equal(t, []string{"net_revenue"}, labels(at("SELECT * FROM metric('net|", cat)))
	assert.Equal(t, []string{"net_revenue"}, labels(at("SELECT * FROM metric('NET|')", cat)), "inside a closed literal, before the quote")
	for _, it := range at("SELECT * FROM metric('|", cat) {
		assert.Equal(t, KindMetric, it.Kind)
		assert.Empty(t, it.Insert, "a metric name is inserted as it is")
	}

	assert.ElementsMatch(t, []string{"region", "month"}, labels(at("SELECT * FROM metric('net_revenue', by => '|", cat)))
	assert.Equal(t, []string{"month"}, labels(at("SELECT * FROM metric('net_revenue', by => 'mo|", cat)))
	assert.ElementsMatch(t, []string{"region", "month"}, labels(at("SELECT * FROM metric('net_revenue', by => ['region', '|", cat)))
	assert.Equal(t, []string{"channel"}, labels(at("SELECT * FROM metric('orders', by => '|')", cat)))
	assert.Empty(t, at("SELECT * FROM metric('nope', by => '|", cat), "an unknown metric has no dimensions")

	// Not in a metric call: strings stay opaque, and nothing offers metric names in other calls.
	assert.Empty(t, at("SELECT * FROM read_csv('|", cat))
	assert.Empty(t, at("SELECT 'net|", cat))
	assert.Empty(t, at("SELECT * FROM x.metric('|", cat)[:0])

	// The replaced range starts after the opening quote.
	src := "SELECT * FROM metric('ne"
	_, from, _ := completeAt(src, len(src), cat)
	assert.Equal(t, len("SELECT * FROM metric('"), from)
}

func TestComplete_MetricIsOfferedAsAFunction(t *testing.T) {
	cat := metricFixture()
	assert.Contains(t, labelsOf(at("SELECT * FROM |", cat), KindFunction), "metric")
	assert.Contains(t, labelsOf(at("SELECT |", cat), KindFunction), "metric")
	assert.NotContains(t, labelsOf(at("SELECT * FROM |", fixture()), KindFunction), "metric", "no metrics, no suggestion")
	assert.Equal(t, "metric", KindMetric.String())
	assert.Equal(t, 12, lspKind(KindMetric))
}

func TestHover_MetricAndDimension(t *testing.T) {
	cat := metricFixture()
	src := "SELECT * FROM metric('net_revenue', by => 'region')"
	md := Hover(src, strings.Index(src, "net_revenue")+3, cat)
	assert.Contains(t, md, "**metric** `net_revenue` — Net revenue")
	assert.Contains(t, md, "Revenue after refunds.")
	assert.Contains(t, md, "dimensions: region, month")
	assert.Contains(t, md, "saved query: `net_revenue_base`")
	md = Hover(src, strings.Index(src, "'region'")+2, cat)
	assert.Equal(t, "**dimension** `region` of metric `net_revenue`", md)
	assert.Empty(t, Hover("SELECT * FROM metric('nope')", strings.Index("SELECT * FROM metric('nope')", "nope"), cat))
	assert.Empty(t, Hover("SELECT 'net_revenue'", strings.Index("SELECT 'net_revenue'", "net"), cat))
}

type fakeMetrics struct{ ms []semantics.Metric }

func (f *fakeMetrics) Metrics(context.Context) ([]semantics.Metric, error) { return f.ms, nil }

func testMetric(name string) semantics.Metric {
	return semantics.Metric{Name: name, Title: "T " + name, Query: "base", Measure: "sum(secret_amount)", Dimensions: []string{"d1"}}
}

func TestLive_FollowsTheMetricFiles(t *testing.T) {
	sess := hubWith(t)
	src := &fakeMetrics{ms: []semantics.Metric{testMetric("alpha"), {Name: "broken", Meta: semantics.Meta{Error: "bad"}}}}
	root := t.TempDir()
	l := newLive(t, sess, WithLiveSemantics(src), WithLiveSnapshot(root, true))
	settled(t, l)
	cat := l.Catalog()
	require.Len(t, cat.Metrics, 1, "a file that fails validation is left out")
	assert.Equal(t, "alpha", cat.Metrics[0].Name)
	res := complete(l, "SELECT * FROM metric('", "")
	assert.Equal(t, []string{"alpha"}, labels(res.Items))

	src.ms = append(src.ms, testMetric("beta"))
	l.InvalidateSemantics()
	settled(t, l)
	assert.ElementsMatch(t, []string{"alpha", "beta"}, labels(complete(l, "SELECT * FROM metric('", "").Items))

	// The snapshot carries names and dimensions, never SQL.
	b, err := os.ReadFile(SnapshotPath(root))
	require.NoError(t, err)
	assert.Contains(t, string(b), `"alpha"`)
	assert.NotContains(t, string(b), "secret_amount", "the measure is SQL and stays out")
	snap, err := Load(SnapshotPath(root))
	require.NoError(t, err)
	assert.Len(t, snap.Metrics, 2)

	// A name that is also a connection name does not collide with the reload key.
	l.Invalidate("semantics")
	settled(t, l)
}

func TestSnapshot_WritesMetricsAndMergeKeepsThemWhenTheWriterDidNotLook(t *testing.T) {
	sess := hubWith(t)
	root := t.TempDir()
	src := &fakeMetrics{ms: []semantics.Metric{testMetric("alpha")}}
	n, err := Snapshot(context.Background(), sess, root, WithMetrics(src))
	require.NoError(t, err)
	require.Positive(t, n)
	got, err := Load(SnapshotPath(root))
	require.NoError(t, err)
	require.Len(t, got.Metrics, 1)
	assert.Equal(t, MetricDoc{Name: "alpha", Title: "T alpha", Dimensions: []string{"d1"}, Query: "base"}, got.Metrics[0])

	// The TUI's snapshot knows no metrics: it must not forget them.
	_, err = Snapshot(context.Background(), sess, root)
	require.NoError(t, err)
	got, err = Load(SnapshotPath(root))
	require.NoError(t, err)
	assert.Len(t, got.Metrics, 1)

	// A writer that looked and found none clears them.
	_, err = Snapshot(context.Background(), sess, root, WithMetrics(&fakeMetrics{ms: []semantics.Metric{}}))
	require.NoError(t, err)
	got, err = Load(SnapshotPath(root))
	require.NoError(t, err)
	assert.Empty(t, got.Metrics)

	// The server reads them from the file.
	require.NoError(t, os.MkdirAll(filepath.Dir(SnapshotPath(root)), 0o755))
	raw, _ := json.Marshal(&Catalog{Version: catalogVersion, Metrics: []MetricDoc{{Name: "zed", Dimensions: []string{"d"}}}})
	require.NoError(t, os.WriteFile(SnapshotPath(root), raw, 0o600))
	cat := NewSource(SnapshotPath(root)).Catalog()
	assert.Equal(t, []string{"zed"}, labels(func() []Item { i, _, _ := completeAt("SELECT metric('", len("SELECT metric('"), cat); return i }()))
}
