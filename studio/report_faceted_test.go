package main

// W6 Report View backend tests: faceted filters (eq / in / range), chart
// metadata round-trip, editable report SQL, and CSV export — all against a
// real .vibe container + DuckDB (no mocks).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedFacetedReport(t *testing.T, app *App) {
	t.Helper()
	seedRepOrders(t, app) // Alice 50,100,200; Bob 75,300

	store := reports.NewReportStore(app.container.SQLiteDB())
	require.NoError(t, store.Save(reports.ReportSpec{
		ID:     "rpt_faceted",
		Title:  "Orders faceted",
		Source: "SELECT sales_rep, order_id, amount FROM orders",
		Detail: reports.DetailBand{Columns: []reports.ReportColumn{
			{Field: "order_id"}, {Field: "amount"},
		}},
		Chart: &reports.ChartSpec{
			LabelField:  "order_id",
			SeriesField: "sales_rep",
			ValueField:  "amount",
		},
	}))
}

func TestGetReportDataFacetedIn(t *testing.T) {
	app := newOpenApp(t)
	seedFacetedReport(t, app)

	// Multi-select: both reps → all rows; one rep → that rep only.
	both, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "sales_rep", Op: "in", Values: []string{"Alice", "Bob"}},
	})
	require.NoError(t, err)
	assert.Len(t, both.Rows, 5)

	alice, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "sales_rep", Op: "in", Values: []string{"Alice"}},
	})
	require.NoError(t, err)
	assert.Len(t, alice.Rows, 3)
	for _, row := range alice.Rows {
		assert.Equal(t, "Alice", row[0])
	}

	// Empty selections are a no-op, not an empty result.
	none, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "sales_rep", Op: "in", Values: []string{}},
	})
	require.NoError(t, err)
	assert.Len(t, none.Rows, 5)
}

func TestGetReportDataFacetedRange(t *testing.T) {
	app := newOpenApp(t)
	seedFacetedReport(t, app)

	// amount in [75, 200] → 75, 100, 200.
	mid, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "amount", Op: "range", Values: []string{"75", "200"}},
	})
	require.NoError(t, err)
	assert.Len(t, mid.Rows, 3)

	// Open-ended: >= 200 → 200, 300.
	high, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "amount", Op: "range", Values: []string{"200", ""}},
	})
	require.NoError(t, err)
	assert.Len(t, high.Rows, 2)

	// Numeric, not lexical: >= 75 must include 100 and 300.
	num, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "amount", Op: "range", Values: []string{"75", ""}},
	})
	require.NoError(t, err)
	assert.Len(t, num.Rows, 4, "numeric comparison: 75,100,200,300")
}

func TestGetReportDataFacetedRejectsBadInput(t *testing.T) {
	app := newOpenApp(t)
	seedFacetedReport(t, app)

	_, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: `amount" OR 1=1 --`, Op: "eq", Values: []string{"1"}},
	})
	assert.Error(t, err, "non-identifier columns are rejected")

	_, err = app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "amount", Op: "between", Values: []string{"1", "2"}},
	})
	assert.Error(t, err, "unknown ops are rejected")

	// Values are bound, not concatenated.
	inj, err := app.GetReportDataFaceted("rpt_faceted", []ReportFilter{
		{Column: "sales_rep", Op: "in", Values: []string{"Alice' OR '1'='1"}},
	})
	require.NoError(t, err)
	assert.Len(t, inj.Rows, 0, "injection neutralized by binding")
}

