package main

// TDD RED — Phase 1 / unit P1.2: "Transactional CRUD backend with type
// coercion and validation" (parity plan §1.1).
//
// This is a REAL integration test: it opens a real encrypted .vibe container,
// materializes DuckDB, creates typed tables, and drives the production
// Wails-bound write path (InsertRow/UpdateRow/DeleteRow). No mocks.
//
// #68 removed the bound-forms layer that used to author _vibe_field_meta
// rows (SetFieldMeta/the Form Wizard); a NEW file never gets a
// _vibe_field_meta table at all any more. The server-side required +
// validation_rule enforcement in internal/crud stays, though, for an OLD
// file that still carries _vibe_field_meta rows from before #68 (additive
// migration: existing tables are never dropped) — seedFieldMeta below
// simulates that by writing rows directly, the way such an old file would
// already have them.
//
// It asserts the acceptance behavior end-to-end:
//   - string inputs are coerced to column types via information_schema before write
//   - required + validation_rule from an old file's _vibe_field_meta are
//     enforced server-side, returning a FIELD-KEYED structured error (not a
//     raw SQL string)
//   - UpdateRow surfaces a 0-affected-row PK mismatch as an error
//   - DeleteRow runs transactionally, coerces the PK, validates the pk column
//     identifier, and surfaces a 0-affected-row PK mismatch as a *WriteError
//   - FK behavior follows the P1.0 rely-on-duckdb-fk decision: an orphan child
//     insert is rejected (RESTRICT)

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createOrdersTable(t *testing.T, app *App) {
	t.Helper()
	ddb := app.container.DuckDBDB()
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

// seedFieldMeta writes a _vibe_field_meta row directly via SQL, simulating an
// OLD file that already carries field metadata from before #68 removed the
// bound-forms layer that used to author it (SetFieldMeta is gone).
func seedFieldMeta(t *testing.T, app *App, table, column, caption string, required bool, rule, msg string) {
	t.Helper()
	db := app.container.SQLiteDB()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _vibe_field_meta (
		table_name         TEXT NOT NULL,
		column_name        TEXT NOT NULL,
		caption            TEXT NOT NULL DEFAULT '',
		control_type       TEXT NOT NULL DEFAULT '',
		default_value      TEXT NOT NULL DEFAULT '',
		required           INTEGER NOT NULL DEFAULT 0,
		format_code        TEXT NOT NULL DEFAULT '',
		validation_rule    TEXT NOT NULL DEFAULT '',
		validation_message TEXT NOT NULL DEFAULT '',
		input_mask         TEXT NOT NULL DEFAULT '',
		lookup_sql         TEXT NOT NULL DEFAULT '',
		display_col        TEXT NOT NULL DEFAULT '',
		value_col          TEXT NOT NULL DEFAULT '',
		control_tip        TEXT NOT NULL DEFAULT '',
		sort_order         INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (table_name, column_name)
	);`)
	require.NoError(t, err)

	req := 0
	if required {
		req = 1
	}
	_, err = db.Exec(
		`INSERT INTO _vibe_field_meta (table_name, column_name, caption, required, validation_rule, validation_message)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		table, column, caption, req, rule, msg,
	)
	require.NoError(t, err)
}

// asWriteError extracts the structured, field-keyed error from a CRUD failure.
// It must NOT be a raw SQL string — the UI relies on per-field feedback.
func asWriteError(t *testing.T, err error) *WriteError {
	t.Helper()
	require.Error(t, err)
	var we *WriteError
	require.True(t, errors.As(err, &we), "error must be a *WriteError with per-field feedback, got: %v", err)
	return we
}

func TestCRUDCoercionAndValidation(t *testing.T) {
	app := newOpenApp(t)
	createOrdersTable(t, app)

	// qty is required and must satisfy qty > 0 (an old file's carried-over
	// field-meta rule; see seedFieldMeta's doc comment).
	seedFieldMeta(t, app, "orders", "qty", "Quantity", true, "qty > 0", "Quantity must be positive")

	// --- (1) coercion: all-string HTTP/Vue values become typed columns ---
	res, err := app.InsertRow("orders", map[string]any{
		"id":        "1",
		"qty":       "5",
		"total":     "19.99",
		"ship_date": "2026-05-30",
		"paid":      "true",
	})
	require.NoError(t, err, "insert with all-string values must coerce and succeed")
	require.NotNil(t, res)
	assert.Equal(t, int64(1), res.Affected, "exactly one row inserted")

	// Read back and assert the values are stored with the correct types.
	ddb := app.container.DuckDBDB()
	var (
		qty       int
		total     float64
		shipDate  string
		paid      bool
		typeofQty string
	)
	require.NoError(t, ddb.QueryRow(`SELECT qty, total, CAST(ship_date AS VARCHAR), paid, typeof(qty) FROM orders WHERE id = 1;`).
		Scan(&qty, &total, &shipDate, &paid, &typeofQty))
	assert.Equal(t, 5, qty, "qty stored as integer")
	assert.InDelta(t, 19.99, total, 0.001, "total stored as decimal")
	assert.Equal(t, "2026-05-30", shipDate, "ship_date stored as date")
	assert.True(t, paid, "paid stored as boolean true")
	assert.NotContains(t, typeofQty, "VARCHAR", "qty must NOT be stored as a string")

	// --- (2) required violation: qty missing -> field-keyed error naming 'qty' ---
	_, err = app.InsertRow("orders", map[string]any{
		"id":    "2",
		"total": "1.00",
	})
	we := asWriteError(t, err)
	require.Contains(t, we.Fields, "qty", "structured error must key the missing required field")

	// --- (3) validation_rule violation: qty = -1 -> field-keyed error naming 'qty' ---
	_, err = app.InsertRow("orders", map[string]any{
		"id":  "3",
		"qty": "-1",
	})
	we = asWriteError(t, err)
	require.Contains(t, we.Fields, "qty", "validation_rule failure must key the offending field")

	// Nothing should have been written for the two failed inserts.
	var count int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders;`).Scan(&count))
	assert.Equal(t, 1, count, "failed inserts must not write rows")

	// --- (4) UpdateRow with a non-matching PK -> 0 affected surfaced as error ---
	_, err = app.UpdateRow("orders", "id", "9999", map[string]any{"qty": "7"})
	require.Error(t, err, "updating a non-existent PK must be an error, not a silent no-op")
}

// TestCRUDCoercionAndValidationNoFieldMeta verifies a NEW file (no
// _vibe_field_meta table at all — #68 stopped creating it) still coerces
// types correctly; required/validation_rule enforcement is simply absent
// since there is no metadata to enforce.
func TestCRUDCoercionAndValidationNoFieldMeta(t *testing.T) {
	app := newOpenApp(t)
	createOrdersTable(t, app)

	res, err := app.InsertRow("orders", map[string]any{"id": "1", "qty": "5"})
	require.NoError(t, err, "insert must succeed with no field-meta table present")
	require.NotNil(t, res)
	assert.Equal(t, int64(1), res.Affected)
}

// TestDeleteRowTransactional verifies DeleteRow is consistent with the
// Insert/Update contract: it deletes the matching row, surfaces a non-existent
// PK as a structured *WriteError (not a silent no-op), and rejects an invalid
// pk column identifier rather than interpolating it into SQL.
func TestDeleteRowTransactional(t *testing.T) {
	app := newOpenApp(t)
	createOrdersTable(t, app)

	ddb := app.container.DuckDBDB()
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
	ddb := app.container.DuckDBDB()
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
