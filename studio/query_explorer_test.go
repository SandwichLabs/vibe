package main

// W5 Query Explorer backend tests: schema-tree stats, real PK flags from
// DescribeTable, and CSV/Parquet query exports — all against a real .vibe
// container + DuckDB (no mocks).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedExplorer(t *testing.T, app *App) {
	t.Helper()
	require.NoError(t, app.CreateTable("customers", []ColumnInfo{
		{Name: "id", Type: "BIGINT", PK: true},
		{Name: "name", Type: "VARCHAR"},
	}))
	require.NoError(t, app.CreateTable("service_calls", []ColumnInfo{
		{Name: "id", Type: "BIGINT", PK: true},
		{Name: "customer_id", Type: "BIGINT"},
		{Name: "total", Type: "DECIMAL(12,2)"},
	}))
	for _, q := range []string{
		`INSERT INTO customers (name) VALUES ('Acme'), ('Beta')`,
		`INSERT INTO service_calls (customer_id, total) VALUES (1, 100.50), (1, 75.25), (2, 20.00)`,
	} {
		_, err := app.ExecuteQuery(q)
		require.NoError(t, err)
	}
}

func TestDescribeTableReportsPrimaryKeys(t *testing.T) {
	app := newOpenApp(t)
	seedExplorer(t, app)

	info, err := app.DescribeTable("service_calls")
	require.NoError(t, err)
	pks := map[string]bool{}
	for _, c := range info.Columns {
		pks[c.Name] = c.PK
	}
	assert.True(t, pks["id"], "id must be flagged as the primary key")
	assert.False(t, pks["customer_id"])
	assert.False(t, pks["total"])
}

func TestExportQueryResults(t *testing.T) {
	app := newOpenApp(t)
	seedExplorer(t, app)
	dir := t.TempDir()

	t.Run("csv", func(t *testing.T) {
		dst := filepath.Join(dir, "out.csv")
		path, err := app.ExportQueryResults(
			"SELECT name, COUNT(*) AS calls FROM customers c JOIN service_calls s ON s.customer_id = c.id GROUP BY name ORDER BY name",
			"csv", dst)
		require.NoError(t, err)
		assert.Equal(t, dst, path)
		data, err := os.ReadFile(dst)
		require.NoError(t, err)
		text := string(data)
		assert.True(t, strings.HasPrefix(text, "name,calls"), "CSV header expected, got: %q", text[:min(40, len(text))])
		assert.Contains(t, text, "Acme,2")
		assert.Contains(t, text, "Beta,1")
	})

	t.Run("parquet", func(t *testing.T) {
		dst := filepath.Join(dir, "out.parquet")
		path, err := app.ExportQueryResults("SELECT * FROM service_calls", "parquet", dst)
		require.NoError(t, err)
		assert.Equal(t, dst, path)
		// Parquet files start with the PAR1 magic bytes.
		data, err := os.ReadFile(dst)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(data), 4)
		assert.Equal(t, "PAR1", string(data[:4]))
		// And DuckDB itself can read it back.
		rs, err := app.ExecuteQuery("SELECT COUNT(*) FROM read_parquet('" + strings.ReplaceAll(dst, "'", "''") + "')")
		require.NoError(t, err)
		require.Len(t, rs.Rows, 1)
	})

	t.Run("extension appended when missing", func(t *testing.T) {
		dst := filepath.Join(dir, "noext")
		path, err := app.ExportQueryResults("SELECT 1 AS x", "csv", dst)
		require.NoError(t, err)
		assert.Equal(t, dst+".csv", path)
	})

	t.Run("rejects unknown formats and empty SQL", func(t *testing.T) {
		_, err := app.ExportQueryResults("SELECT 1", "xml", filepath.Join(dir, "x"))
		assert.Error(t, err)
		_, err = app.ExportQueryResults("  ", "csv", filepath.Join(dir, "y"))
		assert.Error(t, err)
	})
}
