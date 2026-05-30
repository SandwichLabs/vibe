package engine_test

// P2.1: DuckDB relationship/FK risk spike (Phase-2 patterns).
//
// Phase-2-scoped extension of the P1.0 CRUD spike. P1.0 (crud_spike.go) proved
// per-row UPDATE/DELETE and inline CREATE-TABLE ... REFERENCES with RESTRICT on
// parent-delete, concluding "rely-on-duckdb-fk". Phase 2 introduces relationship
// patterns P1.0 did NOT cover and which are known DuckDB weak spots:
//
//   (a) adding a FK to an ALREADY-EXISTING table via
//       ALTER TABLE ... ADD ... REFERENCES (historically unsupported by DuckDB),
//   (b) ON DELETE CASCADE / ON UPDATE behavior (cascade options the relationship
//       UI exposes),
//   (c) the full 3-level customers -> orders -> order_items chain with enforced
//       integrity (orphan inserts rejected, RESTRICT parent-delete errors),
//   (d) self-referential FK (employees.manager_id -> employees.id).
//
// This is a REAL integration test: it opens a real encrypted .vibe container,
// materializes DuckDB, and exercises the actual container.DuckDBDB().Exec path.
// No mocks. It establishes WHICH relationship/cascade features can lean on
// DuckDB-native constraints vs. must be enforced in the application CRUD layer,
// and records a concrete verdict (engine.RelationshipSpikeVerdict()) for P2.2.

