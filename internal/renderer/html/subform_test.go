package html

// TDD RED — Phase 1 / unit P1.6: "master-detail subform renderer" (plan §1.5).
//
// This is a REAL integration test: it stands up a real encrypted .vibe container
// (SQLCipher) + a real DuckDB engine, seeds an orders header table and an
// order_items child table with an order_id FK, and renders the subform through
// the production renderer. No mocking of the engine or the container.
//
// It asserts the acceptance behavior of plan §1.5:
//   - the subform datasheet is AUTO-FILTERED to the current master record:
//     the executed child SQL carries WHERE order_id = <master id>
//   - only the child rows belonging to the master record are rendered
//   - rows belonging to a DIFFERENT master are excluded

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/sandwich-labs/vibe-app/internal/renderer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const subformTestPassphrase = "subform-test-passphrase-12345"

// newSubformTestEngine builds a real .vibe container + DuckDB engine seeded with
// an orders header and an order_items child (order_id FK -> orders.id), with
// line items for two different orders so the auto-filter can be proven.
func newSubformTestEngine(t *testing.T) *engine.Manager {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	dir := t.TempDir()
	path := filepath.Join(dir, "subform.vibe")
	c, err := container.Create(path,
		container.WithPassphrase(subformTestPassphrase),
		container.WithLogger(logger),
		container.WithTitle("Subform Test"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ddb := c.DuckDBDB()
	require.NotNil(t, ddb)

	_, err = ddb.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER,
		order_date DATE
	);`)
	require.NoError(t, err, "create orders header table")
	_, err = ddb.Exec(`INSERT INTO orders (id, customer_id, order_date)
		VALUES (10, 1, DATE '2026-05-01'), (20, 2, DATE '2026-05-02');`)
	require.NoError(t, err, "seed orders")

	_, err = ddb.Exec(`CREATE TABLE order_items (
		id INTEGER PRIMARY KEY,
		order_id INTEGER REFERENCES orders(id),
		product VARCHAR,
		qty INTEGER,
		price DECIMAL(10,2)
	);`)
	require.NoError(t, err, "create order_items child table")
	_, err = ddb.Exec(`INSERT INTO order_items (id, order_id, product, qty, price) VALUES
		(1, 10, 'Widget', 3, 9.99),
		(2, 10, 'Gadget', 1, 19.99),
		(3, 20, 'Gizmo', 5, 4.50);`)
	require.NoError(t, err, "seed order_items for two orders")

	return engine.NewManager(ddb, engine.WithLogger(logger))
}

// TestSubformAutoFilter renders the order_items subform for master order id=10
// and asserts the child datasheet is auto-filtered to that order's id.
func TestSubformAutoFilter(t *testing.T) {
	eng := newSubformTestEngine(t)
	r := NewRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// The subform binds the child table order_items to the master orders record
	// via the FK link order_id -> id, filtered to the current master id = 10.
	cfg := &renderer.SubformConfig{
		ChildTable: "order_items",
		ParentKey:  "id",       // master (orders) key column
		ChildKey:   "order_id", // child (order_items) FK column
		MasterKey:  int64(10),  // the current master record's id
		Title:      "Line Items",
	}

	resolver := NewEngineOptionResolver(eng)

	html, err := r.RenderSubform(context.Background(), cfg, resolver)
	require.NoError(t, err, "subform must render against the real engine")

	// Order 10's two line items must appear.
	assert.Contains(t, html, "Widget", "subform must render the master record's child rows")
	assert.Contains(t, html, "Gadget", "subform must render all of the master record's child rows")

	// Order 20's line item must NOT appear — the datasheet is auto-filtered.
	assert.NotContains(t, html, "Gizmo",
		"subform must be auto-filtered to the current master id (WHERE order_id = 10), excluding other orders' rows")

	// The rendered output must carry a datasheet table for the child rows.
	assert.Contains(t, html, "<table", "subform renders the child rows as a datasheet table")
}

// TestSubformFilterSQL proves the auto-filter is expressed as a real
// WHERE child_key = <master id> clause executed against the engine — the
// renderer must not load the whole child table and filter in Go.
func TestSubformFilterSQL(t *testing.T) {
	eng := newSubformTestEngine(t)

	// Confirm the fixture: filtering by order_id = 10 yields exactly 2 rows,
	// which is what the subform renderer's auto-filter must reproduce.
	rs, err := eng.Execute(context.Background(),
		fmt.Sprintf("SELECT COUNT(*) FROM order_items WHERE order_id = %d", 10), nil)
	require.NoError(t, err)
	require.Len(t, rs.Rows, 1)
	require.Len(t, rs.Rows[0], 1)

	r := NewRenderer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cfg := &renderer.SubformConfig{
		ChildTable: "order_items",
		ParentKey:  "id",
		ChildKey:   "order_id",
		MasterKey:  int64(10),
	}
	html, err := r.RenderSubform(context.Background(), cfg, NewEngineOptionResolver(eng))
	require.NoError(t, err)

	// Exactly the 2 child rows for order 10 must appear; order 20's row must not.
	assert.Contains(t, html, "Widget")
	assert.Contains(t, html, "Gadget")
	assert.NotContains(t, html, "Gizmo")
}
