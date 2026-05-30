package main

// TDD RED — Phase 1 / unit P1.6: "_vibe_forms definitions + master-detail
// subform + atomic header/lines submit" (parity plan §1.5/§1.6).
//
// This is a REAL integration test: it opens a real encrypted .vibe container
// (SQLCipher + DuckDB), exercises the production Wails-bound form-definition
// methods (SaveForm/GetForm/ListForms), proves the spec persists as versioned
// JSON across Save+Close+reopen, and drives a master-detail (header + line
// items) submit through the transactional CRUD path. No mocks.
//
// Acceptance behavior asserted:
//   - SaveForm stores a master_detail FormSpec in _vibe_forms; GetForm
//     round-trips it; ListForms includes it
//   - the spec persists across Save() + Close() + reopen (versioned JSON)
//   - a master-detail header+2-lines submit commits all three rows atomically
//   - a forced failure on the 2nd line rolls back the header AND the 1st line

import (
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/queries"
	"github.com/sandwich-labs/vibe-app/internal/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createOrderEntrySchema builds the canonical order-entry relational schema used
// by the master-detail form: orders(id PK, customer_id, order_date) and
// order_items(id PK, order_id FK->orders.id, product, qty, price).
func createOrderEntrySchema(t *testing.T, app *App) {
	t.Helper()
	ddb := app.container.DuckDBDB()
	require.NotNil(t, ddb)
	_, err := ddb.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER,
		order_date DATE
	);`)
	require.NoError(t, err, "create orders header table")
	_, err = ddb.Exec(`CREATE TABLE order_items (
		id INTEGER PRIMARY KEY,
		order_id INTEGER REFERENCES orders(id),
		product VARCHAR,
		qty INTEGER,
		price DECIMAL(10,2)
	);`)
	require.NoError(t, err, "create order_items child table")
}

// orderEntryFormSpec is the master-detail form definition under test.
func orderEntryFormSpec() schema.FormSpec {
	return schema.FormSpec{
		ID:        "order_entry",
		Title:     "Order Entry",
		Table:     "orders",
		Archetype: "master_detail",
		OnSubmit:  "refresh",
		Fields: []schema.FormFieldSpec{
			{Name: "id", Caption: "Order #", ControlType: "text"},
			{Name: "customer_id", Caption: "Customer", ControlType: "combo"},
			{Name: "order_date", Caption: "Date", ControlType: "date"},
		},
		Subform: &schema.SubformSpec{
			ChildTable: "order_items",
			// link: child.order_id = master.id
			ChildKey:  "order_id",
			ParentKey: "id",
			Fields: []schema.FormFieldSpec{
				{Name: "product", Caption: "Product", ControlType: "text"},
				{Name: "qty", Caption: "Qty", ControlType: "number"},
				{Name: "price", Caption: "Price", ControlType: "currency"},
			},
		},
	}
}

// TestMasterDetailSubform is the headline P1.6 acceptance test.
func TestMasterDetailSubform(t *testing.T) {
	app := newOpenApp(t)
	createOrderEntrySchema(t, app)

	spec := orderEntryFormSpec()

	// --- SaveForm + GetForm round-trip ---
	require.NoError(t, app.SaveForm(spec), "SaveForm must persist a master_detail form spec")

	got, err := app.GetForm("order_entry")
	require.NoError(t, err, "GetForm must read back the saved spec")
	require.NotNil(t, got)
	assert.Equal(t, "orders", got.Table)
	assert.Equal(t, "master_detail", got.Archetype)
	require.NotNil(t, got.Subform, "round-tripped spec must retain the subform link")
	assert.Equal(t, "order_items", got.Subform.ChildTable)
	assert.Equal(t, "order_id", got.Subform.ChildKey)
	assert.Equal(t, "id", got.Subform.ParentKey)
	require.Len(t, got.Fields, 3, "round-tripped spec must retain its bound controls")
	require.Len(t, got.Subform.Fields, 3, "round-tripped subform must retain its bound controls")

	// --- ListForms includes it ---
	forms, err := app.ListForms()
	require.NoError(t, err)
	var found bool
	for _, f := range forms {
		if f.ID == "order_entry" {
			found = true
			assert.Equal(t, "Order Entry", f.Title)
			assert.Equal(t, "master_detail", f.Archetype)
		}
	}
	assert.True(t, found, "ListForms must include the saved master-detail form")

	// --- persistence across Save() + Close() + reopen ---
	require.NoError(t, app.SaveFile(), "checkpoint DuckDB + re-serialize container")
	require.NoError(t, app.container.Close())

	reopened, err := container.Open(app.filePath,
		container.WithPassphrase(testPassphrase),
		container.WithLogger(testLogger()),
	)
	require.NoError(t, err, "reopen the persisted .vibe")
	t.Cleanup(func() { _ = reopened.Close() })

	app.container = reopened
	app.engine = engine.NewManager(reopened.DuckDBDB(), engine.WithLogger(testLogger()))
	app.registry = queries.NewRegistry(reopened.SQLiteDB(), testLogger())

	persisted, err := app.GetForm("order_entry")
	require.NoError(t, err, "form spec must survive a reopen (stored as versioned JSON in _vibe_forms)")
	require.NotNil(t, persisted)
	require.NotNil(t, persisted.Subform, "subform link must survive the reopen")
	assert.Equal(t, "order_items", persisted.Subform.ChildTable)
	assert.Equal(t, "order_id", persisted.Subform.ChildKey)
}

// TestMasterDetailAtomicSubmit drives a header + 2 line items through the
// transactional CRUD path and asserts all three rows commit atomically; then a
// forced failure on the 2nd line must roll the whole submit back.
func TestMasterDetailAtomicSubmit(t *testing.T) {
	app := newOpenApp(t)
	createOrderEntrySchema(t, app)
	ddb := app.container.DuckDBDB()

	parent := FormSubmitRow{
		Table:  "orders",
		Values: map[string]any{"id": "500", "customer_id": "1", "order_date": "2026-05-30"},
	}
	children := []FormSubmitRow{
		{Table: "order_items", Values: map[string]any{"id": "1", "order_id": "500", "product": "Widget", "qty": "3", "price": "9.99"}},
		{Table: "order_items", Values: map[string]any{"id": "2", "order_id": "500", "product": "Gadget", "qty": "1", "price": "19.99"}},
	}

	// --- happy path: header + 2 lines all commit atomically ---
	require.NoError(t, app.SubmitForm(parent, children),
		"master-detail header + line items must commit through the transactional CRUD path")

	var orderCount, itemCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE id = 500;`).Scan(&orderCount))
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM order_items WHERE order_id = 500;`).Scan(&itemCount))
	assert.Equal(t, 1, orderCount, "header committed")
	assert.Equal(t, 2, itemCount, "both line items committed")

	// --- forced failure on the 2nd line rolls back the whole submit ---
	parent2 := FormSubmitRow{
		Table:  "orders",
		Values: map[string]any{"id": "600", "customer_id": "2", "order_date": "2026-05-31"},
	}
	children2 := []FormSubmitRow{
		{Table: "order_items", Values: map[string]any{"id": "10", "order_id": "600", "product": "Good", "qty": "1", "price": "1.00"}},
		// Duplicate PK id=10 forces the 2nd line insert to fail.
		{Table: "order_items", Values: map[string]any{"id": "10", "order_id": "600", "product": "Bad", "qty": "1", "price": "1.00"}},
	}
	err := app.SubmitForm(parent2, children2)
	require.Error(t, err, "a failure on the 2nd line must fail the whole submit")

	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE id = 600;`).Scan(&orderCount))
	assert.Equal(t, 0, orderCount, "header must be rolled back — no orphan order 600")
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM order_items WHERE order_id = 600;`).Scan(&itemCount))
	assert.Equal(t, 0, itemCount, "neither line item may be committed when the 2nd line fails")
}
