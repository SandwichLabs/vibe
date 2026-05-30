package main

// TDD RED — Phase 3 / unit P3.8 (plan §3.3): the studio binding
// RenderReportContent(reportID).
//
// REAL integration test: opens a real .vibe container (SQLCipher + DuckDB via
// newOpenApp), seeds an `orders` table, persists a "sales by rep" report spec
// via the P3.7 ReportStore, then drives the production Wails-bound
// RenderReportContent end-to-end. The binding must:
//
//   - load the spec via ReportStore
//   - run the bound query on the REAL engine
//   - compute bands (P3.7 reports.ComputeBands)
//   - render banded HTML (P3.8 RenderReport)
//
// Assertions verify the returned HTML contains a report header with the title,
// one group-header band per rep, detail rows, group-footer subtotals (=Sum),
// a report-footer grand total, print CSS / page-break, and a page-number/date
// element — and that the subtotal values match the DuckDB aggregates.
//
// This targets the not-yet-implemented RenderReportContent binding (RED).

import (
	"regexp"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedRepOrders creates and populates an orders-by-rep table on the real DuckDB
// engine. Alice: 50,100,200 -> 350. Bob: 75,300 -> 375. Grand total 725.
func seedRepOrders(t *testing.T, app *App) {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)

	_, err := ddb.Exec(`CREATE TABLE orders (
		order_id  INTEGER,
		sales_rep TEXT,
		amount    DOUBLE
	);`)
	require.NoError(t, err)
	// Inserted out of group/sort order so band computation is exercised.
	_, err = ddb.Exec(`INSERT INTO orders (order_id, sales_rep, amount) VALUES
		(3, 'Bob',   300),
		(1, 'Alice', 200),
		(5, 'Alice', 50),
		(2, 'Bob',   75),
		(4, 'Alice', 100);`)
	require.NoError(t, err)
}

// TestRenderReportContent drives the Wails-bound RenderReportContent over a real
// container + DuckDB and asserts the full banded report HTML.
func TestRenderReportContent(t *testing.T) {
	app := newOpenApp(t)
	seedRepOrders(t, app)

	// Persist the canonical "sales by rep" report spec via the real ReportStore.
	store := reports.NewReportStore(app.container.SQLiteDB())
	spec := reports.ReportSpec{
		ID:     "rpt_sales_by_rep",
		Title:  "Sales by Rep",
		Source: "SELECT sales_rep, order_id, amount FROM orders",
		Groups: []reports.GroupLevel{{Field: "sales_rep", SortDir: "asc"}},
		Sort:   []reports.SortSpec{{Field: "amount", Dir: "asc"}},
		Detail: reports.DetailBand{
			Columns: []reports.ReportColumn{
				{Field: "order_id", Caption: "Order"},
				{Field: "amount", Caption: "Amount"},
			},
		},
		GroupFooterAggregates: []reports.Aggregate{
			{Name: "rep_total", Func: reports.AggSum, Field: "amount"},
		},
		ReportFooterAggregates: []reports.Aggregate{
			{Name: "grand_total", Func: reports.AggSum, Field: "amount"},
		},
	}
	require.NoError(t, store.Save(spec))

	html, err := app.RenderReportContent("rpt_sales_by_rep")
	require.NoError(t, err)

	// Report header with title.
	assert.Regexp(t, `class="[^"]*vibe-report-header`, html, "report-header band")
	assert.Contains(t, html, "Sales by Rep", "report title")

	// One group-header band per rep.
	assert.Equal(t, 2, strings.Count(html, "vibe-group-header"),
		"one group-header band per distinct sales_rep")
	assert.Contains(t, html, "Alice")
	assert.Contains(t, html, "Bob")

	// Detail rows under each group.
	assert.Regexp(t, `class="[^"]*vibe-report-detail`, html, "detail rows")

	// Group-footer subtotals (=Sum) — values match the DuckDB aggregates.
	assert.Regexp(t, `class="[^"]*vibe-group-footer`, html, "group-footer band")
	assert.Contains(t, html, "350", "Alice subtotal must match the DuckDB Sum (50+100+200)")
	assert.Contains(t, html, "375", "Bob subtotal must match the DuckDB Sum (300+75)")

	// Report-footer grand total.
	assert.Regexp(t, `class="[^"]*vibe-report-footer`, html, "report-footer band")
	assert.Contains(t, html, "725", "grand total must match the DuckDB Sum of all amounts")

	// Print CSS + page-break + page-number/date footer.
	assert.Contains(t, html, "@media print", "print CSS")
	assert.Regexp(t, `page-break-(before|after)`, html, "page-break rules")
	assert.Regexp(t, `class="[^"]*vibe-page-number`, html, "page-number element")
	assert.Regexp(t, `class="[^"]*vibe-page-date`, html, "page-date element")
}

// TestRenderReportContentEmpty asserts an empty result renders an empty-report
// state rather than an error.
func TestRenderReportContentEmpty(t *testing.T) {
	app := newOpenApp(t)

	ddb := app.container.DuckDBDB()
	_, err := ddb.Exec(`CREATE TABLE orders (order_id INTEGER, sales_rep TEXT, amount DOUBLE);`)
	require.NoError(t, err)

	store := reports.NewReportStore(app.container.SQLiteDB())
	require.NoError(t, store.Save(reports.ReportSpec{
		ID:     "rpt_empty",
		Title:  "Empty Report",
		Source: "SELECT sales_rep, order_id, amount FROM orders",
		Groups: []reports.GroupLevel{{Field: "sales_rep", SortDir: "asc"}},
		Detail: reports.DetailBand{Columns: []reports.ReportColumn{{Field: "order_id"}}},
	}))

	html, err := app.RenderReportContent("rpt_empty")
	require.NoError(t, err, "empty result must not be an error")
	assert.Regexp(t, regexp.MustCompile(`class="[^"]*vibe-report-empty`), html,
		"empty result must render an empty-report state")
}
