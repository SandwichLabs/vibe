package main

// TDD RED — Phase 3 / unit P3.3: "Enhanced metric and table components"
// (parity plan §3, Evidence WU-7 + WU-8), studio render-path slice.
//
// REAL integration test: opens a real .vibe container (via newOpenApp), seeds a
// DuckDB `sales` table, inserts pages exercising the enhanced {metric} and
// {table} directives into _vibe_pages, and drives the production Wails-bound
// RenderPageContent end-to-end.
//
// Enhanced metric (WU-7): title, fmt, comparison delta vs a prior value, and an
// optional sparkline from a time-series result.
// Enhanced table (WU-8): client-side pagination scaffold honoring pageSize,
// per-column fmt (ColumnFmt), title/subtitle, and row_shading (alternating rows).
//
// These assertions intentionally target behavior that renderDirective +
// metric.go/table.go do NOT yet implement (RED). The existing single-value
// metric / plain table assertions guard backward compatibility.

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSales creates and populates a sales table on the real DuckDB engine.
func seedSales(t *testing.T, app *App) {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)

	_, err := ddb.Exec(`CREATE TABLE sales (
		id INTEGER,
		order_date DATE,
		total DECIMAL(14,2)
	);`)
	require.NoError(t, err)

	// 12 rows so pageSize:10 produces more than one page.
	_, err = ddb.Exec(`INSERT INTO sales (id, order_date, total) VALUES
		(1,  DATE '2026-01-01', 100000.00),
		(2,  DATE '2026-01-15', 150000.50),
		(3,  DATE '2026-02-01', 200000.00),
		(4,  DATE '2026-02-15', 250000.00),
		(5,  DATE '2026-03-01', 300000.00),
		(6,  DATE '2026-03-15', 350000.00),
		(7,  DATE '2026-04-01', 175000.25),
		(8,  DATE '2026-04-15', 225000.00),
		(9,  DATE '2026-05-01', 125000.00),
		(10, DATE '2026-05-15', 275000.00),
		(11, DATE '2026-05-20', 325000.00),
		(12, DATE '2026-05-25', 425000.00);`)
	require.NoError(t, err)
}

func insertPage(t *testing.T, app *App, id, title, content string) {
	t.Helper()
	_, err := app.container.SQLiteDB().Exec(
		"INSERT INTO _vibe_pages (id, title, content, page_type, sort_order, created_at, updated_at) VALUES (?, ?, ?, 'notebook', 0, datetime('now'), datetime('now'))",
		id, title, content,
	)
	require.NoError(t, err)
}

// TestRenderMetricEnhancedComparison drives a two-value metric result through
// RenderPageContent and asserts the metric card shows the title, the
// Format(value)-formatted current value, and a comparison delta element that
// encodes the up/down direction as a class.
func TestRenderMetricEnhancedComparison(t *testing.T) {
	app := newOpenApp(t)
	seedSales(t, app)

	// Current revenue 2,000,000 vs prior 1,500,000 -> +33.3% (up).
	content := "" +
		"```sql rev\n" +
		"SELECT 2000000.0 AS value, 1500000.0 AS prior\n" +
		"```\n\n" +
		"{metric: rev, title: \"Revenue\", fmt: usd1m, comparison: prior_period}\n"

	insertPage(t, app, "metric_cmp", "MetricCmp", content)

	html, err := app.RenderPageContent("metric_cmp", nil)
	require.NoError(t, err)

	assert.Contains(t, html, "Revenue",
		"metric must render its title")
	// usd1m of 2,000,000 == "$2.0M"
	assert.Contains(t, html, "$2.0M",
		"metric must render the current value via the usd1m Format code")

	// A delta element with a direction class. current > prior => "up".
	assert.Regexp(t, `class="[^"]*vibe-metric-delta[^"]*up`, html,
		"metric must emit a comparison delta element carrying an up direction class")
	// The signed delta text should be present (positive direction).
	assert.True(t,
		strings.Contains(html, "+33") || strings.Contains(html, "▲") || strings.Contains(html, "↑"),
		"metric delta must indicate a positive change vs the prior value")
}

