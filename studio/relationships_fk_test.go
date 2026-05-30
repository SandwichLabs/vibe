package main

import (
	"context"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reopenApp closes the app's container (after saving) and reopens it from the
// same path, re-wiring the engine/registry — mirroring a real Save → close →
// reopen lifecycle so persistence can be asserted.
func reopenApp(t *testing.T, app *App) {
	t.Helper()

	path := app.filePath
	require.NoError(t, app.container.Save())
	require.NoError(t, app.container.Close())

	c, err := container.Open(path,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(app.logger),
	)
	require.NoError(t, err)

	app.container = c
	app.engine = engine.NewManager(c.DuckDBDB(), engine.WithLogger(app.logger))
	app.registry = queries.NewRegistry(c.SQLiteDB(), app.logger)

	t.Cleanup(func() { _ = c.Close() })
}

// seedCustomersOrders creates a parent customers table (id PK) and a child
// orders table (id PK, customer_id) for FK-enforcement tests.
func seedCustomersOrders(t *testing.T, app *App) {
	t.Helper()

	require.NoError(t, app.CreateTable("customers", []ColumnInfo{
		{Name: "id", Type: "INTEGER", PK: true},
		{Name: "name", Type: "VARCHAR"},
	}))
	require.NoError(t, app.CreateTable("orders", []ColumnInfo{
		{Name: "id", Type: "INTEGER", PK: true},
		{Name: "customer_id", Type: "INTEGER"},
		{Name: "total", Type: "DOUBLE"},
	}))

	// Seed one valid customer so valid child inserts have a referent.
	_, err := app.InsertRow("customers", map[string]any{"id": 1, "name": "Acme"})
	require.NoError(t, err)
}

// TestAppAddForeignKeyEnforcement drives the P2.3 Wails-bound FK methods against
// a real .vibe container + DuckDB and asserts the full acceptance behavior:
// enforcement of referential integrity on insert/delete, metadata persistence,
// listing, removal, and survival across Save + reopen.
func TestAppAddForeignKeyEnforcement(t *testing.T) {
	app := newOpenApp(t)
	seedCustomersOrders(t, app)

	// AddForeignKey: orders.customer_id -> customers.id, ON DELETE RESTRICT.
	relID, err := app.AddForeignKey(ForeignKeySpec{
		ChildTable:   "orders",
		ChildColumn:  "customer_id",
		ParentTable:  "customers",
		ParentColumn: "id",
		OnDelete:     "RESTRICT",
	})
	require.NoError(t, err)
	require.NotEmpty(t, relID)

	// A valid child insert (referent exists) succeeds.
	_, err = app.InsertRow("orders", map[string]any{"id": 10, "customer_id": 1, "total": 99.5})
	require.NoError(t, err)

	// An orphan child insert (non-existent customer) is REJECTED with a wrapped,
	// non-panic error surfacing the FK violation.
	_, err = app.InsertRow("orders", map[string]any{"id": 11, "customer_id": 999, "total": 5})
	require.Error(t, err, "insert referencing a non-existent customer must be rejected")

	// Deleting a referenced parent (customer 1 is referenced by order 10) is
	// rejected by the ON DELETE RESTRICT constraint.
	_, err = app.DeleteRow("customers", "id", 1)
	require.Error(t, err, "deleting a referenced customer must be rejected by ON DELETE RESTRICT")

	// ListRelationships returns the stored relationship.
	rels, err := app.ListRelationships()
	require.NoError(t, err)
	require.Len(t, rels, 1)
	assert.Equal(t, "orders", rels[0].ChildTable)
	assert.Equal(t, "customer_id", rels[0].ChildColumn)
	assert.Equal(t, "customers", rels[0].ParentTable)
	assert.Equal(t, "id", rels[0].ParentColumn)

	// Relationship metadata survives Save + reopen.
	reopenApp(t, app)
	rels, err = app.ListRelationships()
	require.NoError(t, err)
	require.Len(t, rels, 1, "relationship must persist across save + reopen")
	persistedID := rels[0].ID

	// RemoveForeignKey drops the constraint and the metadata row, allowing the
	// previously-rejected orphan insert to succeed.
	require.NoError(t, app.RemoveForeignKey(persistedID))

	rels, err = app.ListRelationships()
	require.NoError(t, err)
	require.Empty(t, rels, "relationship metadata row must be removed")

	_, err = app.InsertRow("orders", map[string]any{"id": 12, "customer_id": 999, "total": 1})
	require.NoError(t, err, "orphan insert must succeed once the FK constraint is dropped")
}

// TestAppAddForeignKeyValidation verifies AddForeignKey rejects invalid or
// nonexistent tables/columns and non-unique referenced columns with wrapped
// errors BEFORE mutating anything (no metadata row, no constraint).
func TestAppAddForeignKeyValidation(t *testing.T) {
	app := newOpenApp(t)
	seedCustomersOrders(t, app)

	// A non-unique referenced (parent) column must be rejected: customers.name
	// is not unique/PK.
	_, err := app.AddForeignKey(ForeignKeySpec{
		ChildTable:   "orders",
		ChildColumn:  "customer_id",
		ParentTable:  "customers",
		ParentColumn: "name",
		OnDelete:     "RESTRICT",
	})
	require.Error(t, err, "referenced column must be unique/PK")

	// Nonexistent parent table.
	_, err = app.AddForeignKey(ForeignKeySpec{
		ChildTable:   "orders",
		ChildColumn:  "customer_id",
		ParentTable:  "no_such_table",
		ParentColumn: "id",
	})
	require.Error(t, err, "nonexistent parent table must be rejected")

	// Nonexistent child column.
	_, err = app.AddForeignKey(ForeignKeySpec{
		ChildTable:   "orders",
		ChildColumn:  "no_such_col",
		ParentTable:  "customers",
		ParentColumn: "id",
	})
	require.Error(t, err, "nonexistent child column must be rejected")

	// Invalid (injection-y) identifier.
	_, err = app.AddForeignKey(ForeignKeySpec{
		ChildTable:   "orders; DROP TABLE customers;--",
		ChildColumn:  "customer_id",
		ParentTable:  "customers",
		ParentColumn: "id",
	})
	require.Error(t, err, "invalid identifier must be rejected")

	// No metadata rows were written by any of the rejected calls.
	rels, err := app.ListRelationships()
	require.NoError(t, err)
	assert.Empty(t, rels, "no relationship row should be written when validation fails")
}

// TestAppForeignKeyNoFileOpen verifies the no-file-open guards mirror the other
// bindings (mutex-guarded, clear wrapped error).
func TestAppForeignKeyNoFileOpen(t *testing.T) {
	app := NewApp(testLogger())
	app.startup(context.Background())

	_, err := app.AddForeignKey(ForeignKeySpec{
		ChildTable:   "orders",
		ChildColumn:  "customer_id",
		ParentTable:  "customers",
		ParentColumn: "id",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	_, err = app.ListRelationships()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")

	err = app.RemoveForeignKey("some-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no file open")
}
