package main

// TDD RED — Phase 3 / unit P3.4: "Multi-series / stacked / styled charts"
// (parity plan §3, Evidence WU-9), studio render-path slice.
//
// REAL integration test: opens a real .vibe container (SQLCipher + DuckDB via
// newOpenApp), seeds a `chartdata` table with (month, region, revenue, cost),
// inserts pages exercising the enhanced {chart} directive into _vibe_pages, and
// drives the production Wails-bound RenderPageContent end-to-end. The emitted
// Chart.js config JSON is parsed out of the HTML <script> and asserted against
// the WU-9 acceptance behavior.
//
// These assertions intentionally target behavior that renderDirective + chart.go
// do NOT yet implement (RED):
//   - series= splits one Y column into N datasets by distinct categorical value
//   - y:[a,b] produces one dataset per listed Y column
//   - stacked:true sets Chart.js stacked scale options; "100%" normalizes
//   - x_title / y_title axis titles, per-series colors, legend, etc.
// The single-series legacy chart assertion guards backward compatibility.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedChartData creates and populates a month/region/revenue/cost table on the
// real DuckDB engine. Two regions across three months -> a 2x3 multi-series grid.
func seedChartData(t *testing.T, app *App) {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)

	_, err := ddb.Exec(`CREATE TABLE chartdata (
		month   VARCHAR,
		region  VARCHAR,
		revenue DECIMAL(14,2),
		cost    DECIMAL(14,2)
	);`)
	require.NoError(t, err)

	_, err = ddb.Exec(`INSERT INTO chartdata (month, region, revenue, cost) VALUES
		('Jan', 'East', 1000000.00, 600000.00),
		('Jan', 'West',  800000.00, 500000.00),
		('Feb', 'East', 1200000.00, 700000.00),
		('Feb', 'West',  900000.00, 550000.00),
		('Mar', 'East', 1500000.00, 800000.00),
		('Mar', 'West', 1100000.00, 650000.00);`)
	require.NoError(t, err)
}

// chartScriptRe extracts the Chart.js config object literal passed as the second
// argument to `new Chart(el, { ... })`. It captures from the opening brace of the
// config to the matching close before `);`.
var chartScriptRe = regexp.MustCompile(`(?s)new Chart\([^,]+,\s*(\{.*?\})\s*\)\s*;`)

// extractChartConfig pulls the Chart.js config out of the rendered HTML and
// parses it as JSON. The renderer is expected to emit valid JSON for the config
// (Chart.js accepts a JSON-compatible object literal). Returns the decoded map.
func extractChartConfig(t *testing.T, html string) map[string]any {
	t.Helper()
	m := chartScriptRe.FindStringSubmatch(html)
	require.Len(t, m, 2, "expected exactly one Chart.js config in rendered HTML:\n%s", html)

	var cfg map[string]any
	require.NoError(t, json.Unmarshal([]byte(m[1]), &cfg),
		"Chart.js config must be valid JSON; got:\n%s", m[1])
	return cfg
}

// datasets returns the data.datasets array from a decoded Chart.js config.
func datasets(t *testing.T, cfg map[string]any) []map[string]any {
	t.Helper()
	data, ok := cfg["data"].(map[string]any)
	require.True(t, ok, "config must have a data object")
	raw, ok := data["datasets"].([]any)
	require.True(t, ok, "data must have a datasets array")
	out := make([]map[string]any, 0, len(raw))
	for _, d := range raw {
		ds, ok := d.(map[string]any)
		require.True(t, ok, "each dataset must be an object")
		out = append(out, ds)
	}
	return out
}

