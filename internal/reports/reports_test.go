package reports_test

// P3.7 integration test (plan §3.2): banded report model + grouping/aggregation.
//
// This is a REAL integration test against an encrypted .vibe container
// (SQLCipher + DuckDB single-engine). It asserts the full P3.7 acceptance:
//
//   - the additive _vibe_reports migration creates the table with the expected
//     columns (existing containers still open; new ones get the table)
//   - ReportStore round-trips a versioned JSON report spec (group by sales_rep,
//     sort by amount, detail columns, group-footer =Sum(amount), report-footer
//     grand =Sum(amount)) through Save/Get with JSON equality
//   - the pure-Go band-computation step, given a real DuckDB ResultSet of
//     orders-by-rep + the grouping spec, produces an ordered band tree
//     (report header > group header > detail > group footer > report footer)
//     with correct nesting
//   - aggregate expressions (Sum/Count/Avg/Min/Max) compute correctly per group
//     and as a grand total
//
// No mocks: the ResultSet is produced by a real DuckDB query via the engine.

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/reports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPassphrase = "test-passphrase-12345"

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func tableExists(t *testing.T, c *container.Container, name string) bool {
	t.Helper()
	var n int
	err := c.SQLiteDB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", name,
	).Scan(&n)
	require.NoError(t, err)
	return n == 1
}

func columnExists(t *testing.T, c *container.Container, table, col string) bool {
	t.Helper()
	rows, err := c.SQLiteDB().Query("PRAGMA table_info(" + table + ");")
	require.NoError(t, err)
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    any
			pk      int
		)
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		if name == col {
			return true
		}
	}
	return false
}

// sampleReportSpec is the canonical "sales by rep" banded report:
// group by sales_rep, sort detail by amount, detail columns (order_id, amount),
// a group footer subtotal =Sum(amount), and a report footer grand =Sum(amount).
func sampleReportSpec() reports.ReportSpec {
	return reports.ReportSpec{
		ID:     "rpt_sales_by_rep",
		Title:  "Sales by Rep",
		Source: "SELECT sales_rep, order_id, amount FROM orders",
		Groups: []reports.GroupLevel{
			{Field: "sales_rep", SortDir: "asc"},
		},
		Sort: []reports.SortSpec{
			{Field: "amount", Dir: "asc"},
		},
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
}

// TestReportStoreRoundTrip asserts the migration + versioned-JSON round-trip.
func TestReportStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reports.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Reports Test"),
	)
	require.NoError(t, err)

	// Additive migration: _vibe_reports must exist with the expected columns.
	require.True(t, tableExists(t, c, "_vibe_reports"),
		"_vibe_reports table should exist after migration")
	for _, col := range []string{"id", "title", "source", "groups", "sort", "bands", "version", "created_at", "updated_at"} {
		assert.True(t, columnExists(t, c, "_vibe_reports", col),
			"_vibe_reports should have column %q", col)
	}

	store := reports.NewReportStore(c.SQLiteDB())

	spec := sampleReportSpec()
	require.NoError(t, store.Save(spec))

	got, err := store.Get("rpt_sales_by_rep")
	require.NoError(t, err)
	require.NotNil(t, got)

	// JSON equality of the round-tripped spec.
	want, err := json.Marshal(spec)
	require.NoError(t, err)
	have, err := json.Marshal(*got)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(have),
		"report spec must round-trip through Save/Get unchanged")

	// Persist across Save + Close + reopen.
	require.NoError(t, c.Save())
	require.NoError(t, c.Close())

	c2, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c2.Close() //nolint:errcheck

	reopened, err := reports.NewReportStore(c2.SQLiteDB()).Get("rpt_sales_by_rep")
	require.NoError(t, err)
	require.NotNil(t, reopened)
	have2, err := json.Marshal(*reopened)
	require.NoError(t, err)
	assert.JSONEq(t, string(want), string(have2),
		"report spec must persist across reopen")
}

