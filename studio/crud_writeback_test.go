package main

// TDD RED — Phase 1 / unit P1.2: "Transactional CRUD backend with type
// coercion and validation" (parity plan §1.1).
//
// This is a REAL integration test: it opens a real encrypted .vibe container,
// materializes DuckDB, creates typed tables + _vibe_field_meta rules, and
// drives the production Wails-bound write path (InsertRow/UpdateRow/DeleteRow
// + the atomic multi-statement form submit). No mocks.
//
// It asserts the acceptance behavior end-to-end:
//   - string inputs are coerced to column types via information_schema before write
//   - required + validation_rule from _vibe_field_meta are enforced server-side,
//     returning a FIELD-KEYED structured error (not a raw SQL string)
//   - UpdateRow surfaces a 0-affected-row PK mismatch as an error
//   - DeleteRow runs transactionally, coerces the PK, validates the pk column
//     identifier, and surfaces a 0-affected-row PK mismatch as a *WriteError
//   - a failed child statement rolls back the whole logical submit (atomicity),
//     leaving no orphaned parent row
//   - FK behavior follows the P1.0 rely-on-duckdb-fk decision: an orphan child
//     insert is rejected (RESTRICT) and rolls the parent back as well

import (
	"errors"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/schema"
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

	// qty is required and must satisfy qty > 0 (Access-style validation rule).
	require.NoError(t, app.SetFieldMeta("orders", "qty", schema.FieldMeta{
		Caption:        "Quantity",
		Required:       true,
		ValidationRule: "qty > 0",
		ValidationMsg:  "Quantity must be positive",
	}))

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

// TestCRUDAtomicSubmitRollback verifies that a multi-statement form+subform
// submit is atomic: when the child statement fails, the whole submit rolls
// back and the parent is NOT left orphaned.
func TestCRUDAtomicSubmitRollback(t *testing.T) {
	app := newOpenApp(t)
	createOrdersTable(t, app)

	ddb := app.container.DuckDBDB()
	// A child table whose insert will fail validation, forcing rollback.
	_, err := ddb.Exec(`CREATE TABLE order_items (
		id INTEGER PRIMARY KEY,
		order_id INTEGER,
		qty INTEGER
	);`)
	require.NoError(t, err)

	// Child qty must be > 0; we will submit a child with qty = 0 to force failure.
	require.NoError(t, app.SetFieldMeta("order_items", "qty", schema.FieldMeta{
		Required:       true,
		ValidationRule: "qty > 0",
	}))

	parent := FormSubmitRow{
		Table:  "orders",
		Values: map[string]any{"id": "100", "qty": "2"},
	}
	children := []FormSubmitRow{
		{Table: "order_items", Values: map[string]any{"id": "1", "order_id": "100", "qty": "0"}},
	}

	err = app.SubmitForm(parent, children)
	require.Error(t, err, "child validation failure must fail the whole submit")

	// Atomicity: the parent order must NOT have been committed.
	var parentCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE id = 100;`).Scan(&parentCount))
	assert.Equal(t, 0, parentCount, "parent must be rolled back — no orphan left behind")

	var childCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM order_items;`).Scan(&childCount))
	assert.Equal(t, 0, childCount, "no child rows committed")
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
// through the production CRUD write path: a REFERENCES constraint must reject
// an orphan child (RESTRICT) and, within an atomic SubmitForm, roll the parent
// back when the FK-violating child insert fails — true referential-integrity
// atomicity, not merely validation-rule rollback.
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

	// (a) Orphan child via InsertRow: customer_id=42 has no parent -> FK reject.
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

	// (b) Atomic submit: a valid parent customer + an FK-violating child invoice
	// (pointing at a non-existent customer) must roll BOTH back — referential
	// integrity, not a validation rule, triggers the rollback.
	parent := FormSubmitRow{
		Table:  "customers",
		Values: map[string]any{"id": "100", "name": "Acme"},
	}
	children := []FormSubmitRow{
		{Table: "invoices", Values: map[string]any{"id": "2", "customer_id": "999", "amount": "5.00"}},
	}
	err = app.SubmitForm(parent, children)
	require.Error(t, err, "FK-violating child must fail the whole submit")

	var custCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = 100;`).Scan(&custCount))
	assert.Equal(t, 0, custCount, "parent must be rolled back when the FK child fails")

	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM invoices;`).Scan(&invCount))
	assert.Equal(t, 0, invCount, "no invoice committed")
}
