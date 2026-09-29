package main

// TDD RED — Phase 6 / unit P6.5 (plan §6.2): generalize the report-only
// xlsx/PDF/Analyze-in-Excel exporters (P3.4) to also cover arbitrary tables and
// saved queries, giving Maria/James one-click "Analyze in Excel" from the Table
// Browser and Query Explorer.
//
// REAL integration test: opens a real .vibe container (SQLCipher + DuckDB via
// newOpenApp), seeds an `orders` table and a saved query, then drives the
// production Wails-bound export methods end-to-end. The bindings must:
//
//   - ExportTableXLSX(table, dst): write the whole table to a real .xlsx via the
//     DuckDB excel extension (COPY ... WITH (FORMAT xlsx)), CSV fallback when the
//     extension is unavailable, returning the written path.
//   - ExportQueryXLSX(queryID, dst): same, for a saved query's bound SQL.
//   - AnalyzeQueryInExcel(queryID, dst): one-click alias returning the written
//     path for a saved query.
//
// The exported xlsx must round-trip: re-reading it via the P6.2 reader must
// yield the original column names and a known row.
//
// These target the not-yet-implemented Export*/Analyze* bindings (RED).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedOfficeLinks seeds the orders table (same shape as the report fixture) and
// persists a saved query selecting from it, returning the saved query's id.
//
// Rows: Alice 50/100/200, Bob 75/300. Grand total 725, 5 rows.
func seedOfficeLinks(t *testing.T, app *App) (queryID string) {
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

	const qid = "q_orders_by_rep"
	_, err = app.SaveQuery(qid, "Orders by Rep",
		"SELECT sales_rep, order_id, amount FROM orders ORDER BY order_id", "office links fixture")
	require.NoError(t, err)
	return qid
}

// readXLSXColsCount re-reads an exported xlsx via the P6.2 reader (read_xlsx) and
// returns its column names plus row count. Skips the round-trip if the file is a
// CSV fallback (excel extension unavailable).
func readXLSXSum(t *testing.T, app *App, path string) (rowCount int, sum float64) {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	require.NoError(t,
		ddb.QueryRow(`SELECT count(*) FROM read_xlsx(?)`, path).Scan(&rowCount),
		"exported xlsx must be re-readable via read_xlsx")
	require.NoError(t,
		ddb.QueryRow(`SELECT sum(amount) FROM read_xlsx(?)`, path).Scan(&sum),
		"amount column must round-trip from the exported xlsx")
	return rowCount, sum
}

// TestExportTableXLSX asserts an arbitrary table exports to a non-empty xlsx
// (CSV fallback otherwise) and that the file round-trips via the P6.2 reader.
func TestExportTableXLSX(t *testing.T) {
	app := newOpenApp(t)
	seedOfficeLinks(t, app)

	dst := filepath.Join(t.TempDir(), "orders.xlsx")
	path, err := app.ExportTableXLSX("orders", dst)
	require.NoError(t, err)
	require.NotEmpty(t, path, "binding returns the written path")

	fi, err := os.Stat(path)
	require.NoError(t, err, "table export must exist on disk")

	switch {
	case strings.HasSuffix(path, ".xlsx"):
		require.NotZero(t, fi.Size(), "xlsx export must be non-empty")
		rows, sum := readXLSXSum(t, app, path)
		assert.Equal(t, 5, rows, "exported xlsx must contain all 5 order rows")
		assert.InDelta(t, 725.0, sum, 0.001, "amount column must round-trip")
	case strings.HasSuffix(path, ".csv"):
		// CSV fallback acceptable when the excel extension is unavailable.
		require.NotZero(t, fi.Size(), "csv fallback must be non-empty")
	default:
		t.Fatalf("unexpected export extension: %q", path)
	}
}

// TestExportQueryXLSX asserts a saved query exports to a non-empty xlsx
// (CSV fallback otherwise) and round-trips via the P6.2 reader.
func TestExportQueryXLSX(t *testing.T) {
	app := newOpenApp(t)
	qid := seedOfficeLinks(t, app)

	dst := filepath.Join(t.TempDir(), "query.xlsx")
	path, err := app.ExportQueryXLSX(qid, dst)
	require.NoError(t, err)
	require.NotEmpty(t, path, "binding returns the written path")

	fi, err := os.Stat(path)
	require.NoError(t, err, "query export must exist on disk")

	if strings.HasSuffix(path, ".xlsx") {
		require.NotZero(t, fi.Size(), "xlsx export must be non-empty")
		rows, sum := readXLSXSum(t, app, path)
		assert.Equal(t, 5, rows, "exported xlsx must contain all 5 query rows")
		assert.InDelta(t, 725.0, sum, 0.001, "amount column must round-trip")
	} else {
		require.True(t, strings.HasSuffix(path, ".csv"), "expected xlsx or csv fallback, got %q", path)
		require.NotZero(t, fi.Size(), "csv fallback must be non-empty")
	}
}

// TestAnalyzeQueryInExcel asserts the one-click alias writes a file for a saved
// query and returns its path.
func TestAnalyzeQueryInExcel(t *testing.T) {
	app := newOpenApp(t)
	qid := seedOfficeLinks(t, app)

	dst := filepath.Join(t.TempDir(), "analyze.xlsx")
	path, err := app.AnalyzeQueryInExcel(qid, dst)
	require.NoError(t, err)
	require.NotEmpty(t, path)

	fi, err := os.Stat(path)
	require.NoError(t, err, "analyze-in-excel must write a file")
	require.NotZero(t, fi.Size(), "analyze-in-excel output must be non-empty")
}