// TestRenderChartSeriesStacked drives a bar chart split by a categorical `region`
// column with stacked:true, axis title, and currency Y format. It asserts the
// emitted config has one dataset per distinct region, stacked scale options, the
// axis title, and a distinct color per series.
func TestRenderChartSeriesStacked(t *testing.T) {
	app := newOpenApp(t)
	seedChartData(t, app)

	content := "" +
		"```sql rev\n" +
		"SELECT month, region, revenue FROM chartdata ORDER BY month, region\n" +
		"```\n\n" +
		"{chart: rev, type: bar, x: month, series: region, stacked: true, " +
		"y_fmt: usd1m, y_title: \"Revenue\"}\n"

	insertPage(t, app, "chart_series", "ChartSeries", content)

	html, err := app.RenderPageContent("chart_series", nil)
	require.NoError(t, err)

	cfg := extractChartConfig(t, html)
	ds := datasets(t, cfg)

	// One dataset per distinct region (East, West).
	require.Len(t, ds, 2,
		"series=region must split the single revenue Y column into one dataset per distinct region")

	labels := map[string]bool{}
	for _, d := range ds {
		if l, ok := d["label"].(string); ok {
			labels[l] = true
		}
	}
	assert.True(t, labels["East"] && labels["West"],
		"datasets must be keyed/labeled by the distinct region values, got %v", labels)

	// Distinct color per series.
	colors := map[string]bool{}
	for _, d := range ds {
		if c, ok := d["backgroundColor"].(string); ok {
			colors[c] = true
		}
	}
	assert.GreaterOrEqual(t, len(colors), 2,
		"each series must be assigned a distinct color from the palette")

	// Stacked scale options.
	opts, ok := cfg["options"].(map[string]any)
	require.True(t, ok, "config must carry an options object for stacked/axis settings")
	scales, ok := opts["scales"].(map[string]any)
	require.True(t, ok, "options must carry a scales object")

	xScale, _ := scales["x"].(map[string]any)
	yScale, _ := scales["y"].(map[string]any)
	require.NotNil(t, xScale, "scales.x must be present")
	require.NotNil(t, yScale, "scales.y must be present")
	assert.Equal(t, true, xScale["stacked"], "stacked:true must set scales.x.stacked=true")
	assert.Equal(t, true, yScale["stacked"], "stacked:true must set scales.y.stacked=true")

	// Y axis title from y_title.
	yTitle, _ := yScale["title"].(map[string]any)
	require.NotNil(t, yTitle, "scales.y must carry a title object from y_title")
	assert.Equal(t, "Revenue", yTitle["text"], "y_title must populate scales.y.title.text")
	assert.Equal(t, true, yTitle["display"], "y axis title must be displayed")
}

// TestRenderChartMultiY drives a line chart with two listed Y columns
// (y: [revenue, cost]). It asserts one dataset per listed column, labeled by the
// column name.
func TestRenderChartMultiY(t *testing.T) {
	app := newOpenApp(t)
	seedChartData(t, app)

	// Aggregate to one row per month so the multi-y line has clean labels.
	content := "" +
		"```sql tot\n" +
		"SELECT month, SUM(revenue) AS revenue, SUM(cost) AS cost " +
		"FROM chartdata GROUP BY month ORDER BY month\n" +
		"```\n\n" +
		"{chart: tot, type: line, x: month, y: [revenue, cost], y_title: \"Amount\"}\n"

	insertPage(t, app, "chart_multiy", "ChartMultiY", content)

	html, err := app.RenderPageContent("chart_multiy", nil)
	require.NoError(t, err)

	cfg := extractChartConfig(t, html)
	ds := datasets(t, cfg)

	require.Len(t, ds, 2,
		"y:[revenue, cost] must produce one dataset per listed Y column")

	labels := map[string]bool{}
	for _, d := range ds {
		if l, ok := d["label"].(string); ok {
			labels[l] = true
		}
	}
	assert.True(t, labels["revenue"] && labels["cost"],
		"multi-y datasets must be labeled by their Y column names, got %v", labels)
}

// TestRenderChartSingleSeriesBackwardCompatible guards that an existing
// single-series {chart} page renders unchanged: chart type, a single dataset
// whose data matches the result, and the legacy single backgroundColor. It must
// NOT emit stacked scale options (no stacked key given).
func TestRenderChartSingleSeriesBackwardCompatible(t *testing.T) {
	app := newOpenApp(t)
	seedChartData(t, app)

	content := "" +
		"```sql tot\n" +
		"SELECT month, SUM(revenue) AS revenue FROM chartdata GROUP BY month ORDER BY month\n" +
		"```\n\n" +
		"{chart: tot, type: bar, x: month, y: revenue}\n"

	insertPage(t, app, "chart_single", "ChartSingle", content)

	html, err := app.RenderPageContent("chart_single", nil)
	require.NoError(t, err)

	// Type and canvas still emitted as before.
	assert.Contains(t, html, `type: "bar"`,
		"single-series chart must still emit its Chart.js type")
	assert.Contains(t, html, "vibe-chart-container",
		"single-series chart must still render the chart container")

	cfg := extractChartConfig(t, html)
	ds := datasets(t, cfg)
	require.Len(t, ds, 1,
		"a single non-series, single-Y chart must produce exactly one dataset")

	// The one dataset's data is the three monthly revenue sums.
	raw, ok := ds[0]["data"].([]any)
	require.True(t, ok, "dataset must carry a data array")
	require.Len(t, raw, 3, "single-series dataset must carry one point per month")

	// No stacked scales for a chart without stacked:true.
	if opts, ok := cfg["options"].(map[string]any); ok {
		if scales, ok := opts["scales"].(map[string]any); ok {
			if x, ok := scales["x"].(map[string]any); ok {
				assert.NotEqual(t, true, x["stacked"],
					"a chart without stacked:true must not set stacked scales")
			}
		}
	}

	// Sanity: only one Chart config in the page.
	assert.Equal(t, 1, strings.Count(html, "new Chart("),
		"single-series page must emit exactly one chart")
}