func TestGetReportDataFacetedReturnsChartAndDuration(t *testing.T) {
	app := newOpenApp(t)
	seedFacetedReport(t, app)

	data, err := app.GetReportDataFaceted("rpt_faceted", nil)
	require.NoError(t, err)
	require.NotNil(t, data.Chart, "stored chart spec rides along with the data")
	assert.Equal(t, "order_id", data.Chart.LabelField)
	assert.Equal(t, "sales_rep", data.Chart.SeriesField)
	assert.Equal(t, "amount", data.Chart.ValueField)
	assert.GreaterOrEqual(t, data.DurationMs, 0.0)

	// A report saved without a chart spec returns nil (viewer heuristics).
	store := reports.NewReportStore(app.container.SQLiteDB())
	require.NoError(t, store.Save(reports.ReportSpec{
		ID: "rpt_nochart", Title: "No chart", Source: "SELECT * FROM orders",
		Detail: reports.DetailBand{Columns: []reports.ReportColumn{{Field: "order_id"}}},
	}))
	plain, err := app.GetReportDataFaceted("rpt_nochart", nil)
	require.NoError(t, err)
	assert.Nil(t, plain.Chart)
}

func TestRenderReportContentFaceted(t *testing.T) {
	app := newOpenApp(t)
	seedFilterReport(t, app) // grouped sales-by-rep spec

	htmlOut, err := app.RenderReportContentFaceted("rpt_sales_by_rep", []ReportFilter{
		{Column: "sales_rep", Op: "in", Values: []string{"Bob"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(htmlOut, "vibe-group-header"))
	assert.Contains(t, htmlOut, "Bob")
	assert.NotContains(t, htmlOut, "Alice")
}

func TestUpdateReportSQL(t *testing.T) {
	app := newOpenApp(t)
	seedFacetedReport(t, app)

	// Invalid SQL is rejected by actually running it.
	err := app.UpdateReportSQL("rpt_faceted", "SELECT nope FROM missing_table")
	assert.ErrorContains(t, err, "validation")

	// Empty SQL is rejected.
	assert.Error(t, app.UpdateReportSQL("rpt_faceted", "   "))

	// Valid SQL persists and flows through data + SQL bindings.
	newSQL := "SELECT sales_rep, amount FROM orders WHERE amount > 100"
	require.NoError(t, app.UpdateReportSQL("rpt_faceted", newSQL))
	got, err := app.GetReportSQL("rpt_faceted")
	require.NoError(t, err)
	assert.Equal(t, newSQL, got)
	data, err := app.GetReportDataFaceted("rpt_faceted", nil)
	require.NoError(t, err)
	assert.Len(t, data.Rows, 2, "200 and 300 only")

	// The rest of the spec (chart metadata) survives the SQL edit.
	require.NotNil(t, data.Chart)
	assert.Equal(t, "sales_rep", data.Chart.SeriesField)
}

func TestUpdateReportSQLIsStudioOnly(t *testing.T) {
	app := newOpenApp(t)
	seedFacetedReport(t, app)
	require.NoError(t, app.SetMode("app"))

	err := app.UpdateReportSQL("rpt_faceted", "SELECT 1")
	assert.Error(t, err, "App Mode cannot edit report SQL")
}

func TestExportReportCSV(t *testing.T) {
	app := newOpenApp(t)
	seedFacetedReport(t, app)
	dir := t.TempDir()

	dst := filepath.Join(dir, "orders.csv")
	path, err := app.ExportReportCSV("rpt_faceted", []ReportFilter{
		{Column: "sales_rep", Op: "eq", Values: []string{"Bob"}},
	}, dst)
	require.NoError(t, err)
	assert.Equal(t, dst, path)

	data, err := os.ReadFile(dst)
	require.NoError(t, err)
	text := string(data)
	assert.True(t, strings.HasPrefix(text, "sales_rep,order_id,amount"), "header row, got %q", text)
	assert.Contains(t, text, "Bob")
	assert.NotContains(t, text, "Alice")
	assert.Len(t, strings.Split(strings.TrimSpace(text), "\n"), 3, "header + 2 Bob rows")

	// Extension appended when missing.
	noext, err := app.ExportReportCSV("rpt_faceted", nil, filepath.Join(dir, "plain"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "plain")+".csv", noext)
}
