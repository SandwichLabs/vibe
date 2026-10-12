package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SandwichLabs/vibe/internal/importer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- #91 quick wins: studio binding tests ---

// TestSaveQueryOn_ReturnsID proves SaveQueryOn(no id) returns a non-empty id,
// and calling SaveQueryOn(id) again updates the same row rather than
// creating a second one (#91: the id used to be discarded entirely).
func TestSaveQueryOn_ReturnsID(t *testing.T) {
	app := newOpenApp(t)

	first, err := app.SaveQueryOn(SaveQueryRequest{Title: "First title", SQL: "SELECT 1"})
	require.NoError(t, err)
	require.NotEmpty(t, first.ID)

	second, err := app.SaveQueryOn(SaveQueryRequest{ID: first.ID, Title: "Second title", SQL: "SELECT 2", Rev: first.Rev})
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)

	list, err := app.ListQueries()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "Second title", list[0].Title)
}

// TestExportQueryResults_TrailingSemicolonAndComment proves the fix
// end-to-end (P-F10): a query ending in ";" or "-- comment" must export,
// not error.
func TestExportQueryResults_TrailingSemicolonAndComment(t *testing.T) {
	app := newOpenApp(t)

	for _, tc := range []struct {
		name, query string
	}{
		{"semicolon", "SELECT 1 AS a;"},
		{"comment", "SELECT 1 AS a -- c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "out.csv")
			path, err := app.ExportQueryResults(tc.query, "csv", dst)
			require.NoError(t, err)
			assert.NotEmpty(t, path)
			_, statErr := os.Stat(path)
			assert.NoError(t, statErr)
		})
	}
}

// TestExecuteQuery_RowCap proves a query returning more than
// queryGridRowCap rows is capped in RowCount while TotalRows reports the
// true count.
func TestExecuteQuery_RowCap(t *testing.T) {
	app := newOpenApp(t)

	result, err := app.ExecuteQuery("SELECT * FROM range(6000)")
	require.NoError(t, err)
	assert.Equal(t, queryGridRowCap, result.RowCount)
	assert.Equal(t, 6000, result.TotalRows)
	assert.Len(t, result.Rows, queryGridRowCap)
}

// TestGetTableData_DateColumnRendersAsPlainDate proves the datasheet scan
// path (GetTableData -> engine.ScanRows -> engine.NormalizeColumnValue)
// formats a DATE cell as "2026-06-01", not Go's default time.Time
// stringification.
func TestGetTableData_DateColumnRendersAsPlainDate(t *testing.T) {
	app := newOpenApp(t)

	csvPath := filepath.Join(t.TempDir(), "calls.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(
		"id,name,total,active,visited\n"+
			"1,\"Acme, Inc.\",250.50,true,2026-06-01\n"+
			"2,Beta,125.00,false,2026-06-02\n"), 0o644))
	sum, err := app.ImportFile(importer.Options{
		Source: importer.Source{Path: csvPath, HasHeader: true}, Table: "calls_dated", Key: importer.KeyNone,
	})
	require.NoError(t, err)
	require.True(t, sum.OK, "%+v", sum)

	data, err := app.GetTableData("calls_dated", 1, 50, "id", "ASC")
	require.NoError(t, err)
	visitedIdx := -1
	for i, c := range data.Columns {
		if c == "visited" {
			visitedIdx = i
		}
	}
	require.GreaterOrEqual(t, visitedIdx, 0)
	assert.Equal(t, "2026-06-01", data.Rows[0][visitedIdx])
}
