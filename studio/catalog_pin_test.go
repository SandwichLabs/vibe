package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/SandwichLabs/vibe/internal/workspace"
)

// These pin the connection tree's levels at the binding (#54 slice 8): schemas
// (hidden only for a lone `main`), tables with kinds and row estimates, columns.
func TestListCatalog_LevelsSchemasTablesViewsColumns(t *testing.T) {
	app, _ := connApp(t)
	p := filepath.Join(t.TempDir(), "multi.duckdb")
	db, err := sql.Open("duckdb", p)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE SCHEMA sales; CREATE TABLE sales.orders (id INTEGER PRIMARY KEY, total DOUBLE);
		INSERT INTO sales.orders SELECT i, i*1.5 FROM range(5) t(i);
		CREATE VIEW sales.big AS SELECT * FROM sales.orders WHERE total > 1;
		CREATE TABLE main.top (n INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = app.SaveConnection(SaveConnectionRequest{Scope: "local", IsNew: true, Def: workspace.ConnectionDef{Name: "multi", Type: "duckdb", Path: p}})
	require.NoError(t, err)

	// Two schemas: the schema level is shown.
	lvl, err := app.ListCatalog(CatalogRequest{Connection: "multi"})
	require.NoError(t, err)
	assert.Equal(t, "schemas", lvl.Kind)
	assert.False(t, lvl.Flattened)
	names := []string{}
	for _, n := range lvl.Nodes {
		assert.Equal(t, "schema", n.Kind)
		assert.EqualValues(t, -1, n.Rows)
		names = append(names, n.Name)
	}
	assert.ElementsMatch(t, []string{"main", "sales"}, names)

	// A schema's tables and views; the estimate is for tables only.
	lvl, err = app.ListCatalog(CatalogRequest{Connection: "multi", Schema: "sales"})
	require.NoError(t, err)
	assert.Equal(t, "tables", lvl.Kind)
	assert.Equal(t, "sales", lvl.Schema)
	assert.False(t, lvl.Flattened)
	kinds := map[string]string{}
	for _, n := range lvl.Nodes {
		kinds[n.Name] = n.Kind
		assert.Equal(t, "multi.sales."+n.Name, n.Qualified)
		if n.Kind == "view" {
			assert.EqualValues(t, -1, n.Rows)
		}
	}
	assert.Equal(t, map[string]string{"orders": "table", "big": "view"}, kinds)

	// Columns, with the schema defaulted when none is sent.
	cols, err := app.ListCatalog(CatalogRequest{Connection: "multi", Table: "top"})
	require.NoError(t, err)
	assert.Equal(t, "columns", cols.Kind)
	assert.Equal(t, "main", cols.Schema)
	require.Len(t, cols.Nodes, 1)
	assert.Equal(t, "column", cols.Nodes[0].Kind)
	assert.EqualValues(t, -1, cols.Nodes[0].Rows)

	// A bad or unknown connection name.
	_, err = app.ListCatalog(CatalogRequest{Connection: `x"; DROP`})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "There is no connection called")
	_, err = app.ListCatalog(CatalogRequest{Connection: "nope"})
	require.Error(t, err)
}

func TestListCatalog_ALoneNonMainSchemaStillShowsTheLevel(t *testing.T) {
	app, _ := connApp(t)
	p := filepath.Join(t.TempDir(), "one.duckdb")
	db, err := sql.Open("duckdb", p)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE SCHEMA only_one; CREATE TABLE only_one.t (id INTEGER)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = app.SaveConnection(SaveConnectionRequest{Scope: "local", IsNew: true, Def: workspace.ConnectionDef{Name: "one", Type: "duckdb", Path: p}})
	require.NoError(t, err)
	lvl, err := app.ListCatalog(CatalogRequest{Connection: "one"})
	require.NoError(t, err)
	// main is empty but still a schema, so two schemas: the level shows.
	assert.Equal(t, "schemas", lvl.Kind)
}
