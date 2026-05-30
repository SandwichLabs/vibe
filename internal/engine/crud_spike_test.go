package engine_test

// P1.0: DuckDB-as-CRUD risk spike (per-row UPDATE/DELETE + FK/relationship viability).
//
// Mandatory risk spike from the MS Access parity plan (§8/§4.1) before building
// anything on the CRUD path. This is a REAL integration test: it opens a real
// encrypted .vibe container, materializes DuckDB, and exercises the actual
// engine.Manager.Execute / container.DuckDBDB().Exec paths. No mocks.
//
// It validates and DOCUMENTS:
//   (a) per-row INSERT/UPDATE/DELETE by PK round-trips correctly,
//   (b) DuckDB FK constraint enforcement semantics (orphan insert + parent delete),
//   (c) changes survive Save (checkpoint -> re-serialize blob) + Close + reopen,
//   (d) rough per-row UPDATE latency over N=1000 rows is acceptable at SMB scale.
//
// CONCLUSION (recorded for P1.6/P2): the spike must populate
// engine.CRUDSpikeVerdict() with a concrete recommendation —
// "rely-on-duckdb-fk" if DuckDB enforces FK constraints (orphan rejected +
// RESTRICT/CASCADE on parent delete), otherwise "enforce-in-crud-layer".

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const spikePassphrase = "spike-passphrase-12345"

// openSpikeContainer creates a throwaway encrypted .vibe with a real DuckDB and
// returns it. Caller is responsible for Close().
func openSpikeContainer(t *testing.T, path string) *container.Container {
	t.Helper()
	c, err := container.Create(path, container.WithPassphrase(spikePassphrase))
	require.NoError(t, err, "should create encrypted .vibe container")
	require.NotNil(t, c.DuckDBDB(), "DuckDB should be materialized")
	return c
}

func reopenSpikeContainer(t *testing.T, path string) *container.Container {
	t.Helper()
	c, err := container.Open(path, container.WithPassphrase(spikePassphrase))
	require.NoError(t, err, "should reopen encrypted .vibe container")
	require.NotNil(t, c.DuckDBDB(), "DuckDB should be materialized on reopen")
	return c
}