// TestComputeBands runs the grouping/aggregation step over a REAL DuckDB result
// set of orders-by-rep and asserts the ordered band tree + computed aggregates.
func TestComputeBands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report-bands.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
		container.WithTitle("Report Bands"),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	ddb := c.DuckDBDB()
	_, err = ddb.Exec(`CREATE TABLE orders (
		order_id  INTEGER,
		sales_rep TEXT,
		amount    DOUBLE
	);`)
	require.NoError(t, err)
	// Two reps: Alice (3 orders: 100,200,50) and Bob (2 orders: 300,75).
	// Insert out of group/sort order so grouping+sorting is actually exercised.
	_, err = ddb.Exec(`INSERT INTO orders (order_id, sales_rep, amount) VALUES
		(3, 'Bob',   300),
		(1, 'Alice', 200),
		(5, 'Alice', 50),
		(2, 'Bob',   75),
		(4, 'Alice', 100);`)
	require.NoError(t, err)

	mgr := engine.NewManager(ddb, engine.WithLogger(testLogger()))
	rs, err := mgr.Execute(context.Background(),
		`SELECT sales_rep, order_id, amount FROM orders;`, nil)
	require.NoError(t, err)
	require.Equal(t, 5, rs.RowCount())

	spec := sampleReportSpec()
	tree, err := reports.ComputeBands(rs, spec)
	require.NoError(t, err)
	require.NotNil(t, tree)

	// Ordered band tree: a single report-header band, one group band per rep in
	// sort order (Alice before Bob), and a report footer carrying the grand total.
	require.Len(t, tree.Groups, 2, "one group band per distinct sales_rep")
	assert.Equal(t, "Alice", tree.Groups[0].Key, "groups must be in ascending sort order")
	assert.Equal(t, "Bob", tree.Groups[1].Key)

	// Alice: 3 detail rows nested under her group, sorted by amount asc.
	alice := tree.Groups[0]
	require.Len(t, alice.Details, 3, "detail rows nested under the correct group")
	amtIdx := rs.ColumnIndex("amount")
	require.GreaterOrEqual(t, amtIdx, 0)
	assert.Equal(t, 50.0, toFloat(alice.Details[0][amtIdx]), "detail rows sorted by amount asc")
	assert.Equal(t, 100.0, toFloat(alice.Details[1][amtIdx]))
	assert.Equal(t, 200.0, toFloat(alice.Details[2][amtIdx]))

	// Bob: 2 detail rows.
	bob := tree.Groups[1]
	require.Len(t, bob.Details, 2)

	// Group-footer subtotal =Sum(amount) per rep.
	assert.InDelta(t, 350.0, alice.FooterAggregates["rep_total"], 1e-9,
		"Alice group footer Sum must equal her subtotal (50+100+200)")
	assert.InDelta(t, 375.0, bob.FooterAggregates["rep_total"], 1e-9,
		"Bob group footer Sum must equal his subtotal (300+75)")

	// Report-footer grand =Sum(amount) over all rows.
	assert.InDelta(t, 725.0, tree.FooterAggregates["grand_total"], 1e-9,
		"report footer grand total must equal the sum of all amounts")
}

// TestAggregateFunctions exercises each aggregate function (Sum/Count/Avg/Min/Max)
// over a single group and the whole report.
func TestAggregateFunctions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report-agg.vibe")

	c, err := container.Create(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err)
	defer c.Close() //nolint:errcheck

	ddb := c.DuckDBDB()
	_, err = ddb.Exec(`CREATE TABLE orders (sales_rep TEXT, amount DOUBLE);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`INSERT INTO orders VALUES
		('Alice', 100),('Alice', 200),('Alice', 50);`)
	require.NoError(t, err)

	mgr := engine.NewManager(ddb, engine.WithLogger(testLogger()))
	rs, err := mgr.Execute(context.Background(),
		`SELECT sales_rep, amount FROM orders;`, nil)
	require.NoError(t, err)

	spec := reports.ReportSpec{
		ID:     "rpt_agg",
		Title:  "Aggregates",
		Source: "SELECT sales_rep, amount FROM orders",
		Groups: []reports.GroupLevel{{Field: "sales_rep", SortDir: "asc"}},
		Detail: reports.DetailBand{
			Columns: []reports.ReportColumn{{Field: "amount"}},
		},
		ReportFooterAggregates: []reports.Aggregate{
			{Name: "s", Func: reports.AggSum, Field: "amount"},
			{Name: "c", Func: reports.AggCount, Field: "amount"},
			{Name: "a", Func: reports.AggAvg, Field: "amount"},
			{Name: "mn", Func: reports.AggMin, Field: "amount"},
			{Name: "mx", Func: reports.AggMax, Field: "amount"},
		},
	}

	tree, err := reports.ComputeBands(rs, spec)
	require.NoError(t, err)

	assert.InDelta(t, 350.0, tree.FooterAggregates["s"], 1e-9, "Sum")
	assert.InDelta(t, 3.0, tree.FooterAggregates["c"], 1e-9, "Count")
	assert.InDelta(t, 350.0/3.0, tree.FooterAggregates["a"], 1e-9, "Avg")
	assert.InDelta(t, 50.0, tree.FooterAggregates["mn"], 1e-9, "Min")
	assert.InDelta(t, 200.0, tree.FooterAggregates["mx"], 1e-9, "Max")
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
