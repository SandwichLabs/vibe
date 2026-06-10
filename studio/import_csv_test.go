package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImportCSVInfersTypes proves the CSV importer delegates to DuckDB's
// sniffer: numeric, boolean, and date columns must not land as VARCHAR (the
// old hand-rolled importer typed everything TEXT, which broke aggregates and
// made imported tables unable to participate in relationships).
func TestImportCSVInfersTypes(t *testing.T) {
	app := newOpenApp(t)

	csvPath := filepath.Join(t.TempDir(), "calls.csv")
	require.NoError(t, os.WriteFile(csvPath, []byte(
		"id,name,total,active,visited\n"+
			"1,\"Acme, Inc.\",250.50,true,2026-06-01\n"+
			"2,Beta,125.00,false,2026-06-02\n"), 0o644))

	res, err := app.ImportCSV(csvPath, "calls_typed")
	require.NoError(t, err)
	assert.Equal(t, 2, res.RowsImported)

	desc, err := app.DescribeTable("calls_typed")
	require.NoError(t, err)
	types := map[string]string{}
	for _, c := range desc.Columns {
		types[c.Name] = c.Type
	}
	assert.NotEqual(t, "VARCHAR", types["id"], "id should be numeric, got %s", types["id"])
	assert.NotEqual(t, "VARCHAR", types["total"], "total should be numeric, got %s", types["total"])
	assert.Equal(t, "BOOLEAN", types["active"])
	assert.Equal(t, "DATE", types["visited"])
	assert.Equal(t, "VARCHAR", types["name"])

	// Quoted field with an embedded comma must survive intact.
	rs, err := app.ExecuteQuery("SELECT name FROM calls_typed WHERE id = 1")
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)
	assert.Equal(t, "Acme, Inc.", rs.Rows[0][0])

	// Aggregates over the inferred numeric column must work (previously:
	// "No function matches sum(VARCHAR)").
	rs, err = app.ExecuteQuery("SELECT sum(total) FROM calls_typed")
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)

	// Appending into the existing table goes through INSERT ... BY NAME.
	csvPath2 := filepath.Join(t.TempDir(), "more.csv")
	require.NoError(t, os.WriteFile(csvPath2, []byte(
		"total,id,name,active,visited\n99.99,3,Gamma,true,2026-06-03\n"), 0o644))
	res, err = app.ImportCSV(csvPath2, "calls_typed")
	require.NoError(t, err)
	assert.Equal(t, 1, res.RowsImported)
}
