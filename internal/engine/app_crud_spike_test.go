package engine_test

// P4.0: DuckDB-as-CRUD risk spike — per-row UPDATE/DELETE + FK relationship
// pattern on a real .vibe (parity plan §8 / §4.1, gating Phase 4 App Shell /
// Switchboard / dual-mode).
//
// This is the MANDATORY first unit before building further on
// DuckDB-as-transactional-store. Unlike the P1.0 spike (raw ddb.Exec) and the
// P2.1 spike (relationship DDL surface), P4.0 drives the *production* write
// path that App Mode's switchboard/forms actually navigate to:
//
//   - a plain-SQL inline foreign key (REFERENCES) on the child table
//   - the transactional CRUD core: crud.InsertRowTx / UpdateRowTx / DeleteRowTx
//     run inside ddb.Begin() ... tx.Commit() exactly as studio/app.go and
//     internal/server/handlers.go invoke them.
//
// It validates, against a REAL encrypted .vibe + REAL DuckDB (no mocks):
//   (1) N=500 per-row UPDATEs by PK through crud.UpdateRowTx succeed + the
//       batch latency is recorded as an SMB-scale viability signal,
//   (2) 100 per-row DELETEs by PK through crud.DeleteRowTx succeed,
//   (3) an orphan child INSERT (non-existent customer_id) is REJECTED by the FK,
//   (4) deleting a referenced parent is BLOCKED (RESTRICT),
//   (5) all valid writes + the FK constraint survive Save (checkpoint ->
//       re-serialize blob) + Close + reopen.
//
// CONCLUSION (recorded for the later P4.x units): the spike must populate
// engine.AppCRUDViabilitySpikeVerdict() with a concrete go/no-go —
// "app-mode-crud-viable" only if per-row CRUD is durable AND the FK keeps
// enforcing integrity after the blob round-trip; otherwise "blocking-crud-risk"
// (a BLOCKING finding that stops the rest of Phase 4).

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sandwich-labs/vibe-app/internal/container"
	"github.com/sandwich-labs/vibe-app/internal/crud"
	"github.com/sandwich-labs/vibe-app/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const appCRUDPassphrase = "p4-app-crud-spike-12345"

// isFKViolation reports whether err is a genuine DuckDB foreign-key / referential
// integrity rejection (RESTRICT), NOT an unrelated error such as a
// "Table ... does not exist" catalog error (which is exactly the dangling-FK
// dependency defect that previously masqueraded as RESTRICT).
func isFKViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "does not exist") {
		return false
	}
	return strings.Contains(msg, "foreign key") ||
		strings.Contains(msg, "violates foreign key") ||
		(strings.Contains(msg, "constraint") && strings.Contains(msg, "key")) ||
		strings.Contains(msg, "referenced")
}