import (
	"path/filepath"
	"testing"

	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDuckDBRelationshipSpike(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rel-spike.vibe")

	c := openSpikeContainer(t, path)
	ddb := c.DuckDBDB()

	// === (1) ALTER TABLE ... ADD FOREIGN KEY on an already-existing table ===
	// Create the three tables as PLAIN tables first (no inline FK), then try to
	// add a FK after the fact. DuckDB has historically NOT supported this.
	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);`)
	require.NoError(t, err, "create customers (plain)")
	_, err = ddb.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER, total DECIMAL(10,2));`)
	require.NoError(t, err, "create orders (plain)")

	_, alterErr := ddb.Exec(`ALTER TABLE orders ADD FOREIGN KEY (customer_id) REFERENCES customers(id);`)
	alterAddFKSupported := alterErr == nil
	t.Logf("SPIKE FINDING: ALTER TABLE ADD FOREIGN KEY supported = %v (err=%v)", alterAddFKSupported, alterErr)

	// === (2) ON DELETE CASCADE via inline REFERENCES ===
	// Drop and recreate orders with an inline ON DELETE CASCADE FK, then verify
	// whether deleting a parent actually cascades to children.
	_, err = ddb.Exec(`DROP TABLE IF EXISTS orders;`)
	require.NoError(t, err, "drop orders before cascade recreate")

	onDeleteCascadeSupported := false
	cascadeRecreated := false
	_, cascadeDDLErr := ddb.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER REFERENCES customers(id) ON DELETE CASCADE,
		total DECIMAL(10,2)
	);`)
	t.Logf("SPIKE FINDING: CREATE TABLE with ON DELETE CASCADE DDL err = %v", cascadeDDLErr)
	if cascadeDDLErr == nil {
		cascadeRecreated = true
		_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Alice') ON CONFLICT DO NOTHING;`)
		require.NoError(t, err, "seed customer for cascade test")
		_, err = ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (10, 1, 99.50);`)
		require.NoError(t, err, "seed order for cascade test")

		_, cascadeDelErr := ddb.Exec(`DELETE FROM customers WHERE id = 1;`)
		if cascadeDelErr == nil {
			var remaining int
			require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE customer_id = 1;`).Scan(&remaining))
			onDeleteCascadeSupported = remaining == 0
			t.Logf("SPIKE FINDING: ON DELETE CASCADE removed children = %v (remaining=%d)", onDeleteCascadeSupported, remaining)
		} else {
			// Parent delete errored despite ON DELETE CASCADE clause => cascade
			// not honored (treated as RESTRICT).
			t.Logf("SPIKE FINDING: parent delete with ON DELETE CASCADE clause errored = %v", cascadeDelErr)
		}
	}

	// === (3) Full 3-level chain with enforced integrity ===
	// Rebuild a clean inline-FK chain: customers -> orders -> order_items.
	// (Use inline REFERENCES since that is the pattern P1.0 proved works.)
	_, err = ddb.Exec(`DROP TABLE IF EXISTS order_items;`)
	require.NoError(t, err)
	_, err = ddb.Exec(`DROP TABLE IF EXISTS orders;`)
	require.NoError(t, err)
	_, err = ddb.Exec(`DROP TABLE IF EXISTS customers;`)
	require.NoError(t, err)

	_, err = ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);`)
	require.NoError(t, err, "create customers (chain)")
	_, err = ddb.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER REFERENCES customers(id),
		total DECIMAL(10,2)
	);`)
	require.NoError(t, err, "create orders (chain)")
	_, err = ddb.Exec(`CREATE TABLE order_items (
		id INTEGER PRIMARY KEY,
		order_id INTEGER REFERENCES orders(id),
		sku TEXT
	);`)
	require.NoError(t, err, "create order_items (chain)")

	_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Alice');`)
	require.NoError(t, err, "insert customer")
	_, err = ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (10, 1, 99.50);`)
	require.NoError(t, err, "insert order")
	_, err = ddb.Exec(`INSERT INTO order_items (id, order_id, sku) VALUES (100, 10, 'WIDGET');`)
	require.NoError(t, err, "insert order_item")

	// Orphan inserts at each child level must be rejected.
	_, orphanOrderErr := ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (11, 4242, 1.00);`)
	assert.Error(t, orphanOrderErr, "orphan order (non-existent customer) must be rejected")
	_, orphanItemErr := ddb.Exec(`INSERT INTO order_items (id, order_id, sku) VALUES (101, 4242, 'NOPE');`)
	assert.Error(t, orphanItemErr, "orphan order_item (non-existent order) must be rejected")

	// RESTRICT parent-delete: deleting a referenced parent must error.
	_, restrictCustErr := ddb.Exec(`DELETE FROM customers WHERE id = 1;`)
	assert.Error(t, restrictCustErr, "deleting referenced customer must be RESTRICTed")
	_, restrictOrderErr := ddb.Exec(`DELETE FROM orders WHERE id = 10;`)
	assert.Error(t, restrictOrderErr, "deleting referenced order must be RESTRICTed")

	// === (4) Self-referential FK ===
	_, err = ddb.Exec(`CREATE TABLE employees (
		id INTEGER PRIMARY KEY,
		name TEXT,
		manager_id INTEGER REFERENCES employees(id)
	);`)
	require.NoError(t, err, "create self-referential employees table")
	_, err = ddb.Exec(`INSERT INTO employees (id, name, manager_id) VALUES (1, 'Boss', NULL);`)
	require.NoError(t, err, "insert root employee (NULL manager)")
	_, err = ddb.Exec(`INSERT INTO employees (id, name, manager_id) VALUES (2, 'Report', 1);`)
	require.NoError(t, err, "insert employee referencing existing manager")
	_, selfOrphanErr := ddb.Exec(`INSERT INTO employees (id, name, manager_id) VALUES (3, 'Bad', 9999);`)
	assert.Error(t, selfOrphanErr, "self-referential orphan (non-existent manager) must be rejected")

	// === (5) Constraints survive Save -> Close -> reopen ===
	require.NoError(t, c.Save(), "save checkpoints DuckDB into the encrypted blob")
	require.NoError(t, c.Close(), "close container")

	c2 := reopenSpikeContainer(t, path)
	defer c2.Close() //nolint:errcheck
	ddb2 := c2.DuckDBDB()

	// After reopen, FK constraints must still be enforced: orphan insert rejected.
	_, reopenOrphanErr := ddb2.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (12, 7777, 1.00);`)
	assert.Error(t, reopenOrphanErr, "FK constraint must persist + still reject orphans after reopen")
	// And RESTRICT parent-delete still enforced.
	_, reopenRestrictErr := ddb2.Exec(`DELETE FROM customers WHERE id = 1;`)
	assert.Error(t, reopenRestrictErr, "RESTRICT parent-delete must persist after reopen")

	// === Recorded verdict for P2.2 ===
	// engine.RelationshipSpikeVerdict() must agree with the runtime observations
	// above AND carry a concrete recommendation. The stub returns the zero value,
	// so these assertions fail until the spike findings are actually recorded.
	verdict := engine.RelationshipSpikeVerdict()

	assert.Equal(t, alterAddFKSupported, verdict.AlterAddFKSupported,
		"recorded verdict must match observed ALTER ADD FOREIGN KEY support")
	assert.Equal(t, onDeleteCascadeSupported, verdict.OnDeleteCascadeSupported,
		"recorded verdict must match observed ON DELETE CASCADE support")
	assert.Contains(t,
		[]string{
			engine.RelRecommendRelyOnDuckDB,
			engine.RelRecommendRecreateForFK,
			engine.RelRecommendEnforceInCRUD,
		},
		verdict.Recommendation,
		"spike must record a concrete relationship-strategy recommendation for P2.2")

	// Recommendation must be self-consistent with observed behavior.
	if !alterAddFKSupported {
		// If you cannot ALTER ADD a FK to an existing table, P2.2 must NOT silently
		// assume it can; it must recreate-the-table-with-FK or enforce in the CRUD
		// layer.
		assert.Contains(t,
			[]string{engine.RelRecommendRecreateForFK, engine.RelRecommendEnforceInCRUD},
			verdict.Recommendation,
			"when ALTER ADD FK is unsupported, verdict must direct P2.2 to recreate-for-FK or CRUD-layer enforcement")
	}

	// Note: cascadeRecreated documents whether the ON DELETE CASCADE DDL parsed at
	// all; assert it is observed (recorded for the doc comment), not required.
	t.Logf("SPIKE FINDING: ON DELETE CASCADE DDL accepted = %v", cascadeRecreated)
}