// TestRenderMetricSparkline asserts a time-series result emits sparkline markup.
func TestRenderMetricSparkline(t *testing.T) {
	app := newOpenApp(t)
	seedSales(t, app)

	content := "" +
		"```sql trend\n" +
		"SELECT order_date, total FROM sales ORDER BY order_date\n" +
		"```\n\n" +
		"{metric: trend, title: \"Sales Trend\", fmt: usd0, sparkline: true}\n"

	insertPage(t, app, "metric_spark", "MetricSpark", content)

	html, err := app.RenderPageContent("metric_spark", nil)
	require.NoError(t, err)

	assert.Contains(t, html, "Sales Trend", "metric must render its title")
	assert.Contains(t, html, "vibe-metric-sparkline",
		"a time-series metric with sparkline:true must emit sparkline markup")
}

// TestRenderMetricSingleValueBackwardCompatible guards that an existing
// single-value metric (no title/comparison) still renders unchanged: the value
// and label, and NO delta element.
func TestRenderMetricSingleValueBackwardCompatible(t *testing.T) {
	app := newOpenApp(t)
	seedSales(t, app)

	content := "" +
		"```sql tot\n" +
		"SELECT SUM(total) AS total FROM sales\n" +
		"```\n\n" +
		"{metric: tot, label: \"Total\", fmt: usd0}\n"

	insertPage(t, app, "metric_single", "MetricSingle", content)

	html, err := app.RenderPageContent("metric_single", nil)
	require.NoError(t, err)

	assert.Contains(t, html, "vibe-metric-value",
		"single-value metric must still render the value element")
	assert.Contains(t, html, "Total", "single-value metric must still render its label")
	assert.NotContains(t, html, "vibe-metric-delta",
		"single-value metric without a comparison must not emit a delta element")
}

// TestRenderTableEnhanced drives the enhanced {table} directive: per-column
// fmt, title, row_shading, and a pagination scaffold honoring pageSize.
func TestRenderTableEnhanced(t *testing.T) {
	app := newOpenApp(t)
	seedSales(t, app)

	content := "" +
		"```sql orders\n" +
		"SELECT order_date, total FROM sales ORDER BY order_date LIMIT 12\n" +
		"```\n\n" +
		"{table: orders, page_size: 10, title: \"Orders\", subtitle: \"Recent\", " +
		"fmt: {total: usd2, order_date: shortdate}, row_shading: true}\n"

	insertPage(t, app, "tbl_enh", "TableEnh", content)

	html, err := app.RenderPageContent("tbl_enh", nil)
	require.NoError(t, err)

	// Per-column format applied to cells.
	// usd2 of 100000.00 == "$100,000.00"
	assert.Contains(t, html, "$100,000.00",
		"table must apply the usd2 ColumnFmt to the total column cells")
	// shortdate of 2026-01-01 == "Jan 1, 2026"
	assert.Contains(t, html, "Jan 1, 2026",
		"table must apply the shortdate ColumnFmt to the order_date column cells")

	// Title / subtitle.
	assert.Contains(t, html, "Orders", "table must render its title")
	assert.Contains(t, html, "Recent", "table must render its subtitle")

	// Pagination scaffold honoring pageSize=10.
	assert.Regexp(t, `data-page-size="10"`, html,
		"table must emit a pagination scaffold carrying the page size")
	assert.Regexp(t, `class="[^"]*vibe-table-pagination`, html,
		"table must emit pagination controls when pageSize is set")

	// Alternating-row (row_shading) markup.
	assert.Regexp(t, `class="[^"]*vibe-row-(odd|even|alt|shaded)`, html,
		"table with row_shading must emit alternating-row markup")
}

// TestRenderTablePlainBackwardCompatible guards that a plain table (no enhanced
// keys) still renders the standard table markup unchanged.
func TestRenderTablePlainBackwardCompatible(t *testing.T) {
	app := newOpenApp(t)
	seedSales(t, app)

	content := "" +
		"```sql plain\n" +
		"SELECT id, total FROM sales ORDER BY id LIMIT 3\n" +
		"```\n\n" +
		"{table: plain}\n"

	insertPage(t, app, "tbl_plain", "TablePlain", content)

	html, err := app.RenderPageContent("tbl_plain", nil)
	require.NoError(t, err)

	assert.Contains(t, html, `class="vibe-table"`,
		"plain table must still render the base table element")
	assert.NotContains(t, html, "vibe-table-pagination",
		"plain table without pageSize must not emit a pagination scaffold")
}
