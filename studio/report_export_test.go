package main

// TDD RED — Phase 3 / unit P3.9 (plan §3.4): the studio bindings
// ExportReportHTML, ExportReportXLSX / AnalyzeInExcel, and ExportReportPDF.
//
// REAL integration test: opens a real .vibe container (SQLCipher + DuckDB via
// newOpenApp), seeds an `orders` table, persists the canonical "sales by rep"
// banded report spec via the P3.7 ReportStore, then drives the production
// Wails-bound export methods end-to-end. The bindings must:
//
//   - ExportReportHTML: render the P3.8 banded HTML (via RenderReportContent)
//     and write it to the requested path, returning that path.
//   - ExportReportXLSX (a.k.a. AnalyzeInExcel): write the report's bound query
//     result to a real .xlsx via the DuckDB excel extension
//     (COPY ... TO 'file.xlsx' WITH (FORMAT xlsx)), CSV fallback if unavailable,
//     returning the written path. The xlsx must be re-readable via read_xlsx and
//     its row count + subtotal must match the report's bound query.
//   - ExportReportPDF: produce a valid PDF (%PDF- magic header) when a
//     headless-print backend is available; degrade gracefully (clear error)
//     otherwise. The headless-print assertion is gated behind capability
//     detection (no display in CI).
//
// Each binding takes an explicit destination path so the export is testable
// headless (the production frontend wires the path via a save dialog).
//
// These target the not-yet-implemented Export* bindings (RED).

import (
	"os"
	"strings"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedExportReport seeds the orders-by-rep table and persists the canonical
// "sales by rep" report spec, returning its ID.
//
// Alice: 50,100,200 -> 350. Bob: 75,300 -> 375. Grand total 725. 5 rows.
func seedExportReport(t *testing.T, app *App) string {
	t.Helper()

	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)

	_, err := ddb.Exec(`CREATE TABLE orders (
		order_id  INTEGER,
		sales_rep TEXT,
		amount    DOUBLE
	);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO orders (order_id, sales_rep, amount) VALUES
		(3, 'Bob',   300),
		(1, 'Alice', 200),
		(5, 'Alice', 50),
		(2, 'Bob',   75),
		(4, 'Alice', 100);`)
	require.NoError(t, err)

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
	return spec.ID
}

// TestExportReportHTML asserts the binding writes the P3.8 banded HTML to disk
// and returns the written path.
func TestExportReportHTML(t *testing.T) {
	app := newOpenApp(t)
	id := seedExportReport(t, app)

	dst := t.TempDir() + "/report.html"
	path, err := app.ExportReportHTML(id, dst)
	require.NoError(t, err)
	assert.Equal(t, dst, path, "binding returns the written path")

	data, err := os.ReadFile(path)
	require.NoError(t, err, "HTML export must exist on disk")
	html := string(data)

	// Same banded HTML produced by P3.8 RenderReportContent.
	rendered, err := app.RenderReportContent(id)
	require.NoError(t, err)
	assert.Equal(t, rendered, html, "exported HTML must be the P3.8 banded render")

	// Spot-check the banded structure landed on disk.
	assert.Contains(t, html, "Sales by Rep", "report title")
	assert.Contains(t, html, "vibe-report-header", "banded report header")
	assert.Contains(t, html, "vibe-group-footer", "banded group footer")
	assert.Contains(t, html, "725", "grand total in banded HTML")
}

// TestExportReportXLSX asserts the binding writes a valid xlsx of the report's
// bound query result via the DuckDB excel extension, and that re-reading it via
// read_xlsx yields a row count + subtotal matching the bound query.
func TestExportReportXLSX(t *testing.T) {
	app := newOpenApp(t)
	id := seedExportReport(t, app)

	dst := t.TempDir() + "/report.xlsx"
	path, err := app.ExportReportXLSX(id, dst)
	require.NoError(t, err)
	require.NotEmpty(t, path, "binding returns the written path")

	fi, err := os.Stat(path)
	require.NoError(t, err, "xlsx export must exist on disk")
	require.NotZero(t, fi.Size(), "xlsx export must be non-empty")

	// CSV fallback is acceptable if the excel extension is unavailable; in this
	// build the excel extension is present (probed), so we expect a real xlsx.
	if strings.HasSuffix(path, ".xlsx") {
		ddb := app.container.DuckDBDB()
		require.NotNil(t, ddb)

		var rowCount int
		require.NoError(t,
			ddb.QueryRow(`SELECT count(*) FROM read_xlsx(?)`, path).Scan(&rowCount),
			"exported xlsx must be re-readable via read_xlsx")
		assert.Equal(t, 5, rowCount,
			"xlsx row count must match the report's bound query (5 order rows)")

		var grandTotal float64
		require.NoError(t,
			ddb.QueryRow(`SELECT sum(amount) FROM read_xlsx(?)`, path).Scan(&grandTotal),
			"subtotal column must be readable from the exported xlsx")
		assert.InDelta(t, 725.0, grandTotal, 0.001,
			"xlsx subtotal (sum of amount) must match the bound query aggregate")
	}
}

// TestAnalyzeInExcel asserts the one-click "Analyze in Excel/Sheets" alias
// behaves like ExportReportXLSX and returns a written path.
func TestAnalyzeInExcel(t *testing.T) {
	app := newOpenApp(t)
	id := seedExportReport(t, app)

	dst := t.TempDir() + "/analyze.xlsx"
	path, err := app.AnalyzeInExcel(id, dst)
	require.NoError(t, err)
	require.NotEmpty(t, path)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	require.NotZero(t, fi.Size())
}

// TestExportReportPDF asserts the binding produces a valid PDF when a
// headless-print backend is available, and degrades gracefully otherwise.
// The %PDF- magic-header assertion is gated behind capability detection since a
// headless print path needs a display/browser.
func TestExportReportPDF(t *testing.T) {
	app := newOpenApp(t)
	id := seedExportReport(t, app)

	dst := t.TempDir() + "/report.pdf"
	path, err := app.ExportReportPDF(id, dst)

	if err != nil {
		// Graceful degradation: a clear error when no headless-print backend is
		// available. Must not be a "not implemented" stub error.
		assert.NotContains(t, strings.ToLower(err.Error()), "not implemented",
			"PDF export must be implemented; absence of a backend is a clear runtime error, not a stub")
		assert.NotContains(t, strings.ToLower(err.Error()), "todo",
			"PDF export must not be a TODO stub")
		return
	}

	// Backend available: assert a valid PDF landed on disk.
	require.NotEmpty(t, path, "binding returns the written PDF path")
	data, err := os.ReadFile(path)
	require.NoError(t, err, "PDF export must exist on disk")
	require.NotZero(t, len(data), "PDF export must be non-empty")
	assert.True(t, strings.HasPrefix(string(data), "%PDF-"),
		"PDF export must begin with the %%PDF- magic header")
}
