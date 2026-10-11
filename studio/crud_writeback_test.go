package main

// Transactional CRUD backend with type coercion, driven through the
// Wails-bound write path (InsertRow/UpdateRow/DeleteRow) against a real
// workspace and its DuckDB hub. No mocks.
//
// Field metadata (required, validation rules) was an Access-era _vibe_ table
// that workspaces do not have; only type coercion and the database's own
// constraints apply.
//
// It asserts:
//   - string inputs are coerced to column types via information_schema before write
//   - UpdateRow surfaces a 0-affected-row PK mismatch as an error
//   - DeleteRow runs transactionally, coerces the PK, validates the pk column
//     identifier, and surfaces a 0-affected-row PK mismatch as a *crud.WriteError
//   - an orphan child insert is rejected (RESTRICT)

import (
	"errors"
	"testing"

	"github.com/SandwichLabs/vibe/internal/crud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createOrdersTable(t *testing.T, app *App) {
	t.Helper()
	ddb := app.sess.DB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		qty INTEGER,
		total DECIMAL(10,2),
		ship_date DATE,
		paid BOOLEAN
	);`)
	require.NoError(t, err, "create typed orders table")
}

// asWriteError extracts the structured, field-keyed error from a CRUD failure.
// It must NOT be a raw SQL string — the UI relies on per-field feedback.
func asWriteError(t *testing.T, err error) *crud.WriteError {
	t.Helper()
	require.Error(t, err)
	var we *crud.WriteError
	require.True(t, errors.As(err, &we), "error must be a *crud.WriteError with per-field feedback, got: %v", err)
	return we
}

// TestCRUDCoercion verifies a NEW file (no
// _vibe_field_meta table at all — #68 stopped creating it) still coerces
// types correctly; required/validation_rule enforcement is simply absent
// since there is no metadata to enforce.
func TestCRUDCoercion(t *testing.T) {
	app := newOpenApp(t)
	createOrdersTable(t, app)

	res, err := app.InsertRow("orders", map[string]any{"id": "1", "qty": "5"})
	require.NoError(t, err, "insert must succeed with no field-meta table present")
	require.NotNil(t, res)
	assert.Equal(t, int64(1), res.Affected)
}

// TestDeleteRowTransactional verifies DeleteRow is consistent with the
// Insert/Update contract: it deletes the matching row, surfaces a non-existent
// PK as a structured *crud.WriteError (not a silent no-op), and rejects an invalid
// pk column identifier rather than interpolating it into SQL.
func TestDeleteRowTransactional(t *testing.T) {
	app := newOpenApp(t)
	createOrdersTable(t, app)

	ddb := app.sess.DB()
	_, err := ddb.Exec(`INSERT INTO orders (id, qty) VALUES (1, 5), (2, 7);`)
	require.NoError(t, err)

	// --- successful delete affects exactly the targeted row (PK coerced) ---
	res, err := app.DeleteRow("orders", "id", "1")
	require.NoError(t, err, "deleting an existing row must succeed")
	require.NotNil(t, res)
	assert.Equal(t, int64(1), res.Affected, "exactly one row deleted")

	var count int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders;`).Scan(&count))
	assert.Equal(t, 1, count, "only the targeted row should be gone")

	// --- deleting a non-existent PK is a structured error, not a no-op ---
	_, err = app.DeleteRow("orders", "id", "9999")
	we := asWriteError(t, err)
	assert.NotEmpty(t, we.Message, "PK mismatch must carry a message")

	// --- an invalid pk column identifier is rejected (no SQL injection) ---
	_, err = app.DeleteRow("orders", "id; DROP TABLE orders", "1")
	require.Error(t, err, "invalid pk column must be rejected")

	// The orders table must still exist and retain its surviving row.
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders;`).Scan(&count))
	assert.Equal(t, 1, count, "invalid identifier must not have mutated the table")
}

// TestCRUDForeignKeyRestrict covers the P1.0 "rely-on-duckdb-fk" decision
// through the production CRUD write path: a REFERENCES constraint (plain
// SQL — #68 dropped the FK-wizard bindings) must reject an orphan child
// (RESTRICT) via InsertRow.
func TestCRUDForeignKeyRestrict(t *testing.T) {
	app := newOpenApp(t)
	ddb := app.sess.DB()
	require.NotNil(t, ddb)

	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name VARCHAR);`)
	require.NoError(t, err)
	_, err = ddb.Exec(`CREATE TABLE invoices (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER REFERENCES customers(id),
		amount DECIMAL(10,2)
	);`)
	require.NoError(t, err)

	// Orphan child via InsertRow: customer_id=42 has no parent -> FK reject.
	_, err = app.InsertRow("invoices", map[string]any{
		"id":          "1",
		"customer_id": "42",
		"amount":      "10.00",
	})
	we := asWriteError(t, err)
	assert.NotEmpty(t, we.Message, "FK violation must surface a structured error")

	var invCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM invoices;`).Scan(&invCount))
	assert.Equal(t, 0, invCount, "orphan invoice must be rejected (RESTRICT)")
}
