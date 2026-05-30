package main

// P3.10 (plan §3.4 / mockup #11): live Report View bindings — filtered banded
// render, tabular data (chart/table toggle), and the Studio-only Edit-SQL
// source. REAL integration test against a real .vibe container + DuckDB.

import (
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedFilterReport(t *testing.T, app *App) {
	t.Helper()
	seedRepOrders(t, app) // Alice 50,100,200; Bob 75,300

	store := reports.NewReportStore(app.container.SQLiteDB())
	require.NoError(t, store.Save(reports.ReportSpec{
		ID:     "rpt_sales_by_rep",
		Title:  "Sales by Rep",
		Source: "SELECT sales_rep, order_id, amount FROM orders",
		Groups: []reports.GroupLevel{{Field: "sales_rep", SortDir: "asc"}},
		Sort:   []reports.SortSpec{{Field: "amount", Dir: "asc"}},
		Detail: reports.DetailBand{Columns: []reports.ReportColumn{
			{Field: "order_id"}, {Field: "amount"},
		}},
		GroupFooterAggregates:  []reports.Aggregate{{Name: "rep_total", Func: reports.AggSum, Field: "amount"}},
		ReportFooterAggregates: []reports.Aggregate{{Name: "grand_total", Func: reports.AggSum, Field: "amount"}},
	}))
}

// TestRenderReportContentFiltered asserts a live equality filter narrows the
// banded render to the matching group only.
func TestRenderReportContentFiltered(t *testing.T) {
	app := newOpenApp(t)
	seedFilterReport(t, app)

	// Unfiltered: both reps present.
	full, err := app.RenderReportContent("rpt_sales_by_rep")
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(full, "vibe-group-header"), "both reps unfiltered")

	// Filtered to Alice only: one group, no Bob.
	filtered, err := app.RenderReportContentFiltered("rpt_sales_by_rep",
		map[string]string{"sales_rep": "Alice"})
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(filtered, "vibe-group-header"), "one group after filter")
	assert.Contains(t, filtered, "Alice")
	assert.NotContains(t, filtered, "Bob", "Bob filtered out")
	assert.Contains(t, filtered, "350", "Alice subtotal preserved")
}

// TestGetReportData asserts the tabular data binding returns columns + rows and
// honours live filters (drives the chart/table toggle + filter option lists).
func TestGetReportData(t *testing.T) {
	app := newOpenApp(t)
	seedFilterReport(t, app)

	all, err := app.GetReportData("rpt_sales_by_rep", nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"sales_rep", "order_id", "amount"}, all.Columns)
	assert.Len(t, all.Rows, 5, "all 5 orders")

	alice, err := app.GetReportData("rpt_sales_by_rep", map[string]string{"sales_rep": "Alice"})
	require.NoError(t, err)
	assert.Len(t, alice.Rows, 3, "Alice has 3 orders")
	for _, row := range alice.Rows {
		assert.Equal(t, "Alice", row[0])
	}
}

// TestGetReportDataFilterIsParameterized asserts the live filter is bound, not
// string-concatenated (a quote-bearing value must not error or inject).
func TestGetReportDataFilterIsParameterized(t *testing.T) {
	app := newOpenApp(t)
	seedFilterReport(t, app)

	data, err := app.GetReportData("rpt_sales_by_rep",
		map[string]string{"sales_rep": "Alice' OR '1'='1"})
	require.NoError(t, err, "malicious value must be bound, not injected")
	assert.Len(t, data.Rows, 0, "no rows match the literal (injection neutralized)")
}

// TestGetReportSQL asserts the Studio-only Edit-SQL source binding returns the
// report's bound query.
func TestGetReportSQL(t *testing.T) {
	app := newOpenApp(t)
	seedFilterReport(t, app)

	sqlStr, err := app.GetReportSQL("rpt_sales_by_rep")
	require.NoError(t, err)
	assert.Equal(t, "SELECT sales_rep, order_id, amount FROM orders", sqlStr)
}