func TestDuckDBCRUDSpike(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spike.vibe")

	c := openSpikeContainer(t, path)
	ddb := c.DuckDBDB()
	mgr := engine.NewManager(ddb)
	ctx := context.Background()

	// --- DDL: parent + child with a FOREIGN KEY constraint ---
	_, err := ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);`)
	require.NoError(t, err, "create customers")
	_, err = ddb.Exec(`CREATE TABLE orders (
		id INTEGER PRIMARY KEY,
		customer_id INTEGER REFERENCES customers(id),
		total DECIMAL(10,2)
	);`)
	require.NoError(t, err, "create orders with FK to customers")

	// --- (a) per-row INSERT round-trip ---
	_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (1, 'Alice'), (2, 'Bob');`)
	require.NoError(t, err, "insert parents")
	_, err = ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (10, 1, 99.50), (11, 2, 5.00);`)
	require.NoError(t, err, "insert children")

	// Read back via the engine.Manager.Execute path (the production CRUD read path).
	rs, err := mgr.Execute(ctx, `SELECT total FROM orders WHERE id = $id`, map[string]any{"id": 10})
	require.NoError(t, err, "select via engine manager")
	require.Equal(t, 1, rs.RowCount(), "exactly one order with id=10")

	// --- per-row UPDATE by PK affects exactly 1 row + reads back ---
	res, err := ddb.Exec(`UPDATE orders SET total = ? WHERE id = ?;`, 123.45, 10)
	require.NoError(t, err, "update by pk")
	affected, err := res.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected, "UPDATE by PK must affect exactly 1 row")

	var total float64
	err = ddb.QueryRow(`SELECT total FROM orders WHERE id = 10;`).Scan(&total)
	require.NoError(t, err, "read back updated total")
	assert.InDelta(t, 123.45, total, 0.001, "updated value must persist in-session")

	// --- per-row DELETE by PK removes exactly 1 row ---
	res, err = ddb.Exec(`DELETE FROM orders WHERE id = 11;`)
	require.NoError(t, err, "delete by pk")
	affected, err = res.RowsAffected()
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected, "DELETE by PK must remove exactly 1 row")

	// --- (b) FK enforcement semantics: orphan-child insert ---
	// Record whether DuckDB rejects a child referencing a non-existent parent.
	_, orphanErr := ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (99, 4242, 1.00);`)
	orphanRejected := orphanErr != nil
	t.Logf("SPIKE FINDING: orphan-child insert rejected by DuckDB FK = %v (err=%v)", orphanRejected, orphanErr)

	// --- (b) FK enforcement semantics: delete a referenced parent ---
	// customer 1 is still referenced by order 10. Determine RESTRICT/CASCADE/UNENFORCED.
	var ordersForCust1Before int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE customer_id = 1;`).Scan(&ordersForCust1Before))
	require.Equal(t, 1, ordersForCust1Before, "precondition: customer 1 has 1 referencing order")

	_, parentDelErr := ddb.Exec(`DELETE FROM customers WHERE id = 1;`)
	var observedParentDeleteBehavior string
	if parentDelErr != nil {
		observedParentDeleteBehavior = engine.FKParentDeleteRestrict
	} else {
		var ordersForCust1After int
		require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE customer_id = 1;`).Scan(&ordersForCust1After))
		if ordersForCust1After == 0 {
			observedParentDeleteBehavior = engine.FKParentDeleteCascade
		} else {
			observedParentDeleteBehavior = engine.FKParentDeleteUnenforced
		}
	}
	t.Logf("SPIKE FINDING: parent-delete behavior = %s (err=%v)", observedParentDeleteBehavior, parentDelErr)

	// --- The recorded spike conclusion that P1.6/P2 build on ---
	// engine.CRUDSpikeVerdict() must agree with what we just observed at runtime,
	// AND must carry a concrete recommendation. The stub returns the zero value,
	// so these assertions fail until the spike findings are actually recorded.
	verdict := engine.CRUDSpikeVerdict()

	assert.Equal(t, orphanRejected, verdict.OrphanInsertRejected,
		"recorded verdict must match observed orphan-insert behavior")
	assert.Equal(t, observedParentDeleteBehavior, verdict.ParentDeleteBehavior,
		"recorded verdict must match observed parent-delete behavior")
	assert.Contains(t,
		[]string{engine.FKRecommendRelyOnDuckDB, engine.FKRecommendEnforceInCRUD},
		verdict.Recommendation,
		"spike must record a concrete CRUD-layer recommendation for P1.6/P2")

	// Recommendation must be self-consistent with the observed enforcement.
	fkEnforced := orphanRejected && observedParentDeleteBehavior != engine.FKParentDeleteUnenforced
	if fkEnforced {
		assert.Equal(t, engine.FKRecommendRelyOnDuckDB, verdict.Recommendation,
			"if DuckDB enforces FKs, spike should recommend relying on it")
	} else {
		assert.Equal(t, engine.FKRecommendEnforceInCRUD, verdict.Recommendation,
			"if DuckDB does not fully enforce FKs, spike should recommend enforcing in the CRUD layer")
	}

	// --- (d) per-row UPDATE latency over N=1000 rows ---
	seedSpikePerfRows(t, ddb, 1000)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		_, err := ddb.Exec(`UPDATE orders SET total = ? WHERE id = ?;`, float64(i), 1000+i)
		require.NoError(t, err, "perf-loop update")
	}
	elapsed := time.Since(start)
	t.Logf("SPIKE FINDING: 1000 single-row UPDATEs took %s", elapsed)
	assert.Less(t, elapsed, 5*time.Second,
		"1000 single-row UPDATEs must complete under a generous SMB-scale budget")

	// --- (c) persistence across Save + Close + reopen ---
	require.NoError(t, c.Save(), "save checkpoints DuckDB into the encrypted blob")
	require.NoError(t, c.Close(), "close container")

	c2 := reopenSpikeContainer(t, path)
	defer c2.Close() //nolint:errcheck
	ddb2 := c2.DuckDBDB()

	var reopenedTotal float64
	err = ddb2.QueryRow(`SELECT total FROM orders WHERE id = 10;`).Scan(&reopenedTotal)
	require.NoError(t, err, "order 10 must still exist after reopen")
	assert.InDelta(t, 123.45, reopenedTotal, 0.001, "updated value must persist across Save+Close+reopen")

	var orderCount int
	require.NoError(t, ddb2.QueryRow(`SELECT COUNT(*) FROM orders WHERE id < 1000;`).Scan(&orderCount))
	t.Logf("SPIKE FINDING: %d base orders survived reopen", orderCount)
}

func seedSpikePerfRows(t *testing.T, ddb *sql.DB, n int) {
	t.Helper()
	// Ensure a parent exists for FK validity regardless of enforcement.
	_, _ = ddb.Exec(`INSERT INTO customers (id, name) VALUES (9999, 'Perf') ON CONFLICT DO NOTHING;`)
	for i := 0; i < n; i++ {
		_, err := ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (?, 9999, 0);`, 1000+i)
		require.NoError(t, err, "seed perf row")
	}
}