func TestP4AppModeCRUDViabilitySpike(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p4-app-crud.vibe")

	c, err := container.Create(path, container.WithPassphrase(appCRUDPassphrase))
	require.NoError(t, err, "should create encrypted .vibe container")
	require.NotNil(t, c.DuckDBDB(), "DuckDB should be materialized")

	ddb := c.DuckDBDB()
	_ = engine.NewManager(ddb)
	ctx := context.Background()
	_ = ctx

	// --- DDL: parent (customers, PK) + child (orders), FK declared inline ---
	// (#68 dropped the FK-emit wizard helper; foreign keys are plain SQL now.)
	_, err = ddb.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT);`)
	require.NoError(t, err, "create customers")
	_, err = ddb.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER REFERENCES customers(id), total DOUBLE);`)
	require.NoError(t, err, "create orders")

	// --- seed: 50 customers + 500 orders (each order -> a valid customer) ---
	for i := 1; i <= 50; i++ {
		_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (?, ?);`, i, "cust")
		require.NoError(t, err, "seed customer %d", i)
	}
	for i := 1; i <= 500; i++ {
		custID := (i % 50) + 1
		_, err = ddb.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (?, ?, ?);`, i, custID, 0.0)
		require.NoError(t, err, "seed order %d", i)
	}

	// Field metadata is empty for these tables; the CRUD core tolerates that.
	store := crud.NewFieldMetaStore(c.SQLiteDB())
	orderColTypes, err := crud.ColumnTypes(ddb, "orders")
	require.NoError(t, err, "read orders column types")
	orderMetas, err := crud.FieldMetaMap(store, "orders")
	require.NoError(t, err, "read orders field meta (may be empty)")

	// --- (1) N=500 per-row UPDATEs by PK through the production CRUD core ---
	start := time.Now()
	for i := 1; i <= 500; i++ {
		tx, err := ddb.Begin()
		require.NoError(t, err, "begin tx for update %d", i)
		_, werr := crud.UpdateRowTx(tx, orderColTypes, orderMetas, "orders", "id", i,
			map[string]any{"total": float64(i)})
		require.NoError(t, werr, "crud.UpdateRowTx by PK should succeed for order %d", i)
		require.NoError(t, tx.Commit(), "commit update %d", i)
	}
	batchElapsed := time.Since(start)
	t.Logf("P4.0 VIABILITY SIGNAL: 500 per-row UPDATEs through crud.UpdateRowTx took %s", batchElapsed)

	// Spot-check a few updated values persisted in-session.
	var total float64
	require.NoError(t, ddb.QueryRow(`SELECT total FROM orders WHERE id = 250;`).Scan(&total))
	assert.InDelta(t, 250.0, total, 0.001, "per-row UPDATE must persist in-session")

	// --- (2) 100 per-row DELETEs by PK through the production CRUD core ---
	for i := 401; i <= 500; i++ {
		tx, err := ddb.Begin()
		require.NoError(t, err, "begin tx for delete %d", i)
		_, werr := crud.DeleteRowTx(tx, orderColTypes, "orders", "id", i)
		require.NoError(t, werr, "crud.DeleteRowTx by PK should succeed for order %d", i)
		require.NoError(t, tx.Commit(), "commit delete %d", i)
	}
	var remaining int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders;`).Scan(&remaining))
	assert.Equal(t, 400, remaining, "100 per-row DELETEs must remove exactly 100 of 500 orders")

	// --- (3) orphan child INSERT (non-existent customer_id) must be REJECTED ---
	tx, err := ddb.Begin()
	require.NoError(t, err, "begin tx for orphan insert")
	_, orphanErr := crud.InsertRowTx(tx, orderColTypes, orderMetas, "orders",
		map[string]any{"id": 99999, "customer_id": 4242, "total": 1.0})
	_ = tx.Rollback()
	orphanRejected := orphanErr != nil
	t.Logf("P4.0 FINDING: orphan-child insert rejected by FK = %v (err=%v)", orphanRejected, orphanErr)
	assert.True(t, orphanRejected, "FK must reject an order referencing a non-existent customer")

	// --- (4) RESTRICT semantics: an UNREFERENCED parent delete must SUCCEED, and
	// a REFERENCED parent delete must be BLOCKED specifically by an FK violation.
	//
	// The CONTROL ASSERTION (unreferenced parent delete succeeds) is mandatory:
	// without it, the dangling-FK-dependency defect found in adversarial review
	// (where the rebuilt child renamed into place left the parent's recorded FK
	// dependency pointing at a dropped temp table, so ALL parent deletes errored
	// with a "Table ... does not exist" catalog error) would masquerade as
	// RESTRICT. A genuine RESTRICT must allow unreferenced deletes AND block
	// referenced ones via a real referential-integrity rejection.
	custColTypes, err := crud.ColumnTypes(ddb, "customers")
	require.NoError(t, err, "read customers column types")

	// Pick a customer that is NOT referenced by any surviving order. (Orders
	// 401-500 were deleted in step (2); customer_id = (i % 50) + 1 means some
	// customers may now be unreferenced.) Insert a guaranteed-unreferenced
	// customer so the control is deterministic.
	_, err = ddb.Exec(`INSERT INTO customers (id, name) VALUES (9999, 'no-orders');`)
	require.NoError(t, err, "seed an unreferenced parent customer")
	var unrefCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE customer_id = 9999;`).Scan(&unrefCount))
	require.Equal(t, 0, unrefCount, "precondition: customer 9999 is referenced by no orders")

	tx, err = ddb.Begin()
	require.NoError(t, err, "begin tx for unreferenced parent delete")
	_, unrefDelErr := crud.DeleteRowTx(tx, custColTypes, "customers", "id", 9999)
	if unrefDelErr == nil {
		require.NoError(t, tx.Commit(), "commit unreferenced parent delete")
	} else {
		_ = tx.Rollback()
	}
	unreferencedParentDeleteAllowed := unrefDelErr == nil
	t.Logf("P4.0 FINDING: unreferenced-parent delete allowed = %v (err=%v)", unreferencedParentDeleteAllowed, unrefDelErr)
	require.NoError(t, unrefDelErr,
		"deleting an UNREFERENCED parent row MUST succeed (proves no dangling FK dependency; guards against the false-positive RESTRICT)")

	// customer 2 is referenced by surviving orders -> RESTRICT should block it.
	var refCount int
	require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE customer_id = 2;`).Scan(&refCount))
	require.Greater(t, refCount, 0, "precondition: customer 2 is still referenced")

	tx, err = ddb.Begin()
	require.NoError(t, err, "begin tx for parent delete")
	_, parentDelErr := crud.DeleteRowTx(tx, custColTypes, "customers", "id", 2)
	if parentDelErr != nil {
		_ = tx.Rollback()
	} else {
		_ = tx.Commit()
	}

	// Only classify as RESTRICT when the referenced-parent delete fails with a
	// genuine foreign-key/constraint violation AND the row survives — NOT on any
	// error (a catalog/"does not exist" error must NOT count as RESTRICT).
	var observedParentDeleteBehavior string
	if parentDelErr != nil {
		var refAfter int
		require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = 2;`).Scan(&refAfter))
		if isFKViolation(parentDelErr) && refAfter == 1 && unreferencedParentDeleteAllowed {
			observedParentDeleteBehavior = engine.FKParentDeleteRestrict
		} else {
			// Errored, but not a clean RESTRICT (e.g. a dangling-dependency
			// catalog error, or it also blocked the unreferenced delete).
			observedParentDeleteBehavior = engine.FKParentDeleteUnenforced
		}
	} else {
		var after int
		require.NoError(t, ddb.QueryRow(`SELECT COUNT(*) FROM orders WHERE customer_id = 2;`).Scan(&after))
		if after == 0 {
			observedParentDeleteBehavior = engine.FKParentDeleteCascade
		} else {
			observedParentDeleteBehavior = engine.FKParentDeleteUnenforced
		}
	}
	t.Logf("P4.0 FINDING: referenced-parent delete behavior = %s (err=%v)", observedParentDeleteBehavior, parentDelErr)
	assert.True(t, isFKViolation(parentDelErr),
		"referenced-parent delete must fail with a foreign-key/constraint violation, got: %v", parentDelErr)
	assert.Equal(t, engine.FKParentDeleteRestrict, observedParentDeleteBehavior,
		"FK must block (RESTRICT) deleting a customer that still has orders, while allowing unreferenced-parent deletes")

	// --- (5) durability: Save (checkpoint -> re-serialize blob) + Close + reopen ---
	require.NoError(t, c.Save(), "save checkpoints DuckDB into the encrypted blob")
	require.NoError(t, c.Close(), "close container")

	c2, err := container.Open(path, container.WithPassphrase(appCRUDPassphrase))
	require.NoError(t, err, "should reopen encrypted .vibe container")
	require.NotNil(t, c2.DuckDBDB(), "DuckDB should be materialized on reopen")
	defer c2.Close() //nolint:errcheck
	ddb2 := c2.DuckDBDB()

	// per-row writes survived the round-trip
	var orderCount, custCount int
	require.NoError(t, ddb2.QueryRow(`SELECT COUNT(*) FROM orders;`).Scan(&orderCount))
	require.NoError(t, ddb2.QueryRow(`SELECT COUNT(*) FROM customers;`).Scan(&custCount))
	assert.Equal(t, 400, orderCount, "order count must survive Save+Close+reopen")
	assert.Equal(t, 50, custCount, "customer count must survive Save+Close+reopen")

	var reopenedTotal float64
	require.NoError(t, ddb2.QueryRow(`SELECT total FROM orders WHERE id = 250;`).Scan(&reopenedTotal))
	assert.InDelta(t, 250.0, reopenedTotal, 0.001, "updated value must persist across reopen")
	assert.True(t, orderCount == 400 && custCount == 50, "all per-row writes must be durable across the round-trip")

	// the FK constraint must still enforce integrity after the blob round-trip
	_, orphanErr2 := ddb2.Exec(`INSERT INTO orders (id, customer_id, total) VALUES (88888, 4242, 1.0);`)
	fkSurvives := orphanErr2 != nil
	t.Logf("P4.0 FINDING: FK still rejects orphan after reopen = %v (err=%v)", fkSurvives, orphanErr2)
	assert.True(t, fkSurvives, "FK constraint must survive the DuckDB-blob serialize/reopen round-trip")

	// This test's original purpose (recording a go/no-go verdict for building
	// App Mode on top of DuckDB-as-CRUD) is moot — App Mode was removed in
	// #68, and engine.AppCRUDViabilitySpikeVerdict()/AppCRUDVerdict were
	// deleted as dead weight along with it. The substantive regression
	// coverage above (durable per-row writes, FK enforcement including the
	// RESTRICT-vs-dangling-dependency distinction, survival across a
	// Save+Close+reopen round-trip) still guards real, kept behavior
	// (internal/crud + plain-SQL foreign keys), so the test itself stays.
}
